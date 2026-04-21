package context

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/runtimeenv"
	"github.com/longyisang/emoagent/internal/storage"
)

const delegationGuideline = `## Delegation Guideline

When the user's request fits the criteria below, call delegate_to_work instead of trying to handle it yourself:
- Requires reading files, exploring directories, or running commands.
- Needs multi-step tool calls (3 or more steps) to complete.
- Produces large or noisy intermediate output that should stay out of the main chat.
- Requires verification or long-chain research.

When the user just wants to talk, vent, ask a trivial factual question, or wants you to express something, handle it yourself. Do not delegate casual conversation.

Set permission_scope to "workspace-write" only when the task explicitly requires writing files or running shell commands; use "read-only" by default.

The TaskReport you receive is for your eyes only. Never paste raw tool output into your reply; summarize findings in your own voice.

When delegate_to_work returns {"status":"needs_emotion_decision"}, a Work task paused and needs your judgment.

Step 1: Determine whether you can decide from your persona, conversation history, relationship memory, and the decision packet's findings/tradeoffs/recommendation.
If you can decide confidently, call resume_work immediately in this turn.

Step 2: Only if you genuinely lack information that the user has never provided and cannot infer, ask a natural-language follow-up question and end your turn.
Do not expose raw JSON to the user. Never mention "decision_packet".

Category guidance:
- auto: resume immediately when the packet is operational and you can decide confidently without asking the user.
- emotion_judgment: use persona, conversation history, and relationship memory; ask only if genuinely missing necessary user information.
- human_confirmation: clearly explain the consequence and request explicit confirmation before resuming.
- tool_approval is runtime-only: a destructive tool call needs approval, so explain the operation, ask for confirmation, and then call resume_work with approval_request_id once approval is granted. Do not ask Work to emit tool_approval.

If resume_work returns {"status":"expired"}, apologize naturally and offer to re-run the task.`

// BuildEmotionContext assembles the emotion context with no persisted session state.
func BuildEmotionContext(persona *config.Persona, history []storage.MessageRecord, cfg config.ContextConfig, env runtimeenv.Facts) (AssembledContext, error) {
	return buildEmotionContext(persona, history, nil, nil, nil, cfg, env)
}

// BuildEmotionContextWithState assembles the emotion context using persisted session state.
func BuildEmotionContextWithState(persona *config.Persona, history []storage.MessageRecord, state *ContextState, cfg config.ContextConfig, env runtimeenv.Facts) (AssembledContext, error) {
	return buildEmotionContext(persona, history, state, nil, nil, cfg, env)
}

// BuildEmotionContextWithToolDigests assembles the emotion context with an explicit ToolDigest slot.
func BuildEmotionContextWithToolDigests(persona *config.Persona, history []storage.MessageRecord, toolDigests []ToolDigest, cfg config.ContextConfig, env runtimeenv.Facts) (AssembledContext, error) {
	return buildEmotionContext(persona, history, nil, toolDigests, nil, cfg, env)
}

// BuildEmotionContextWithPending assembles context and injects paused decision notes.
func BuildEmotionContextWithPending(persona *config.Persona, history []storage.MessageRecord, state *ContextState, pendingDecisions []protocol.DecisionPacket, cfg config.ContextConfig, env runtimeenv.Facts) (AssembledContext, error) {
	return buildEmotionContext(persona, history, state, nil, pendingDecisions, cfg, env)
}

// BuildEmotionContextWithPendingSummaries assembles context and injects persisted decision summaries.
func BuildEmotionContextWithPendingSummaries(persona *config.Persona, history []storage.MessageRecord, state *ContextState, pendingSummaries []protocol.DecisionSummary, cfg config.ContextConfig, env runtimeenv.Facts) (AssembledContext, error) {
	return buildEmotionContext(persona, history, state, nil, pendingSummaries, cfg, env)
}

