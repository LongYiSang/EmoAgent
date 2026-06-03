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
	"github.com/longyisang/emoagent/internal/configcenter"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/progress"
	"github.com/longyisang/emoagent/internal/protocol"
	sidecarruntime "github.com/longyisang/emoagent/internal/sidecar"
	"github.com/longyisang/emoagent/internal/storage"
)

// AdminApp exposes the management operations needed by the admin API.
type AdminApp interface {
	ListLLMProviders() ([]config.LLMProvider, error)
	GetLLMProvider(id string) (*config.LLMProvider, error)
	CreateLLMProvider(provider config.LLMProvider) error
	UpdateLLMProvider(id string, provider config.LLMProvider) error
	DeleteLLMProvider(id string) error
	RefreshLLMProviderModels(id string) ([]llm.ModelInfo, error)
	GetLLMProviderModels(id string) ([]llm.ModelInfo, error)
	GetLLMProviderEnvStatus(id string) (configcenter.ProviderEnvStatus, error)
	ListAgentConfigs() ([]config.AgentConfig, error)
	GetAgentConfig(id string) (*config.AgentConfig, error)
	GetActiveAgentConfig() (*config.AgentConfig, bool, error)
	CreateAgentConfig(agent config.AgentConfig) error
	UpdateAgentConfig(id string, agent config.AgentConfig) error
	ActivateAgentConfig(id string) error
	DeleteAgentConfig(id string) error
	ListPersonas() map[string]*config.Persona
	GetPersona(name string) (*config.Persona, bool)
	CreatePersona(key string, p *config.Persona) error
	UpdatePersona(key string, p *config.Persona) error
	DeletePersona(key string) error
	GetProgressPhrases(key string) (map[string][]string, error)
	UpdateProgressPhrases(key string, phrases map[string][]string) error
	ListSessions(ctx context.Context, persona string, limit int) ([]storage.SessionSummary, error)
	GetLatestSession(ctx context.Context, persona string) (*storage.SessionSummary, error)
	GetSessionDetail(ctx context.Context, id string) (*storage.SessionRecord, []storage.MessageRecord, error)
	DeleteSession(ctx context.Context, id string) error
	ListSessionApprovals(ctx context.Context, sessionID string) ([]protocol.ApprovalRequest, error)
	QueueMemoryExtraction(ctx context.Context, req MemoryExtractionRequest) (MemoryExtractionQueueResponse, error)
	ListMemoryExtractions(ctx context.Context, req MemoryExtractionListRequest) ([]storage.MemoryExtractionJob, error)
	ListMemorySegments(ctx context.Context, sessionID string) ([]storage.MemorySegment, error)
	GetChatSettings() config.ChatConfig
	UpdateChatSettings(settings config.ChatConfig) error
	GetEffectiveConfig(ctx context.Context) (configcenter.EffectiveConfig, error)
	ValidateConfig(ctx context.Context, req configcenter.ValidateRequest) (configcenter.ValidateResponse, error)
	ListConfigIssues(ctx context.Context) ([]configcenter.ConfigIssue, error)
	GetMemoryConfig(ctx context.Context) (configcenter.MemoryConfigResponse, error)
	UpdateMemoryConfig(ctx context.Context, memory config.MemoryConfig) (configcenter.EffectiveConfig, error)
	GetMemoryFeatures(ctx context.Context) (configcenter.MemoryConfigResponse, error)
	UpdateMemoryFeatures(ctx context.Context, memory config.MemoryConfig) (configcenter.EffectiveConfig, error)
	GetSidecarStatus(ctx context.Context) (sidecarruntime.Status, error)
	StartSidecar(ctx context.Context) (sidecarruntime.Status, error)
	StopSidecar(ctx context.Context) (sidecarruntime.Status, error)
	RestartSidecar(ctx context.Context) (sidecarruntime.Status, error)
	GetSidecarGeneratedConfig(ctx context.Context) (string, error)
	GetSidecarLogs(ctx context.Context, maxBytes int) (string, error)
}

type APIHandler struct {
	app    AdminApp
	logger *slog.Logger
}

