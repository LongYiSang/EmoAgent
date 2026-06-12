package storage

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
)

func TestMediaAndModelCapabilityTablesAreMigrated(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "emo.db"), slog.Default())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	for _, table := range []string{
		"media_assets",
		"message_parts",
		"message_media_deliveries",
		"provider_media_refs",
		"llm_model_capabilities",
	} {
		var name string
		if err := db.SqlDB().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name); err != nil {
			t.Fatalf("table %s missing: %v", table, err)
		}
	}
}

func TestMessagePartsRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "emo.db"), slog.Default())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := db.CreateSession(ctx, "sess-1", "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := db.AddMessage(ctx, "msg-1", "sess-1", "user", "look\n[used image]"); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	if _, err := db.SqlDB().ExecContext(ctx, `
		INSERT INTO media_assets (
			id, sha256, kind, mime_type, byte_size, storage_uri, created_by_role
		)
		VALUES ('med_1', 'sha', 'image', 'image/png', 1, 'local/path.png', 'user')
	`); err != nil {
		t.Fatalf("insert media asset: %v", err)
	}
	parts := []MessagePartRecord{
		{ID: "part-1", SessionID: "sess-1", MessageID: "msg-1", Role: "user", Ordinal: 0, PartType: "text", TextContent: "look"},
		{ID: "part-2", SessionID: "sess-1", MessageID: "msg-1", Role: "user", Ordinal: 1, PartType: "image", MediaAssetID: "med_1"},
	}
	if err := db.AddMessageParts(ctx, parts); err != nil {
		t.Fatalf("AddMessageParts: %v", err)
	}

	got, err := db.GetMessageParts(ctx, "sess-1", "msg-1")
	if err != nil {
		t.Fatalf("GetMessageParts: %v", err)
	}
	if len(got) != 2 || got[0].TextContent != "look" || got[1].MediaAssetID != "med_1" {
		t.Fatalf("parts = %#v, want text and image records", got)
	}
}

func TestMediaDeliveriesRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "emo.db"), slog.Default())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := db.CreateSession(ctx, "sess-1", "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := db.AddMessage(ctx, "msg-1", "sess-1", "user", "look\n[used image]"); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	if _, err := db.SqlDB().ExecContext(ctx, `
		INSERT INTO media_assets (
			id, sha256, kind, mime_type, byte_size, storage_uri, created_by_role
		)
		VALUES ('med_1', 'sha', 'image', 'image/png', 1, 'local/path.png', 'user')
	`); err != nil {
		t.Fatalf("insert media asset: %v", err)
	}
	if err := db.AddMessageParts(ctx, []MessagePartRecord{
		{ID: "part-1", SessionID: "sess-1", MessageID: "msg-1", Role: "user", Ordinal: 0, PartType: "text", TextContent: "look"},
		{ID: "part-2", SessionID: "sess-1", MessageID: "msg-1", Role: "user", Ordinal: 1, PartType: "image", MediaAssetID: "med_1"},
	}); err != nil {
		t.Fatalf("AddMessageParts: %v", err)
	}
	if err := db.AddMediaDeliveries(ctx, []MediaDeliveryRecord{{
		ID:            "delivery-1",
		MessageID:     "msg-1",
		PartID:        "part-2",
		MediaAssetID:  "med_1",
		ProviderID:    "openai",
		ModelID:       "gpt-4.1",
		TurnID:        "turn-1",
		DeliveryScope: "current_turn",
		Transport:     "data_url",
		Status:        "sent",
		ByteSizeSent:  68,
	}}); err != nil {
		t.Fatalf("AddMediaDeliveries: %v", err)
	}

	got, err := db.ListMediaDeliveriesForMessage(ctx, "msg-1")
	if err != nil {
		t.Fatalf("ListMediaDeliveriesForMessage: %v", err)
	}
	if len(got) != 1 || got[0].Status != "sent" || got[0].PartID != "part-2" || got[0].ByteSizeSent != 68 {
		t.Fatalf("deliveries = %#v, want sent media delivery", got)
	}
}

func TestNoImageBase64GuardsRejectTextStorage(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "emo.db"), slog.Default())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := db.CreateSession(ctx, "sess-1", "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := db.AddMessage(ctx, "msg-base64", "sess-1", "user", "data:image/png;base64,iVBORw0KGgo="); err == nil {
		t.Fatal("AddMessage accepted image base64 data URL, want trigger rejection")
	}
	if err := db.AddMessage(ctx, "msg-1", "sess-1", "user", "look\n[used image]"); err != nil {
		t.Fatalf("AddMessage sanitized: %v", err)
	}
	if err := db.AddMessageParts(ctx, []MessagePartRecord{{
		ID:          "part-base64",
		SessionID:   "sess-1",
		MessageID:   "msg-1",
		Role:        "user",
		Ordinal:     0,
		PartType:    "text",
		TextContent: "iVBORw0KGgo",
	}}); err == nil {
		t.Fatal("AddMessageParts accepted PNG base64 header, want trigger rejection")
	}
	if _, err := db.SqlDB().ExecContext(ctx, `
		INSERT INTO memory_extraction_jobs (
			id, persona_id, trigger, run_after, request_json, dedupe_key, created_at, updated_at
		)
		VALUES ('job-base64', 'default', 'manual', 'now', '{"image":"data:image/jpeg;base64,/9j/"}', 'dedupe', 'now', 'now')
	`); err == nil {
		t.Fatal("memory_extraction_jobs accepted image base64, want trigger rejection")
	}
	if err := db.AddMediaDeliveries(ctx, []MediaDeliveryRecord{{
		ID:            "delivery-base64",
		MessageID:     "msg-1",
		PartID:        "part-1",
		MediaAssetID:  "med-1",
		ProviderID:    "openai",
		ModelID:       "gpt-4.1",
		TurnID:        "turn-1",
		DeliveryScope: "current_turn",
		Transport:     "data_url",
		Status:        "failed",
		ErrorMessage:  "provider echoed data:image/png;base64,iVBORw0KGgo=",
	}}); err == nil {
		t.Fatal("message_media_deliveries accepted image base64 in error_message, want trigger rejection")
	}
}
