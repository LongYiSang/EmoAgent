package storage

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func TestLLMUsageMigrationFreshDB(t *testing.T) {
	db := testDB(t)

	for _, table := range []string{"llm_usage_events", "token_estimator_calibrations"} {
		var name string
		if err := db.SqlDB().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name); err != nil {
			t.Fatalf("table %s not found: %v", table, err)
		}
	}
	for _, column := range []string{"cost_micros", "currency", "price_card_id", "billing_tokens"} {
		if hasTableColumn(t, db.SqlDB(), "llm_usage_events", column) {
			t.Fatalf("llm_usage_events should not contain pricing column %q", column)
		}
	}
}

func TestRecordLLMUsageEventDefaults(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	if err := db.RecordLLMUsageEvent(ctx, LLMUsageEvent{
		RequestID:            "req-1",
		SessionID:            "session-1",
		Component:            "emotion_chat",
		Operation:            "chat_stream",
		ProviderID:           "deepseek",
		Model:                "deepseek-v4-pro",
		UsageSource:          "provider_usage",
		ActualInputTokens:    10,
		ActualOutputTokens:   5,
		EstimatedInputTokens: 9,
		EstimateMethod:       "heuristic_cjk",
		EstimateConfidence:   0.55,
		Status:               "success",
		RawUsageJSON:         `{"prompt_tokens":10}`,
	}); err != nil {
		t.Fatalf("RecordLLMUsageEvent: %v", err)
	}

	events, err := db.ListLLMUsageEvents(ctx, LLMUsageEventFilter{SessionID: "session-1", Limit: 10})
	if err != nil {
		t.Fatalf("ListLLMUsageEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %#v, want one event", events)
	}
	got := events[0]
	if strings.TrimSpace(got.ID) == "" || strings.TrimSpace(got.CreatedAt) == "" {
		t.Fatalf("event missing generated id/created_at: %#v", got)
	}
	if got.TotalTokens != 15 || got.ActualTotalTokens != 15 || got.EstimatedTotalTokens != 9 {
		t.Fatalf("totals = %#v, want effective/provider/estimate totals", got)
	}
	if got.MetadataJSON != "{}" {
		t.Fatalf("metadata_json = %q, want {}", got.MetadataJSON)
	}
}

func TestListLLMUsageEventsFilters(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	seedUsageEvent(t, db, LLMUsageEvent{ID: "usage-1", SessionID: "s1", Component: "emotion_chat", ProviderID: "deepseek", Model: "m1", UsageSource: "provider_usage", TotalTokens: 10, Status: "success"})
	seedUsageEvent(t, db, LLMUsageEvent{ID: "usage-2", SessionID: "s2", Component: "plugin", PluginID: "plugin-a", ProviderID: "moonshot", Model: "m2", UsageSource: "estimated", TotalTokens: 20, Status: "error"})

	events, err := db.ListLLMUsageEvents(ctx, LLMUsageEventFilter{ProviderID: "moonshot", Component: "plugin", UsageSource: "estimated", Status: "error", Limit: 10})
	if err != nil {
		t.Fatalf("ListLLMUsageEvents: %v", err)
	}
	if len(events) != 1 || events[0].ID != "usage-2" {
		t.Fatalf("events = %#v, want usage-2", events)
	}
}

