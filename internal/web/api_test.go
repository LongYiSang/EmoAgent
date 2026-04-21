package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/longyisang/emoagent/internal/apperrors"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/storage"
)

type fakeAdminApp struct {
	profiles            []config.LLMProfile
	active              *config.LLMProfile
	personas            map[string]*config.Persona
	progressPhrases     map[string]map[string][]string
	sessions            []storage.SessionSummary
	sessionDetail       *storage.SessionRecord
	sessionMessages     []storage.MessageRecord
	defaultKey          string
	createErr           error
	activateErr         error
	activatePersonaErr  error
	getErr              error
	sessionErr          error
	deleteSessionErr    error
	approvals           []protocol.ApprovalRequest
	lastCreate          config.LLMProfile
	lastActivate        string
	lastPersonaActivate string
	lastPersonaKey      string
	lastPersona         *config.Persona
	lastSessionPersona  string
	lastSessionLimit    int
	lastDeleteSessionID string
	lastPhrasesKey      string
	lastPhrasesValue    map[string][]string
	lastApprovalSession string
}

func (f *fakeAdminApp) ListLLMProfiles() ([]config.LLMProfile, error) {
	return append([]config.LLMProfile(nil), f.profiles...), nil
}
func (f *fakeAdminApp) GetLLMProfile(id string) (*config.LLMProfile, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	for i := range f.profiles {
		if f.profiles[i].Name == id {
			cp := f.profiles[i]
			return &cp, nil
		}
	}
	return nil, errors.New("llm profile not found")
}
func (f *fakeAdminApp) GetActiveLLMProfile() (*config.LLMProfile, bool) {
	if f.active == nil {
		return nil, false
	}
	cp := *f.active
	return &cp, true
}
func (f *fakeAdminApp) CreateLLMProfile(profile config.LLMProfile) error {
	f.lastCreate = profile
	return f.createErr
}
func (f *fakeAdminApp) UpdateLLMProfile(id string, profile config.LLMProfile) error { return nil }
func (f *fakeAdminApp) ActivateLLMProfile(id string) error {
	f.lastActivate = id
	return f.activateErr
}
func (f *fakeAdminApp) DeleteLLMProfile(id string) error         { return nil }
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
func (f *fakeAdminApp) ActivatePersona(key string) error {
	f.lastPersonaActivate = key
	return f.activatePersonaErr
}
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
func (f *fakeAdminApp) GetDefaultPersonaName() string { return f.defaultKey }
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

func floatPtr(v float64) *float64 { return &v }

