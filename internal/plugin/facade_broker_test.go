package plugin

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/longyisang/emoagent/internal/storage"
)

func TestFacadeBrokerKVRecordsAccessEvents(t *testing.T) {
	db := openPluginTestDB(t)
	ctx := context.Background()
	manifest := facadeTestManifest([]Capability{CapabilityPluginKV})
	if err := db.SetPluginEnabled(ctx, manifest.ID, manifest.Version, true, facadeTestGrant(CapabilityPluginKV)); err != nil {
		t.Fatalf("SetPluginEnabled: %v", err)
	}
	broker := NewFacadeBroker(db, nil)
	broker.AddPlugin(manifest)

	if _, err := broker.Call(ctx, manifest.ID, "plugin.kv.set", json.RawMessage(`{"key":"seen","value":{"count":1}}`)); err != nil {
		t.Fatalf("Call set: %v", err)
	}
	raw, err := broker.Call(ctx, manifest.ID, "plugin.kv.get", json.RawMessage(`{"key":"seen"}`))
	if err != nil {
		t.Fatalf("Call get: %v", err)
	}
	if !strings.Contains(string(raw), `"count":1`) {
		t.Fatalf("get raw = %s", raw)
	}
	events, err := db.ListPluginAccessEvents(ctx, manifest.ID, 10)
	if err != nil {
		t.Fatalf("ListPluginAccessEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	for _, event := range events {
		if event.Status != "allowed" || event.Capability != string(CapabilityPluginKV) {
			t.Fatalf("event = %#v", event)
		}
	}
}

func TestFacadeBrokerDeniesMissingCapabilityAndRecordsAudit(t *testing.T) {
	db := openPluginTestDB(t)
	ctx := context.Background()
	manifest := facadeTestManifest([]Capability{CapabilityTurnRead})
	if err := db.SetPluginEnabled(ctx, manifest.ID, manifest.Version, true, facadeTestGrant(CapabilityTurnRead)); err != nil {
		t.Fatalf("SetPluginEnabled: %v", err)
	}
	broker := NewFacadeBroker(db, nil)
	broker.AddPlugin(manifest)

	_, err := broker.Call(ctx, manifest.ID, "plugin.kv.set", json.RawMessage(`{"key":"seen","value":true}`))
	if err == nil || !strings.Contains(err.Error(), "plugin capability denied") {
		t.Fatalf("Call error = %v, want capability denied", err)
	}
	events, err := db.ListPluginAccessEvents(ctx, manifest.ID, 10)
	if err != nil {
		t.Fatalf("ListPluginAccessEvents: %v", err)
	}
	if len(events) != 1 || events[0].Status != "denied" || events[0].Capability != string(CapabilityPluginKV) {
		t.Fatalf("events = %#v", events)
	}
}

func TestFacadeBrokerDeniesManifestCapabilityWithoutExplicitGrant(t *testing.T) {
	db := openPluginTestDB(t)
	ctx := context.Background()
	manifest := facadeTestManifest([]Capability{CapabilityPluginFiles, CapabilityProviderGenerate})
	if err := db.SetPluginEnabled(ctx, manifest.ID, manifest.Version, true, `{}`); err != nil {
		t.Fatalf("SetPluginEnabled: %v", err)
	}
	broker := NewFacadeBroker(db, nil)
	broker.AddPlugin(manifest)

	_, err := broker.Call(ctx, manifest.ID, "plugin.files.read_text", json.RawMessage(`{"path":"notes/today.txt"}`))
	if err == nil || !strings.Contains(err.Error(), "user grant lacks plugin.files") {
		t.Fatalf("plugin.files error = %v, want user grant denial", err)
	}
	_, err = broker.Call(ctx, manifest.ID, "provider.generate", json.RawMessage(`{"messages":[]}`))
	if err == nil || !strings.Contains(err.Error(), "user grant lacks provider.generate") {
		t.Fatalf("provider.generate error = %v, want user grant denial", err)
	}
}

func TestFacadeBrokerDeniesCapabilityOutsideHostPolicy(t *testing.T) {
	db := openPluginTestDB(t)
	ctx := context.Background()
	manifest := facadeTestManifest([]Capability{CapabilityPluginKV, CapabilityProviderGenerate})
	if err := db.SetPluginEnabled(ctx, manifest.ID, manifest.Version, true, facadeTestGrant(CapabilityPluginKV, CapabilityProviderGenerate)); err != nil {
		t.Fatalf("SetPluginEnabled: %v", err)
	}
	broker := NewFacadeBroker(db, nil)
	broker.SetHostPolicy(FacadeHostPolicy{AllowedCapabilities: []Capability{CapabilityPluginKV}})
	broker.AddPlugin(manifest)

	if _, err := broker.Call(ctx, manifest.ID, "plugin.kv.set", json.RawMessage(`{"key":"seen","value":true}`)); err != nil {
		t.Fatalf("plugin.kv.set: %v", err)
	}
	_, err := broker.Call(ctx, manifest.ID, "provider.generate", json.RawMessage(`{"messages":[]}`))
	if err == nil || !strings.Contains(err.Error(), "host policy lacks provider.generate") {
		t.Fatalf("provider.generate error = %v, want host policy denial", err)
	}
	events, err := db.ListPluginAccessEvents(ctx, manifest.ID, 10)
	if err != nil {
		t.Fatalf("ListPluginAccessEvents: %v", err)
	}
	var denied bool
	for _, event := range events {
		if event.Status == "denied" && event.Capability == string(CapabilityProviderGenerate) {
			denied = true
		}
	}
	if !denied {
		t.Fatalf("events = %#v, want denied provider.generate audit", events)
	}
}

func TestFacadeBrokerDeniesGrantTierMismatchFromStoredState(t *testing.T) {
	db := openPluginTestDB(t)
	ctx := context.Background()
	manifest := facadeTestManifest([]Capability{CapabilityPluginKV})
	if err := db.SetPluginEnabled(ctx, manifest.ID, manifest.Version, true, `{"tier":"trusted","capabilities":["plugin.kv"]}`); err != nil {
		t.Fatalf("SetPluginEnabled: %v", err)
	}
	broker := NewFacadeBroker(db, nil)
	broker.AddPlugin(manifest)

	_, err := broker.Call(ctx, manifest.ID, "plugin.kv.get", json.RawMessage(`{"key":"seen"}`))
	if err == nil || !strings.Contains(err.Error(), "does not match manifest tier") {
		t.Fatalf("Call error = %v, want tier mismatch", err)
	}
}

func TestFacadeBrokerDeniesActiveVersionMismatch(t *testing.T) {
	db := openPluginTestDB(t)
	ctx := context.Background()
	active := facadeTestManifest([]Capability{CapabilityPluginKV})
	registered := active
	registered.Version = "0.2.0"
	if err := db.SetPluginEnabled(ctx, active.ID, active.Version, true, facadeTestGrant(CapabilityPluginKV)); err != nil {
		t.Fatalf("SetPluginEnabled: %v", err)
	}
	broker := NewFacadeBroker(db, nil)
	broker.AddPlugin(registered)

	_, err := broker.Call(ctx, active.ID, "plugin.kv.get", json.RawMessage(`{"key":"seen"}`))
	if err == nil || !strings.Contains(err.Error(), "active version") {
		t.Fatalf("Call error = %v, want active version mismatch", err)
	}
}

func TestFacadeBrokerConcurrentCallAndRemovePlugin(t *testing.T) {
	ctx := context.Background()
	manifest := facadeTestManifest([]Capability{CapabilityTurnRead})
	broker := NewFacadeBroker(staticFacadeStorage{
		state: &storage.PluginEnabledState{
			PluginID:      manifest.ID,
			Version:       manifest.Version,
			Enabled:       true,
			UserGrantJSON: "{}",
		},
	}, nil)
	broker.AddPlugin(manifest)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_, _ = broker.Call(ctx, manifest.ID, "plugin.info", json.RawMessage(`{}`))
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 200; j++ {
			broker.RemovePlugin(manifest.ID)
			broker.AddPlugin(manifest)
		}
	}()
	wg.Wait()
}

