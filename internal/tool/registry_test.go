package tool

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/longyisang/emoagent/internal/llm"
)

func noopHandler(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
	return nil, nil
}

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()

	spec := Spec{
		Name:        "get_time",
		Description: "Get current time",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		Scope:       ScopeBoth,
		Permission:  PermReadOnly,
	}

	r.Register(spec, noopHandler)

	h, ok := r.Get("get_time")
	if !ok || h == nil {
		t.Fatal("expected handler for get_time")
	}

	s, ok := r.GetSpec("get_time")
	if !ok {
		t.Fatal("expected spec for get_time")
	}
	if s.Name != "get_time" {
		t.Errorf("spec Name: got %q", s.Name)
	}

	_, ok = r.Get("nonexistent")
	if ok {
		t.Error("expected not found for nonexistent tool")
	}
}

func TestRegistryDuplicatePanics(t *testing.T) {
	r := NewRegistry()

	spec := Spec{Name: "dup_tool", Parameters: json.RawMessage(`{}`), Scope: ScopeBoth, Permission: PermReadOnly}
	r.Register(spec, noopHandler)

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	r.Register(spec, noopHandler)
}

func TestRegistryTryRegisterRejectsDuplicateWithoutPanic(t *testing.T) {
	r := NewRegistry()
	first := Spec{Name: "dup_tool", Parameters: json.RawMessage(`{}`), Scope: ScopeBoth, Permission: PermReadOnly}
	second := Spec{Name: "dup_tool", Parameters: json.RawMessage(`{}`), Scope: ScopeWork, Permission: PermWorkspaceWrite}
	r.Register(first, noopHandler)

	err := r.TryRegister(second, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"second":true}`), nil
	})
	if err == nil {
		t.Fatal("TryRegister duplicate error = nil, want error")
	}
	got, ok := r.GetSpec("dup_tool")
	if !ok {
		t.Fatal("dup_tool missing after TryRegister duplicate")
	}
	if got.Permission != PermReadOnly {
		t.Fatalf("dup_tool permission = %q, want original %q", got.Permission, PermReadOnly)
	}
}

func TestRegistryTryRegisterValidatesNameAndHandler(t *testing.T) {
	r := NewRegistry()
	if err := r.TryRegister(Spec{Name: "", Scope: ScopeBoth, Permission: PermReadOnly}, noopHandler); err == nil {
		t.Fatal("TryRegister empty name error = nil, want error")
	}
	if err := r.TryRegister(Spec{Name: "missing_handler", Scope: ScopeBoth, Permission: PermReadOnly}, nil); err == nil {
		t.Fatal("TryRegister nil handler error = nil, want error")
	}
	if err := r.TryRegister(Spec{Name: "ok", Scope: ScopeBoth, Permission: PermReadOnly}, noopHandler); err != nil {
		t.Fatalf("TryRegister valid: %v", err)
	}
}

func TestRegistryForScope(t *testing.T) {
	r := NewRegistry()

	r.Register(Spec{Name: "emotion_only", Parameters: json.RawMessage(`{}`), Scope: ScopeEmotion, Permission: PermReadOnly}, noopHandler)
	r.Register(Spec{Name: "work_only", Parameters: json.RawMessage(`{}`), Scope: ScopeWork, Permission: PermWorkspaceWrite}, noopHandler)
	r.Register(Spec{Name: "shared", Parameters: json.RawMessage(`{}`), Scope: ScopeBoth, Permission: PermReadOnly}, noopHandler)

	// Emotion scope: emotion_only + shared.
	emotionTools := r.ForScope(ScopeEmotion)
	emotionNames := extractNames(emotionTools)
	assertContains(t, emotionNames, "emotion_only", "Emotion scope")
	assertContains(t, emotionNames, "shared", "Emotion scope")
	assertNotContains(t, emotionNames, "work_only", "Emotion scope")

	// Work scope: work_only + shared.
	workTools := r.ForScope(ScopeWork)
	workNames := extractNames(workTools)
	assertContains(t, workNames, "work_only", "Work scope")
	assertContains(t, workNames, "shared", "Work scope")
	assertNotContains(t, workNames, "emotion_only", "Work scope")
}

func TestRegistrySpecs(t *testing.T) {
	r := NewRegistry()
	r.Register(Spec{Name: "a", Parameters: json.RawMessage(`{}`), Scope: ScopeBoth, Permission: PermReadOnly}, noopHandler)
	r.Register(Spec{Name: "b", Parameters: json.RawMessage(`{}`), Scope: ScopeWork, Permission: PermWorkspaceWrite}, noopHandler)

	specs := r.Specs()
	if len(specs) != 2 {
		t.Errorf("Specs: got %d, want 2", len(specs))
	}
}

func extractNames(defs []llm.ToolDef) []string {
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Name
	}
	return names
}

func assertContains(t *testing.T, ss []string, s, ctx string) {
	t.Helper()
	for _, x := range ss {
		if x == s {
			return
		}
	}
	t.Errorf("%s: expected %q in %v", ctx, s, ss)
}

func assertNotContains(t *testing.T, ss []string, s, ctx string) {
	t.Helper()
	for _, x := range ss {
		if x == s {
			t.Errorf("%s: unexpected %q in %v", ctx, s, ss)
			return
		}
	}
}
