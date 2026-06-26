package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type LLMUsageEvent struct {
	ID                string `json:"id"`
	RequestID         string `json:"request_id"`
	ProviderRequestID string `json:"provider_request_id"`
	ResponseID        string `json:"response_id"`
	SessionID         string `json:"session_id"`
	TurnID            string `json:"turn_id"`
	AgentID           string `json:"agent_id"`
	PersonaKey        string `json:"persona_key"`
	PluginID          string `json:"plugin_id"`
	TaskID            string `json:"task_id"`
	Component         string `json:"component"`
	Operation         string `json:"operation"`
	ProviderID        string `json:"provider_id"`
	ProviderName      string `json:"provider_name"`
	Protocol          string `json:"protocol"`
	Model             string `json:"model"`
	Endpoint          string `json:"endpoint"`
	Stream            bool   `json:"stream"`
	Status            string `json:"status"`
	ErrorKind         string `json:"error_kind"`
	ErrorMessage      string `json:"error_message"`
	DurationMS        int64  `json:"duration_ms"`

	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`

	EstimatedInputTokens  int     `json:"estimated_input_tokens"`
	EstimatedOutputTokens int     `json:"estimated_output_tokens"`
	EstimatedTotalTokens  int     `json:"estimated_total_tokens"`
	EstimateMethod        string  `json:"estimate_method"`
	EstimateConfidence    float64 `json:"estimate_confidence"`

	ActualInputTokens  int `json:"actual_input_tokens"`
	ActualOutputTokens int `json:"actual_output_tokens"`
	ActualTotalTokens  int `json:"actual_total_tokens"`

	CachedInputTokens    int `json:"cached_input_tokens"`
	CacheHitInputTokens  int `json:"cache_hit_input_tokens"`
	CacheMissInputTokens int `json:"cache_miss_input_tokens"`
	CacheReadTokens      int `json:"cache_read_tokens"`
	CacheWriteTokens     int `json:"cache_write_tokens"`
	ReasoningTokens      int `json:"reasoning_tokens"`
	ImageTokens          int `json:"image_tokens"`
	AudioTokens          int `json:"audio_tokens"`

	UsageSource    string `json:"usage_source"`
	PromptHash     string `json:"prompt_hash"`
	CompletionHash string `json:"completion_hash"`
	RawUsageJSON   string `json:"raw_usage_json"`
	MetadataJSON   string `json:"metadata_json"`
	CreatedAt      string `json:"created_at"`
}

type LLMUsageEventFilter struct {
	SessionID   string
	ProviderID  string
	Model       string
	Component   string
	Operation   string
	PluginID    string
	TaskID      string
	UsageSource string
	Status      string
	From        string
	To          string
	Limit       int
}

type LLMUsageSummaryFilter struct {
	LLMUsageEventFilter
	GroupBy []string
}

type LLMUsageSummaryRow struct {
	ProviderID  string `json:"provider_id,omitempty"`
	Model       string `json:"model,omitempty"`
	Component   string `json:"component,omitempty"`
	Operation   string `json:"operation,omitempty"`
	PluginID    string `json:"plugin_id,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	UsageSource string `json:"usage_source,omitempty"`
	Day         string `json:"day,omitempty"`
	Hour        string `json:"hour,omitempty"`

	RequestCount          int `json:"request_count"`
	ErrorCount            int `json:"error_count"`
	InputTokens           int `json:"input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	TotalTokens           int `json:"total_tokens"`
	EstimatedInputTokens  int `json:"estimated_input_tokens"`
	EstimatedOutputTokens int `json:"estimated_output_tokens"`
	ActualInputTokens     int `json:"actual_input_tokens"`
	ActualOutputTokens    int `json:"actual_output_tokens"`
	CachedInputTokens     int `json:"cached_input_tokens"`
	CacheHitInputTokens   int `json:"cache_hit_input_tokens"`
	CacheMissInputTokens  int `json:"cache_miss_input_tokens"`
	ReasoningTokens       int `json:"reasoning_tokens"`
	ImageTokens           int `json:"image_tokens"`
	AudioTokens           int `json:"audio_tokens"`
	ProviderUsageCount    int `json:"provider_usage_count"`
	EstimatedUsageCount   int `json:"estimated_usage_count"`
	HybridUsageCount      int `json:"hybrid_usage_count"`
}

type TokenEstimatorCalibration struct {
	ID                       string  `json:"id"`
	ProviderID               string  `json:"provider_id"`
	Model                    string  `json:"model"`
	EstimateMethod           string  `json:"estimate_method"`
	Bucket                   string  `json:"bucket"`
	SampleCount              int     `json:"sample_count"`
	EstimatedInputTokensSum  int     `json:"estimated_input_tokens_sum"`
	ActualInputTokensSum     int     `json:"actual_input_tokens_sum"`
	EstimatedOutputTokensSum int     `json:"estimated_output_tokens_sum"`
	ActualOutputTokensSum    int     `json:"actual_output_tokens_sum"`
	CorrectionFactor         float64 `json:"correction_factor"`
	MeanAbsPctError          float64 `json:"mean_abs_pct_error"`
	LastEventID              string  `json:"last_event_id"`
	CreatedAt                string  `json:"created_at"`
	UpdatedAt                string  `json:"updated_at"`
}

type TokenEstimatorCalibrationFilter struct {
	ProviderID     string
	Model          string
	EstimateMethod string
	Bucket         string
}

func (d *DB) RecordLLMUsageEvent(ctx context.Context, event LLMUsageEvent) error {
	event = normalizeLLMUsageEvent(event, d.nowText())
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO llm_usage_events (
			id, request_id, provider_request_id, response_id, session_id, turn_id,
			agent_id, persona_key, plugin_id, task_id, component, operation,
			provider_id, provider_name, protocol, model, endpoint, stream,
			status, error_kind, error_message, duration_ms,
			input_tokens, output_tokens, total_tokens,
			estimated_input_tokens, estimated_output_tokens, estimated_total_tokens,
			estimate_method, estimate_confidence,
			actual_input_tokens, actual_output_tokens, actual_total_tokens,
			cached_input_tokens, cache_hit_input_tokens, cache_miss_input_tokens,
			cache_read_tokens, cache_write_tokens, reasoning_tokens, image_tokens, audio_tokens,
			usage_source, prompt_hash, completion_hash, raw_usage_json, metadata_json, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, event.ID, event.RequestID, event.ProviderRequestID, event.ResponseID, event.SessionID, event.TurnID,
		event.AgentID, event.PersonaKey, event.PluginID, event.TaskID, event.Component, event.Operation,
		event.ProviderID, event.ProviderName, event.Protocol, event.Model, event.Endpoint, boolInt(event.Stream),
		event.Status, event.ErrorKind, event.ErrorMessage, event.DurationMS,
		event.InputTokens, event.OutputTokens, event.TotalTokens,
		event.EstimatedInputTokens, event.EstimatedOutputTokens, event.EstimatedTotalTokens,
		event.EstimateMethod, event.EstimateConfidence,
		event.ActualInputTokens, event.ActualOutputTokens, event.ActualTotalTokens,
		event.CachedInputTokens, event.CacheHitInputTokens, event.CacheMissInputTokens,
		event.CacheReadTokens, event.CacheWriteTokens, event.ReasoningTokens, event.ImageTokens, event.AudioTokens,
		event.UsageSource, event.PromptHash, event.CompletionHash, event.RawUsageJSON, event.MetadataJSON, event.CreatedAt)
	return err
}

func (d *DB) ListLLMUsageEvents(ctx context.Context, filter LLMUsageEventFilter) ([]LLMUsageEvent, error) {
	conditions, args := llmUsageConditions(filter)
	query := llmUsageSelectSQL()
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)
	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []LLMUsageEvent
	for rows.Next() {
		event, err := scanLLMUsageEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (d *DB) SummarizeLLMUsage(ctx context.Context, filter LLMUsageSummaryFilter) ([]LLMUsageSummaryRow, error) {
	groupExprs, groupNames, err := usageGroupExprs(filter.GroupBy)
	if err != nil {
		return nil, err
	}
	conditions, args := llmUsageConditions(filter.LLMUsageEventFilter)
	selectParts := append([]string{}, groupExprs...)
	selectParts = append(selectParts,
		"COUNT(*)",
		"SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END)",
		"COALESCE(SUM(input_tokens), 0)",
		"COALESCE(SUM(output_tokens), 0)",
		"COALESCE(SUM(total_tokens), 0)",
		"COALESCE(SUM(estimated_input_tokens), 0)",
		"COALESCE(SUM(estimated_output_tokens), 0)",
		"COALESCE(SUM(actual_input_tokens), 0)",
		"COALESCE(SUM(actual_output_tokens), 0)",
		"COALESCE(SUM(cached_input_tokens), 0)",
		"COALESCE(SUM(cache_hit_input_tokens), 0)",
		"COALESCE(SUM(cache_miss_input_tokens), 0)",
		"COALESCE(SUM(reasoning_tokens), 0)",
		"COALESCE(SUM(image_tokens), 0)",
		"COALESCE(SUM(audio_tokens), 0)",
		"SUM(CASE WHEN usage_source = 'provider_usage' THEN 1 ELSE 0 END)",
		"SUM(CASE WHEN usage_source = 'estimated' THEN 1 ELSE 0 END)",
		"SUM(CASE WHEN usage_source = 'hybrid' THEN 1 ELSE 0 END)",
	)
	query := "SELECT " + strings.Join(selectParts, ", ") + " FROM llm_usage_events"
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	if len(groupExprs) > 0 {
		query += " GROUP BY " + strings.Join(groupExprs, ", ")
	}
	query += " ORDER BY total_tokens DESC"

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var summaries []LLMUsageSummaryRow
	for rows.Next() {
		row, err := scanLLMUsageSummaryRow(rows, groupNames)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, row)
	}
	return summaries, rows.Err()
}

func (d *DB) UpsertTokenEstimatorCalibration(ctx context.Context, c TokenEstimatorCalibration) error {
	now := d.nowText()
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	if c.Bucket == "" {
		c.Bucket = "global"
	}
	if c.CorrectionFactor == 0 {
		c.CorrectionFactor = 1
	}
	if c.CreatedAt == "" {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO token_estimator_calibrations (
			id, provider_id, model, estimate_method, bucket, sample_count,
			estimated_input_tokens_sum, actual_input_tokens_sum,
			estimated_output_tokens_sum, actual_output_tokens_sum,
			correction_factor, mean_abs_pct_error, last_event_id, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider_id, model, estimate_method, bucket) DO UPDATE SET
			sample_count = excluded.sample_count,
			estimated_input_tokens_sum = excluded.estimated_input_tokens_sum,
			actual_input_tokens_sum = excluded.actual_input_tokens_sum,
			estimated_output_tokens_sum = excluded.estimated_output_tokens_sum,
			actual_output_tokens_sum = excluded.actual_output_tokens_sum,
			correction_factor = excluded.correction_factor,
			mean_abs_pct_error = excluded.mean_abs_pct_error,
			last_event_id = excluded.last_event_id,
			updated_at = excluded.updated_at
	`, c.ID, c.ProviderID, c.Model, c.EstimateMethod, c.Bucket, c.SampleCount,
		c.EstimatedInputTokensSum, c.ActualInputTokensSum,
		c.EstimatedOutputTokensSum, c.ActualOutputTokensSum,
		c.CorrectionFactor, c.MeanAbsPctError, c.LastEventID, c.CreatedAt, c.UpdatedAt)
	return err
}

