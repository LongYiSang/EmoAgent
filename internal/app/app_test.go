package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/longyisang/emoagent/internal/apperrors"
	"github.com/longyisang/emoagent/internal/chat"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/tool/builtin"
	"github.com/longyisang/emoagent/internal/web"
)

type routeTestAdminApp struct {
	profiles            []config.LLMProfile
	active              *config.LLMProfile
	lastActive          string
	defaultKey          string
	lastPersonaActivate string
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
func (a *routeTestAdminApp) GetProgressPhrases(key string) (map[string][]string, error) {
	return map[string][]string{}, nil
}
func (a *routeTestAdminApp) UpdateProgressPhrases(key string, phrases map[string][]string) error {
	return nil
}
func (a *routeTestAdminApp) ActivatePersona(key string) error {
	a.lastPersonaActivate = key
	a.defaultKey = key
	return nil
}
func (a *routeTestAdminApp) ListSessions(ctx context.Context, persona string, limit int) ([]storage.SessionSummary, error) {
	return nil, nil
}
func (a *routeTestAdminApp) GetLatestSession(ctx context.Context, persona string) (*storage.SessionSummary, error) {
	return nil, nil
}
func (a *routeTestAdminApp) GetSessionDetail(ctx context.Context, id string) (*storage.SessionRecord, []storage.MessageRecord, error) {
	return nil, nil, nil
}
func (a *routeTestAdminApp) DeleteSession(ctx context.Context, id string) error {
	return nil
}
func (a *routeTestAdminApp) ListSessionApprovals(ctx context.Context, sessionID string) ([]protocol.ApprovalRequest, error) {
	return nil, nil
}
func (a *routeTestAdminApp) GetChatSettings() config.ChatConfig {
	return config.ChatConfig{}
}
func (a *routeTestAdminApp) UpdateChatSettings(settings config.ChatConfig) error {
	return nil
}
func (a *routeTestAdminApp) GetDefaultPersonaName() string {
	if a.defaultKey == "" {
		return "default"
	}
	return a.defaultKey
}

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
		profiles:   []config.LLMProfile{{Name: "default", Provider: "openai", Model: "gpt-4o"}},
		active:     &config.LLMProfile{Name: "default", Provider: "openai", Model: "gpt-4o"},
		defaultKey: "default",
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

	t.Run("activate persona route dispatches correctly", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/personas/default/activate", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if adminApp.lastPersonaActivate != "default" {
			t.Fatalf("lastPersonaActivate = %q, want default", adminApp.lastPersonaActivate)
		}
	})
}

func TestUpdateLLMProfileRebuildsClientWhenActiveClientIsNil(t *testing.T) {
	t.Setenv("TEST_OPENAI_API_KEY", "test-key")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.UpsertLLMProfile(config.LLMProfile{
		Name:         "default",
		Provider:     "openai",
		BaseURL:      "https://api.openai.com",
		Model:        "gpt-4o-mini",
		SummaryModel: "",
		MaxTokens:    128,
		Temperature:  0.2,
		APIKeyEnv:    "TEST_OPENAI_API_KEY",
	}); err != nil {
		t.Fatalf("UpsertLLMProfile: %v", err)
	}

	a := &App{
		Config: &config.Config{
			LLM: config.LLMConfig{
				Provider:    "openai",
				BaseURL:     "https://api.openai.com",
				Model:       "gpt-4o-mini",
				MaxTokens:   128,
				Temperature: 0.2,
				APIKeyEnv:   "TEST_OPENAI_API_KEY",
			},
		},
		DB:               db,
		Logger:           logger,
		ActiveLLMProfile: &config.LLMProfile{Name: "default", Provider: "openai", BaseURL: "https://api.openai.com", Model: "gpt-4o-mini", MaxTokens: 128, Temperature: 0.2, APIKeyEnv: "TEST_OPENAI_API_KEY", InputBudgetTokens: intPtr(9000), ReserveOutputTokens: intPtr(512)},
		engine:           chat.NewEngine(chat.EngineConfig{DB: db, Logger: logger, Model: "gpt-4o-mini", MaxTokens: 128, Temperature: 0.2}),
	}

	err = a.UpdateLLMProfile("default", config.LLMProfile{
		Provider:    "openai",
		BaseURL:     "https://api.openai.com",
		Model:       "gpt-4.1-mini",
		MaxTokens:   256,
		Temperature: 0.4,
		APIKeyEnv:   "TEST_OPENAI_API_KEY",
	})
	if err != nil {
		t.Fatalf("UpdateLLMProfile: %v", err)
	}
	if a.LLM == nil {
		t.Fatal("UpdateLLMProfile did not rebuild missing active client")
	}
	if a.ActiveLLMProfile == nil || a.ActiveLLMProfile.Model != "gpt-4.1-mini" {
		t.Fatalf("ActiveLLMProfile = %#v, want updated model", a.ActiveLLMProfile)
	}
}

