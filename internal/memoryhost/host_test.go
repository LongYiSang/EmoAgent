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

	memconfig "github.com/longyisang/emoagent-memorycore/config"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"github.com/longyisang/emoagent/internal/config"
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

func TestOpenFromConfigWithOptionsUsesProviderRegistry(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "memory.db")
	configPath := writeMemoryCoreConfigWithPipeline(t, dir, dbPath, "moonshot")

	host, err := OpenFromConfigWithOptions(context.Background(), OpenConfigOptions{
		ConfigPath: configPath,
		ProviderRegistry: BuildProviderRegistry([]config.LLMProvider{{
			ID:        "moonshot",
			Name:      "Moonshot",
			Protocol:  "openai_compatible",
			BaseURL:   "https://api.moonshot.cn/v1",
			APIKeyEnv: "MOONSHOT_API_KEY",
			Enabled:   true,
		}}),
		Logger: testMemoryLogger(),
	})
	if err != nil {
		t.Fatalf("OpenFromConfigWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = host.Close() })

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("memory db was not created: %v", err)
	}
}

func TestOpenFromConfigWithOptionsAppliesNaturalMemoryRuntimeOverrides(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "memory.db")
	configPath := writeMemoryCoreConfig(t, dir, true, true, dbPath)

	host, err := OpenFromConfigWithOptions(context.Background(), OpenConfigOptions{
		ConfigPath: configPath,
		NaturalMemory: NaturalMemoryCoreOverrides{
			Configured:              true,
			Enabled:                 true,
			LocalTime:               "04:20",
			Timezone:                "UTC",
			RunMissedOnStart:        false,
			ManualEnabled:           true,
			AllowDryRun:             true,
			AllowForce:              false,
			MarkSleepCycleByDefault: true,
		},
		Logger: testMemoryLogger(),
	})
	if err != nil {
		t.Fatalf("OpenFromConfigWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = host.Close() })

	result, err := host.Core.RunNaturalMemoryTick(context.Background(), memorycore.RunNaturalMemoryTickRequest{
		PersonaID: "default",
		Now:       time.Date(2026, 6, 6, 3, 45, 0, 0, time.UTC),
		Explain:   true,
	})
	if err != nil {
		t.Fatalf("RunNaturalMemoryTick: %v", err)
	}
	if result.Status != memorycore.NaturalMemoryRunStatusSkipped || !naturalExplainHasReason(result.Explain, "sleep cycle local_time not reached") {
		t.Fatalf("natural tick result = %#v, want skipped by overlaid local_time", result)
	}

	_, err = host.Core.RunNaturalMemoryCycle(context.Background(), memorycore.RunNaturalMemoryCycleRequest{
		PersonaID: "default",
		RunKind:   memorycore.NaturalMemoryRunManual,
		Force:     true,
	})
	if err == nil || !strings.Contains(err.Error(), "allow_force is false") {
		t.Fatalf("RunNaturalMemoryCycle force error = %v, want allow_force false", err)
	}
}

func naturalExplainHasReason(items []memorycore.NaturalMemoryExplainItem, reason string) bool {
	for _, item := range items {
		if strings.Contains(item.SafeReasonSummary, reason) {
			return true
		}
		for _, code := range item.ReasonCodes {
			if strings.Contains(code, reason) {
				return true
			}
		}
	}
	return false
}

func TestProjectMemoryCoreConfigLoadsWithInjectedProviders(t *testing.T) {
	path := filepath.Join("..", "..", "config", "memorycore.yaml")
	cfg, err := memconfig.LoadEffective(memconfig.LoadEffectiveOptions{
		ConfigPath: path,
		ProviderRegistry: BuildProviderRegistry([]config.LLMProvider{
			{
				ID:        "deepseek",
				Name:      "DeepSeek",
				Protocol:  "openai_compatible",
				BaseURL:   "https://api.deepseek.com",
				APIKeyEnv: "DEEPSEEK_API_KEY",
				Enabled:   true,
			},
			{
				ID:        "dashscope_embedding",
				Name:      "DashScope Embedding",
				Protocol:  "openai_compatible",
				BaseURL:   "https://dashscope.aliyuncs.com/compatible-mode/v1",
				APIKeyEnv: "DASHSCOPE_API_KEY",
				Enabled:   true,
			},
			{
				ID:        "dashscope_rerank",
				Name:      "DashScope Rerank",
				Protocol:  "dashscope_vl",
				BaseURL:   "https://dashscope.aliyuncs.com/api/v1/services/rerank/text-rerank/text-rerank",
				APIKeyEnv: "DASHSCOPE_API_KEY",
				Enabled:   true,
			},
		}),
	})
	if err != nil {
		t.Fatalf("LoadEffective(%s): %v", path, err)
	}
	if !cfg.NaturalMemory.Enabled || !cfg.NaturalMemory.SleepCycle.Enabled || !cfg.NaturalMemory.ManualTrigger.Enabled {
		t.Fatalf("NaturalMemory = %#v, want enabled sleep/manual", cfg.NaturalMemory)
	}
	if cfg.NaturalMemory.SleepCycle.LocalTime != "03:30" {
		t.Fatalf("natural memory local time = %q, want 03:30", cfg.NaturalMemory.SleepCycle.LocalTime)
	}
	if cfg.NaturalMemory.Scoring.Model != "power_law_with_reactivation" || cfg.NaturalMemory.Scoring.DefaultDecayExponent != 0.6 || cfg.NaturalMemory.Scoring.ReactivationThreshold != 0.55 {
		t.Fatalf("natural memory scoring = %#v, want explicit scoring defaults", cfg.NaturalMemory.Scoring)
	}
	if cfg.NaturalMemory.FactDefaults["transient_context"].TauDays != 7 || cfg.NaturalMemory.FactDefaults["transient_context"].Alpha != 0.90 {
		t.Fatalf("transient_context defaults = %#v, want explicit tau/alpha", cfg.NaturalMemory.FactDefaults["transient_context"])
	}
	if !cfg.NaturalMemory.Protection.ProtectPinned || cfg.NaturalMemory.Protection.ProtectedMinTier != "warm" {
		t.Fatalf("natural memory protection = %#v, want explicit protection defaults", cfg.NaturalMemory.Protection)
	}
	if !cfg.NaturalMemory.Compression.Enabled || cfg.NaturalMemory.Compression.RequireMinConfidence != 0.70 || cfg.NaturalMemory.Compression.MaxCandidatesPerRun != 20 {
		t.Fatalf("natural memory compression = %#v, want explicit compression defaults", cfg.NaturalMemory.Compression)
	}
	if cfg.NaturalMemory.Limits.MaxCandidatesPerRun != 5000 || cfg.NaturalMemory.Limits.MaxWritesPerRun != 1000 || cfg.NaturalMemory.Limits.BatchSize != 200 {
		t.Fatalf("natural memory limits = %#v, want explicit limits", cfg.NaturalMemory.Limits)
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
	user, err := host.Core.EnsureEntity(ctx, memorycore.EnsureEntityRequest{
		CanonicalName: "Long",
		EntityType:    memorycore.EntityTypeUser,
	})
	if err != nil {
		t.Fatalf("EnsureEntity: %v", err)
	}
	coffee := "咖啡"
	result, err := host.Core.ConsolidateCandidate(ctx, memorycore.ConsolidateCandidateRequest{
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
	if !strings.Contains(block, "[长期记忆上下文：使用约束]") {
		t.Fatalf("prompt block = %q, want prompt usage constraint title", block)
	}
	if strings.Contains(block, "[长期记忆上下文]\n") {
		t.Fatalf("prompt block = %q, want new prompt titles", block)
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
	drainExtractionWorker(t, fixture.ctx, fixture.host, fixture.chatDB, 1)

	fact := requireMemoryFact(t, fixture.memoryDBPath, "likes", "手冲咖啡")
	if fact.ContentSummary != "用户喜欢手冲咖啡。" {
		t.Fatalf("content summary = %q, want 用户喜欢手冲咖啡。", fact.ContentSummary)
	}
	if fact.FactType != memorycore.FactTypeStablePreference {
		t.Fatalf("fact type = %q, want %q", fact.FactType, memorycore.FactTypeStablePreference)
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

func TestBridgeManualPinUsesExtractionGateForDuplicateInput(t *testing.T) {
	fixture := openManualBridgeFixture(t, "chat-manual-repeat")

	for _, turn := range []struct {
		messageID string
		text      string
	}{
		{messageID: "msg-repeat-1", text: "请记住我喜欢咖啡"},
		{messageID: "msg-repeat-2", text: "请记住我喜欢咖啡"},
	} {
		if _, err := fixture.bridge.AppendUserEpisode(fixture.ctx, fixture.segment.SegmentID, turn.messageID, turn.text); err != nil {
			t.Fatalf("AppendUserEpisode(%q): %v", turn.text, err)
		}
		drainExtractionWorker(t, fixture.ctx, fixture.host, fixture.chatDB, 1)
	}

	requireMemoryFact(t, fixture.memoryDBPath, "likes", "手冲咖啡")
	requireMemoryFactCount(t, fixture.memoryDBPath, "likes", 1)
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

func TestFormatMemoryContextForPromptSectionsGuidanceAndSafety(t *testing.T) {
	contextResult := &memorycore.MemoryContext{
		Blocks: []memorycore.MemoryBlock{
			{
				BlockType: memorycore.MemoryBlockTypeFacts,
				Items: []memorycore.MemoryContextItem{
					{
						NodeID:        "fact-user-name",
						Summary:       "用户名叫 Long。",
						UsageGuidance: "用于称呼回应",
						SourceRefs: []memorycore.MemorySourceRef{
							{EpisodeID: "episode-safe"},
						},
					},
				},
			},
			{
				BlockType: memorycore.MemoryBlockTypeRelationshipArcMemory,
				Items: []memorycore.MemoryContextItem{
					{
						NodeID:           "arc-trust",
						Summary:          "用户偏好直接指出风险边界。",
						UsageGuidance:    "用于称呼回应",
						DoNotOverstate:   true,
						HistoricalStatus: memorycore.MemoryHistoricalStatusSuperseded,
					},
				},
			},
			{
				BlockType: memorycore.MemoryBlockTypeRelevantCausalMemory,
				Items: []memorycore.MemoryContextItem{
					{
						NodeID:           "causal-deadline",
						Summary:          "用户赶在周五前完成长期记忆提示词改造。",
						HistoricalStatus: memorycore.MemoryHistoricalStatusHistorical,
					},
				},
			},
			{
				BlockType: memorycore.MemoryBlockTypeSupportiveMemory,
				Items: []memorycore.MemoryContextItem{
					{
						NodeID:        "supportive-style",
						Summary:       "用户喜欢先看失败测试再实现。",
						UsageGuidance: "用于工作流回应",
					},
				},
			},
			{
				BlockType: memorycore.MemoryBlockTypeExperienceContext,
				Items: []memorycore.MemoryContextItem{
					{NodeID: "experience-review", Summary: "用户上次要求最终报告列出 concerns。"},
				},
			},
			{
				BlockType: "future_block_type",
				Items: []memorycore.MemoryContextItem{
					{NodeID: "unknown-safe", Summary: "未知分区条目应保守放入当前相关记忆。"},
				},
			},
			{
				BlockType: memorycore.MemoryBlockTypeHistoricalTransitionMemory,
				Items: []memorycore.MemoryContextItem{
					{NodeID: "transition-old-city", Summary: "用户以前住在上海。"},
				},
			},
			{
				BlockType: memorycore.MemoryBlockTypeProvenanceMemory,
				Items: []memorycore.MemoryContextItem{
					{NodeID: "provenance-source", Summary: "这个偏好来自用户主动说明。"},
				},
			},
			{
				BlockType: memorycore.MemoryBlockTypePremiseCheckMemory,
				Items: []memorycore.MemoryContextItem{
					{NodeID: "premise-counter", Summary: "用户不是每次都要求完整重构。"},
				},
			},
			{
				BlockType: memorycore.MemoryBlockTypeFacts,
				Items: []memorycore.MemoryContextItem{
					{
						NodeID:  "excluded-current-only",
						Summary: "这条只来自当前回合，应被过滤。",
						SourceRefs: []memorycore.MemorySourceRef{
							{EpisodeID: "episode-current"},
						},
					},
					{
						NodeID:  "mixed-source",
						Summary: "混合来源记忆应保留。",
						SourceRefs: []memorycore.MemorySourceRef{
							{EpisodeID: "episode-current"},
							{EpisodeID: "episode-safe"},
						},
					},
				},
			},
		},
		DoNotMention: []memorycore.MemorySuppression{
			{NodeType: "fact", NodeID: "supportive-style", Reason: "fatigue"},
			{NodeType: "fact", NodeID: "missing-summary-id", Reason: "context_budget"},
		},
		QueryAnalysis: &memorycore.QueryAnalysis{Raw: "diagnostic-query"},
		Mirror:        &memorycore.MirrorRetrievalDiagnostics{Status: "diagnostic-mirror"},
		RetrievalConfidence: &memorycore.RetrievalConfidence{
			Overall:           0.42,
			HardFailureReason: "diagnostic-confidence",
		},
	}

	block := FormatMemoryContextForPrompt(contextResult, "episode-current")
	for _, want := range []string{
		"[长期记忆上下文：使用约束]",
		"- 不要主动说明“我记得”，除非用户询问来源。",
		"- 历史事实不能当当前事实说。",
		"- 低置信度记忆只可柔和使用。",
		"- 用于称呼回应",
		"- 用于工作流回应",
		"[核心身份与边界]",
		"- 用户名叫 Long。 (用于称呼回应)",
		"- 用户偏好直接指出风险边界。 [superseded] (用于称呼回应；不要夸大)",
		"- 混合来源记忆应保留。",
		"[当前相关记忆]",
		"- 用户赶在周五前完成长期记忆提示词改造。 [historical]",
		"- 用户上次要求最终报告列出 concerns。",
		"- 未知分区条目应保守放入当前相关记忆。",
		"[因果/历史上下文]",
		"- 用户以前住在上海。",
		"- 这个偏好来自用户主动说明。",
		"- 用户不是每次都要求完整重构。",
		"[不要主动提及]",
		"- 用户喜欢先看失败测试再实现。 (近期已多次使用，避免主动提及)",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("prompt block = %q, want %q", block, want)
		}
	}
	if strings.Count(block, "用户喜欢先看失败测试再实现。") != 1 {
		t.Fatalf("prompt block = %q, want suppressed item only in do-not-mention section", block)
	}
	if strings.Count(block, "- 用于称呼回应") != 1 {
		t.Fatalf("prompt block = %q, want deduplicated usage guidance", block)
	}
	for _, forbidden := range []string{
		"fact-user-name",
		"arc-trust",
		"causal-deadline",
		"supportive-style",
		"missing-summary-id",
		"future_block_type",
		"excluded-current-only",
		"这条只来自当前回合，应被过滤。",
		"episode-current",
		"episode-safe",
		"EpisodeID",
		"NodeID",
		"GraphActivation",
		"QueryAnalysis",
		"RetrievalConfidence",
		"Mirror",
		"diagnostic-query",
		"diagnostic-mirror",
		"diagnostic-confidence",
		"0.42",
		"confidence",
		"fatigue",
		"context_budget",
		"mmr_duplicate",
		"[facts]",
		"[relationship_arc_memory]",
		"[relevant_causal_memory]",
		"[historical_transition_memory]",
		"[provenance_memory]",
		"[premise_check_memory]",
		"[supportive_memory]",
		"[experience_context]",
	} {
		if strings.Contains(block, forbidden) {
			t.Fatalf("prompt block leaked %q: %s", forbidden, block)
		}
	}
}

func TestFormatMemoryContextForPromptEmptyReturnsEmptyString(t *testing.T) {
	if got := FormatMemoryContextForPrompt(nil); got != "" {
		t.Fatalf("FormatMemoryContextForPrompt(nil) = %q, want empty", got)
	}
	if got := FormatMemoryContextForPrompt(&memorycore.MemoryContext{}); got != "" {
		t.Fatalf("FormatMemoryContextForPrompt(empty) = %q, want empty", got)
	}
	got := FormatMemoryContextForPrompt(&memorycore.MemoryContext{
		Blocks: []memorycore.MemoryBlock{
			{
				BlockType: memorycore.MemoryBlockTypeFacts,
				Items: []memorycore.MemoryContextItem{
					{Summary: "   "},
					{
						Summary: "只来自被排除来源。",
						SourceRefs: []memorycore.MemorySourceRef{
							{EpisodeID: "episode-current"},
						},
					},
				},
			},
		},
	}, "episode-current")
	if got != "" {
		t.Fatalf("FormatMemoryContextForPrompt(no valid items) = %q, want empty", got)
	}
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

func writeMemoryCoreConfigWithPipeline(t *testing.T, dir string, dbPath string, providerID string) string {
	t.Helper()

	configPath := filepath.Join(dir, "memorycore.yaml")
	body := "schema_version: memorycore.config.v0.2\n" +
		"enabled: true\n" +
		"core:\n" +
		"  db_path: " + filepath.ToSlash(dbPath) + "\n" +
		"  persona_id: default\n" +
		"  auto_migrate: true\n" +
		"  enable_fts: true\n" +
		"pipelines:\n" +
		"  extraction:\n" +
		"    enabled: true\n" +
		"    provider_id: " + providerID + "\n" +
		"    model: memory-model\n"
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
		Extraction: memorycore.ExtractionOptions{
			Enabled: true,
			Provider: memorycore.ExtractionProviderOptions{
				Kind: memorycore.ExtractionProviderMock,
				ID:   memorycore.ExtractionProviderMock,
			},
			Defaults: memorycore.ExtractionDefaults{
				Configured:         true,
				Mode:               memorycore.ExtractionRunModeApply,
				Timezone:           "Asia/Shanghai",
				AllowInference:     true,
				MaxFacts:           12,
				MaxLinks:           20,
				ApplyAcceptedFacts: true,
			},
			Runtime: memorycore.ExtractionRuntimeOptions{
				Configured:    true,
				RepairEnabled: true,
			},
			Audit: memorycore.ExtractionAuditOptions{
				Configured: true,
				Enabled:    true,
			},
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
