package web

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/longyisang/emoagent/internal/resource"
)

type ResourceChangeSetAdminApp interface {
	ListHostResourceChangeSets(context.Context, []resource.ChangeSetStatus) ([]resource.ChangeSet, error)
	GetHostResourceChangeSet(context.Context, string) (resource.ChangeSet, error)
	CancelHostResourceChangeSet(context.Context, string) (resource.ChangeSet, error)
}

func (h *APIHandler) resourceChangeSetAdminApp(w http.ResponseWriter) (ResourceChangeSetAdminApp, bool) {
	app, ok := any(h.app).(ResourceChangeSetAdminApp)
	if !ok {
		writeError(w, http.StatusNotImplemented, "host resource changeset admin API is not available")
		return nil, false
	}
	return app, true
}

func (h *APIHandler) HandleListResourceChangeSets(w http.ResponseWriter, r *http.Request) {
	app, ok := h.resourceChangeSetAdminApp(w)
	if !ok {
		return
	}
	changesets, err := app.ListHostResourceChangeSets(r.Context(), parseChangeSetStatuses(r.URL.Query().Get("status")))
	if err != nil {
		h.writeResourceChangeSetError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"changesets": changesets})
}

func (h *APIHandler) HandleGetResourceChangeSet(w http.ResponseWriter, r *http.Request) {
	app, ok := h.resourceChangeSetAdminApp(w)
	if !ok {
		return
	}
	cs, err := app.GetHostResourceChangeSet(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeResourceChangeSetError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cs)
}

func (h *APIHandler) HandleCancelResourceChangeSet(w http.ResponseWriter, r *http.Request) {
	app, ok := h.resourceChangeSetAdminApp(w)
	if !ok {
		return
	}
	cs, err := app.CancelHostResourceChangeSet(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeResourceChangeSetError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cs)
}

func parseChangeSetStatuses(raw string) []resource.ChangeSetStatus {
	var statuses []resource.ChangeSetStatus
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			statuses = append(statuses, resource.ChangeSetStatus(part))
		}
	}
	return statuses
}

func (h *APIHandler) writeResourceChangeSetError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, resource.ErrChangeSetNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case strings.Contains(err.Error(), "required"):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}