func buildEmotionContext(persona *config.Persona, history []storage.MessageRecord, state *ContextState, toolDigests []ToolDigest, pendingDecisions any, cfg config.ContextConfig, env runtimeenv.Facts) (AssembledContext, error) {
	if persona == nil {
		return AssembledContext{}, fmt.Errorf("persona is required")
	}
	if err := cfg.Validate(); err != nil {
		return AssembledContext{}, err
	}

	recent := KeepRecentUserTurns(history, cfg.KeepRecentUserTurns)
	recentMessages := make([]llm.Message, 0, len(recent))
	for _, msg := range recent {
		recentMessages = append(recentMessages, llm.Message{
			Role:    llm.Role(msg.Role),
			Content: msg.Content,
		})
	}

	messages, err := composeEmotionMessages(state, toolDigests, recentMessages)
	if err != nil {
		return AssembledContext{}, err
	}
	system := buildEmotionSystemPrompt(persona.SystemPrompt, pendingDecisions, env)
	budget := NewBudget(cfg, system, messages)
	return AssembledContext{
		System:      system,
		ToolDigests: append([]ToolDigest(nil), toolDigests...),
		Messages:    messages,
		Budget:      budget,
		CompactReport: CompactReport{
			Mode:                    "deterministic",
			CompactReason:           "budget_soft",
			KeptRecentTurns:         cfg.KeepRecentUserTurns,
			SnippedToolResultsCount: len(toolDigests),
			PreEstimatedTokens:      budget.EstimatedTokens,
			PostEstimatedTokens:     budget.EstimatedTokens,
			KeptRecentUserTurns:     cfg.KeepRecentUserTurns,
			SnippedToolResults:      len(toolDigests),
			UsedToolDigest:          len(toolDigests) > 0,
		},
	}, nil
}

func buildEmotionSystemPrompt(base string, pendingDecisions any, env runtimeenv.Facts) string {
	var result string
	if base == "" {
		result = delegationGuideline
	} else {
		result = base + "\n\n" + delegationGuideline
	}
	if env.OS != "" {
		result += "\n\nExecution environment: " + env.DisplayOS() + "."
	}
	switch items := pendingDecisions.(type) {
	case nil:
		return result
	case []protocol.DecisionPacket:
		if len(items) == 0 {
			return result
		}
		return result + "\n\n" + buildResumeNote(items)
	case []protocol.DecisionSummary:
		if len(items) == 0 {
			return result
		}
		return result + "\n\n" + buildResumeSummaryNote(items)
	default:
		return result
	}
}

func composeEmotionMessages(state *ContextState, toolDigests []ToolDigest, recentMessages []llm.Message) ([]llm.Message, error) {
	capHint := len(recentMessages) + 1
	if len(toolDigests) > 0 {
		capHint++
	}
	if state != nil && !state.RunningSummary.IsZero() {
		capHint++
	}
	messages := make([]llm.Message, 0, capHint)
	for _, slot := range EmotionSlotOrder {
		switch slot {
		case SlotPinnedContext:
			continue
		case SlotRunningSummary:
			if state == nil || state.RunningSummary.IsZero() {
				continue
			}
			msg, err := buildRunningSummarySlotMessage(state.RunningSummary)
			if err != nil {
				return nil, err
			}
			messages = append(messages, msg)
		case SlotToolDigest:
			if len(toolDigests) == 0 {
				continue
			}
			msg, err := buildToolDigestSlotMessage(toolDigests)
			if err != nil {
				return nil, err
			}
			messages = append(messages, msg)
		case SlotRecentTurns:
			messages = append(messages, recentMessages...)
		default:
			return nil, fmt.Errorf("unsupported emotion slot: %s", slot)
		}
	}
	return messages, nil
}

func buildRunningSummarySlotMessage(summary RunningSummary) (llm.Message, error) {
	payload, err := json.Marshal(struct {
		RunningSummary RunningSummary `json:"running_summary"`
	}{
		RunningSummary: normalizeRunningSummary(summary),
	})
	if err != nil {
		return llm.Message{}, fmt.Errorf("marshal running summary slot: %w", err)
	}
	return llm.Message{
		Role:    llm.RoleUser,
		Content: string(payload),
	}, nil
}

func buildToolDigestSlotMessage(toolDigests []ToolDigest) (llm.Message, error) {
	payload, err := json.Marshal(struct {
		ToolDigests []ToolDigest `json:"tool_digests"`
	}{
		ToolDigests: toolDigests,
	})
	if err != nil {
		return llm.Message{}, fmt.Errorf("marshal tool digest slot: %w", err)
	}
	return llm.Message{
		Role:    llm.RoleUser,
		Content: string(payload),
	}, nil
}

