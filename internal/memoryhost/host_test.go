package memoryhost

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"github.com/longyisang/emoagent/internal/storage"
	_ "modernc.org/sqlite"
)

func TestOpenWithOptionsCreatesMemoryDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")

	host, err := OpenWithOptions(context.Background(), memorycore.Options{
		DBPath:      dbPath,
		AutoMigrate: true,
		EnableFTS:   true,
	}, testMemoryLogger())
	if err != nil {
		t.Fatalf("OpenWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = host.Close() })

	if host.DBPath != dbPath {
		t.Fatalf("Host.DBPath = %q, want %q", host.DBPath, dbPath)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("memory db was not created: %v", err)
	}
}

func TestOpenWithOptionsRequiresAutoMigrate(t *testing.T) {
	_, err := OpenWithOptions(context.Background(), memorycore.Options{
		DBPath: filepath.Join(t.TempDir(), "memory.db"),
	}, testMemoryLogger())
	if err == nil {
		t.Fatal("OpenWithOptions succeeded with AutoMigrate=false, want error")
	}
	if !strings.Contains(err.Error(), "AutoMigrate") {
		t.Fatalf("error = %q, want AutoMigrate", err.Error())
	}
}

func TestOpenFromConfigCreatesMemoryDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "memory.db")
	configPath := writeMemoryCoreConfig(t, dir, true, true, dbPath)

	host, err := OpenFromConfig(context.Background(), configPath, testMemoryLogger())
	if err != nil {
		t.Fatalf("OpenFromConfig: %v", err)
	}
	t.Cleanup(func() { _ = host.Close() })

	if host.Source != configPath {
		t.Fatalf("Host.Source = %q, want %q", host.Source, configPath)
	}
	if host.DBPath != filepath.ToSlash(dbPath) {
		t.Fatalf("Host.DBPath = %q, want %q", host.DBPath, filepath.ToSlash(dbPath))
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("memory db was not created: %v", err)
	}
}

func TestOpenFromConfigStoresRetrievalPolicy(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "memory.db")
	configPath := writeMemoryCoreConfig(t, dir, true, true, dbPath)

	host, err := OpenFromConfig(context.Background(), configPath, testMemoryLogger())
	if err != nil {
		t.Fatalf("OpenFromConfig: %v", err)
	}
	t.Cleanup(func() { _ = host.Close() })

	if host.retrievalPolicy.FinalMemoryCount != 3 {
		t.Fatalf("FinalMemoryCount = %d, want 3", host.retrievalPolicy.FinalMemoryCount)
	}
	if host.retrievalPolicy.ContextBudgetTokens != 321 {
		t.Fatalf("ContextBudgetTokens = %d, want 321", host.retrievalPolicy.ContextBudgetTokens)
	}
	if host.retrievalPolicy.UseFTS {
		t.Fatal("UseFTS = true, want false")
	}
	if host.retrievalPolicy.UseMirror {
		t.Fatal("UseMirror = true, want false")
	}
	if host.retrievalPolicy.SensitivityPermission != memorycore.SensitivitySensitive {
		t.Fatalf("SensitivityPermission = %q, want %q", host.retrievalPolicy.SensitivityPermission, memorycore.SensitivitySensitive)
	}
}

func TestOpenFromConfigRejectsMissingConfig(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.yaml")

	_, err := OpenFromConfig(context.Background(), missing, testMemoryLogger())
	if err == nil {
		t.Fatal("OpenFromConfig succeeded with missing config, want error")
	}
	if !strings.Contains(err.Error(), "load memorycore config") {
		t.Fatalf("error = %q, want load memorycore config", err.Error())
	}
}

func TestOpenFromConfigRequiresEnabledConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := writeMemoryCoreConfig(t, dir, false, true, filepath.Join(dir, "memory.db"))

	_, err := OpenFromConfig(context.Background(), configPath, testMemoryLogger())
	if err == nil {
		t.Fatal("OpenFromConfig succeeded with enabled=false, want error")
	}
	if !strings.Contains(err.Error(), "enabled must be true") {
		t.Fatalf("error = %q, want enabled must be true", err.Error())
	}
}

