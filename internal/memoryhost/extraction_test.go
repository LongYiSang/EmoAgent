package memoryhost

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"github.com/longyisang/emoagent/internal/storage"
)

func TestBridgeFinalizeSegmentDryRunExtractionDoesNotWriteFacts(t *testing.T) {
	fixture := openExtractionBridgeFixture(t, "chat-extract-dry", ExtractionConfig{
		Enabled:                  true,
		Mode:                     memorycore.ExtractionRunModeDryRun,
		TriggerOnFinalizeSegment: true,
		Provider: ExtractionProviderConfig{
			Kind:  "fake",
			ID:    "fake",
			Model: "fake-model",
		},
		AuditEnabled: true,
	}, &fakeExtractionLLM{})

	if _, err := fixture.bridge.AppendUserEpisode(fixture.ctx, fixture.segment.SegmentID, "msg-user", "我喜欢手冲咖啡，尤其是浅烘。"); err != nil {
		t.Fatalf("AppendUserEpisode: %v", err)
	}
	if _, err := fixture.bridge.AppendAssistantEpisode(fixture.ctx, fixture.segment.SegmentID, "msg-assistant", "记下了。"); err != nil {
		t.Fatalf("AppendAssistantEpisode: %v", err)
	}
	if err := fixture.bridge.FinalizeSegment(fixture.ctx, fixture.segment.SegmentID, "session_end", ""); err != nil {
		t.Fatalf("FinalizeSegment: %v", err)
	}

	requireMemoryFactCount(t, fixture.memoryDBPath, "", 0)
	requireMemoryExtractionAuditStatus(t, fixture.memoryDBPath, memorycore.ExtractionRunStatusDryRun)
}

