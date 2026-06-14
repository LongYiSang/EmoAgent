package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/longyisang/emoagent/internal/promptcenter"
)

type PromptAdminApp interface {
	ListPromptComponents(context.Context, string) (promptcenter.PromptComponentsResponse, error)
	GetPromptComponent(context.Context, string, string) (promptcenter.PromptComponentDetail, error)
	UpsertPromptOverride(context.Context, promptcenter.UpsertOverrideRequest) (promptcenter.UpsertOverrideResponse, error)
	DeletePromptOverride(context.Context, promptcenter.DeleteOverrideRequest) error
	PreviewPrompt(context.Context, promptcenter.PromptPreviewRequest) (promptcenter.PromptPreviewResponse, error)
	ListPromptSnapshots(context.Context, promptcenter.PromptSnapshotListRequest) (promptcenter.PromptSnapshotListResponse, error)
	GetPromptSnapshot(context.Context, string) (promptcenter.PromptSnapshotDetail, error)
}

func (h *APIHandler) promptAdminApp(w http.ResponseWriter) (PromptAdminApp, bool) {
	app, ok := any(h.app).(PromptAdminApp)
	if !ok {
		writeError(w, http.StatusNotImplemented, "prompt center API is not available")
		return nil, false
	}
	return app, true
}

func (h *APIHandler) HandleListPromptComponents(w http.ResponseWriter, r *http.Request) {
	app, ok := h.promptAdminApp(w)
	if !ok {
		return
	}
	resp, err := app.ListPromptComponents(r.Context(), strings.TrimSpace(r.URL.Query().Get("agent_id")))
	if err != nil {
		h.writePromptCenterError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *APIHandler) HandleGetPromptComponent(w http.ResponseWriter, r *http.Request) {
	app, ok := h.promptAdminApp(w)
	if !ok {
		return
	}
	resp, err := app.GetPromptComponent(r.Context(), r.PathValue("component_id"), strings.TrimSpace(r.URL.Query().Get("agent_id")))
	if err != nil {
		h.writePromptCenterError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *APIHandler) HandleUpsertPromptOverride(w http.ResponseWriter, r *http.Request) {
	app, ok := h.promptAdminApp(w)
	if !ok {
		return
	}
	var req promptcenter.UpsertOverrideRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	resp, err := app.UpsertPromptOverride(r.Context(), req)
	if err != nil {
		h.writePromptCenterError(w, err)
		return
	}
	if !resp.OK {
		resp.OK = true
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *APIHandler) HandleDeletePromptOverride(w http.ResponseWriter, r *http.Request) {
	app, ok := h.promptAdminApp(w)
	if !ok {
		return
	}
	req := promptcenter.DeleteOverrideRequest{
		ComponentID: strings.TrimSpace(r.URL.Query().Get("component_id")),
		ScopeType:   strings.TrimSpace(r.URL.Query().Get("scope_type")),
		ScopeID:     strings.TrimSpace(r.URL.Query().Get("scope_id")),
	}
	if err := app.DeletePromptOverride(r.Context(), req); err != nil {
		h.writePromptCenterError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *APIHandler) HandlePreviewPrompt(w http.ResponseWriter, r *http.Request) {
	app, ok := h.promptAdminApp(w)
	if !ok {
		return
	}
	var req promptcenter.PromptPreviewRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	resp, err := app.PreviewPrompt(r.Context(), req)
	if err != nil {
		h.writePromptCenterError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *APIHandler) HandleListPromptSnapshots(w http.ResponseWriter, r *http.Request) {
	app, ok := h.promptAdminApp(w)
	if !ok {
		return
	}
	req := promptcenter.PromptSnapshotListRequest{
		AgentID:   strings.TrimSpace(r.URL.Query().Get("agent_id")),
		SessionID: strings.TrimSpace(r.URL.Query().Get("session_id")),
		Purpose:   strings.TrimSpace(r.URL.Query().Get("purpose")),
		Limit:     queryInt(r, "limit", 50),
	}
	resp, err := app.ListPromptSnapshots(r.Context(), req)
	if err != nil {
		h.writePromptCenterError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *APIHandler) HandleGetPromptSnapshot(w http.ResponseWriter, r *http.Request) {
	app, ok := h.promptAdminApp(w)
	if !ok {
		return
	}
	resp, err := app.GetPromptSnapshot(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writePromptCenterError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *APIHandler) writePromptCenterError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "not found"):
		writeError(w, http.StatusNotFound, message)
	case strings.Contains(message, "required"),
		strings.Contains(message, "unknown prompt component"),
		strings.Contains(message, "scope"),
		strings.Contains(message, "mode"),
		strings.Contains(message, "override_text"),
		strings.Contains(message, "agent"):
		writeError(w, http.StatusBadRequest, message)
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}
