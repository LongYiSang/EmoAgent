package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/longyisang/emoagent/internal/plugin"
	"github.com/longyisang/emoagent/internal/storage"
)

type PluginAdminApp interface {
	InstallLocalPlugin(context.Context, plugin.AdminPluginInstallRequest) (plugin.AdminPluginSummary, error)
	InstallGitHubPluginRelease(context.Context, plugin.AdminGitHubInstallRequest) (plugin.AdminPluginSummary, error)
	ListPlugins(context.Context) ([]plugin.AdminPluginSummary, error)
	GetPlugin(context.Context, string) (plugin.AdminPluginSummary, error)
	EnablePlugin(context.Context, string, plugin.AdminPluginEnableRequest) (plugin.AdminPluginSummary, error)
	DisablePlugin(context.Context, string) (plugin.AdminPluginSummary, error)
	RestartPlugin(context.Context, string) (plugin.AdminPluginSummary, error)
	DeletePlugin(context.Context, string) error
	PluginLogs(context.Context, string) (plugin.AdminPluginLogs, error)
	PluginDiagnostics(context.Context) (plugin.AdminPluginDiagnostics, error)
	ListPluginAccessEvents(context.Context, string, int) ([]storage.PluginAccessEvent, error)
	ListPluginProviderUsage(context.Context, string, int) ([]storage.PluginProviderUsage, error)
}

type PluginAdminVersionApp interface {
	GetPluginVersion(context.Context, string, string) (plugin.AdminPluginSummary, error)
}

type PluginAdminVersionGrantPreviewApp interface {
	GetPluginVersionForGrant(context.Context, string, string, string) (plugin.AdminPluginSummary, error)
}

type PluginAdminSettingsApp interface {
	GetPluginSettings(context.Context, string) (plugin.AdminPluginSettings, error)
	UpdatePluginSettings(context.Context, string, plugin.AdminPluginSettingsUpdateRequest) (plugin.AdminPluginSettings, error)
}

func (h *APIHandler) pluginAdminApp(w http.ResponseWriter) (PluginAdminApp, bool) {
	app, ok := any(h.app).(PluginAdminApp)
	if !ok {
		writeError(w, http.StatusNotImplemented, "plugin admin API is not available")
		return nil, false
	}
	return app, true
}

func (h *APIHandler) pluginAdminSettingsApp(w http.ResponseWriter) (PluginAdminSettingsApp, bool) {
	app, ok := any(h.app).(PluginAdminSettingsApp)
	if !ok {
		writeError(w, http.StatusNotImplemented, "plugin settings API is not available")
		return nil, false
	}
	return app, true
}

