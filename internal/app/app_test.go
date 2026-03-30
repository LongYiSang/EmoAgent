package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/web"
)

type routeTestAdminApp struct {
	profiles   []config.LLMProfile
	active     *config.LLMProfile
	lastActive string
}

func (a *routeTestAdminApp) ListLLMProfiles() ([]config.LLMProfile, error) {
	return append([]config.LLMProfile(nil), a.profiles...), nil
}

func (a *routeTestAdminApp) GetLLMProfile(id string) (*config.LLMProfile, error) {
	for i := range a.profiles {
		if a.profiles[i].Name == id {
			cp := a.profiles[i]
			return &cp, nil
		}
	}
	return nil, ErrLLMProfileNotFound
}

func (a *routeTestAdminApp) GetActiveLLMProfile() (*config.LLMProfile, bool) {
	if a.active == nil {
		return nil, false
	}
	cp := *a.active
	return &cp, true
}

func (a *routeTestAdminApp) CreateLLMProfile(profile config.LLMProfile) error { return nil }
func (a *routeTestAdminApp) UpdateLLMProfile(id string, profile config.LLMProfile) error {
	return nil
}
func (a *routeTestAdminApp) ActivateLLMProfile(id string) error {
	a.lastActive = id
	return nil
}
func (a *routeTestAdminApp) DeleteLLMProfile(id string) error { return nil }
func (a *routeTestAdminApp) ListPersonas() map[string]*config.Persona {
	return map[string]*config.Persona{}
}
func (a *routeTestAdminApp) GetPersona(name string) (*config.Persona, bool) { return nil, false }
func (a *routeTestAdminApp) CreatePersona(key string, p *config.Persona) error {
	return nil
}
func (a *routeTestAdminApp) UpdatePersona(key string, p *config.Persona) error { return nil }
func (a *routeTestAdminApp) DeletePersona(key string) error                    { return nil }
func (a *routeTestAdminApp) GetDefaultPersonaName() string                     { return "default" }

func TestRunAllowsStartupWithoutLLM(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a := &App{
		Config: &config.Config{
			Server: config.ServerConfig{Host: "127.0.0.1", Port: 0},
			LLM:    config.LLMConfig{Model: "test-model", MaxTokens: 64, Temperature: 0.3},
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	if err := a.Run(ctx); err != nil {
		t.Fatalf("Run() with canceled context should still shut down cleanly, got %v", err)
	}
}

func TestGetDefaultPersonaName(t *testing.T) {
	a := &App{
		Config: &config.Config{
			Personas: config.PersonasConfig{Default: "default"},
		},
	}

	if got := a.GetDefaultPersonaName(); got != "default" {
		t.Fatalf("GetDefaultPersonaName = %q, want default", got)
	}
}

func TestGetActiveLLMProfileReturnsCopy(t *testing.T) {
	a := &App{
		ActiveLLMProfile: &config.LLMProfile{Name: "default", Model: "gpt-4o"},
	}

	profile, ok := a.GetActiveLLMProfile()
	if !ok {
		t.Fatal("GetActiveLLMProfile returned ok=false")
	}
	profile.Model = "changed"

	if a.ActiveLLMProfile.Model != "gpt-4o" {
		t.Fatalf("ActiveLLMProfile mutated through copy, got %q", a.ActiveLLMProfile.Model)
	}
}

func TestRegisterRoutesLLMProfileDispatch(t *testing.T) {
	adminApp := &routeTestAdminApp{
		profiles: []config.LLMProfile{{Name: "default", Provider: "openai", Model: "gpt-4o"}},
		active:   &config.LLMProfile{Name: "default", Provider: "openai", Model: "gpt-4o"},
	}
	api := web.NewAPIHandler(adminApp, slog.New(slog.NewTextHandler(io.Discard, nil)))
	mux := http.NewServeMux()

	registerRoutes(
		mux,
		api,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		http.NotFoundHandler(),
	)

	t.Run("list profiles", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/llm-profiles", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}

		var resp struct {
			ActiveID string `json:"active_id"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if resp.ActiveID != "default" {
			t.Fatalf("active_id = %q, want default", resp.ActiveID)
		}
	})

	t.Run("get profile detail", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/llm-profiles/default", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}

		var resp struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if resp.ID != "default" {
			t.Fatalf("id = %q, want default", resp.ID)
		}
	})

	t.Run("activate does not collide with detail route", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/llm-profiles/default/activate", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if adminApp.lastActive != "default" {
			t.Fatalf("lastActive = %q, want default", adminApp.lastActive)
		}
	})

	t.Run("activate path with wrong method does not hit activate handler", func(t *testing.T) {
		adminApp.lastActive = ""

		req := httptest.NewRequest(http.MethodGet, "/api/llm-profiles/default/activate", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
		if adminApp.lastActive != "" {
			t.Fatalf("lastActive changed on GET activate path, got %q", adminApp.lastActive)
		}
	})

	t.Run("trailing slash does not hit list route", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/llm-profiles/", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})
}
