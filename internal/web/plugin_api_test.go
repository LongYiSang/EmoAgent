package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/plugin"
	"github.com/longyisang/emoagent/internal/storage"
)

type pluginAPIApp struct {
	fakeAdminApp
	summary            plugin.AdminPluginSummary
	versioned          plugin.AdminPluginSummary
	diagnostics        plugin.AdminPluginDiagnostics
	accessEvents       []storage.PluginAccessEvent
	providerUsage      []storage.PluginProviderUsage
	getVersion         string
	getVersionGrant    string
	installLocalReq    plugin.AdminPluginInstallRequest
	installGitHubReq   plugin.AdminGitHubInstallRequest
	enableReq          plugin.AdminPluginEnableRequest
	enableErr          error
	enabledID          string
	disabledID         string
	restartedID        string
	deletedID          string
	logsID             string
	settingsID         string
	settingsValue      json.RawMessage
	settingsUpdateID   string
	settingsUpdateReq  plugin.AdminPluginSettingsUpdateRequest
	accessEventsID     string
	accessEventsLimit  int
	providerUsageID    string
	providerUsageLimit int
}

func (a *pluginAPIApp) InstallLocalPlugin(_ context.Context, req plugin.AdminPluginInstallRequest) (plugin.AdminPluginSummary, error) {
	a.installLocalReq = req
	return a.summary, nil
}
func (a *pluginAPIApp) InstallGitHubPluginRelease(_ context.Context, req plugin.AdminGitHubInstallRequest) (plugin.AdminPluginSummary, error) {
	a.installGitHubReq = req
	return a.summary, nil
}
func (a *pluginAPIApp) ListPlugins(context.Context) ([]plugin.AdminPluginSummary, error) {
	return []plugin.AdminPluginSummary{a.summary}, nil
}
func (a *pluginAPIApp) GetPlugin(context.Context, string) (plugin.AdminPluginSummary, error) {
	return a.summary, nil
}
func (a *pluginAPIApp) GetPluginVersion(_ context.Context, _ string, version string) (plugin.AdminPluginSummary, error) {
	a.getVersion = version
	if a.versioned.PluginID != "" {
		return a.versioned, nil
	}
	return a.summary, nil
}
func (a *pluginAPIApp) GetPluginVersionForGrant(_ context.Context, _ string, version string, userGrantJSON string) (plugin.AdminPluginSummary, error) {
	a.getVersion = version
	a.getVersionGrant = userGrantJSON
	if a.versioned.PluginID != "" {
		return a.versioned, nil
	}
	return a.summary, nil
}
func (a *pluginAPIApp) EnablePlugin(_ context.Context, pluginID string, req plugin.AdminPluginEnableRequest) (plugin.AdminPluginSummary, error) {
	a.enabledID = pluginID
	a.enableReq = req
	if a.enableErr != nil {
		return plugin.AdminPluginSummary{}, a.enableErr
	}
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
func (a *pluginAPIApp) RestartPlugin(_ context.Context, pluginID string) (plugin.AdminPluginSummary, error) {
	a.restartedID = pluginID
	return a.summary, nil
}
func (a *pluginAPIApp) DeletePlugin(_ context.Context, pluginID string) error {
	a.deletedID = pluginID
	return nil
}
func (a *pluginAPIApp) PluginLogs(_ context.Context, pluginID string) (plugin.AdminPluginLogs, error) {
	a.logsID = pluginID
	return plugin.AdminPluginLogs{PluginID: a.summary.PluginID, StderrTail: "tail"}, nil
}
func (a *pluginAPIApp) GetPluginSettings(_ context.Context, pluginID string) (plugin.AdminPluginSettings, error) {
	a.settingsID = pluginID
	value := a.settingsValue
	if len(value) == 0 {
		value = json.RawMessage(`{}`)
	}
	return plugin.AdminPluginSettings{PluginID: pluginID, Key: "settings", Found: len(a.settingsValue) > 0, Value: value}, nil
}
func (a *pluginAPIApp) UpdatePluginSettings(_ context.Context, pluginID string, req plugin.AdminPluginSettingsUpdateRequest) (plugin.AdminPluginSettings, error) {
	a.settingsUpdateID = pluginID
	a.settingsUpdateReq = req
	return plugin.AdminPluginSettings{PluginID: pluginID, Key: "settings", Found: true, Value: req.Value}, nil
}
func (a *pluginAPIApp) ListPluginAccessEvents(_ context.Context, pluginID string, limit int) ([]storage.PluginAccessEvent, error) {
	a.accessEventsID = pluginID
	a.accessEventsLimit = limit
	return a.accessEvents, nil
}
func (a *pluginAPIApp) ListPluginProviderUsage(_ context.Context, pluginID string, limit int) ([]storage.PluginProviderUsage, error) {
	a.providerUsageID = pluginID
	a.providerUsageLimit = limit
	return a.providerUsage, nil
}
func (a *pluginAPIApp) PluginDiagnostics(context.Context) (plugin.AdminPluginDiagnostics, error) {
	return a.diagnostics, nil
}

func capabilitiesToStringValues(capabilities []plugin.Capability) []string {
	values := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		values = append(values, string(capability))
	}
	return values
}

