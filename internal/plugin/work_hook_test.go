package plugin

import (
	"context"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/turn"
)

func TestWorkAnnotatorAppliesOnlyAppendHints(t *testing.T) {
	journal := turn.NewMemoryJournal()
	if err := journal.StartTurn(context.Background(), turn.TurnRecord{TurnID: "turn-work", Kind: turn.InboundUserMessage}); err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	bus := NewHookBus(HookBusConfig{DefaultTimeout: 50 * time.Millisecond, MaxTimeout: time.Second}, NewTurnJournalAudit(journal))
	err := bus.Register(RegisteredHook{
		PluginID:      "com.example.work",
		Hook:          HookWorkDispatchAnnotate,
		Mode:          HookModeTransform,
		FailurePolicy: FailurePolicyFailOpen,
		Handler: func(context.Context, HookContext) (HookResult, error) {
			return HookResult{Patches: []Patch{
				{Type: PatchWorkAddConstraintHint, Operation: PatchOpAppend, Value: "plugin constraint", ReasonCode: "test"},
				{Type: PatchWorkAddAcceptanceHint, Operation: PatchOpAppend, Value: "plugin acceptance", ReasonCode: "test"},
			}}, nil
		},
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	annotator := NewWorkAnnotator(&PluginHost{enabled: true, bus: bus})
	brief := &protocol.TaskBrief{
		TaskID:             "task-1",
		Goal:               "inspect",
		Constraints:        []string{"user constraint"},
		AcceptanceCriteria: []string{"user acceptance"},
		PermissionScope:    "read-only",
	}
	ctx := turn.WithCorrelationContext(context.Background(), turn.CorrelationContext{
		TurnID:     "turn-work",
		SessionID:  "session-1",
		PersonaKey: "default",
		RequestID:  "request-1",
		Kind:       turn.InboundUserMessage,
		Stage:      turn.StageEmotionLoop,
	})
	if err := annotator.AnnotateTaskBrief(ctx, brief); err != nil {
		t.Fatalf("AnnotateTaskBrief: %v", err)
	}
	if got := brief.Constraints; len(got) != 2 || got[1] != "plugin constraint" {
		t.Fatalf("constraints = %#v", got)
	}
	if got := brief.AcceptanceCriteria; len(got) != 2 || got[1] != "plugin acceptance" {
		t.Fatalf("acceptance = %#v", got)
	}
	if brief.TaskID != "task-1" || brief.Goal != "inspect" || brief.PermissionScope != "read-only" {
		t.Fatalf("immutable fields changed: %#v", brief)
	}
	snapshot, ok := journal.GetTurn("turn-work")
	if !ok {
		t.Fatal("journal missing turn-work")
	}
	if !hasPluginInvocation(snapshot.Events, "com.example.work", HookWorkDispatchAnnotate) {
		t.Fatalf("events = %#v, want work.dispatch.annotate plugin_invocation", snapshot.Events)
	}
}