func TestActivatePersonaUpdatesRuntimeDefault(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	a := &App{
		Config: &config.Config{
			Personas: config.PersonasConfig{Default: "default"},
		},
		DB:     db,
		Logger: logger,
		Personas: map[string]*config.Persona{
			"default": {Name: "Emo"},
			"tami":    {Name: "Tami"},
		},
	}

	if err := a.ActivatePersona("tami"); err != nil {
		t.Fatalf("ActivatePersona: %v", err)
	}
	if got := a.GetDefaultPersonaName(); got != "tami" {
		t.Fatalf("GetDefaultPersonaName = %q, want tami", got)
	}

	value, found, err := db.GetRuntimeConfig("personas.default")
	if err != nil {
		t.Fatalf("GetRuntimeConfig: %v", err)
	}
	if !found {
		t.Fatal("personas.default not persisted")
	}
	if value != "tami" {
		t.Fatalf("personas.default = %q, want tami", value)
	}
}

func TestUpdateChatSettingsPersistsRuntimeOverrideAndHotUpdatesEngine(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	engine := chat.NewEngine(chat.EngineConfig{
		DB:          db,
		Logger:      logger,
		Model:       "test-model",
		MaxTokens:   128,
		Temperature: 0.2,
	})
	a := &App{
		Config: &config.Config{Chat: config.ChatConfig{RealtimeStreaming: false}},
		DB:     db,
		Logger: logger,
		engine: engine,
	}

	if err := a.UpdateChatSettings(config.ChatConfig{RealtimeStreaming: true}); err != nil {
		t.Fatalf("UpdateChatSettings: %v", err)
	}

	value, ok, err := db.GetRuntimeConfig("chat.realtime_streaming")
	if err != nil {
		t.Fatalf("GetRuntimeConfig: %v", err)
	}
	if !ok || value != "true" {
		t.Fatalf("runtime chat.realtime_streaming = %q/%t, want true/true", value, ok)
	}
	if !a.Config.Chat.RealtimeStreaming {
		t.Fatal("Config.Chat.RealtimeStreaming = false, want true")
	}
	if !engine.RuntimeConfig().RealtimeStreaming {
		t.Fatal("engine realtime streaming = false, want true")
	}
}

func TestCreatePersonaStoresByKeyInDB(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	a := &App{
		Config: &config.Config{
			Personas: config.PersonasConfig{Dir: t.TempDir(), Default: "default"},
		},
		DB:       db,
		Logger:   logger,
		Personas: map[string]*config.Persona{},
	}

	err = a.CreatePersona("neko", &config.Persona{
		Name:         "Tami",
		Description:  "cat roommate",
		SystemPrompt: "You are Tami.",
		Tone:         "snarky",
		Greeting:     "meow",
	})
	if err != nil {
		t.Fatalf("CreatePersona: %v", err)
	}

	record, err := db.GetPersona(context.Background(), "neko")
	if err != nil {
		t.Fatalf("GetPersona: %v", err)
	}
	if record == nil {
		t.Fatal("GetPersona returned nil")
	}
	if record.Key != "neko" {
		t.Fatalf("record.Key = %q, want neko", record.Key)
	}
	if record.Name != "Tami" {
		t.Fatalf("record.Name = %q, want Tami", record.Name)
	}
}