type llmProvidersResponse struct {
	Providers []config.LLMProvider `json:"providers"`
}

type llmProviderPresetsResponse struct {
	Presets []llm.ProviderPreset `json:"presets"`
}

type agentConfigsResponse struct {
	ActiveID string               `json:"active_id"`
	Configs  []config.AgentConfig `json:"configs"`
}

type providerModelsResponse struct {
	Models []llm.ModelInfo `json:"models"`
}

type personaSummary struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Tone        string `json:"tone"`
}

type personasResponse struct {
	Personas []personaSummary `json:"personas"`
}

type chatSettingsResponse struct {
	RealtimeStreaming bool `json:"realtime_streaming"`
}

type chatSettingsRequest struct {
	RealtimeStreaming bool `json:"realtime_streaming"`
}

type personaDetailResponse struct {
	Key                 string              `json:"key"`
	Name                string              `json:"name"`
	Description         string              `json:"description"`
	SystemPrompt        string              `json:"system_prompt"`
	Tone                string              `json:"tone"`
	Quirks              []string            `json:"quirks"`
	Greeting            string              `json:"greeting"`
	WorkProgressPhrases map[string][]string `json:"work_progress_phrases"`
}

type personaRequest struct {
	Key                 string              `json:"key"`
	Name                string              `json:"name"`
	Description         string              `json:"description"`
	SystemPrompt        string              `json:"system_prompt"`
	Tone                string              `json:"tone"`
	Quirks              []string            `json:"quirks"`
	Greeting            string              `json:"greeting"`
	WorkProgressPhrases map[string][]string `json:"work_progress_phrases"`
}

type progressPhrasesResponse struct {
	Phrases map[string][]string `json:"phrases"`
}

type progressPhrasesRequest struct {
	Phrases map[string][]string `json:"phrases"`
}

type MemoryExtractionRequest struct {
	SessionID string `json:"session_id"`
	SegmentID string `json:"segment_id"`
	PersonaID string `json:"persona_id"`
	Scope     string `json:"scope"`
	Force     bool   `json:"force"`
	Mode      string `json:"mode"`
}

type MemoryExtractionListRequest struct {
	SessionID string
	SegmentID string
	Status    string
	Limit     int
}

type MemoryExtractionQueueResponse struct {
	Status        string                        `json:"status"`
	EnqueuedCount int                           `json:"enqueued_count"`
	SkippedCount  int                           `json:"skipped_count"`
	Jobs          []storage.MemoryExtractionJob `json:"jobs"`
}

type memoryConfigRequest struct {
	Memory config.MemoryConfig `json:"memory"`
}

type sidecarGeneratedConfigResponse struct {
	Config string `json:"config"`
}

type sidecarLogsResponse struct {
	Logs string `json:"logs"`
}

func NewAPIHandler(app AdminApp, logger *slog.Logger) *APIHandler {
	return &APIHandler{app: app, logger: logger}
}

func (h *APIHandler) HandleListLLMProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := h.app.ListLLMProviders()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list llm providers")
		return
	}
	writeJSON(w, http.StatusOK, llmProvidersResponse{Providers: providers})
}

func (h *APIHandler) HandleListLLMProviderPresets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, llmProviderPresetsResponse{Presets: llm.ListProviderPresets()})
}

func (h *APIHandler) HandleGetLLMProvider(w http.ResponseWriter, r *http.Request) {
	provider, err := h.app.GetLLMProvider(r.PathValue("id"))
	if err != nil {
		h.writeLLMProviderError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, provider)
}

