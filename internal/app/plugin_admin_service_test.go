package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/plugin"
	"github.com/longyisang/emoagent/internal/storage"
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

func TestPluginServiceBlocksConfiguredProviderKeyEnvNames(t *testing.T) {
	db := openPluginServiceTestDB(t)
	if err := db.UpsertLLMProvider(config.LLMProvider{ID: "db-provider", Name: "DB", Protocol: "openai_compatible", APIKeyEnv: "CUSTOM_SECRETLESS_ENV", Enabled: true}); err != nil {
		t.Fatalf("UpsertLLMProvider: %v", err)
	}
	cfg := config.DefaultConfig()
	cfg.LLMProviders = []config.LLMProvider{{ID: "file-provider", Name: "File", Protocol: "openai_compatible", APIKeyEnv: "FILE_ONLY_KEY", Enabled: true}}
	service := &PluginService{infra: &Infra{Config: cfg, DB: db}}
	got := strings.Join(service.pluginBlockedEnvNames(), "\n")
	for _, want := range []string{"CUSTOM_SECRETLESS_ENV", "FILE_ONLY_KEY"} {
		if !strings.Contains(got, want) {
			t.Fatalf("pluginBlockedEnvNames = %s, want %s", got, want)
		}
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

func writeAdminFixturePlugin(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "fixture-plugin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := `schema_version: emoagent.plugin.v0.2
id: com.example.admin
name: Admin Fixture
version: 0.1.0
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