func hookNamesToStringValues(hooks []plugin.HookName) []string {
	values := make([]string, 0, len(hooks))
	for _, hook := range hooks {
		values = append(values, string(hook))
	}
	return values
}

func TestPluginAdminAPIListEnableDisableStatus(t *testing.T) {
	app := &pluginAPIApp{summary: plugin.AdminPluginSummary{
		PluginID:    "com.example.echo",
		Version:     "0.1.0",
		Name:        "Echo",
		RuntimeKind: plugin.RuntimePythonProcess,
		TrustLevel:  plugin.TrustDeveloper,
		TrustAcceptance: plugin.PluginTrustAcceptance{
			AcceptedAt:          "2026-06-22T10:00:00Z",
			AcknowledgementHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Reasons:             []string{"capability_added:provider.generate"},
		},
		DependencySummary: plugin.DependencyLockSummary{
			Present:      true,
			LockDigest:   "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			PackageCount: 1,
			Packages: []plugin.DependencyPackageSummary{{
				Name:   "depmod",
				Kind:   "python_module_zip",
				Path:   "deps/depmod.zip",
				SHA256: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
			}},
		},
		HostAPIPolicy: plugin.PluginHostAPIPolicySummary{EffectiveCapabilities: []plugin.Capability{plugin.CapabilityProviderGenerate}, HostPolicyMode: "allowlist"},
		ToolPolicy:    plugin.PluginToolPolicySummary{DefaultExposure: plugin.ExposureWork, DefaultInvocation: plugin.InvocationAsk},
		HookPolicy:    plugin.PluginHookPolicySummary{AllowActiveHooks: false, ObserveHooks: []plugin.HookName{plugin.HookAfterTurnEnd}},
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
	if listResp.Plugins[0].TrustLevel != plugin.TrustDeveloper {
		t.Fatalf("list trust_level = %q, want developer", listResp.Plugins[0].TrustLevel)
	}
	if listResp.Plugins[0].TrustAcceptance.AcceptedAt == "" || listResp.Plugins[0].TrustAcceptance.AcknowledgementHash == "" {
		t.Fatalf("list trust_acceptance = %#v, want accepted_at and acknowledgement hash", listResp.Plugins[0].TrustAcceptance)
	}
	if listResp.Plugins[0].ToolPolicy.DefaultExposure != plugin.ExposureWork || listResp.Plugins[0].ToolPolicy.DefaultInvocation != plugin.InvocationAsk {
		t.Fatalf("list tool_policy = %#v, want work + ask", listResp.Plugins[0].ToolPolicy)
	}
	if got := strings.Join(capabilitiesToStringValues(listResp.Plugins[0].HostAPIPolicy.EffectiveCapabilities), ","); got != "provider.generate" {
		t.Fatalf("list effective capabilities = %q, want provider.generate", got)
	}
	if got := strings.Join(hookNamesToStringValues(listResp.Plugins[0].HookPolicy.ObserveHooks), ","); got != "after_turn_end" {
		t.Fatalf("list observe hooks = %q, want after_turn_end", got)
	}
	if !listResp.Plugins[0].DependencySummary.Present || listResp.Plugins[0].DependencySummary.PackageCount != 1 {
		t.Fatalf("list dependency summary = %#v, want one package", listResp.Plugins[0].DependencySummary)
	}

	grantPreview := `{"tier":"runtime_safe","capabilities":["turn.read","provider.generate"]}`
	detailReq := httptest.NewRequest(http.MethodGet, "/api/plugins/com.example.echo?version=0.1.0&user_grant_json="+url.QueryEscape(grantPreview), nil)
	detailReq.SetPathValue("id", "com.example.echo")
	detailRec := httptest.NewRecorder()
	handler.HandleGetPlugin(detailRec, detailReq)
	if detailRec.Code != http.StatusOK || app.getVersion != "0.1.0" || app.getVersionGrant != grantPreview {
		t.Fatalf("detail preview status=%d version=%q grant=%q body=%s", detailRec.Code, app.getVersion, app.getVersionGrant, detailRec.Body.String())
	}

	enableReq := httptest.NewRequest(http.MethodPost, "/api/plugins/com.example.echo/enable", bytes.NewBufferString(`{"user_grant_json":"{}"}`))
	enableReq.SetPathValue("id", "com.example.echo")
	enableRec := httptest.NewRecorder()
	handler.HandleEnablePlugin(enableRec, enableReq)
	if enableRec.Code != http.StatusOK || app.enabledID != "com.example.echo" {
		t.Fatalf("enable status=%d enabledID=%q body=%s", enableRec.Code, app.enabledID, enableRec.Body.String())
	}

	enableTrustReq := httptest.NewRequest(http.MethodPost, "/api/plugins/com.example.echo/enable", bytes.NewBufferString(`{
		"version":"0.1.0",
		"user_grant_json":"{}",
		"trust_acknowledgement":{
			"plugin_id":"com.example.echo",
			"version":"0.1.0",
			"package_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"manifest_digest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			"signature_status":"verified",
			"publisher_id":"example",
			"target_user_grant_hash":"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			"dependency_lock_digest":"sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			"ack_nonce":"nonce-1",
			"ack_issued_at":"2026-06-22T10:00:00Z",
			"user_action":"enable_plugin",
			"reasons":["publisher_changed"]
		}
	}`))
	enableTrustReq.SetPathValue("id", "com.example.echo")
	enableTrustRec := httptest.NewRecorder()
	handler.HandleEnablePlugin(enableTrustRec, enableTrustReq)
	if enableTrustRec.Code != http.StatusOK || app.enableReq.TrustAcknowledgement == nil {
		t.Fatalf("enable trust status=%d acknowledgement=%#v body=%s", enableTrustRec.Code, app.enableReq.TrustAcknowledgement, enableTrustRec.Body.String())
	}
	if app.enableReq.TrustAcknowledgement.PublisherID != "example" ||
		app.enableReq.TrustAcknowledgement.AckNonce != "nonce-1" ||
		app.enableReq.TrustAcknowledgement.AckIssuedAt != "2026-06-22T10:00:00Z" ||
		app.enableReq.TrustAcknowledgement.UserAction != plugin.TrustAcknowledgementActionEnablePlugin ||
		app.enableReq.TrustAcknowledgement.TargetUserGrantHash != "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc" ||
		app.enableReq.TrustAcknowledgement.DependencyLockDigest != "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd" ||
		len(app.enableReq.TrustAcknowledgement.Reasons) != 1 {
		t.Fatalf("enable trust acknowledgement = %#v", app.enableReq.TrustAcknowledgement)
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

func TestPluginAdminAPISettings(t *testing.T) {
	app := &pluginAPIApp{
		settingsValue: json.RawMessage(`{"amap_key":"k","city_adcode":"110101","extensions":"base"}`),
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	getReq := httptest.NewRequest(http.MethodGet, "/api/plugins/com.longyisang.amap-weather/settings", nil)
	getReq.SetPathValue("id", "com.longyisang.amap-weather")
	getRec := httptest.NewRecorder()
	handler.HandleGetPluginSettings(getRec, getReq)
	if getRec.Code != http.StatusOK || app.settingsID != "com.longyisang.amap-weather" {
		t.Fatalf("get settings status=%d id=%q body=%s", getRec.Code, app.settingsID, getRec.Body.String())
	}

	putReq := httptest.NewRequest(http.MethodPut, "/api/plugins/com.longyisang.amap-weather/settings", bytes.NewBufferString(`{"value":{"amap_key":"k2","city_adcode":"310101","extensions":"all"}}`))
	putReq.SetPathValue("id", "com.longyisang.amap-weather")
	putRec := httptest.NewRecorder()
	handler.HandleUpdatePluginSettings(putRec, putReq)
	if putRec.Code != http.StatusOK || app.settingsUpdateID != "com.longyisang.amap-weather" {
		t.Fatalf("put settings status=%d id=%q body=%s", putRec.Code, app.settingsUpdateID, putRec.Body.String())
	}
	if string(app.settingsUpdateReq.Value) != `{"amap_key":"k2","city_adcode":"310101","extensions":"all"}` {
		t.Fatalf("settings update value = %s", app.settingsUpdateReq.Value)
	}
}

func TestPluginAdminAPIInstallRestartDeleteAndLogs(t *testing.T) {
	app := &pluginAPIApp{summary: plugin.AdminPluginSummary{
		PluginID:      "com.example.echo",
		Version:       "0.1.0",
		Name:          "Echo",
		RuntimeKind:   plugin.RuntimeManagedPythonProcess,
		RuntimeStatus: plugin.RuntimeStatus{PluginID: "com.example.echo", Status: "stopped"},
	}}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	localRec := httptest.NewRecorder()
	handler.HandleInstallLocalPlugin(localRec, httptest.NewRequest(http.MethodPost, "/api/plugins/install/local", bytes.NewBufferString(`{"path":"D:\\plugins\\echo","installed_by":"dev"}`)))
	if localRec.Code != http.StatusCreated {
		t.Fatalf("install local status=%d body=%s", localRec.Code, localRec.Body.String())
	}
	if app.installLocalReq.Path != `D:\plugins\echo` || app.installLocalReq.InstalledBy != "dev" {
		t.Fatalf("install local request = %#v", app.installLocalReq)
	}
	var localSummary plugin.AdminPluginSummary
	if err := json.Unmarshal(localRec.Body.Bytes(), &localSummary); err != nil {
		t.Fatalf("decode install local: %v", err)
	}
	if localSummary.PluginID != "com.example.echo" || localSummary.RuntimeKind != plugin.RuntimeManagedPythonProcess {
		t.Fatalf("install local summary = %#v", localSummary)
	}

	githubRec := httptest.NewRecorder()
	handler.HandleInstallGitHubPlugin(githubRec, httptest.NewRequest(http.MethodPost, "/api/plugins/install/github-release", bytes.NewBufferString(`{"owner":"acme","repo":"echo","tag":"v0.1.0","asset":"echo.zip","installed_by":"dev"}`)))
	if githubRec.Code != http.StatusCreated {
		t.Fatalf("install github status=%d body=%s", githubRec.Code, githubRec.Body.String())
	}
	if app.installGitHubReq.Owner != "acme" || app.installGitHubReq.Repo != "echo" || app.installGitHubReq.Tag != "v0.1.0" || app.installGitHubReq.Asset != "echo.zip" || app.installGitHubReq.InstalledBy != "dev" {
		t.Fatalf("install github request = %#v", app.installGitHubReq)
	}

	restartReq := httptest.NewRequest(http.MethodPost, "/api/plugins/com.example.echo/restart", nil)
	restartReq.SetPathValue("id", "com.example.echo")
	restartRec := httptest.NewRecorder()
	handler.HandleRestartPlugin(restartRec, restartReq)
	if restartRec.Code != http.StatusOK || app.restartedID != "com.example.echo" {
		t.Fatalf("restart status=%d restartedID=%q body=%s", restartRec.Code, app.restartedID, restartRec.Body.String())
	}

	logsReq := httptest.NewRequest(http.MethodGet, "/api/plugins/com.example.echo/logs", nil)
	logsReq.SetPathValue("id", "com.example.echo")
	logsRec := httptest.NewRecorder()
	handler.HandlePluginLogs(logsRec, logsReq)
	if logsRec.Code != http.StatusOK || app.logsID != "com.example.echo" {
		t.Fatalf("logs status=%d logsID=%q body=%s", logsRec.Code, app.logsID, logsRec.Body.String())
	}
	var logs plugin.AdminPluginLogs
	if err := json.Unmarshal(logsRec.Body.Bytes(), &logs); err != nil {
		t.Fatalf("decode logs: %v", err)
	}
	if logs.PluginID != "com.example.echo" || logs.StderrTail != "tail" {
		t.Fatalf("logs = %#v", logs)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/plugins/com.example.echo", nil)
	deleteReq.SetPathValue("id", "com.example.echo")
	deleteRec := httptest.NewRecorder()
	handler.HandleDeletePlugin(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK || app.deletedID != "com.example.echo" {
		t.Fatalf("delete status=%d deletedID=%q body=%s", deleteRec.Code, app.deletedID, deleteRec.Body.String())
	}
	if !strings.Contains(deleteRec.Body.String(), `"ok":true`) {
		t.Fatalf("delete body=%s, want ok true", deleteRec.Body.String())
	}
}

func TestPluginAdminAPIAccessEventsAndProviderUsage(t *testing.T) {
	app := &pluginAPIApp{
		summary: plugin.AdminPluginSummary{PluginID: "com.example.echo", Version: "0.1.0", Name: "Echo"},
		accessEvents: []storage.PluginAccessEvent{{
			ID:             "evt-1",
			PluginID:       "com.example.echo",
			AccessKind:     "provider.generate",
			Capability:     "provider.generate",
			Status:         "allowed",
			RequestSummary: "provider.generate bytes=42",
			InputHash:      "sha256:input",
			OutputHash:     "sha256:output",
			DurationMS:     12,
			CreatedAt:      "2026-06-21T12:00:00Z",
		}},
		providerUsage: []storage.PluginProviderUsage{{
			ID:              "usage-1",
			PluginID:        "com.example.echo",
			ProviderID:      "fake",
			Model:           "fake-model",
			Purpose:         "provider_ping",
			InputTokens:     3,
			OutputTokens:    5,
			EstimatedTokens: 8,
			Status:          "success",
			DurationMS:      21,
			CreatedAt:       "2026-06-21T12:00:01Z",
		}},
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	eventsReq := httptest.NewRequest(http.MethodGet, "/api/plugins/com.example.echo/access-events?limit=7", nil)
	eventsReq.SetPathValue("id", "com.example.echo")
	eventsRec := httptest.NewRecorder()
	handler.HandlePluginAccessEvents(eventsRec, eventsReq)
	if eventsRec.Code != http.StatusOK || app.accessEventsID != "com.example.echo" || app.accessEventsLimit != 7 {
		t.Fatalf("events status=%d id=%q limit=%d body=%s", eventsRec.Code, app.accessEventsID, app.accessEventsLimit, eventsRec.Body.String())
	}
	var events plugin.AdminPluginAccessEvents
	if err := json.Unmarshal(eventsRec.Body.Bytes(), &events); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if len(events.Events) != 1 || events.Events[0].AccessKind != "provider.generate" || events.Events[0].InputHash != "sha256:input" || events.Events[0].OutputHash != "sha256:output" {
		t.Fatalf("events = %#v", events)
	}

	usageReq := httptest.NewRequest(http.MethodGet, "/api/plugins/com.example.echo/provider-usage?limit=3", nil)
	usageReq.SetPathValue("id", "com.example.echo")
	usageRec := httptest.NewRecorder()
	handler.HandlePluginProviderUsage(usageRec, usageReq)
	if usageRec.Code != http.StatusOK || app.providerUsageID != "com.example.echo" || app.providerUsageLimit != 3 {
		t.Fatalf("usage status=%d id=%q limit=%d body=%s", usageRec.Code, app.providerUsageID, app.providerUsageLimit, usageRec.Body.String())
	}
	var usage plugin.AdminPluginProviderUsage
	if err := json.Unmarshal(usageRec.Body.Bytes(), &usage); err != nil {
		t.Fatalf("decode usage: %v", err)
	}
	if len(usage.Usage) != 1 || usage.Usage[0].ProviderID != "fake" || usage.Usage[0].InputTokens != 3 || usage.Usage[0].OutputTokens != 5 || usage.Usage[0].EstimatedTokens != 8 {
		t.Fatalf("usage = %#v", usage)
	}
}

func TestPluginAdminAPIEnableValidationErrorReturnsBadRequest(t *testing.T) {
	app := &pluginAPIApp{
		summary:   plugin.AdminPluginSummary{PluginID: "com.example.echo", Version: "0.1.0", Name: "Echo"},
		enableErr: errors.New("user grant capability provider.generate is not declared by manifest"),
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	enableReq := httptest.NewRequest(http.MethodPost, "/api/plugins/com.example.echo/enable", bytes.NewBufferString(`{"user_grant_json":"{\"tier\":\"runtime_safe\",\"capabilities\":[\"provider.generate\"]}"}`))
	enableReq.SetPathValue("id", "com.example.echo")
	enableRec := httptest.NewRecorder()
	handler.HandleEnablePlugin(enableRec, enableReq)
	if enableRec.Code != http.StatusBadRequest {
		t.Fatalf("enable status=%d body=%s, want 400", enableRec.Code, enableRec.Body.String())
	}
	if !strings.Contains(enableRec.Body.String(), "not declared by manifest") {
		t.Fatalf("enable body=%s, want validation message", enableRec.Body.String())
	}
}

func TestPluginAdminAPIGetPluginVersionQuery(t *testing.T) {
	app := &pluginAPIApp{
		summary: plugin.AdminPluginSummary{
			PluginID: "com.example.echo",
			Version:  "0.1.0",
			Name:     "Echo 0.1",
		},
		versioned: plugin.AdminPluginSummary{
			PluginID: "com.example.echo",
			Version:  "0.2.0",
			Name:     "Echo 0.2",
		},
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/plugins/com.example.echo?version=0.2.0", nil)
	req.SetPathValue("id", "com.example.echo")
	rec := httptest.NewRecorder()
	handler.HandleGetPlugin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}
	if app.getVersion != "0.2.0" {
		t.Fatalf("GetPluginVersion version = %q, want 0.2.0", app.getVersion)
	}
	var summary plugin.AdminPluginSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if summary.Version != "0.2.0" {
		t.Fatalf("summary version = %q, want 0.2.0", summary.Version)
	}
}

func TestPluginAdminAPIStatusIncludesRuntimeDiagnostics(t *testing.T) {
	available := false
	app := &pluginAPIApp{summary: plugin.AdminPluginSummary{
		PluginID:    "com.example.echo",
		Version:     "0.1.0",
		Name:        "Echo",
		RuntimeKind: plugin.RuntimeManagedPythonProcess,
		RuntimeStatus: plugin.RuntimeStatus{
			PluginID:                  "com.example.echo",
			RuntimeKind:               plugin.RuntimeManagedPythonProcess,
			Status:                    "failed",
			LastError:                 "managed python environment unavailable",
			PythonExecutablePath:      "data/python-envs/plugins/com.example.echo/0.1.0/Scripts/python.exe",
			PythonExecutableSource:    plugin.PythonExecutableSourceToolchainUV,
			PythonExecutableAvailable: &available,
			PythonEnvironmentDir:      "data/python-envs/plugins/com.example.echo/0.1.0",
			PID:                       1234,
			ProcessGuardKind:          "windows_job_object",
			ProcessGuardAttached:      true,
		},
	}}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

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
	if status.RuntimeKind != plugin.RuntimeManagedPythonProcess ||
		status.PythonExecutablePath != "data/python-envs/plugins/com.example.echo/0.1.0/Scripts/python.exe" ||
		status.PythonExecutableSource != plugin.PythonExecutableSourceToolchainUV ||
		status.PythonExecutableAvailable == nil ||
		*status.PythonExecutableAvailable ||
		status.PythonEnvironmentDir != "data/python-envs/plugins/com.example.echo/0.1.0" ||
		status.PID != 1234 ||
		status.ProcessGuardKind != "windows_job_object" ||
		!status.ProcessGuardAttached {
		t.Fatalf("runtime diagnostics = %#v", status)
	}

	listRec := httptest.NewRecorder()
	handler.HandleListPlugins(listRec, httptest.NewRequest(http.MethodGet, "/api/plugins", nil))
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	var listResp struct {
		Plugins []plugin.AdminPluginSummary `json:"plugins"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.Plugins) != 1 ||
		listResp.Plugins[0].RuntimeStatus.PythonExecutableAvailable == nil ||
		*listResp.Plugins[0].RuntimeStatus.PythonExecutableAvailable ||
		listResp.Plugins[0].RuntimeStatus.PythonEnvironmentDir != "data/python-envs/plugins/com.example.echo/0.1.0" ||
		listResp.Plugins[0].RuntimeStatus.PID != 1234 ||
		listResp.Plugins[0].RuntimeStatus.ProcessGuardKind != "windows_job_object" ||
		!listResp.Plugins[0].RuntimeStatus.ProcessGuardAttached {
		t.Fatalf("list runtime diagnostics = %#v", listResp)
	}
}

func TestPluginAdminAPIDiagnostics(t *testing.T) {
	app := &pluginAPIApp{diagnostics: plugin.AdminPluginDiagnostics{
		Status: "warning",
		Checks: []plugin.AdminPluginDiagnosticCheck{
			{ID: "python_toolchain", Status: "warning", Label: "Python Toolchain", Message: "toolchain missing"},
			{ID: "python_environments", Status: "warning", Label: "uv environments", Message: "environment missing"},
			{ID: "process_guard", Status: "ok", Label: "Job Object / ProcessGuard", Message: "windows_job_object"},
			{ID: "uv_project", Status: "ok", Label: "uv project", Message: "1 project"},
			{ID: "plugin_logs", Status: "ok", Label: "Plugin logs", Message: "bounded stderr"},
			{ID: "repair", Status: "ok", Label: "Repair", Message: "manual reinstall available"},
		},
	}}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/plugins/diagnostics", nil)
	rec := httptest.NewRecorder()
	handler.HandlePluginDiagnostics(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("diagnostics status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got plugin.AdminPluginDiagnostics
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode diagnostics: %v", err)
	}
	if got.Status != "warning" || len(got.Checks) != 6 {
		t.Fatalf("diagnostics = %#v", got)
	}
	ids := make([]string, 0, len(got.Checks))
	for _, check := range got.Checks {
		ids = append(ids, check.ID)
	}
	if strings.Join(ids, ",") != "python_toolchain,python_environments,process_guard,uv_project,plugin_logs,repair" {
		t.Fatalf("diagnostic check ids = %q", strings.Join(ids, ","))
	}
}