func TestUpdatePersonaKeepsStableDBKeyWhenDisplayNameChanges(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	personaDir := t.TempDir()
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	a := &App{
		Config: &config.Config{
			Personas: config.PersonasConfig{Dir: personaDir, Default: "default"},
		},
		DB:     db,
		Logger: logger,
		Personas: map[string]*config.Persona{
			"neko": {Name: "Tami", Description: "cat roommate"},
		},
	}

	if err := config.SavePersona(personaDir, "neko", a.Personas["neko"]); err != nil {
		t.Fatalf("SavePersona: %v", err)
	}
	if err := db.UpsertPersona("neko", "Tami", "cat roommate", "prompt", "snarky", nil, "meow", nil); err != nil {
		t.Fatalf("UpsertPersona: %v", err)
	}

	err = a.UpdatePersona("neko", &config.Persona{
		Name:         "Mimi",
		Description:  "updated cat roommate",
		SystemPrompt: "You are Mimi.",
		Tone:         "cool",
		Greeting:     "hi",
	})
	if err != nil {
		t.Fatalf("UpdatePersona: %v", err)
	}

	record, err := db.GetPersona(context.Background(), "neko")
	if err != nil {
		t.Fatalf("GetPersona(updated): %v", err)
	}
	if record == nil {
		t.Fatal("updated record missing")
	}
	if record.Key != "neko" {
		t.Fatalf("record.Key = %q, want neko", record.Key)
	}
	if record.Name != "Mimi" {
		t.Fatalf("record.Name = %q, want Mimi", record.Name)
	}
}

func TestGetPersonaReturnsDeepCopyOfWorkProgressPhrases(t *testing.T) {
	a := &App{
		Personas: map[string]*config.Persona{
			"default": {
				Name: "default",
				WorkProgressPhrases: map[string][]string{
					"read_file": {"看看文件"},
				},
			},
		},
	}

	persona, ok := a.GetPersona("default")
	if !ok || persona == nil {
		t.Fatal("GetPersona returned nil")
	}
	persona.WorkProgressPhrases["read_file"][0] = "mutated"
	persona.WorkProgressPhrases["new_key"] = []string{"new"}

	original := a.Personas["default"].WorkProgressPhrases
	if original["read_file"][0] != "看看文件" {
		t.Fatalf("original read_file phrase = %q, want untouched", original["read_file"][0])
	}
	if _, exists := original["new_key"]; exists {
		t.Fatalf("original map unexpectedly mutated: %#v", original)
	}
}

func TestUpdateProgressPhrasesPersistsToFileDBAndMemory(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	personaDir := t.TempDir()
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	initial := &config.Persona{
		Name:        "default",
		Description: "desc",
	}

	a := &App{
		Config: &config.Config{
			Personas: config.PersonasConfig{Dir: personaDir, Default: "default"},
		},
		DB:     db,
		Logger: logger,
		Personas: map[string]*config.Persona{
			"default": initial,
		},
	}

	if err := config.SavePersona(personaDir, "default", initial); err != nil {
		t.Fatalf("SavePersona: %v", err)
	}
	if err := db.UpsertPersona("default", initial.Name, initial.Description, initial.SystemPrompt, initial.Tone, initial.Quirks, initial.Greeting, initial.WorkProgressPhrases); err != nil {
		t.Fatalf("UpsertPersona: %v", err)
	}

	phrases := map[string][]string{
		"read_file": {"看看文件"},
		"_default":  {"处理中"},
	}
	if err := a.UpdateProgressPhrases("default", phrases); err != nil {
		t.Fatalf("UpdateProgressPhrases: %v", err)
	}

	if got := a.Personas["default"].WorkProgressPhrases["read_file"]; len(got) != 1 || got[0] != "看看文件" {
		t.Fatalf("memory phrases = %#v, want read_file phrase", a.Personas["default"].WorkProgressPhrases)
	}

	loaded, err := config.LoadPersona(filepath.Join(personaDir, "default.yaml"))
	if err != nil {
		t.Fatalf("LoadPersona: %v", err)
	}
	if got := loaded.WorkProgressPhrases["read_file"]; len(got) != 1 || got[0] != "看看文件" {
		t.Fatalf("file phrases = %#v, want read_file phrase", loaded.WorkProgressPhrases)
	}

	record, err := db.GetPersona(context.Background(), "default")
	if err != nil {
		t.Fatalf("GetPersona: %v", err)
	}
	if record == nil {
		t.Fatal("GetPersona returned nil")
	}
	var decoded map[string][]string
	if err := json.Unmarshal([]byte(record.WorkProgressPhrases), &decoded); err != nil {
		t.Fatalf("Unmarshal WorkProgressPhrases: %v", err)
	}
	if got := decoded["read_file"]; len(got) != 1 || got[0] != "看看文件" {
		t.Fatalf("db phrases = %#v, want read_file phrase", decoded)
	}
}

