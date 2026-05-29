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

func TestHostExtractSessionEndUsesServiceRunExtraction(t *testing.T) {
	fake := &fakeMemoryService{
		runExtractionResult: &memorycore.ExtractionRunResult{
			Status: memorycore.ExtractionRunStatusApplied,
		},
	}
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
	if err != nil {
		t.Fatalf("ExtractSessionEnd: %v", err)
	}
	if result == nil || result.Status != memorycore.ExtractionRunStatusApplied {
		t.Fatalf("result = %#v", result)
	}
	if len(fake.runExtractionCalls) != 1 {
		t.Fatalf("RunExtraction calls = %d, want 1", len(fake.runExtractionCalls))
	}
	req := fake.runExtractionCalls[0]
	if req.PersonaID != "persona-1" || req.SessionID == nil || *req.SessionID != sessionID {
		t.Fatalf("RunExtraction persona/session = %#v", req)
	}
	if req.Trigger != memorycore.ExtractionTriggerSessionEnd {
		t.Fatalf("trigger = %q, want %q", req.Trigger, memorycore.ExtractionTriggerSessionEnd)
	}
	if req.Mode != memorycore.ExtractionRunModeApply {
		t.Fatalf("mode = %q, want apply", req.Mode)
	}
	if req.Build == nil || req.Build.SessionID == nil || *req.Build.SessionID != sessionID || req.Build.Limit != 7 {
		t.Fatalf("build selector = %#v", req.Build)
	}
	if !req.SemanticDedup.Enabled || !req.SemanticDedup.Shadow || req.SemanticDedup.CandidateLimit != 5 {
		t.Fatalf("semantic dedup = %#v", req.SemanticDedup)
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

func TestBridgeFinalizeSegmentTriggersRunExtractionAndDoesNotFailOnExtractionError(t *testing.T) {
	fixture := openFacadeBridgeFixture(t, "chat-finalize", &fakeMemoryService{
		runExtractionResult: &memorycore.ExtractionRunResult{
			Status:             memorycore.ExtractionRunStatusFailed,
			SanitizedErrorCode: "provider_failed",
		},
		runExtractionErr: errors.New("raw provider failed with user text 我喜欢手冲咖啡"),
	})

	if err := fixture.bridge.FinalizeSegment(fixture.ctx, fixture.segment.SegmentID, "session_end", "summary"); err != nil {
		t.Fatalf("FinalizeSegment: %v", err)
	}
	if fixture.service.endSessionCalls != 1 {
		t.Fatalf("EndSession calls = %d, want 1", fixture.service.endSessionCalls)
	}
	if len(fixture.service.runExtractionCalls) != 1 {
		t.Fatalf("RunExtraction calls = %d, want 1", len(fixture.service.runExtractionCalls))
	}
	if fixture.service.runExtractionCalls[0].Trigger != memorycore.ExtractionTriggerSessionEnd {
		t.Fatalf("trigger = %q", fixture.service.runExtractionCalls[0].Trigger)
	}
}

func TestManualPinUsesRunExtractionAndAppendDoesNotFailOnExtractionError(t *testing.T) {
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
	if len(fixture.service.runExtractionCalls) != 1 {
		t.Fatalf("RunExtraction calls = %d, want 1", len(fixture.service.runExtractionCalls))
	}
	req := fixture.service.runExtractionCalls[0]
	if req.Trigger != memorycore.ExtractionTriggerManualPin {
		t.Fatalf("trigger = %q, want manual_pin", req.Trigger)
	}
	if req.Mode != memorycore.ExtractionRunModeApply {
		t.Fatalf("mode = %q, want apply", req.Mode)
	}
	if req.Build == nil || len(req.Build.EpisodeIDs) != 1 || req.Build.EpisodeIDs[0] != episodeID {
		t.Fatalf("episode ids = %#v, want %q", req.Build, episodeID)
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
	if !ok || !strings.Contains(notice, "已删除") {
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
	if !strings.Contains(logs, "error_code=provider_failed") {
		t.Fatalf("log = %s, want sanitized error code", logs)
	}
}

type facadeBridgeFixture struct {
	ctx     context.Context
	service *fakeMemoryService
	bridge  *Bridge
	segment storage.MemorySegmentRef
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
		segment: segment,
	}
}

type fakeMemoryService struct {
	memorycore.Service

	startSessionID      string
	appendEpisodeSeq    int
	endSessionCalls     int
	runExtractionCalls  []memorycore.RunExtractionRequest
	runExtractionResult *memorycore.ExtractionRunResult
	runExtractionErr    error
	consolidateCalls    int
	previewForgetCalls  []memorycore.ForgetPreviewRequest
	previewForgetResult *memorycore.ForgetPreviewResult
	previewForgetErr    error
	executeForgetCalls  []memorycore.ForgetExecuteRequest
	executeForgetResult *memorycore.ForgetExecuteResult
	executeForgetErr    error
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
	if f.runExtractionResult != nil || f.runExtractionErr != nil {
		return f.runExtractionResult, f.runExtractionErr
	}
	return &memorycore.ExtractionRunResult{Status: memorycore.ExtractionRunStatusApplied, AppliedCount: 1}, nil
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
