package builtin

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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
	if spec.Scope != tool.ScopeWork {
		t.Errorf("Scope = %q, want %q", spec.Scope, tool.ScopeWork)
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

func TestRegisterAllKeepsCurrentTimeWorkOnlyBecauseEmotionGetsTimeContext(t *testing.T) {
	registry := tool.NewRegistry()
	RegisterAll(registry, config.DefaultConfig(), t.TempDir(), slog.Default())

	for _, def := range registry.ForScope(tool.ScopeEmotion) {
		if def.Name == "get_current_time" {
			t.Fatal("get_current_time should not be exposed to Emotion scope")
		}
	}

	var foundInWork bool
	for _, def := range registry.ForScope(tool.ScopeWork) {
		if def.Name == "get_current_time" {
			foundInWork = true
			break
		}
	}
	if !foundInWork {
		t.Fatal("get_current_time should remain exposed to Work scope")
	}
}

func TestRegisterAll(t *testing.T) {
	registry := tool.NewRegistry()
	RegisterAll(registry, config.DefaultConfig(), t.TempDir(), slog.Default())

	// get_current_time, read_file, list_dir, write_file, edit_file, web_fetch (enabled by default)
	specs := registry.Specs()
	if len(specs) != 6 {
		t.Fatalf("expected 6 tools, got %d", len(specs))
	}
	for _, name := range []string{"get_current_time", "read_file", "list_dir", "write_file", "edit_file", "web_fetch"} {
		if _, ok := registry.Get(name); !ok {
			t.Fatalf("handler not found for %s", name)
		}
	}
}

func TestRegisterAllPanicsOnDuplicate(t *testing.T) {
	registry := tool.NewRegistry()
	RegisterAll(registry, config.DefaultConfig(), t.TempDir(), slog.Default())

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate RegisterAll")
		}
	}()
	RegisterAll(registry, config.DefaultConfig(), t.TempDir(), slog.Default())
}

func TestRegisterAll_WebSearchDisabled(t *testing.T) {
	cfg := config.DefaultConfig() // WebSearch.Enabled = false, WebFetch.Enabled = true
	registry := tool.NewRegistry()
	RegisterAll(registry, cfg, t.TempDir(), slog.Default())

	specs := registry.Specs()
	if len(specs) != 6 {
		t.Fatalf("expected 6 tools, got %d", len(specs))
	}
	if spec, ok := registry.GetSpec("read_file"); !ok {
		t.Fatal("read_file not found in registered specs")
	} else {
		if spec.Scope != tool.ScopeWork {
			t.Fatalf("read_file scope = %q, want %q", spec.Scope, tool.ScopeWork)
		}
		if spec.Permission != tool.PermReadOnly {
			t.Fatalf("read_file permission = %q, want %q", spec.Permission, tool.PermReadOnly)
		}
	}
}

func TestRegisterAll_WebSearchEnabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WebSearch.Enabled = true
	cfg.WebSearch.Provider = "tavily"
	cfg.WebSearch.APIKeyEnv = "TEST_TAVILY_KEY"
	t.Setenv("TEST_TAVILY_KEY", "fake-key")

	registry := tool.NewRegistry()
	RegisterAll(registry, cfg, t.TempDir(), slog.Default())

	specs := registry.Specs()
	if len(specs) != 7 {
		t.Fatalf("expected 7 tools, got %d", len(specs))
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
	RegisterAll(registry, cfg, t.TempDir(), slog.Default()) // must not panic

	specs := registry.Specs()
	// web_search fails; remaining: get_current_time, read_file, list_dir, write_file, edit_file, web_fetch
	if len(specs) != 6 {
		t.Fatalf("expected 6 tools, got %d", len(specs))
	}
}

