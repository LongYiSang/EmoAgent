package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/longyisang/emoagent/internal/agentaffect"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/storage"
)

const agentAffectAppraisalJSON = `{
	"schema_version": "agent_affect.v3.appraisal.v1",
	"appraisal": {
		"event_significance": 0.5,
		"novelty": 0.1,
		"goal_relevance": 0.2,
		"relationship_impact": 0.1,
		"boundary_impact": 0,
		"uncertainty": 0.1
	},
	"delta": {"valence": 0.05},
	"label": "steady",
	"cause": {
		"code": "test",
		"summary": "test cause",
		"visible_summary": "test cause",
		"tags": ["test"]
	},
	"confidence": 0.8
}`

type agentAffectProviderRecorder struct {
	server *httptest.Server
	mu     sync.Mutex
	models []string
}

func newAgentAffectProviderRecorder(t *testing.T) *agentAffectProviderRecorder {
	t.Helper()
	rec := &agentAffectProviderRecorder{}
	rec.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		rec.mu.Lock()
		rec.models = append(rec.models, payload.Model)
		rec.mu.Unlock()
		content, _ := json.Marshal(agentAffectAppraisalJSON)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"chatcmpl-affect","model":%q,"choices":[{"message":{"content":%s},"finish_reason":"stop"}],"usage":{"prompt_tokens":11,"completion_tokens":7}}`, payload.Model, content)
	}))
	t.Cleanup(rec.server.Close)
	return rec
}

func (r *agentAffectProviderRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.models)
}

func (r *agentAffectProviderRecorder) firstModel() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.models) == 0 {
		return ""
	}
	return r.models[0]
}

func TestAgentAffectEvaluatorUsesExplicitProviderClient(t *testing.T) {
	deepseek := newAgentAffectProviderRecorder(t)
	moonshot := newAgentAffectProviderRecorder(t)
	cfg := agentAffectProviderTestConfig()
	cfg.AgentAffect.Evaluator.ProviderID = "moonshot"
	cfg.AgentAffect.Evaluator.Model = "kimi-k2.6"
	a, _ := newAgentAffectProviderTestApp(t, cfg, deepseek.server.URL, moonshot.server.URL)

	resp, err := a.kernel.Services.AgentAffect.SubmitMoodImpact(context.Background(), agentAffectProviderTestRequest())
	if err != nil {
		t.Fatalf("SubmitMoodImpact: %v", err)
	}
	if resp.Status != agentaffect.EvaluationStatusCommitted {
		t.Fatalf("status = %q, want committed", resp.Status)
	}
	if deepseek.count() != 0 {
		t.Fatalf("deepseek requests = %d, want 0", deepseek.count())
	}
	if moonshot.count() != 1 || moonshot.firstModel() != "kimi-k2.6" {
		t.Fatalf("moonshot requests/model = %d/%q, want 1/kimi-k2.6", moonshot.count(), moonshot.firstModel())
	}

	history, err := a.kernel.Services.AgentAffect.ListHistory(context.Background(), agentaffect.HistoryQuery{PersonaID: "default", SessionID: "session-1", Limit: 10})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(history.Evaluations) != 1 || history.Evaluations[0].LLMProvider != "moonshot" {
		t.Fatalf("evaluations = %#v, want one moonshot evaluation", history.Evaluations)
	}
}

func TestAgentAffectEvaluatorFallsBackToActiveEmotionSummaryWhenProviderUnset(t *testing.T) {
	deepseek := newAgentAffectProviderRecorder(t)
	moonshot := newAgentAffectProviderRecorder(t)
	cfg := agentAffectProviderTestConfig()
	cfg.AgentAffect.Evaluator.ProviderID = ""
	cfg.AgentAffect.Evaluator.Model = ""
	a, _ := newAgentAffectProviderTestApp(t, cfg, deepseek.server.URL, moonshot.server.URL)

	resp, err := a.kernel.Services.AgentAffect.SubmitMoodImpact(context.Background(), agentAffectProviderTestRequest())
	if err != nil {
		t.Fatalf("SubmitMoodImpact: %v", err)
	}
	if resp.Status != agentaffect.EvaluationStatusCommitted {
		t.Fatalf("status = %q, want committed", resp.Status)
	}
	if deepseek.count() != 1 || deepseek.firstModel() != "deepseek-v4-pro" {
		t.Fatalf("deepseek requests/model = %d/%q, want 1/deepseek-v4-pro", deepseek.count(), deepseek.firstModel())
	}
	if moonshot.count() != 0 {
		t.Fatalf("moonshot requests = %d, want 0", moonshot.count())
	}

	history, err := a.kernel.Services.AgentAffect.ListHistory(context.Background(), agentaffect.HistoryQuery{PersonaID: "default", SessionID: "session-1", Limit: 10})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(history.Evaluations) != 1 || history.Evaluations[0].LLMProvider != "deepseek" {
		t.Fatalf("evaluations = %#v, want one deepseek evaluation", history.Evaluations)
	}
}

func TestAgentAffectEvaluatorExplicitProviderRequiresModelUnlessActiveProviderMatches(t *testing.T) {
	deepseek := newAgentAffectProviderRecorder(t)
	moonshot := newAgentAffectProviderRecorder(t)
	cfg := agentAffectProviderTestConfig()
	cfg.AgentAffect.Evaluator.ProviderID = "moonshot"
	cfg.AgentAffect.Evaluator.Model = ""
	a, db := newAgentAffectProviderTestApp(t, cfg, deepseek.server.URL, moonshot.server.URL)

	_, err := a.kernel.Services.AgentAffect.SubmitMoodImpact(context.Background(), agentAffectProviderTestRequest())
	if err == nil || !strings.Contains(err.Error(), "agent_affect.evaluator.model is required") {
		t.Fatalf("SubmitMoodImpact error = %v, want model required", err)
	}
	if deepseek.count() != 0 || moonshot.count() != 0 {
		t.Fatalf("provider requests deepseek/moonshot = %d/%d, want 0/0", deepseek.count(), moonshot.count())
	}
	for _, table := range []string{"agent_affect_states", "agent_affect_events", "agent_affect_evaluations"} {
		if got := countAgentAffectRows(t, db, table); got != 0 {
			t.Fatalf("%s rows = %d, want 0", table, got)
		}
	}
}

func agentAffectProviderTestConfig() *config.Config {
	cfg := config.DefaultConfig()
	cfg.AgentAffect.Enabled = true
	cfg.AgentAffect.StorageEnabled = true
	cfg.AgentAffect.Context.StoreRawInputs = false
	cfg.AgentAffect.Context.StorePromptSnapshot = false
	return cfg
}

func newAgentAffectProviderTestApp(t *testing.T, cfg *config.Config, deepseekURL, moonshotURL string) (*App, *storage.DB) {
	t.Helper()
	t.Setenv("TEST_DEEPSEEK_KEY", "test-key")
	t.Setenv("TEST_MOONSHOT_KEY", "test-key")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, provider := range []config.LLMProvider{
		{ID: "deepseek", Name: "DeepSeek", Protocol: "openai_compatible", BaseURL: deepseekURL, APIKeyEnv: "TEST_DEEPSEEK_KEY", ModelDiscovery: "manual", Enabled: true},
		{ID: "moonshot", Name: "Moonshot", Protocol: "openai_compatible", BaseURL: moonshotURL, APIKeyEnv: "TEST_MOONSHOT_KEY", ModelDiscovery: "manual", Enabled: true},
	} {
		if err := db.UpsertLLMProvider(provider); err != nil {
			t.Fatalf("UpsertLLMProvider(%s): %v", provider.ID, err)
		}
	}
	a := newTestApp(cfg, db, logger)
	deepseekSummary, err := a.kernel.Services.AgentRuntime.modelRuntime(config.ModelBinding{ProviderID: "deepseek", Model: "deepseek-v4-pro"}, true)
	if err != nil {
		t.Fatalf("modelRuntime(deepseek): %v", err)
	}
	setTestActiveRuntime(a, &ActiveAgentRuntime{
		PersonaKey:     "default",
		EmotionSummary: deepseekSummary,
	})
	return a, db
}

func agentAffectProviderTestRequest() agentaffect.SubmitMoodImpactRequest {
	return agentaffect.SubmitMoodImpactRequest{
		PersonaID: "default",
		SessionID: "session-1",
		Trigger:   agentaffect.TriggerDescriptor{TriggerType: "user_message"},
		Input:     agentaffect.MoodImpactInput{Mode: "raw", Text: "hello"},
	}
}

func countAgentAffectRows(t *testing.T, db *storage.DB, table string) int {
	t.Helper()
	var count int
	if err := db.SqlDB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}
