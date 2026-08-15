package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/turn"
)

// These tests run against the real Engine and a real SQLite database, because
// the guarantees they cover are about what lands in the messages table — and
// getting that wrong corrupts the persona on the following turn.

func runProactiveTurnAgainstRealEngine(t *testing.T, reply string, silentEnabled bool) (*Engine, string, []turn.OutboundEvent) {
	t.Helper()

	fakeLLM := &fakeLLMClient{response: &llm.ChatResponse{ID: "resp-1", Content: reply, Model: "test-model"}}
	engine, _, logger := newTestEngine(t, fakeLLM)

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	runtime := newChatTurnRuntime(engine, config.TurnPipelineConfig{
		Enabled: true, MemoryStages: true, RolloutPercent: 100,
	}, turn.NewMemoryJournal(), logger)
	runtime.SetProactiveConfig(config.ProactiveConfig{
		Enabled:           true,
		SilentTermination: config.ProactiveSilentConfig{Enabled: silentEnabled},
	})

	env := proactiveEnvelope("dec-1")
	env.SessionID = sessionID

	var events []turn.OutboundEvent
	if _, err := runtime.Execute(context.Background(), env, &config.Persona{Name: "default", SystemPrompt: "You are warm."},
		turn.SinkFunc(func(_ context.Context, event turn.OutboundEvent) error {
			events = append(events, event)
			return nil
		})); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return engine, sessionID, events
}

// The trigger text explains why the host woke Emotion up. If it were persisted
// as a user message, the next turn would read it back as the user's own words
// ("用户说：用户过去 40 分钟在改代码") and the persona would break.
func TestProactiveTurnNeverPersistsTriggerAsUserMessage(t *testing.T) {
	engine, sessionID, _ := runProactiveTurnAgainstRealEngine(t, "卡这么久啦，要不要歇会儿", true)

	messages, err := engine.db.GetAllMessages(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("GetAllMessages: %v", err)
	}
	for _, message := range messages {
		if message.Role == "user" {
			t.Fatalf("proactive turn wrote a user message: %q", message.Content)
		}
		if strings.Contains(message.Content, "主动开口") || strings.Contains(message.Content, "观察到的情况") {
			t.Fatalf("trigger block leaked into the transcript: %q", message.Content)
		}
	}
}

// The agent's own words must be recorded, or the next turn has a hole in its
// history and Emotion will not know it already spoke.
func TestProactiveTurnPersistsAssistantReply(t *testing.T) {
	engine, sessionID, _ := runProactiveTurnAgainstRealEngine(t, "卡这么久啦，要不要歇会儿", true)

	messages, err := engine.db.GetAllMessages(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("GetAllMessages: %v", err)
	}
	found := false
	for _, message := range messages {
		if message.Role == "assistant" && strings.Contains(message.Content, "歇会儿") {
			found = true
		}
	}
	if !found {
		t.Fatalf("assistant reply not persisted; messages = %#v", messages)
	}
}

// A declined proactive turn must leave no trace in the transcript at all.
func TestProactiveSilentTerminationWritesNoMessages(t *testing.T) {
	engine, sessionID, events := runProactiveTurnAgainstRealEngine(t, SilentMarker, true)

	messages, err := engine.db.GetAllMessages(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("GetAllMessages: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("messages = %#v, want none after a silenced proactive turn", messages)
	}
	// Diagnostics (context_stats and friends) may still be journalled — they cost
	// the user nothing. What must never escape is anything user-visible.
	for _, event := range events {
		if turn.IsUserVisibleEvent(event) {
			t.Fatalf("user-visible event escaped a silenced proactive turn: %#v", event)
		}
	}
}

// The trigger reaches the model as a system block, not as user content.
func TestProactiveTriggerReachesModelAsSystemBlock(t *testing.T) {
	fakeLLM := &fakeLLMClient{response: &llm.ChatResponse{ID: "resp-1", Content: "好呀", Model: "test-model"}}
	engine, _, logger := newTestEngine(t, fakeLLM)

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	runtime := newChatTurnRuntime(engine, config.TurnPipelineConfig{
		Enabled: true, MemoryStages: true, RolloutPercent: 100,
	}, turn.NewMemoryJournal(), logger)
	runtime.SetProactiveConfig(config.ProactiveConfig{
		Enabled:           true,
		SilentTermination: config.ProactiveSilentConfig{Enabled: true},
	})

	env := proactiveEnvelope("dec-1")
	env.SessionID = sessionID
	if _, err := runtime.Execute(context.Background(), env, &config.Persona{Name: "default"},
		turn.SinkFunc(func(context.Context, turn.OutboundEvent) error { return nil })); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(fakeLLM.lastRequest.System, "反复运行 go test") {
		t.Fatalf("System prompt missing the activity block:\n%s", fakeLLM.lastRequest.System)
	}
	for _, message := range fakeLLM.lastRequest.Messages {
		if message.Role == llm.RoleUser && strings.Contains(message.Content, "主动开口") {
			t.Fatalf("trigger block was sent as a user message: %q", message.Content)
		}
	}
}
