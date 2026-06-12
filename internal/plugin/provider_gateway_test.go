package plugin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
)

func TestProviderGatewayThroughFacadeBrokerRecordsUsage(t *testing.T) {
	db := openPluginTestDB(t)
	ctx := context.Background()
	manifest := facadeTestManifest([]Capability{CapabilityProviderGenerate})
	manifest.Provider.DefaultProviderID = "fake-provider"
	manifest.Provider.DefaultModel = "fake-model"
	if err := db.SetPluginEnabled(ctx, manifest.ID, manifest.Version, true, `{"tier":"runtime_safe"}`); err != nil {
		t.Fatalf("SetPluginEnabled: %v", err)
	}
	fake := &fakePluginLLMClient{}
	gateway := NewProviderGateway(db, config.PluginProviderGatewayConfig{Enabled: true}, func(_ context.Context, providerID string) (llm.Client, error) {
		if providerID != "fake-provider" {
			t.Fatalf("providerID = %q, want fake-provider", providerID)
		}
		return fake, nil
	})
	broker := NewFacadeBroker(db, gateway)
	broker.AddPlugin(manifest)

	raw, err := broker.Call(ctx, manifest.ID, "provider.generate", json.RawMessage(`{
		"purpose":"unit-test",
		"system":"short system",
		"messages":[{"role":"user","content":"hello"}],
		"max_tokens":32
	}`))
	if err != nil {
		t.Fatalf("Call provider.generate: %v", err)
	}
	if !strings.Contains(string(raw), "fake response") {
		t.Fatalf("raw response = %s", raw)
	}
	if fake.lastRequest.Model != "fake-model" || fake.lastRequest.System != "short system" {
		t.Fatalf("last request = %#v", fake.lastRequest)
	}
	usages, err := db.ListPluginProviderUsage(ctx, manifest.ID, 10)
	if err != nil {
		t.Fatalf("ListPluginProviderUsage: %v", err)
	}
	if len(usages) != 1 {
		t.Fatalf("len(usages) = %d, want 1", len(usages))
	}
	if usages[0].Status != "success" || usages[0].ProviderID != "fake-provider" || usages[0].Model != "fake-model" || usages[0].InputTokens != 3 || usages[0].OutputTokens != 5 {
		t.Fatalf("usage = %#v", usages[0])
	}
	events, err := db.ListPluginAccessEvents(ctx, manifest.ID, 10)
	if err != nil {
		t.Fatalf("ListPluginAccessEvents: %v", err)
	}
	if len(events) != 1 || events[0].Capability != string(CapabilityProviderGenerate) || events[0].Status != "allowed" {
		t.Fatalf("access events = %#v", events)
	}
}

func TestProviderGatewayRecordsUsageOnError(t *testing.T) {
	db := openPluginTestDB(t)
	gateway := NewProviderGateway(db, config.PluginProviderGatewayConfig{Enabled: true}, nil)
	_, err := gateway.Generate(t.Context(), "com.example.echo", PluginGenerateRequest{ProviderID: "missing", Model: "fake"})
	if err == nil || !strings.Contains(err.Error(), "resolver") {
		t.Fatalf("Generate error = %v, want resolver error", err)
	}
	usages, err := db.ListPluginProviderUsage(t.Context(), "com.example.echo", 10)
	if err != nil {
		t.Fatalf("ListPluginProviderUsage: %v", err)
	}
	if len(usages) != 1 || usages[0].Status != "error" || !strings.Contains(usages[0].ErrorMessage, "resolver") {
		t.Fatalf("usage = %#v", usages)
	}
}

func TestProviderGatewayRejectsDisallowedRequestedProvider(t *testing.T) {
	db := openPluginTestDB(t)
	manifest := facadeTestManifest([]Capability{CapabilityProviderGenerate})
	manifest.Provider.AllowedProviderIDs = []string{"allowed-provider"}
	gateway := NewProviderGateway(db, config.PluginProviderGatewayConfig{Enabled: true}, func(context.Context, string) (llm.Client, error) {
		t.Fatal("resolver should not be called for disallowed provider")
		return nil, nil
	})
	gateway.AddPlugin(manifest)

	_, err := gateway.Generate(t.Context(), manifest.ID, PluginGenerateRequest{ProviderID: "blocked-provider", Model: "fake-model"})
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("Generate error = %v, want not allowed", err)
	}
}

func TestProviderGatewayUsesFallbackWhenNoDefaults(t *testing.T) {
	db := openPluginTestDB(t)
	fake := &fakePluginLLMClient{}
	gateway := NewProviderGateway(db, config.PluginProviderGatewayConfig{Enabled: true}, func(_ context.Context, providerID string) (llm.Client, error) {
		if providerID != "fallback-provider" {
			t.Fatalf("providerID = %q, want fallback-provider", providerID)
		}
		return fake, nil
	})
	gateway.SetFallbackResolver(func(context.Context) (string, string, bool, error) {
		return "fallback-provider", "fallback-model", true, nil
	})

	resp, err := gateway.Generate(t.Context(), "com.example.echo", PluginGenerateRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Model != "fallback-model" || fake.lastRequest.Model != "fallback-model" {
		t.Fatalf("response=%#v request=%#v", resp, fake.lastRequest)
	}
}

type fakePluginLLMClient struct {
	lastRequest llm.ChatRequest
}

func (f *fakePluginLLMClient) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	f.lastRequest = req
	return &llm.ChatResponse{
		Content:    "fake response",
		Model:      req.Model,
		Usage:      llm.Usage{InputTokens: 3, OutputTokens: 5},
		StopReason: "end_turn",
	}, nil
}

func (f *fakePluginLLMClient) ChatStream(ctx context.Context, req llm.ChatRequest, cb llm.StreamCallback) (*llm.ChatResponse, error) {
	return f.Chat(ctx, req)
}