func TestHandleListLLMProfiles(t *testing.T) {
	app := &fakeAdminApp{
		profiles: []config.LLMProfile{{Name: "default", Provider: "openai", SummaryTemperature: floatPtr(0.11)}},
		active:   &config.LLMProfile{Name: "default", Provider: "openai", SummaryTemperature: floatPtr(0.11)},
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/llm-profiles", nil)
	rec := httptest.NewRecorder()
	handler.HandleListLLMProfiles(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp llmProfilesResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if resp.ActiveID != "default" {
		t.Fatalf("ActiveID = %q, want default", resp.ActiveID)
	}
	if len(resp.Profiles) != 1 || resp.Profiles[0].ID != "default" {
		t.Fatalf("Profiles = %#v, want one default profile", resp.Profiles)
	}
	if resp.Profiles[0].SummaryTemperature == nil || *resp.Profiles[0].SummaryTemperature != 0.11 {
		t.Fatalf("SummaryTemperature = %#v, want 0.11", resp.Profiles[0].SummaryTemperature)
	}
}

func TestHandleCreateLLMProfileMapsConflict(t *testing.T) {
	app := &fakeAdminApp{createErr: apperrors.ErrLLMProfileExists}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	body := bytes.NewBufferString(`{"id":"default","name":"Default","provider":"openai","base_url":"https://api.openai.com","model":"gpt-4o","max_tokens":128,"temperature":0.7}`)
	req := httptest.NewRequest(http.MethodPost, "/api/llm-profiles", body)
	rec := httptest.NewRecorder()
	handler.HandleCreateLLMProfile(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestHandleCreateLLMProfileParsesBudgetOverrides(t *testing.T) {
	app := &fakeAdminApp{}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	body := bytes.NewBufferString(`{"id":"default","name":"Default","provider":"openai","base_url":"https://api.openai.com","model":"gpt-4o","max_tokens":128,"temperature":0.7,"input_budget_tokens":12000,"soft_compact_ratio":0.6,"hard_compact_ratio":0.85,"reserve_output_tokens":1024}`)
	req := httptest.NewRequest(http.MethodPost, "/api/llm-profiles", body)
	rec := httptest.NewRecorder()
	handler.HandleCreateLLMProfile(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if app.lastCreate.InputBudgetTokens == nil || *app.lastCreate.InputBudgetTokens != 12000 {
		t.Fatalf("InputBudgetTokens = %#v, want 12000", app.lastCreate.InputBudgetTokens)
	}
	if app.lastCreate.SoftCompactRatio == nil || *app.lastCreate.SoftCompactRatio != 0.6 {
		t.Fatalf("SoftCompactRatio = %#v, want 0.6", app.lastCreate.SoftCompactRatio)
	}
	if app.lastCreate.HardCompactRatio == nil || *app.lastCreate.HardCompactRatio != 0.85 {
		t.Fatalf("HardCompactRatio = %#v, want 0.85", app.lastCreate.HardCompactRatio)
	}
	if app.lastCreate.ReserveOutputTokens == nil || *app.lastCreate.ReserveOutputTokens != 1024 {
		t.Fatalf("ReserveOutputTokens = %#v, want 1024", app.lastCreate.ReserveOutputTokens)
	}
}

func TestHandleCreateLLMProfileParsesSummaryTemperature(t *testing.T) {
	t.Run("numeric summary temperature", func(t *testing.T) {
		app := &fakeAdminApp{}
		handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

		body := bytes.NewBufferString(`{"id":"default","name":"Default","provider":"openai","base_url":"https://api.openai.com","model":"gpt-4o","max_tokens":128,"temperature":0.7,"summary_temperature":0.15}`)
		req := httptest.NewRequest(http.MethodPost, "/api/llm-profiles", body)
		rec := httptest.NewRecorder()
		handler.HandleCreateLLMProfile(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201", rec.Code)
		}
		if app.lastCreate.SummaryTemperature == nil || *app.lastCreate.SummaryTemperature != 0.15 {
			t.Fatalf("SummaryTemperature = %#v, want 0.15", app.lastCreate.SummaryTemperature)
		}
	})

	t.Run("null summary temperature", func(t *testing.T) {
		app := &fakeAdminApp{}
		handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

		body := bytes.NewBufferString(`{"id":"default","name":"Default","provider":"openai","base_url":"https://api.openai.com","model":"gpt-4o","max_tokens":128,"temperature":0.7,"summary_temperature":null}`)
		req := httptest.NewRequest(http.MethodPost, "/api/llm-profiles", body)
		rec := httptest.NewRecorder()
		handler.HandleCreateLLMProfile(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201", rec.Code)
		}
		if app.lastCreate.SummaryTemperature != nil {
			t.Fatalf("SummaryTemperature = %#v, want nil", app.lastCreate.SummaryTemperature)
		}
	})
}

func TestHandleGetLLMProfileMapsWrappedNotFound(t *testing.T) {
	app := &fakeAdminApp{getErr: fmt.Errorf("wrapped: %w", apperrors.ErrLLMProfileNotFound)}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/llm-profiles/missing", nil)
	req.SetPathValue("id", "missing")
	rec := httptest.NewRecorder()
	handler.HandleGetLLMProfile(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandleGetLLMProfileMapsUnknownErrorToInternalServerError(t *testing.T) {
	app := &fakeAdminApp{getErr: errors.New("db down")}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/llm-profiles/missing", nil)
	req.SetPathValue("id", "missing")
	rec := httptest.NewRecorder()
	handler.HandleGetLLMProfile(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestHandleActivateLLMProfileMapsBadRequest(t *testing.T) {
	app := &fakeAdminApp{activateErr: errors.New("OPENAI_API_KEY environment variable not set")}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodPost, "/api/llm-profiles/default/activate", nil)
	req.SetPathValue("id", "default")
	rec := httptest.NewRecorder()
	handler.HandleActivateLLMProfile(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if app.lastActivate != "default" {
		t.Fatalf("lastActivate = %q, want default", app.lastActivate)
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

func TestHandleActivatePersona(t *testing.T) {
	app := &fakeAdminApp{}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodPost, "/api/personas/default/activate", nil)
	req.SetPathValue("name", "default")
	rec := httptest.NewRecorder()
	handler.HandleActivatePersona(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if app.lastPersonaActivate != "default" {
		t.Fatalf("lastPersonaActivate = %q, want default", app.lastPersonaActivate)
	}
}

func TestHandleActivatePersonaMapsNotFound(t *testing.T) {
	app := &fakeAdminApp{activatePersonaErr: apperrors.ErrPersonaNotFound}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodPost, "/api/personas/missing/activate", nil)
	req.SetPathValue("name", "missing")
	rec := httptest.NewRecorder()
	handler.HandleActivatePersona(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
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
