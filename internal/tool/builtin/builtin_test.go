package builtin

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/tool"
)

func TestGetCurrentTimeHandler(t *testing.T) {
	before := time.Now()
	result, err := GetCurrentTimeHandler(context.Background(), nil)
	after := time.Now()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp currentTimeResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Timezone == "" {
		t.Error("timezone should not be empty")
	}

	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", resp.CurrentTime, time.Now().Location())
	if err != nil {
		t.Fatalf("parse time %q: %v", resp.CurrentTime, err)
	}

	// The parsed time should be between before and after (within the same second tolerance).
	if parsed.Before(before.Truncate(time.Second)) || parsed.After(after.Add(time.Second)) {
		t.Errorf("time %v not between %v and %v", parsed, before, after)
	}
}

func TestGetCurrentTimeSpec(t *testing.T) {
	spec := GetCurrentTimeSpec

	if spec.Name != "get_current_time" {
		t.Errorf("Name = %q", spec.Name)
	}
	if spec.Scope != tool.ScopeBoth {
		t.Errorf("Scope = %q, want %q", spec.Scope, tool.ScopeBoth)
	}
	if spec.Permission != tool.PermReadOnly {
		t.Errorf("Permission = %q, want %q", spec.Permission, tool.PermReadOnly)
	}
	if spec.Parameters == nil {
		t.Fatal("Parameters should not be nil")
	}
	want := `{"type":"object","properties":{},"additionalProperties":false}`
	if string(spec.Parameters) != want {
		t.Errorf("Parameters = %s, want %s", spec.Parameters, want)
	}
}

func TestRegisterAll(t *testing.T) {
	registry := tool.NewRegistry()
	RegisterAll(registry, config.DefaultConfig(), slog.Default())

	specs := registry.Specs()
	if len(specs) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(specs))
	}
	if specs[0].Name != "get_current_time" {
		t.Errorf("tool name = %q", specs[0].Name)
	}

	handler, ok := registry.Get("get_current_time")
	if !ok || handler == nil {
		t.Fatal("handler not found for get_current_time")
	}
}

func TestRegisterAllPanicsOnDuplicate(t *testing.T) {
	registry := tool.NewRegistry()
	RegisterAll(registry, config.DefaultConfig(), slog.Default())

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate RegisterAll")
		}
	}()
	RegisterAll(registry, config.DefaultConfig(), slog.Default())
}

func TestRegisterAll_WebSearchDisabled(t *testing.T) {
	cfg := config.DefaultConfig() // WebSearch.Enabled = false
	registry := tool.NewRegistry()
	RegisterAll(registry, cfg, slog.Default())

	specs := registry.Specs()
	if len(specs) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(specs))
	}
	if specs[0].Name != "get_current_time" {
		t.Errorf("tool name = %q, want get_current_time", specs[0].Name)
	}
}

func TestRegisterAll_WebSearchEnabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WebSearch.Enabled = true
	cfg.WebSearch.Provider = "tavily"
	cfg.WebSearch.APIKeyEnv = "TEST_TAVILY_KEY"
	t.Setenv("TEST_TAVILY_KEY", "fake-key")

	registry := tool.NewRegistry()
	RegisterAll(registry, cfg, slog.Default())

	specs := registry.Specs()
	if len(specs) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(specs))
	}

	var found bool
	for _, s := range specs {
		if s.Name == "web_search" {
			found = true
		}
	}
	if !found {
		t.Error("web_search not found in registered specs")
	}
}

func TestRegisterAll_WebSearchProviderFails(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WebSearch.Enabled = true
	cfg.WebSearch.APIKeyEnv = "NONEXISTENT_KEY_XYZ"
	// ensure env var is not set (it shouldn't be, but unset explicitly to be safe)
	t.Setenv("NONEXISTENT_KEY_XYZ", "")

	registry := tool.NewRegistry()
	RegisterAll(registry, cfg, slog.Default()) // must not panic

	specs := registry.Specs()
	if len(specs) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(specs))
	}
	if specs[0].Name != "get_current_time" {
		t.Errorf("tool name = %q, want get_current_time", specs[0].Name)
	}
}
