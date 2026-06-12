package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/storage"
)

func TestRefreshModelsPersistsCapabilitiesAndHonorsManualOverride(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %s, want /v1/models", r.URL.Path)
		}
		fmt.Fprint(w, `{"data":[{"id":"gpt-5-mini"},{"id":"custom-vision","input_modalities":["text","image"],"image_formats":["image/png"]}]}`)
	}))
	defer server.Close()
	t.Setenv("TEST_OPENAI_KEY", "test-key")

	db, err := storage.Open(filepath.Join(t.TempDir(), "emo.db"), slog.Default())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	provider := config.LLMProvider{
		ID:             "openai",
		Name:           "OpenAI",
		PresetID:       "openai",
		Protocol:       "openai_compatible",
		BaseURL:        server.URL,
		APIKeyEnv:      "TEST_OPENAI_KEY",
		ModelDiscovery: "openai_models",
		Enabled:        true,
	}
	if err := db.UpsertLLMProvider(provider); err != nil {
		t.Fatalf("UpsertLLMProvider: %v", err)
	}
	if err := db.UpsertModelCapability(context.Background(), storage.ModelCapabilityRecord{
		ProviderID:       "openai",
		ModelID:          "gpt-5-mini",
		InputModalities:  []string{"text"},
		OutputModalities: []string{"text"},
		ImageTransports:  []string{},
		ImageFormats:     []string{},
		CapabilitySource: string(llm.CapabilitySourceManualOverride),
		Confidence:       1,
		LastVerifiedAt:   "2026-06-13T00:00:00Z",
		RawProviderJSON:  `{"manual":true}`,
	}); err != nil {
		t.Fatalf("manual UpsertModelCapability: %v", err)
	}

	service := &LLMProviderService{infra: &Infra{DB: db, Config: config.DefaultConfig()}}
	models, err := service.RefreshModels("openai")
	if err != nil {
		t.Fatalf("RefreshModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %#v, want 2", models)
	}

	manual, err := db.GetModelCapability(context.Background(), "openai", "gpt-5-mini")
	if err != nil {
		t.Fatalf("GetModelCapability manual: %v", err)
	}
	if manual.CapabilitySource != string(llm.CapabilitySourceManualOverride) || len(manual.InputModalities) != 1 || manual.InputModalities[0] != "text" {
		t.Fatalf("manual capability = %#v, want manual text-only override preserved", manual)
	}
	metadata, err := db.GetModelCapability(context.Background(), "openai", "custom-vision")
	if err != nil {
		t.Fatalf("GetModelCapability metadata: %v", err)
	}
	if metadata.CapabilitySource != string(llm.CapabilitySourceProviderMetadata) || !contains(metadata.InputModalities, "image") {
		t.Fatalf("metadata capability = %#v, want provider metadata image capability", metadata)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

var _ = os.Getenv