func (h *APIHandler) HandleCreateLLMProvider(w http.ResponseWriter, r *http.Request) {
	var provider config.LLMProvider
	if err := readJSON(r, &provider); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	normalizeProvider(&provider)
	if err := h.app.CreateLLMProvider(provider); err != nil {
		h.writeLLMProviderError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

func (h *APIHandler) HandleUpdateLLMProvider(w http.ResponseWriter, r *http.Request) {
	var provider config.LLMProvider
	if err := readJSON(r, &provider); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	normalizeProvider(&provider)
	if err := h.app.UpdateLLMProvider(r.PathValue("id"), provider); err != nil {
		h.writeLLMProviderError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *APIHandler) HandleDeleteLLMProvider(w http.ResponseWriter, r *http.Request) {
	if err := h.app.DeleteLLMProvider(r.PathValue("id")); err != nil {
		h.writeLLMProviderError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *APIHandler) HandleRefreshLLMProviderModels(w http.ResponseWriter, r *http.Request) {
	models, err := h.app.RefreshLLMProviderModels(r.PathValue("id"))
	if err != nil {
		h.writeLLMProviderError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, providerModelsResponse{Models: models})
}

func (h *APIHandler) HandleGetLLMProviderModels(w http.ResponseWriter, r *http.Request) {
	models, err := h.app.GetLLMProviderModels(r.PathValue("id"))
	if err != nil {
		h.writeLLMProviderError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, providerModelsResponse{Models: models})
}

func (h *APIHandler) HandleGetLLMProviderEnvStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.app.GetLLMProviderEnvStatus(r.PathValue("id"))
	if err != nil {
		h.writeLLMProviderError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *APIHandler) HandleTestProvider(w http.ResponseWriter, r *http.Request) {
	provider, err := h.app.GetLLMProvider(r.PathValue("id"))
	if err != nil {
		h.writeLLMProviderError(w, err)
		return
	}
	status, err := h.app.GetLLMProviderEnvStatus(r.PathValue("id"))
	if err != nil {
		h.writeLLMProviderError(w, err)
		return
	}
	ok := !provider.Enabled || status.APIKeyEnv == "" || status.Present
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          ok,
		"provider_id": provider.ID,
		"env":         status,
	})
}

func (h *APIHandler) HandleGetConfigEffective(w http.ResponseWriter, r *http.Request) {
	effective, err := h.app.GetEffectiveConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build effective config")
		return
	}
	writeJSON(w, http.StatusOK, effective)
}

func (h *APIHandler) HandleValidateConfig(w http.ResponseWriter, r *http.Request) {
	var req configcenter.ValidateRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
	}
	resp, err := h.app.ValidateConfig(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to validate config")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *APIHandler) HandleListConfigIssues(w http.ResponseWriter, r *http.Request) {
	issues, err := h.app.ListConfigIssues(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list config issues")
		return
	}
	writeJSON(w, http.StatusOK, map[string][]configcenter.ConfigIssue{"issues": issues})
}

func (h *APIHandler) HandleGetMemoryConfig(w http.ResponseWriter, r *http.Request) {
	resp, err := h.app.GetMemoryConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get memory config")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *APIHandler) HandleUpdateMemoryConfig(w http.ResponseWriter, r *http.Request) {
	memory, ok := h.readMemoryConfigRequest(w, r)
	if !ok {
		return
	}
	effective, err := h.app.UpdateMemoryConfig(r.Context(), memory)
	if err != nil {
		h.writeConfigMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, effective)
}

func (h *APIHandler) HandleGetMemoryFeatures(w http.ResponseWriter, r *http.Request) {
	resp, err := h.app.GetMemoryFeatures(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get memory features")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *APIHandler) HandleUpdateMemoryFeatures(w http.ResponseWriter, r *http.Request) {
	memory, ok := h.readMemoryConfigRequest(w, r)
	if !ok {
		return
	}
	effective, err := h.app.UpdateMemoryFeatures(r.Context(), memory)
	if err != nil {
		h.writeConfigMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, effective)
}

func (h *APIHandler) HandleGetSidecarStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.app.GetSidecarStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get sidecar status")
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *APIHandler) HandleStartSidecar(w http.ResponseWriter, r *http.Request) {
	status, err := h.app.StartSidecar(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *APIHandler) HandleStopSidecar(w http.ResponseWriter, r *http.Request) {
	status, err := h.app.StopSidecar(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to stop sidecar")
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *APIHandler) HandleRestartSidecar(w http.ResponseWriter, r *http.Request) {
	status, err := h.app.RestartSidecar(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *APIHandler) HandleGetSidecarGeneratedConfig(w http.ResponseWriter, r *http.Request) {
	body, err := h.app.GetSidecarGeneratedConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to render sidecar generated config")
		return
	}
	writeJSON(w, http.StatusOK, sidecarGeneratedConfigResponse{Config: body})
}

func (h *APIHandler) HandleGetSidecarLogs(w http.ResponseWriter, r *http.Request) {
	maxBytes := 65536
	if raw := strings.TrimSpace(r.URL.Query().Get("max_bytes")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			maxBytes = n
		}
	}
	logs, err := h.app.GetSidecarLogs(r.Context(), maxBytes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read sidecar logs")
		return
	}
	writeJSON(w, http.StatusOK, sidecarLogsResponse{Logs: logs})
}

func (h *APIHandler) readMemoryConfigRequest(w http.ResponseWriter, r *http.Request) (config.MemoryConfig, bool) {
	var req memoryConfigRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return config.MemoryConfig{}, false
	}
	return req.Memory, true
}

func (h *APIHandler) writeConfigMutationError(w http.ResponseWriter, err error) {
	var validation *configcenter.ValidationError
	if errors.As(err, &validation) {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":  err.Error(),
			"issues": validation.Issues,
		})
		return
	}
	writeError(w, http.StatusInternalServerError, "failed to update config")
}

