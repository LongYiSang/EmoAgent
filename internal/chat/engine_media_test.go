package chat

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/media"
	"github.com/longyisang/emoagent/internal/storage"
)

func TestEngineSendMessagePartsSendsCurrentTurnImageOnly(t *testing.T) {
	fakeLLM := &fakeLLMClient{response: &llm.ChatResponse{ID: "resp-1", Content: "ok", StopReason: "end_turn"}}
	engine, db, _ := newTestEngine(t, fakeLLM)
	engine.contextCfg.KeepRecentUserTurns = 6
	store := media.NewLocalStore(db.SqlDB(), filepath.Join(t.TempDir(), "media"), media.StoreOptions{MaxBytes: 1024 * 1024})
	asset, err := store.Put(context.Background(), bytes.NewReader(chatTinyPNG()), media.UploadMeta{CreatedByRole: "user"})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	engine.mediaStore = store
	engine.mediaPlanner = media.NewPlanner(store, staticMediaResolver{caps: &llm.ModelCapabilities{
		ProviderID:       "openai",
		ModelID:          "test-model",
		InputModalities:  []string{"text", "image"},
		OutputModalities: []string{"text"},
		ImageTransports:  []string{media.TransportDataURL},
		ImageFormats:     []string{"image/png"},
		CapabilitySource: string(llm.CapabilitySourceProviderDocsPreset),
		Confidence:       0.9,
	}})
	engine.providerID = "openai"
	bridge := &fakeMemoryBridge{ensureResult: MemorySegmentRef{SegmentID: "segment-current", MemorySessionID: "memory-current"}}
	engine.memory = bridge

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	_, err = engine.SendMessageParts(context.Background(), sessionID, &config.Persona{Name: "default", SystemPrompt: "system"}, []llm.ContentBlock{
		{Type: string(llm.PartText), Text: "look"},
		{Type: string(llm.PartImage), Media: &llm.MediaPart{MediaAssetID: asset.ID, Kind: "image", MimeType: "image/png"}},
	}, nil)
	if err != nil {
		t.Fatalf("SendMessageParts: %v", err)
	}

	last := fakeLLM.lastRequest.Messages[len(fakeLLM.lastRequest.Messages)-1]
	if len(last.ContentBlocks) != 2 || last.ContentBlocks[1].Media == nil || len(last.ContentBlocks[1].Media.Data) == 0 {
		t.Fatalf("current turn message = %#v, want image bytes attached", last)
	}
	if len(bridge.userEpisodes) != 1 || bridge.userEpisodes[0].Content != "look\n[used image]" {
		t.Fatalf("user memory episodes = %#v, want text plus placeholder only", bridge.userEpisodes)
	}
	if strings.Contains(bridge.userEpisodes[0].Content, asset.ID) || strings.Contains(bridge.userEpisodes[0].Content, "iVBOR") {
		t.Fatalf("memory episode leaked media data/id: %q", bridge.userEpisodes[0].Content)
	}
	mediaMessageID := userMessageIDWithPlaceholder(t, db, sessionID)
	storedParts, err := db.GetMessageParts(context.Background(), sessionID, mediaMessageID)
	if err != nil {
		t.Fatalf("GetMessageParts: %v", err)
	}
	if len(storedParts) != 2 || storedParts[1].MediaAssetID != asset.ID {
		t.Fatalf("stored parts = %#v, want text+image parts", storedParts)
	}
	hydrated := engine.hydrateStoredMessageParts(context.Background(), sessionID, []llm.Message{{
		ID:      mediaMessageID,
		Role:    llm.RoleUser,
		Content: "look\n[used image]",
	}})
	if len(hydrated) != 1 || len(hydrated[0].ContentBlocks) != 2 || hydrated[0].ContentBlocks[1].Media == nil {
		t.Fatalf("hydrated messages = %#v, want stored image part", hydrated)
	}
	deliveries, err := db.ListMediaDeliveriesForMessage(context.Background(), mediaMessageID)
	if err != nil {
		t.Fatalf("ListMediaDeliveriesForMessage after first send: %v", err)
	}
	if !hasMediaDelivery(deliveries, "sent", "current_turn", media.TransportDataURL) {
		t.Fatalf("deliveries after first send = %#v, want sent current_turn data_url", deliveries)
	}

	_, err = engine.SendMessage(context.Background(), sessionID, &config.Persona{Name: "default", SystemPrompt: "system"}, "next", nil)
	if err != nil {
		t.Fatalf("second SendMessage: %v", err)
	}
	var historical llm.Message
	for _, msg := range fakeLLM.lastRequest.Messages {
		if msg.Role == llm.RoleUser && strings.Contains(msg.Content, "[used image]") {
			historical = msg
			break
		}
	}
	if historical.Content == "" {
		t.Fatalf("second request messages = %#v, want historical placeholder", fakeLLM.lastRequest.Messages)
	}
	if len(historical.ContentBlocks) != 0 || strings.Contains(historical.Content, "iVBOR") || strings.Contains(historical.Content, asset.ID) {
		t.Fatalf("historical message leaked media: %#v", historical)
	}
	expectedOmissions := engine.historicalPlaceholderDeliveries(context.Background(), sessionID, fakeLLM.lastRequest.Messages, "", "turn-check", "openai", "test-model")
	if !hasPlannedMediaDelivery(expectedOmissions, "omitted", "history_placeholder", media.TransportPlaceholder) {
		t.Fatalf("direct historicalPlaceholderDeliveries = %#v, want omitted history placeholder", expectedOmissions)
	}
	deliveries, err = db.ListMediaDeliveriesForMessage(context.Background(), mediaMessageID)
	if err != nil {
		t.Fatalf("ListMediaDeliveriesForMessage after history replay: %v", err)
	}
	if !hasMediaDelivery(deliveries, "omitted", "history_placeholder", media.TransportPlaceholder) {
		t.Fatalf("deliveries after history replay = %#v, all deliveries = %#v, request messages = %#v, want omitted history placeholder", deliveries, allMediaDeliveries(t, db), fakeLLM.lastRequest.Messages)
	}
}

