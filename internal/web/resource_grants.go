package web

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/longyisang/emoagent/internal/resource"
)

type ResourceGrantAdminApp interface {
	ListResourceGrants(context.Context, resource.GrantListFilter) ([]resource.GrantEnvelope, error)
	RevokeResourceGrant(context.Context, string) (resource.GrantEnvelope, error)
}

func (h *APIHandler) resourceGrantAdminApp(w http.ResponseWriter) (ResourceGrantAdminApp, bool) {
	app, ok := any(h.app).(ResourceGrantAdminApp)
	if !ok {
		writeError(w, http.StatusNotImplemented, "resource grant admin API is not available")
		return nil, false
	}
	return app, true
}

func (h *APIHandler) HandleListResourceGrants(w http.ResponseWriter, r *http.Request) {
	app, ok := h.resourceGrantAdminApp(w)
	if !ok {
		return
	}
	filter := resource.GrantListFilter{
		Status:     resource.GrantStatus(strings.TrimSpace(r.URL.Query().Get("status"))),
		Capability: strings.TrimSpace(r.URL.Query().Get("capability")),
	}
	principalKind := strings.TrimSpace(r.URL.Query().Get("principal_kind"))
	principalID := strings.TrimSpace(r.URL.Query().Get("principal_id"))
	if principalKind != "" || principalID != "" {
		if principalKind == "" || principalID == "" {
			writeError(w, http.StatusBadRequest, "principal_kind and principal_id are both required")
			return
		}
		filter.Principal = &resource.PrincipalRef{Kind: resource.PrincipalKind(principalKind), ID: principalID}
	}
	grants, err := app.ListResourceGrants(r.Context(), filter)
	if err != nil {
		h.writeResourceGrantError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"grants": grants})
}

func (h *APIHandler) HandleRevokeResourceGrant(w http.ResponseWriter, r *http.Request) {
	app, ok := h.resourceGrantAdminApp(w)
	if !ok {
		return
	}
	grant, err := app.RevokeResourceGrant(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeResourceGrantError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, grant)
}

func (h *APIHandler) writeResourceGrantError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, resource.ErrGrantNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case strings.Contains(err.Error(), "required"):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}