func buildResumeNote(packets []protocol.DecisionPacket) string {
	var b strings.Builder
	b.WriteString("## Pending Decision(s) Resume Note\n\n")
	b.WriteString("The following Work task(s) are paused waiting for your decision.\n\n")

	for i, p := range packets {
		if i > 0 {
			b.WriteString("---\n\n")
		}
		fmt.Fprintf(&b, "Task: %s\n", p.TaskID)
		fmt.Fprintf(&b, "Category: %s | Risk: %s\n", p.Category, displayRiskLevel(p.Category, p.RiskLevel))
		fmt.Fprintf(&b, "Goal: %s\n", p.GoalSummary)
		fmt.Fprintf(&b, "Question: %s\n", p.Question)
		fmt.Fprintf(&b, "Why blocked: %s\n\n", p.WhyBlocked)

		b.WriteString("Options:\n")
		for _, opt := range p.Options {
			fmt.Fprintf(&b, "- %s: %s\n", opt.ID, opt.Summary)
			for _, pro := range opt.Pros {
				fmt.Fprintf(&b, "  Pro: %s\n", pro)
			}
			for _, con := range opt.Cons {
				fmt.Fprintf(&b, "  Con: %s\n", con)
			}
		}
		b.WriteString("\n")

		if len(p.RelevantFindings) > 0 {
			b.WriteString("Relevant findings:\n")
			for _, f := range p.RelevantFindings {
				fmt.Fprintf(&b, "- %s\n", f.Finding)
			}
			b.WriteString("\n")
		}
		if len(p.KeyTradeoffs) > 0 {
			b.WriteString("Key tradeoffs:\n")
			for _, t := range p.KeyTradeoffs {
				fmt.Fprintf(&b, "- %s: %s\n", t.Dimension, t.Note)
			}
			b.WriteString("\n")
		}
		if p.RecommendedOption != "" {
			fmt.Fprintf(&b, "Work recommends: %s — %s\n\n", p.RecommendedOption, p.RecommendationReason)
		}
	}

	b.WriteString("Action: Determine the decision and call resume_work. Use task_id plus decision/reason for ordinary pauses. For fail-closed approval-gated pauses, wait for approval and then resume with task_id and approval_request_id.")
	return b.String()
}

func buildResumeSummaryNote(summaries []protocol.DecisionSummary) string {
	var b strings.Builder
	b.WriteString("## Pending Decision(s) Resume Note\n\n")
	b.WriteString("The following Work task(s) are paused waiting for your decision.\n\n")

	for i, s := range summaries {
		if i > 0 {
			b.WriteString("---\n\n")
		}
		fmt.Fprintf(&b, "Task: %s\n", s.TaskID)
		fmt.Fprintf(&b, "Status: %s\n", s.Status)
		fmt.Fprintf(&b, "Category: %s | Risk: %s\n", s.Category, displayRiskLevel(protocol.EscalationCategory(s.Category), s.RiskLevel))
		fmt.Fprintf(&b, "Goal: %s\n", s.GoalSummary)
		fmt.Fprintf(&b, "Question: %s\n", s.Question)
		fmt.Fprintf(&b, "Claimable: %t\n", s.Claimable)
		if len(s.Options) > 0 {
			b.WriteString("Options:\n")
			for _, opt := range s.Options {
				fmt.Fprintf(&b, "- %s: %s\n", opt.ID, opt.Summary)
			}
		}
		if s.Approval != nil && s.Approval.Required {
			fmt.Fprintf(&b, "Approval request: %s\n", s.Approval.RequestID)
			if s.Approval.Status != "" {
				fmt.Fprintf(&b, "Approval status: %s\n", s.Approval.Status)
			}
			if s.Approval.SelectedOptionID != "" {
				fmt.Fprintf(&b, "Approved option: %s\n", s.Approval.SelectedOptionID)
			}
			if s.Approval.ExpiresAt != "" {
				fmt.Fprintf(&b, "Approval expires at: %s\n", s.Approval.ExpiresAt)
			}
		}
		if s.Report != nil && s.Report.Summary != "" {
			fmt.Fprintf(&b, "\nReport: %s\n", s.Report.Summary)
		}
		b.WriteString("\n")
	}

	b.WriteString("Action: Determine the decision and call resume_work. Use task_id plus decision/reason for ordinary pauses. For fail-closed approval-gated pauses, wait for approval and then resume with task_id and approval_request_id.")
	return b.String()
}

func displayRiskLevel(category protocol.EscalationCategory, explicit string) string {
	if explicit != "" {
		return explicit
	}
	switch category {
	case protocol.CatHumanConfirmation, protocol.CatToolApproval:
		return "high"
	default:
		return "low"
	}
}
