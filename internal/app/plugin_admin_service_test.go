package app

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/plugin"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/tool/resultv2"
)

func TestPluginServiceInstallEnableDisableList(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = false
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Installer.AllowUnsignedDev = true
	cfg.Plugins.Installer.RequireSignature = true

	service := &PluginService{
		infra: &Infra{
			Config:      cfg,
			DB:          db,
			Logger:      logger,
			ProjectRoot: dir,
		},
	}
	sourceDir := writeAdminFixturePlugin(t, dir)

	installed, err := service.InstallLocal(context.Background(), plugin.AdminPluginInstallRequest{
		Path:        sourceDir,
		InstalledBy: "test",
	})
	if err != nil {
		t.Fatalf("InstallLocal: %v", err)
	}
	if installed.PluginID != "com.example.admin" || installed.SignatureStatus != plugin.SignatureStatusUnsignedDev {
		t.Fatalf("installed summary = %#v", installed)
	}

	enabled, err := service.EnablePlugin(context.Background(), installed.PluginID, plugin.AdminPluginEnableRequest{
		UserGrantJSON: `{"tier":"runtime_safe"}`,
	})
	if err != nil {
		t.Fatalf("EnablePlugin: %v", err)
	}
	if !enabled.Enabled {
		t.Fatalf("enabled summary = %#v, want enabled", enabled)
	}

	list, err := service.ListPlugins(context.Background())
	if err != nil {
		t.Fatalf("ListPlugins: %v", err)
	}
	if len(list) != 1 || !list[0].Enabled || list[0].RuntimeStatus.Status != "stopped" {
		t.Fatalf("plugins = %#v", list)
	}

	disabled, err := service.DisablePlugin(context.Background(), installed.PluginID)
	if err != nil {
		t.Fatalf("DisablePlugin: %v", err)
	}
	if disabled.Enabled {
		t.Fatalf("disabled summary = %#v, want disabled", disabled)
	}
}

func TestPluginServiceSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = false
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Installer.AllowUnsignedDev = true
	cfg.Plugins.Installer.RequireSignature = true
	service := &PluginService{infra: &Infra{Config: cfg, DB: db, Logger: logger, ProjectRoot: dir}}

	sourceDir := writeAdminFixturePlugin(t, dir)
	installed, err := service.InstallLocal(context.Background(), plugin.AdminPluginInstallRequest{Path: sourceDir})
	if err != nil {
		t.Fatalf("InstallLocal: %v", err)
	}

	value := json.RawMessage(`{"amap_key":"k","city_adcode":"110101","extensions":"base"}`)
	saved, err := service.UpdatePluginSettings(context.Background(), installed.PluginID, plugin.AdminPluginSettingsUpdateRequest{Value: value})
	if err != nil {
		t.Fatalf("UpdatePluginSettings: %v", err)
	}
	if !saved.Found || saved.PluginID != installed.PluginID || saved.Key != "settings" || string(saved.Value) != string(value) {
		t.Fatalf("saved settings = %#v", saved)
	}

	loaded, err := service.GetPluginSettings(context.Background(), installed.PluginID)
	if err != nil {
		t.Fatalf("GetPluginSettings: %v", err)
	}
	if !loaded.Found || loaded.PluginID != installed.PluginID || loaded.Key != "settings" || string(loaded.Value) != string(value) {
		t.Fatalf("loaded settings = %#v", loaded)
	}
}

func TestPluginServiceSettingsSchemaRoundTrip(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = false
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Installer.AllowUnsignedDev = true
	cfg.Plugins.Installer.RequireSignature = true
	service := &PluginService{infra: &Infra{Config: cfg, DB: db, Logger: logger, ProjectRoot: dir}}

	sourceDir := writeAdminFixtureSettingsPlugin(t, dir)
	installed, err := service.InstallLocal(context.Background(), plugin.AdminPluginInstallRequest{Path: sourceDir})
	if err != nil {
		t.Fatalf("InstallLocal: %v", err)
	}
	if installed.SettingsSchema == nil || installed.SettingsSchema.Properties["api_key"].Type != "string" {
		t.Fatalf("installed settings_schema = %#v", installed.SettingsSchema)
	}

	value := json.RawMessage(`{"api_key":"k","mode":"all","retries":2,"ratio":1.5,"enabled":true,"extra":"drop"}`)
	saved, err := service.UpdatePluginSettings(context.Background(), installed.PluginID, plugin.AdminPluginSettingsUpdateRequest{Value: value})
	if err != nil {
		t.Fatalf("UpdatePluginSettings: %v", err)
	}
	var savedValue map[string]any
	if err := json.Unmarshal(saved.Value, &savedValue); err != nil {
		t.Fatalf("decode saved settings: %v", err)
	}
	if _, ok := savedValue["extra"]; ok {
		t.Fatalf("saved settings kept schema-external field: %s", saved.Value)
	}
	if savedValue["api_key"] != "k" || savedValue["mode"] != "all" || savedValue["enabled"] != true {
		t.Fatalf("saved settings = %#v", savedValue)
	}

	loaded, err := service.GetPluginSettings(context.Background(), installed.PluginID)
	if err != nil {
		t.Fatalf("GetPluginSettings: %v", err)
	}
	if string(loaded.Value) != string(saved.Value) {
		t.Fatalf("loaded settings = %s, want %s", loaded.Value, saved.Value)
	}
}