func (h *APIHandler) HandleListAgentConfigs(w http.ResponseWriter, r *http.Request) {
	configs, err := h.app.ListAgentConfigs()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent configs")
		return
	}
	activeID := ""
	if active, ok, err := h.app.GetActiveAgentConfig(); err == nil && ok && active != nil {
		activeID = active.ID
	}
	writeJSON(w, http.StatusOK, agentConfigsResponse{ActiveID: activeID, Configs: configs})
}

func (h *APIHandler) HandleGetActiveAgentConfig(w http.ResponseWriter, r *http.Request) {
	active, ok, err := h.app.GetActiveAgentConfig()
	if err != nil {
		h.writeAgentConfigError(w, err)
		return
	}
	if !ok || active == nil {
		writeError(w, http.StatusNotFound, "agent config not found")
		return
	}
	writeJSON(w, http.StatusOK, active)
}

func (h *APIHandler) HandleGetAgentConfig(w http.ResponseWriter, r *http.Request) {
	agent, err := h.app.GetAgentConfig(r.PathValue("id"))
	if err != nil {
		h.writeAgentConfigError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, agent)
}

func (h *APIHandler) HandleCreateAgentConfig(w http.ResponseWriter, r *http.Request) {
	var agent config.AgentConfig
	if err := readJSON(r, &agent); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.app.CreateAgentConfig(agent); err != nil {
		h.writeAgentConfigError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

func (h *APIHandler) HandleUpdateAgentConfig(w http.ResponseWriter, r *http.Request) {
	var agent config.AgentConfig
	if err := readJSON(r, &agent); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.app.UpdateAgentConfig(r.PathValue("id"), agent); err != nil {
		h.writeAgentConfigError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *APIHandler) HandleActivateAgentConfig(w http.ResponseWriter, r *http.Request) {
	if err := h.app.ActivateAgentConfig(r.PathValue("id")); err != nil {
		h.writeAgentConfigError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *APIHandler) HandleDeleteAgentConfig(w http.ResponseWriter, r *http.Request) {
	if err := h.app.DeleteAgentConfig(r.PathValue("id")); err != nil {
		h.writeAgentConfigError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *APIHandler) HandleGetChatSettings(w http.ResponseWriter, r *http.Request) {
	settings := h.app.GetChatSettings()
	writeJSON(w, http.StatusOK, chatSettingsResponse{
		RealtimeStreaming: settings.RealtimeStreaming,
	})
}

func (h *APIHandler) HandleUpdateChatSettings(w http.ResponseWriter, r *http.Request) {
	var req chatSettingsRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	settings := config.ChatConfig{RealtimeStreaming: req.RealtimeStreaming}
	if err := h.app.UpdateChatSettings(settings); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update chat settings")
		return
	}
	writeJSON(w, http.StatusOK, chatSettingsResponse{
		RealtimeStreaming: settings.RealtimeStreaming,
	})
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
		Key:                 key,
		Name:                persona.Name,
		Description:         persona.Description,
		SystemPrompt:        persona.SystemPrompt,
		Tone:                persona.Tone,
		Quirks:              append([]string(nil), persona.Quirks...),
		Greeting:            persona.Greeting,
		WorkProgressPhrases: cloneProgressPhrases(persona.WorkProgressPhrases),
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
		Name:                firstNonEmpty(strings.TrimSpace(req.Name), key),
		Description:         strings.TrimSpace(req.Description),
		SystemPrompt:        req.SystemPrompt,
		Tone:                strings.TrimSpace(req.Tone),
		Quirks:              normalizeQuirks(req.Quirks),
		Greeting:            req.Greeting,
		WorkProgressPhrases: normalizeProgressPhrases(req.WorkProgressPhrases),
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
		Name:                strings.TrimSpace(req.Name),
		Description:         strings.TrimSpace(req.Description),
		SystemPrompt:        req.SystemPrompt,
		Tone:                strings.TrimSpace(req.Tone),
		Quirks:              normalizeQuirks(req.Quirks),
		Greeting:            req.Greeting,
		WorkProgressPhrases: normalizeProgressPhrases(req.WorkProgressPhrases),
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

func (h *APIHandler) HandleGetProgressPhrases(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("name")
	phrases, err := h.app.GetProgressPhrases(key)
	if err != nil {
		h.writePersonaError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, progressPhrasesResponse{Phrases: cloneProgressPhrases(phrases)})
}

func (h *APIHandler) HandleUpdateProgressPhrases(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("name")
	var req progressPhrasesRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.app.UpdateProgressPhrases(key, normalizeProgressPhrases(req.Phrases)); err != nil {
		h.writePersonaError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *APIHandler) HandleGetProgressPhrasesDefaults(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, progressPhrasesResponse{Phrases: cloneProgressPhrases(progress.DefaultTemplates)})
}

func (h *APIHandler) HandleListSessions(w http.ResponseWriter, r *http.Request) {
	persona := strings.TrimSpace(r.URL.Query().Get("persona"))
	limit, ok := parsePositiveLimit(r, 20)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid limit")
		return
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

func (h *APIHandler) HandleListSessionApprovals(w http.ResponseWriter, r *http.Request) {
	approvals, err := h.app.ListSessionApprovals(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeSessionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"approvals": approvals})
}

func (h *APIHandler) HandleQueueMemoryExtraction(w http.ResponseWriter, r *http.Request) {
	var req MemoryExtractionRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	normalizeMemoryExtractionRequest(&req)
	resp, err := h.app.QueueMemoryExtraction(r.Context(), req)
	if err != nil {
		h.writeMemoryExtractionError(w, err)
		return
	}
	if resp.Status == "" {
		resp.Status = "queued"
	}
	writeJSON(w, http.StatusAccepted, resp)
}

func (h *APIHandler) HandleListMemoryExtractions(w http.ResponseWriter, r *http.Request) {
	limit, ok := parsePositiveLimit(r, 20)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid limit")
		return
	}
	jobs, err := h.app.ListMemoryExtractions(r.Context(), MemoryExtractionListRequest{
		SessionID: strings.TrimSpace(r.URL.Query().Get("session_id")),
		SegmentID: strings.TrimSpace(r.URL.Query().Get("segment_id")),
		Status:    strings.TrimSpace(r.URL.Query().Get("status")),
		Limit:     limit,
	})
	if err != nil {
		h.writeMemoryExtractionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

func (h *APIHandler) HandleListMemorySegments(w http.ResponseWriter, r *http.Request) {
	segments, err := h.app.ListMemorySegments(r.Context(), strings.TrimSpace(r.URL.Query().Get("session_id")))
	if err != nil {
		h.writeMemoryExtractionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"segments": segments})
}

func (h *APIHandler) writeLLMProviderError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, apperrors.ErrLLMProviderExists):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, apperrors.ErrLLMProviderNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, apperrors.ErrLLMProviderInUse),
		isLLMProviderValidationError(err):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		h.logger.Error("llm provider internal error", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func (h *APIHandler) writeAgentConfigError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, apperrors.ErrAgentConfigExists):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, apperrors.ErrAgentConfigNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, apperrors.ErrCannotDeleteActiveAgentConfig),
		errors.Is(err, apperrors.ErrCannotDeleteLastAgentConfig),
		isAgentConfigValidationError(err):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		h.logger.Error("agent config internal error", "error", err)
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

func (h *APIHandler) writeMemoryExtractionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, apperrors.ErrSessionNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case isMemoryExtractionValidationError(err):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		h.logger.Error("memory extraction internal error", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func isLLMProviderValidationError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	switch {
	case strings.HasSuffix(message, " environment variable not set"):
		return true
	case message == "id is required",
		message == "name is required",
		message == "base_url is required",
		message == "api_key_env is required":
		return true
	case strings.HasPrefix(message, "unsupported protocol:"),
		strings.HasPrefix(message, "unsupported model_discovery:"):
		return true
	default:
		return false
	}
}

func isAgentConfigValidationError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "provider_id is required"),
		strings.Contains(message, "model is required"),
		strings.Contains(message, "persona_key is required"),
		strings.Contains(message, "params.max_tokens must be >= 0"),
		strings.Contains(message, "unsupported key"):
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

func isMemoryExtractionValidationError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "session_id") ||
		strings.Contains(message, "segment_id") ||
		strings.Contains(message, "scope") ||
		strings.Contains(message, "mode") ||
		strings.Contains(message, "memory extraction")
}