func TestDeleteSessionReturnsNotFoundForMissingSession(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	a := &App{DB: db, Logger: logger}

	err = a.DeleteSession(context.Background(), "missing")
	if err == nil {
		t.Fatal("DeleteSession should fail for missing session")
	}
	if !errors.Is(err, apperrors.ErrSessionNotFound) {
		t.Fatalf("DeleteSession error = %v, want ErrSessionNotFound", err)
	}
}

func TestGetSessionDetailReturnsMessages(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	a := &App{DB: db, Logger: logger}
	ctx := context.Background()
	if err := db.CreateSession(ctx, "session-1", "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := db.AddMessage(ctx, "msg-1", "session-1", "user", "hello"); err != nil {
		t.Fatalf("AddMessage(user): %v", err)
	}
	if err := db.AddMessage(ctx, "msg-2", "session-1", "assistant", "hi"); err != nil {
		t.Fatalf("AddMessage(assistant): %v", err)
	}

	session, messages, err := a.GetSessionDetail(ctx, "session-1")
	if err != nil {
		t.Fatalf("GetSessionDetail: %v", err)
	}
	if session == nil || session.ID != "session-1" {
		t.Fatalf("session = %#v, want session-1", session)
	}
	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(messages))
	}
	if messages[0].Content != "hello" || messages[1].Content != "hi" {
		t.Fatalf("messages = %#v, want [hello hi]", messages)
	}
}

func TestRunPassesSummaryModelAndContextConfigToEngine(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a := &App{
		Config: &config.Config{
			Server: config.ServerConfig{Host: "127.0.0.1", Port: 0},
			LLM: config.LLMConfig{
				Model:              "primary-model",
				SummaryModel:       "summary-model",
				SummaryTemperature: floatPtr(0.25),
				MaxTokens:          64,
				Temperature:        0.3,
			},
			Context: config.ContextConfig{
				InputBudgetTokens:    111,
				SoftCompactRatio:     0.60,
				HardCompactRatio:     0.80,
				ReserveOutputTokens:  22,
				KeepRecentUserTurns:  3,
				ToolResultSoftTokens: 44,
				ToolResultHardTokens: 55,
			},
		},
		Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		ActiveLLMProfile: &config.LLMProfile{Name: "active", Provider: "openai", Model: "profile-model", SummaryModel: "profile-summary", SummaryTemperature: floatPtr(0.05), MaxTokens: 128, Temperature: 0.1, InputBudgetTokens: intPtr(9000), ReserveOutputTokens: intPtr(512)},
	}

	if err := a.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if a.engine == nil {
		t.Fatal("engine was not initialized")
	}

	runtimeCfg := a.engine.RuntimeConfig()
	if runtimeCfg.Model != "profile-model" {
		t.Fatalf("runtime model = %q, want profile-model", runtimeCfg.Model)
	}
	if runtimeCfg.SummaryModel != "profile-summary" {
		t.Fatalf("runtime summary model = %q, want profile-summary", runtimeCfg.SummaryModel)
	}
	if runtimeCfg.SummaryTemperature == nil || *runtimeCfg.SummaryTemperature != 0.05 {
		t.Fatalf("runtime summary temperature = %#v, want 0.05", runtimeCfg.SummaryTemperature)
	}
	if runtimeCfg.ContextConfig.KeepRecentUserTurns != 3 {
		t.Fatalf("runtime keep recent = %d, want 3", runtimeCfg.ContextConfig.KeepRecentUserTurns)
	}
	if runtimeCfg.ContextConfig.InputBudgetTokens != 9000 {
		t.Fatalf("runtime input budget = %d, want 9000", runtimeCfg.ContextConfig.InputBudgetTokens)
	}
	if runtimeCfg.ContextConfig.ReserveOutputTokens != 512 {
		t.Fatalf("runtime reserve output = %d, want 512", runtimeCfg.ContextConfig.ReserveOutputTokens)
	}
	if runtimeCfg.ContextConfig.SoftCompactRatio != 0.60 {
		t.Fatalf("runtime soft ratio = %v, want 0.60", runtimeCfg.ContextConfig.SoftCompactRatio)
	}
}

