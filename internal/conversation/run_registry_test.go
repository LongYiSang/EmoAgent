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

func TestRunRegistryTryRegisterRejectsDuplicateRun(t *testing.T) {
	registry := NewRunRegistry()
	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel1()
	defer cancel2()
	ref := RunRef{OriginKey: "onebot:main:private:10001", SessionID: "session-1", Kind: "platform_text"}

	unregister, ok := registry.TryRegister(ref, cancel1)
	defer unregister()
	if !ok {
		t.Fatal("first TryRegister rejected, want accepted")
	}
	duplicateUnregister, ok := registry.TryRegister(ref, cancel2)
	defer duplicateUnregister()
	if ok {
		t.Fatal("duplicate TryRegister accepted, want rejected")
	}

	stopped := registry.Stop(StopSelector{OriginKey: ref.OriginKey, SessionID: ref.SessionID})
	if stopped != 1 {
		t.Fatalf("Stop count = %d, want 1", stopped)
	}
	if ctx1.Err() == nil {
		t.Fatal("registered run was not cancelled")
	}
	if ctx2.Err() != nil {
		t.Fatal("duplicate run should not be registered or cancelled")
	}
}
