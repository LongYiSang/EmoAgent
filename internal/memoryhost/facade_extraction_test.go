package memoryhost

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"github.com/longyisang/emoagent/internal/storage"
)

func TestHostExtractSessionEndIsAsyncOnly(t *testing.T) {
	fake := &fakeMemoryService{}
	host := &Host{
		Service: fake,
		extractionPolicy: ExtractionHostPolicy{
			Enabled:                  true,
			TriggerOnFinalizeSegment: true,
			SessionEndMode:           memorycore.ExtractionRunModeApply,
			Timezone:                 "Asia/Shanghai",
			Limit:                    7,
			SemanticDedup: memorycore.SemanticDedupOptions{
				Enabled:        true,
				Shadow:         true,
				CandidateLimit: 5,
			},
		},
	}

	sessionID := "memory-session-1"
	result, err := host.ExtractSessionEnd(context.Background(), "persona-1", sessionID)
	if err == nil || !strings.Contains(err.Error(), "async_extraction_required") {
		t.Fatalf("ExtractSessionEnd error = %v, want async_extraction_required", err)
	}
	if result != nil {
		t.Fatalf("result = %#v, want nil", result)
	}
	if len(fake.runExtractionCalls) != 0 {
		t.Fatalf("RunExtraction calls = %d, want 0", len(fake.runExtractionCalls))
	}
}

func TestHostExtractSessionEndDisabledDoesNotCallRunExtraction(t *testing.T) {
	fake := &fakeMemoryService{}
	host := &Host{
		Service: fake,
		extractionPolicy: ExtractionHostPolicy{
			Enabled:                  false,
			TriggerOnFinalizeSegment: true,
		},
	}

	result, err := host.ExtractSessionEnd(context.Background(), "persona-1", "memory-session-1")
	if err != nil {
		t.Fatalf("ExtractSessionEnd disabled error = %v", err)
	}
	if result != nil {
		t.Fatalf("ExtractSessionEnd disabled result = %#v, want nil", result)
	}
	if len(fake.runExtractionCalls) != 0 {
		t.Fatalf("RunExtraction calls = %d, want 0", len(fake.runExtractionCalls))
	}
}

func TestHostConfigureExtractionPolicyPreservesMemoryCoreSemanticDedup(t *testing.T) {
	host := &Host{
		Service: &fakeMemoryService{},
		extractionPolicy: ExtractionHostPolicy{
			Enabled: true,
			SemanticDedup: memorycore.SemanticDedupOptions{
				Enabled:        true,
				Shadow:         true,
				CandidateLimit: 8,
			},
		},
	}

	host.ConfigureExtractionPolicy(ExtractionHostPolicy{
		Enabled:                  true,
		TriggerOnFinalizeSegment: true,
	})

	if !host.extractionPolicy.SemanticDedup.Enabled || !host.extractionPolicy.SemanticDedup.Shadow || host.extractionPolicy.SemanticDedup.CandidateLimit != 8 {
		t.Fatalf("semantic dedup = %#v, want preserved MemoryCore policy", host.extractionPolicy.SemanticDedup)
	}
}

func TestBridgeFinalizeSegmentQueuesExtractionAndDoesNotRunSynchronously(t *testing.T) {
	fixture := openFacadeBridgeFixture(t, "chat-finalize", &fakeMemoryService{
		runExtractionResult: &memorycore.ExtractionRunResult{
			Status:             memorycore.ExtractionRunStatusFailed,
			SanitizedErrorCode: "provider_failed",
		},
		runExtractionErr: errors.New("raw provider failed with user text 我喜欢手冲咖啡"),
	})

	if _, err := fixture.bridge.AppendUserEpisode(fixture.ctx, fixture.segment.SegmentID, "msg-user", "今天喝了咖啡"); err != nil {
		t.Fatalf("AppendUserEpisode: %v", err)
	}
	if err := fixture.bridge.FinalizeSegment(fixture.ctx, fixture.segment.SegmentID, "session_end", "summary"); err != nil {
		t.Fatalf("FinalizeSegment: %v", err)
	}
	if fixture.service.endSessionCalls != 1 {
		t.Fatalf("EndSession calls = %d, want 1", fixture.service.endSessionCalls)
	}
	if len(fixture.service.runExtractionCalls) != 0 {
		t.Fatalf("RunExtraction calls = %d, want 0", len(fixture.service.runExtractionCalls))
	}
	jobs, err := fixture.db.ListMemoryExtractionJobs(fixture.ctx, storage.ListMemoryExtractionJobsFilter{SegmentID: fixture.segment.SegmentID, Limit: 10})
	if err != nil {
		t.Fatalf("ListMemoryExtractionJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs = %#v, want one queued job", jobs)
	}
	job := jobs[0]
	if job.Trigger != storage.MemoryExtractionTriggerSessionEnd || job.Status != storage.MemoryExtractionJobStatusPending {
		t.Fatalf("queued job = %#v, want pending session_end", job)
	}
	if job.Mode != string(memorycore.ExtractionRunModeApply) || job.MemorySessionID != fixture.segment.MemorySessionID {
		t.Fatalf("queued job = %#v, want apply mode and memory session", job)
	}
}