func TestEngineRejectsMediaForUnsupportedModel(t *testing.T) {
	fakeLLM := &fakeLLMClient{response: &llm.ChatResponse{ID: "resp-1", Content: "ok", StopReason: "end_turn"}}
	engine, db, _ := newTestEngine(t, fakeLLM)
	store := media.NewLocalStore(db.SqlDB(), filepath.Join(t.TempDir(), "media"), media.StoreOptions{MaxBytes: 1024 * 1024})
	asset, err := store.Put(context.Background(), bytes.NewReader(chatTinyPNG()), media.UploadMeta{CreatedByRole: "user"})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	engine.mediaStore = store
	engine.mediaPlanner = media.NewPlanner(store, staticMediaResolver{caps: &llm.ModelCapabilities{
		ProviderID:       "openai",
		ModelID:          "test-model",
		InputModalities:  []string{"text"},
		OutputModalities: []string{"text"},
		CapabilitySource: string(llm.CapabilitySourceProviderDocsPreset),
		Confidence:       0.9,
	}})
	engine.providerID = "openai"

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	_, err = engine.SendMessageParts(context.Background(), sessionID, &config.Persona{Name: "default", SystemPrompt: "system"}, []llm.ContentBlock{
		{Type: string(llm.PartText), Text: "look"},
		{Type: string(llm.PartImage), Media: &llm.MediaPart{MediaAssetID: asset.ID, Kind: "image", MimeType: "image/png"}},
	}, nil)
	if err == nil {
		t.Fatal("SendMessageParts succeeded for unsupported model, want error")
	}
}

func TestEngineRecordsSanitizedMediaDeliveryFailure(t *testing.T) {
	providerEcho := "provider rejected request: data:image/png;base64,iVBORw0KGgo="
	fakeLLM := &fakeLLMClient{err: errors.New(providerEcho)}
	engine, db, _ := newTestEngine(t, fakeLLM)
	store := media.NewLocalStore(db.SqlDB(), filepath.Join(t.TempDir(), "media"), media.StoreOptions{MaxBytes: 1024 * 1024})
	asset, err := store.Put(context.Background(), bytes.NewReader(chatTinyPNG()), media.UploadMeta{CreatedByRole: "user"})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	engine.mediaStore = store
	engine.mediaPlanner = media.NewPlanner(store, staticMediaResolver{caps: &llm.ModelCapabilities{
		ProviderID:       "openai",
		ModelID:          "test-model",
		InputModalities:  []string{"text", "image"},
		OutputModalities: []string{"text"},
		ImageTransports:  []string{media.TransportDataURL},
		ImageFormats:     []string{"image/png"},
		CapabilitySource: string(llm.CapabilitySourceProviderDocsPreset),
		Confidence:       0.9,
	}})
	engine.providerID = "openai"

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	_, err = engine.SendMessageParts(context.Background(), sessionID, &config.Persona{Name: "default", SystemPrompt: "system"}, []llm.ContentBlock{
		{Type: string(llm.PartText), Text: "look"},
		{Type: string(llm.PartImage), Media: &llm.MediaPart{MediaAssetID: asset.ID, Kind: "image", MimeType: "image/png"}},
	}, nil)
	if err == nil {
		t.Fatal("SendMessageParts succeeded, want provider error")
	}

	messageID := userMessageIDWithPlaceholder(t, db, sessionID)
	deliveries, err := db.ListMediaDeliveriesForMessage(context.Background(), messageID)
	if err != nil {
		t.Fatalf("ListMediaDeliveriesForMessage: %v", err)
	}
	if len(deliveries) == 0 {
		t.Fatal("no media deliveries recorded for failed image send")
	}
	for _, delivery := range deliveries {
		if delivery.Status != media.DeliveryStatusFailed {
			continue
		}
		lower := strings.ToLower(delivery.ErrorMessage)
		if strings.Contains(lower, "data:image") || strings.Contains(lower, "base64") || strings.Contains(delivery.ErrorMessage, "iVBOR") || strings.Contains(delivery.ErrorMessage, "/9j/") {
			t.Fatalf("delivery error leaked image data: %#v", delivery)
		}
		return
	}
	t.Fatalf("deliveries = %#v, want failed media delivery", deliveries)
}