func TestSummarizeLLMUsageByProviderModelComponent(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	seedUsageEvent(t, db, LLMUsageEvent{ID: "usage-1", ProviderID: "deepseek", Model: "m1", Component: "emotion_chat", UsageSource: "provider_usage", InputTokens: 10, OutputTokens: 5, TotalTokens: 15, Status: "success"})
	seedUsageEvent(t, db, LLMUsageEvent{ID: "usage-2", ProviderID: "deepseek", Model: "m1", Component: "emotion_chat", UsageSource: "estimated", InputTokens: 4, OutputTokens: 2, TotalTokens: 6, Status: "error"})
	seedUsageEvent(t, db, LLMUsageEvent{ID: "usage-3", ProviderID: "moonshot", Model: "m2", Component: "plugin", UsageSource: "provider_usage", InputTokens: 3, OutputTokens: 2, TotalTokens: 5, Status: "success"})

	rows, err := db.SummarizeLLMUsage(ctx, LLMUsageSummaryFilter{GroupBy: []string{"provider", "model", "component"}})
	if err != nil {
		t.Fatalf("SummarizeLLMUsage: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("summary rows = %#v, want two groups", rows)
	}
	var deepseek LLMUsageSummaryRow
	for _, row := range rows {
		if row.ProviderID == "deepseek" {
			deepseek = row
		}
	}
	if deepseek.RequestCount != 2 || deepseek.ErrorCount != 1 || deepseek.InputTokens != 14 || deepseek.OutputTokens != 7 || deepseek.TotalTokens != 21 {
		t.Fatalf("deepseek summary = %#v", deepseek)
	}
	if deepseek.ProviderUsageCount != 1 || deepseek.EstimatedUsageCount != 1 {
		t.Fatalf("usage source counts = %#v", deepseek)
	}
}

func TestTokenEstimatorCalibrationUpsertAndList(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	if err := db.UpsertTokenEstimatorCalibration(ctx, TokenEstimatorCalibration{
		ProviderID:       "deepseek",
		Model:            "m1",
		EstimateMethod:   "heuristic_cjk",
		Bucket:           "input",
		SampleCount:      3,
		CorrectionFactor: 1.2,
		MeanAbsPctError:  0.1,
		LastEventID:      "usage-3",
	}); err != nil {
		t.Fatalf("UpsertTokenEstimatorCalibration: %v", err)
	}

	calibrations, err := db.ListTokenEstimatorCalibrations(ctx, TokenEstimatorCalibrationFilter{ProviderID: "deepseek", Model: "m1"})
	if err != nil {
		t.Fatalf("ListTokenEstimatorCalibrations: %v", err)
	}
	if len(calibrations) != 1 || calibrations[0].CorrectionFactor != 1.2 {
		t.Fatalf("calibrations = %#v", calibrations)
	}
}

func TestRefreshTokenEstimatorCalibrations(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	seedUsageEvent(t, db, LLMUsageEvent{
		ID:                    "usage-1",
		ProviderID:            "deepseek",
		Model:                 "m1",
		UsageSource:           "hybrid",
		Status:                "success",
		EstimateMethod:        "heuristic_cjk",
		EstimatedInputTokens:  8,
		EstimatedOutputTokens: 4,
		ActualInputTokens:     10,
		ActualOutputTokens:    5,
	})
	seedUsageEvent(t, db, LLMUsageEvent{
		ID:                    "usage-2",
		ProviderID:            "deepseek",
		Model:                 "m1",
		UsageSource:           "provider_usage",
		Status:                "success",
		EstimateMethod:        "heuristic_cjk",
		EstimatedInputTokens:  12,
		EstimatedOutputTokens: 8,
		ActualInputTokens:     15,
		ActualOutputTokens:    10,
	})

	count, err := db.RefreshTokenEstimatorCalibrations(ctx, TokenEstimatorCalibrationFilter{ProviderID: "deepseek"})
	if err != nil {
		t.Fatalf("RefreshTokenEstimatorCalibrations: %v", err)
	}
	if count != 1 {
		t.Fatalf("refresh count = %d, want 1", count)
	}
	calibrations, err := db.ListTokenEstimatorCalibrations(ctx, TokenEstimatorCalibrationFilter{ProviderID: "deepseek", Model: "m1"})
	if err != nil {
		t.Fatalf("ListTokenEstimatorCalibrations: %v", err)
	}
	if len(calibrations) != 1 {
		t.Fatalf("calibrations = %#v, want one", calibrations)
	}
	got := calibrations[0]
	if got.SampleCount != 2 || got.EstimatedInputTokensSum != 20 || got.ActualInputTokensSum != 25 || got.Bucket != "global" {
		t.Fatalf("calibration = %#v, want aggregate sums", got)
	}
	if got.CorrectionFactor <= 1.2 || got.CorrectionFactor >= 1.3 {
		t.Fatalf("correction factor = %v, want around 1.25", got.CorrectionFactor)
	}
}

func TestRefreshTokenEstimatorCalibrationsUsesComparableHybridDimensions(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	seedUsageEvent(t, db, LLMUsageEvent{
		ID:                    "usage-output-only",
		ProviderID:            "moonshot",
		Model:                 "m1",
		UsageSource:           "hybrid",
		Status:                "success",
		EstimateMethod:        "heuristic_cjk",
		EstimatedInputTokens:  1000,
		EstimatedOutputTokens: 10,
		ActualOutputTokens:    20,
	})

	count, err := db.RefreshTokenEstimatorCalibrations(ctx, TokenEstimatorCalibrationFilter{ProviderID: "moonshot"})
	if err != nil {
		t.Fatalf("RefreshTokenEstimatorCalibrations: %v", err)
	}
	if count != 1 {
		t.Fatalf("refresh count = %d, want 1", count)
	}
	calibrations, err := db.ListTokenEstimatorCalibrations(ctx, TokenEstimatorCalibrationFilter{ProviderID: "moonshot", Model: "m1"})
	if err != nil {
		t.Fatalf("ListTokenEstimatorCalibrations: %v", err)
	}
	got := calibrations[0]
	if got.EstimatedInputTokensSum != 0 || got.ActualInputTokensSum != 0 {
		t.Fatalf("input sums = estimated %d actual %d, want unmatched input excluded", got.EstimatedInputTokensSum, got.ActualInputTokensSum)
	}
	if got.EstimatedOutputTokensSum != 10 || got.ActualOutputTokensSum != 20 || got.CorrectionFactor != 2 {
		t.Fatalf("calibration = %#v, want output-only comparison", got)
	}
}

func seedUsageEvent(t *testing.T, db *DB, event LLMUsageEvent) {
	t.Helper()
	if err := db.RecordLLMUsageEvent(context.Background(), event); err != nil {
		t.Fatalf("RecordLLMUsageEvent(%s): %v", event.ID, err)
	}
}

func hasTableColumn(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatalf("PRAGMA table_info(%s): %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultVal any
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			t.Fatalf("scan table_info(%s): %v", table, err)
		}
		if name == column {
			return true
		}
	}
	return false
}
