package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

type MessagePartRecord struct {
	ID                  string
	SessionID           string
	MessageID           string
	Role                string
	Ordinal             int
	PartType            string
	TextContent         string
	MediaAssetID        string
	MemoryRenderPolicy  string
	HistoryRenderPolicy string
	CreatedAt           string
}

type MediaDeliveryRecord struct {
	ID            string
	MessageID     string
	PartID        string
	MediaAssetID  string
	ProviderID    string
	ModelID       string
	TurnID        string
	DeliveryScope string
	Transport     string
	Status        string
	ByteSizeSent  int64
	ErrorMessage  string
	CreatedAt     string
}

func (d *DB) AddMessageParts(ctx context.Context, parts []MessagePartRecord) error {
	if len(parts) == 0 {
		return nil
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, part := range parts {
		if part.ID == "" {
			return fmt.Errorf("message part id is required")
		}
		if part.SessionID == "" || part.MessageID == "" {
			return fmt.Errorf("message part session/message id is required")
		}
		if part.MemoryRenderPolicy == "" {
			part.MemoryRenderPolicy = "placeholder_only"
		}
		if part.HistoryRenderPolicy == "" {
			part.HistoryRenderPolicy = "placeholder_only"
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO message_parts (
				id, session_id, message_id, role, ordinal, part_type, text_content, media_asset_id,
				memory_render_policy, history_render_policy
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, part.ID, part.SessionID, part.MessageID, part.Role, part.Ordinal, part.PartType, nullEmpty(part.TextContent), nullEmpty(part.MediaAssetID), part.MemoryRenderPolicy, part.HistoryRenderPolicy); err != nil {
			return err
		}
		if strings.TrimSpace(part.MediaAssetID) != "" {
			if _, err := tx.ExecContext(ctx, `UPDATE media_assets SET reference_count = reference_count + 1 WHERE id = ?`, part.MediaAssetID); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (d *DB) GetMessageParts(ctx context.Context, sessionID, messageID string) ([]MessagePartRecord, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, session_id, message_id, role, ordinal, part_type,
		       COALESCE(text_content, ''), COALESCE(media_asset_id, ''),
		       memory_render_policy, history_render_policy, created_at
		FROM message_parts
		WHERE session_id = ? AND message_id = ?
		ORDER BY ordinal ASC
	`, sessionID, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var parts []MessagePartRecord
	for rows.Next() {
		var part MessagePartRecord
		if err := rows.Scan(&part.ID, &part.SessionID, &part.MessageID, &part.Role, &part.Ordinal, &part.PartType, &part.TextContent, &part.MediaAssetID, &part.MemoryRenderPolicy, &part.HistoryRenderPolicy, &part.CreatedAt); err != nil {
			return nil, err
		}
		parts = append(parts, part)
	}
	return parts, rows.Err()
}

func (d *DB) GetMessagePartsForSession(ctx context.Context, sessionID string) (map[string][]MessagePartRecord, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, session_id, message_id, role, ordinal, part_type,
		       COALESCE(text_content, ''), COALESCE(media_asset_id, ''),
		       memory_render_policy, history_render_policy, created_at
		FROM message_parts
		WHERE session_id = ?
		ORDER BY message_id ASC, ordinal ASC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string][]MessagePartRecord{}
	for rows.Next() {
		var part MessagePartRecord
		if err := rows.Scan(&part.ID, &part.SessionID, &part.MessageID, &part.Role, &part.Ordinal, &part.PartType, &part.TextContent, &part.MediaAssetID, &part.MemoryRenderPolicy, &part.HistoryRenderPolicy, &part.CreatedAt); err != nil {
			return nil, err
		}
		result[part.MessageID] = append(result[part.MessageID], part)
	}
	return result, rows.Err()
}

func (d *DB) AddMediaDeliveries(ctx context.Context, deliveries []MediaDeliveryRecord) error {
	if len(deliveries) == 0 {
		return nil
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := d.nowText()
	for _, delivery := range deliveries {
		if delivery.ID == "" {
			return fmt.Errorf("media delivery id is required")
		}
		if delivery.MessageID == "" || delivery.PartID == "" || delivery.MediaAssetID == "" {
			return fmt.Errorf("media delivery message/part/media ids are required")
		}
		if delivery.ProviderID == "" || delivery.ModelID == "" {
			return fmt.Errorf("media delivery provider/model ids are required")
		}
		if delivery.CreatedAt == "" {
			delivery.CreatedAt = now
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO message_media_deliveries (
				id, message_id, part_id, media_asset_id, provider_id, model_id, turn_id,
				delivery_scope, transport, status, byte_size_sent, error_message, created_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, delivery.ID, delivery.MessageID, delivery.PartID, delivery.MediaAssetID, delivery.ProviderID, delivery.ModelID,
			delivery.TurnID, delivery.DeliveryScope, delivery.Transport, delivery.Status, nullZeroInt64(delivery.ByteSizeSent),
			nullEmpty(delivery.ErrorMessage), delivery.CreatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *DB) ListMediaDeliveriesForMessage(ctx context.Context, messageID string) ([]MediaDeliveryRecord, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, message_id, part_id, media_asset_id, provider_id, model_id, turn_id,
		       delivery_scope, transport, status, COALESCE(byte_size_sent, 0),
		       COALESCE(error_message, ''), created_at
		FROM message_media_deliveries
		WHERE message_id = ?
		ORDER BY created_at ASC, rowid ASC
	`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var deliveries []MediaDeliveryRecord
	for rows.Next() {
		var delivery MediaDeliveryRecord
		if err := rows.Scan(&delivery.ID, &delivery.MessageID, &delivery.PartID, &delivery.MediaAssetID, &delivery.ProviderID,
			&delivery.ModelID, &delivery.TurnID, &delivery.DeliveryScope, &delivery.Transport, &delivery.Status,
			&delivery.ByteSizeSent, &delivery.ErrorMessage, &delivery.CreatedAt); err != nil {
			return nil, err
		}
		deliveries = append(deliveries, delivery)
	}
	return deliveries, rows.Err()
}

type ModelCapabilityRecord struct {
	ProviderID              string
	ModelID                 string
	InputModalities         []string
	OutputModalities        []string
	ImageTransports         []string
	ImageFormats            []string
	MaxImagesPerRequest     int
	MaxImageBytes           int64
	MaxRequestBytes         int64
	MaxLongEdgePixels       int64
	SupportsVisionTools     bool
	SupportsVisionStreaming bool
	SupportsVisionJSONMode  bool
	ParamPolicyJSON         string
	CapabilitySource        string
	Confidence              float64
	LastRefreshedAt         string
	LastVerifiedAt          string
	RawProviderJSON         string
}

func (d *DB) UpsertModelCapability(ctx context.Context, cap ModelCapabilityRecord) error {
	id := cap.ProviderID + ":" + cap.ModelID
	inputJSON, err := encodeStringArray(cap.InputModalities, []string{"text"})
	if err != nil {
		return err
	}
	outputJSON, err := encodeStringArray(cap.OutputModalities, []string{"text"})
	if err != nil {
		return err
	}
	transportsJSON, err := encodeStringArray(cap.ImageTransports, []string{})
	if err != nil {
		return err
	}
	formatsJSON, err := encodeStringArray(cap.ImageFormats, []string{})
	if err != nil {
		return err
	}
	if cap.CapabilitySource == "" {
		cap.CapabilitySource = "unknown"
	}
	_, err = d.db.ExecContext(ctx, `
		INSERT INTO llm_model_capabilities (
			id, provider_id, model_id, input_modalities_json, output_modalities_json,
			image_transports_json, image_formats_json, max_images_per_request, max_image_bytes,
			max_request_bytes, max_long_edge_pixels, supports_vision_tools, supports_vision_streaming,
			supports_vision_json_mode, param_policy_json, capability_source, confidence,
			last_refreshed_at, last_verified_at, raw_provider_json, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider_id, model_id) DO UPDATE SET
			input_modalities_json = excluded.input_modalities_json,
			output_modalities_json = excluded.output_modalities_json,
			image_transports_json = excluded.image_transports_json,
			image_formats_json = excluded.image_formats_json,
			max_images_per_request = excluded.max_images_per_request,
			max_image_bytes = excluded.max_image_bytes,
			max_request_bytes = excluded.max_request_bytes,
			max_long_edge_pixels = excluded.max_long_edge_pixels,
			supports_vision_tools = excluded.supports_vision_tools,
			supports_vision_streaming = excluded.supports_vision_streaming,
			supports_vision_json_mode = excluded.supports_vision_json_mode,
			param_policy_json = excluded.param_policy_json,
			capability_source = excluded.capability_source,
			confidence = excluded.confidence,
			last_refreshed_at = excluded.last_refreshed_at,
			last_verified_at = excluded.last_verified_at,
			raw_provider_json = excluded.raw_provider_json,
			updated_at = excluded.updated_at
	`, id, cap.ProviderID, cap.ModelID, inputJSON, outputJSON, transportsJSON, formatsJSON,
		nullZeroInt(cap.MaxImagesPerRequest), nullZeroInt64(cap.MaxImageBytes), nullZeroInt64(cap.MaxRequestBytes), nullZeroInt64(cap.MaxLongEdgePixels),
		boolInt(cap.SupportsVisionTools), boolInt(cap.SupportsVisionStreaming), boolInt(cap.SupportsVisionJSONMode),
		nullEmpty(cap.ParamPolicyJSON), cap.CapabilitySource, cap.Confidence, nullEmpty(cap.LastRefreshedAt), nullEmpty(cap.LastVerifiedAt), nullEmpty(cap.RawProviderJSON), d.nowText())
	return err
}

func (d *DB) GetModelCapability(ctx context.Context, providerID, modelID string) (*ModelCapabilityRecord, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT provider_id, model_id, input_modalities_json, output_modalities_json,
		       image_transports_json, image_formats_json, COALESCE(max_images_per_request, 0),
		       COALESCE(max_image_bytes, 0), COALESCE(max_request_bytes, 0), COALESCE(max_long_edge_pixels, 0),
		       supports_vision_tools, supports_vision_streaming, supports_vision_json_mode,
		       COALESCE(param_policy_json, ''), capability_source, confidence,
		       COALESCE(last_refreshed_at, ''), COALESCE(last_verified_at, ''), COALESCE(raw_provider_json, '')
		FROM llm_model_capabilities
		WHERE provider_id = ? AND model_id = ?
	`, providerID, modelID)
	record, err := scanModelCapability(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (d *DB) ListModelCapabilities(ctx context.Context, providerID string) (map[string]ModelCapabilityRecord, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT provider_id, model_id, input_modalities_json, output_modalities_json,
		       image_transports_json, image_formats_json, COALESCE(max_images_per_request, 0),
		       COALESCE(max_image_bytes, 0), COALESCE(max_request_bytes, 0), COALESCE(max_long_edge_pixels, 0),
		       supports_vision_tools, supports_vision_streaming, supports_vision_json_mode,
		       COALESCE(param_policy_json, ''), capability_source, confidence,
		       COALESCE(last_refreshed_at, ''), COALESCE(last_verified_at, ''), COALESCE(raw_provider_json, '')
		FROM llm_model_capabilities
		WHERE provider_id = ?
	`, providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]ModelCapabilityRecord{}
	for rows.Next() {
		record, err := scanModelCapability(rows)
		if err != nil {
			return nil, err
		}
		result[record.ModelID] = record
	}
	return result, rows.Err()
}

func scanModelCapability(row scanner) (ModelCapabilityRecord, error) {
	var record ModelCapabilityRecord
	var inputJSON, outputJSON, transportsJSON, formatsJSON string
	var tools, streaming, jsonMode int
	if err := row.Scan(&record.ProviderID, &record.ModelID, &inputJSON, &outputJSON, &transportsJSON, &formatsJSON,
		&record.MaxImagesPerRequest, &record.MaxImageBytes, &record.MaxRequestBytes, &record.MaxLongEdgePixels,
		&tools, &streaming, &jsonMode, &record.ParamPolicyJSON, &record.CapabilitySource, &record.Confidence,
		&record.LastRefreshedAt, &record.LastVerifiedAt, &record.RawProviderJSON); err != nil {
		return record, err
	}
	record.InputModalities = decodeStringArray(inputJSON, []string{"text"})
	record.OutputModalities = decodeStringArray(outputJSON, []string{"text"})
	record.ImageTransports = decodeStringArray(transportsJSON, nil)
	record.ImageFormats = decodeStringArray(formatsJSON, nil)
	record.SupportsVisionTools = tools == 1
	record.SupportsVisionStreaming = streaming == 1
	record.SupportsVisionJSONMode = jsonMode == 1
	return record, nil
}

func encodeStringArray(values []string, fallback []string) (string, error) {
	if len(values) == 0 {
		values = fallback
	}
	payload, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func decodeStringArray(value string, fallback []string) []string {
	if strings.TrimSpace(value) == "" {
		return append([]string(nil), fallback...)
	}
	var values []string
	if err := json.Unmarshal([]byte(value), &values); err != nil {
		return append([]string(nil), fallback...)
	}
	return values
}

func nullEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullZeroInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

func nullZeroInt64(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}
