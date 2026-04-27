package context_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	ctxpkg "github.com/longyisang/emoagent/internal/context"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/runtimeenv"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/work"
)

type summaryUpdateClient struct {
	responses    []*llm.ChatResponse
	response     *llm.ChatResponse
	err          error
	lastReq      llm.ChatRequest
	chatRequests []llm.ChatRequest
	index        int
}

func (c *summaryUpdateClient) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	c.lastReq = req
	c.chatRequests = append(c.chatRequests, req)
	if len(c.responses) > 0 {
		resp := c.responses[min(c.index, len(c.responses)-1)]
		c.index++
		return resp, c.err
	}
	return c.response, c.err
}

func (c *summaryUpdateClient) ChatStream(context.Context, llm.ChatRequest, llm.StreamCallback) (*llm.ChatResponse, error) {
	panic("unexpected ChatStream call")
}

func TestBuildEmotionContextUsesPinnedContextAndRecentTurns(t *testing.T) {
	persona := &config.Persona{
		Name:         "default",
		Description:  "A steady companion for focused work.",
		SystemPrompt: "You are warm.",
		Tone:         "warm, direct",
		Quirks:       []string{"remembers follow-ups", "keeps replies concise"},
		Greeting:     "hello from greeting only",
	}
	history := []storage.MessageRecord{
		{ID: "1", Role: "user", Content: "old question"},
		{ID: "2", Role: "assistant", Content: "old answer"},
		{ID: "3", Role: "user", Content: "recent question"},
		{ID: "4", Role: "assistant", Content: "recent answer"},
	}

	assembled, err := ctxpkg.BuildEmotionContext(persona, history, config.ContextConfig{
		InputBudgetTokens:    24000,
		SoftCompactRatio:     0.75,
		HardCompactRatio:     0.92,
		ReserveOutputTokens:  4096,
		KeepRecentUserTurns:  1,
		ToolResultSoftTokens: 1000,
		ToolResultHardTokens: 3000,
	}, runtimeenv.Facts{OS: "windows"})
	if err != nil {
		t.Fatalf("BuildEmotionContext: %v", err)
	}

	for _, snippet := range []string{
		"<persona>",
		"You are warm.",
		"## Persona Description\nA steady companion for focused work.",
		"## Tone\nwarm, direct",
		"## Quirks\n- remembers follow-ups\n- keeps replies concise",
		"</persona>",
		"<operating_contract>",
		"</operating_contract>",
		"<runtime_context>",
		"</runtime_context>",
		"<internal_context_data_policy>",
		"</internal_context_data_policy>",
		"running_summary",
		"tool_digests",
	} {
		if !strings.Contains(assembled.System, snippet) {
			t.Fatalf("System = %q, want snippet %q", assembled.System, snippet)
		}
	}
	if strings.Contains(assembled.System, "hello from greeting only") {
		t.Fatalf("System = %q, should not include greeting", assembled.System)
	}
	if !strings.Contains(assembled.System, "Emotion Work Delegation Contract") {
		t.Fatalf("System = %q, want Emotion Work Delegation Contract section", assembled.System)
	}
	if !strings.Contains(assembled.System, "delegate_to_work") {
		t.Fatalf("System = %q, want delegate_to_work reference", assembled.System)
	}
	if !strings.Contains(assembled.System, "Execution environment: Windows.") {
		t.Fatalf("System = %q, want Windows environment note", assembled.System)
	}
	if !strings.Contains(assembled.System, "当前时间上下文：") {
		t.Fatalf("System = %q, want current time context", assembled.System)
	}
	for _, forbidden := range []string{"cmd /c", "Workspace root", "Path style"} {
		if strings.Contains(assembled.System, forbidden) {
			t.Fatalf("Emotion prompt should stay minimal, found %q in %s", forbidden, assembled.System)
		}
	}
	if len(assembled.Messages) != 2 {
		t.Fatalf("len(Messages) = %d, want 2", len(assembled.Messages))
	}
	if assembled.Messages[0].Content != "recent question" {
		t.Fatalf("Messages[0] = %#v, want recent user turn first", assembled.Messages[0])
	}
	if assembled.Messages[1].Content != "recent answer" {
		t.Fatalf("Messages[1] = %#v, want recent assistant turn preserved", assembled.Messages[1])
	}
	for _, msg := range assembled.Messages {
		if strings.Contains(msg.Content, "当前时间上下文：") {
			t.Fatalf("time context should stay out of message slots, found in %#v", msg)
		}
	}
}

func TestKeepRecentByUserTurnsNotMessageCount(t *testing.T) {
	history := []storage.MessageRecord{
		{ID: "1", Role: "user", Content: "u1"},
		{ID: "2", Role: "assistant", Content: "a1"},
		{ID: "3", Role: "assistant", Content: "a1-followup"},
		{ID: "4", Role: "user", Content: "u2"},
		{ID: "5", Role: "assistant", Content: "a2"},
	}

	kept := ctxpkg.KeepRecentUserTurns(history, 1)
	if len(kept) != 2 {
		t.Fatalf("len(kept) = %d, want 2", len(kept))
	}
	if kept[0].Content != "u2" || kept[1].Content != "a2" {
		t.Fatalf("kept = %#v, want only final user turn", kept)
	}
}

