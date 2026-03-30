package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
)

type fakeAdminApp struct {
	profiles       []config.LLMProfile
	active         *config.LLMProfile
	personas       map[string]*config.Persona
	defaultKey     string
	createErr      error
	activateErr    error
	lastCreate     config.LLMProfile
	lastActivate   string
	lastPersonaKey string
	lastPersona    *config.Persona
}

func (f *fakeAdminApp) ListLLMProfiles() ([]config.LLMProfile, error) {
	return append([]config.LLMProfile(nil), f.profiles...), nil
}
func (f *fakeAdminApp) GetLLMProfile(id string) (*config.LLMProfile, error) {
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
func (f *fakeAdminApp) GetDefaultPersonaName() string                     { return f.defaultKey }

func TestHandleListLLMProfiles(t *testing.T) {
	app := &fakeAdminApp{
		profiles: []config.LLMProfile{{Name: "default", Provider: "openai"}},
		active:   &config.LLMProfile{Name: "default", Provider: "openai"},
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
}

func TestHandleCreateLLMProfileMapsConflict(t *testing.T) {
	app := &fakeAdminApp{createErr: errors.New("llm profile already exists")}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	body := bytes.NewBufferString(`{"id":"default","name":"Default","provider":"openai","base_url":"https://api.openai.com","model":"gpt-4o","max_tokens":128,"temperature":0.7}`)
	req := httptest.NewRequest(http.MethodPost, "/api/llm-profiles", body)
	rec := httptest.NewRecorder()
	handler.HandleCreateLLMProfile(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
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
