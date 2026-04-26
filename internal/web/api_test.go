package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/longyisang/emoagent/internal/apperrors"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/storage"
)

type fakeAdminApp struct {
	providers           []config.LLMProvider
	agentConfigs        []config.AgentConfig
	activeAgent         *config.AgentConfig
	personas            map[string]*config.Persona
	progressPhrases     map[string]map[string][]string
	sessions            []storage.SessionSummary
	sessionDetail       *storage.SessionRecord
	sessionMessages     []storage.MessageRecord
	createErr           error
	activateErr         error
	sessionErr          error
	deleteSessionErr    error
	approvals           []protocol.ApprovalRequest
	lastProvider        config.LLMProvider
	lastAgentConfig     config.AgentConfig
	lastActivate        string
	lastPersonaKey      string
	lastPersona         *config.Persona
	lastSessionPersona  string
	lastSessionLimit    int
	lastDeleteSessionID string
	lastPhrasesKey      string
	lastPhrasesValue    map[string][]string
	lastApprovalSession string
	chatSettings        config.ChatConfig
	lastChatSettings    config.ChatConfig
	updateChatErr       error
}

func (f *fakeAdminApp) ListLLMProviders() ([]config.LLMProvider, error) {
	return append([]config.LLMProvider(nil), f.providers...), nil
}
func (f *fakeAdminApp) GetLLMProvider(id string) (*config.LLMProvider, error) {
	for i := range f.providers {
		if f.providers[i].ID == id {
			cp := f.providers[i]
			return &cp, nil
		}
	}
	return nil, apperrors.ErrLLMProviderNotFound
}
func (f *fakeAdminApp) CreateLLMProvider(provider config.LLMProvider) error {
	f.lastProvider = provider
	return f.createErr
}
func (f *fakeAdminApp) UpdateLLMProvider(id string, provider config.LLMProvider) error {
	provider.ID = id
	f.lastProvider = provider
	return nil
}
func (f *fakeAdminApp) DeleteLLMProvider(id string) error { return nil }
func (f *fakeAdminApp) RefreshLLMProviderModels(id string) ([]llm.ModelInfo, error) {
	return []llm.ModelInfo{{ID: "model-a"}}, nil
}
func (f *fakeAdminApp) GetLLMProviderModels(id string) ([]llm.ModelInfo, error) {
	return []llm.ModelInfo{{ID: "model-a"}}, nil
}
func (f *fakeAdminApp) ListAgentConfigs() ([]config.AgentConfig, error) {
	return append([]config.AgentConfig(nil), f.agentConfigs...), nil
}
func (f *fakeAdminApp) GetAgentConfig(id string) (*config.AgentConfig, error) {
	for i := range f.agentConfigs {
		if f.agentConfigs[i].ID == id {
			cp := f.agentConfigs[i]
			return &cp, nil
		}
	}
	return nil, apperrors.ErrAgentConfigNotFound
}
func (f *fakeAdminApp) GetActiveAgentConfig() (*config.AgentConfig, bool, error) {
	if f.activeAgent == nil {
		return nil, false, nil
	}
	cp := *f.activeAgent
	return &cp, true, nil
}
func (f *fakeAdminApp) CreateAgentConfig(agent config.AgentConfig) error {
	f.lastAgentConfig = agent
	return f.createErr
}
func (f *fakeAdminApp) UpdateAgentConfig(id string, agent config.AgentConfig) error {
	agent.ID = id
	f.lastAgentConfig = agent
	return nil
}
func (f *fakeAdminApp) ActivateAgentConfig(id string) error {
	f.lastActivate = id
	return f.activateErr
}
func (f *fakeAdminApp) DeleteAgentConfig(id string) error        { return nil }
func (f *fakeAdminApp) ListPersonas() map[string]*config.Persona { return f.personas }
func (f *fakeAdminApp) GetPersona(name string) (*config.Persona, bool) {
	p, ok := f.personas[name]
	return p, ok
}
func (f *fakeAdminApp) CreatePersona(key string, p *config.Persona) error {
	f.lastPersonaKey = key
	f.lastPersona = p
	return nil
}
func (f *fakeAdminApp) UpdatePersona(key string, p *config.Persona) error { return nil }
func (f *fakeAdminApp) DeletePersona(key string) error                    { return nil }
func (f *fakeAdminApp) GetProgressPhrases(key string) (map[string][]string, error) {
	if f.progressPhrases == nil {
		return map[string][]string{}, nil
	}
	phrases, ok := f.progressPhrases[key]
	if !ok {
		return nil, apperrors.ErrPersonaNotFound
	}
	out := make(map[string][]string, len(phrases))
	for k, v := range phrases {
		out[k] = append([]string(nil), v...)
	}
	return out, nil
}
func (f *fakeAdminApp) UpdateProgressPhrases(key string, phrases map[string][]string) error {
	f.lastPhrasesKey = key
	f.lastPhrasesValue = make(map[string][]string, len(phrases))
	for k, v := range phrases {
		f.lastPhrasesValue[k] = append([]string(nil), v...)
	}
	if f.progressPhrases == nil {
		f.progressPhrases = map[string]map[string][]string{}
	}
	f.progressPhrases[key] = f.lastPhrasesValue
	return nil
}
func (f *fakeAdminApp) ListSessions(_ context.Context, persona string, limit int) ([]storage.SessionSummary, error) {
	f.lastSessionPersona = persona
	f.lastSessionLimit = limit
	if f.sessionErr != nil {
		return nil, f.sessionErr
	}
	return append([]storage.SessionSummary(nil), f.sessions...), nil
}
func (f *fakeAdminApp) GetLatestSession(_ context.Context, persona string) (*storage.SessionSummary, error) {
	f.lastSessionPersona = persona
	if f.sessionErr != nil {
		return nil, f.sessionErr
	}
	if len(f.sessions) == 0 {
		return nil, nil
	}
	session := f.sessions[0]
	return &session, nil
}
func (f *fakeAdminApp) GetSessionDetail(_ context.Context, id string) (*storage.SessionRecord, []storage.MessageRecord, error) {
	if f.sessionErr != nil {
		return nil, nil, f.sessionErr
	}
	return f.sessionDetail, append([]storage.MessageRecord(nil), f.sessionMessages...), nil
}
func (f *fakeAdminApp) DeleteSession(_ context.Context, id string) error {
	f.lastDeleteSessionID = id
	return f.deleteSessionErr
}
func (f *fakeAdminApp) ListSessionApprovals(_ context.Context, sessionID string) ([]protocol.ApprovalRequest, error) {
	f.lastApprovalSession = sessionID
	return append([]protocol.ApprovalRequest(nil), f.approvals...), nil
}
func (f *fakeAdminApp) GetChatSettings() config.ChatConfig {
	return f.chatSettings
}
func (f *fakeAdminApp) UpdateChatSettings(settings config.ChatConfig) error {
	f.lastChatSettings = settings
	return f.updateChatErr
}

