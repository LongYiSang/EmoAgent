package web

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/longyisang/emoagent/internal/apperrors"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/storage"
)

// AdminApp exposes the management operations needed by the admin API.
type AdminApp interface {
	ListLLMProfiles() ([]config.LLMProfile, error)
	GetLLMProfile(id string) (*config.LLMProfile, error)
	GetActiveLLMProfile() (*config.LLMProfile, bool)
	CreateLLMProfile(profile config.LLMProfile) error
	UpdateLLMProfile(id string, profile config.LLMProfile) error
	ActivateLLMProfile(id string) error
	DeleteLLMProfile(id string) error
	ListPersonas() map[string]*config.Persona
	GetPersona(name string) (*config.Persona, bool)
	CreatePersona(key string, p *config.Persona) error
	UpdatePersona(key string, p *config.Persona) error
	DeletePersona(key string) error
	ActivatePersona(key string) error
	GetDefaultPersonaName() string
	ListSessions(ctx context.Context, persona string, limit int) ([]storage.SessionSummary, error)
	GetLatestSession(ctx context.Context, persona string) (*storage.SessionSummary, error)
	GetSessionDetail(ctx context.Context, id string) (*storage.SessionRecord, []storage.MessageRecord, error)
	DeleteSession(ctx context.Context, id string) error
}

type APIHandler struct {
	app    AdminApp
	logger *slog.Logger
}

type llmProfileResponse struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Provider            string   `json:"provider"`
	BaseURL             string   `json:"base_url"`
	APIKeyEnv           string   `json:"api_key_env"`
	Model               string   `json:"model"`
	SummaryModel        string   `json:"summary_model"`
	MaxTokens           int      `json:"max_tokens"`
	Temperature         float64  `json:"temperature"`
	InputBudgetTokens   *int     `json:"input_budget_tokens"`
	SoftCompactRatio    *float64 `json:"soft_compact_ratio"`
	HardCompactRatio    *float64 `json:"hard_compact_ratio"`
	ReserveOutputTokens *int     `json:"reserve_output_tokens"`
}

type llmProfilesResponse struct {
	ActiveID string               `json:"active_id"`
	Profiles []llmProfileResponse `json:"profiles"`
}

type personaSummary struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Tone        string `json:"tone"`
}

type personasResponse struct {
	Default  string           `json:"default"`
	Personas []personaSummary `json:"personas"`
}

type personaDetailResponse struct {
	Key          string   `json:"key"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	SystemPrompt string   `json:"system_prompt"`
	Tone         string   `json:"tone"`
	Quirks       []string `json:"quirks"`
	Greeting     string   `json:"greeting"`
}

type llmProfileRequest struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Provider            string   `json:"provider"`
	BaseURL             string   `json:"base_url"`
	APIKeyEnv           string   `json:"api_key_env"`
	Model               string   `json:"model"`
	SummaryModel        string   `json:"summary_model"`
	MaxTokens           int      `json:"max_tokens"`
	Temperature         float64  `json:"temperature"`
	InputBudgetTokens   *int     `json:"input_budget_tokens"`
	SoftCompactRatio    *float64 `json:"soft_compact_ratio"`
	HardCompactRatio    *float64 `json:"hard_compact_ratio"`
	ReserveOutputTokens *int     `json:"reserve_output_tokens"`
}

type personaRequest struct {
	Key          string   `json:"key"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	SystemPrompt string   `json:"system_prompt"`
	Tone         string   `json:"tone"`
	Quirks       []string `json:"quirks"`
	Greeting     string   `json:"greeting"`
}

func NewAPIHandler(app AdminApp, logger *slog.Logger) *APIHandler {
	return &APIHandler{app: app, logger: logger}
}

func (h *APIHandler) HandleListLLMProfiles(w http.ResponseWriter, r *http.Request) {
	profiles, err := h.app.ListLLMProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list llm profiles")
		return
	}

	activeID := ""
	if active, ok := h.app.GetActiveLLMProfile(); ok && active != nil {
		activeID = active.Name
	}

	items := make([]llmProfileResponse, 0, len(profiles))
	for _, profile := range profiles {
		items = append(items, toLLMProfileResponse(profile))
	}

	writeJSON(w, http.StatusOK, llmProfilesResponse{
		ActiveID: activeID,
		Profiles: items,
	})
}

func (h *APIHandler) HandleGetLLMProfile(w http.ResponseWriter, r *http.Request) {
	profile, err := h.app.GetLLMProfile(r.PathValue("id"))
	if err != nil {
		h.writeLLMProfileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toLLMProfileResponse(*profile))
}