func TestEngineRejectsUnsupportedUserPart(t *testing.T) {
	fakeLLM := &fakeLLMClient{response: &llm.ChatResponse{ID: "resp-1", Content: "ok", StopReason: "end_turn"}}
	engine, _, _ := newTestEngine(t, fakeLLM)
	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	_, err = engine.SendMessageParts(context.Background(), sessionID, &config.Persona{Name: "default", SystemPrompt: "system"}, []llm.ContentBlock{
		{Type: string(llm.PartText), Text: "hello"},
		{
			Type: string(llm.PartToolUse),
			ID:   "tool-call-1",
			Name: "spoofed_tool",
		},
	}, nil)
	if err == nil {
		t.Fatal("SendMessageParts accepted tool_use from user input, want rejection")
	}
	if fakeLLM.lastRequest.Model != "" {
		t.Fatalf("LLM was called with request %#v, want rejection before provider call", fakeLLM.lastRequest)
	}
}

func TestEngineRejectsUnsupportedImagePartFields(t *testing.T) {
	fakeLLM := &fakeLLMClient{response: &llm.ChatResponse{ID: "resp-1", Content: "ok", StopReason: "end_turn"}}
	engine, _, _ := newTestEngine(t, fakeLLM)
	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	_, err = engine.SendMessageParts(context.Background(), sessionID, &config.Persona{Name: "default", SystemPrompt: "system"}, []llm.ContentBlock{{
		Type: string(llm.PartImage),
		Media: &llm.MediaPart{
			MediaAssetID: "med_1",
			Kind:         "image",
			MimeType:     "image/png",
			AltText:      "caption-like text",
		},
	}}, nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported media fields on user image part") {
		t.Fatalf("err = %v, want unsupported media fields rejection", err)
	}
	if fakeLLM.lastRequest.Model != "" {
		t.Fatalf("LLM was called with request %#v, want rejection before provider call", fakeLLM.lastRequest)
	}
}

type staticMediaResolver struct {
	caps *llm.ModelCapabilities
}

func (r staticMediaResolver) Resolve(context.Context, string, string) (*llm.ModelCapabilities, error) {
	return r.caps, nil
}

func userMessageIDWithPlaceholder(t *testing.T, db *storage.DB, sessionID string) string {
	t.Helper()
	messages, err := db.GetAllMessages(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("GetAllMessages: %v", err)
	}
	for _, msg := range messages {
		if msg.Role == "user" && strings.Contains(msg.Content, "[used image]") {
			return msg.ID
		}
	}
	t.Fatalf("no user message with image placeholder in %#v", messages)
	return ""
}

func hasMediaDelivery(deliveries []storage.MediaDeliveryRecord, status, scope, transport string) bool {
	for _, delivery := range deliveries {
		if delivery.Status == status && delivery.DeliveryScope == scope && delivery.Transport == transport {
			return true
		}
	}
	return false
}

func hasPlannedMediaDelivery(deliveries []media.DeliveryRecord, status, scope, transport string) bool {
	for _, delivery := range deliveries {
		if delivery.Status == status && delivery.DeliveryScope == scope && delivery.Transport == transport {
			return true
		}
	}
	return false
}

func allMediaDeliveries(t *testing.T, db *storage.DB) []storage.MediaDeliveryRecord {
	t.Helper()
	rows, err := db.SqlDB().Query(`
		SELECT id, message_id, part_id, media_asset_id, provider_id, model_id, COALESCE(turn_id, ''),
		       delivery_scope, transport, status, COALESCE(byte_size_sent, 0), COALESCE(error_message, ''), created_at
		FROM message_media_deliveries
		ORDER BY created_at ASC, rowid ASC
	`)
	if err != nil {
		t.Fatalf("query all deliveries: %v", err)
	}
	defer rows.Close()
	var deliveries []storage.MediaDeliveryRecord
	for rows.Next() {
		var delivery storage.MediaDeliveryRecord
		if err := rows.Scan(&delivery.ID, &delivery.MessageID, &delivery.PartID, &delivery.MediaAssetID, &delivery.ProviderID,
			&delivery.ModelID, &delivery.TurnID, &delivery.DeliveryScope, &delivery.Transport, &delivery.Status,
			&delivery.ByteSizeSent, &delivery.ErrorMessage, &delivery.CreatedAt); err != nil {
			t.Fatalf("scan delivery: %v", err)
		}
		deliveries = append(deliveries, delivery)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate deliveries: %v", err)
	}
	return deliveries
}

func chatTinyPNG() []byte {
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
		0x42, 0x60, 0x82,
	}
}
