package conversation

import (
	"context"
	"testing"
)

func TestRunRegistryStopsMatchingRuns(t *testing.T) {
	registry := NewRunRegistry()
	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel1()
	defer cancel2()

	unregister1 := registry.Register(RunRef{OriginKey: "webui:local:main", SessionID: "session-1", Kind: "chat"}, cancel1)
	defer unregister1()
	unregister2 := registry.Register(RunRef{OriginKey: "webui:local:other", SessionID: "session-2", Kind: "chat"}, cancel2)
	defer unregister2()

	stopped := registry.Stop(StopSelector{OriginKey: "webui:local:main"})
	if stopped != 1 {
		t.Fatalf("Stop count = %d, want 1", stopped)
	}
	if ctx1.Err() == nil {
		t.Fatal("first run was not cancelled")
	}
	if ctx2.Err() != nil {
		t.Fatal("second run should not be cancelled")
	}
}