func TestPluginServiceSettingsSchemaRejectsInvalidValues(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = false
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Installer.AllowUnsignedDev = true
	cfg.Plugins.Installer.RequireSignature = true
	service := &PluginService{infra: &Infra{Config: cfg, DB: db, Logger: logger, ProjectRoot: dir}}

	sourceDir := writeAdminFixtureSettingsPlugin(t, dir)
	installed, err := service.InstallLocal(context.Background(), plugin.AdminPluginInstallRequest{Path: sourceDir})
	if err != nil {
		t.Fatalf("InstallLocal: %v", err)
	}

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "not object", value: `[]`, want: "object"},
		{name: "required missing", value: `{}`, want: "api_key"},
		{name: "required empty string", value: `{"api_key":" "}`, want: "api_key"},
		{name: "enum invalid", value: `{"api_key":"k","mode":"bad"}`, want: "mode"},
		{name: "number type", value: `{"api_key":"k","ratio":"fast"}`, want: "ratio"},
		{name: "integer type", value: `{"api_key":"k","retries":1.2}`, want: "retries"},
		{name: "boolean type", value: `{"api_key":"k","enabled":"yes"}`, want: "enabled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.UpdatePluginSettings(context.Background(), installed.PluginID, plugin.AdminPluginSettingsUpdateRequest{Value: json.RawMessage(tt.value)})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("UpdatePluginSettings error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestPluginServiceInstallUpdateRollbackUsesEnabledVersionForDefaultDetail(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = false
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Installer.AllowUnsignedDev = true
	cfg.Plugins.Installer.RequireSignature = true

	service := &PluginService{
		infra: &Infra{
			Config:      cfg,
			DB:          db,
			Logger:      logger,
			ProjectRoot: dir,
		},
	}
	v1Dir := writeAdminFixturePluginVersion(t, dir, "0.1.0", "Admin Fixture 0.1")
	v2Dir := writeAdminFixturePluginVersion(t, dir, "0.2.0", "Admin Fixture 0.2")

	v1, err := service.InstallLocal(context.Background(), plugin.AdminPluginInstallRequest{Path: v1Dir})
	if err != nil {
		t.Fatalf("InstallLocal v1: %v", err)
	}
	v2, err := service.InstallLocal(context.Background(), plugin.AdminPluginInstallRequest{Path: v2Dir})
	if err != nil {
		t.Fatalf("InstallLocal v2: %v", err)
	}

	if _, err := service.EnablePlugin(context.Background(), v2.PluginID, plugin.AdminPluginEnableRequest{
		Version:       v2.Version,
		UserGrantJSON: `{"tier":"runtime_safe"}`,
	}); err != nil {
		t.Fatalf("EnablePlugin v2: %v", err)
	}
	detail, err := service.GetPlugin(context.Background(), v2.PluginID)
	if err != nil {
		t.Fatalf("GetPlugin after v2 enable: %v", err)
	}
	if detail.Version != v2.Version || !detail.Enabled {
		t.Fatalf("detail after v2 enable = %#v, want active v2", detail)
	}

	if _, err := service.EnablePlugin(context.Background(), v1.PluginID, plugin.AdminPluginEnableRequest{
		Version:       v1.Version,
		UserGrantJSON: `{"tier":"runtime_safe"}`,
	}); err != nil {
		t.Fatalf("EnablePlugin rollback v1: %v", err)
	}
	detail, err = service.GetPlugin(context.Background(), v1.PluginID)
	if err != nil {
		t.Fatalf("GetPlugin after rollback: %v", err)
	}
	if detail.Version != v1.Version || !detail.Enabled {
		t.Fatalf("detail after rollback = %#v, want active v1", detail)
	}

	list, err := service.ListPlugins(context.Background())
	if err != nil {
		t.Fatalf("ListPlugins: %v", err)
	}
	enabledByVersion := map[string]bool{}
	for _, item := range list {
		enabledByVersion[item.Version] = item.Enabled
	}
	if !enabledByVersion[v1.Version] || enabledByVersion[v2.Version] {
		t.Fatalf("enabled versions after rollback = %#v, want only v1 enabled", enabledByVersion)
	}
	restarted, err := service.RestartPlugin(context.Background(), v1.PluginID)
	if err != nil {
		t.Fatalf("RestartPlugin after rollback: %v", err)
	}
	if restarted.Version != v1.Version {
		t.Fatalf("RestartPlugin after rollback returned version %q, want %q", restarted.Version, v1.Version)
	}
}

func TestPluginServiceTrustLevelAndBlockedManifestDigest(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	sourceDir := writeAdminFixturePlugin(t, dir)
	manifestDigest := testSHA256Digest(readFileBytes(t, filepath.Join(sourceDir, "emo_plugin.yaml")))
	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = false
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Installer.AllowUnsignedDev = true
	cfg.Plugins.Installer.RequireSignature = true
	cfg.Plugins.Installer.BlockedManifestDigests = []string{manifestDigest}
	service := &PluginService{infra: &Infra{Config: cfg, DB: db, Logger: logger, ProjectRoot: dir}}

	_, err = service.InstallLocal(context.Background(), plugin.AdminPluginInstallRequest{Path: sourceDir})
	if err == nil || !strings.Contains(err.Error(), "blocked manifest digest") {
		t.Fatalf("InstallLocal error = %v, want blocked manifest digest", err)
	}

	enableDir := t.TempDir()
	enableDB, err := storage.Open(filepath.Join(enableDir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open enable DB: %v", err)
	}
	t.Cleanup(func() { _ = enableDB.Close() })
	enableSourceDir := writeAdminFixturePlugin(t, enableDir)
	enableCfg := config.DefaultConfig()
	enableCfg.Plugins.Enabled = false
	enableCfg.Plugins.Store.RootDir = filepath.Join(enableDir, "store")
	enableCfg.Plugins.Installer.AllowUnsignedDev = true
	enableCfg.Plugins.Installer.RequireSignature = true
	enableService := &PluginService{infra: &Infra{Config: enableCfg, DB: enableDB, Logger: logger, ProjectRoot: enableDir}}
	installed, err := enableService.InstallLocal(context.Background(), plugin.AdminPluginInstallRequest{Path: enableSourceDir})
	if err != nil {
		t.Fatalf("InstallLocal unblocked: %v", err)
	}
	if installed.TrustLevel != plugin.TrustDeveloper {
		t.Fatalf("installed trust_level = %q, want developer", installed.TrustLevel)
	}
	enableCfg.Plugins.Installer.BlockedManifestDigests = []string{installed.ManifestDigest}
	_, err = enableService.EnablePlugin(context.Background(), installed.PluginID, plugin.AdminPluginEnableRequest{UserGrantJSON: `{}`})
	if err == nil || !strings.Contains(err.Error(), "blocked manifest digest") {
		t.Fatalf("EnablePlugin error = %v, want blocked manifest digest", err)
	}
}

func TestPluginServiceRequiresTrustAcknowledgementForCapabilityExpansion(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = false
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Installer.AllowUnsignedDev = true
	cfg.Plugins.Installer.RequireSignature = true
	service := &PluginService{infra: &Infra{Config: cfg, DB: db, Logger: logger, ProjectRoot: dir}}
	v1Dir := writeAdminFixturePluginVersionWithCapabilities(t, dir, "0.1.0", "Trust Fixture 0.1", []plugin.Capability{plugin.CapabilityTurnRead})
	v2Dir := writeAdminFixturePluginVersionWithCapabilities(t, dir, "0.2.0", "Trust Fixture 0.2", []plugin.Capability{plugin.CapabilityTurnRead, plugin.CapabilityProviderGenerate})

	v1, err := service.InstallLocal(context.Background(), plugin.AdminPluginInstallRequest{Path: v1Dir})
	if err != nil {
		t.Fatalf("InstallLocal v1: %v", err)
	}
	v2, err := service.InstallLocal(context.Background(), plugin.AdminPluginInstallRequest{Path: v2Dir})
	if err != nil {
		t.Fatalf("InstallLocal v2: %v", err)
	}
	if _, err := service.EnablePlugin(context.Background(), v1.PluginID, plugin.AdminPluginEnableRequest{Version: v1.Version, UserGrantJSON: `{"tier":"runtime_safe","capabilities":["turn.read"]}`}); err != nil {
		t.Fatalf("EnablePlugin v1: %v", err)
	}
	history, err := db.ListPluginTrustAcceptanceHistory(context.Background(), v1.PluginID, 10)
	if err != nil {
		t.Fatalf("ListPluginTrustAcceptanceHistory after v1: %v", err)
	}
	if len(history) != 1 || history[0].Version != v1.Version || history[0].TrustLevel != string(plugin.TrustDeveloper) {
		t.Fatalf("history after v1 = %#v, want one developer acceptance for %s", history, v1.Version)
	}
	if history[0].UserGrantHash == "" || !strings.HasPrefix(history[0].UserGrantHash, "sha256:") ||
		history[0].HostPolicyFingerprint == "" || !strings.HasPrefix(history[0].HostPolicyFingerprint, "sha256:") {
		t.Fatalf("history after v1 grant/policy hashes = %#v", history[0])
	}

	_, err = service.EnablePlugin(context.Background(), v2.PluginID, plugin.AdminPluginEnableRequest{
		Version:       v2.Version,
		UserGrantJSON: `{"tier":"runtime_safe","capabilities":["turn.read","provider.generate"]}`,
	})
	if err == nil || !strings.Contains(err.Error(), "plugin trust review required") {
		t.Fatalf("EnablePlugin v2 without trust acknowledgement error = %v, want trust review required", err)
	}
	history, err = db.ListPluginTrustAcceptanceHistory(context.Background(), v2.PluginID, 10)
	if err != nil {
		t.Fatalf("ListPluginTrustAcceptanceHistory after rejected v2: %v", err)
	}
	if len(history) != 1 || history[0].Version != v1.Version {
		t.Fatalf("history after rejected v2 = %#v, want only v1 acceptance", history)
	}
	v2Grant := `{"tier":"runtime_safe","capabilities":["turn.read","provider.generate"]}`
	detail, err := service.GetPluginVersionForGrant(context.Background(), v2.PluginID, v2.Version, v2Grant)
	if err != nil {
		t.Fatalf("GetPluginVersionForGrant v2: %v", err)
	}
	if !detail.TrustReview.Required || len(detail.TrustReview.Reasons) != 1 || detail.TrustReview.Reasons[0] != "capability_added:provider.generate" {
		t.Fatalf("trust review = %#v, want capability expansion reason", detail.TrustReview)
	}
	_, err = service.EnablePlugin(context.Background(), v2.PluginID, plugin.AdminPluginEnableRequest{
		Version:              v2.Version,
		UserGrantJSON:        v2Grant,
		TrustAcknowledgement: detail.TrustReview.Acknowledgement,
	})
	if err != nil {
		t.Fatalf("EnablePlugin v2 with trust acknowledgement: %v", err)
	}
	state, err := db.GetPluginEnabledState(context.Background(), v2.PluginID)
	if err != nil {
		t.Fatalf("GetPluginEnabledState v2: %v", err)
	}
	wantHash, err := plugin.HashPluginTrustAcknowledgement(*detail.TrustReview.Acknowledgement)
	if err != nil {
		t.Fatalf("HashPluginTrustAcknowledgement: %v", err)
	}
	if state.TrustLevel != string(plugin.TrustDeveloper) || state.TrustAcceptedAt == "" || state.TrustAcknowledgementHash != wantHash {
		t.Fatalf("enabled trust acceptance = %#v, want developer with hash %s", state, wantHash)
	}
	if state.TrustReviewReasonsJSON != `["capability_added:provider.generate"]` {
		t.Fatalf("trust reasons json = %q", state.TrustReviewReasonsJSON)
	}
	if state.DefaultToolExposure != string(plugin.ExposureWork) || state.DefaultInvocationPolicy != string(plugin.InvocationAsk) {
		t.Fatalf("stored tool policy = %q/%q", state.DefaultToolExposure, state.DefaultInvocationPolicy)
	}
	history, err = db.ListPluginTrustAcceptanceHistory(context.Background(), v2.PluginID, 10)
	if err != nil {
		t.Fatalf("ListPluginTrustAcceptanceHistory after v2: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history after v2 len = %d, want 2: %#v", len(history), history)
	}
	var v2History *storage.PluginTrustAcceptanceHistoryRecord
	for i := range history {
		if history[i].Version == v2.Version {
			v2History = &history[i]
			break
		}
	}
	if v2History == nil {
		t.Fatalf("history after v2 = %#v, want record for %s", history, v2.Version)
	}
	if v2History.AcknowledgementHash != wantHash || v2History.ReviewReasonsJSON != `["capability_added:provider.generate"]` {
		t.Fatalf("v2 history = %#v, want hash %s and capability reason", v2History, wantHash)
	}
	if v2History.DefaultToolExposure != string(plugin.ExposureWork) || v2History.DefaultInvocationPolicy != string(plugin.InvocationAsk) {
		t.Fatalf("v2 history tool policy = %#v", v2History)
	}
	if v2History.UserGrantHash == "" || !strings.HasPrefix(v2History.UserGrantHash, "sha256:") ||
		v2History.HostPolicyFingerprint == "" || !strings.HasPrefix(v2History.HostPolicyFingerprint, "sha256:") {
		t.Fatalf("v2 history grant/policy hashes = %#v", v2History)
	}
	enabled, err := service.GetPluginVersion(context.Background(), v2.PluginID, v2.Version)
	if err != nil {
		t.Fatalf("GetPluginVersion enabled v2: %v", err)
	}
	if enabled.TrustAcceptance.TrustLevel != plugin.TrustDeveloper || enabled.TrustAcceptance.AcceptedAt == "" || enabled.TrustAcceptance.AcknowledgementHash != wantHash {
		t.Fatalf("summary trust acceptance = %#v, want hash %s", enabled.TrustAcceptance, wantHash)
	}
}

func TestPluginServiceRequiresTrustAcknowledgementForDependencyLockChange(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = false
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Installer.AllowUnsignedDev = true
	service := &PluginService{infra: &Infra{Config: cfg, DB: db, Logger: logger, ProjectRoot: dir}}
	capabilities := []plugin.Capability{plugin.CapabilityTurnRead}
	v1Dir := writeAdminFixturePluginVersionWithCapabilities(t, dir, "0.1.0", "Dependency Lock Fixture 0.1", capabilities)
	v2Dir := writeAdminFixturePluginVersionWithCapabilities(t, dir, "0.2.0", "Dependency Lock Fixture 0.2", capabilities)
	v1LockDigest := writeAdminDependencyLock(t, v1Dir, `VALUE = "v1"`)
	v2LockDigest := writeAdminDependencyLock(t, v2Dir, `VALUE = "v2"`)
	if v1LockDigest == v2LockDigest {
		t.Fatalf("dependency lock digests unexpectedly match: %s", v1LockDigest)
	}

	v1, err := service.InstallLocal(context.Background(), plugin.AdminPluginInstallRequest{Path: v1Dir})
	if err != nil {
		t.Fatalf("InstallLocal v1: %v", err)
	}
	v2, err := service.InstallLocal(context.Background(), plugin.AdminPluginInstallRequest{Path: v2Dir})
	if err != nil {
		t.Fatalf("InstallLocal v2: %v", err)
	}
	grant := `{"tier":"runtime_safe","capabilities":["turn.read"]}`
	if _, err := service.EnablePlugin(context.Background(), v1.PluginID, plugin.AdminPluginEnableRequest{
		Version:       v1.Version,
		UserGrantJSON: grant,
	}); err != nil {
		t.Fatalf("EnablePlugin v1: %v", err)
	}
	history, err := db.ListPluginTrustAcceptanceHistory(context.Background(), v1.PluginID, 10)
	if err != nil {
		t.Fatalf("ListPluginTrustAcceptanceHistory v1: %v", err)
	}
	if len(history) != 1 || history[0].DependencyLockDigest != v1LockDigest {
		t.Fatalf("history after v1 = %#v, want dependency lock digest %s", history, v1LockDigest)
	}

	_, err = service.EnablePlugin(context.Background(), v2.PluginID, plugin.AdminPluginEnableRequest{
		Version:       v2.Version,
		UserGrantJSON: grant,
	})
	if err == nil || !strings.Contains(err.Error(), "plugin trust review required") {
		t.Fatalf("EnablePlugin v2 without dependency lock acknowledgement error = %v, want trust review required", err)
	}
	detail, err := service.GetPluginVersionForGrant(context.Background(), v2.PluginID, v2.Version, grant)
	if err != nil {
		t.Fatalf("GetPluginVersionForGrant v2: %v", err)
	}
	if !detail.TrustReview.Required || strings.Join(detail.TrustReview.Reasons, ",") != "dependency_lock_changed" || detail.TrustReview.Acknowledgement == nil {
		t.Fatalf("trust review after dependency lock change = %#v, want dependency_lock_changed", detail.TrustReview)
	}
	if detail.TrustReview.Acknowledgement.DependencyLockDigest != v2LockDigest {
		t.Fatalf("trust review dependency lock digest = %#v, want %s", detail.TrustReview.Acknowledgement, v2LockDigest)
	}
	forged := *detail.TrustReview.Acknowledgement
	forged.DependencyLockDigest = v1LockDigest
	if _, err := service.EnablePlugin(context.Background(), v2.PluginID, plugin.AdminPluginEnableRequest{
		Version:              v2.Version,
		UserGrantJSON:        grant,
		TrustAcknowledgement: &forged,
	}); err == nil || !strings.Contains(err.Error(), "plugin trust review acknowledgement does not match target") {
		t.Fatalf("EnablePlugin with forged dependency lock acknowledgement error = %v, want target mismatch", err)
	}
	if _, err := service.EnablePlugin(context.Background(), v2.PluginID, plugin.AdminPluginEnableRequest{
		Version:              v2.Version,
		UserGrantJSON:        grant,
		TrustAcknowledgement: detail.TrustReview.Acknowledgement,
	}); err != nil {
		t.Fatalf("EnablePlugin v2 with dependency lock acknowledgement: %v", err)
	}
	history, err = db.ListPluginTrustAcceptanceHistory(context.Background(), v2.PluginID, 10)
	if err != nil {
		t.Fatalf("ListPluginTrustAcceptanceHistory v2: %v", err)
	}
	var v2History *storage.PluginTrustAcceptanceHistoryRecord
	for i := range history {
		if history[i].Version == v2.Version {
			v2History = &history[i]
			break
		}
	}
	if v2History == nil || v2History.DependencyLockDigest != v2LockDigest || v2History.ReviewReasonsJSON != `["dependency_lock_changed"]` {
		t.Fatalf("history after v2 = %#v, want dependency lock digest %s and review reason", history, v2LockDigest)
	}
}

func TestPluginServiceRequiresTrustAcknowledgementForDependencyLockRemoval(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = false
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Installer.AllowUnsignedDev = true
	service := &PluginService{infra: &Infra{Config: cfg, DB: db, Logger: logger, ProjectRoot: dir}}
	capabilities := []plugin.Capability{plugin.CapabilityTurnRead}
	v1Dir := writeAdminFixturePluginVersionWithCapabilities(t, dir, "0.1.0", "Dependency Lock Removal Fixture 0.1", capabilities)
	v2Dir := writeAdminFixturePluginVersionWithCapabilities(t, dir, "0.2.0", "Dependency Lock Removal Fixture 0.2", capabilities)
	v1LockDigest := writeAdminDependencyLock(t, v1Dir, `VALUE = "v1"`)

	v1, err := service.InstallLocal(context.Background(), plugin.AdminPluginInstallRequest{Path: v1Dir})
	if err != nil {
		t.Fatalf("InstallLocal v1: %v", err)
	}
	v2, err := service.InstallLocal(context.Background(), plugin.AdminPluginInstallRequest{Path: v2Dir})
	if err != nil {
		t.Fatalf("InstallLocal v2: %v", err)
	}
	grant := `{"tier":"runtime_safe","capabilities":["turn.read"]}`
	if _, err := service.EnablePlugin(context.Background(), v1.PluginID, plugin.AdminPluginEnableRequest{
		Version:       v1.Version,
		UserGrantJSON: grant,
	}); err != nil {
		t.Fatalf("EnablePlugin v1: %v", err)
	}
	history, err := db.ListPluginTrustAcceptanceHistory(context.Background(), v1.PluginID, 10)
	if err != nil {
		t.Fatalf("ListPluginTrustAcceptanceHistory v1: %v", err)
	}
	if len(history) != 1 || history[0].DependencyLockDigest != v1LockDigest {
		t.Fatalf("history after v1 = %#v, want dependency lock digest %s", history, v1LockDigest)
	}

	detail, err := service.GetPluginVersionForGrant(context.Background(), v2.PluginID, v2.Version, grant)
	if err != nil {
		t.Fatalf("GetPluginVersionForGrant v2: %v", err)
	}
	if !detail.TrustReview.Required || strings.Join(detail.TrustReview.Reasons, ",") != "dependency_lock_changed" || detail.TrustReview.Acknowledgement == nil {
		t.Fatalf("trust review after dependency lock removal = %#v, want dependency_lock_changed", detail.TrustReview)
	}
	if detail.TrustReview.Acknowledgement.DependencyLockDigest != "" {
		t.Fatalf("trust review dependency lock digest = %#v, want empty after lock removal", detail.TrustReview.Acknowledgement)
	}
	if _, err := service.EnablePlugin(context.Background(), v2.PluginID, plugin.AdminPluginEnableRequest{
		Version:              v2.Version,
		UserGrantJSON:        grant,
		TrustAcknowledgement: detail.TrustReview.Acknowledgement,
	}); err != nil {
		t.Fatalf("EnablePlugin v2 with dependency lock removal acknowledgement: %v", err)
	}
}

func TestPluginServiceTrustAcceptanceHistoryGrantHashIsCanonical(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = false
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Installer.AllowUnsignedDev = true
	cfg.Plugins.Policy.AllowedCapabilities = []string{string(plugin.CapabilityProviderGenerate), string(plugin.CapabilityTurnRead)}
	service := &PluginService{infra: &Infra{Config: cfg, DB: db, Logger: logger, ProjectRoot: dir}}
	sourceDir := writeAdminFixturePluginVersionWithCapabilities(t, dir, "0.1.0", "Trust Hash Fixture", []plugin.Capability{plugin.CapabilityTurnRead, plugin.CapabilityProviderGenerate})

	installed, err := service.InstallLocal(context.Background(), plugin.AdminPluginInstallRequest{Path: sourceDir})
	if err != nil {
		t.Fatalf("InstallLocal: %v", err)
	}
	if _, err := service.EnablePlugin(context.Background(), installed.PluginID, plugin.AdminPluginEnableRequest{
		Version:       installed.Version,
		UserGrantJSON: `{"tier":"runtime_safe","capabilities":["provider.generate","turn.read"]}`,
	}); err != nil {
		t.Fatalf("EnablePlugin first: %v", err)
	}
	if _, err := service.EnablePlugin(context.Background(), installed.PluginID, plugin.AdminPluginEnableRequest{
		Version:       installed.Version,
		UserGrantJSON: `{"capabilities":["turn.read","provider.generate"],"tier":"runtime_safe"}`,
	}); err != nil {
		t.Fatalf("EnablePlugin second: %v", err)
	}

	history, err := db.ListPluginTrustAcceptanceHistory(context.Background(), installed.PluginID, 10)
	if err != nil {
		t.Fatalf("ListPluginTrustAcceptanceHistory: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history len = %d, want 2: %#v", len(history), history)
	}
	if history[0].UserGrantHash == "" || history[0].UserGrantHash != history[1].UserGrantHash {
		t.Fatalf("user grant hashes = %q/%q, want canonical match", history[0].UserGrantHash, history[1].UserGrantHash)
	}
	if history[0].HostPolicyFingerprint == "" || history[0].HostPolicyFingerprint != history[1].HostPolicyFingerprint {
		t.Fatalf("host policy fingerprints = %q/%q, want stable match", history[0].HostPolicyFingerprint, history[1].HostPolicyFingerprint)
	}
}

func TestPluginServiceInitialTrustAcceptanceHashBindsUserGrantHash(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = false
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Installer.AllowUnsignedDev = true
	service := &PluginService{infra: &Infra{Config: cfg, DB: db, Logger: logger, ProjectRoot: dir}}
	capabilities := []plugin.Capability{plugin.CapabilityTurnRead, plugin.CapabilityProviderGenerate}
	sourceDir := writeAdminFixturePluginVersionWithCapabilities(t, dir, "0.1.0", "Trust Initial Hash Fixture", capabilities)

	installed, err := service.InstallLocal(context.Background(), plugin.AdminPluginInstallRequest{Path: sourceDir})
	if err != nil {
		t.Fatalf("InstallLocal: %v", err)
	}
	grantJSON := `{"tier":"runtime_safe","capabilities":["turn.read"]}`
	if _, err := service.EnablePlugin(context.Background(), installed.PluginID, plugin.AdminPluginEnableRequest{
		Version:       installed.Version,
		UserGrantJSON: grantJSON,
	}); err != nil {
		t.Fatalf("EnablePlugin initial grant: %v", err)
	}
	state, err := db.GetPluginEnabledState(context.Background(), installed.PluginID)
	if err != nil {
		t.Fatalf("GetPluginEnabledState: %v", err)
	}
	manifest := adminTrustManifest(installed.Version, capabilities)
	grant, err := plugin.ValidateUserGrantForManifest(grantJSON, manifest)
	if err != nil {
		t.Fatalf("ValidateUserGrantForManifest: %v", err)
	}
	grantHash, err := plugin.HashUserGrant(grant)
	if err != nil {
		t.Fatalf("HashUserGrant: %v", err)
	}
	ack := plugin.BuildPluginTrustAcknowledgement(plugin.PluginTrustSubject{
		PluginID:                installed.PluginID,
		Version:                 installed.Version,
		PackageDigest:           installed.PackageDigest,
		ManifestDigest:          installed.ManifestDigest,
		SignatureStatus:         installed.SignatureStatus,
		PublisherID:             installed.PublisherID,
		RuntimeKind:             manifest.Runtime.Kind,
		Capabilities:            append([]plugin.Capability(nil), manifest.Access.Capabilities...),
		Hooks:                   append([]plugin.HookSpec(nil), manifest.Hooks...),
		DefaultToolExposure:     plugin.ExposureWork,
		DefaultInvocationPolicy: plugin.InvocationAsk,
	}, nil)
	ack.TargetUserGrantHash = grantHash
	wantHash, err := plugin.HashPluginTrustAcknowledgement(ack)
	if err != nil {
		t.Fatalf("HashPluginTrustAcknowledgement: %v", err)
	}
	if state.TrustAcknowledgementHash != wantHash {
		t.Fatalf("initial trust acknowledgement hash = %q, want grant-bound hash %q", state.TrustAcknowledgementHash, wantHash)
	}
}

func TestPluginServiceRequiresTrustAcknowledgementForHostPolicyDrift(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = false
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Installer.AllowUnsignedDev = true
	cfg.Plugins.Policy.AllowedCapabilities = []string{string(plugin.CapabilityTurnRead)}
	service := &PluginService{infra: &Infra{Config: cfg, DB: db, Logger: logger, ProjectRoot: dir}}
	sourceDir := writeAdminFixturePluginVersionWithCapabilities(t, dir, "0.1.0", "Trust Host Policy Fixture", []plugin.Capability{plugin.CapabilityTurnRead, plugin.CapabilityProviderGenerate})

	installed, err := service.InstallLocal(context.Background(), plugin.AdminPluginInstallRequest{Path: sourceDir})
	if err != nil {
		t.Fatalf("InstallLocal: %v", err)
	}
	grant := `{"tier":"runtime_safe","capabilities":["turn.read","provider.generate"]}`
	if _, err := service.EnablePlugin(context.Background(), installed.PluginID, plugin.AdminPluginEnableRequest{
		Version:       installed.Version,
		UserGrantJSON: grant,
	}); err != nil {
		t.Fatalf("EnablePlugin initial narrowed host policy: %v", err)
	}
	narrowedHistory, err := db.ListPluginTrustAcceptanceHistory(context.Background(), installed.PluginID, 10)
	if err != nil {
		t.Fatalf("ListPluginTrustAcceptanceHistory initial: %v", err)
	}
	if len(narrowedHistory) != 1 || narrowedHistory[0].HostPolicyFingerprint == "" {
		t.Fatalf("initial history = %#v, want one row with host policy fingerprint", narrowedHistory)
	}

	cfg.Plugins.Policy.AllowedCapabilities = []string{string(plugin.CapabilityTurnRead), string(plugin.CapabilityProviderGenerate)}
	detail, err := service.GetPluginVersionForGrant(context.Background(), installed.PluginID, installed.Version, grant)
	if err != nil {
		t.Fatalf("GetPluginVersionForGrant after host policy drift: %v", err)
	}
	if !detail.TrustReview.Required || strings.Join(detail.TrustReview.Reasons, ",") != "host_policy_changed" || detail.TrustReview.Acknowledgement == nil {
		t.Fatalf("trust review after host policy drift = %#v, want host_policy_changed acknowledgement", detail.TrustReview)
	}
	_, err = service.EnablePlugin(context.Background(), installed.PluginID, plugin.AdminPluginEnableRequest{
		Version:       installed.Version,
		UserGrantJSON: grant,
	})
	if err == nil || !strings.Contains(err.Error(), "plugin trust review required") {
		t.Fatalf("EnablePlugin after host policy drift without acknowledgement error = %v, want trust review required", err)
	}
	historyAfterReject, err := db.ListPluginTrustAcceptanceHistory(context.Background(), installed.PluginID, 10)
	if err != nil {
		t.Fatalf("ListPluginTrustAcceptanceHistory rejected: %v", err)
	}
	if len(historyAfterReject) != 1 {
		t.Fatalf("history after rejected drift enable = %#v, want unchanged", historyAfterReject)
	}

	if _, err := service.EnablePlugin(context.Background(), installed.PluginID, plugin.AdminPluginEnableRequest{
		Version:              installed.Version,
		UserGrantJSON:        grant,
		TrustAcknowledgement: detail.TrustReview.Acknowledgement,
	}); err != nil {
		t.Fatalf("EnablePlugin after host policy drift with acknowledgement: %v", err)
	}
	historyAfterAccept, err := db.ListPluginTrustAcceptanceHistory(context.Background(), installed.PluginID, 10)
	if err != nil {
		t.Fatalf("ListPluginTrustAcceptanceHistory accepted: %v", err)
	}
	if len(historyAfterAccept) != 2 {
		t.Fatalf("history after accepted drift enable len = %d, want 2: %#v", len(historyAfterAccept), historyAfterAccept)
	}
	var driftHistory *storage.PluginTrustAcceptanceHistoryRecord
	for i := range historyAfterAccept {
		if historyAfterAccept[i].ReviewReasonsJSON == `["host_policy_changed"]` {
			driftHistory = &historyAfterAccept[i]
			break
		}
	}
	if driftHistory == nil {
		t.Fatalf("history after accepted drift enable = %#v, want host_policy_changed row", historyAfterAccept)
	}
	if driftHistory.HostPolicyFingerprint == "" || driftHistory.HostPolicyFingerprint == narrowedHistory[0].HostPolicyFingerprint {
		t.Fatalf("host policy fingerprint after drift = %q, before = %q", driftHistory.HostPolicyFingerprint, narrowedHistory[0].HostPolicyFingerprint)
	}
	_, err = service.EnablePlugin(context.Background(), installed.PluginID, plugin.AdminPluginEnableRequest{
		Version:              installed.Version,
		UserGrantJSON:        grant,
		TrustAcknowledgement: detail.TrustReview.Acknowledgement,
	})
	if err == nil || !strings.Contains(err.Error(), "trust review acknowledgement is not required") {
		t.Fatalf("EnablePlugin replay consumed host policy acknowledgement error = %v, want not-required acknowledgement rejection", err)
	}
	historyAfterReplay, err := db.ListPluginTrustAcceptanceHistory(context.Background(), installed.PluginID, 10)
	if err != nil {
		t.Fatalf("ListPluginTrustAcceptanceHistory replay: %v", err)
	}
	if len(historyAfterReplay) != 2 {
		t.Fatalf("history after replay = %#v, want unchanged", historyAfterReplay)
	}
}

func TestPluginServiceRequiresTrustAcknowledgementForActiveHookPolicyDrift(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = false
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Installer.AllowUnsignedDev = true
	cfg.Plugins.Policy.AllowActiveHooks = false
	service := &PluginService{infra: &Infra{Config: cfg, DB: db, Logger: logger, ProjectRoot: dir}}
	sourceDir := writeAdminFixturePluginVersionWithHooks(t, dir, "0.1.0", "Trust Active Hook Fixture", []plugin.Capability{plugin.CapabilityTurnRead, plugin.CapabilityToolObserve}, []plugin.HookSpec{{
		Name:          plugin.HookBeforeToolCall,
		Mode:          plugin.HookModeTransform,
		FailurePolicy: plugin.FailurePolicyFailClosed,
		Priority:      100,
		TimeoutMS:     200,
	}})

	installed, err := service.InstallLocal(context.Background(), plugin.AdminPluginInstallRequest{Path: sourceDir})
	if err != nil {
		t.Fatalf("InstallLocal: %v", err)
	}
	grant := `{"tier":"runtime_safe","capabilities":["turn.read","tool.observe"]}`
	if _, err := service.EnablePlugin(context.Background(), installed.PluginID, plugin.AdminPluginEnableRequest{
		Version:       installed.Version,
		UserGrantJSON: grant,
	}); err != nil {
		t.Fatalf("EnablePlugin initial active-hook-denied policy: %v", err)
	}
	deniedHistory, err := db.ListPluginTrustAcceptanceHistory(context.Background(), installed.PluginID, 10)
	if err != nil {
		t.Fatalf("ListPluginTrustAcceptanceHistory initial: %v", err)
	}
	if len(deniedHistory) != 1 || deniedHistory[0].HostPolicyFingerprint == "" {
		t.Fatalf("initial history = %#v, want one row with host policy fingerprint", deniedHistory)
	}

	cfg.Plugins.Policy.AllowActiveHooks = true
	detail, err := service.GetPluginVersionForGrant(context.Background(), installed.PluginID, installed.Version, grant)
	if err != nil {
		t.Fatalf("GetPluginVersionForGrant after active hook policy drift: %v", err)
	}
	if !detail.TrustReview.Required || strings.Join(detail.TrustReview.Reasons, ",") != "active_hook_allowed:before_tool_call:transform" || detail.TrustReview.Acknowledgement == nil {
		t.Fatalf("trust review after active hook policy drift = %#v, want active_hook_allowed acknowledgement", detail.TrustReview)
	}
	_, err = service.EnablePlugin(context.Background(), installed.PluginID, plugin.AdminPluginEnableRequest{
		Version:       installed.Version,
		UserGrantJSON: grant,
	})
	if err == nil || !strings.Contains(err.Error(), "plugin trust review required") {
		t.Fatalf("EnablePlugin after active hook policy drift without acknowledgement error = %v, want trust review required", err)
	}

	if _, err := service.EnablePlugin(context.Background(), installed.PluginID, plugin.AdminPluginEnableRequest{
		Version:              installed.Version,
		UserGrantJSON:        grant,
		TrustAcknowledgement: detail.TrustReview.Acknowledgement,
	}); err != nil {
		t.Fatalf("EnablePlugin after active hook policy drift with acknowledgement: %v", err)
	}
	historyAfterAccept, err := db.ListPluginTrustAcceptanceHistory(context.Background(), installed.PluginID, 10)
	if err != nil {
		t.Fatalf("ListPluginTrustAcceptanceHistory accepted: %v", err)
	}
	if len(historyAfterAccept) != 2 {
		t.Fatalf("history after accepted active hook drift len = %d, want 2: %#v", len(historyAfterAccept), historyAfterAccept)
	}
	var driftHistory *storage.PluginTrustAcceptanceHistoryRecord
	for i := range historyAfterAccept {
		if historyAfterAccept[i].ReviewReasonsJSON == `["active_hook_allowed:before_tool_call:transform"]` {
			driftHistory = &historyAfterAccept[i]
			break
		}
	}
	if driftHistory == nil {
		t.Fatalf("history after accepted active hook drift = %#v, want active_hook_allowed row", historyAfterAccept)
	}
	if driftHistory.HostPolicyFingerprint == "" || driftHistory.HostPolicyFingerprint == deniedHistory[0].HostPolicyFingerprint {
		t.Fatalf("host policy fingerprint after active hook drift = %q, before = %q", driftHistory.HostPolicyFingerprint, deniedHistory[0].HostPolicyFingerprint)
	}
}

func TestPluginServiceRestartRequiresTrustAcknowledgementForActiveHookPolicyDrift(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = false
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Installer.AllowUnsignedDev = true
	cfg.Plugins.Policy.AllowActiveHooks = false
	service := &PluginService{infra: &Infra{Config: cfg, DB: db, Logger: logger, ProjectRoot: dir}}
	sourceDir := writeAdminFixturePluginVersionWithHooks(t, dir, "0.1.0", "Trust Active Hook Restart Fixture", []plugin.Capability{plugin.CapabilityTurnRead, plugin.CapabilityToolObserve}, []plugin.HookSpec{{
		Name:          plugin.HookBeforeToolCall,
		Mode:          plugin.HookModeTransform,
		FailurePolicy: plugin.FailurePolicyFailClosed,
		Priority:      100,
		TimeoutMS:     200,
	}})

	installed, err := service.InstallLocal(context.Background(), plugin.AdminPluginInstallRequest{Path: sourceDir})
	if err != nil {
		t.Fatalf("InstallLocal: %v", err)
	}
	grant := `{"tier":"runtime_safe","capabilities":["turn.read","tool.observe"]}`
	if _, err := service.EnablePlugin(context.Background(), installed.PluginID, plugin.AdminPluginEnableRequest{
		Version:       installed.Version,
		UserGrantJSON: grant,
	}); err != nil {
		t.Fatalf("EnablePlugin initial active-hook-denied policy: %v", err)
	}

	cfg.Plugins.Enabled = true
	cfg.Plugins.Policy.AllowActiveHooks = true
	_, err = service.RestartPlugin(context.Background(), installed.PluginID)
	if err == nil || !strings.Contains(err.Error(), "plugin trust review required") {
		t.Fatalf("RestartPlugin after active hook policy drift error = %v, want trust review required before runtime activation", err)
	}
	if service.registered[installed.PluginID] != "" {
		t.Fatalf("registered process plugin after rejected restart = %q, want not registered", service.registered[installed.PluginID])
	}
}

func TestPluginServiceLoadEnabledProcessPluginsRequiresTrustAcknowledgementForActiveHookPolicyDrift(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = false
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Installer.AllowUnsignedDev = true
	cfg.Plugins.Runtime.FailClosedIfUnavailable = true
	cfg.Plugins.Policy.AllowActiveHooks = false
	service := &PluginService{infra: &Infra{Config: cfg, DB: db, Logger: logger, ProjectRoot: dir}}
	sourceDir := writeAdminFixturePluginVersionWithHooks(t, dir, "0.1.0", "Trust Active Hook Startup Fixture", []plugin.Capability{plugin.CapabilityTurnRead, plugin.CapabilityToolObserve}, []plugin.HookSpec{{
		Name:          plugin.HookBeforeToolCall,
		Mode:          plugin.HookModeTransform,
		FailurePolicy: plugin.FailurePolicyFailClosed,
		Priority:      100,
		TimeoutMS:     200,
	}})

	installed, err := service.InstallLocal(context.Background(), plugin.AdminPluginInstallRequest{Path: sourceDir})
	if err != nil {
		t.Fatalf("InstallLocal: %v", err)
	}
	grant := `{"tier":"runtime_safe","capabilities":["turn.read","tool.observe"]}`
	if _, err := service.EnablePlugin(context.Background(), installed.PluginID, plugin.AdminPluginEnableRequest{
		Version:       installed.Version,
		UserGrantJSON: grant,
	}); err != nil {
		t.Fatalf("EnablePlugin initial active-hook-denied policy: %v", err)
	}

	cfg.Plugins.Enabled = true
	cfg.Plugins.Policy.AllowActiveHooks = true
	err = service.loadEnabledProcessPluginsLocked(context.Background())
	if err == nil || !strings.Contains(err.Error(), "plugin trust review required") {
		t.Fatalf("loadEnabledProcessPluginsLocked after active hook policy drift error = %v, want trust review required before runtime activation", err)
	}
	if service.registered[installed.PluginID] != "" {
		t.Fatalf("registered process plugin after rejected startup load = %q, want not registered", service.registered[installed.PluginID])
	}
}

func TestPluginServiceRequiresTrustAcknowledgementForSameVersionUserGrantExpansion(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = false
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Installer.AllowUnsignedDev = true
	service := &PluginService{infra: &Infra{Config: cfg, DB: db, Logger: logger, ProjectRoot: dir}}
	sourceDir := writeAdminFixturePluginVersionWithCapabilities(t, dir, "0.1.0", "Trust Grant Expansion Fixture", []plugin.Capability{plugin.CapabilityTurnRead, plugin.CapabilityProviderGenerate})

	installed, err := service.InstallLocal(context.Background(), plugin.AdminPluginInstallRequest{Path: sourceDir})
	if err != nil {
		t.Fatalf("InstallLocal: %v", err)
	}
	narrowGrant := `{"tier":"runtime_safe","capabilities":["turn.read"]}`
	if _, err := service.EnablePlugin(context.Background(), installed.PluginID, plugin.AdminPluginEnableRequest{
		Version:       installed.Version,
		UserGrantJSON: narrowGrant,
	}); err != nil {
		t.Fatalf("EnablePlugin initial narrow grant: %v", err)
	}
	initialHistory, err := db.ListPluginTrustAcceptanceHistory(context.Background(), installed.PluginID, 10)
	if err != nil {
		t.Fatalf("ListPluginTrustAcceptanceHistory initial: %v", err)
	}
	if len(initialHistory) != 1 || initialHistory[0].UserGrantHash == "" {
		t.Fatalf("initial history = %#v, want one row with user grant hash", initialHistory)
	}

	expandedGrant := `{"tier":"runtime_safe","capabilities":["turn.read","provider.generate"]}`
	detail, err := service.GetPluginVersionForGrant(context.Background(), installed.PluginID, installed.Version, expandedGrant)
	if err != nil {
		t.Fatalf("GetPluginVersionForGrant expanded: %v", err)
	}
	if !detail.TrustReview.Required || strings.Join(detail.TrustReview.Reasons, ",") != "user_grant_capability_added:provider.generate" || detail.TrustReview.Acknowledgement == nil {
		t.Fatalf("trust review after same-version grant expansion = %#v, want user_grant_capability_added acknowledgement", detail.TrustReview)
	}
	_, err = service.EnablePlugin(context.Background(), installed.PluginID, plugin.AdminPluginEnableRequest{
		Version:       installed.Version,
		UserGrantJSON: expandedGrant,
	})
	if err == nil || !strings.Contains(err.Error(), "plugin trust review required") {
		t.Fatalf("EnablePlugin expanded grant without acknowledgement error = %v, want trust review required", err)
	}
	historyAfterReject, err := db.ListPluginTrustAcceptanceHistory(context.Background(), installed.PluginID, 10)
	if err != nil {
		t.Fatalf("ListPluginTrustAcceptanceHistory rejected: %v", err)
	}
	if len(historyAfterReject) != 1 {
		t.Fatalf("history after rejected grant expansion = %#v, want unchanged", historyAfterReject)
	}

	if _, err := service.EnablePlugin(context.Background(), installed.PluginID, plugin.AdminPluginEnableRequest{
		Version:              installed.Version,
		UserGrantJSON:        expandedGrant,
		TrustAcknowledgement: detail.TrustReview.Acknowledgement,
	}); err != nil {
		t.Fatalf("EnablePlugin expanded grant with acknowledgement: %v", err)
	}
	historyAfterAccept, err := db.ListPluginTrustAcceptanceHistory(context.Background(), installed.PluginID, 10)
	if err != nil {
		t.Fatalf("ListPluginTrustAcceptanceHistory accepted: %v", err)
	}
	if len(historyAfterAccept) != 2 {
		t.Fatalf("history after accepted grant expansion len = %d, want 2: %#v", len(historyAfterAccept), historyAfterAccept)
	}
	var expansionHistory *storage.PluginTrustAcceptanceHistoryRecord
	for i := range historyAfterAccept {
		if historyAfterAccept[i].ReviewReasonsJSON == `["user_grant_capability_added:provider.generate"]` {
			expansionHistory = &historyAfterAccept[i]
			break
		}
	}
	if expansionHistory == nil {
		t.Fatalf("history after accepted grant expansion = %#v, want user_grant_capability_added row", historyAfterAccept)
	}
	if expansionHistory.UserGrantHash == "" || expansionHistory.UserGrantHash == initialHistory[0].UserGrantHash {
		t.Fatalf("user grant hash after expansion = %q, before = %q", expansionHistory.UserGrantHash, initialHistory[0].UserGrantHash)
	}
	detail, err = service.GetPluginVersionForGrant(context.Background(), installed.PluginID, installed.Version, narrowGrant)
	if err != nil {
		t.Fatalf("GetPluginVersionForGrant narrow after expansion: %v", err)
	}
	if detail.TrustReview.Required {
		t.Fatalf("trust review for grant narrowing = %#v, want no required review", detail.TrustReview)
	}
}

func TestPluginServiceTrustAcknowledgementBindsTargetUserGrantHash(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = false
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Installer.AllowUnsignedDev = true
	service := &PluginService{infra: &Infra{Config: cfg, DB: db, Logger: logger, ProjectRoot: dir}}
	sourceDir := writeAdminFixturePluginVersionWithCapabilities(t, dir, "0.1.0", "Trust Grant Target Fixture", []plugin.Capability{plugin.CapabilityTurnRead, plugin.CapabilityProviderGenerate})

	installed, err := service.InstallLocal(context.Background(), plugin.AdminPluginInstallRequest{Path: sourceDir})
	if err != nil {
		t.Fatalf("InstallLocal: %v", err)
	}
	narrowGrant := `{"tier":"runtime_safe","capabilities":["turn.read"]}`
	if _, err := service.EnablePlugin(context.Background(), installed.PluginID, plugin.AdminPluginEnableRequest{
		Version:       installed.Version,
		UserGrantJSON: narrowGrant,
	}); err != nil {
		t.Fatalf("EnablePlugin initial narrow grant: %v", err)
	}

	previewGrant := `{"tier":"runtime_safe","capabilities":["provider.generate"]}`
	targetGrant := `{"tier":"runtime_safe","capabilities":["turn.read","provider.generate"]}`
	detail, err := service.GetPluginVersionForGrant(context.Background(), installed.PluginID, installed.Version, previewGrant)
	if err != nil {
		t.Fatalf("GetPluginVersionForGrant preview: %v", err)
	}
	if !detail.TrustReview.Required || strings.Join(detail.TrustReview.Reasons, ",") != "user_grant_capability_added:provider.generate" || detail.TrustReview.Acknowledgement == nil {
		t.Fatalf("trust review for preview grant = %#v, want user_grant_capability_added acknowledgement", detail.TrustReview)
	}
	wantPreviewGrantHash, err := plugin.HashUserGrant(plugin.UserGrant{
		Tier:         plugin.AccessTierRuntimeSafe,
		Capabilities: []plugin.Capability{plugin.CapabilityProviderGenerate},
	})
	if err != nil {
		t.Fatalf("HashUserGrant preview: %v", err)
	}
	ackRaw, err := json.Marshal(detail.TrustReview.Acknowledgement)
	if err != nil {
		t.Fatalf("Marshal acknowledgement: %v", err)
	}
	var ackPayload map[string]any
	if err := json.Unmarshal(ackRaw, &ackPayload); err != nil {
		t.Fatalf("Unmarshal acknowledgement: %v", err)
	}
	if got, _ := ackPayload["target_user_grant_hash"].(string); got != wantPreviewGrantHash {
		t.Errorf("ack target_user_grant_hash = %q, want preview grant hash %q", got, wantPreviewGrantHash)
	}

	_, err = service.EnablePlugin(context.Background(), installed.PluginID, plugin.AdminPluginEnableRequest{
		Version:              installed.Version,
		UserGrantJSON:        targetGrant,
		TrustAcknowledgement: detail.TrustReview.Acknowledgement,
	})
	if err == nil {
		t.Fatalf("EnablePlugin reused preview acknowledgement for different target grant, want rejection")
	}
	historyAfterReject, err := db.ListPluginTrustAcceptanceHistory(context.Background(), installed.PluginID, 10)
	if err != nil {
		t.Fatalf("ListPluginTrustAcceptanceHistory rejected: %v", err)
	}
	if len(historyAfterReject) != 1 {
		t.Fatalf("history after rejected mismatched grant acknowledgement = %#v, want unchanged", historyAfterReject)
	}
	stateAfterReject, err := db.GetPluginEnabledState(context.Background(), installed.PluginID)
	if err != nil {
		t.Fatalf("GetPluginEnabledState after rejected mismatched grant acknowledgement: %v", err)
	}
	if stateAfterReject == nil || stateAfterReject.UserGrantJSON != narrowGrant {
		t.Fatalf("enabled state after rejected mismatched grant acknowledgement = %#v, want original grant %s", stateAfterReject, narrowGrant)
	}
}

func TestPluginServiceSameVersionUserGrantExpansionReasonsAreCanonical(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = false
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Installer.AllowUnsignedDev = true
	service := &PluginService{infra: &Infra{Config: cfg, DB: db, Logger: logger, ProjectRoot: dir}}
	sourceDir := writeAdminFixturePluginVersionWithCapabilities(t, dir, "0.1.0", "Trust Grant Canonical Fixture", []plugin.Capability{plugin.CapabilityTurnRead, plugin.CapabilityProviderGenerate})

	installed, err := service.InstallLocal(context.Background(), plugin.AdminPluginInstallRequest{Path: sourceDir})
	if err != nil {
		t.Fatalf("InstallLocal: %v", err)
	}
	if _, err := service.EnablePlugin(context.Background(), installed.PluginID, plugin.AdminPluginEnableRequest{
		Version:       installed.Version,
		UserGrantJSON: `{"tier":"runtime_safe","capabilities":["turn.read"]}`,
	}); err != nil {
		t.Fatalf("EnablePlugin initial narrow grant: %v", err)
	}
	detail, err := service.GetPluginVersionForGrant(context.Background(), installed.PluginID, installed.Version, `{"tier":"runtime_safe","capabilities":["provider.generate","turn.read","provider.generate"]}`)
	if err != nil {
		t.Fatalf("GetPluginVersionForGrant duplicate expansion: %v", err)
	}
	if strings.Join(detail.TrustReview.Reasons, ",") != "user_grant_capability_added:provider.generate" {
		t.Fatalf("grant expansion reasons = %#v, want one canonical provider.generate reason", detail.TrustReview.Reasons)
	}
}

func TestPluginServiceRequiresTrustAcknowledgementForLegacyHistoryWithoutHostPolicyFingerprint(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = false
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Installer.AllowUnsignedDev = true
	cfg.Plugins.Policy.AllowedCapabilities = []string{string(plugin.CapabilityTurnRead)}
	service := &PluginService{infra: &Infra{Config: cfg, DB: db, Logger: logger, ProjectRoot: dir}}
	sourceDir := writeAdminFixturePluginVersionWithCapabilities(t, dir, "0.1.0", "Legacy Trust Host Policy Fixture", []plugin.Capability{plugin.CapabilityTurnRead})

	installed, err := service.InstallLocal(context.Background(), plugin.AdminPluginInstallRequest{Path: sourceDir})
	if err != nil {
		t.Fatalf("InstallLocal: %v", err)
	}
	grant := `{"tier":"runtime_safe","capabilities":["turn.read"]}`
	legacyTrust := storage.PluginTrustAcceptanceRecord{
		TrustLevel:              string(plugin.TrustDeveloper),
		AcceptedAt:              "2026-06-22T10:00:00Z",
		AcknowledgementHash:     "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ReviewReasonsJSON:       "[]",
		DefaultToolExposure:     string(plugin.ExposureWork),
		DefaultInvocationPolicy: string(plugin.InvocationAsk),
	}
	if err := db.SetPluginEnabledWithTrust(context.Background(), installed.PluginID, installed.Version, true, grant, legacyTrust); err != nil {
		t.Fatalf("SetPluginEnabledWithTrust legacy: %v", err)
	}
	if err := db.RecordPluginTrustAcceptance(context.Background(), storage.PluginTrustAcceptanceHistoryRecord{
		PluginID:                installed.PluginID,
		Version:                 installed.Version,
		TrustLevel:              legacyTrust.TrustLevel,
		AcceptedAt:              legacyTrust.AcceptedAt,
		AcknowledgementHash:     legacyTrust.AcknowledgementHash,
		ReviewReasonsJSON:       legacyTrust.ReviewReasonsJSON,
		DefaultToolExposure:     legacyTrust.DefaultToolExposure,
		DefaultInvocationPolicy: legacyTrust.DefaultInvocationPolicy,
		PackageDigest:           installed.PackageDigest,
		ManifestDigest:          installed.ManifestDigest,
		SignatureStatus:         installed.SignatureStatus,
		PublisherID:             installed.PublisherID,
		SourceType:              installed.SourceType,
	}); err != nil {
		t.Fatalf("RecordPluginTrustAcceptance legacy: %v", err)
	}

	detail, err := service.GetPluginVersionForGrant(context.Background(), installed.PluginID, installed.Version, grant)
	if err != nil {
		t.Fatalf("GetPluginVersionForGrant legacy: %v", err)
	}
	if !detail.TrustReview.Required || strings.Join(detail.TrustReview.Reasons, ",") != "host_policy_changed" || detail.TrustReview.Acknowledgement == nil {
		t.Fatalf("trust review for legacy empty fingerprint = %#v, want host_policy_changed", detail.TrustReview)
	}
	if _, err := service.EnablePlugin(context.Background(), installed.PluginID, plugin.AdminPluginEnableRequest{
		Version:              installed.Version,
		UserGrantJSON:        grant,
		TrustAcknowledgement: detail.TrustReview.Acknowledgement,
	}); err != nil {
		t.Fatalf("EnablePlugin legacy with acknowledgement: %v", err)
	}
	detail, err = service.GetPluginVersion(context.Background(), installed.PluginID, installed.Version)
	if err != nil {
		t.Fatalf("GetPluginVersion legacy after reaccept: %v", err)
	}
	if detail.TrustReview.Required {
		t.Fatalf("trust review after legacy reaccept = %#v, want no required review", detail.TrustReview)
	}
}

func TestPluginServiceTrustAcknowledgementRequiresOneTimeIssuedNonce(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = false
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Installer.AllowUnsignedDev = true
	cfg.Plugins.Installer.RequireSignature = true
	service := &PluginService{infra: &Infra{Config: cfg, DB: db, Logger: logger, ProjectRoot: dir}}
	v1Dir := writeAdminFixturePluginVersionWithCapabilities(t, dir, "0.1.0", "Trust Nonce Fixture 0.1", []plugin.Capability{plugin.CapabilityTurnRead})
	v2Dir := writeAdminFixturePluginVersionWithCapabilities(t, dir, "0.2.0", "Trust Nonce Fixture 0.2", []plugin.Capability{plugin.CapabilityTurnRead, plugin.CapabilityProviderGenerate})

	v1, err := service.InstallLocal(context.Background(), plugin.AdminPluginInstallRequest{Path: v1Dir})
	if err != nil {
		t.Fatalf("InstallLocal v1: %v", err)
	}
	v2, err := service.InstallLocal(context.Background(), plugin.AdminPluginInstallRequest{Path: v2Dir})
	if err != nil {
		t.Fatalf("InstallLocal v2: %v", err)
	}
	if _, err := service.EnablePlugin(context.Background(), v1.PluginID, plugin.AdminPluginEnableRequest{
		Version:       v1.Version,
		UserGrantJSON: `{"tier":"runtime_safe","capabilities":["turn.read"]}`,
	}); err != nil {
		t.Fatalf("EnablePlugin v1: %v", err)
	}

	v2Grant := `{"tier":"runtime_safe","capabilities":["turn.read","provider.generate"]}`
	detail, err := service.GetPluginVersionForGrant(context.Background(), v2.PluginID, v2.Version, v2Grant)
	if err != nil {
		t.Fatalf("GetPluginVersionForGrant v2: %v", err)
	}
	ack := detail.TrustReview.Acknowledgement
	if ack == nil || ack.AckNonce == "" || ack.AckIssuedAt == "" || ack.UserAction != plugin.TrustAcknowledgementActionEnablePlugin {
		t.Fatalf("trust acknowledgement binding = %#v, want issued nonce, timestamp, and enable action", ack)
	}
	withoutNonce := *ack
	withoutNonce.AckNonce = ""
	if _, err := service.EnablePlugin(context.Background(), v2.PluginID, plugin.AdminPluginEnableRequest{
		Version:              v2.Version,
		UserGrantJSON:        v2Grant,
		TrustAcknowledgement: &withoutNonce,
	}); err == nil || !strings.Contains(err.Error(), "acknowledgement nonce") {
		t.Fatalf("EnablePlugin without nonce error = %v, want acknowledgement nonce error", err)
	}

	if _, err := service.EnablePlugin(context.Background(), v2.PluginID, plugin.AdminPluginEnableRequest{
		Version:              v2.Version,
		UserGrantJSON:        v2Grant,
		TrustAcknowledgement: ack,
	}); err != nil {
		t.Fatalf("EnablePlugin v2 with issued acknowledgement: %v", err)
	}
	if _, err := service.EnablePlugin(context.Background(), v1.PluginID, plugin.AdminPluginEnableRequest{
		Version:       v1.Version,
		UserGrantJSON: `{"tier":"runtime_safe","capabilities":["turn.read"]}`,
	}); err != nil {
		t.Fatalf("EnablePlugin rollback v1: %v", err)
	}
	if _, err := service.EnablePlugin(context.Background(), v2.PluginID, plugin.AdminPluginEnableRequest{
		Version:              v2.Version,
		UserGrantJSON:        v2Grant,
		TrustAcknowledgement: ack,
	}); err == nil || !strings.Contains(err.Error(), "not issued or has expired") {
		t.Fatalf("EnablePlugin replay acknowledgement error = %v, want one-time issued acknowledgement rejection", err)
	}
}

func TestPluginServiceRequiresTrustAcknowledgementForPublisherAndSignatureChange(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = false
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	service := &PluginService{infra: &Infra{Config: cfg, DB: db, Logger: logger, ProjectRoot: dir}}
	ctx := context.Background()
	v1Manifest := adminTrustManifest("0.1.0", []plugin.Capability{plugin.CapabilityTurnRead})
	v2Manifest := adminTrustManifest("0.2.0", []plugin.Capability{plugin.CapabilityTurnRead})
	v1JSON := marshalManifestForTest(t, v1Manifest)
	v2JSON := marshalManifestForTest(t, v2Manifest)
	for _, record := range []storage.PluginInstallation{
		{PluginID: v1Manifest.ID, Version: v1Manifest.Version, Name: v1Manifest.Name, ManifestJSON: v1JSON, SourceType: "local_zip", StorePath: filepath.Join(dir, "store", "v1"), PackageDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ManifestDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", SignatureStatus: plugin.SignatureStatusVerified, PublisherID: "publisher-a"},
		{PluginID: v2Manifest.ID, Version: v2Manifest.Version, Name: v2Manifest.Name, ManifestJSON: v2JSON, SourceType: "local_zip", StorePath: filepath.Join(dir, "store", "v2"), PackageDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", ManifestDigest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", SignatureStatus: plugin.SignatureStatusMissingSignature, PublisherID: "publisher-b"},
	} {
		if err := db.UpsertPluginInstallation(ctx, record); err != nil {
			t.Fatalf("UpsertPluginInstallation: %v", err)
		}
	}
	if err := db.SetPluginEnabled(ctx, v1Manifest.ID, v1Manifest.Version, true, `{"tier":"runtime_safe","capabilities":["turn.read"]}`); err != nil {
		t.Fatalf("SetPluginEnabled: %v", err)
	}

	v2Grant := `{"tier":"runtime_safe","capabilities":["turn.read"]}`
	_, err = service.EnablePlugin(ctx, v2Manifest.ID, plugin.AdminPluginEnableRequest{Version: v2Manifest.Version, UserGrantJSON: v2Grant})
	if err == nil || !strings.Contains(err.Error(), "plugin trust review required") {
		t.Fatalf("EnablePlugin v2 without trust acknowledgement error = %v, want trust review required", err)
	}
	detail, err := service.GetPluginVersionForGrant(ctx, v2Manifest.ID, v2Manifest.Version, v2Grant)
	if err != nil {
		t.Fatalf("GetPluginVersionForGrant v2: %v", err)
	}
	reasons := strings.Join(detail.TrustReview.Reasons, ",")
	for _, want := range []string{"publisher_changed", "signature_status_changed"} {
		if !strings.Contains(reasons, want) {
			t.Fatalf("trust review reasons = %#v, want %s", detail.TrustReview.Reasons, want)
		}
	}
	if detail.TrustLevel == plugin.TrustUserTrusted {
		t.Fatalf("detail trust_level = %q, unacknowledged target must not be user_trusted", detail.TrustLevel)
	}
	_, err = service.EnablePlugin(ctx, v2Manifest.ID, plugin.AdminPluginEnableRequest{
		Version:              v2Manifest.Version,
		UserGrantJSON:        v2Grant,
		TrustAcknowledgement: detail.TrustReview.Acknowledgement,
	})
	if err != nil {
		t.Fatalf("EnablePlugin v2 with trust acknowledgement: %v", err)
	}
}

func TestPluginServiceDoesNotRecordTrustAcceptanceHistoryWhenProcessRegistrationFails(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = true
	cfg.Plugins.BuiltinEnabled = nil
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Installer.AllowUnsignedDev = true
	cfg.Plugins.Runtime.FailClosedIfUnavailable = true
	cfg.Plugins.Runtime.PrivatePythonExecutable = ""
	cfg.Plugins.Runtime.PrivatePythonArtifactPath = ""
	cfg.Plugins.Runtime.PrivatePythonArtifactSHA256 = ""
	service := &PluginService{
		infra: &Infra{
			Config:      cfg,
			DB:          db,
			Logger:      logger,
			ProjectRoot: dir,
		},
		tools: &ToolService{registry: tool.NewRegistry()},
		host:  plugin.NewPluginHost(cfg.Plugins, nil, logger),
	}
	t.Cleanup(func() { _ = service.Close(context.Background()) })

	ctx := context.Background()
	sourceDir := writeAdminManagedProviderProcessPluginVersion(t, dir, "0.1.0", "provider_ping", "fail")
	installed, err := service.InstallLocal(ctx, plugin.AdminPluginInstallRequest{Path: sourceDir})
	if err != nil {
		t.Fatalf("InstallLocal: %v", err)
	}
	_, err = service.EnablePlugin(ctx, installed.PluginID, plugin.AdminPluginEnableRequest{
		Version:       installed.Version,
		UserGrantJSON: `{"tier":"runtime_safe","capabilities":["turn.read","tool.register","provider.generate"]}`,
	})
	if err == nil || !strings.Contains(err.Error(), "python toolchain is not configured") {
		t.Fatalf("EnablePlugin error = %v, want missing python toolchain", err)
	}
	history, err := db.ListPluginTrustAcceptanceHistory(ctx, installed.PluginID, 10)
	if err != nil {
		t.Fatalf("ListPluginTrustAcceptanceHistory: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("history = %#v, want none after failed process registration", history)
	}
	state, err := db.GetPluginEnabledState(ctx, installed.PluginID)
	if err != nil {
		t.Fatalf("GetPluginEnabledState: %v", err)
	}
	if state == nil || state.Enabled || state.TrustAcceptedAt != "" || state.TrustAcknowledgementHash != "" {
		t.Fatalf("state after failed process registration = %#v, want disabled with cleared acceptance", state)
	}
}

func TestPluginServiceSummaryShowsEffectiveHostAPIAndToolPolicy(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = false
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Installer.AllowUnsignedDev = true
	cfg.Plugins.Policy.AllowedCapabilities = []string{"provider.generate"}
	service := &PluginService{infra: &Infra{Config: cfg, DB: db, Logger: logger, ProjectRoot: dir}}
	ctx := context.Background()
	src := writeAdminFixturePluginVersionWithCapabilities(t, dir, "0.1.0", "Policy Summary", []plugin.Capability{plugin.CapabilityTurnRead, plugin.CapabilityProviderGenerate})

	installed, err := service.InstallLocal(ctx, plugin.AdminPluginInstallRequest{Path: src})
	if err != nil {
		t.Fatalf("InstallLocal: %v", err)
	}
	enabled, err := service.EnablePlugin(ctx, installed.PluginID, plugin.AdminPluginEnableRequest{
		Version:       installed.Version,
		UserGrantJSON: `{"tier":"runtime_safe","capabilities":["turn.read","provider.generate"]}`,
	})
	if err != nil {
		t.Fatalf("EnablePlugin: %v", err)
	}

	if enabled.HostAPIPolicy.HostPolicyMode != "allowlist" {
		t.Fatalf("host policy mode = %q, want allowlist", enabled.HostAPIPolicy.HostPolicyMode)
	}
	if got := strings.Join(capabilitiesToStrings(enabled.HostAPIPolicy.EffectiveCapabilities), ","); got != "provider.generate" {
		t.Fatalf("effective capabilities = %q, want provider.generate", got)
	}
	if got := strings.Join(capabilitiesToStrings(enabled.HostAPIPolicy.UserGrantedCapabilities), ","); got != "turn.read,provider.generate" {
		t.Fatalf("user granted capabilities = %q", got)
	}
	if enabled.ToolPolicy.DefaultExposure != plugin.ExposureWork || enabled.ToolPolicy.DefaultInvocation != plugin.InvocationAsk {
		t.Fatalf("tool policy = %#v, want Host-derived work + ask", enabled.ToolPolicy)
	}
	if enabled.HookPolicy.AllowActiveHooks {
		t.Fatalf("hook policy allow_active_hooks = true, want default false")
	}
	if got := strings.Join(hookNamesToStrings(enabled.HookPolicy.ObserveHooks), ","); got != "after_turn_end" {
		t.Fatalf("observe hooks = %q, want after_turn_end", got)
	}
	if len(enabled.HookPolicy.ActiveHooks) != 0 {
		t.Fatalf("active hooks = %#v, want none", enabled.HookPolicy.ActiveHooks)
	}
}

func TestPluginServiceSummaryShowsDependencyLockTransparency(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = false
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Installer.AllowUnsignedDev = true
	service := &PluginService{infra: &Infra{Config: cfg, DB: db, Logger: logger, ProjectRoot: dir}}
	ctx := context.Background()
	src := writeAdminFixturePluginVersionWithCapabilities(t, dir, "0.1.0", "Dependency Summary", []plugin.Capability{plugin.CapabilityTurnRead})
	lockDigest := writeAdminDependencyLock(t, src, `VALUE = "summary"`)

	installed, err := service.InstallLocal(ctx, plugin.AdminPluginInstallRequest{Path: src})
	if err != nil {
		t.Fatalf("InstallLocal: %v", err)
	}
	if !installed.DependencySummary.Present ||
		installed.DependencySummary.LockDigest != lockDigest ||
		installed.DependencySummary.PackageCount != 1 ||
		len(installed.DependencySummary.Packages) != 1 {
		t.Fatalf("dependency summary = %#v, want lock digest and one dependency", installed.DependencySummary)
	}
	pkg := installed.DependencySummary.Packages[0]
	if pkg.Name != "depmod" || pkg.Kind != "python_module_zip" || pkg.Path != "deps/depmod.zip" || !strings.HasPrefix(pkg.SHA256, "sha256:") {
		t.Fatalf("dependency package summary = %#v", pkg)
	}
}

func TestPluginServiceDiagnosticsSummarizeRuntimeCloseoutChecks(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = false
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Installer.AllowUnsignedDev = true
	service := &PluginService{infra: &Infra{Config: cfg, DB: db, Logger: logger, ProjectRoot: dir}}
	ctx := context.Background()
	src := writeAdminFixturePluginVersionWithCapabilities(t, dir, "0.1.0", "Diagnostics Summary", []plugin.Capability{plugin.CapabilityTurnRead})
	writeAdminDependencyLock(t, src, `VALUE = "diagnostics"`)
	installed, err := service.InstallLocal(ctx, plugin.AdminPluginInstallRequest{Path: src})
	if err != nil {
		t.Fatalf("InstallLocal: %v", err)
	}

	diagnostics, err := service.PluginDiagnostics(ctx)
	if err != nil {
		t.Fatalf("PluginDiagnostics: %v", err)
	}
	if diagnostics.Status != "warning" {
		t.Fatalf("diagnostics status = %q, want warning because a legacy dependency lock is present", diagnostics.Status)
	}
	checks := map[string]plugin.AdminPluginDiagnosticCheck{}
	for _, check := range diagnostics.Checks {
		checks[check.ID] = check
	}
	for _, id := range []string{"python_toolchain", "python_environments", "process_guard", "uv_project", "plugin_logs", "repair"} {
		if checks[id].ID == "" {
			t.Fatalf("diagnostics missing %q in %#v", id, diagnostics.Checks)
		}
	}
	if checks["python_toolchain"].Status != "ok" || !strings.Contains(checks["python_toolchain"].Message, "no managed Python plugin") {
		t.Fatalf("python toolchain diagnostic = %#v, want no managed plugin ok", checks["python_toolchain"])
	}
	if checks["python_environments"].Status != "ok" {
		t.Fatalf("environment diagnostic = %#v, want ok", checks["python_environments"])
	}
	if checks["uv_project"].Status != "warning" || checks["uv_project"].Details["legacy_locks"] != 1 {
		t.Fatalf("uv project diagnostic = %#v, want one legacy lock warning", checks["uv_project"])
	}
	if !strings.Contains(checks["repair"].Message, installed.PluginID) {
		t.Fatalf("repair diagnostic = %#v, want plugin id", checks["repair"])
	}
}

func TestPluginServiceEnableRejectsGrantOutsideManifest(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = false
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Installer.AllowUnsignedDev = true
	cfg.Plugins.Installer.RequireSignature = true
	service := &PluginService{
		infra: &Infra{
			Config:      cfg,
			DB:          db,
			Logger:      logger,
			ProjectRoot: dir,
		},
	}
	sourceDir := writeAdminFixturePlugin(t, dir)
	installed, err := service.InstallLocal(context.Background(), plugin.AdminPluginInstallRequest{Path: sourceDir})
	if err != nil {
		t.Fatalf("InstallLocal: %v", err)
	}

	tests := []struct {
		name  string
		grant string
		want  string
	}{
		{
			name:  "capability outside manifest",
			grant: `{"tier":"runtime_safe","capabilities":["turn.read","provider.generate"]}`,
			want:  "not declared by manifest",
		},
		{
			name:  "unknown capability",
			grant: `{"tier":"runtime_safe","capabilities":["does.not.exist"]}`,
			want:  "unknown capability",
		},
		{
			name:  "tier exceeds manifest",
			grant: `{"tier":"trusted","capabilities":["turn.read"]}`,
			want:  "does not match manifest tier",
		},
		{
			name:  "trust field is rejected",
			grant: `{"tier":"runtime_safe","capabilities":["turn.read"],"trust":"official"}`,
			want:  "unknown field",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.EnablePlugin(context.Background(), installed.PluginID, plugin.AdminPluginEnableRequest{
				UserGrantJSON: tt.grant,
			})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("EnablePlugin error = %v, want %q", err, tt.want)
			}
		})
	}
	state, err := db.GetPluginEnabledState(context.Background(), installed.PluginID)
	if err != nil {
		t.Fatalf("GetPluginEnabledState: %v", err)
	}
	if state != nil {
		t.Fatalf("plugin enabled state after invalid grants = %#v, want nil", state)
	}

	if _, err := service.EnablePlugin(context.Background(), installed.PluginID, plugin.AdminPluginEnableRequest{
		UserGrantJSON: `{}`,
	}); err != nil {
		t.Fatalf("EnablePlugin empty grant: %v", err)
	}
}

func TestPluginServiceHostRPCBindsPluginIdentity(t *testing.T) {
	db := openPluginServiceTestDB(t)
	ctx := context.Background()
	manifestA := plugin.ManifestV2{
		SchemaVersion:   plugin.ManifestSchemaV02,
		ID:              "com.example.a",
		Name:            "A",
		Version:         "0.1.0",
		EmoAgentVersion: ">=0.2.0",
		Runtime:         plugin.ManifestV2Runtime{Kind: plugin.RuntimePythonProcess, Entry: "main.py"},
		Access:          plugin.ManifestV2Access{Tier: plugin.AccessTierRuntimeSafe, Capabilities: []plugin.Capability{plugin.CapabilityPluginKV}},
	}
	manifestB := manifestA
	manifestB.ID = "com.example.b"
	for _, manifest := range []plugin.ManifestV2{manifestA, manifestB} {
		if err := db.SetPluginEnabled(ctx, manifest.ID, manifest.Version, true, `{"tier":"runtime_safe"}`); err != nil {
			t.Fatalf("SetPluginEnabled: %v", err)
		}
	}
	broker := plugin.NewFacadeBroker(db, nil)
	broker.AddPlugin(manifestA)
	broker.AddPlugin(manifestB)
	service := &PluginService{facadeBroker: broker}

	_, err := service.hostRPCHandler(ctx, manifestA.ID, "facade.call", json.RawMessage(`{"plugin_id":"com.example.b","method":"plugin.info","params":{}}`))
	if err == nil || err.Error() != "plugin_id mismatch" {
		t.Fatalf("hostRPCHandler impersonation error = %v, want plugin_id mismatch", err)
	}
	raw, err := service.hostRPCHandler(ctx, manifestA.ID, "facade.call", json.RawMessage(`{"method":"plugin.info","params":{}}`))
	if err != nil {
		t.Fatalf("hostRPCHandler bound call: %v", err)
	}
	if !strings.Contains(string(raw), "com.example.a") {
		t.Fatalf("bound response = %s", raw)
	}
}

func TestPluginServiceEnableDifferentVersionReplacesProcessRegistrations(t *testing.T) {
	dir := t.TempDir()
	service := newProcessPluginAdminService(t, dir)
	ctx := context.Background()
	v1Dir := writeAdminProcessPluginVersion(t, dir, "0.1.0", "echo_v1", "v1", "fail_closed")
	v2Dir := writeAdminProcessPluginVersion(t, dir, "0.2.0", "echo_v2", "v2", "fail_closed")

	v1, err := service.InstallLocal(ctx, plugin.AdminPluginInstallRequest{Path: v1Dir})
	if err != nil {
		t.Fatalf("InstallLocal v1: %v", err)
	}
	v2, err := service.InstallLocal(ctx, plugin.AdminPluginInstallRequest{Path: v2Dir})
	if err != nil {
		t.Fatalf("InstallLocal v2: %v", err)
	}
	if _, err := service.EnablePlugin(ctx, v1.PluginID, plugin.AdminPluginEnableRequest{
		Version:       v1.Version,
		UserGrantJSON: `{"tier":"runtime_safe","capabilities":["turn.read","tool.register"]}`,
	}); err != nil {
		t.Fatalf("EnablePlugin v1: %v", err)
	}
	if !processWorkToolRegistered(service, "plugin_com_example_admin_echo_v1") {
		t.Fatalf("v1 tool not registered")
	}

	if _, err := service.EnablePlugin(ctx, v2.PluginID, plugin.AdminPluginEnableRequest{
		Version:       v2.Version,
		UserGrantJSON: `{"tier":"runtime_safe","capabilities":["turn.read","tool.register"]}`,
	}); err != nil {
		t.Fatalf("EnablePlugin v2: %v", err)
	}

	if processWorkToolRegistered(service, "plugin_com_example_admin_echo_v1") {
		t.Fatalf("v1 tool still registered after v2 enable")
	}
	if !processWorkToolRegistered(service, "plugin_com_example_admin_echo_v2") {
		t.Fatalf("v2 tool not registered after v2 enable")
	}
	result, err := service.host.HookBus().Dispatch(ctx, plugin.HookAfterTurnEnd, plugin.HookContext{})
	if err != nil {
		t.Fatalf("Dispatch after v2 enable: %v", err)
	}
	if result.Annotations["runtime_version"] != "v2" {
		t.Fatalf("hook annotations = %#v, want v2", result.Annotations)
	}
	if spec, ok := service.tools.Registry().GetSpec("plugin_com_example_admin_echo_v2"); !ok || spec.Source.ProducerVersion != "0.2.0" {
		t.Fatalf("v2 source metadata = %#v, ok=%v", spec.Source, ok)
	}
}

func TestPluginServiceDisableUnregistersProcessHooksAndTools(t *testing.T) {
	dir := t.TempDir()
	service := newProcessPluginAdminService(t, dir)
	ctx := context.Background()
	sourceDir := writeAdminProcessPluginVersion(t, dir, "0.1.0", "echo_v1", "v1", "fail_closed")

	installed, err := service.InstallLocal(ctx, plugin.AdminPluginInstallRequest{Path: sourceDir})
	if err != nil {
		t.Fatalf("InstallLocal: %v", err)
	}
	if _, err := service.EnablePlugin(ctx, installed.PluginID, plugin.AdminPluginEnableRequest{
		Version:       installed.Version,
		UserGrantJSON: `{"tier":"runtime_safe","capabilities":["turn.read","tool.register"]}`,
	}); err != nil {
		t.Fatalf("EnablePlugin: %v", err)
	}
	if !processWorkToolRegistered(service, "plugin_com_example_admin_echo_v1") {
		t.Fatalf("process tool not registered before disable")
	}

	if _, err := service.DisablePlugin(ctx, installed.PluginID); err != nil {
		t.Fatalf("DisablePlugin: %v", err)
	}
	if processWorkToolRegistered(service, "plugin_com_example_admin_echo_v1") {
		t.Fatalf("process tool still registered after disable")
	}
	result, err := service.host.HookBus().Dispatch(ctx, plugin.HookAfterTurnEnd, plugin.HookContext{})
	if err != nil {
		t.Fatalf("Dispatch after disable: %v", err)
	}
	if len(result.Annotations) != 0 {
		t.Fatalf("hook annotations after disable = %#v, want none", result.Annotations)
	}
}

func TestPluginServiceToolPolicySummaryUsesProcessInvocationPolicy(t *testing.T) {
	dir := t.TempDir()
	service := newProcessPluginAdminService(t, dir)
	ctx := context.Background()
	sourceDir := writeAdminProcessPluginVersion(t, dir, "0.1.0", "echo_auto", "v1", "fail_closed")
	patchAdminProcessPluginToolInvocation(t, sourceDir, plugin.InvocationAuto)

	installed, err := service.InstallLocal(ctx, plugin.AdminPluginInstallRequest{Path: sourceDir})
	if err != nil {
		t.Fatalf("InstallLocal: %v", err)
	}
	enabled, err := service.EnablePlugin(ctx, installed.PluginID, plugin.AdminPluginEnableRequest{
		Version:       installed.Version,
		UserGrantJSON: `{"tier":"runtime_safe","capabilities":["turn.read","tool.register"]}`,
	})
	if err != nil {
		t.Fatalf("EnablePlugin: %v", err)
	}
	if len(enabled.ToolPolicy.RegisteredTools) != 1 {
		t.Fatalf("registered tools = %#v, want one process tool", enabled.ToolPolicy.RegisteredTools)
	}
	entry := enabled.ToolPolicy.RegisteredTools[0]
	if entry.Name != "echo_auto" || entry.HostInvocation != plugin.InvocationAuto {
		t.Fatalf("tool policy entry = %#v, want echo_auto with auto invocation", entry)
	}
}

func TestPluginServicePersistsProcessRuntimeStartStopEvidence(t *testing.T) {
	dir := t.TempDir()
	service := newProcessPluginAdminService(t, dir)
	ctx := context.Background()
	sourceDir := writeAdminProcessPluginVersion(t, dir, "0.1.0", "echo_v1", "v1", "fail_closed")

	installed, err := service.InstallLocal(ctx, plugin.AdminPluginInstallRequest{Path: sourceDir})
	if err != nil {
		t.Fatalf("InstallLocal: %v", err)
	}
	if _, err := service.EnablePlugin(ctx, installed.PluginID, plugin.AdminPluginEnableRequest{
		Version:       installed.Version,
		UserGrantJSON: `{"tier":"runtime_safe","capabilities":["turn.read","tool.register"]}`,
	}); err != nil {
		t.Fatalf("EnablePlugin: %v", err)
	}

	runtimeRecord, err := service.infra.DB.GetPluginRuntimeRecord(ctx, installed.PluginID)
	if err != nil {
		t.Fatalf("GetPluginRuntimeRecord after enable: %v", err)
	}
	if runtimeRecord == nil || runtimeRecord.Status != "running" || runtimeRecord.PID == nil || *runtimeRecord.PID <= 0 || runtimeRecord.LastStartedAt == "" || runtimeRecord.LastStoppedAt != "" {
		t.Fatalf("runtime record after enable = %#v, want running PID and start evidence", runtimeRecord)
	}

	if _, err := service.DisablePlugin(ctx, installed.PluginID); err != nil {
		t.Fatalf("DisablePlugin: %v", err)
	}
	runtimeRecord, err = service.infra.DB.GetPluginRuntimeRecord(ctx, installed.PluginID)
	if err != nil {
		t.Fatalf("GetPluginRuntimeRecord after disable: %v", err)
	}
	if runtimeRecord == nil || runtimeRecord.Status != "stopped" || runtimeRecord.PID != nil || runtimeRecord.LastStartedAt == "" || runtimeRecord.LastStoppedAt == "" {
		t.Fatalf("runtime record after disable = %#v, want stopped evidence with retained start time", runtimeRecord)
	}
}

func TestPluginServiceConfigureFailClosesEnabledProcessPluginRuntimeUnavailable(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = true
	cfg.Plugins.BuiltinEnabled = nil
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Runtime.FailClosedIfUnavailable = true
	cfg.Plugins.Runtime.PrivatePythonExecutable = ""
	cfg.Plugins.Runtime.PrivatePythonArtifactPath = ""
	cfg.Plugins.Runtime.PrivatePythonArtifactSHA256 = ""
	manifest := plugin.ManifestV2{
		SchemaVersion:   plugin.ManifestSchemaV02,
		ID:              "com.example.failclosed",
		Name:            "Fail Closed Fixture",
		Version:         "0.1.0",
		EmoAgentVersion: ">=0.2.0",
		Runtime:         plugin.ManifestV2Runtime{Kind: plugin.RuntimeManagedPythonProcess, Entry: "main.py"},
		Access:          plugin.ManifestV2Access{Tier: plugin.AccessTierRuntimeSafe, Capabilities: []plugin.Capability{plugin.CapabilityTurnRead}},
	}
	store, err := plugin.NewPluginStore(cfg.Plugins.Store.RootDir)
	if err != nil {
		t.Fatalf("NewPluginStore: %v", err)
	}
	packageDir, err := store.PackageDir(manifest.ID, manifest.Version)
	if err != nil {
		t.Fatalf("PackageDir: %v", err)
	}
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatalf("MkdirAll package dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "main.py"), []byte("print('should not start')\n"), 0o644); err != nil {
		t.Fatalf("WriteFile main.py: %v", err)
	}
	writeAdminMinimalUVProject(t, packageDir)
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("Marshal manifest: %v", err)
	}
	ctx := context.Background()
	if err := db.UpsertPluginInstallation(ctx, storage.PluginInstallation{
		ID:           manifest.ID + "@" + manifest.Version,
		PluginID:     manifest.ID,
		Version:      manifest.Version,
		Name:         manifest.Name,
		ManifestJSON: string(manifestJSON),
		SourceType:   "local",
		StorePath:    packageDir,
	}); err != nil {
		t.Fatalf("UpsertPluginInstallation: %v", err)
	}
	if err := db.SetPluginEnabled(ctx, manifest.ID, manifest.Version, true, `{"tier":"runtime_safe"}`); err != nil {
		t.Fatalf("SetPluginEnabled: %v", err)
	}
	service := &PluginService{
		infra: &Infra{
			Config:      cfg,
			DB:          db,
			Logger:      logger,
			ProjectRoot: dir,
		},
		tools: &ToolService{registry: tool.NewRegistry()},
	}
	t.Cleanup(func() { _ = service.Close(context.Background()) })

	err = service.Configure(ctx, nil, nil)
	if err == nil ||
		!strings.Contains(err.Error(), manifest.ID) ||
		!strings.Contains(err.Error(), "python toolchain is not configured") {
		t.Fatalf("Configure error = %v, want fail-closed missing python toolchain for %s", err, manifest.ID)
	}
}

func TestPluginServiceManagedPythonInstallUpdateRollbackProviderLoop(t *testing.T) {
	dir := t.TempDir()
	service := newManagedProviderPluginAdminService(t, dir)
	ctx := context.Background()
	fakeProvider := &adminFakeLLMClient{}
	attachAdminFakeProviderGateway(t, service, fakeProvider)
	v1Dir := writeAdminManagedProviderProcessPluginVersion(t, dir, "0.1.0", "provider_ping_v1", "v1")
	v2Dir := writeAdminManagedProviderProcessPluginVersion(t, dir, "0.2.0", "provider_ping_v2", "v2")

	v1, err := service.InstallLocal(ctx, plugin.AdminPluginInstallRequest{Path: v1Dir})
	if err != nil {
		t.Fatalf("InstallLocal v1: %v", err)
	}
	v2, err := service.InstallLocal(ctx, plugin.AdminPluginInstallRequest{Path: v2Dir})
	if err != nil {
		t.Fatalf("InstallLocal v2: %v", err)
	}
	grant := `{"tier":"runtime_safe","capabilities":["turn.read","tool.register","provider.generate"]}`
	if _, err := service.EnablePlugin(ctx, v1.PluginID, plugin.AdminPluginEnableRequest{Version: v1.Version, UserGrantJSON: grant}); err != nil {
		t.Fatalf("EnablePlugin v1: %v", err)
	}
	assertManagedProviderRuntime(t, service, v1.Version)
	assertManagedProviderHook(t, service, "v1")
	result := executeApprovedPluginToolForTest(t, ctx, service, "plugin_com_example_admin_provider_ping_v1", `{"text":"hello v1"}`)
	assertManagedProviderToolResult(t, result, "v1", "fake-model-v1")
	assertProviderUsageCount(t, service, ctx, v1.PluginID, 1)

	if _, err := service.EnablePlugin(ctx, v2.PluginID, plugin.AdminPluginEnableRequest{Version: v2.Version, UserGrantJSON: grant}); err != nil {
		t.Fatalf("EnablePlugin v2: %v", err)
	}
	if processWorkToolRegistered(service, "plugin_com_example_admin_provider_ping_v1") {
		t.Fatalf("v1 provider tool still registered after v2 enable")
	}
	assertManagedProviderRuntime(t, service, v2.Version)
	assertManagedProviderHook(t, service, "v2")
	result = executeApprovedPluginToolForTest(t, ctx, service, "plugin_com_example_admin_provider_ping_v2", `{"text":"hello v2"}`)
	assertManagedProviderToolResult(t, result, "v2", "fake-model-v2")
	assertProviderUsageCount(t, service, ctx, v2.PluginID, 2)

	if _, err := service.EnablePlugin(ctx, v1.PluginID, plugin.AdminPluginEnableRequest{Version: v1.Version, UserGrantJSON: grant}); err != nil {
		t.Fatalf("EnablePlugin rollback v1: %v", err)
	}
	if processWorkToolRegistered(service, "plugin_com_example_admin_provider_ping_v2") {
		t.Fatalf("v2 provider tool still registered after v1 rollback")
	}
	assertManagedProviderRuntime(t, service, v1.Version)
	assertManagedProviderHook(t, service, "v1")
	result = executeApprovedPluginToolForTest(t, ctx, service, "plugin_com_example_admin_provider_ping_v1", `{"text":"rollback v1"}`)
	assertManagedProviderToolResult(t, result, "v1", "fake-model-v1")
	assertProviderUsageCount(t, service, ctx, v1.PluginID, 3)

	if len(fakeProvider.calls) != 3 {
		t.Fatalf("provider calls = %d, want 3", len(fakeProvider.calls))
	}
}

func TestPluginServiceFacadeHostPolicyNarrowsProviderCapability(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Policy.AllowedCapabilities = []string{"plugin.kv"}
	service := &PluginService{
		infra: &Infra{
			Config:      cfg,
			DB:          db,
			Logger:      logger,
			ProjectRoot: dir,
		},
	}
	t.Cleanup(func() { _ = service.Close(context.Background()) })
	if err := service.ensureRuntimeLocked(); err != nil {
		t.Fatalf("ensureRuntimeLocked: %v", err)
	}

	ctx := context.Background()
	manifest := plugin.ManifestV2{
		SchemaVersion:   plugin.ManifestSchemaV02,
		ID:              "com.example.policy",
		Name:            "Policy Fixture",
		Version:         "0.1.0",
		EmoAgentVersion: ">=0.2.0",
		Runtime:         plugin.ManifestV2Runtime{Kind: plugin.RuntimeManagedPythonProcess, Entry: "main.py"},
		Access:          plugin.ManifestV2Access{Tier: plugin.AccessTierRuntimeSafe, Capabilities: []plugin.Capability{plugin.CapabilityProviderGenerate}},
	}
	service.facadeBroker.AddPlugin(manifest)
	if err := db.SetPluginEnabled(ctx, manifest.ID, manifest.Version, true, `{"tier":"runtime_safe","capabilities":["provider.generate"]}`); err != nil {
		t.Fatalf("SetPluginEnabled: %v", err)
	}

	_, err = service.hostRPCHandler(ctx, manifest.ID, "facade.call", json.RawMessage(`{"method":"provider.generate","params":{"messages":[]}}`))
	if err == nil || !strings.Contains(err.Error(), "host policy lacks provider.generate") {
		t.Fatalf("hostRPCHandler error = %v, want host policy denial", err)
	}
	events, err := db.ListPluginAccessEvents(ctx, manifest.ID, 10)
	if err != nil {
		t.Fatalf("ListPluginAccessEvents: %v", err)
	}
	if len(events) != 1 || events[0].Status != "denied" || events[0].Capability != string(plugin.CapabilityProviderGenerate) {
		t.Fatalf("events = %#v, want denied provider.generate audit", events)
	}
}

func TestPluginServiceProcessActiveHookDeniedByDefault(t *testing.T) {
	dir := t.TempDir()
	service := newProcessPluginAdminService(t, dir)
	manifest := plugin.ManifestV2{
		SchemaVersion:   plugin.ManifestSchemaV02,
		ID:              "com.example.activehook",
		Name:            "Active Hook Fixture",
		Version:         "0.1.0",
		EmoAgentVersion: ">=0.2.0",
		Runtime:         plugin.ManifestV2Runtime{Kind: plugin.RuntimeManagedPythonProcess, Entry: "main.py"},
		Access:          plugin.ManifestV2Access{Tier: plugin.AccessTierRuntimeSafe, Capabilities: []plugin.Capability{plugin.CapabilityToolObserve}},
		Hooks: []plugin.HookSpec{{
			Name:          plugin.HookBeforeToolCall,
			Mode:          plugin.HookModeTransform,
			FailurePolicy: plugin.FailurePolicyFailClosed,
			Priority:      100,
			TimeoutMS:     200,
		}},
	}

	err := service.registerProcessPluginLocked(context.Background(), manifest)
	if err == nil || !strings.Contains(err.Error(), "active hooks are disabled by host policy") {
		t.Fatalf("registerProcessPluginLocked error = %v, want active hook host policy denial", err)
	}
}

func TestPluginServiceBlocksConfiguredProviderKeyEnvNames(t *testing.T) {
	db := openPluginServiceTestDB(t)
	if err := db.UpsertLLMProvider(config.LLMProvider{ID: "db-provider", Name: "DB", Protocol: "openai_compatible", APIKeyEnv: "CUSTOM_SECRETLESS_ENV", Enabled: true}); err != nil {
		t.Fatalf("UpsertLLMProvider: %v", err)
	}
	if err := db.UpsertRuntimeSetting("websearch", "config", `{"api_key_env":"RUNTIME_SEARCH_KEY","pipeline":{"rerank":{"api_key_env":"RUNTIME_RERANK_KEY"}}}`, "test"); err != nil {
		t.Fatalf("UpsertRuntimeSetting websearch: %v", err)
	}
	if err := db.UpsertRuntimeSetting("memory", "config", `{"extraction":{"provider":{"api_key_env":"RUNTIME_MEMORY_KEY"}}}`, "test"); err != nil {
		t.Fatalf("UpsertRuntimeSetting memory: %v", err)
	}
	cfg := config.DefaultConfig()
	cfg.LLMProviders = []config.LLMProvider{{ID: "file-provider", Name: "File", Protocol: "openai_compatible", APIKeyEnv: "FILE_ONLY_KEY", Enabled: true}}
	cfg.Memory.Extraction.Provider.APIKeyEnv = "MEMORY_EXTRACT_KEY"
	cfg.WebSearch.APIKeyEnv = "SEARCH_KEY"
	cfg.WebSearch.Pipeline.Rerank.APIKeyEnv = "RERANK_KEY"
	cfg.WebFetch.APIKeyEnv = "FETCH_KEY"
	service := &PluginService{infra: &Infra{Config: cfg, DB: db}}
	got := strings.Join(service.pluginBlockedEnvNames(), "\n")
	for _, want := range []string{"CUSTOM_SECRETLESS_ENV", "FETCH_KEY", "FILE_ONLY_KEY", "MEMORY_EXTRACT_KEY", "RERANK_KEY", "SEARCH_KEY"} {
		if !strings.Contains(got, want) {
			t.Fatalf("pluginBlockedEnvNames = %s, want %s", got, want)
		}
	}
	runtimeGot := strings.Join(service.pluginRuntimeBlockedEnvNames(), "\n")
	for _, want := range []string{"CUSTOM_SECRETLESS_ENV", "RUNTIME_MEMORY_KEY", "RUNTIME_RERANK_KEY", "RUNTIME_SEARCH_KEY"} {
		if !strings.Contains(runtimeGot, want) {
			t.Fatalf("pluginRuntimeBlockedEnvNames = %s, want %s", runtimeGot, want)
		}
	}
}

func TestPluginServiceProcessManifestKindsIncludeManagedPython(t *testing.T) {
	tests := []struct {
		kind plugin.RuntimeKind
		want bool
	}{
		{plugin.RuntimeManagedPythonProcess, true},
		{plugin.RuntimePythonProcess, true},
		{plugin.RuntimeProcess, true},
		{plugin.RuntimeContainer, false},
		{plugin.RuntimeBuiltin, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			manifest := plugin.ManifestV2{Runtime: plugin.ManifestV2Runtime{Kind: tt.kind}}
			if got := isProcessManifest(manifest); got != tt.want {
				t.Fatalf("isProcessManifest(%q) = %v, want %v", tt.kind, got, tt.want)
			}
		})
	}
}