func TestRunFallsBackToGlobalOrDefaultSummaryTemperature(t *testing.T) {
	t.Run("inherit global summary temperature when profile unset", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		a := &App{
			Config: &config.Config{
				Server: config.ServerConfig{Host: "127.0.0.1", Port: 0},
				LLM: config.LLMConfig{
					Model:              "primary-model",
					SummaryTemperature: floatPtr(0.2),
					MaxTokens:          64,
					Temperature:        0.3,
				},
			},
			Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
			ActiveLLMProfile: &config.LLMProfile{Name: "active", Provider: "openai", Model: "profile-model", MaxTokens: 128, Temperature: 0.1},
		}

		if err := a.Run(ctx); err != nil {
			t.Fatalf("Run: %v", err)
		}

		runtimeCfg := a.engine.RuntimeConfig()
		if runtimeCfg.SummaryTemperature == nil || *runtimeCfg.SummaryTemperature != 0.2 {
			t.Fatalf("runtime summary temperature = %#v, want inherited 0.2", runtimeCfg.SummaryTemperature)
		}
	})

	t.Run("default summary temperature when unset everywhere", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		a := &App{
			Config: &config.Config{
				Server: config.ServerConfig{Host: "127.0.0.1", Port: 0},
				LLM: config.LLMConfig{
					Model:       "primary-model",
					MaxTokens:   64,
					Temperature: 0.3,
				},
			},
			Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
			ActiveLLMProfile: &config.LLMProfile{Name: "active", Provider: "openai", Model: "profile-model", MaxTokens: 128, Temperature: 0.1},
		}

		if err := a.Run(ctx); err != nil {
			t.Fatalf("Run: %v", err)
		}

		runtimeCfg := a.engine.RuntimeConfig()
		if runtimeCfg.SummaryTemperature == nil || *runtimeCfg.SummaryTemperature != 0.1 {
			t.Fatalf("runtime summary temperature = %#v, want default 0.1", runtimeCfg.SummaryTemperature)
		}
	})
}