func TestCJKTokenEstimatorDiffersFromASCII(t *testing.T) {
	ascii := ctxpkg.EstimateTokens("hello world")
	cjk := ctxpkg.EstimateTokens("你好世界")
	if ascii <= 0 || cjk <= 0 {
		t.Fatalf("EstimateTokens returned non-positive values: ascii=%d cjk=%d", ascii, cjk)
	}
	if cjk == ascii {
		t.Fatalf("EstimateTokens returned same value for ASCII and CJK: %d", cjk)
	}
}

func TestToolDigestTruncatesLargeToolResult(t *testing.T) {
	content := json.RawMessage(`{"body":"` + strings.Repeat("中", 5000) + `"}`)

	digest := ctxpkg.SnipToolResult("web_search", "call_1", content, 100, 200)
	if !digest.IsTruncated {
		t.Fatal("expected tool digest to be truncated")
	}
	if digest.Size <= 0 {
		t.Fatalf("Size = %d, want > 0", digest.Size)
	}
	if digest.Preview == "" {
		t.Fatal("expected preview to be populated")
	}
	if len(digest.FullContent) != 0 {
		t.Fatalf("FullContent = %q, want empty after hard truncation", digest.FullContent)
	}
}

func TestBuildEmotionContextPlacesToolDigestBeforeRecentTurns(t *testing.T) {
	persona := &config.Persona{
		Name:         "default",
		SystemPrompt: "You are warm.",
	}
	history := []storage.MessageRecord{
		{ID: "1", Role: "user", Content: "recent question"},
		{ID: "2", Role: "assistant", Content: "recent answer"},
	}
	digests := []ctxpkg.ToolDigest{
		{
			ToolName:    "web_search",
			CallID:      "call_1",
			Preview:     "Top results: A, B, C",
			Hash:        "deadbeef",
			Size:        128,
			IsTruncated: true,
		},
	}

	assembled, err := ctxpkg.BuildEmotionContextWithToolDigests(persona, history, digests, config.ContextConfig{
		InputBudgetTokens:    24000,
		SoftCompactRatio:     0.75,
		HardCompactRatio:     0.92,
		ReserveOutputTokens:  4096,
		KeepRecentUserTurns:  1,
		ToolResultSoftTokens: 1000,
		ToolResultHardTokens: 3000,
	}, runtimeenv.Facts{OS: "windows"})
	if err != nil {
		t.Fatalf("BuildEmotionContextWithToolDigests: %v", err)
	}

	if len(assembled.Messages) != 3 {
		t.Fatalf("len(Messages) = %d, want 3", len(assembled.Messages))
	}
	if !strings.Contains(assembled.System, "<persona>\nYou are warm.\n</persona>") {
		t.Fatalf("System = %q, want persona section", assembled.System)
	}
	if !strings.Contains(assembled.System, "Emotion Work Delegation Contract") {
		t.Fatalf("System = %q, want Emotion Work Delegation Contract section", assembled.System)
	}
	if assembled.Messages[0].Role != "user" {
		t.Fatalf("Messages[0].Role = %q, want user", assembled.Messages[0].Role)
	}
	if !strings.Contains(assembled.Messages[0].Content, `"tool_digests"`) {
		t.Fatalf("Messages[0] = %#v, want tool digest payload first", assembled.Messages[0])
	}
	if assembled.Messages[1].Content != "recent question" {
		t.Fatalf("Messages[1] = %#v, want recent question after digest", assembled.Messages[1])
	}
	if assembled.Messages[2].Content != "recent answer" {
		t.Fatalf("Messages[2] = %#v, want recent answer last", assembled.Messages[2])
	}
	if !assembled.CompactReport.UsedToolDigest {
		t.Fatal("CompactReport.UsedToolDigest = false, want true")
	}
}