func TestHandleCreateLLMProviderNormalizesPayload(t *testing.T) {
	app := &fakeAdminApp{}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	body := bytes.NewBufferString(`{"id":" moonshot ","name":" Moonshot ","preset_id":" moonshot ","protocol":"openai_compatible","base_url":"https://api.moonshot.cn/","api_key_env":" MOONSHOT_API_KEY ","enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/llm-providers", body)
	rec := httptest.NewRecorder()
	handler.HandleCreateLLMProvider(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if app.lastProvider.ID != "moonshot" || app.lastProvider.BaseURL != "https://api.moonshot.cn" {
		t.Fatalf("lastProvider = %#v, want normalized id/base_url", app.lastProvider)
	}
	if app.lastProvider.ModelDiscovery != "manual" {
		t.Fatalf("ModelDiscovery = %q, want manual", app.lastProvider.ModelDiscovery)
	}
	if app.lastProvider.PresetID != "moonshot" {
		t.Fatalf("PresetID = %q, want moonshot", app.lastProvider.PresetID)
	}
}

func TestHandleListLLMProviderPresets(t *testing.T) {
	handler := NewAPIHandler(&fakeAdminApp{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/llm-provider-presets", nil)
	rec := httptest.NewRecorder()
	handler.HandleListLLMProviderPresets(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Presets []llm.ProviderPreset `json:"presets"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	var foundMoonshot bool
	for _, preset := range body.Presets {
		if preset.ID == "moonshot" {
			foundMoonshot = true
			if preset.Admin.MainDefaults.MaxTokens == 0 {
				t.Fatalf("moonshot main defaults missing: %#v", preset.Admin)
			}
		}
	}
	if !foundMoonshot {
		t.Fatalf("moonshot preset missing from response: %#v", body.Presets)
	}
}

func TestHandleCreateAgentConfigParsesBindings(t *testing.T) {
	app := &fakeAdminApp{}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	body := bytes.NewBufferString(`{
		"id":"default",
		"name":"Default",
		"persona_key":"default",
		"emotion":{
			"main":{"provider_id":"moonshot","model":"emotion-main","params":{"max_tokens":111,"temperature":0.2,"stream":true}},
			"summary":{"provider_id":"moonshot","model":"emotion-summary","params":{"max_tokens":222,"temperature":0.1,"stream":false}}
		},
		"work":{
			"main":{"provider_id":"anthropic","model":"work-main","params":{"max_tokens":333,"thinking":{"mode":"enabled","budget_tokens":1024}}},
			"summary":{"provider_id":"moonshot","model":"work-summary","params":{"max_tokens":444}}
		},
		"context_overrides":{"input_budget_tokens":9000}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/agent-configs", body)
	rec := httptest.NewRecorder()
	handler.HandleCreateAgentConfig(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if app.lastAgentConfig.Emotion.Main.Model != "emotion-main" || app.lastAgentConfig.Work.Summary.Model != "work-summary" {
		t.Fatalf("lastAgentConfig = %#v, want parsed models", app.lastAgentConfig)
	}
	if app.lastAgentConfig.Emotion.Main.Params.Temperature == nil || *app.lastAgentConfig.Emotion.Main.Params.Temperature != 0.2 {
		t.Fatalf("emotion main temperature = %#v, want 0.2", app.lastAgentConfig.Emotion.Main.Params.Temperature)
	}
	if app.lastAgentConfig.Work.Main.Params.Thinking == nil || app.lastAgentConfig.Work.Main.Params.Thinking.BudgetTokens == nil || *app.lastAgentConfig.Work.Main.Params.Thinking.BudgetTokens != 1024 {
		t.Fatalf("work thinking = %#v, want budget 1024", app.lastAgentConfig.Work.Main.Params.Thinking)
	}
}

func TestHandleChatSettingsRoundTrip(t *testing.T) {
	app := &fakeAdminApp{chatSettings: config.ChatConfig{RealtimeStreaming: true}}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	getReq := httptest.NewRequest(http.MethodGet, "/api/settings/chat", nil)
	getRec := httptest.NewRecorder()
	handler.HandleGetChatSettings(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", getRec.Code)
	}
	var getResp struct {
		RealtimeStreaming bool `json:"realtime_streaming"`
	}
	if err := json.NewDecoder(getRec.Body).Decode(&getResp); err != nil {
		t.Fatalf("Decode GET: %v", err)
	}
	if !getResp.RealtimeStreaming {
		t.Fatal("GET realtime_streaming = false, want true")
	}

	putReq := httptest.NewRequest(http.MethodPut, "/api/settings/chat", bytes.NewBufferString(`{"realtime_streaming":false}`))
	putRec := httptest.NewRecorder()
	handler.HandleUpdateChatSettings(putRec, putReq)

	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", putRec.Code)
	}
	if app.lastChatSettings.RealtimeStreaming {
		t.Fatal("UpdateChatSettings received realtime_streaming = true, want false")
	}
}

func TestHandleUpdateChatSettingsRejectsInvalidJSON(t *testing.T) {
	app := &fakeAdminApp{}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodPut, "/api/settings/chat", bytes.NewBufferString(`{"realtime_streaming":"yes"}`))
	rec := httptest.NewRecorder()
	handler.HandleUpdateChatSettings(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleCreatePersonaFallsBackToNameAsKey(t *testing.T) {
	app := &fakeAdminApp{}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	body := bytes.NewBufferString(`{"name":"default","description":"desc","tone":"warm"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/personas", body)
	rec := httptest.NewRecorder()
	handler.HandleCreatePersona(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if app.lastPersonaKey != "default" {
		t.Fatalf("lastPersonaKey = %q, want default", app.lastPersonaKey)
	}
	if app.lastPersona == nil || app.lastPersona.Name != "default" {
		t.Fatalf("lastPersona = %#v, want name default", app.lastPersona)
	}
}

func TestHandleListSessions(t *testing.T) {
	app := &fakeAdminApp{
		sessions: []storage.SessionSummary{
			{ID: "session-1", Persona: "default", MessageCount: 2, LastMessage: "hello", UpdatedAt: "2026-03-31T12:00:00Z"},
		},
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/sessions?persona=default&limit=5", nil)
	rec := httptest.NewRecorder()
	handler.HandleListSessions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if app.lastSessionPersona != "default" {
		t.Fatalf("lastSessionPersona = %q, want default", app.lastSessionPersona)
	}
	if app.lastSessionLimit != 5 {
		t.Fatalf("lastSessionLimit = %d, want 5", app.lastSessionLimit)
	}

	var payload struct {
		Sessions []storage.SessionSummary `json:"sessions"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(payload.Sessions) != 1 || payload.Sessions[0].ID != "session-1" {
		t.Fatalf("payload.Sessions = %#v, want session-1", payload.Sessions)
	}
}

func TestHandleDeleteSessionMapsNotFound(t *testing.T) {
	app := &fakeAdminApp{deleteSessionErr: apperrors.ErrSessionNotFound}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/missing", nil)
	req.SetPathValue("id", "missing")
	rec := httptest.NewRecorder()
	handler.HandleDeleteSession(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if app.lastDeleteSessionID != "missing" {
		t.Fatalf("lastDeleteSessionID = %q, want missing", app.lastDeleteSessionID)
	}
}

func TestHandleGetPersonaIncludesWorkProgressPhrases(t *testing.T) {
	app := &fakeAdminApp{
		personas: map[string]*config.Persona{
			"default": {
				Name: "default",
				WorkProgressPhrases: map[string][]string{
					"read_file": {"看看文件"},
				},
			},
		},
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/personas/default", nil)
	req.SetPathValue("name", "default")
	rec := httptest.NewRecorder()
	handler.HandleGetPersona(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	raw, ok := resp["work_progress_phrases"].(map[string]any)
	if !ok {
		t.Fatalf("work_progress_phrases missing: %#v", resp)
	}
	readFile, ok := raw["read_file"].([]any)
	if !ok || len(readFile) != 1 || readFile[0] != "看看文件" {
		t.Fatalf("work_progress_phrases.read_file = %#v, want [看看文件]", raw["read_file"])
	}
}

func TestHandleGetProgressPhrases(t *testing.T) {
	app := &fakeAdminApp{
		progressPhrases: map[string]map[string][]string{
			"default": {
				"read_file": {"看看文件"},
				"_default":  {"处理中"},
			},
		},
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/personas/default/progress-phrases", nil)
	req.SetPathValue("name", "default")
	rec := httptest.NewRecorder()
	handler.HandleGetProgressPhrases(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp map[string]map[string][]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got := resp["phrases"]["read_file"]; len(got) != 1 || got[0] != "看看文件" {
		t.Fatalf("phrases.read_file = %#v, want [看看文件]", got)
	}
}

func TestHandleUpdateProgressPhrases(t *testing.T) {
	app := &fakeAdminApp{}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	body := bytes.NewBufferString(`{"phrases":{"read_file":["看看文件"],"_default":["处理中"]}}`)
	req := httptest.NewRequest(http.MethodPut, "/api/personas/default/progress-phrases", body)
	req.SetPathValue("name", "default")
	rec := httptest.NewRecorder()
	handler.HandleUpdateProgressPhrases(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if app.lastPhrasesKey != "default" {
		t.Fatalf("lastPhrasesKey = %q, want default", app.lastPhrasesKey)
	}
	if got := app.lastPhrasesValue["read_file"]; len(got) != 1 || got[0] != "看看文件" {
		t.Fatalf("lastPhrasesValue.read_file = %#v, want [看看文件]", got)
	}
}

func TestHandleGetDefaultProgressPhrases(t *testing.T) {
	app := &fakeAdminApp{}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/progress-phrases/defaults", nil)
	rec := httptest.NewRecorder()
	handler.HandleGetProgressPhrasesDefaults(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp map[string]map[string][]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(resp["phrases"]) == 0 {
		t.Fatalf("phrases should not be empty: %#v", resp)
	}
}

func TestHandleListSessionApprovals(t *testing.T) {
	app := &fakeAdminApp{
		approvals: []protocol.ApprovalRequest{
			{
				ID:             "approval-1",
				SessionID:      "session-1",
				TaskID:         "task-1",
				Status:         string(protocol.ApprovalStatusPending),
				RejectOptionID: "cancel",
				Options:        []protocol.DecisionOption{{ID: "delete", Summary: "Delete it"}, {ID: "cancel", Summary: "Cancel"}},
				GoalSummary:    "Delete generated files",
				Question:       "Proceed?",
				ExpiresAt:      "2026-04-21T10:00:00Z",
				CreatedAt:      "2026-04-21T09:00:00Z",
				UpdatedAt:      "2026-04-21T09:00:00Z",
			},
		},
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/session-1/approvals", nil)
	req.SetPathValue("id", "session-1")
	rec := httptest.NewRecorder()
	handler.HandleListSessionApprovals(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if app.lastApprovalSession != "session-1" {
		t.Fatalf("lastApprovalSession = %q, want session-1", app.lastApprovalSession)
	}

	var payload struct {
		Approvals []protocol.ApprovalRequest `json:"approvals"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(payload.Approvals) != 1 || payload.Approvals[0].ID != "approval-1" {
		t.Fatalf("payload.Approvals = %#v, want approval-1", payload.Approvals)
	}
}