func TestUpdateLLMProfilePassesSummaryModelAndContextConfigToEngine(t *testing.T) {
	t.Setenv("TEST_OPENAI_API_KEY", "test-key")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.UpsertLLMProfile(config.LLMProfile{
		Name:               "default",
		Provider:           "openai",
		BaseURL:            "https://api.openai.com",
		Model:              "gpt-4o-mini",
		SummaryModel:       "",
		SummaryTemperature: floatPtr(0.18),
		MaxTokens:          128,
		Temperature:        0.2,
		APIKeyEnv:          "TEST_OPENAI_API_KEY",
	}); err != nil {
		t.Fatalf("UpsertLLMProfile: %v", err)
	}

	a := &App{
		Config: &config.Config{
			LLM: config.LLMConfig{
				Provider:           "openai",
				BaseURL:            "https://api.openai.com",
				Model:              "gpt-4o-mini",
				SummaryTemperature: floatPtr(0.16),
				MaxTokens:          128,
				Temperature:        0.2,
				APIKeyEnv:          "TEST_OPENAI_API_KEY",
			},
			Context: config.ContextConfig{
				InputBudgetTokens:    999,
				SoftCompactRatio:     0.65,
				HardCompactRatio:     0.85,
				ReserveOutputTokens:  100,
				KeepRecentUserTurns:  7,
				ToolResultSoftTokens: 88,
				ToolResultHardTokens: 99,
			},
		},
		DB:               db,
		Logger:           logger,
		ActiveLLMProfile: &config.LLMProfile{Name: "default", Provider: "openai", BaseURL: "https://api.openai.com", Model: "gpt-4o-mini", SummaryTemperature: floatPtr(0.18), MaxTokens: 128, Temperature: 0.2, APIKeyEnv: "TEST_OPENAI_API_KEY", ReserveOutputTokens: intPtr(4096)},
		engine:           chat.NewEngine(chat.EngineConfig{DB: db, Logger: logger, Model: "gpt-4o-mini", MaxTokens: 128, Temperature: 0.2, ContextConfig: config.DefaultConfig().Context}),
	}

	err = a.UpdateLLMProfile("default", config.LLMProfile{
		Provider:           "openai",
		BaseURL:            "https://api.openai.com",
		Model:              "gpt-4.1-mini",
		SummaryModel:       "gpt-4.1-nano",
		SummaryTemperature: floatPtr(0.12),
		MaxTokens:          256,
		Temperature:        0.4,
		APIKeyEnv:          "TEST_OPENAI_API_KEY",
	})
	if err != nil {
		t.Fatalf("UpdateLLMProfile: %v", err)
	}

	runtimeCfg := a.engine.RuntimeConfig()
	if runtimeCfg.SummaryModel != "gpt-4.1-nano" {
		t.Fatalf("runtime summary model = %q, want gpt-4.1-nano", runtimeCfg.SummaryModel)
	}
	if runtimeCfg.SummaryTemperature == nil || *runtimeCfg.SummaryTemperature != 0.12 {
		t.Fatalf("runtime summary temperature = %#v, want 0.12", runtimeCfg.SummaryTemperature)
	}
	if runtimeCfg.ContextConfig.KeepRecentUserTurns != 7 {
		t.Fatalf("runtime keep recent = %d, want 7", runtimeCfg.ContextConfig.KeepRecentUserTurns)
	}
	if runtimeCfg.ContextConfig.InputBudgetTokens != 999 {
		t.Fatalf("runtime input budget = %d, want 999", runtimeCfg.ContextConfig.InputBudgetTokens)
	}
	if runtimeCfg.ContextConfig.ReserveOutputTokens != 100 {
		t.Fatalf("runtime reserve output = %d, want 100", runtimeCfg.ContextConfig.ReserveOutputTokens)
	}
}

