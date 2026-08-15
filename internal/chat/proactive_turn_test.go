package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/turn"
)

func proactiveEnvelope(decisionID string) turn.InboundEnvelope {
	return turn.InboundEnvelope{
		EnvelopeID: "env-proactive-1",
		Source:     turn.SourceSystem,
		Kind:       turn.InboundProactivePrompt,
		SessionID:  "session-test",
		PersonaKey: "default",
		Proactive: &turn.ProactiveTrigger{
			DecisionID: decisionID,
			Hint:       "他在调 turn 管线的测试，卡了挺久",
			Activity:   "14:02–14:41 反复运行 go test，终端持续报错",
			Urgency:    0.7,
		},
	}
}

func TestProactiveStagesOmitApprovalWait(t *testing.T) {
	_, engine := newTestHandler()
	env := proactiveEnvelope("dec-1")

	staged := newChatTurnRuntime(engine, config.TurnPipelineConfig{Enabled: true, MemoryStages: true}, turn.NewMemoryJournal(), discardLogger())
	got := stageNames(staged.stages(env, &config.Persona{Name: "default"}))

	// No approval stage: there is no user waiting to answer an approval prompt
	// on a turn they did not start.
	if !sameStageNames(got, []turn.StageName{
		turn.StageNormalize,
		turn.StageMemoryPrepare,
		turn.StageEmotionPrepare,
		turn.StageEmotionLoop,
		turn.StageMemoryCommit,
	}) {
		t.Fatalf("proactive stages = %#v", got)
	}
}

func TestProactiveNormalizeRequiresDecisionID(t *testing.T) {
	_, engine := newTestHandler()
	runtime := newChatTurnRuntime(engine, config.TurnPipelineConfig{Enabled: true}, turn.NewMemoryJournal(), discardLogger())

	env := proactiveEnvelope("")
	result, err := runtime.Execute(context.Background(), env, &config.Persona{Name: "default"},
		turn.SinkFunc(func(context.Context, turn.OutboundEvent) error { return nil }))
	if err == nil {
		t.Fatal("Execute accepted a proactive turn without a decision id")
	}
	if result.ErrorKind != "validation_error" {
		t.Fatalf("error kind = %q, want validation_error", result.ErrorKind)
	}
}

// The whole point of the silent-termination switch: Emotion may decline, and
// when it does the user must see absolutely nothing.
func TestProactiveSilentTerminationEmitsNothing(t *testing.T) {
	handler, engine := newTestHandlerWithOptions(
		WithTurnPipelineConfig(config.TurnPipelineConfig{Enabled: true, RolloutPercent: 100}),
	)
	engine.sendReply = SilentMarker
	handler.turnRuntime.SetProactiveConfig(config.ProactiveConfig{
		Enabled:           true,
		SilentTermination: config.ProactiveSilentConfig{Enabled: true},
	})

	var events []turn.OutboundEvent
	result, err := handler.turnRuntime.Execute(context.Background(), proactiveEnvelope("dec-1"), &config.Persona{Name: "default"},
		turn.SinkFunc(func(_ context.Context, event turn.OutboundEvent) error {
			events = append(events, event)
			return nil
		}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != "done" {
		t.Fatalf("status = %q, want done", result.Status)
	}
	if len(events) != 0 {
		t.Fatalf("events = %#v, want none — a declined proactive turn must be invisible", events)
	}
}

func TestProactiveSpeaksWhenEmotionHasSomethingToSay(t *testing.T) {
	handler, engine := newTestHandlerWithOptions(
		WithTurnPipelineConfig(config.TurnPipelineConfig{Enabled: true, RolloutPercent: 100}),
	)
	engine.sendReply = "卡这么久啦，要不要歇会儿"
	handler.turnRuntime.SetProactiveConfig(config.ProactiveConfig{
		Enabled:           true,
		SilentTermination: config.ProactiveSilentConfig{Enabled: true},
	})

	var events []turn.OutboundEvent
	if _, err := handler.turnRuntime.Execute(context.Background(), proactiveEnvelope("dec-1"), &config.Persona{Name: "default"},
		turn.SinkFunc(func(_ context.Context, event turn.OutboundEvent) error {
			events = append(events, event)
			return nil
		})); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var delivered string
	sawStart := false
	for _, event := range events {
		if event.Type == turn.EventStreamStart {
			sawStart = true
		}
		delivered += event.Content
	}
	if !sawStart {
		t.Fatalf("no stream start emitted; events = %#v", events)
	}
	if !strings.Contains(delivered, "歇会儿") {
		t.Fatalf("delivered = %q, want the reply", delivered)
	}
}

// With the switch off, Emotion loses its veto and the marker is delivered as-is
// rather than silently swallowed.
func TestProactiveSilentTerminationDisabledDeliversMarker(t *testing.T) {
	handler, engine := newTestHandlerWithOptions(
		WithTurnPipelineConfig(config.TurnPipelineConfig{Enabled: true, RolloutPercent: 100}),
	)
	engine.sendReply = SilentMarker
	handler.turnRuntime.SetProactiveConfig(config.ProactiveConfig{
		Enabled:           true,
		SilentTermination: config.ProactiveSilentConfig{Enabled: false},
	})

	var events []turn.OutboundEvent
	if _, err := handler.turnRuntime.Execute(context.Background(), proactiveEnvelope("dec-1"), &config.Persona{Name: "default"},
		turn.SinkFunc(func(_ context.Context, event turn.OutboundEvent) error {
			events = append(events, event)
			return nil
		})); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("events = none, want the marker delivered when the veto is disabled")
	}
}

func TestBuildProactiveTriggerBlock(t *testing.T) {
	trigger := &turn.ProactiveTrigger{
		DecisionID: "dec-1",
		Activity:   "反复运行 go test",
		Hint:       "卡了挺久",
	}

	withVeto := buildProactiveTriggerBlock(trigger, true)
	if !strings.Contains(withVeto, "反复运行 go test") || !strings.Contains(withVeto, "卡了挺久") {
		t.Fatalf("block = %q, want activity and hint", withVeto)
	}
	if !strings.Contains(withVeto, SilentMarker) {
		t.Fatalf("block = %q, want the silence instruction", withVeto)
	}

	withoutVeto := buildProactiveTriggerBlock(trigger, false)
	if strings.Contains(withoutVeto, SilentMarker) {
		t.Fatalf("block = %q, must not offer silence when the veto is disabled", withoutVeto)
	}

	if got := buildProactiveTriggerBlock(nil, true); got != "" {
		t.Fatalf("nil trigger produced %q, want empty", got)
	}
}

func TestIsProactiveSilence(t *testing.T) {
	for _, tc := range []struct {
		reply  string
		silent bool
	}{
		{SilentMarker, true},
		{"  " + SilentMarker + "\n", true},
		{"", true},
		{"   ", true},
		{"卡这么久啦", false},
		// A reply that merely mentions the marker is still a real reply.
		{"我本来想说 " + SilentMarker + " 但还是说点什么吧", false},
	} {
		if got := isProactiveSilence(tc.reply); got != tc.silent {
			t.Fatalf("isProactiveSilence(%q) = %v, want %v", tc.reply, got, tc.silent)
		}
	}
}
