package media

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/storage"
)

func TestPlannerRejectsUnsupportedModelByDefault(t *testing.T) {
	store, asset := testStoreWithImage(t)
	planner := NewPlanner(store, staticResolver{caps: &llm.ModelCapabilities{
		ProviderID:       "openai",
		ModelID:          "text-only",
		InputModalities:  []string{"text"},
		ImageTransports:  nil,
		CapabilitySource: "provider_docs_preset",
		Confidence:       0.9,
	}})

	_, err := planner.Prepare(context.Background(), PrepareRequest{
		ProviderID:    "openai",
		ModelID:       "text-only",
		CurrentTurnID: "turn-1",
		Policy:        DefaultPolicy(),
		Messages: []llm.Message{{Role: llm.RoleUser, TurnID: "turn-1", ContentBlocks: []llm.ContentBlock{
			{Type: string(llm.PartText), Text: "look"},
			{Type: string(llm.PartImage), Media: &llm.MediaPart{MediaAssetID: asset.ID, Kind: "image", MimeType: "image/png"}},
		}}},
	})
	if err == nil {
		t.Fatal("Prepare succeeded for unsupported image model, want error")
	}
}

func TestPlannerOptimisticUnknownModelCanAttachCurrentTurnImage(t *testing.T) {
	store, asset := testStoreWithImage(t)
	planner := NewPlanner(store, staticResolver{})

	result, err := planner.Prepare(context.Background(), PrepareRequest{
		ProviderID:    "openai",
		ModelID:       "unknown-model",
		CurrentTurnID: "turn-1",
		Policy: MediaPolicy{
			Enabled:            true,
			UnknownModelPolicy: PolicyOptimisticSend,
			PreferredTransports: []string{
				TransportDataURL,
			},
		},
		Messages: []llm.Message{{Role: llm.RoleUser, TurnID: "turn-1", ContentBlocks: []llm.ContentBlock{
			{Type: string(llm.PartText), Text: "look"},
			{Type: string(llm.PartImage), Media: &llm.MediaPart{MediaAssetID: asset.ID, Kind: "image", MimeType: "image/png"}},
		}}},
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(result.Messages) != 1 || len(result.Messages[0].ContentBlocks) != 2 {
		t.Fatalf("prepared messages = %#v, want text+image blocks", result.Messages)
	}
	media := result.Messages[0].ContentBlocks[1].Media
	if media == nil || len(media.Data) == 0 || media.Transport != TransportDataURL {
		t.Fatalf("prepared media = %#v, want data_url bytes", media)
	}
}

func testStoreWithImage(t *testing.T) (*LocalStore, *MediaAsset) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "emo.db"), slog.Default())
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := NewLocalStore(db.SqlDB(), filepath.Join(t.TempDir(), "media"), StoreOptions{MaxBytes: 1024 * 1024})
	asset, err := store.Put(context.Background(), bytes.NewReader(tinyPNG()), UploadMeta{CreatedByRole: "user"})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	return store, asset
}

type staticResolver struct {
	caps *llm.ModelCapabilities
}

func (r staticResolver) Resolve(context.Context, string, string) (*llm.ModelCapabilities, error) {
	return r.caps, nil
}