func TestActivateLLMProfilePassesSummaryModelAndContextConfigToEngine(t *testing.T) {
	t.Setenv("TEST_OPENAI_API_KEY", "test-key")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.UpsertLLMProfile(config.LLMProfile{
		Name:               "default",
		Provider:           "openai",
		BaseURL:            "https://api.openai.com",
		Model:              "gpt-4o-mini",
		SummaryModel:       "gpt-4o-mini",
		SummaryTemperature: floatPtr(0.2),
		MaxTokens:          128,
		Temperature:        0.2,
		APIKeyEnv:          "TEST_OPENAI_API_KEY",
	}); err != nil {
		t.Fatalf("UpsertLLMProfile(default): %v", err)
	}
	if err := db.UpsertLLMProfile(config.LLMProfile{
		Name:                "alt",
		Provider:            "openai",
		BaseURL:             "https://api.openai.com",
		Model:               "gpt-4.1-mini",
		SummaryModel:        "gpt-4.1-nano",
		SummaryTemperature:  floatPtr(0.07),
		MaxTokens:           256,
		Temperature:         0.1,
		APIKeyEnv:           "TEST_OPENAI_API_KEY",
		InputBudgetTokens:   intPtr(8888),
		SoftCompactRatio:    floatPtr(0.72),
		ReserveOutputTokens: intPtr(512),
	}); err != nil {
		t.Fatalf("UpsertLLMProfile(alt): %v", err)
	}

	a := &App{
		Config: &config.Config{
			Context: config.ContextConfig{
				InputBudgetTokens:    321,
				SoftCompactRatio:     0.66,
				HardCompactRatio:     0.88,
				ReserveOutputTokens:  123,
				KeepRecentUserTurns:  5,
				ToolResultSoftTokens: 77,
				ToolResultHardTokens: 111,
			},
		},
		DB:               db,
		Logger:           logger,
		ActiveLLMProfile: &config.LLMProfile{Name: "default", Provider: "openai", BaseURL: "https://api.openai.com", Model: "gpt-4o-mini", SummaryModel: "gpt-4o-mini", SummaryTemperature: floatPtr(0.2), MaxTokens: 128, Temperature: 0.2, APIKeyEnv: "TEST_OPENAI_API_KEY"},
		engine:           chat.NewEngine(chat.EngineConfig{DB: db, Logger: logger, Model: "gpt-4o-mini", MaxTokens: 128, Temperature: 0.2, ContextConfig: config.DefaultConfig().Context}),
	}

	if err := a.ActivateLLMProfile("alt"); err != nil {
		t.Fatalf("ActivateLLMProfile: %v", err)
	}

	runtimeCfg := a.engine.RuntimeConfig()
	if runtimeCfg.Model != "gpt-4.1-mini" {
		t.Fatalf("runtime model = %q, want gpt-4.1-mini", runtimeCfg.Model)
	}
	if runtimeCfg.SummaryModel != "gpt-4.1-nano" {
		t.Fatalf("runtime summary model = %q, want gpt-4.1-nano", runtimeCfg.SummaryModel)
	}
	if runtimeCfg.SummaryTemperature == nil || *runtimeCfg.SummaryTemperature != 0.07 {
		t.Fatalf("runtime summary temperature = %#v, want 0.07", runtimeCfg.SummaryTemperature)
	}
	if runtimeCfg.ContextConfig.KeepRecentUserTurns != 5 {
		t.Fatalf("runtime keep recent = %d, want 5", runtimeCfg.ContextConfig.KeepRecentUserTurns)
	}
	if runtimeCfg.ContextConfig.InputBudgetTokens != 8888 {
		t.Fatalf("runtime input budget = %d, want 8888", runtimeCfg.ContextConfig.InputBudgetTokens)
	}
	if runtimeCfg.ContextConfig.SoftCompactRatio != 0.72 {
		t.Fatalf("runtime soft ratio = %v, want 0.72", runtimeCfg.ContextConfig.SoftCompactRatio)
	}
	if runtimeCfg.ContextConfig.HardCompactRatio != 0.88 {
		t.Fatalf("runtime hard ratio = %v, want 0.88", runtimeCfg.ContextConfig.HardCompactRatio)
	}
	if runtimeCfg.ContextConfig.ReserveOutputTokens != 512 {
		t.Fatalf("runtime reserve output = %d, want 512", runtimeCfg.ContextConfig.ReserveOutputTokens)
	}
}

func TestResolveWorkProfilePrefersDB(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.UpsertLLMProfile(config.LLMProfile{
		Name:         "analysis",
		Provider:     "openai",
		BaseURL:      "https://api.openai.com",
		Model:        "db-model",
		SummaryModel: "db-summary",
		MaxTokens:    2048,
		Temperature:  0.2,
		APIKeyEnv:    "TEST_OPENAI_API_KEY",
	}); err != nil {
		t.Fatalf("UpsertLLMProfile: %v", err)
	}

	a := &App{
		Config: &config.Config{
			Work: config.WorkConfig{Profile: "analysis"},
			LLMProfiles: []config.LLMProfile{{
				Name:         "analysis",
				Provider:     "openai",
				BaseURL:      "https://api.openai.com",
				Model:        "config-model",
				SummaryModel: "config-summary",
				MaxTokens:    1024,
				Temperature:  0.7,
				APIKeyEnv:    "TEST_OPENAI_API_KEY",
			}},
		},
		DB:     db,
		Logger: logger,
	}

	profile, err := a.resolveWorkProfile()
	if err != nil {
		t.Fatalf("resolveWorkProfile: %v", err)
	}
	if profile.Model != "db-model" {
		t.Fatalf("model = %q, want db-model", profile.Model)
	}
	if profile.SummaryModel != "db-summary" {
		t.Fatalf("summary model = %q, want db-summary", profile.SummaryModel)
	}
}

func TestResolveWorkSummaryModelFallsBackToWorkPrimaryModel(t *testing.T) {
	profile := config.LLMProfile{
		Model:        "work-main",
		SummaryModel: "",
	}
	if got := resolveWorkSummaryModel(profile); got != "work-main" {
		t.Fatalf("resolveWorkSummaryModel() = %q, want work-main", got)
	}

	profile.SummaryModel = "work-summary"
	if got := resolveWorkSummaryModel(profile); got != "work-summary" {
		t.Fatalf("resolveWorkSummaryModel() = %q, want work-summary", got)
	}
}

