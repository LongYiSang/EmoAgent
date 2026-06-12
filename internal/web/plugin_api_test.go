package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/longyisang/emoagent/internal/plugin"
	"github.com/longyisang/emoagent/internal/storage"
)

type pluginAPIApp struct {
	fakeAdminApp
	summary    plugin.AdminPluginSummary
	enabledID  string
	disabledID string
}

func (a *pluginAPIApp) InstallLocalPlugin(context.Context, plugin.AdminPluginInstallRequest) (plugin.AdminPluginSummary, error) {
	return a.summary, nil
}
func (a *pluginAPIApp) InstallGitHubPluginRelease(context.Context, plugin.AdminGitHubInstallRequest) (plugin.AdminPluginSummary, error) {
	return a.summary, nil
}
func (a *pluginAPIApp) ListPlugins(context.Context) ([]plugin.AdminPluginSummary, error) {
	return []plugin.AdminPluginSummary{a.summary}, nil
}
func (a *pluginAPIApp) GetPlugin(context.Context, string) (plugin.AdminPluginSummary, error) {
	return a.summary, nil
}
func (a *pluginAPIApp) EnablePlugin(_ context.Context, pluginID string, _ plugin.AdminPluginEnableRequest) (plugin.AdminPluginSummary, error) {
	a.enabledID = pluginID
	a.summary.Enabled = true
	a.summary.RuntimeStatus.Status = "running"
	return a.summary, nil
}
func (a *pluginAPIApp) DisablePlugin(_ context.Context, pluginID string) (plugin.AdminPluginSummary, error) {
	a.disabledID = pluginID
	a.summary.Enabled = false
	a.summary.RuntimeStatus.Status = "stopped"
	return a.summary, nil
}
func (a *pluginAPIApp) RestartPlugin(context.Context, string) (plugin.AdminPluginSummary, error) {
	return a.summary, nil
}
func (a *pluginAPIApp) DeletePlugin(context.Context, string) error {
	return nil
}
func (a *pluginAPIApp) PluginLogs(context.Context, string) (plugin.AdminPluginLogs, error) {
	return plugin.AdminPluginLogs{PluginID: a.summary.PluginID, StderrTail: "tail"}, nil
}
func (a *pluginAPIApp) ListPluginAccessEvents(context.Context, string, int) ([]storage.PluginAccessEvent, error) {
	return nil, nil
}
func (a *pluginAPIApp) ListPluginProviderUsage(context.Context, string, int) ([]storage.PluginProviderUsage, error) {
	return nil, nil
}

func TestPluginAdminAPIListEnableDisableStatus(t *testing.T) {
	app := &pluginAPIApp{summary: plugin.AdminPluginSummary{
		PluginID:      "com.example.echo",
		Version:       "0.1.0",
		Name:          "Echo",
		RuntimeKind:   plugin.RuntimePythonProcess,
		RuntimeStatus: plugin.RuntimeStatus{PluginID: "com.example.echo", Status: "stopped"},
	}}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	listRec := httptest.NewRecorder()
	handler.HandleListPlugins(listRec, httptest.NewRequest(http.MethodGet, "/api/plugins", nil))
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	var listResp struct {
		Plugins []plugin.AdminPluginSummary `json:"plugins"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.Plugins) != 1 || listResp.Plugins[0].PluginID != "com.example.echo" {
		t.Fatalf("list response = %#v", listResp)
	}

	enableReq := httptest.NewRequest(http.MethodPost, "/api/plugins/com.example.echo/enable", bytes.NewBufferString(`{"user_grant_json":"{}"}`))
	enableReq.SetPathValue("id", "com.example.echo")
	enableRec := httptest.NewRecorder()
	handler.HandleEnablePlugin(enableRec, enableReq)
	if enableRec.Code != http.StatusOK || app.enabledID != "com.example.echo" {
		t.Fatalf("enable status=%d enabledID=%q body=%s", enableRec.Code, app.enabledID, enableRec.Body.String())
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/api/plugins/com.example.echo/status", nil)
	statusReq.SetPathValue("id", "com.example.echo")
	statusRec := httptest.NewRecorder()
	handler.HandlePluginStatus(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("status status=%d body=%s", statusRec.Code, statusRec.Body.String())
	}
	var status plugin.RuntimeStatus
	if err := json.Unmarshal(statusRec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.Status != "running" {
		t.Fatalf("runtime status = %#v", status)
	}

	disableReq := httptest.NewRequest(http.MethodPost, "/api/plugins/com.example.echo/disable", nil)
	disableReq.SetPathValue("id", "com.example.echo")
	disableRec := httptest.NewRecorder()
	handler.HandleDisablePlugin(disableRec, disableReq)
	if disableRec.Code != http.StatusOK || app.disabledID != "com.example.echo" {
		t.Fatalf("disable status=%d disabledID=%q body=%s", disableRec.Code, app.disabledID, disableRec.Body.String())
	}
}