func (d *DB) ListTokenEstimatorCalibrations(ctx context.Context, filter TokenEstimatorCalibrationFilter) ([]TokenEstimatorCalibration, error) {
	var conditions []string
	var args []any
	if filter.ProviderID != "" {
		conditions = append(conditions, "provider_id = ?")
		args = append(args, filter.ProviderID)
	}
	if filter.Model != "" {
		conditions = append(conditions, "model = ?")
		args = append(args, filter.Model)
	}
	if filter.EstimateMethod != "" {
		conditions = append(conditions, "estimate_method = ?")
		args = append(args, filter.EstimateMethod)
	}
	if filter.Bucket != "" {
		conditions = append(conditions, "bucket = ?")
		args = append(args, filter.Bucket)
	}
	query := `
		SELECT id, provider_id, model, estimate_method, bucket, sample_count,
		       estimated_input_tokens_sum, actual_input_tokens_sum,
		       estimated_output_tokens_sum, actual_output_tokens_sum,
		       correction_factor, mean_abs_pct_error, last_event_id, created_at, updated_at
		FROM token_estimator_calibrations`
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY updated_at DESC"
	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TokenEstimatorCalibration
	for rows.Next() {
		var c TokenEstimatorCalibration
		if err := rows.Scan(&c.ID, &c.ProviderID, &c.Model, &c.EstimateMethod, &c.Bucket, &c.SampleCount,
			&c.EstimatedInputTokensSum, &c.ActualInputTokensSum,
			&c.EstimatedOutputTokensSum, &c.ActualOutputTokensSum,
			&c.CorrectionFactor, &c.MeanAbsPctError, &c.LastEventID, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (d *DB) RefreshTokenEstimatorCalibrations(ctx context.Context, filter TokenEstimatorCalibrationFilter) (int, error) {
	if filter.Bucket != "" && filter.Bucket != "global" {
		return 0, nil
	}
	conditions := []string{
		"status = 'success'",
		"estimate_method <> ''",
		"((actual_input_tokens > 0 AND estimated_input_tokens > 0) OR (actual_output_tokens > 0 AND estimated_output_tokens > 0))",
	}
	var args []any
	if filter.ProviderID != "" {
		conditions = append(conditions, "provider_id = ?")
		args = append(args, filter.ProviderID)
	}
	if filter.Model != "" {
		conditions = append(conditions, "model = ?")
		args = append(args, filter.Model)
	}
	if filter.EstimateMethod != "" {
		conditions = append(conditions, "estimate_method = ?")
		args = append(args, filter.EstimateMethod)
	}
	query := `
		SELECT provider_id, model, estimate_method,
		       COUNT(*),
		       COALESCE(SUM(CASE WHEN actual_input_tokens > 0 AND estimated_input_tokens > 0 THEN estimated_input_tokens ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN actual_input_tokens > 0 AND estimated_input_tokens > 0 THEN actual_input_tokens ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN actual_output_tokens > 0 AND estimated_output_tokens > 0 THEN estimated_output_tokens ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN actual_output_tokens > 0 AND estimated_output_tokens > 0 THEN actual_output_tokens ELSE 0 END), 0),
		       COALESCE(SUM(
		           CASE WHEN actual_input_tokens > 0 AND estimated_input_tokens > 0 THEN estimated_input_tokens ELSE 0 END +
		           CASE WHEN actual_output_tokens > 0 AND estimated_output_tokens > 0 THEN estimated_output_tokens ELSE 0 END
		       ), 0),
		       COALESCE(SUM(
		           CASE WHEN actual_input_tokens > 0 AND estimated_input_tokens > 0 THEN actual_input_tokens ELSE 0 END +
		           CASE WHEN actual_output_tokens > 0 AND estimated_output_tokens > 0 THEN actual_output_tokens ELSE 0 END
		       ), 0),
		       COALESCE(AVG(CASE
		           WHEN (
		               CASE WHEN actual_input_tokens > 0 AND estimated_input_tokens > 0 THEN actual_input_tokens ELSE 0 END +
		               CASE WHEN actual_output_tokens > 0 AND estimated_output_tokens > 0 THEN actual_output_tokens ELSE 0 END
		           ) > 0
		           THEN ABS((
		               CASE WHEN actual_input_tokens > 0 AND estimated_input_tokens > 0 THEN actual_input_tokens ELSE 0 END +
		               CASE WHEN actual_output_tokens > 0 AND estimated_output_tokens > 0 THEN actual_output_tokens ELSE 0 END
		           ) - (
		               CASE WHEN actual_input_tokens > 0 AND estimated_input_tokens > 0 THEN estimated_input_tokens ELSE 0 END +
		               CASE WHEN actual_output_tokens > 0 AND estimated_output_tokens > 0 THEN estimated_output_tokens ELSE 0 END
		           )) * 1.0 / (
		               CASE WHEN actual_input_tokens > 0 AND estimated_input_tokens > 0 THEN actual_input_tokens ELSE 0 END +
		               CASE WHEN actual_output_tokens > 0 AND estimated_output_tokens > 0 THEN actual_output_tokens ELSE 0 END
		           )
		           ELSE 0
		       END), 0),
		       MAX(id)
		FROM llm_usage_events
		WHERE ` + strings.Join(conditions, " AND ") + `
		GROUP BY provider_id, model, estimate_method`
	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var c TokenEstimatorCalibration
		var estimatedTotal, actualTotal int
		if err := rows.Scan(&c.ProviderID, &c.Model, &c.EstimateMethod, &c.SampleCount,
			&c.EstimatedInputTokensSum, &c.ActualInputTokensSum,
			&c.EstimatedOutputTokensSum, &c.ActualOutputTokensSum,
			&estimatedTotal, &actualTotal, &c.MeanAbsPctError, &c.LastEventID); err != nil {
			return count, err
		}
		c.Bucket = "global"
		c.CorrectionFactor = 1
		if estimatedTotal > 0 && actualTotal > 0 {
			c.CorrectionFactor = float64(actualTotal) / float64(estimatedTotal)
		}
		if err := d.UpsertTokenEstimatorCalibration(ctx, c); err != nil {
			return count, err
		}
		count++
	}
	return count, rows.Err()
}

func normalizeLLMUsageEvent(event LLMUsageEvent, now string) LLMUsageEvent {
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	if event.Component == "" {
		event.Component = "unknown"
	}
	if event.Status == "" {
		event.Status = "success"
	}
	if event.UsageSource == "" {
		event.UsageSource = "unknown"
	}
	if event.ActualTotalTokens == 0 && (event.ActualInputTokens > 0 || event.ActualOutputTokens > 0) {
		event.ActualTotalTokens = event.ActualInputTokens + event.ActualOutputTokens
	}
	if event.EstimatedTotalTokens == 0 && (event.EstimatedInputTokens > 0 || event.EstimatedOutputTokens > 0) {
		event.EstimatedTotalTokens = event.EstimatedInputTokens + event.EstimatedOutputTokens
	}
	if event.InputTokens == 0 && event.OutputTokens == 0 && event.TotalTokens == 0 {
		switch event.UsageSource {
		case "provider_usage":
			event.InputTokens = event.ActualInputTokens
			event.OutputTokens = event.ActualOutputTokens
			event.TotalTokens = event.ActualTotalTokens
		case "estimated":
			event.InputTokens = event.EstimatedInputTokens
			event.OutputTokens = event.EstimatedOutputTokens
			event.TotalTokens = event.EstimatedTotalTokens
		case "hybrid":
			if event.ActualInputTokens > 0 {
				event.InputTokens = event.ActualInputTokens
			} else {
				event.InputTokens = event.EstimatedInputTokens
			}
			if event.ActualOutputTokens > 0 {
				event.OutputTokens = event.ActualOutputTokens
			} else {
				event.OutputTokens = event.EstimatedOutputTokens
			}
			event.TotalTokens = event.InputTokens + event.OutputTokens
		}
	}
	if event.TotalTokens == 0 && (event.InputTokens > 0 || event.OutputTokens > 0) {
		event.TotalTokens = event.InputTokens + event.OutputTokens
	}
	if event.MetadataJSON == "" {
		event.MetadataJSON = "{}"
	}
	if event.RawUsageJSON == "" {
		event.RawUsageJSON = "{}"
	}
	if event.CreatedAt == "" {
		event.CreatedAt = now
	}
	return event
}

func llmUsageConditions(filter LLMUsageEventFilter) ([]string, []any) {
	var conditions []string
	var args []any
	add := func(column, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		conditions = append(conditions, column+" = ?")
		args = append(args, value)
	}
	add("session_id", filter.SessionID)
	add("provider_id", filter.ProviderID)
	add("model", filter.Model)
	add("component", filter.Component)
	add("operation", filter.Operation)
	add("plugin_id", filter.PluginID)
	add("task_id", filter.TaskID)
	add("usage_source", filter.UsageSource)
	add("status", filter.Status)
	if filter.From != "" {
		conditions = append(conditions, "created_at >= ?")
		args = append(args, filter.From)
	}
	if filter.To != "" {
		conditions = append(conditions, "created_at <= ?")
		args = append(args, filter.To)
	}
	return conditions, args
}

func llmUsageSelectSQL() string {
	return `
		SELECT id, request_id, provider_request_id, response_id, session_id, turn_id,
		       agent_id, persona_key, plugin_id, task_id, component, operation,
		       provider_id, provider_name, protocol, model, endpoint, stream,
		       status, error_kind, error_message, duration_ms,
		       input_tokens, output_tokens, total_tokens,
		       estimated_input_tokens, estimated_output_tokens, estimated_total_tokens,
		       estimate_method, estimate_confidence,
		       actual_input_tokens, actual_output_tokens, actual_total_tokens,
		       cached_input_tokens, cache_hit_input_tokens, cache_miss_input_tokens,
		       cache_read_tokens, cache_write_tokens, reasoning_tokens, image_tokens, audio_tokens,
		       usage_source, prompt_hash, completion_hash, raw_usage_json, metadata_json, created_at
		FROM llm_usage_events`
}

func scanLLMUsageEvent(scanner interface{ Scan(...any) error }) (LLMUsageEvent, error) {
	var event LLMUsageEvent
	var stream int
	err := scanner.Scan(&event.ID, &event.RequestID, &event.ProviderRequestID, &event.ResponseID, &event.SessionID, &event.TurnID,
		&event.AgentID, &event.PersonaKey, &event.PluginID, &event.TaskID, &event.Component, &event.Operation,
		&event.ProviderID, &event.ProviderName, &event.Protocol, &event.Model, &event.Endpoint, &stream,
		&event.Status, &event.ErrorKind, &event.ErrorMessage, &event.DurationMS,
		&event.InputTokens, &event.OutputTokens, &event.TotalTokens,
		&event.EstimatedInputTokens, &event.EstimatedOutputTokens, &event.EstimatedTotalTokens,
		&event.EstimateMethod, &event.EstimateConfidence,
		&event.ActualInputTokens, &event.ActualOutputTokens, &event.ActualTotalTokens,
		&event.CachedInputTokens, &event.CacheHitInputTokens, &event.CacheMissInputTokens,
		&event.CacheReadTokens, &event.CacheWriteTokens, &event.ReasoningTokens, &event.ImageTokens, &event.AudioTokens,
		&event.UsageSource, &event.PromptHash, &event.CompletionHash, &event.RawUsageJSON, &event.MetadataJSON, &event.CreatedAt)
	event.Stream = stream != 0
	return event, err
}

func usageGroupExprs(groupBy []string) ([]string, []string, error) {
	var exprs []string
	var names []string
	for _, group := range groupBy {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		expr, ok := map[string]string{
			"provider":  "provider_id",
			"model":     "model",
			"component": "component",
			"operation": "operation",
			"plugin":    "plugin_id",
			"session":   "session_id",
			"source":    "usage_source",
			"day":       "substr(created_at, 1, 10)",
			"hour":      "substr(created_at, 1, 13)",
		}[group]
		if !ok {
			return nil, nil, fmt.Errorf("unsupported usage summary group_by %q", group)
		}
		exprs = append(exprs, expr)
		names = append(names, group)
	}
	return exprs, names, nil
}

func scanLLMUsageSummaryRow(rows *sql.Rows, groupNames []string) (LLMUsageSummaryRow, error) {
	var row LLMUsageSummaryRow
	groupValues := make([]string, len(groupNames))
	scanArgs := make([]any, 0, len(groupNames)+18)
	for i := range groupValues {
		scanArgs = append(scanArgs, &groupValues[i])
	}
	scanArgs = append(scanArgs,
		&row.RequestCount,
		&row.ErrorCount,
		&row.InputTokens,
		&row.OutputTokens,
		&row.TotalTokens,
		&row.EstimatedInputTokens,
		&row.EstimatedOutputTokens,
		&row.ActualInputTokens,
		&row.ActualOutputTokens,
		&row.CachedInputTokens,
		&row.CacheHitInputTokens,
		&row.CacheMissInputTokens,
		&row.ReasoningTokens,
		&row.ImageTokens,
		&row.AudioTokens,
		&row.ProviderUsageCount,
		&row.EstimatedUsageCount,
		&row.HybridUsageCount,
	)
	if err := rows.Scan(scanArgs...); err != nil {
		return LLMUsageSummaryRow{}, err
	}
	for i, name := range groupNames {
		switch name {
		case "provider":
			row.ProviderID = groupValues[i]
		case "model":
			row.Model = groupValues[i]
		case "component":
			row.Component = groupValues[i]
		case "operation":
			row.Operation = groupValues[i]
		case "plugin":
			row.PluginID = groupValues[i]
		case "session":
			row.SessionID = groupValues[i]
		case "source":
			row.UsageSource = groupValues[i]
		case "day":
			row.Day = groupValues[i]
		case "hour":
			row.Hour = groupValues[i]
		}
	}
	return row, nil
}