func TestRegisterAll_WebFetchDefaultMissingKeyFallsBackToDirect(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WebFetch.APIKeyEnv = "MISSING_TAVILY_KEY"
	t.Setenv("MISSING_TAVILY_KEY", "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("direct fallback"))
	}))
	defer srv.Close()

	registry := tool.NewRegistry()
	RegisterAll(registry, cfg, t.TempDir(), slog.Default())

	handler, ok := registry.Get("web_fetch")
	if !ok {
		t.Fatal("web_fetch handler not found")
	}
	input, _ := json.Marshal(map[string]string{"url": srv.URL})
	raw, err := handler(context.Background(), input)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	var out struct {
		Provider string `json:"provider"`
		Text     string `json:"text"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Provider != "direct" {
		t.Fatalf("provider = %q, want direct", out.Provider)
	}
	if !strings.Contains(out.Text, "direct fallback") {
		t.Fatalf("text = %q, want direct fallback", out.Text)
	}
}

func TestRegisterAll_WebFetchTavilyWithKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WebFetch.APIKeyEnv = "TEST_TAVILY_KEY"
	t.Setenv("TEST_TAVILY_KEY", "fake-key")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/extract" {
			t.Fatalf("path = %q, want /extract", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"results": [{"url": "https://example.com", "raw_content": "tavily content"}],
			"failed_results": [],
			"request_id": "req-register"
		}`))
	}))
	defer srv.Close()
	cfg.WebFetch.BaseURL = srv.URL

	registry := tool.NewRegistry()
	RegisterAll(registry, cfg, t.TempDir(), slog.Default())

	handler, ok := registry.Get("web_fetch")
	if !ok {
		t.Fatal("web_fetch handler not found")
	}
	input, _ := json.Marshal(map[string]string{"url": "https://example.com"})
	raw, err := handler(context.Background(), input)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	var out struct {
		Provider  string `json:"provider"`
		Text      string `json:"text"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Provider != "tavily" {
		t.Fatalf("provider = %q, want tavily", out.Provider)
	}
	if out.Text != "tavily content" {
		t.Fatalf("text = %q, want tavily content", out.Text)
	}
	if out.RequestID != "req-register" {
		t.Fatalf("request_id = %q, want req-register", out.RequestID)
	}
}

func TestRegisterAll_WebFetchDirectDoesNotNeedKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WebFetch.Provider = "direct"
	cfg.WebFetch.APIKeyEnv = "MISSING_TAVILY_KEY"
	t.Setenv("MISSING_TAVILY_KEY", "")

	registry := tool.NewRegistry()
	RegisterAll(registry, cfg, t.TempDir(), slog.Default())

	if _, ok := registry.Get("web_fetch"); !ok {
		t.Fatal("web_fetch handler not found")
	}
}

func TestBuiltinToolDescriptionsDocumentP1SafetyAndSourceRules(t *testing.T) {
	root := t.TempDir()
	readSpec, _ := NewReadFileTool(root)
	listSpec, _ := NewListDirTool(root)
	writeSpec, _ := NewWriteFileTool(root)
	editSpec, _ := NewEditFileTool(root)
	webFetchSpec, _ := NewWebFetchTool(defaultWebFetchCfg(), nil)

	assertDescriptionContains(t, readSpec.Description,
		"workspace-relative path",
		"Absolute paths and path traversal are rejected",
		"valid UTF-8",
		"1 MiB",
	)
	assertDescriptionContains(t, listSpec.Description,
		"workspace-relative path",
		"truncated",
		"max_entries",
	)
	assertDescriptionContains(t, writeSpec.Description,
		"workspace-relative path",
		"overwrites",
		"1 MiB",
	)
	assertDescriptionContains(t, editSpec.Description,
		"workspace-relative path",
		"exactly once",
		"valid UTF-8",
	)
	assertDescriptionContains(t, webFetchSpec.Description,
		"final_url",
		"status",
		"truncated",
	)
	assertDescriptionContains(t, WebSearchSpec.Description,
		"source URLs",
		"Use web_fetch",
	)
	assertDescriptionContains(t, GetCurrentTimeSpec.Description,
		"current local time",
		"timezone",
	)
}

func assertDescriptionContains(t *testing.T, description string, snippets ...string) {
	t.Helper()
	for _, snippet := range snippets {
		if !strings.Contains(description, snippet) {
			t.Fatalf("description missing %q: %s", snippet, description)
		}
	}
}