func TestFacadeBrokerPluginFilesStayInPluginState(t *testing.T) {
	db := openPluginTestDB(t)
	ctx := context.Background()
	store, err := NewPluginStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatalf("NewPluginStore: %v", err)
	}
	manifest := facadeTestManifest([]Capability{CapabilityPluginFiles})
	if err := store.PrepareRuntimeDirs(manifest.ID); err != nil {
		t.Fatalf("PrepareRuntimeDirs: %v", err)
	}
	if err := db.SetPluginEnabled(ctx, manifest.ID, manifest.Version, true, facadeTestGrant(CapabilityPluginFiles)); err != nil {
		t.Fatalf("SetPluginEnabled: %v", err)
	}
	broker := NewFacadeBroker(db, nil)
	broker.SetStore(store)
	broker.AddPlugin(manifest)

	if _, err := broker.Call(ctx, manifest.ID, "plugin.files.write_text", json.RawMessage(`{"path":"notes/today.txt","content":"hello"}`)); err != nil {
		t.Fatalf("write_text: %v", err)
	}
	raw, err := broker.Call(ctx, manifest.ID, "plugin.files.read_text", json.RawMessage(`{"path":"notes/today.txt"}`))
	if err != nil {
		t.Fatalf("read_text: %v", err)
	}
	if !strings.Contains(string(raw), "hello") {
		t.Fatalf("read_text raw = %s", raw)
	}
	if _, err := broker.Call(ctx, manifest.ID, "plugin.files.read_text", json.RawMessage(`{"path":"../secret.txt"}`)); err == nil {
		t.Fatal("read_text ../ error = nil")
	}
}