func TestBridgeFinalizeEmptySegmentDoesNotQueueExtraction(t *testing.T) {
	fixture := openFacadeBridgeFixture(t, "chat-finalize-empty", &fakeMemoryService{})

	if err := fixture.bridge.FinalizeSegment(fixture.ctx, fixture.segment.SegmentID, "session_end", "summary"); err != nil {
		t.Fatalf("FinalizeSegment: %v", err)
	}
	if fixture.service.endSessionCalls != 1 {
		t.Fatalf("EndSession calls = %d, want 1", fixture.service.endSessionCalls)
	}
	if len(fixture.service.runExtractionCalls) != 0 {
		t.Fatalf("RunExtraction calls = %d, want 0", len(fixture.service.runExtractionCalls))
	}
	jobs, err := fixture.db.ListMemoryExtractionJobs(fixture.ctx, storage.ListMemoryExtractionJobsFilter{SegmentID: fixture.segment.SegmentID, Limit: 10})
	if err != nil {
		t.Fatalf("ListMemoryExtractionJobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("jobs = %#v, want none for empty finalized segment", jobs)
	}
}

func TestBridgeFinalizeSegmentDoesNotBlockOnSlowExtraction(t *testing.T) {
	blockExtraction := make(chan struct{})
	fixture := openFacadeBridgeFixture(t, "chat-finalize-slow", &fakeMemoryService{
		runExtractionBlock: blockExtraction,
	})

	done := make(chan error, 1)
	go func() {
		done <- fixture.bridge.FinalizeSegment(fixture.ctx, fixture.segment.SegmentID, "session_end", "summary")
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("FinalizeSegment: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		close(blockExtraction)
		t.Fatal("FinalizeSegment blocked on RunExtraction")
	}
	if len(fixture.service.runExtractionCalls) != 0 {
		t.Fatalf("RunExtraction calls = %d, want 0", len(fixture.service.runExtractionCalls))
	}
}

func TestManualPinQueuesExtractionAndAppendDoesNotRunSynchronously(t *testing.T) {
	fixture := openFacadeBridgeFixture(t, "chat-manual-pin", &fakeMemoryService{
		runExtractionResult: &memorycore.ExtractionRunResult{
			Status:             memorycore.ExtractionRunStatusFailed,
			SanitizedErrorCode: "provider_failed",
		},
		runExtractionErr: errors.New("raw provider failed with user text 我喜欢手冲咖啡"),
	})

	episodeID, err := fixture.bridge.AppendUserEpisode(fixture.ctx, fixture.segment.SegmentID, "msg-user", "请记住我喜欢手冲咖啡")
	if err != nil {
		t.Fatalf("AppendUserEpisode: %v", err)
	}
	if episodeID == "" {
		t.Fatal("AppendUserEpisode episode id is empty")
	}
	if len(fixture.service.runExtractionCalls) != 0 {
		t.Fatalf("RunExtraction calls = %d, want 0", len(fixture.service.runExtractionCalls))
	}
	jobs, err := fixture.db.ListMemoryExtractionJobs(fixture.ctx, storage.ListMemoryExtractionJobsFilter{SegmentID: fixture.segment.SegmentID, Limit: 10})
	if err != nil {
		t.Fatalf("ListMemoryExtractionJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs = %#v, want one queued job", jobs)
	}
	job := jobs[0]
	if job.Trigger != storage.MemoryExtractionTriggerManualPin || job.Priority != 10 {
		t.Fatalf("queued job = %#v, want high-priority manual_pin", job)
	}
	if len(job.EpisodeIDs) != 1 || job.EpisodeIDs[0] != episodeID {
		t.Fatalf("job episode ids = %#v, want %q", job.EpisodeIDs, episodeID)
	}
	if job.Mode != string(memorycore.ExtractionRunModeApply) {
		t.Fatalf("job mode = %q, want apply", job.Mode)
	}
	if fixture.service.consolidateCalls != 0 {
		t.Fatalf("ConsolidateCandidate calls = %d, want 0", fixture.service.consolidateCalls)
	}
}

func TestManualForgetPreviewQueuesConfirmationNoticeWithoutExecute(t *testing.T) {
	fixture := openFacadeBridgeFixture(t, "chat-manual-forget-preview", &fakeMemoryService{
		previewForgetResult: &memorycore.ForgetPreviewResult{
			PersonaID:            "default",
			RequestID:            "manual-forget-1",
			PreviewHash:          "hash-1",
			RequestedLevel:       memorycore.ForgetLevelSoft,
			ScopeMode:            memorycore.ForgetScopeSemanticQuery,
			RequiresConfirmation: true,
			Targets: []memorycore.ForgetResolvedTarget{
				{
					NodeType:    memorycore.ForgetNodeFact,
					NodeID:      "fact-coffee",
					SafeSummary: "用户喜欢手冲咖啡。",
				},
			},
		},
	})

	if _, err := fixture.bridge.AppendUserEpisode(fixture.ctx, fixture.segment.SegmentID, "msg-forget", "忘记我喜欢手冲咖啡"); err != nil {
		t.Fatalf("AppendUserEpisode: %v", err)
	}
	if len(fixture.service.previewForgetCalls) != 1 {
		t.Fatalf("PreviewForget calls = %d, want 1", len(fixture.service.previewForgetCalls))
	}
	req := fixture.service.previewForgetCalls[0]
	if req.ScopeMode != memorycore.ForgetScopeSemanticQuery || req.SemanticQuery == nil || *req.SemanticQuery != "我喜欢手冲咖啡" {
		t.Fatalf("PreviewForget request = %#v", req)
	}
	if len(fixture.service.executeForgetCalls) != 0 {
		t.Fatalf("ExecuteForget calls = %d, want 0", len(fixture.service.executeForgetCalls))
	}
	notice, ok := fixture.bridge.TakeManualMemoryNotice("chat-manual-forget-preview")
	if !ok {
		t.Fatal("manual memory notice missing")
	}
	for _, snippet := range []string{
		"我准备执行一次长期记忆删除，尚未执行。",
		"候选：",
		"用户喜欢手冲咖啡。",
		"影响：确认后只会删除上面列出的 exact-node 目标。",
		"确认删除请回复“确认删除”；取消请回复“取消”。",
	} {
		if !strings.Contains(notice, snippet) {
			t.Fatalf("notice = %q, missing %q", notice, snippet)
		}
	}
	if !strings.Contains(notice, "用户喜欢手冲咖啡。") || !strings.Contains(notice, "确认") {
		t.Fatalf("notice = %q, want safe candidate summary and confirmation prompt", notice)
	}
}

func TestManualForgetConfirmationExecutesExactTargetsWithPreviewHash(t *testing.T) {
	fixture := openFacadeBridgeFixture(t, "chat-manual-forget-confirm", &fakeMemoryService{
		previewForgetResult: &memorycore.ForgetPreviewResult{
			PersonaID:            "default",
			RequestID:            "manual-forget-1",
			PreviewHash:          "hash-1",
			RequestedLevel:       memorycore.ForgetLevelSoft,
			ScopeMode:            memorycore.ForgetScopeSemanticQuery,
			RequiresConfirmation: true,
			Targets: []memorycore.ForgetResolvedTarget{
				{
					NodeType:    memorycore.ForgetNodeFact,
					NodeID:      "fact-coffee",
					SafeSummary: "用户喜欢手冲咖啡。",
				},
			},
		},
		executeForgetResult: &memorycore.ForgetExecuteResult{
			PersonaID:   "default",
			Executed:    1,
			PreviewHash: "hash-1",
		},
	})

	if _, err := fixture.bridge.AppendUserEpisode(fixture.ctx, fixture.segment.SegmentID, "msg-forget", "忘记我喜欢手冲咖啡"); err != nil {
		t.Fatalf("AppendUserEpisode(forget): %v", err)
	}
	_, _ = fixture.bridge.TakeManualMemoryNotice("chat-manual-forget-confirm")
	if _, err := fixture.bridge.AppendUserEpisode(fixture.ctx, fixture.segment.SegmentID, "msg-confirm", "确认删除"); err != nil {
		t.Fatalf("AppendUserEpisode(confirm): %v", err)
	}
	if len(fixture.service.executeForgetCalls) != 1 {
		t.Fatalf("ExecuteForget calls = %d, want 1", len(fixture.service.executeForgetCalls))
	}
	req := fixture.service.executeForgetCalls[0]
	if req.PreviewHash != "hash-1" || req.Level != memorycore.ForgetLevelSoft || !req.Confirmed {
		t.Fatalf("ExecuteForget request = %#v", req)
	}
	if len(req.ConfirmedTargets) != 1 || req.ConfirmedTargets[0] != (memorycore.ExactNodeRef{NodeType: memorycore.ForgetNodeFact, NodeID: "fact-coffee"}) {
		t.Fatalf("confirmed targets = %#v", req.ConfirmedTargets)
	}
	notice, ok := fixture.bridge.TakeManualMemoryNotice("chat-manual-forget-confirm")
	if !ok || !strings.Contains(notice, "已执行长期记忆删除：1 条。") {
		t.Fatalf("notice = %q ok=%v, want executed notice", notice, ok)
	}
}

func TestExtractionWarningsDoNotLogRawProviderText(t *testing.T) {
	var sink strings.Builder
	logger := slog.New(slog.NewTextHandler(&sink, nil))
	fixture := openFacadeBridgeFixtureWithLogger(t, "chat-log-sanitize", logger, &fakeMemoryService{
		runExtractionResult: &memorycore.ExtractionRunResult{
			Status:             memorycore.ExtractionRunStatusFailed,
			SanitizedErrorCode: "provider_failed",
		},
		runExtractionErr: errors.New("raw provider failed with user text 我喜欢手冲咖啡 target_description provider_raw_response"),
	})

	if _, err := fixture.bridge.AppendUserEpisode(fixture.ctx, fixture.segment.SegmentID, "msg-user", "请记住我喜欢手冲咖啡"); err != nil {
		t.Fatalf("AppendUserEpisode: %v", err)
	}
	logs := sink.String()
	for _, forbidden := range []string{"手冲咖啡", "target_description", "provider_raw_response", "raw provider failed"} {
		if strings.Contains(logs, forbidden) {
			t.Fatalf("log leaked %q: %s", forbidden, logs)
		}
	}
	if !strings.Contains(logs, "manual memory pin extraction queued") {
		t.Fatalf("log = %s, want queued extraction log", logs)
	}
}

type facadeBridgeFixture struct {
	ctx     context.Context
	service *fakeMemoryService
	bridge  *Bridge
	db      *storage.DB
	segment storage.MemorySegmentRef
}

func (f facadeBridgeFixture) serviceHost() *Host {
	if f.bridge == nil {
		return nil
	}
	return f.bridge.host
}

func openFacadeBridgeFixture(t *testing.T, chatSessionID string, service *fakeMemoryService) facadeBridgeFixture {
	t.Helper()
	return openFacadeBridgeFixtureWithLogger(t, chatSessionID, testMemoryLogger(), service)
}

func openFacadeBridgeFixtureWithLogger(t *testing.T, chatSessionID string, logger *slog.Logger, service *fakeMemoryService) facadeBridgeFixture {
	t.Helper()

	ctx := context.Background()
	if service == nil {
		service = &fakeMemoryService{}
	}
	chatDB := openBridgeChatDB(t, logger)
	if err := chatDB.CreateSession(ctx, chatSessionID, "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	host := &Host{
		Service: service,
		DBPath:  filepath.Join(t.TempDir(), "memory.db"),
		extractionPolicy: ExtractionHostPolicy{
			Enabled:                  true,
			AsyncEnabled:             true,
			TriggerOnFinalizeSegment: true,
			TriggerOnManualPin:       true,
			SessionEndMode:           memorycore.ExtractionRunModeApply,
			ManualPinMode:            memorycore.ExtractionRunModeApply,
			Timezone:                 "Asia/Shanghai",
			Limit:                    50,
		},
		logger: logger,
	}
	bridge := NewBridge(host, chatDB, logger, DefaultManualRules())
	segment, err := bridge.EnsureSegment(ctx, chatSessionID, "default")
	if err != nil {
		t.Fatalf("EnsureSegment: %v", err)
	}

	return facadeBridgeFixture{
		ctx:     ctx,
		service: service,
		bridge:  bridge,
		db:      chatDB,
		segment: segment,
	}
}

type fakeMemoryService struct {
	memorycore.Service

	startSessionID      string
	appendEpisodeSeq    int
	endSessionCalls     int
	runExtractionCalls  []memorycore.RunExtractionRequest
	runExtractionBlock  <-chan struct{}
	runExtractionResult *memorycore.ExtractionRunResult
	runExtractionErr    error
	consolidateCalls    int
	previewForgetCalls  []memorycore.ForgetPreviewRequest
	previewForgetResult *memorycore.ForgetPreviewResult
	previewForgetErr    error
	executeForgetCalls  []memorycore.ForgetExecuteRequest
	executeForgetResult *memorycore.ForgetExecuteResult
	executeForgetErr    error
	mirrorSyncCalls     int
	mirrorSyncResult    *memorycore.RunMirrorSyncResult
	mirrorSyncErr       error
}

func (f *fakeMemoryService) StartSession(context.Context, memorycore.StartSessionRequest) (*memorycore.Session, error) {
	if f.startSessionID == "" {
		f.startSessionID = "memory-session-1"
	}
	return &memorycore.Session{ID: f.startSessionID}, nil
}

func (f *fakeMemoryService) EndSession(context.Context, memorycore.EndSessionRequest) (*memorycore.Session, error) {
	f.endSessionCalls++
	endedAt := time.Now().UTC()
	return &memorycore.Session{ID: f.startSessionID, EndedAt: &endedAt}, nil
}

func (f *fakeMemoryService) AppendEpisode(_ context.Context, req memorycore.AppendEpisodeRequest) (*memorycore.Episode, error) {
	f.appendEpisodeSeq++
	return &memorycore.Episode{ID: "episode-" + req.Role + "-" + strconv.Itoa(f.appendEpisodeSeq)}, nil
}

func (f *fakeMemoryService) EnsureEntity(context.Context, memorycore.EnsureEntityRequest) (*memorycore.Entity, error) {
	return &memorycore.Entity{ID: "entity-user"}, nil
}

func (f *fakeMemoryService) ConsolidateCandidate(context.Context, memorycore.ConsolidateCandidateRequest) (*memorycore.ConsolidationResult, error) {
	f.consolidateCalls++
	return nil, errors.New("ConsolidateCandidate should not be called")
}

func (f *fakeMemoryService) RunExtraction(_ context.Context, req memorycore.RunExtractionRequest) (*memorycore.ExtractionRunResult, error) {
	f.runExtractionCalls = append(f.runExtractionCalls, req)
	if f.runExtractionBlock != nil {
		<-f.runExtractionBlock
	}
	if f.runExtractionResult != nil || f.runExtractionErr != nil {
		return f.runExtractionResult, f.runExtractionErr
	}
	return &memorycore.ExtractionRunResult{Status: memorycore.ExtractionRunStatusApplied, AppliedCount: 1}, nil
}

func (f *fakeMemoryService) RunMirrorSync(context.Context, memorycore.RunMirrorSyncRequest) (*memorycore.RunMirrorSyncResult, error) {
	f.mirrorSyncCalls++
	if f.mirrorSyncResult != nil || f.mirrorSyncErr != nil {
		return f.mirrorSyncResult, f.mirrorSyncErr
	}
	return &memorycore.RunMirrorSyncResult{}, nil
}

func (f *fakeMemoryService) PreviewForget(_ context.Context, req memorycore.ForgetPreviewRequest) (*memorycore.ForgetPreviewResult, error) {
	f.previewForgetCalls = append(f.previewForgetCalls, req)
	if f.previewForgetResult != nil || f.previewForgetErr != nil {
		return f.previewForgetResult, f.previewForgetErr
	}
	return &memorycore.ForgetPreviewResult{PersonaID: req.PersonaID, RequestID: req.RequestID, PreviewHash: "hash-1", RequestedLevel: req.RequestedLevel}, nil
}

func (f *fakeMemoryService) ExecuteForget(_ context.Context, req memorycore.ForgetExecuteRequest) (*memorycore.ForgetExecuteResult, error) {
	f.executeForgetCalls = append(f.executeForgetCalls, req)
	if f.executeForgetResult != nil || f.executeForgetErr != nil {
		return f.executeForgetResult, f.executeForgetErr
	}
	return &memorycore.ForgetExecuteResult{PersonaID: req.PersonaID, Executed: len(req.ConfirmedTargets), PreviewHash: req.PreviewHash}, nil
}