func (h *APIHandler) HandleListPlugins(w http.ResponseWriter, r *http.Request) {
	app, ok := h.pluginAdminApp(w)
	if !ok {
		return
	}
	plugins, err := app.ListPlugins(r.Context())
	if err != nil {
		h.writePluginError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plugins": plugins})
}

func (h *APIHandler) HandleGetPlugin(w http.ResponseWriter, r *http.Request) {
	app, ok := h.pluginAdminApp(w)
	if !ok {
		return
	}
	version := strings.TrimSpace(r.URL.Query().Get("version"))
	if version != "" {
		userGrantJSON := strings.TrimSpace(r.URL.Query().Get("user_grant_json"))
		if userGrantJSON != "" {
			grantPreviewApp, ok := any(app).(PluginAdminVersionGrantPreviewApp)
			if !ok {
				writeError(w, http.StatusNotImplemented, "plugin version grant preview API is not available")
				return
			}
			summary, err := grantPreviewApp.GetPluginVersionForGrant(r.Context(), r.PathValue("id"), version, userGrantJSON)
			if err != nil {
				h.writePluginError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, summary)
			return
		}
		versionApp, ok := any(app).(PluginAdminVersionApp)
		if !ok {
			writeError(w, http.StatusNotImplemented, "plugin version detail API is not available")
			return
		}
		summary, err := versionApp.GetPluginVersion(r.Context(), r.PathValue("id"), version)
		if err != nil {
			h.writePluginError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, summary)
		return
	}
	summary, err := app.GetPlugin(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writePluginError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (h *APIHandler) HandleGetPluginSettings(w http.ResponseWriter, r *http.Request) {
	app, ok := h.pluginAdminSettingsApp(w)
	if !ok {
		return
	}
	settings, err := app.GetPluginSettings(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writePluginError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (h *APIHandler) HandleUpdatePluginSettings(w http.ResponseWriter, r *http.Request) {
	app, ok := h.pluginAdminSettingsApp(w)
	if !ok {
		return
	}
	var req plugin.AdminPluginSettingsUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	settings, err := app.UpdatePluginSettings(r.Context(), r.PathValue("id"), req)
	if err != nil {
		h.writePluginError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (h *APIHandler) HandleInstallLocalPlugin(w http.ResponseWriter, r *http.Request) {
	app, ok := h.pluginAdminApp(w)
	if !ok {
		return
	}
	var req plugin.AdminPluginInstallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	summary, err := app.InstallLocalPlugin(r.Context(), req)
	if err != nil {
		h.writePluginError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, summary)
}

func (h *APIHandler) HandleInstallGitHubPlugin(w http.ResponseWriter, r *http.Request) {
	app, ok := h.pluginAdminApp(w)
	if !ok {
		return
	}
	var req plugin.AdminGitHubInstallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	summary, err := app.InstallGitHubPluginRelease(r.Context(), req)
	if err != nil {
		h.writePluginError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, summary)
}

func (h *APIHandler) HandleEnablePlugin(w http.ResponseWriter, r *http.Request) {
	app, ok := h.pluginAdminApp(w)
	if !ok {
		return
	}
	var req plugin.AdminPluginEnableRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
	}
	summary, err := app.EnablePlugin(r.Context(), r.PathValue("id"), req)
	if err != nil {
		h.writePluginError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (h *APIHandler) HandleDisablePlugin(w http.ResponseWriter, r *http.Request) {
	app, ok := h.pluginAdminApp(w)
	if !ok {
		return
	}
	summary, err := app.DisablePlugin(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writePluginError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (h *APIHandler) HandleRestartPlugin(w http.ResponseWriter, r *http.Request) {
	app, ok := h.pluginAdminApp(w)
	if !ok {
		return
	}
	summary, err := app.RestartPlugin(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writePluginError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (h *APIHandler) HandlePluginStatus(w http.ResponseWriter, r *http.Request) {
	app, ok := h.pluginAdminApp(w)
	if !ok {
		return
	}
	summary, err := app.GetPlugin(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writePluginError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summary.RuntimeStatus)
}

func (h *APIHandler) HandleDeletePlugin(w http.ResponseWriter, r *http.Request) {
	app, ok := h.pluginAdminApp(w)
	if !ok {
		return
	}
	if err := app.DeletePlugin(r.Context(), r.PathValue("id")); err != nil {
		h.writePluginError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *APIHandler) HandlePluginLogs(w http.ResponseWriter, r *http.Request) {
	app, ok := h.pluginAdminApp(w)
	if !ok {
		return
	}
	logs, err := app.PluginLogs(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writePluginError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, logs)
}

func (h *APIHandler) HandlePluginDiagnostics(w http.ResponseWriter, r *http.Request) {
	app, ok := h.pluginAdminApp(w)
	if !ok {
		return
	}
	diagnostics, err := app.PluginDiagnostics(r.Context())
	if err != nil {
		h.writePluginError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, diagnostics)
}

func (h *APIHandler) HandlePluginAccessEvents(w http.ResponseWriter, r *http.Request) {
	app, ok := h.pluginAdminApp(w)
	if !ok {
		return
	}
	events, err := app.ListPluginAccessEvents(r.Context(), r.PathValue("id"), pluginLimit(r))
	if err != nil {
		h.writePluginError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, plugin.AdminPluginAccessEvents{Events: events})
}

func (h *APIHandler) HandlePluginProviderUsage(w http.ResponseWriter, r *http.Request) {
	app, ok := h.pluginAdminApp(w)
	if !ok {
		return
	}
	usage, err := app.ListPluginProviderUsage(r.Context(), r.PathValue("id"), pluginLimit(r))
	if err != nil {
		h.writePluginError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, plugin.AdminPluginProviderUsage{Usage: usage})
}

func pluginLimit(r *http.Request) int {
	limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
	return limit
}

func (h *APIHandler) writePluginError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, plugin.ErrPluginNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, plugin.ErrPluginAdminDisabled):
		writeError(w, http.StatusForbidden, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}