func TestContextStateRoundTripInSessionMetadata(t *testing.T) {
	db := openContextTestDB(t)
	ctx := context.Background()
	if err := db.CreateSession(ctx, "session-1", "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	want := ctxpkg.ContextState{
		ContextVersion: 1,
		Mode:           ctxpkg.ModeEmotion,
		RunningSummary: ctxpkg.RunningSummary{
			SessionGoal: "ship phase 5b",
			UserFacts:   []string{"prefers concise status"},
			RelationshipState: ctxpkg.RelationshipState{
				Tone:          "direct",
				RecentEmotion: "focused",
				PromisesMade:  []string{"report final status"},
			},
			OpenLoops:   []string{"wire summary runtime"},
			Decisions:   []string{"store state in sessions.metadata"},
			DoNotForget: []string{"do not pollute visible history"},
		},
		SummaryCoveredUntilMessageID: "msg-2",
		SummaryUpdatedAt:             "2026-04-16T00:00:00Z",
		LastCompactReason:            "summary",
		LastInputEstimate:            321,
		KeepRecentUserTurns:          4,
	}

	if err := ctxpkg.UpdateSessionContextState(ctx, db, "session-1", want); err != nil {
		t.Fatalf("UpdateSessionContextState: %v", err)
	}

	got, err := ctxpkg.LoadSessionState(ctx, db, "session-1", config.ContextConfig{
		InputBudgetTokens:    24000,
		SoftCompactRatio:     0.75,
		HardCompactRatio:     0.92,
		ReserveOutputTokens:  4096,
		KeepRecentUserTurns:  6,
		ToolResultSoftTokens: 1000,
		ToolResultHardTokens: 3000,
	})
	if err != nil {
		t.Fatalf("LoadSessionState: %v", err)
	}

	if got.ContextVersion != 1 {
		t.Fatalf("ContextVersion = %d, want 1", got.ContextVersion)
	}
	if got.SummaryCoveredUntilMessageID != "msg-2" {
		t.Fatalf("SummaryCoveredUntilMessageID = %q, want msg-2", got.SummaryCoveredUntilMessageID)
	}
	if len(got.RunningSummary.RelationshipState.PromisesMade) != 1 || got.RunningSummary.RelationshipState.PromisesMade[0] != "report final status" {
		t.Fatalf("PromisesMade = %#v, want preserved promise", got.RunningSummary.RelationshipState.PromisesMade)
	}
}