func (h *APIHandler) HandleCreateLLMProfile(w http.ResponseWriter, r *http.Request) {
	var req llmProfileRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	profile := config.LLMProfile{
		Name:                firstNonEmpty(strings.TrimSpace(req.ID), strings.TrimSpace(req.Name)),
		Provider:            strings.TrimSpace(req.Provider),
		BaseURL:             strings.TrimSpace(req.BaseURL),
		APIKeyEnv:           strings.TrimSpace(req.APIKeyEnv),
		Model:               strings.TrimSpace(req.Model),
		SummaryModel:        strings.TrimSpace(req.SummaryModel),
		MaxTokens:           req.MaxTokens,
		Temperature:         req.Temperature,
		InputBudgetTokens:   req.InputBudgetTokens,
		SoftCompactRatio:    req.SoftCompactRatio,
		HardCompactRatio:    req.HardCompactRatio,
		ReserveOutputTokens: req.ReserveOutputTokens,
	}

	if err := h.app.CreateLLMProfile(profile); err != nil {
		h.writeLLMProfileError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

func (h *APIHandler) HandleUpdateLLMProfile(w http.ResponseWriter, r *http.Request) {
	var req llmProfileRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	id := r.PathValue("id")
	profile := config.LLMProfile{
		Name:                id,
		Provider:            strings.TrimSpace(req.Provider),
		BaseURL:             strings.TrimSpace(req.BaseURL),
		APIKeyEnv:           strings.TrimSpace(req.APIKeyEnv),
		Model:               strings.TrimSpace(req.Model),
		SummaryModel:        strings.TrimSpace(req.SummaryModel),
		MaxTokens:           req.MaxTokens,
		Temperature:         req.Temperature,
		InputBudgetTokens:   req.InputBudgetTokens,
		SoftCompactRatio:    req.SoftCompactRatio,
		HardCompactRatio:    req.HardCompactRatio,
		ReserveOutputTokens: req.ReserveOutputTokens,
	}

	if err := h.app.UpdateLLMProfile(id, profile); err != nil {
		h.writeLLMProfileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *APIHandler) HandleActivateLLMProfile(w http.ResponseWriter, r *http.Request) {
	if err := h.app.ActivateLLMProfile(r.PathValue("id")); err != nil {
		h.writeLLMProfileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *APIHandler) HandleDeleteLLMProfile(w http.ResponseWriter, r *http.Request) {
	if err := h.app.DeleteLLMProfile(r.PathValue("id")); err != nil {
		h.writeLLMProfileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *APIHandler) HandleListPersonas(w http.ResponseWriter, r *http.Request) {
	personas := h.app.ListPersonas()
	result := make([]personaSummary, 0, len(personas))
	for key, p := range personas {
		result = append(result, personaSummary{
			Key:         key,
			Name:        p.Name,
			Description: p.Description,
			Tone:        p.Tone,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })

	writeJSON(w, http.StatusOK, personasResponse{
		Default:  h.app.GetDefaultPersonaName(),
		Personas: result,
	})
}

func (h *APIHandler) HandleGetPersona(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("name")
	persona, ok := h.app.GetPersona(key)
	if !ok || persona == nil {
		writeError(w, http.StatusNotFound, "persona not found")
		return
	}

	writeJSON(w, http.StatusOK, personaDetailResponse{
		Key:          key,
		Name:         persona.Name,
		Description:  persona.Description,
		SystemPrompt: persona.SystemPrompt,
		Tone:         persona.Tone,
		Quirks:       append([]string(nil), persona.Quirks...),
		Greeting:     persona.Greeting,
	})
}

func (h *APIHandler) HandleCreatePersona(w http.ResponseWriter, r *http.Request) {
	var req personaRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	key := firstNonEmpty(strings.TrimSpace(req.Key), strings.TrimSpace(req.Name))
	if key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}

	if err := h.app.CreatePersona(key, &config.Persona{
		Name:         firstNonEmpty(strings.TrimSpace(req.Name), key),
		Description:  strings.TrimSpace(req.Description),
		SystemPrompt: req.SystemPrompt,
		Tone:         strings.TrimSpace(req.Tone),
		Quirks:       normalizeQuirks(req.Quirks),
		Greeting:     req.Greeting,
	}); err != nil {
		h.writePersonaError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

func (h *APIHandler) HandleUpdatePersona(w http.ResponseWriter, r *http.Request) {
	var req personaRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	key := r.PathValue("name")
	if err := h.app.UpdatePersona(key, &config.Persona{
		Name:         strings.TrimSpace(req.Name),
		Description:  strings.TrimSpace(req.Description),
		SystemPrompt: req.SystemPrompt,
		Tone:         strings.TrimSpace(req.Tone),
		Quirks:       normalizeQuirks(req.Quirks),
		Greeting:     req.Greeting,
	}); err != nil {
		h.writePersonaError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *APIHandler) HandleDeletePersona(w http.ResponseWriter, r *http.Request) {
	if err := h.app.DeletePersona(r.PathValue("name")); err != nil {
		h.writePersonaError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *APIHandler) HandleActivatePersona(w http.ResponseWriter, r *http.Request) {
	if err := h.app.ActivatePersona(r.PathValue("name")); err != nil {
		h.writePersonaError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *APIHandler) HandleListSessions(w http.ResponseWriter, r *http.Request) {
	persona := strings.TrimSpace(r.URL.Query().Get("persona"))
	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = parsed
	}

	sessions, err := h.app.ListSessions(r.Context(), persona, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sessions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"sessions": sessions})
}

func (h *APIHandler) HandleGetLatestSession(w http.ResponseWriter, r *http.Request) {
	persona := strings.TrimSpace(r.URL.Query().Get("persona"))
	session, err := h.app.GetLatestSession(r.Context(), persona)
	if err != nil {
		h.writeSessionError(w, err)
		return
	}
	if session == nil {
		writeError(w, http.StatusNotFound, "no sessions found")
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (h *APIHandler) HandleGetSession(w http.ResponseWriter, r *http.Request) {
	session, messages, err := h.app.GetSessionDetail(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeSessionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":         session.ID,
		"persona":    session.Persona,
		"title":      session.Title,
		"created_at": session.CreatedAt,
		"updated_at": session.UpdatedAt,
		"messages":   messages,
	})
}

func (h *APIHandler) HandleDeleteSession(w http.ResponseWriter, r *http.Request) {
	if err := h.app.DeleteSession(r.Context(), r.PathValue("id")); err != nil {
		h.writeSessionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *APIHandler) writeLLMProfileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, apperrors.ErrLLMProfileExists):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, apperrors.ErrLLMProfileNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, apperrors.ErrCannotDeleteActiveLLMProfile),
		errors.Is(err, apperrors.ErrCannotDeleteLastLLMProfile),
		isLLMProfileValidationError(err):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		h.logger.Error("llm profile internal error", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func (h *APIHandler) writePersonaError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, apperrors.ErrPersonaExists):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, apperrors.ErrPersonaNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, apperrors.ErrCannotDeleteDefault),
		isPersonaValidationError(err):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		h.logger.Error("persona internal error", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func (h *APIHandler) writeSessionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, apperrors.ErrSessionNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		h.logger.Error("session internal error", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func isLLMProfileValidationError(err error) bool {
	if err == nil {
		return false
	}

	message := err.Error()
	switch {
	case strings.HasSuffix(message, " environment variable not set"):
		return true
	case strings.HasPrefix(message, "unsupported provider:"):
		return true
	case message == "name is required":
		return true
	case message == "base_url is required":
		return true
	case message == "model is required":
		return true
	case message == "max_tokens must be greater than 0":
		return true
	case message == "temperature must be between 0 and 2":
		return true
	case message == "input_budget_tokens must be > 0":
		return true
	case message == "reserve_output_tokens must be > 0":
		return true
	case message == "soft_compact_ratio must be between 0 and 1":
		return true
	case message == "hard_compact_ratio must be between 0 and 1":
		return true
	case message == "soft_compact_ratio must be < hard_compact_ratio":
		return true
	default:
		return false
	}
}

func isPersonaValidationError(err error) bool {
	if err == nil {
		return false
	}

	message := err.Error()
	switch {
	case message == "persona is required":
		return true
	case message == "persona key is required":
		return true
	case strings.HasPrefix(message, "persona key must"):
		return true
	default:
		return false
	}
}

func normalizeQuirks(quirks []string) []string {
	result := make([]string, 0, len(quirks))
	for _, quirk := range quirks {
		trimmed := strings.TrimSpace(quirk)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func toLLMProfileResponse(profile config.LLMProfile) llmProfileResponse {
	return llmProfileResponse{
		ID:                  profile.Name,
		Name:                profile.Name,
		Provider:            profile.Provider,
		BaseURL:             profile.BaseURL,
		APIKeyEnv:           profile.APIKeyEnv,
		Model:               profile.Model,
		SummaryModel:        profile.SummaryModel,
		MaxTokens:           profile.MaxTokens,
		Temperature:         profile.Temperature,
		InputBudgetTokens:   profile.InputBudgetTokens,
		SoftCompactRatio:    profile.SoftCompactRatio,
		HardCompactRatio:    profile.HardCompactRatio,
		ReserveOutputTokens: profile.ReserveOutputTokens,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func readJSON(r *http.Request, target interface{}) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