func TestPluginServiceProcessEnvUsesSDKPathWithoutInheritedPythonPath(t *testing.T) {
	dir := t.TempDir()
	sdkPath := filepath.Join(dir, "sdk", "python")
	if err := os.MkdirAll(sdkPath, 0o755); err != nil {
		t.Fatalf("MkdirAll sdk path: %v", err)
	}
	inherited := filepath.Join(dir, "host-pythonpath")
	t.Setenv("PYTHONPATH", inherited)
	service := &PluginService{
		infra: &Infra{ProjectRoot: dir},
	}

	env := service.pluginProcessEnv()
	if len(env) != 1 || env[0] != "PYTHONPATH="+sdkPath {
		t.Fatalf("pluginProcessEnv = %#v, want only sdk PYTHONPATH", env)
	}
}

func openPluginServiceTestDB(t *testing.T) *storage.DB {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func writeAdminDependencyLock(t *testing.T, packageDir, moduleSource string) string {
	t.Helper()
	artifactRel := filepath.ToSlash(filepath.Join("deps", "depmod.zip"))
	artifactPath := filepath.Join(packageDir, filepath.FromSlash(artifactRel))
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatalf("MkdirAll dependency zip dir: %v", err)
	}
	out, err := os.Create(artifactPath)
	if err != nil {
		t.Fatalf("Create dependency zip: %v", err)
	}
	archive := zip.NewWriter(out)
	writer, err := archive.Create("depmod.py")
	if err != nil {
		t.Fatalf("Create dependency zip entry: %v", err)
	}
	if _, err := writer.Write([]byte(moduleSource)); err != nil {
		t.Fatalf("Write dependency zip entry: %v", err)
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("Close dependency zip: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("Close dependency zip file: %v", err)
	}
	lockRaw := []byte(`{
  "version": 1,
  "packages": [
    {
      "name": "depmod",
      "kind": "python_module_zip",
      "path": "` + artifactRel + `",
      "sha256": "` + testSHA256Digest(readFileBytes(t, artifactPath)) + `"
    }
  ]
}`)
	if err := os.WriteFile(filepath.Join(packageDir, "emo_dependencies.lock.json"), lockRaw, 0o644); err != nil {
		t.Fatalf("write dependency lock: %v", err)
	}
	return testSHA256Digest(lockRaw)
}

func readFileBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	return data
}

