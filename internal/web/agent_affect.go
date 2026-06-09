package web

import "net/http"

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
