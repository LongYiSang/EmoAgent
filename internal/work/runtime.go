package work

import (
	"context"
	"fmt"
	"log/slog"

	contextutil "github.com/longyisang/emoagent/internal/context"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/tool"
)

// RuntimeConfig describes the dependencies for one Work runtime instance.
type RuntimeConfig struct {
	LLM            llm.Client
	Provider       string
	Model          string
	MaxTokens      int
	Temperature    float64
	MaxToolRounds  int
	MaxInputTokens int
	Registry       *tool.Registry
	Dispatcher     *tool.Dispatcher
	Logger         *slog.Logger
}

// Runtime executes one isolated Work task.
type Runtime struct {
	cfg RuntimeConfig
}

// NewRuntime constructs a Work runtime from the provided dependencies.
func NewRuntime(cfg RuntimeConfig) *Runtime {
	return &Runtime{cfg: cfg}
}

// Run executes the Work tool loop. Work always starts with an empty message
// history so Emotion history cannot leak into the delegated task.
func (r *Runtime) Run(ctx context.Context, brief protocol.TaskBrief, journal *Journal) protocol.TaskReport {
	system := BuildWorkSystem(brief)
	tools := r.cfg.Registry.ForScope(tool.ScopeWork)
	permission := tool.Permission(brief.PermissionScope)
	messages := []llm.Message{}

	for round := 0; round < r.cfg.MaxToolRounds; round++ {
		if err := ctx.Err(); err != nil {
			journal.Write("task_error", round, map[string]any{"error": err.Error(), "last_round": round})
			return failedReport(brief, "context canceled: "+err.Error())
		}
		if r.cfg.MaxInputTokens > 0 && estimateMessagesTokens(messages)+contextutil.EstimateTokens(system) > r.cfg.MaxInputTokens {
			return partialReport(brief, fmt.Sprintf("max input tokens exceeded (%d)", r.cfg.MaxInputTokens))
		}

		resp, err := r.cfg.LLM.ChatStream(ctx, llm.ChatRequest{
			Model:       r.cfg.Model,
			Messages:    messages,
			System:      system,
			MaxTokens:   r.cfg.MaxTokens,
			Temperature: r.cfg.Temperature,
			Stream:      false,
			Tools:       tools,
		}, func(llm.StreamEvent) {})
		if err != nil {
			journal.Write("task_error", round, map[string]any{"error": err.Error(), "last_round": round})
			return failedReport(brief, "llm request failed: "+err.Error())
		}
		if resp.StopReason != "tool_use" {
			return ParseOrFallback(resp.Content, brief)
		}

		messages = append(messages, llm.Message{
			Role:             llm.RoleAssistant,
			Content:          resp.Content,
			ContentBlocks:    resp.ContentBlocks,
			ReasoningContent: resp.ReasoningContent,
		})

		calls := tool.ExtractToolCalls(resp)
		for _, call := range calls {
			journal.Write("tool_call", round, map[string]any{
				"call_id": call.ID,
				"name":    call.Name,
				"input":   string(call.Input),
			})
		}

		results := r.cfg.Dispatcher.ExecuteAll(ctx, calls, permission)
		for _, result := range results {
			preview, truncated := truncateContent(string(result.Content), 500)
			journal.Write("tool_result", round, map[string]any{
				"call_id":   result.CallID,
				"preview":   preview,
				"truncated": truncated,
				"is_error":  result.IsError,
			})
		}

		messages = append(messages, tool.ResultsToMessages(r.cfg.Provider, results)...)
	}

	return partialReport(brief, fmt.Sprintf("max tool rounds exhausted (%d)", r.cfg.MaxToolRounds))
}

func failedReport(brief protocol.TaskBrief, reason string) protocol.TaskReport {
	return protocol.TaskReport{
		TaskID:    brief.TaskID,
		Status:    "failed",
		Goal:      brief.Goal,
		Summary:   reason,
		CreatedAt: brief.CreatedAt,
	}
}

func partialReport(brief protocol.TaskBrief, reason string) protocol.TaskReport {
	return protocol.TaskReport{
		TaskID:    brief.TaskID,
		Status:    "partial",
		Goal:      brief.Goal,
		Summary:   reason,
		CreatedAt: brief.CreatedAt,
	}
}

func truncateContent(text string, maxRunes int) (string, bool) {
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text, false
	}
	return string(runes[:maxRunes]) + "...", true
}

func estimateMessagesTokens(messages []llm.Message) int {
	total := 0
	for _, message := range messages {
		total += contextutil.EstimateTokens(message.Content)
		total += contextutil.EstimateTokens(message.ReasoningContent)
		for _, block := range message.ContentBlocks {
			total += contextutil.EstimateTokens(block.Text)
			total += contextutil.EstimateTokens(string(block.Input))
			total += contextutil.EstimateTokens(block.Content)
		}
	}
	return total
}