func TestFacadeBrokerRejectsUnknownFacadeParams(t *testing.T) {
	db := openPluginTestDB(t)
	ctx := context.Background()
	manifest := facadeTestManifest([]Capability{CapabilityMemoryReadSafe})
	if err := db.SetPluginEnabled(ctx, manifest.ID, manifest.Version, true, facadeTestGrant(CapabilityMemoryReadSafe)); err != nil {
		t.Fatalf("SetPluginEnabled: %v", err)
	}
	broker := NewFacadeBroker(db, nil)
	broker.AddPlugin(manifest)

	_, err := broker.Call(ctx, manifest.ID, "memory.safe_context.current", json.RawMessage(`{"unexpected":true}`))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("Call error = %v, want unknown field", err)
	}
}

func TestFacadeBrokerReservesWebFacadeMethods(t *testing.T) {
	db := openPluginTestDB(t)
	ctx := context.Background()
	manifest := facadeTestManifest([]Capability{CapabilityNetworkWeb})
	if err := db.SetPluginEnabled(ctx, manifest.ID, manifest.Version, true, facadeTestGrant(CapabilityNetworkWeb)); err != nil {
		t.Fatalf("SetPluginEnabled: %v", err)
	}
	broker := NewFacadeBroker(db, nil)
	broker.AddPlugin(manifest)

	_, err := broker.Call(ctx, manifest.ID, "web.search", json.RawMessage(`{"query":"test"}`))
	if err == nil || !strings.Contains(err.Error(), "reserved but not implemented") {
		t.Fatalf("web.search error = %v, want reserved not implemented", err)
	}
	events, err := db.ListPluginAccessEvents(ctx, manifest.ID, 10)
	if err != nil {
		t.Fatalf("ListPluginAccessEvents: %v", err)
	}
	if len(events) != 1 || events[0].Capability != string(CapabilityNetworkWeb) {
		t.Fatalf("events = %#v", events)
	}
}

func openPluginTestDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"), slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func facadeTestManifest(capabilities []Capability) ManifestV2 {
	return ManifestV2{
		SchemaVersion:   ManifestSchemaV02,
		ID:              "com.example.echo",
		Name:            "Echo",
		Version:         "0.1.0",
		EmoAgentVersion: ">=0.2.0",
		Runtime:         ManifestV2Runtime{Kind: RuntimePythonProcess, Entry: "main.py"},
		Access: ManifestV2Access{
			Tier:         AccessTierRuntimeSafe,
			Capabilities: capabilities,
		},
		Hooks: []HookSpec{{Name: HookAfterTurnEnd, Mode: HookModeObserve, FailurePolicy: FailurePolicyFailOpen, TimeoutMS: 100}},
	}
}

func facadeTestGrant(capabilities ...Capability) string {
	values := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		values = append(values, string(capability))
	}
	raw, _ := json.Marshal(map[string]any{
		"tier":         AccessTierRuntimeSafe,
		"capabilities": values,
	})
	return string(raw)
}

type staticFacadeStorage struct {
	state *storage.PluginEnabledState
}

func (s staticFacadeStorage) GetPluginEnabledState(context.Context, string) (*storage.PluginEnabledState, error) {
	return s.state, nil
}

func (s staticFacadeStorage) RecordPluginAccessEvent(context.Context, storage.PluginAccessEvent) error {
	return nil
}

func (s staticFacadeStorage) PluginKVGet(context.Context, string, string) (string, bool, error) {
	return "", false, nil
}

func (s staticFacadeStorage) PluginKVSet(context.Context, string, string, string) error {
	return nil
}