func normalizeMemoryExtractionRequest(req *MemoryExtractionRequest) {
	if req == nil {
		return
	}
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.SegmentID = strings.TrimSpace(req.SegmentID)
	req.PersonaID = strings.TrimSpace(req.PersonaID)
	req.Scope = strings.TrimSpace(req.Scope)
	req.Mode = strings.TrimSpace(req.Mode)
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

func normalizeProgressPhrases(phrases map[string][]string) map[string][]string {
	if len(phrases) == 0 {
		return map[string][]string{}
	}
	out := make(map[string][]string)
	for rawKey, values := range phrases {
		key := strings.TrimSpace(rawKey)
		if key == "" {
			continue
		}
		cleanValues := make([]string, 0, len(values))
		for _, value := range values {
			trimmed := strings.TrimSpace(value)
			if trimmed != "" {
				cleanValues = append(cleanValues, trimmed)
			}
		}
		if len(cleanValues) > 0 {
			out[key] = cleanValues
		}
	}
	if len(out) == 0 {
		return map[string][]string{}
	}
	return out
}

func normalizeProvider(provider *config.LLMProvider) {
	if provider == nil {
		return
	}
	provider.ID = strings.TrimSpace(provider.ID)
	provider.Name = strings.TrimSpace(provider.Name)
	provider.PresetID = strings.TrimSpace(provider.PresetID)
	provider.Protocol = strings.TrimSpace(provider.Protocol)
	provider.BaseURL = strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
	provider.APIKeyEnv = strings.TrimSpace(provider.APIKeyEnv)
	provider.ModelDiscovery = strings.TrimSpace(provider.ModelDiscovery)
	if provider.ModelDiscovery == "" {
		provider.ModelDiscovery = "manual"
	}
	provider.Capabilities = config.NormalizeProviderCapabilities(provider.Capabilities)
}

func cloneProgressPhrases(src map[string][]string) map[string][]string {
	if src == nil {
		return map[string][]string{}
	}
	dst := make(map[string][]string, len(src))
	for key, values := range src {
		dst[key] = append([]string(nil), values...)
	}
	return dst
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func parsePositiveLimit(r *http.Request, defaultLimit int) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return defaultLimit, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return 0, false
	}
	return parsed, true
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