func TestContextStateVersionUpgradeUsesDefaults(t *testing.T) {
	db := openContextTestDB(t)
	ctx := context.Background()
	if err := db.CreateSession(ctx, "session-1", "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	legacy := `{"running_summary":{"session_goal":"legacy"},"summary_covered_until_message_id":"msg-1"}`
	if _, err := db.SqlDB().ExecContext(ctx, `UPDATE sessions SET metadata = ? WHERE id = ?`, legacy, "session-1"); err != nil {
		t.Fatalf("seed legacy metadata: %v", err)
	}

	got, err := ctxpkg.LoadSessionState(ctx, db, "session-1", config.ContextConfig{
		InputBudgetTokens:    24000,
		SoftCompactRatio:     0.75,
		HardCompactRatio:     0.92,
		ReserveOutputTokens:  4096,
		KeepRecentUserTurns:  6,
		ToolResultSoftTokens: 1000,
		ToolResultHardTokens: 3000,
	})
	if err != nil {
		t.Fatalf("LoadSessionState: %v", err)
	}

	if got.ContextVersion != 1 {
		t.Fatalf("ContextVersion = %d, want upgraded to 1", got.ContextVersion)
	}
	if got.Mode != ctxpkg.ModeEmotion {
		t.Fatalf("Mode = %q, want %q", got.Mode, ctxpkg.ModeEmotion)
	}
	if got.KeepRecentUserTurns != 6 {
		t.Fatalf("KeepRecentUserTurns = %d, want 6", got.KeepRecentUserTurns)
	}
	if got.RunningSummary.SessionGoal != "legacy" {
		t.Fatalf("SessionGoal = %q, want legacy", got.RunningSummary.SessionGoal)
	}
}

func TestBuildEmotionContextWithStatePlacesRunningSummaryBeforeRecentTurns(t *testing.T) {
	persona := &config.Persona{
		Name:         "default",
		SystemPrompt: "You are warm.",
	}
	history := []storage.MessageRecord{
		{ID: "1", Role: "user", Content: "recent question"},
		{ID: "2", Role: "assistant", Content: "recent answer"},
	}
	state := &ctxpkg.ContextState{
		ContextVersion: 1,
		Mode:           ctxpkg.ModeEmotion,
		RunningSummary: ctxpkg.RunningSummary{
			SessionGoal: "help user ship context state",
		},
	}

	assembled, err := ctxpkg.BuildEmotionContextWithState(persona, history, state, config.ContextConfig{
		InputBudgetTokens:    24000,
		SoftCompactRatio:     0.75,
		HardCompactRatio:     0.92,
		ReserveOutputTokens:  4096,
		KeepRecentUserTurns:  1,
		ToolResultSoftTokens: 1000,
		ToolResultHardTokens: 3000,
	}, runtimeenv.Facts{OS: "windows"})
	if err != nil {
		t.Fatalf("BuildEmotionContextWithState: %v", err)
	}

	if len(assembled.Messages) != 3 {
		t.Fatalf("len(Messages) = %d, want 3", len(assembled.Messages))
	}
	if assembled.Messages[0].Role != "user" {
		t.Fatalf("Messages[0].Role = %q, want user", assembled.Messages[0].Role)
	}
	if !strings.Contains(assembled.Messages[0].Content, `"running_summary"`) {
		t.Fatalf("Messages[0] = %#v, want running summary envelope first", assembled.Messages[0])
	}
	if assembled.Messages[1].Content != "recent question" {
		t.Fatalf("Messages[1] = %#v, want recent question second", assembled.Messages[1])
	}
	if assembled.Messages[2].Content != "recent answer" {
		t.Fatalf("Messages[2] = %#v, want recent answer last", assembled.Messages[2])
	}
}

func TestBuildEmotionContextWithPendingSummariesAddsResumeNote(t *testing.T) {
	persona := &config.Persona{
		Name:         "default",
		SystemPrompt: "You are warm.",
	}
	history := []storage.MessageRecord{
		{ID: "1", Role: "user", Content: "latest"},
	}
	pending := []work.DecisionSummary{
		{
			TaskID:      "task-1",
			Status:      "stale",
			Category:    string(protocol.CatHumanConfirmation),
			RiskLevel:   "high",
			GoalSummary: "goal",
			Question:    "which option?",
			Options: []protocol.DecisionOption{
				{ID: "a", Summary: "option a"},
			},
			Approval: &protocol.ApprovalSummary{
				Required:  true,
				RequestID: "approval-1",
				Status:    string(protocol.ApprovalStatusPending),
				ExpiresAt: "2026-04-20T01:00:00Z",
			},
			CreatedAt:       "2026-04-20T00:00:00Z",
			StatusEnteredAt: "2026-04-20T00:30:00Z",
			Claimable:       true,
		},
	}

	assembled, err := ctxpkg.BuildEmotionContextWithPendingSummaries(persona, history, nil, pending, config.ContextConfig{
		InputBudgetTokens:    24000,
		SoftCompactRatio:     0.75,
		HardCompactRatio:     0.92,
		ReserveOutputTokens:  4096,
		KeepRecentUserTurns:  1,
		ToolResultSoftTokens: 1000,
		ToolResultHardTokens: 3000,
	}, runtimeenv.Facts{OS: "windows"})
	if err != nil {
		t.Fatalf("BuildEmotionContextWithPendingSummaries: %v", err)
	}

	if !strings.Contains(assembled.System, "Pending Decision(s) Resume Note") {
		t.Fatalf("system prompt missing resume note: %s", assembled.System)
	}
	if !strings.Contains(assembled.System, "<pending_work>") || !strings.Contains(assembled.System, "</pending_work>") {
		t.Fatalf("system prompt missing pending_work section: %s", assembled.System)
	}
	if !strings.Contains(assembled.System, "task-1") {
		t.Fatalf("system prompt missing pending task id: %s", assembled.System)
	}
	if !strings.Contains(assembled.System, "Status: stale") {
		t.Fatalf("system prompt missing summary status: %s", assembled.System)
	}
	if !strings.Contains(assembled.System, "Approval request: approval-1") {
		t.Fatalf("system prompt missing approval request id: %s", assembled.System)
	}
	if !strings.Contains(assembled.System, "approval_request_id") {
		t.Fatalf("system prompt should explain resume_work approval_request_id usage: %s", assembled.System)
	}
}

func TestBuildEmotionContext_IncludesToolApprovalGuidance(t *testing.T) {
	persona := &config.Persona{
		Name:         "default",
		SystemPrompt: "You are warm.",
	}

	assembled, err := ctxpkg.BuildEmotionContext(persona, nil, config.ContextConfig{
		InputBudgetTokens:    24000,
		SoftCompactRatio:     0.75,
		HardCompactRatio:     0.92,
		ReserveOutputTokens:  4096,
		KeepRecentUserTurns:  1,
		ToolResultSoftTokens: 1000,
		ToolResultHardTokens: 3000,
	}, runtimeenv.Facts{OS: "windows"})
	if err != nil {
		t.Fatalf("BuildEmotionContext: %v", err)
	}

	for _, snippet := range []string{
		"destructive tool call needs approval",
		"explain the operation",
		"ask for confirmation",
		"approval_request_id",
	} {
		if !strings.Contains(assembled.System, snippet) {
			t.Fatalf("system prompt missing %q: %s", snippet, assembled.System)
		}
	}
}

func TestBuildEmotionContext_IncludesPermissionEscalationGuidance(t *testing.T) {
	persona := &config.Persona{
		Name:         "default",
		SystemPrompt: "You are warm.",
	}

	assembled, err := ctxpkg.BuildEmotionContext(persona, nil, config.ContextConfig{
		InputBudgetTokens:    24000,
		SoftCompactRatio:     0.75,
		HardCompactRatio:     0.92,
		ReserveOutputTokens:  4096,
		KeepRecentUserTurns:  1,
		ToolResultSoftTokens: 1000,
		ToolResultHardTokens: 3000,
	}, runtimeenv.Facts{OS: "windows"})
	if err != nil {
		t.Fatalf("BuildEmotionContext: %v", err)
	}

	for _, snippet := range []string{
		"permission_escalation_required",
		"never self-approve",
		"Ask the user for destructive permission",
		"permission_scope_override",
		"emotion_judgment",
	} {
		if !strings.Contains(assembled.System, snippet) {
			t.Fatalf("system prompt missing %q: %s", snippet, assembled.System)
		}
	}
}

func TestCompactReportIncludesReasonAndTokenDeltas(t *testing.T) {
	compacted, report, err := ctxpkg.ApplyReactiveCompact(
		"session-1",
		[]llm.Message{
			{Role: llm.RoleUser, Content: `{"running_summary":{"session_goal":"keep summary"}}`},
			{Role: llm.RoleUser, Content: "older user"},
			{Role: llm.RoleAssistant, Content: "older assistant"},
			{Role: llm.RoleUser, Content: "latest user"},
			{Role: llm.RoleTool, ToolCallID: "call-1", Content: `{"body":"` + strings.Repeat("x", 10000) + `"}`},
		},
		&ctxpkg.ContextState{SummaryCoveredUntilMessageID: "msg-2"},
		"summary-model",
		config.ContextConfig{
			InputBudgetTokens:    24000,
			SoftCompactRatio:     0.75,
			HardCompactRatio:     0.92,
			ReserveOutputTokens:  4096,
			KeepRecentUserTurns:  1,
			ToolResultSoftTokens: 10,
			ToolResultHardTokens: 20,
		},
	)
	if err != nil {
		t.Fatalf("ApplyReactiveCompact: %v", err)
	}
	if len(compacted) != 3 {
		t.Fatalf("len(compacted) = %d, want 3", len(compacted))
	}
	if report.Mode != "reactive" {
		t.Fatalf("Mode = %q, want reactive", report.Mode)
	}
	if report.CompactReason != "reactive_overflow" {
		t.Fatalf("CompactReason = %q, want reactive_overflow", report.CompactReason)
	}
	if report.PreEstimatedTokens <= report.PostEstimatedTokens {
		t.Fatalf("token delta = %d -> %d, want pre > post", report.PreEstimatedTokens, report.PostEstimatedTokens)
	}
	if report.SnippedToolResultsCount != 1 {
		t.Fatalf("SnippedToolResultsCount = %d, want 1", report.SnippedToolResultsCount)
	}
}

func TestCompactReportCarriesSummaryCoverageAndSummaryModel(t *testing.T) {
	_, report, err := ctxpkg.ApplyReactiveCompact(
		"session-42",
		[]llm.Message{
			{Role: llm.RoleUser, Content: `{"running_summary":{"session_goal":"keep summary"}}`},
			{Role: llm.RoleUser, Content: "latest user"},
		},
		&ctxpkg.ContextState{SummaryCoveredUntilMessageID: "msg-9"},
		"summary-model",
		config.ContextConfig{
			InputBudgetTokens:    24000,
			SoftCompactRatio:     0.75,
			HardCompactRatio:     0.92,
			ReserveOutputTokens:  4096,
			KeepRecentUserTurns:  1,
			ToolResultSoftTokens: 1000,
			ToolResultHardTokens: 3000,
		},
	)
	if err != nil {
		t.Fatalf("ApplyReactiveCompact: %v", err)
	}
	if report.SessionID != "session-42" {
		t.Fatalf("SessionID = %q, want session-42", report.SessionID)
	}
	if report.SummaryCoveredUntilMessageID != "msg-9" {
		t.Fatalf("SummaryCoveredUntilMessageID = %q, want msg-9", report.SummaryCoveredUntilMessageID)
	}
	if report.SummaryModel != "summary-model" {
		t.Fatalf("SummaryModel = %q, want summary-model", report.SummaryModel)
	}
}

func TestRunningSummaryIterativeUpdateUsesModelOutputAsMergedSummary(t *testing.T) {
	client := &summaryUpdateClient{
		response: &llm.ChatResponse{
			ID:      "summary-1",
			Model:   "summary-model",
			Content: `{"running_summary":{"session_goal":"updated goal","user_facts":[],"relationship_state":{"tone":"steady","recent_emotion":"focused","promises_made":[]},"open_loops":["new loop"],"decisions":["new decision"],"do_not_forget":[]}}`,
		},
	}
	state := &ctxpkg.ContextState{
		ContextVersion: 1,
		Mode:           ctxpkg.ModeEmotion,
		RunningSummary: ctxpkg.RunningSummary{
			SessionGoal: "old goal",
			RelationshipState: ctxpkg.RelationshipState{
				PromisesMade: []string{"follow up tomorrow"},
			},
			DoNotForget: []string{"user needs concise updates"},
		},
	}
	history := []storage.MessageRecord{
		{ID: "m1", Role: "user", Content: "older user"},
		{ID: "m2", Role: "assistant", Content: "older assistant"},
		{ID: "m3", Role: "user", Content: "latest user"},
	}

	next, report, err := ctxpkg.UpdateRunningSummary(context.Background(), client, "summary-model", 2048, nil, &config.Persona{Name: "default", SystemPrompt: "system"}, history, state, config.ContextConfig{
		InputBudgetTokens:    24000,
		SoftCompactRatio:     0.75,
		HardCompactRatio:     0.92,
		ReserveOutputTokens:  4096,
		KeepRecentUserTurns:  1,
		ToolResultSoftTokens: 1000,
		ToolResultHardTokens: 3000,
	})
	if err != nil {
		t.Fatalf("UpdateRunningSummary: %v", err)
	}
	if !report.Attempted || report.DeltaCount != 2 {
		t.Fatalf("SummaryUpdateReport = %#v, want attempted delta_count=2", report)
	}
	if len(next.RunningSummary.RelationshipState.PromisesMade) != 0 {
		t.Fatalf("PromisesMade = %#v, want model-authoritative empty list", next.RunningSummary.RelationshipState.PromisesMade)
	}
	if len(next.RunningSummary.DoNotForget) != 0 {
		t.Fatalf("DoNotForget = %#v, want model-authoritative empty list", next.RunningSummary.DoNotForget)
	}
	if next.RunningSummary.SessionGoal != "updated goal" {
		t.Fatalf("SessionGoal = %q, want updated goal", next.RunningSummary.SessionGoal)
	}
	if client.lastReq.Temperature != 0.1 {
		t.Fatalf("summary request temperature = %v, want default 0.1", client.lastReq.Temperature)
	}
	if client.lastReq.MaxTokens != 2048 {
		t.Fatalf("summary request max tokens = %d, want configured 2048", client.lastReq.MaxTokens)
	}
}

func TestRunningSummaryPromptIncludesSchemaAndSafetyRules(t *testing.T) {
	client := &summaryUpdateClient{
		response: &llm.ChatResponse{
			ID:      "summary-1",
			Model:   "summary-model",
			Content: `{"running_summary":{"session_goal":"updated goal","user_facts":[],"relationship_state":{"tone":"","recent_emotion":"","promises_made":[]},"open_loops":[],"decisions":[],"do_not_forget":[]}}`,
		},
	}

	_, _, err := ctxpkg.UpdateRunningSummary(context.Background(), client, "summary-model", 0, nil, &config.Persona{Name: "default", SystemPrompt: "system"}, summaryParsingHistory(), nil, testContextConfig())
	if err != nil {
		t.Fatalf("UpdateRunningSummary: %v", err)
	}
	for _, snippet := range []string{
		`"running_summary"`,
		`"relationship_state"`,
		"Do not store credentials",
		"Remove obsolete items",
		"JSON only",
	} {
		if !strings.Contains(client.lastReq.System, snippet) {
			t.Fatalf("summary system prompt missing %q: %s", snippet, client.lastReq.System)
		}
	}
}

func TestRunningSummaryIterativeUpdateUsesConfiguredSummaryTemperature(t *testing.T) {
	summaryTemperature := 0.35
	client := &summaryUpdateClient{
		response: &llm.ChatResponse{
			ID:      "summary-1",
			Model:   "summary-model",
			Content: summaryContent("updated goal"),
		},
	}
	history := []storage.MessageRecord{
		{ID: "m1", Role: "user", Content: "older user"},
		{ID: "m2", Role: "assistant", Content: "older assistant"},
		{ID: "m3", Role: "user", Content: "latest user"},
	}

	_, _, err := ctxpkg.UpdateRunningSummary(context.Background(), client, "summary-model", 0, &summaryTemperature, &config.Persona{Name: "default", SystemPrompt: "system"}, history, nil, config.ContextConfig{
		InputBudgetTokens:    24000,
		SoftCompactRatio:     0.75,
		HardCompactRatio:     0.92,
		ReserveOutputTokens:  4096,
		KeepRecentUserTurns:  1,
		ToolResultSoftTokens: 1000,
		ToolResultHardTokens: 3000,
	})
	if err != nil {
		t.Fatalf("UpdateRunningSummary: %v", err)
	}
	if client.lastReq.Temperature != summaryTemperature {
		t.Fatalf("summary request temperature = %v, want %v", client.lastReq.Temperature, summaryTemperature)
	}
	if client.lastReq.MaxTokens != 4096 {
		t.Fatalf("summary request max tokens = %d, want default 4096", client.lastReq.MaxTokens)
	}
}

func TestRunningSummaryParsesFencedJSON(t *testing.T) {
	client := &summaryUpdateClient{
		response: &llm.ChatResponse{
			ID:      "summary-1",
			Model:   "summary-model",
			Content: "```json\n" + summaryContent("from fenced json") + "\n```",
		},
	}
	next := runSummaryUpdateForParsing(t, client)
	if next.RunningSummary.SessionGoal != "from fenced json" {
		t.Fatalf("SessionGoal = %q, want fenced JSON summary", next.RunningSummary.SessionGoal)
	}
}

func TestRunningSummaryParsesEmbeddedJSON(t *testing.T) {
	client := &summaryUpdateClient{
		response: &llm.ChatResponse{
			ID:      "summary-1",
			Model:   "summary-model",
			Content: "好的，摘要如下：" + summaryContent("from embedded json"),
		},
	}
	next := runSummaryUpdateForParsing(t, client)
	if next.RunningSummary.SessionGoal != "from embedded json" {
		t.Fatalf("SessionGoal = %q, want embedded JSON summary", next.RunningSummary.SessionGoal)
	}
}

func TestRunningSummaryParsesEmbeddedJSONAfterNonSummaryObject(t *testing.T) {
	client := &summaryUpdateClient{
		response: &llm.ChatResponse{
			ID:      "summary-1",
			Model:   "summary-model",
			Content: "{\"note\":\"not the summary\"}\n" + summaryContent("from second object"),
		},
	}
	next := runSummaryUpdateForParsing(t, client)
	if next.RunningSummary.SessionGoal != "from second object" {
		t.Fatalf("SessionGoal = %q, want second JSON object summary", next.RunningSummary.SessionGoal)
	}
}

func TestRunningSummaryUsesReasoningContentJSONFallback(t *testing.T) {
	client := &summaryUpdateClient{
		response: &llm.ChatResponse{
			ID:               "summary-1",
			Model:            "summary-model",
			ReasoningContent: summaryContent("from reasoning json"),
		},
	}
	next := runSummaryUpdateForParsing(t, client)
	if next.RunningSummary.SessionGoal != "from reasoning json" {
		t.Fatalf("SessionGoal = %q, want reasoning JSON fallback summary", next.RunningSummary.SessionGoal)
	}
}

func TestRunningSummaryRejectsFreeformReasoningContent(t *testing.T) {
	client := &summaryUpdateClient{
		response: &llm.ChatResponse{
			ID:               "summary-1",
			Model:            "summary-model",
			ReasoningContent: "I should summarize this conversation, but this is not JSON.",
		},
	}
	next, _, err := ctxpkg.UpdateRunningSummary(context.Background(), client, "summary-model", 0, nil, &config.Persona{Name: "default", SystemPrompt: "system"}, summaryParsingHistory(), nil, testContextConfig())
	if err == nil {
		t.Fatal("UpdateRunningSummary err = nil, want error for freeform reasoning")
	}
	if next == nil {
		t.Fatal("next state is nil, want failure state")
	}
	if next.SummaryFailureCount != 1 {
		t.Fatalf("SummaryFailureCount = %d, want 1", next.SummaryFailureCount)
	}
	if next.SummaryRetryAfter == "" {
		t.Fatal("SummaryRetryAfter is empty, want cooldown timestamp")
	}
}

func TestRunningSummaryRepairsInvalidLeakySummary(t *testing.T) {
	client := &summaryUpdateClient{
		responses: []*llm.ChatResponse{
			{
				ID:      "summary-1",
				Model:   "summary-model",
				Content: `{"running_summary":{"session_goal":"bad","user_facts":["decision_packet leaked"],"relationship_state":{"tone":"","recent_emotion":"","promises_made":[]},"open_loops":[],"decisions":[],"do_not_forget":[]}}`,
			},
			{
				ID:      "summary-2",
				Model:   "summary-model",
				Content: `{"running_summary":{"session_goal":"repaired","user_facts":[],"relationship_state":{"tone":"","recent_emotion":"","promises_made":[]},"open_loops":[],"decisions":[],"do_not_forget":[]}}`,
			},
		},
	}

	next, _, err := ctxpkg.UpdateRunningSummary(context.Background(), client, "summary-model", 0, nil, &config.Persona{Name: "default", SystemPrompt: "system"}, summaryParsingHistory(), nil, testContextConfig())
	if err != nil {
		t.Fatalf("UpdateRunningSummary: %v", err)
	}
	if got := len(client.chatRequests); got != 2 {
		t.Fatalf("summary calls = %d, want initial + repair", got)
	}
	if !strings.Contains(client.chatRequests[1].System, "Repair") {
		t.Fatalf("repair system prompt = %q, want repair instruction", client.chatRequests[1].System)
	}
	if next.RunningSummary.SessionGoal != "repaired" {
		t.Fatalf("SessionGoal = %q, want repaired", next.RunningSummary.SessionGoal)
	}
}

func TestRunningSummaryRepairsMissingRequiredStructure(t *testing.T) {
	client := &summaryUpdateClient{
		responses: []*llm.ChatResponse{
			{
				ID:      "summary-1",
				Model:   "summary-model",
				Content: `{"running_summary":{"session_goal":"bad","relationship_state":{"tone":"","recent_emotion":"","promises_made":[]}}}`,
			},
			{
				ID:      "summary-2",
				Model:   "summary-model",
				Content: summaryContent("repaired structure"),
			},
		},
	}

	next, _, err := ctxpkg.UpdateRunningSummary(context.Background(), client, "summary-model", 0, nil, &config.Persona{Name: "default", SystemPrompt: "system"}, summaryParsingHistory(), nil, testContextConfig())
	if err != nil {
		t.Fatalf("UpdateRunningSummary: %v", err)
	}
	if got := len(client.chatRequests); got != 2 {
		t.Fatalf("summary calls = %d, want initial + repair", got)
	}
	if next.RunningSummary.SessionGoal != "repaired structure" {
		t.Fatalf("SessionGoal = %q, want repaired structure", next.RunningSummary.SessionGoal)
	}
}

func TestRunningSummaryFailureCooldownSkipsNextAttempt(t *testing.T) {
	firstClient := &summaryUpdateClient{
		response: &llm.ChatResponse{ID: "summary-1", Model: "summary-model", Content: "not json"},
	}
	state, _, err := ctxpkg.UpdateRunningSummary(context.Background(), firstClient, "summary-model", 0, nil, &config.Persona{Name: "default", SystemPrompt: "system"}, summaryParsingHistory(), nil, testContextConfig())
	if err == nil {
		t.Fatal("first UpdateRunningSummary err = nil, want parse failure")
	}
	if len(firstClient.chatRequests) != 2 {
		t.Fatalf("first summary calls = %d, want initial + repair", len(firstClient.chatRequests))
	}

	secondClient := &summaryUpdateClient{
		response: &llm.ChatResponse{ID: "summary-2", Model: "summary-model", Content: `{"running_summary":{"session_goal":"should not be used"}}`},
	}
	next, report, err := ctxpkg.UpdateRunningSummary(context.Background(), secondClient, "summary-model", 0, nil, &config.Persona{Name: "default", SystemPrompt: "system"}, summaryParsingHistory(), state, testContextConfig())
	if err != nil {
		t.Fatalf("second UpdateRunningSummary: %v", err)
	}
	if len(secondClient.chatRequests) != 0 {
		t.Fatalf("second summary calls = %d, want 0 during cooldown", len(secondClient.chatRequests))
	}
	if !report.Skipped || report.SkipReason != "summary_retry_cooldown" {
		t.Fatalf("SummaryUpdateReport = %#v, want cooldown skip", report)
	}
	if next.SummaryCoveredUntilMessageID != "" {
		t.Fatalf("SummaryCoveredUntilMessageID = %q, want unchanged empty coverage", next.SummaryCoveredUntilMessageID)
	}
}

func TestRunningSummarySuccessClearsFailureState(t *testing.T) {
	client := &summaryUpdateClient{
		response: &llm.ChatResponse{
			ID:      "summary-1",
			Model:   "summary-model",
			Content: summaryContent("recovered"),
		},
	}
	state := &ctxpkg.ContextState{
		SummaryFailedAt:     "2026-04-24T00:00:00Z",
		SummaryRetryAfter:   "2026-04-24T00:02:00Z",
		SummaryFailureCount: 3,
		SummaryLastError:    "previous failure",
	}
	next, _, err := ctxpkg.UpdateRunningSummary(context.Background(), client, "summary-model", 0, nil, &config.Persona{Name: "default", SystemPrompt: "system"}, summaryParsingHistory(), state, testContextConfig())
	if err != nil {
		t.Fatalf("UpdateRunningSummary: %v", err)
	}
	if next.SummaryFailureCount != 0 || next.SummaryFailedAt != "" || next.SummaryRetryAfter != "" || next.SummaryLastError != "" {
		t.Fatalf("failure state was not cleared: %#v", next)
	}
}

func runSummaryUpdateForParsing(t *testing.T, client *summaryUpdateClient) *ctxpkg.ContextState {
	t.Helper()
	next, _, err := ctxpkg.UpdateRunningSummary(context.Background(), client, "summary-model", 0, nil, &config.Persona{Name: "default", SystemPrompt: "system"}, summaryParsingHistory(), nil, testContextConfig())
	if err != nil {
		t.Fatalf("UpdateRunningSummary: %v", err)
	}
	return next
}

func summaryParsingHistory() []storage.MessageRecord {
	return []storage.MessageRecord{
		{ID: "m1", Role: "user", Content: "older user"},
		{ID: "m2", Role: "assistant", Content: "older assistant"},
		{ID: "m3", Role: "user", Content: "latest user"},
	}
}

func summaryContent(goal string) string {
	payload, err := json.Marshal(map[string]any{
		"running_summary": map[string]any{
			"session_goal":  goal,
			"user_facts":    []string{},
			"open_loops":    []string{},
			"decisions":     []string{},
			"do_not_forget": []string{},
			"relationship_state": map[string]any{
				"tone":           "",
				"recent_emotion": "",
				"promises_made":  []string{},
			},
		},
	})
	if err != nil {
		panic(err)
	}
	return string(payload)
}

func testContextConfig() config.ContextConfig {
	return config.ContextConfig{
		InputBudgetTokens:    24000,
		SoftCompactRatio:     0.75,
		HardCompactRatio:     0.92,
		ReserveOutputTokens:  4096,
		KeepRecentUserTurns:  1,
		ToolResultSoftTokens: 1000,
		ToolResultHardTokens: 3000,
	}
}

func openContextTestDB(t *testing.T) *storage.DB {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := storage.Open(filepath.Join(t.TempDir(), "context.db"), logger)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
