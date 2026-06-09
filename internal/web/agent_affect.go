package web

import (
	"net/http"
	"strconv"

	"github.com/longyisang/emoagent/internal/config"
)

type agentAffectConfigRequest struct {
	AgentAffect config.AgentAffectConfig `json:"agent_affect"`
}

func (h *APIHandler) HandleGetAgentAffectConfig(w http.ResponseWriter, r *http.Request) {
	resp, err := h.app.GetAgentAffectConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *APIHandler) HandleUpdateAgentAffectConfig(w http.ResponseWriter, r *http.Request) {
	var req agentAffectConfigRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resp, err := h.app.UpdateAgentAffectConfig(r.Context(), req.AgentAffect)
	if err != nil {
		h.writeConfigMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *APIHandler) HandleGetAgentAffectProfile(w http.ResponseWriter, r *http.Request) {
	personaID := r.URL.Query().Get("persona_id")
	if personaID == "" {
		personaID = "default"
	}
	resp, err := h.app.GetAgentAffectProfile(r.Context(), personaID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *APIHandler) HandleUpdateAgentAffectProfile(w http.ResponseWriter, r *http.Request) {
	var req AgentAffectProfileResponse
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.PersonaID == "" {
		req.PersonaID = "default"
	}
	resp, err := h.app.UpdateAgentAffectProfile(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *APIHandler) HandleListAgentAffectHistory(w http.ResponseWriter, r *http.Request) {
	req := AgentAffectHistoryRequest{
		PersonaID: r.URL.Query().Get("persona_id"),
		SessionID: r.URL.Query().Get("session_id"),
		Kind:      r.URL.Query().Get("kind"),
		Limit:     queryInt(r, "limit", 30),
	}
	if req.PersonaID == "" {
		req.PersonaID = "default"
	}
	resp, err := h.app.ListAgentAffectHistory(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *APIHandler) HandleListAgentAffectPluginWrites(w http.ResponseWriter, r *http.Request) {
	req := AgentAffectPluginWritesRequest{
		PersonaID: r.URL.Query().Get("persona_id"),
		SessionID: r.URL.Query().Get("session_id"),
		PluginID:  r.URL.Query().Get("plugin_id"),
		Limit:     queryInt(r, "limit", 30),
	}
	resp, err := h.app.ListAgentAffectPluginWrites(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]AgentAffectPluginWritesResponse{"writes": resp})
}

func (h *APIHandler) HandleGetAgentAffectCurrent(w http.ResponseWriter, r *http.Request) {
	req := AgentAffectCurrentRequest{
		PersonaID: r.URL.Query().Get("persona_id"),
		SessionID: r.URL.Query().Get("session_id"),
		View:      r.URL.Query().Get("view"),
	}
	if req.PersonaID == "" {
		req.PersonaID = "default"
	}
	resp, err := h.app.GetAgentAffectCurrent(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *APIHandler) HandleEvaluateAgentAffect(w http.ResponseWriter, r *http.Request) {
	var req AgentAffectEvaluateRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.PersonaID == "" {
		req.PersonaID = "default"
	}
	resp, err := h.app.EvaluateAgentAffect(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *APIHandler) HandlePreviewAgentAffectPrompt(w http.ResponseWriter, r *http.Request) {
	var req AgentAffectPromptPreviewRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.PersonaID == "" {
		req.PersonaID = "default"
	}
	resp, err := h.app.PreviewAgentAffectPrompt(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *APIHandler) HandleResetAgentAffect(w http.ResponseWriter, r *http.Request) {
	var req AgentAffectResetRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.PersonaID == "" {
		req.PersonaID = "default"
	}
	resp, err := h.app.ResetAgentAffect(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *APIHandler) HandleSubmitAgentAffect(w http.ResponseWriter, r *http.Request) {
	var req AgentAffectSubmitRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.PersonaID == "" {
		req.PersonaID = "default"
	}
	resp, err := h.app.SubmitAgentAffect(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func queryInt(r *http.Request, key string, fallback int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func (h *APIHandler) HandleApplyAgentAffectDelta(w http.ResponseWriter, r *http.Request) {
	var req AgentAffectDeltaRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.PersonaID == "" {
		req.PersonaID = "default"
	}
	resp, err := h.app.ApplyAgentAffectDelta(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