func TestOpenFromConfigRequiresAutoMigrate(t *testing.T) {
	dir := t.TempDir()
	configPath := writeMemoryCoreConfig(t, dir, true, false, filepath.Join(dir, "memory.db"))

	_, err := OpenFromConfig(context.Background(), configPath, testMemoryLogger())
	if err == nil {
		t.Fatal("OpenFromConfig succeeded with auto_migrate=false, want error")
	}
	if !strings.Contains(err.Error(), "core.auto_migrate must be true") {
		t.Fatalf("error = %q, want core.auto_migrate must be true", err.Error())
	}
}

func TestBridgeEnsureAppendAndFinalizeSegment(t *testing.T) {
	ctx := context.Background()
	logger := testMemoryLogger()
	chatDB := openBridgeChatDB(t, logger)
	if err := chatDB.CreateSession(ctx, "chat-1", "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	memoryDBPath := filepath.Join(t.TempDir(), "memory.db")
	host, err := OpenWithOptions(ctx, memorycore.Options{
		DBPath:      memoryDBPath,
		PersonaID:   "default",
		AutoMigrate: true,
		EnableFTS:   true,
	}, logger)
	if err != nil {
		t.Fatalf("OpenWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = host.Close() })

	bridge := NewBridge(host, chatDB, logger, nil)
	segment, err := bridge.EnsureSegment(ctx, "chat-1", "default")
	if err != nil {
		t.Fatalf("EnsureSegment: %v", err)
	}
	if segment.SegmentID == "" || segment.MemorySessionID == "" {
		t.Fatalf("segment = %#v, want ids", segment)
	}

	userEpisodeID, err := bridge.AppendUserEpisode(ctx, segment.SegmentID, "msg-user", "hello")
	if err != nil {
		t.Fatalf("AppendUserEpisode: %v", err)
	}
	assistantEpisodeID, err := bridge.AppendAssistantEpisode(ctx, segment.SegmentID, "msg-assistant", "hi")
	if err != nil {
		t.Fatalf("AppendAssistantEpisode: %v", err)
	}
	if userEpisodeID == "" || assistantEpisodeID == "" || userEpisodeID == assistantEpisodeID {
		t.Fatalf("episode ids = %q/%q, want distinct non-empty ids", userEpisodeID, assistantEpisodeID)
	}

	stored, err := chatDB.GetMemorySegment(ctx, segment.SegmentID)
	if err != nil {
		t.Fatalf("GetMemorySegment: %v", err)
	}
	if stored.LastUserEpisodeID != userEpisodeID || stored.LastAssistantEpisodeID != assistantEpisodeID {
		t.Fatalf("stored episode ids = %#v, want %q/%q", stored, userEpisodeID, assistantEpisodeID)
	}

	requireMemoryEpisodeCount(t, memoryDBPath, segment.MemorySessionID, "user", 1)
	requireMemoryEpisodeCount(t, memoryDBPath, segment.MemorySessionID, "assistant", 1)

	if err := bridge.FinalizeSegment(ctx, segment.SegmentID, "manual_debug", "summary text"); err != nil {
		t.Fatalf("FinalizeSegment: %v", err)
	}
	finalized, err := chatDB.GetMemorySegment(ctx, segment.SegmentID)
	if err != nil {
		t.Fatalf("GetMemorySegment(finalized): %v", err)
	}
	if finalized.FinalizedAt == "" || finalized.FinalizeReason != "manual_debug" || finalized.Summary != "summary text" {
		t.Fatalf("finalized segment = %#v, want finalized manual_debug summary", finalized)
	}
	requireMemorySessionEnded(t, memoryDBPath, segment.MemorySessionID, "summary text")
}

func TestBridgeRolloverFinalizesCurrentSegmentAndStartsNext(t *testing.T) {
	ctx := context.Background()
	logger := testMemoryLogger()
	chatDB := openBridgeChatDB(t, logger)
	if err := chatDB.CreateSession(ctx, "chat-rollover", "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	host, err := OpenWithOptions(ctx, memorycore.Options{
		DBPath:      filepath.Join(t.TempDir(), "memory.db"),
		PersonaID:   "default",
		AutoMigrate: true,
		EnableFTS:   true,
	}, logger)
	if err != nil {
		t.Fatalf("OpenWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = host.Close() })

	bridge := NewBridge(host, chatDB, logger, nil)
	first, err := bridge.EnsureSegment(ctx, "chat-rollover", "default")
	if err != nil {
		t.Fatalf("EnsureSegment(first): %v", err)
	}
	second, err := bridge.RolloverSegment(ctx, "chat-rollover", "default", "session_resume")
	if err != nil {
		t.Fatalf("RolloverSegment: %v", err)
	}
	if second.SegmentID == first.SegmentID || second.MemorySessionID == first.MemorySessionID {
		t.Fatalf("second segment = %#v, first = %#v; want new segment and memory session", second, first)
	}

	firstStored, err := chatDB.GetMemorySegment(ctx, first.SegmentID)
	if err != nil {
		t.Fatalf("GetMemorySegment(first): %v", err)
	}
	if firstStored.FinalizedAt == "" || firstStored.FinalizeReason != "session_resume" {
		t.Fatalf("first segment = %#v, want finalized session_resume", firstStored)
	}
	secondStored, err := chatDB.GetMemorySegment(ctx, second.SegmentID)
	if err != nil {
		t.Fatalf("GetMemorySegment(second): %v", err)
	}
	if secondStored.SegmentIndex != 2 || secondStored.FinalizedAt != "" {
		t.Fatalf("second segment = %#v, want active index 2", secondStored)
	}
	link, err := chatDB.GetMemoryChatLink(ctx, "chat-rollover")
	if err != nil {
		t.Fatalf("GetMemoryChatLink: %v", err)
	}
	if link == nil || link.CurrentMemorySessionID != second.MemorySessionID {
		t.Fatalf("link = %#v, want current %q", link, second.MemorySessionID)
	}
}

func TestBridgeRetrievePromptBlockReturnsSeededFact(t *testing.T) {
	ctx := context.Background()
	logger := testMemoryLogger()
	chatDB := openBridgeChatDB(t, logger)
	if err := chatDB.CreateSession(ctx, "chat-retrieve", "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	host, err := OpenWithOptions(ctx, memorycore.Options{
		DBPath:      filepath.Join(t.TempDir(), "memory.db"),
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

	bridge := NewBridge(host, chatDB, logger, nil)
	segment, err := bridge.EnsureSegment(ctx, "chat-retrieve", "default")
	if err != nil {
		t.Fatalf("EnsureSegment: %v", err)
	}
	episodeID, err := bridge.AppendUserEpisode(ctx, segment.SegmentID, "msg-user", "我喜欢咖啡。")
	if err != nil {
		t.Fatalf("AppendUserEpisode: %v", err)
	}
	user, err := host.Service.EnsureEntity(ctx, memorycore.EnsureEntityRequest{
		CanonicalName: "Long",
		EntityType:    memorycore.EntityTypeUser,
	})
	if err != nil {
		t.Fatalf("EnsureEntity: %v", err)
	}
	coffee := "咖啡"
	result, err := host.Service.ConsolidateCandidate(ctx, memorycore.ConsolidateCandidateRequest{
		SessionID: &segment.MemorySessionID,
		Trigger:   memorycore.ConsolidationTriggerManual,
		Candidate: memorycore.ManualFactCandidate{
			SubjectEntityID:  user.ID,
			Predicate:        "likes",
			ObjectLiteral:    &coffee,
			ContentSummary:   "用户喜欢咖啡。",
			SourceEpisodeIDs: []string{episodeID},
			Confidence:       memorycore.ConfidenceExplicit,
			Importance:       0.7,
			Sensitivity:      memorycore.SensitivityNormal,
		},
		Policy: memorycore.ConsolidationPolicy{
			Approved: true,
		},
	})
	if err != nil {
		t.Fatalf("ConsolidateCandidate: %v", err)
	}
	if result.Fact == nil {
		t.Fatalf("ConsolidateCandidate fact = nil: %#v", result)
	}

	block, err := bridge.RetrievePromptBlock(ctx, "chat-retrieve", "咖啡")
	if err != nil {
		t.Fatalf("RetrievePromptBlock: %v", err)
	}
	if !strings.Contains(block, "用户喜欢咖啡。") {
		t.Fatalf("prompt block = %q, want seeded fact summary", block)
	}
	for _, forbidden := range []string{
		result.Fact.ID,
		"QueryAnalysis",
		"query_analysis",
		"RetrievalConfidence",
		"retrieval_confidence",
		"Mirror",
	} {
		if strings.Contains(block, forbidden) {
			t.Fatalf("prompt block leaked %q: %s", forbidden, block)
		}
	}
}

func TestBridgeRetrievePromptBlockEmptyResultsReturnEmptyString(t *testing.T) {
	ctx := context.Background()
	logger := testMemoryLogger()
	chatDB := openBridgeChatDB(t, logger)
	if err := chatDB.CreateSession(ctx, "chat-empty", "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	host, err := OpenWithOptions(ctx, memorycore.Options{
		DBPath:      filepath.Join(t.TempDir(), "memory.db"),
		PersonaID:   "default",
		AutoMigrate: true,
		EnableFTS:   true,
	}, logger)
	if err != nil {
		t.Fatalf("OpenWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = host.Close() })

	bridge := NewBridge(host, chatDB, logger, nil)
	if _, err := bridge.EnsureSegment(ctx, "chat-empty", "default"); err != nil {
		t.Fatalf("EnsureSegment: %v", err)
	}
	block, err := bridge.RetrievePromptBlock(ctx, "chat-empty", "咖啡")
	if err != nil {
		t.Fatalf("RetrievePromptBlock: %v", err)
	}
	if block != "" {
		t.Fatalf("prompt block = %q, want empty", block)
	}
}

func TestBridgeRetrievePromptBlockWithoutCurrentSegmentReturnsEmptyString(t *testing.T) {
	ctx := context.Background()
	logger := testMemoryLogger()
	chatDB := openBridgeChatDB(t, logger)
	if err := chatDB.CreateSession(ctx, "chat-no-segment", "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	host, err := OpenWithOptions(ctx, memorycore.Options{
		DBPath:      filepath.Join(t.TempDir(), "memory.db"),
		PersonaID:   "default",
		AutoMigrate: true,
		EnableFTS:   true,
	}, logger)
	if err != nil {
		t.Fatalf("OpenWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = host.Close() })

	bridge := NewBridge(host, chatDB, logger, nil)
	block, err := bridge.RetrievePromptBlock(ctx, "chat-no-segment", "咖啡")
	if err != nil {
		t.Fatalf("RetrievePromptBlock: %v", err)
	}
	if block != "" {
		t.Fatalf("prompt block = %q, want empty", block)
	}
}

func TestBridgeManualPinCreatesTraceableRetrievableFact(t *testing.T) {
	fixture := openManualBridgeFixture(t, "chat-manual-like")

	episodeID, err := fixture.bridge.AppendUserEpisode(fixture.ctx, fixture.segment.SegmentID, "msg-user", "请记住我喜欢手冲咖啡")
	if err != nil {
		t.Fatalf("AppendUserEpisode: %v", err)
	}
	stored, err := fixture.chatDB.GetMemorySegment(fixture.ctx, fixture.segment.SegmentID)
	if err != nil {
		t.Fatalf("GetMemorySegment: %v", err)
	}
	if stored.LastUserEpisodeID != episodeID {
		t.Fatalf("LastUserEpisodeID = %q, want %q", stored.LastUserEpisodeID, episodeID)
	}

	fact := requireMemoryFact(t, fixture.memoryDBPath, "likes", "手冲咖啡")
	if fact.ContentSummary != "用户喜欢手冲咖啡。" {
		t.Fatalf("content summary = %q, want 用户喜欢手冲咖啡。", fact.ContentSummary)
	}
	if fact.FactType != memorycore.FactTypeStablePreference {
		t.Fatalf("fact type = %q, want %q", fact.FactType, memorycore.FactTypeStablePreference)
	}
	if fact.Pinned != 1 {
		t.Fatalf("pinned = %d, want 1", fact.Pinned)
	}
	requireMemoryFactSource(t, fixture.memoryDBPath, fact.ID, episodeID)

	block, err := fixture.bridge.RetrievePromptBlock(fixture.ctx, "chat-manual-like", "手冲咖啡")
	if err != nil {
		t.Fatalf("RetrievePromptBlock: %v", err)
	}
	if !strings.Contains(block, "用户喜欢手冲咖啡。") {
		t.Fatalf("prompt block = %q, want manual fact summary", block)
	}
}

func TestBridgeManualPinLikesReinforcesDuplicateAndCoexistsDistinct(t *testing.T) {
	fixture := openManualBridgeFixture(t, "chat-manual-repeat")

	for _, turn := range []struct {
		messageID string
		text      string
	}{
		{messageID: "msg-repeat-1", text: "请记住我喜欢咖啡"},
		{messageID: "msg-repeat-2", text: "请记住我喜欢咖啡"},
		{messageID: "msg-repeat-3", text: "请记住我喜欢乌龙茶"},
	} {
		if _, err := fixture.bridge.AppendUserEpisode(fixture.ctx, fixture.segment.SegmentID, turn.messageID, turn.text); err != nil {
			t.Fatalf("AppendUserEpisode(%q): %v", turn.text, err)
		}
	}

	coffee := requireMemoryFact(t, fixture.memoryDBPath, "likes", "咖啡")
	if coffee.ReinforcementCount != 1 {
		t.Fatalf("coffee reinforcement_count = %d, want 1", coffee.ReinforcementCount)
	}
	requireMemoryFact(t, fixture.memoryDBPath, "likes", "乌龙茶")
	requireMemoryFactCount(t, fixture.memoryDBPath, "likes", 2)
}

func TestBridgeManualPinPreferredNameSupersedesPriorName(t *testing.T) {
	fixture := openManualBridgeFixture(t, "chat-manual-name")

	if _, err := fixture.bridge.AppendUserEpisode(fixture.ctx, fixture.segment.SegmentID, "msg-name-1", "以后叫我 Long"); err != nil {
		t.Fatalf("AppendUserEpisode(first): %v", err)
	}
	if _, err := fixture.bridge.AppendUserEpisode(fixture.ctx, fixture.segment.SegmentID, "msg-name-2", "以后叫我 Yi"); err != nil {
		t.Fatalf("AppendUserEpisode(second): %v", err)
	}

	first := requireMemoryFact(t, fixture.memoryDBPath, "prefers_name", "Long")
	second := requireMemoryFact(t, fixture.memoryDBPath, "prefers_name", "Yi")
	if first.ValidityStatus != memorycore.ValidityInvalidated {
		t.Fatalf("first validity = %q, want invalidated", first.ValidityStatus)
	}
	if second.ValidityStatus != memorycore.ValidityValid {
		t.Fatalf("second validity = %q, want valid", second.ValidityStatus)
	}
}

func TestBridgeManualPinBoundaryUsesSensitiveDefault(t *testing.T) {
	fixture := openManualBridgeFixture(t, "chat-manual-boundary")

	if _, err := fixture.bridge.AppendUserEpisode(fixture.ctx, fixture.segment.SegmentID, "msg-boundary", "我不想再聊早会"); err != nil {
		t.Fatalf("AppendUserEpisode: %v", err)
	}

	fact := requireMemoryFact(t, fixture.memoryDBPath, "has_boundary", "早会")
	if fact.ContentSummary != "用户不想再聊早会。" {
		t.Fatalf("content summary = %q, want 用户不想再聊早会。", fact.ContentSummary)
	}
	if fact.FactType != memorycore.FactTypeRelationalState {
		t.Fatalf("fact type = %q, want relational_state", fact.FactType)
	}
	if fact.Sensitivity != memorycore.SensitivitySensitive {
		t.Fatalf("sensitivity = %q, want sensitive", fact.Sensitivity)
	}
}

func TestBridgeManualForgetPrefixesDoNotCreateFacts(t *testing.T) {
	fixture := openManualBridgeFixture(t, "chat-manual-forget")

	for _, turn := range []struct {
		messageID string
		text      string
	}{
		{messageID: "msg-forget-1", text: "忘记我喜欢手冲咖啡"},
		{messageID: "msg-forget-2", text: "别再提我喜欢手冲咖啡"},
		{messageID: "msg-forget-3", text: "删除我喜欢手冲咖啡"},
	} {
		if _, err := fixture.bridge.AppendUserEpisode(fixture.ctx, fixture.segment.SegmentID, turn.messageID, turn.text); err != nil {
			t.Fatalf("AppendUserEpisode(%q): %v", turn.text, err)
		}
	}

	requireMemoryFactCount(t, fixture.memoryDBPath, "", 0)
}

func TestFormatMemoryContextUsesOnlyBlocks(t *testing.T) {
	contextResult := &memorycore.MemoryContext{
		Blocks: []memorycore.MemoryBlock{
			{
				BlockType: memorycore.MemoryBlockTypeFacts,
				Items: []memorycore.MemoryContextItem{
					{
						NodeID:           "fact-diagnostic",
						Summary:          "用户喜欢咖啡。",
						Confidence:       0.99,
						UsageGuidance:    "用于偏好回应",
						HistoricalStatus: memorycore.MemoryHistoricalStatusCurrent,
						SourceRefs: []memorycore.MemorySourceRef{
							{EpisodeID: "episode-diagnostic"},
						},
					},
					{
						NodeID:  "fact-current-only",
						Summary: "用户刚说喜欢手冲咖啡。",
						SourceRefs: []memorycore.MemorySourceRef{
							{EpisodeID: "episode-current"},
						},
					},
					{
						NodeID:           "fact-historical",
						Summary:          "用户以前住在上海。",
						HistoricalStatus: memorycore.MemoryHistoricalStatusHistorical,
					},
					{
						NodeID:  "fact-mixed-source",
						Summary: "用户也喜欢红茶。",
						SourceRefs: []memorycore.MemorySourceRef{
							{EpisodeID: "episode-current"},
							{EpisodeID: "episode-old"},
						},
					},
				},
			},
			{
				BlockType: memorycore.MemoryBlockTypeSupportiveMemory,
			},
		},
		QueryAnalysis: &memorycore.QueryAnalysis{
			Raw: "diagnostic-query",
		},
		Mirror: &memorycore.MirrorRetrievalDiagnostics{
			Status: "diagnostic-mirror",
		},
		RetrievalConfidence: &memorycore.RetrievalConfidence{
			HardFailureReason: "diagnostic-confidence",
		},
	}

	block := FormatMemoryContext(contextResult, "episode-current")
	for _, want := range []string{
		"[长期记忆上下文]",
		"以下是允许用于当前回复的长期记忆。使用时要自然、克制；",
		"不要主动说明“我记得”，除非用户正在询问记忆或来源。",
		"- 用户喜欢咖啡。",
		"- 用户以前住在上海。",
		"- 用户也喜欢红茶。",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("prompt block = %q, want %q", block, want)
		}
	}
	for _, forbidden := range []string{
		"fact-diagnostic",
		"fact-historical",
		"fact-current-only",
		"0.99",
		"episode-diagnostic",
		"episode-current",
		"episode-old",
		"用于偏好回应",
		"用户刚说喜欢手冲咖啡。",
		"diagnostic-query",
		"diagnostic-mirror",
		"diagnostic-confidence",
		"[facts]",
		"[current]",
		"[historical]",
		"[supportive_memory]",
	} {
		if strings.Contains(block, forbidden) {
			t.Fatalf("prompt block leaked %q: %s", forbidden, block)
		}
	}
}

func writeMemoryCoreConfig(t *testing.T, dir string, enabled bool, autoMigrate bool, dbPath string) string {
	t.Helper()

	configPath := filepath.Join(dir, "memorycore.yaml")
	body := "schema_version: memorycore.config.v0.2\n" +
		"enabled: " + boolYAML(enabled) + "\n" +
		"core:\n" +
		"  db_path: " + filepath.ToSlash(dbPath) + "\n" +
		"  persona_id: default\n" +
		"  auto_migrate: " + boolYAML(autoMigrate) + "\n" +
		"  enable_fts: true\n" +
		"retrieval:\n" +
		"  use_fts: false\n" +
		"  use_mirror: false\n" +
		"  allow_historical: false\n" +
		"  allow_deep_archive: false\n" +
		"  sensitivity_permission: sensitive\n" +
		"  final_memory_count: 3\n" +
		"  context_budget_tokens: 321\n"
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile memorycore config: %v", err)
	}
	return configPath
}

func boolYAML(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func testMemoryLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func openBridgeChatDB(t *testing.T, logger *slog.Logger) *storage.DB {
	t.Helper()

	db, err := storage.Open(filepath.Join(t.TempDir(), "chat.db"), logger)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

type manualBridgeFixture struct {
	ctx          context.Context
	chatDB       *storage.DB
	host         *Host
	bridge       *Bridge
	segment      storage.MemorySegmentRef
	memoryDBPath string
}

func openManualBridgeFixture(t *testing.T, chatSessionID string) manualBridgeFixture {
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

	bridge := NewBridge(host, chatDB, logger, DefaultManualRules())
	segment, err := bridge.EnsureSegment(ctx, chatSessionID, "default")
	if err != nil {
		t.Fatalf("EnsureSegment: %v", err)
	}

	return manualBridgeFixture{
		ctx:          ctx,
		chatDB:       chatDB,
		host:         host,
		bridge:       bridge,
		segment:      segment,
		memoryDBPath: memoryDBPath,
	}
}

type memoryFactRow struct {
	ID                 string
	Predicate          string
	ObjectLiteral      string
	ContentSummary     string
	FactType           string
	Sensitivity        string
	ValidityStatus     string
	Pinned             int
	ReinforcementCount int
}

func requireMemoryFact(t *testing.T, dbPath string, predicate string, object string) memoryFactRow {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open(memory): %v", err)
	}
	defer db.Close()

	var fact memoryFactRow
	if err := db.QueryRow(`
		SELECT id, predicate, COALESCE(object_literal, ''), content_summary, fact_type,
		       sensitivity_level, validity_status, pinned, reinforcement_count
		FROM facts
		WHERE predicate = ? AND object_literal = ?
		ORDER BY ingested_at DESC, id DESC
		LIMIT 1
	`, predicate, object).Scan(
		&fact.ID,
		&fact.Predicate,
		&fact.ObjectLiteral,
		&fact.ContentSummary,
		&fact.FactType,
		&fact.Sensitivity,
		&fact.ValidityStatus,
		&fact.Pinned,
		&fact.ReinforcementCount,
	); err != nil {
		t.Fatalf("query memory fact predicate=%q object=%q: %v", predicate, object, err)
	}
	return fact
}

func requireMemoryFactCount(t *testing.T, dbPath string, predicate string, want int) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open(memory): %v", err)
	}
	defer db.Close()

	query := `SELECT COUNT(*) FROM facts`
	var got int
	if predicate == "" {
		if err := db.QueryRow(query).Scan(&got); err != nil {
			t.Fatalf("count memory facts: %v", err)
		}
	} else if err := db.QueryRow(query+` WHERE predicate = ?`, predicate).Scan(&got); err != nil {
		t.Fatalf("count memory facts: %v", err)
	}
	if got != want {
		t.Fatalf("memory fact count predicate=%q = %d, want %d", predicate, got, want)
	}
}

func requireMemoryFactSource(t *testing.T, dbPath string, factID string, episodeID string) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open(memory): %v", err)
	}
	defer db.Close()

	var got int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM memory_links
		WHERE from_node_type = 'fact'
		  AND from_node_id = ?
		  AND link_type = 'EVIDENCED_BY'
		  AND to_node_type = 'episode'
		  AND to_node_id = ?
	`, factID, episodeID).Scan(&got); err != nil {
		t.Fatalf("count memory fact source: %v", err)
	}
	if got != 1 {
		t.Fatalf("fact source link count = %d, want 1 for fact %q episode %q", got, factID, episodeID)
	}
}

func requireMemoryEpisodeCount(t *testing.T, dbPath string, sessionID string, role string, want int) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open(memory): %v", err)
	}
	defer db.Close()

	var got int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM episodes
		WHERE session_id = ? AND role = ?
	`, sessionID, role).Scan(&got); err != nil {
		t.Fatalf("count memory episodes: %v", err)
	}
	if got != want {
		t.Fatalf("episode count for role %s = %d, want %d", role, got, want)
	}
}

func requireMemorySessionEnded(t *testing.T, dbPath string, sessionID string, wantSummary string) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open(memory): %v", err)
	}
	defer db.Close()

	var endedAt string
	var summary string
	if err := db.QueryRow(`
		SELECT COALESCE(ended_at, ''), COALESCE(summary, '')
		FROM sessions
		WHERE id = ?
	`, sessionID).Scan(&endedAt, &summary); err != nil {
		t.Fatalf("read memory session: %v", err)
	}
	if endedAt == "" || summary != wantSummary {
		t.Fatalf("memory session ended_at/summary = %q/%q, want ended and %q", endedAt, summary, wantSummary)
	}
}
