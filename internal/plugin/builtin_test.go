package plugin

import (
	"context"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/turn"
)

func TestBuiltinPluginManifestsValidate(t *testing.T) {
	for _, builtin := range DefaultBuiltinPlugins() {
		if err := builtin.Manifest().Validate(ManifestValidationOptions{MaxTimeoutMS: 1000}); err != nil {
			t.Fatalf("%s manifest Validate: %v", builtin.Manifest().ID, err)
		}
	}
}

func TestBuiltinRunnerLoadsOnlyEnabledPlugins(t *testing.T) {
	journal := turn.NewMemoryJournal()
	if err := journal.StartTurn(context.Background(), turn.TurnRecord{TurnID: "turn-1", Kind: turn.InboundUserMessage}); err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	host := &PluginHost{
		enabled: true,
		bus:     NewHookBus(HookBusConfig{DefaultTimeout: 50 * time.Millisecond, MaxTimeout: time.Second}, NewTurnJournalAudit(journal)),
	}
	runner := NewBuiltinRunner(host, tool.NewRegistry())
	if err := runner.Load(context.Background(), []BuiltinPlugin{NewTurnAuditPlugin(), NewOutboundGuardPlugin()}, []string{TurnAuditPluginID}); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if _, err := host.bus.Dispatch(context.Background(), HookAfterTurnEnd, HookContext{
		Envelope: HookEnvelope{TurnID: "turn-1", Hook: HookAfterTurnEnd},
		Turn:     TurnView{TurnID: "turn-1", SessionID: "session-1", PersonaKey: "default"},
	}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	snapshot, ok := journal.GetTurn("turn-1")
	if !ok {
		t.Fatal("journal missing turn")
	}
	var sawTurnAudit bool
	var sawOutboundGuard bool
	for _, event := range snapshot.Events {
		if event.Payload["plugin_id"] == TurnAuditPluginID {
			sawTurnAudit = true
		}
		if event.Payload["plugin_id"] == OutboundGuardPluginID {
			sawOutboundGuard = true
		}
	}
	if !sawTurnAudit {
		t.Fatalf("events = %#v, want turn-audit invocation", snapshot.Events)
	}
	if sawOutboundGuard {
		t.Fatalf("events = %#v, outbound-guard should not be loaded", snapshot.Events)
	}
}