func TestBridgeFinalizeSegmentExtractionWritesRawLogWhenEnabled(t *testing.T) {
	rawLogDir := filepath.Join(t.TempDir(), "raw")
	fixture := openExtractionBridgeFixture(t, "chat-extract-raw-log", ExtractionConfig{
		Enabled:                  true,
		Mode:                     memorycore.ExtractionRunModeDryRun,
		TriggerOnFinalizeSegment: true,
		RawLog: ExtractionRawLogConfig{
			Enabled:   true,
			Directory: rawLogDir,
		},
		Provider: ExtractionProviderConfig{
			Kind:  "fake",
			ID:    "fake",
			Model: "fake-model",
		},
		AuditEnabled: true,
	}, &fakeExtractionLLM{})

	if _, err := fixture.bridge.AppendUserEpisode(fixture.ctx, fixture.segment.SegmentID, "msg-user", "我喜欢手冲咖啡。"); err != nil {
		t.Fatalf("AppendUserEpisode: %v", err)
	}
	if err := fixture.bridge.FinalizeSegment(fixture.ctx, fixture.segment.SegmentID, "session_end", ""); err != nil {
		t.Fatalf("FinalizeSegment: %v", err)
	}

	entries, err := os.ReadDir(rawLogDir)
	if err != nil {
		t.Fatalf("ReadDir(raw log): %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("raw log file count = %d, want 1", len(entries))
	}
}

func TestBridgeFinalizeSegmentApplyExtractionWritesRetrievableFact(t *testing.T) {
	fixture := openExtractionBridgeFixture(t, "chat-extract-apply", ExtractionConfig{
		Enabled:                  true,
		Mode:                     memorycore.ExtractionRunModeApply,
		TriggerOnFinalizeSegment: true,
		Provider: ExtractionProviderConfig{
			Kind:  "fake",
			ID:    "fake",
			Model: "fake-model",
		},
		AuditEnabled: true,
	}, &fakeExtractionLLM{})

	if _, err := fixture.bridge.AppendUserEpisode(fixture.ctx, fixture.segment.SegmentID, "msg-user", "我喜欢手冲咖啡，尤其是浅烘。"); err != nil {
		t.Fatalf("AppendUserEpisode: %v", err)
	}
	if err := fixture.bridge.FinalizeSegment(fixture.ctx, fixture.segment.SegmentID, "session_end", ""); err != nil {
		t.Fatalf("FinalizeSegment: %v", err)
	}

	fact := requireMemoryFact(t, fixture.memoryDBPath, "likes", "手冲咖啡")
	if fact.ContentSummary != "用户喜欢手冲咖啡。" {
		t.Fatalf("content summary = %q, want 用户喜欢手冲咖啡。", fact.ContentSummary)
	}
	requireMemoryExtractionAuditStatus(t, fixture.memoryDBPath, memorycore.ExtractionRunStatusApplied)

	if _, err := fixture.bridge.EnsureSegment(fixture.ctx, "chat-extract-apply", "default"); err != nil {
		t.Fatalf("EnsureSegment(next): %v", err)
	}
	block, err := fixture.bridge.RetrievePromptBlock(fixture.ctx, "chat-extract-apply", "下午喝点什么好，手冲咖啡可以吗")
	if err != nil {
		t.Fatalf("RetrievePromptBlock: %v", err)
	}
	if !strings.Contains(block, "用户喜欢手冲咖啡。") {
		t.Fatalf("prompt block = %q, want extracted fact", block)
	}
}

func TestBridgeFinalizeSegmentExtractionFailureDoesNotFailFinalize(t *testing.T) {
	fixture := openExtractionBridgeFixture(t, "chat-extract-fail", ExtractionConfig{
		Enabled:                  true,
		Mode:                     memorycore.ExtractionRunModeApply,
		TriggerOnFinalizeSegment: true,
		Provider: ExtractionProviderConfig{
			Kind:  "fake",
			ID:    "fake",
			Model: "fake-model",
		},
		AuditEnabled: false,
	}, &fakeExtractionLLM{err: errors.New("raw provider failed with user text 我喜欢手冲咖啡")})

	if _, err := fixture.bridge.AppendUserEpisode(fixture.ctx, fixture.segment.SegmentID, "msg-user", "我喜欢手冲咖啡。"); err != nil {
		t.Fatalf("AppendUserEpisode: %v", err)
	}
	if err := fixture.bridge.FinalizeSegment(fixture.ctx, fixture.segment.SegmentID, "session_end", ""); err != nil {
		t.Fatalf("FinalizeSegment returned extraction error: %v", err)
	}
	requireMemoryFactCount(t, fixture.memoryDBPath, "", 0)
}

func TestExtractSessionEndReturnsSanitizedProviderError(t *testing.T) {
	fixture := openExtractionBridgeFixture(t, "chat-extract-sanitize", ExtractionConfig{
		Enabled:                  true,
		Mode:                     memorycore.ExtractionRunModeApply,
		TriggerOnFinalizeSegment: true,
		Provider: ExtractionProviderConfig{
			Kind:  "fake",
			ID:    "fake",
			Model: "fake-model",
		},
		AuditEnabled: false,
	}, &fakeExtractionLLM{err: errors.New("raw provider failed with user text 我喜欢手冲咖啡")})

	if _, err := fixture.bridge.AppendUserEpisode(fixture.ctx, fixture.segment.SegmentID, "msg-user", "我喜欢手冲咖啡。"); err != nil {
		t.Fatalf("AppendUserEpisode: %v", err)
	}
	_, err := fixture.host.ExtractSessionEnd(fixture.ctx, "default", fixture.segment.MemorySessionID)
	if err == nil {
		t.Fatal("ExtractSessionEnd error = nil, want sanitized provider error")
	}
	if strings.Contains(err.Error(), "手冲咖啡") || strings.Contains(err.Error(), "raw provider failed") {
		t.Fatalf("error leaked provider text: %v", err)
	}
}

func TestBridgeFinalizeSegmentRepeatedCallDoesNotRepeatExtraction(t *testing.T) {
	llm := &fakeExtractionLLM{}
	fixture := openExtractionBridgeFixture(t, "chat-extract-repeat", ExtractionConfig{
		Enabled:                  true,
		Mode:                     memorycore.ExtractionRunModeApply,
		TriggerOnFinalizeSegment: true,
		Provider: ExtractionProviderConfig{
			Kind:  "fake",
			ID:    "fake",
			Model: "fake-model",
		},
		AuditEnabled: true,
	}, llm)

	if _, err := fixture.bridge.AppendUserEpisode(fixture.ctx, fixture.segment.SegmentID, "msg-user", "我喜欢手冲咖啡。"); err != nil {
		t.Fatalf("AppendUserEpisode: %v", err)
	}
	if err := fixture.bridge.FinalizeSegment(fixture.ctx, fixture.segment.SegmentID, "session_end", ""); err != nil {
		t.Fatalf("FinalizeSegment(first): %v", err)
	}
	if err := fixture.bridge.FinalizeSegment(fixture.ctx, fixture.segment.SegmentID, "session_end", ""); err != nil {
		t.Fatalf("FinalizeSegment(second): %v", err)
	}

	if llm.extractCalls != 1 {
		t.Fatalf("extract calls = %d, want 1", llm.extractCalls)
	}
	requireMemoryFactCount(t, fixture.memoryDBPath, "likes", 1)
}

func TestBridgeFinalizeSegmentExtractionDisabledDoesNotCallLLM(t *testing.T) {
	llm := &fakeExtractionLLM{}
	fixture := openExtractionBridgeFixture(t, "chat-extract-disabled", ExtractionConfig{
		Enabled:                  false,
		Mode:                     memorycore.ExtractionRunModeApply,
		TriggerOnFinalizeSegment: true,
	}, llm)

	if _, err := fixture.bridge.AppendUserEpisode(fixture.ctx, fixture.segment.SegmentID, "msg-user", "我喜欢手冲咖啡。"); err != nil {
		t.Fatalf("AppendUserEpisode: %v", err)
	}
	if err := fixture.bridge.FinalizeSegment(fixture.ctx, fixture.segment.SegmentID, "session_end", ""); err != nil {
		t.Fatalf("FinalizeSegment: %v", err)
	}
	if llm.extractCalls != 0 {
		t.Fatalf("extract calls = %d, want 0", llm.extractCalls)
	}
	requireMemoryFactCount(t, fixture.memoryDBPath, "", 0)
}

type extractionBridgeFixture struct {
	ctx          context.Context
	host         *Host
	bridge       *Bridge
	segment      storage.MemorySegmentRef
	memoryDBPath string
}

func openExtractionBridgeFixture(t *testing.T, chatSessionID string, cfg ExtractionConfig, llm memorycore.ExtractionLLM) extractionBridgeFixture {
	t.Helper()

	ctx := context.Background()
	logger := testMemoryLogger()
	chatDB := openBridgeChatDB(t, logger)
	if err := chatDB.CreateSession(ctx, chatSessionID, "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	memoryDBPath := filepath.Join(t.TempDir(), "memory.db")
	host, err := OpenWithOptions(ctx, memorycore.Options{
		DBPath:      memoryDBPath,
		PersonaID:   "default",
		AutoMigrate: true,
		EnableFTS:   true,
		Now: func() time.Time {
			return time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
		},
	}, logger)
	if err != nil {
		t.Fatalf("OpenWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = host.Close() })

	if cfg.Enabled {
		runner, err := NewExtractionRunnerWithLLM(ctx, host, cfg, logger, llm)
		if err != nil {
			t.Fatalf("NewExtractionRunnerWithLLM: %v", err)
		}
		host.SetExtractionRunner(runner)
	}

	bridge := NewBridge(host, chatDB, logger, DefaultManualRules())
	segment, err := bridge.EnsureSegment(ctx, chatSessionID, "default")
	if err != nil {
		t.Fatalf("EnsureSegment: %v", err)
	}

	return extractionBridgeFixture{
		ctx:          ctx,
		host:         host,
		bridge:       bridge,
		segment:      segment,
		memoryDBPath: memoryDBPath,
	}
}

type fakeExtractionLLM struct {
	err          error
	extractCalls int
}

func (f *fakeExtractionLLM) CompleteJSON(_ context.Context, req memorycore.ExtractionLLMRequest) (memorycore.ExtractionLLMResponse, error) {
	if req.Purpose != memorycore.ExtractionLLMPurposeExtraction {
		return memorycore.ExtractionLLMResponse{Text: "{}"}, nil
	}
	f.extractCalls++
	if f.err != nil {
		return memorycore.ExtractionLLMResponse{}, f.err
	}

	var extractReq memorycore.ExtractionRequest
	_ = json.Unmarshal([]byte(req.UserPrompt), &extractReq)
	episodeIDs := make([]string, 0, len(extractReq.Episodes))
	for _, episode := range extractReq.Episodes {
		episodeIDs = append(episodeIDs, episode.EpisodeID)
	}
	if len(episodeIDs) == 0 {
		episodeIDs = []string{"unknown"}
	}

	body := map[string]any{
		"schema_version": memorycore.ExtractionResponseSchemaVersion,
		"request_id":     extractReq.RequestID,
		"persona_id":     extractReq.PersonaID,
		"session_id":     extractReq.SessionID,
		"trigger":        extractReq.Trigger,
		"source_window":  map[string]any{"episode_ids": episodeIDs, "started_at": nil, "ended_at": nil},
		"entities":       []any{},
		"facts": []any{map[string]any{
			"candidate_id":                "fact_1",
			"subject_entity_candidate_id": "user",
			"predicate":                   "likes",
			"object_entity_candidate_id":  nil,
			"object_literal":              "手冲咖啡",
			"content_summary":             "用户喜欢手冲咖啡。",
			"fact_type":                   memorycore.FactTypeStablePreference,
			"valid_from":                  nil,
			"valid_to":                    nil,
			"temporal_precision":          "unknown",
			"extraction_confidence":       memorycore.ConfidenceExplicit,
			"extraction_confidence_score": 0.9,
			"importance":                  0.7,
			"valence":                     0.2,
			"arousal":                     0.2,
			"sensitivity_level":           memorycore.SensitivityNormal,
			"source_episode_ids":          []string{episodeIDs[0]},
			"evidence_notes":              nil,
			"reasoning":                   nil,
			"operation_hint":              "insert_candidate",
			"pinned":                      false,
			"user_requested":              false,
			"searchable_hint":             true,
			"quality_decision":            "accept_for_consolidation",
			"quality_reasons":             []string{"test"},
		}},
		"links":               []any{},
		"affect_events":       []any{},
		"deletion_intents":    []any{},
		"pin_intents":         []any{},
		"correction_hints":    []any{},
		"rejected_candidates": []any{},
		"quality_flags":       []any{},
		"gate_summary": map[string]any{
			"accepted_fact_count":   1,
			"needs_review_count":    0,
			"rejected_count":        0,
			"has_deletion_intent":   false,
			"has_pin_intent":        false,
			"requires_human_review": false,
			"notes":                 "test",
			"routed_count":          0,
			"not_applied_count":     0,
		},
	}
	data, _ := json.Marshal(body)
	return memorycore.ExtractionLLMResponse{Text: string(data), Model: "fake-model"}, nil
}

func requireMemoryExtractionAuditStatus(t *testing.T, dbPath string, want memorycore.ExtractionRunStatus) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open(memory): %v", err)
	}
	defer db.Close()

	var got string
	if err := db.QueryRow(`
		SELECT status
		FROM extraction_runs
		ORDER BY created_at DESC
		LIMIT 1
	`).Scan(&got); err != nil {
		t.Fatalf("read extraction audit status: %v", err)
	}
	if got != string(want) {
		t.Fatalf("extraction audit status = %q, want %q", got, want)
	}
}