func testSHA256Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func writeAdminFixturePlugin(t *testing.T, root string) string {
	t.Helper()
	return writeAdminFixturePluginVersion(t, root, "0.1.0", "Admin Fixture")
}

func writeAdminFixturePluginVersion(t *testing.T, root, version, name string) string {
	t.Helper()
	dir := filepath.Join(root, "fixture-plugin-"+strings.ReplaceAll(version, ".", "-"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := `schema_version: emoagent.plugin.v0.2
id: com.example.admin
name: ` + name + `
version: ` + version + `
emoagent_version: ">=0.2.0"
runtime:
  kind: python_process
  entry: main.py
access:
  tier: runtime_safe
  capabilities:
    - turn.read
hooks:
  - name: after_turn_end
    mode: observe
    failure_policy: fail_open
    priority: 100
    timeout_ms: 200
`
	if err := os.WriteFile(filepath.Join(dir, "emo_plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.py"), []byte("print('admin fixture')\n"), 0o644); err != nil {
		t.Fatalf("write main: %v", err)
	}
	return dir
}

func writeAdminFixtureSettingsPlugin(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "settings-plugin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := `schema_version: emoagent.plugin.v0.2
id: com.example.settings
name: Settings Fixture
version: 0.1.0
emoagent_version: ">=0.2.0"
runtime:
  kind: python_process
  entry: main.py
access:
  tier: runtime_safe
  capabilities:
    - plugin.kv
settings:
  key: settings
  schema:
    type: object
    required:
      - api_key
    properties:
      api_key:
        type: string
        title: API Key
        secret: true
      mode:
        type: string
        enum:
          - base
          - all
        enum_titles:
          base: Base
          all: All
        default: base
      retries:
        type: integer
      ratio:
        type: number
      enabled:
        type: boolean
hooks: []
`
	if err := os.WriteFile(filepath.Join(dir, "emo_plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.py"), []byte("print('settings fixture')\n"), 0o644); err != nil {
		t.Fatalf("write main: %v", err)
	}
	return dir
}

func writeAdminFixturePluginVersionWithCapabilities(t *testing.T, root, version, name string, capabilities []plugin.Capability) string {
	t.Helper()
	dir := filepath.Join(root, "trust-plugin-"+strings.ReplaceAll(version, ".", "-"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := adminTrustManifest(version, capabilities)
	manifest.Name = name
	manifestRaw, err := manifestYAMLForTest(manifest)
	if err != nil {
		t.Fatalf("manifestYAMLForTest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "emo_plugin.yaml"), manifestRaw, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.py"), []byte("print('trust fixture')\n"), 0o644); err != nil {
		t.Fatalf("write main.py: %v", err)
	}
	return dir
}

func writeAdminFixturePluginVersionWithHooks(t *testing.T, root, version, name string, capabilities []plugin.Capability, hooks []plugin.HookSpec) string {
	t.Helper()
	dir := filepath.Join(root, "trust-hook-plugin-"+strings.ReplaceAll(version, ".", "-"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := adminTrustManifest(version, capabilities)
	manifest.Name = name
	manifest.Hooks = hooks
	manifestRaw, err := manifestYAMLForTest(manifest)
	if err != nil {
		t.Fatalf("manifestYAMLForTest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "emo_plugin.yaml"), manifestRaw, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.py"), []byte("print('trust hook fixture')\n"), 0o644); err != nil {
		t.Fatalf("write main.py: %v", err)
	}
	return dir
}

func adminTrustManifest(version string, capabilities []plugin.Capability) plugin.ManifestV2 {
	return plugin.ManifestV2{
		SchemaVersion:   plugin.ManifestSchemaV02,
		ID:              "com.example.admin",
		Name:            "Trust Fixture " + version,
		Version:         version,
		EmoAgentVersion: ">=0.2.0",
		Runtime:         plugin.ManifestV2Runtime{Kind: plugin.RuntimePythonProcess, Entry: "main.py"},
		Access:          plugin.ManifestV2Access{Tier: plugin.AccessTierRuntimeSafe, Capabilities: capabilities},
		Hooks: []plugin.HookSpec{{
			Name:          plugin.HookAfterTurnEnd,
			Mode:          plugin.HookModeObserve,
			FailurePolicy: plugin.FailurePolicyFailOpen,
			Priority:      100,
			TimeoutMS:     200,
		}},
	}
}

func manifestYAMLForTest(manifest plugin.ManifestV2) ([]byte, error) {
	var b strings.Builder
	b.WriteString("schema_version: " + manifest.SchemaVersion + "\n")
	b.WriteString("id: " + manifest.ID + "\n")
	b.WriteString("name: " + manifest.Name + "\n")
	b.WriteString("version: " + manifest.Version + "\n")
	b.WriteString("emoagent_version: \"" + manifest.EmoAgentVersion + "\"\n")
	b.WriteString("runtime:\n")
	b.WriteString("  kind: " + string(manifest.Runtime.Kind) + "\n")
	b.WriteString("  entry: " + manifest.Runtime.Entry + "\n")
	b.WriteString("access:\n")
	b.WriteString("  tier: " + string(manifest.Access.Tier) + "\n")
	b.WriteString("  capabilities:\n")
	for _, capability := range manifest.Access.Capabilities {
		b.WriteString("    - " + string(capability) + "\n")
	}
	b.WriteString("hooks:\n")
	for _, hook := range manifest.Hooks {
		b.WriteString("  - name: " + string(hook.Name) + "\n")
		b.WriteString("    mode: " + string(hook.Mode) + "\n")
		b.WriteString("    failure_policy: " + string(hook.FailurePolicy) + "\n")
		b.WriteString(fmt.Sprintf("    priority: %d\n", hook.Priority))
		b.WriteString(fmt.Sprintf("    timeout_ms: %d\n", hook.TimeoutMS))
	}
	return []byte(b.String()), nil
}

func marshalManifestForTest(t *testing.T, manifest plugin.ManifestV2) string {
	t.Helper()
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("Marshal manifest: %v", err)
	}
	return string(raw)
}

func capabilitiesToStrings(capabilities []plugin.Capability) []string {
	values := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		values = append(values, string(capability))
	}
	return values
}

func hookNamesToStrings(hooks []plugin.HookName) []string {
	values := make([]string, 0, len(hooks))
	for _, hook := range hooks {
		values = append(values, string(hook))
	}
	return values
}

func newProcessPluginAdminService(t *testing.T, dir string) *PluginService {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = true
	cfg.Plugins.BuiltinEnabled = nil
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Installer.AllowUnsignedDev = true
	cfg.Plugins.Installer.RequireSignature = true
	cfg.Plugins.Runtime.PythonExecutable = findPythonForAppTest(t)
	cfg.Plugins.Runtime.StartupTimeoutMS = 3000
	cfg.Plugins.Runtime.ShutdownTimeoutMS = 1000
	cfg.Plugins.Runtime.MaxStderrBytes = 4096
	service := &PluginService{
		infra: &Infra{
			Config:      cfg,
			DB:          db,
			Logger:      logger,
			ProjectRoot: dir,
		},
		tools: &ToolService{registry: tool.NewRegistry()},
		host:  plugin.NewPluginHost(cfg.Plugins, nil, logger),
	}
	t.Cleanup(func() { _ = service.Close(context.Background()) })
	return service
}

func newManagedProviderPluginAdminService(t *testing.T, dir string) *PluginService {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(dir, "app.db"), logger)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = true
	cfg.Plugins.BuiltinEnabled = nil
	cfg.Plugins.Store.RootDir = filepath.Join(dir, "store")
	cfg.Plugins.Installer.AllowUnsignedDev = true
	cfg.Plugins.Installer.RequireSignature = true
	cfg.Plugins.Runtime.PythonExecutable = filepath.Join(dir, "legacy-python-must-not-be-used")
	cfg.Plugins.Runtime.StartupTimeoutMS = 3000
	cfg.Plugins.Runtime.ShutdownTimeoutMS = 1000
	cfg.Plugins.Runtime.MaxStderrBytes = 4096
	cfg.Plugins.ProviderGateway.Enabled = true
	service := &PluginService{
		infra: &Infra{
			Config:      cfg,
			DB:          db,
			Logger:      logger,
			ProjectRoot: dir,
		},
		tools: &ToolService{registry: tool.NewRegistry()},
		host:  plugin.NewPluginHost(cfg.Plugins, nil, logger),
	}
	t.Cleanup(func() { _ = service.Close(context.Background()) })
	return service
}

func attachAdminFakeProviderGateway(t *testing.T, service *PluginService, client llm.Client) {
	t.Helper()
	if err := service.ensureRuntimeLocked(); err != nil {
		t.Fatalf("ensureRuntimeLocked: %v", err)
	}
	python := findPythonForAppTest(t)
	service.supervisor.SetManagedPythonEnvironmentResolver(func(_ context.Context, manifest plugin.ManifestV2) (plugin.ManagedPythonEnvironment, error) {
		return plugin.ManagedPythonEnvironment{
			PythonExecutable: python,
			EnvironmentDir:   filepath.Join(service.infra.ProjectRoot, "test-python-envs", manifest.ID, manifest.Version),
		}, nil
	})
	gateway := plugin.NewProviderGateway(service.infra.DB, config.PluginProviderGatewayConfig{Enabled: true}, func(_ context.Context, providerID string) (llm.Client, error) {
		if providerID != "fake" {
			return nil, fmt.Errorf("providerID = %q, want fake", providerID)
		}
		return client, nil
	})
	broker := plugin.NewFacadeBroker(service.infra.DB, gateway)
	broker.SetStore(service.store)
	broker.SetHostPolicy(service.pluginFacadeHostPolicy())
	service.providerGateway = gateway
	service.facadeBroker = broker
	service.manager = plugin.NewManager(service.store, service.supervisor, broker, gateway)
}

type adminFakeLLMClient struct {
	calls []llm.ChatRequest
}

func (c *adminFakeLLMClient) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	c.calls = append(c.calls, req)
	return &llm.ChatResponse{
		Content:    "fake response for " + req.Model,
		Model:      req.Model,
		Usage:      llm.Usage{InputTokens: 3, OutputTokens: 5},
		StopReason: "end_turn",
	}, nil
}

func (c *adminFakeLLMClient) ChatStream(ctx context.Context, req llm.ChatRequest, _ llm.StreamCallback) (*llm.ChatResponse, error) {
	return c.Chat(ctx, req)
}

func executeApprovedPluginToolForTest(t *testing.T, ctx context.Context, service *PluginService, name string, input string) tool.Result {
	t.Helper()
	dispatcher := tool.NewDispatcher(service.tools.Registry(), tool.MinimalSchemaValidator{}, nil)
	call := tool.Call{ID: "call-" + strings.TrimPrefix(name, "plugin_com_example_admin_"), Name: name, Input: json.RawMessage(input)}
	result := dispatcher.Execute(ctx, call, tool.PermReadOnly)
	if !result.IsError || !result.NeedsApproval {
		t.Fatalf("initial tool result = %#v, want approval before plugin execution", result)
	}
	binding, err := tool.BuildApprovalBinding(call, "approval-"+call.ID, tool.ApprovalKindPluginInvocation)
	if err != nil {
		t.Fatalf("BuildApprovalBinding: %v", err)
	}
	approvedCtx := tool.WithApproval(ctx, tool.ApprovalContext{
		RequestID:           binding.RequestID,
		ApprovalKind:        binding.ApprovalKind,
		AllowToolCall:       true,
		ToolName:            binding.ToolName,
		NormalizedInputHash: binding.NormalizedInputHash,
		PathDigest:          binding.PathDigest,
	})
	result = dispatcher.Execute(approvedCtx, call, tool.PermReadOnly)
	if result.IsError {
		t.Fatalf("approved tool result = %#v", result)
	}
	return result
}

func assertManagedProviderToolResult(t *testing.T, result tool.Result, wantVersion, wantModel string) {
	t.Helper()
	if result.Envelope == nil {
		t.Fatalf("tool result envelope = nil")
	}
	labels := result.Envelope.Labels
	if labels.Executor != resultv2.ExecutorManagedPythonPlugin ||
		labels.Origin != resultv2.OriginPluginGenerated ||
		labels.Integrity != resultv2.IntegrityUnverified ||
		labels.InstructionAuthority != resultv2.InstructionDataOnly {
		t.Fatalf("result labels = %#v, want host-clamped managed plugin data-only labels", labels)
	}
	var payload struct {
		Version   string `json:"version"`
		Generated struct {
			Content string    `json:"content"`
			Model   string    `json:"model"`
			Usage   llm.Usage `json:"usage"`
		} `json:"generated"`
	}
	if err := json.Unmarshal(result.Content, &payload); err != nil {
		t.Fatalf("decode tool content %s: %v", result.Content, err)
	}
	if payload.Version != wantVersion || payload.Generated.Model != wantModel {
		t.Fatalf("tool content = %#v, want version %s model %s", payload, wantVersion, wantModel)
	}
	if !strings.Contains(payload.Generated.Content, wantModel) || payload.Generated.Usage.InputTokens != 3 || payload.Generated.Usage.OutputTokens != 5 {
		t.Fatalf("generated content = %#v", payload.Generated)
	}
}

func assertManagedProviderRuntime(t *testing.T, service *PluginService, wantVersion string) {
	t.Helper()
	status := service.supervisor.Status("com.example.admin")
	if status.RuntimeKind != plugin.RuntimeManagedPythonProcess || status.Version != wantVersion || status.Status != "running" {
		t.Fatalf("runtime status = %#v, want managed running version %s", status, wantVersion)
	}
	if status.PythonExecutableSource != plugin.PythonExecutableSourceToolchainUV {
		t.Fatalf("python source = %q, want %q", status.PythonExecutableSource, plugin.PythonExecutableSourceToolchainUV)
	}
	if status.PythonExecutablePath == "" || filepath.Base(status.PythonExecutablePath) == "legacy-python-must-not-be-used" {
		t.Fatalf("python path = %q, want resolver-provided environment python", status.PythonExecutablePath)
	}
	if status.PythonEnvironmentDir == "" || !strings.Contains(status.PythonEnvironmentDir, wantVersion) {
		t.Fatalf("python environment dir = %q, want version %s", status.PythonEnvironmentDir, wantVersion)
	}
	if status.PythonExecutableAvailable == nil || !*status.PythonExecutableAvailable {
		t.Fatalf("python available = %#v, want true", status.PythonExecutableAvailable)
	}
}

func assertManagedProviderHook(t *testing.T, service *PluginService, wantVersion string) {
	t.Helper()
	result, err := service.host.HookBus().Dispatch(context.Background(), plugin.HookAfterTurnEnd, plugin.HookContext{})
	if err != nil {
		t.Fatalf("Dispatch hook: %v", err)
	}
	if result.Annotations["runtime_version"] != wantVersion {
		t.Fatalf("hook annotations = %#v, want %s", result.Annotations, wantVersion)
	}
}

func assertProviderUsageCount(t *testing.T, service *PluginService, ctx context.Context, pluginID string, want int) {
	t.Helper()
	usages, err := service.infra.DB.ListPluginProviderUsage(ctx, pluginID, 10)
	if err != nil {
		t.Fatalf("ListPluginProviderUsage: %v", err)
	}
	if len(usages) != want {
		t.Fatalf("provider usage count = %d, want %d: %#v", len(usages), want, usages)
	}
	last := usages[0]
	if last.Status != "success" || last.ProviderID != "fake" || last.InputTokens != 3 || last.OutputTokens != 5 {
		t.Fatalf("latest provider usage = %#v, want successful fake usage", last)
	}
}

func processWorkToolRegistered(service *PluginService, name string) bool {
	for _, def := range service.tools.Registry().ForScope(tool.ScopeWork) {
		if def.Name == name {
			return true
		}
	}
	return false
}

func findPythonForAppTest(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"python", "python3"} {
		path, err := exec.LookPath(name)
		if err == nil {
			return path
		}
	}
	t.Skip("python executable not found")
	return ""
}

func writeAdminProcessPluginVersion(t *testing.T, root, version, toolName, annotation, failurePolicy string) string {
	t.Helper()
	dir := filepath.Join(root, "process-plugin-"+strings.ReplaceAll(version, ".", "-"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := `schema_version: emoagent.plugin.v0.2
id: com.example.admin
name: Process Admin Fixture ` + version + `
version: ` + version + `
emoagent_version: ">=0.2.0"
runtime:
  kind: python_process
  entry: main.py
access:
  tier: runtime_safe
  capabilities:
    - turn.read
    - tool.register
hooks:
  - name: after_turn_end
    mode: observe
    failure_policy: ` + failurePolicy + `
    priority: 100
    timeout_ms: 500
`
	if err := os.WriteFile(filepath.Join(dir, "emo_plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	source := adminProcessPluginSource(toolName, annotation)
	if err := os.WriteFile(filepath.Join(dir, "main.py"), []byte(source), 0o644); err != nil {
		t.Fatalf("write main.py: %v", err)
	}
	return dir
}

func writeAdminManagedProviderProcessPluginVersion(t *testing.T, root, version, toolName, annotation string) string {
	t.Helper()
	dir := filepath.Join(root, "managed-provider-plugin-"+strings.ReplaceAll(version, ".", "-"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	model := "fake-model-" + annotation
	manifest := `schema_version: emoagent.plugin.v0.2
id: com.example.admin
name: Managed Provider Fixture ` + version + `
version: ` + version + `
emoagent_version: ">=0.2.0"
runtime:
  kind: managed_python_process
  entry: main.py
access:
  tier: runtime_safe
  capabilities:
    - turn.read
    - tool.register
    - provider.generate
provider:
  default_provider_id: fake
  default_model: ` + model + `
  allowed_provider_ids:
    - fake
  allowed_models:
    - ` + model + `
hooks:
  - name: after_turn_end
    mode: observe
    failure_policy: fail_closed
    priority: 100
    timeout_ms: 500
`
	if err := os.WriteFile(filepath.Join(dir, "emo_plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	source := adminManagedProviderProcessPluginSource(toolName, annotation)
	if err := os.WriteFile(filepath.Join(dir, "main.py"), []byte(source), 0o644); err != nil {
		t.Fatalf("write main.py: %v", err)
	}
	writeAdminMinimalUVProject(t, dir)
	return dir
}

func writeAdminMinimalUVProject(t *testing.T, dir string) {
	t.Helper()
	pyproject := `[project]
name = "emoagent-test-plugin"
version = "0.1.0"
requires-python = ">=3.12,<3.13"
dependencies = []
`
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(pyproject), 0o644); err != nil {
		t.Fatalf("write pyproject.toml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "uv.lock"), []byte("# test lock placeholder\n"), 0o644); err != nil {
		t.Fatalf("write uv.lock: %v", err)
	}
}

func adminProcessPluginSource(toolName, annotation string) string {
	toolNameJSON, _ := json.Marshal(toolName)
	annotationJSON, _ := json.Marshal(annotation)
	return `import json
import sys

TOOL_NAME = ` + string(toolNameJSON) + `
ANNOTATION = ` + string(annotationJSON) + `

def send(value):
    sys.stdout.write(json.dumps(value, separators=(",", ":")) + "\n")
    sys.stdout.flush()

def result(request_id, value):
    send({"jsonrpc": "2.0", "id": request_id, "result": value})

for line in sys.stdin:
    request = json.loads(line)
    request_id = request.get("id")
    method = request.get("method")
    params = request.get("params") or {}
    if method == "initialize":
        result(request_id, {"tools": [{
            "name": TOOL_NAME,
            "description": "Echo test tool",
            "parameters": {"type": "object"},
            "scope": "both",
            "permission": "read-only"
        }]})
    elif method == "invoke_hook":
        result(request_id, {"Annotations": {"runtime_version": ANNOTATION}})
    elif method == "invoke_tool":
        result(request_id, {"ok": True, "input": params.get("input")})
    elif method == "shutdown":
        result(request_id, None)
        sys.exit(0)
    else:
        send({"jsonrpc": "2.0", "id": request_id, "error": {"code": -32601, "message": "unknown method"}})
`
}

func patchAdminProcessPluginToolInvocation(t *testing.T, dir string, invocation plugin.InvocationPolicy) {
	t.Helper()
	path := filepath.Join(dir, "main.py")
	raw := string(readFileBytes(t, path))
	needle := `            "permission": "read-only"`
	replacement := needle + `,` + "\n" + `            "invocation": "` + string(invocation) + `"`
	updated := strings.Replace(raw, needle, replacement, 1)
	if updated == raw {
		t.Fatalf("main.py fixture did not contain process tool permission marker")
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		t.Fatalf("write patched main.py: %v", err)
	}
}

func adminManagedProviderProcessPluginSource(toolName, annotation string) string {
	toolNameJSON, _ := json.Marshal(toolName)
	annotationJSON, _ := json.Marshal(annotation)
	return `import json
import sys

TOOL_NAME = ` + string(toolNameJSON) + `
ANNOTATION = ` + string(annotationJSON) + `
_host_id = 0

def send(value):
    sys.stdout.write(json.dumps(value, separators=(",", ":")) + "\n")
    sys.stdout.flush()

def result(request_id, value):
    send({"jsonrpc": "2.0", "id": request_id, "result": value})

def host_call(method, params):
    global _host_id
    _host_id += 1
    request_id = "host-" + str(_host_id)
    send({"jsonrpc": "2.0", "id": request_id, "method": "facade.call", "params": {"method": method, "params": params}})
    while True:
        line = sys.stdin.readline()
        if not line:
            raise RuntimeError("host closed while waiting for facade response")
        response = json.loads(line)
        if response.get("id") != request_id:
            continue
        if "error" in response:
            raise RuntimeError(response["error"].get("message", "facade call failed"))
        return response.get("result")

for line in sys.stdin:
    request = json.loads(line)
    request_id = request.get("id")
    method = request.get("method")
    params = request.get("params") or {}
    if method == "initialize":
        result(request_id, {"tools": [{
            "name": TOOL_NAME,
            "description": "Provider loop test tool",
            "parameters": {"type": "object"},
            "scope": "both",
            "permission": "read-only",
            "trust": {"integrity": "host_verified", "instruction_authority": "trusted"}
        }]})
    elif method == "invoke_hook":
        result(request_id, {"Annotations": {"runtime_version": ANNOTATION}})
    elif method == "invoke_tool":
        input_value = params.get("input") or {}
        generated = host_call("provider.generate", {
            "purpose": TOOL_NAME,
            "messages": [{"role": "user", "content": input_value.get("text", "")}],
            "max_tokens": 16
        })
        result(request_id, {"version": ANNOTATION, "generated": generated})
    elif method == "shutdown":
        result(request_id, None)
        sys.exit(0)
    else:
        send({"jsonrpc": "2.0", "id": request_id, "error": {"code": -32601, "message": "unknown method"}})
`
}
