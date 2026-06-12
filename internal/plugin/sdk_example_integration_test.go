package plugin

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/tool"
)

func TestSDKExamplePluginInstallEnableHookToolProviderAudit(t *testing.T) {
	python := findPythonForTest(t)
	db := openPluginTestDB(t)
	ctx := context.Background()
	store, err := NewPluginStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatalf("NewPluginStore: %v", err)
	}
	sourceDir, err := filepath.Abs(filepath.Join("..", "..", "sdk", "python", "examples", "echo_plugin"))
	if err != nil {
		t.Fatalf("Abs sourceDir: %v", err)
	}
	installer := NewPluginInstaller(store, config.PluginInstallerConfig{
		RequireSignature: true,
		AllowUnsignedDev: true,
	})
	result, err := installer.InstallFromDirectory(ctx, sourceDir)
	if err != nil {
		t.Fatalf("InstallFromDirectory: %v", err)
	}
	if result.SignatureStatus != SignatureStatusUnsignedDev {
		t.Fatalf("signature status = %q", result.SignatureStatus)
	}
	if err := db.UpsertPluginInstallation(ctx, storage.PluginInstallation{
		ID:              result.PluginID + "@" + result.Version,
		PluginID:        result.PluginID,
		Version:         result.Version,
		Name:            result.Name,
		ManifestJSON:    result.ManifestJSON,
		SourceType:      result.SourceType,
		SourceRef:       result.SourceRef,
		PackageDigest:   result.PackageDigest,
		ManifestDigest:  result.ManifestDigest,
		SignatureStatus: result.SignatureStatus,
		PublisherID:     result.PublisherID,
		InstalledBy:     "test",
		StorePath:       result.StorePath,
	}); err != nil {
		t.Fatalf("UpsertPluginInstallation: %v", err)
	}
	if err := db.SetPluginEnabled(ctx, result.PluginID, result.Version, true, `{"tier":"runtime_safe"}`); err != nil {
		t.Fatalf("SetPluginEnabled: %v", err)
	}

	fake := &fakePluginLLMClient{}
	gateway := NewProviderGateway(db, config.PluginProviderGatewayConfig{Enabled: true}, func(_ context.Context, providerID string) (llm.Client, error) {
		if providerID != "fake" {
			t.Fatalf("providerID = %q, want fake", providerID)
		}
		return fake, nil
	})
	broker := NewFacadeBroker(db, gateway)
	broker.AddPlugin(result.Manifest)
	hostHandler := func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
		if method != "facade.call" {
			return broker.Call(ctx, result.PluginID, method, params)
		}
		var req struct {
			PluginID string          `json:"plugin_id"`
			Method   string          `json:"method"`
			Params   json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, err
		}
		return broker.Call(ctx, req.PluginID, req.Method, req.Params)
	}
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:    true,
		PythonExecutable:  python,
		StartupTimeoutMS:  3000,
		ShutdownTimeoutMS: 1000,
		MaxStderrBytes:    8192,
	}, hostHandler)
	supervisor.SetEnabledChecker(func(context.Context, string) bool { return true })
	sdkPath, err := filepath.Abs(filepath.Join("..", "..", "sdk", "python"))
	if err != nil {
		t.Fatalf("Abs sdkPath: %v", err)
	}
	supervisor.SetAdditionalEnvVars([]string{"PYTHONPATH=" + sdkPath})
	t.Cleanup(func() { _ = supervisor.StopAll(context.Background()) })

	registry := NewPluginRegistry()
	toolRegistry := tool.NewRegistry()
	bus := NewHookBus(HookBusConfig{DefaultTimeout: 0}, nil)
	if err := RegisterProcessPlugin(ctx, result.Manifest, registry, toolRegistry, bus, supervisor); err != nil {
		t.Fatalf("RegisterProcessPlugin: %v", err)
	}

	hookResult, err := bus.Dispatch(ctx, HookAfterTurnEnd, HookContext{
		Turn: TurnView{TurnID: "turn-sdk"},
	})
	if err != nil {
		t.Fatalf("Dispatch hook: %v", err)
	}
	if hookResult.Annotations["echo_plugin"] != "observed:turn-sdk" {
		events, _ := db.ListPluginAccessEvents(ctx, result.PluginID, 10)
		t.Fatalf("hook annotations = %#v status=%#v events=%#v", hookResult.Annotations, supervisor.Status(result.PluginID), events)
	}

	dispatcher := tool.NewDispatcher(toolRegistry, tool.MinimalSchemaValidator{}, nil)
	call := tool.Call{ID: "call-1", Name: "plugin.com.example.echo.provider_ping", Input: json.RawMessage(`{"text":"hello"}`)}
	toolResult := dispatcher.Execute(ctx, call, tool.PermReadOnly)
	if toolResult.IsError || !strings.Contains(string(toolResult.Content), "fake response") {
		t.Fatalf("tool result = %#v", toolResult)
	}

	events, err := db.ListPluginAccessEvents(ctx, result.PluginID, 10)
	if err != nil {
		t.Fatalf("ListPluginAccessEvents: %v", err)
	}
	if len(events) < 3 {
		t.Fatalf("access events = %#v, want log/kv/provider events", events)
	}
	usages, err := db.ListPluginProviderUsage(ctx, result.PluginID, 10)
	if err != nil {
		t.Fatalf("ListPluginProviderUsage: %v", err)
	}
	if len(usages) != 1 || usages[0].Status != "success" || usages[0].ProviderID != "fake" {
		t.Fatalf("provider usage = %#v", usages)
	}
}