func TestResolveWorkProfileSeedsFromConfigWhenDBMissing(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	a := &App{
		Config: &config.Config{
			Work: config.WorkConfig{Profile: "analysis"},
			LLMProfiles: []config.LLMProfile{{
				Name:         "analysis",
				Provider:     "openai",
				BaseURL:      "https://api.openai.com",
				Model:        "config-model",
				SummaryModel: "config-summary",
				MaxTokens:    1024,
				Temperature:  0.7,
				APIKeyEnv:    "TEST_OPENAI_API_KEY",
			}},
		},
		DB:     db,
		Logger: logger,
	}

	profile, err := a.resolveWorkProfile()
	if err != nil {
		t.Fatalf("resolveWorkProfile: %v", err)
	}
	if profile.Model != "config-model" {
		t.Fatalf("model = %q, want config-model", profile.Model)
	}

	record, err := db.GetLLMProfile(context.Background(), "analysis")
	if err != nil {
		t.Fatalf("GetLLMProfile: %v", err)
	}
	if record == nil {
		t.Fatal("expected seeded profile in db")
	}
	if record.Model != "config-model" {
		t.Fatalf("seeded model = %q, want config-model", record.Model)
	}
}

func TestResolveWorkProfileMissingReturnsError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	a := &App{
		Config: &config.Config{
			Work: config.WorkConfig{Profile: "missing"},
		},
		DB:     db,
		Logger: logger,
	}

	_, err = a.resolveWorkProfile()
	if err == nil {
		t.Fatal("expected error for missing work profile")
	}
}

func TestRunAllowsStartupWhenWorkProfileUnavailable(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Server = config.ServerConfig{Host: "127.0.0.1", Port: 0}
	cfg.Work.Profile = "missing"

	registry := tool.NewRegistry()
	builtin.RegisterAll(registry, cfg, t.TempDir(), logger)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a := &App{
		Config:       cfg,
		DB:           db,
		Logger:       logger,
		toolRegistry: registry,
	}

	if err := a.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, ok := a.toolRegistry.GetSpec("delegate_to_work"); ok {
		t.Fatal("delegate_to_work should not be registered when work profile is unavailable")
	}
}

func TestRunRegistersDelegateToolWhenWorkProfileUsable(t *testing.T) {
	t.Setenv("TEST_OPENAI_API_KEY", "test-key")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.UpsertLLMProfile(config.LLMProfile{
		Name:         "analysis",
		Provider:     "openai",
		BaseURL:      "https://api.openai.com",
		Model:        "gpt-4o-mini",
		SummaryModel: "gpt-4o-mini",
		MaxTokens:    128,
		Temperature:  0.2,
		APIKeyEnv:    "TEST_OPENAI_API_KEY",
	}); err != nil {
		t.Fatalf("UpsertLLMProfile: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Server = config.ServerConfig{Host: "127.0.0.1", Port: 0}
	cfg.Work.Profile = "analysis"

	registry := tool.NewRegistry()
	builtin.RegisterAll(registry, cfg, t.TempDir(), logger)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a := &App{
		Config:       cfg,
		DB:           db,
		Logger:       logger,
		toolRegistry: registry,
	}

	if err := a.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, ok := a.toolRegistry.GetSpec("delegate_to_work"); !ok {
		t.Fatal("delegate_to_work should be registered when work profile is usable")
	}
	if _, ok := a.toolRegistry.GetSpec("request_decision"); !ok {
		t.Fatal("request_decision should be registered when work profile is usable")
	}
	if _, ok := a.toolRegistry.GetSpec("finish_task"); !ok {
		t.Fatal("finish_task should be registered when work profile is usable")
	}
	if _, ok := a.toolRegistry.GetSpec("list_pending_decisions"); !ok {
		t.Fatal("list_pending_decisions should be registered when work profile is usable")
	}
}

func intPtr(v int) *int { return &v }

func floatPtr(v float64) *float64 { return &v }
