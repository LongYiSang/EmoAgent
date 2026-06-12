package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"github.com/longyisang/emoagent/internal/apperrors"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/configcenter"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/media"
	"github.com/longyisang/emoagent/internal/memoryhost"
	"github.com/longyisang/emoagent/internal/protocol"
	sidecarruntime "github.com/longyisang/emoagent/internal/sidecar"
	"github.com/longyisang/emoagent/internal/storage"
)

type fakeAdminApp struct {
	providers              []config.LLMProvider
	agentConfigs           []config.AgentConfig
	activeAgent            *config.AgentConfig
	personas               map[string]*config.Persona
	progressPhrases        map[string]map[string][]string
	sessions               []storage.SessionSummary
	sessionDetail          *storage.SessionRecord
	sessionMessages        []storage.MessageRecord
	createErr              error
	activateErr            error
	sessionErr             error
	deleteSessionErr       error
	approvals              []protocol.ApprovalRequest
	lastProvider           config.LLMProvider
	lastAgentConfig        config.AgentConfig
	lastActivate           string
	lastPersonaKey         string
	lastPersona            *config.Persona
	lastSessionPersona     string
	lastSessionLimit       int
	lastDeleteSessionID    string
	lastPhrasesKey         string
	lastPhrasesValue       map[string][]string
	lastApprovalSession    string
	lastExtractionReq      MemoryExtractionRequest
	lastExtractionList     MemoryExtractionListRequest
	extractionJobs         []storage.MemoryExtractionJob
	lastNaturalReq         NaturalMemoryRunRequest
	naturalRunResp         memoryhost.NaturalMemoryRunResponse
	naturalRunErr          error
	latestNaturalResp      *memoryhost.NaturalMemoryRunResponse
	lastSegmentSession     string
	memorySegments         []storage.MemorySegment
	chatSettings           config.ChatConfig
	lastChatSettings       config.ChatConfig
	updateChatErr          error
	messageParts           map[string][]storage.MessagePartRecord
	mediaAssets            map[string]*media.MediaAsset
	openMediaBytes         []byte
	openMediaAsset         *media.MediaAsset
	openMediaErr           error
	effectiveConfig        configcenter.EffectiveConfig
	configIssues           []configcenter.ConfigIssue
	providerEnvStatus      configcenter.ProviderEnvStatus
	memoryConfig           configcenter.MemoryConfigResponse
	lastMemoryConfig       config.MemoryConfig
	sidecarStatus          sidecarruntime.Status
	sidecarConfig          string
	sidecarLogs            string
	agentAffectConfig      AgentAffectConfigResponse
	updateAgentAffectErr   error
	lastAgentAffectConfig  config.AgentAffectConfig
	agentAffectCurrent     AgentAffectCurrentResponse
	agentAffectProfile     AgentAffectProfileResponse
	lastAgentAffectProfile AgentAffectProfileResponse
	agentAffectHistory     AgentAffectHistoryResponse
	lastAgentAffectHistory AgentAffectHistoryRequest
	agentAffectWrites      AgentAffectPluginWritesResponse
	lastAgentAffectWrites  AgentAffectPluginWritesRequest
	lastAgentAffectEval    AgentAffectEvaluateRequest
	agentAffectEvalResp    AgentAffectEvaluateResponse
	lastAgentAffectSubmit  AgentAffectSubmitRequest
	agentAffectSubmitResp  AgentAffectSubmitResponse
	lastAgentAffectDelta   AgentAffectDeltaRequest
	agentAffectDeltaResp   AgentAffectDeltaResponse
	lastAgentAffectReset   AgentAffectResetRequest
	agentAffectResetResp   AgentAffectResetResponse
	lastAgentAffectPrompt  AgentAffectPromptPreviewRequest
	agentAffectPromptResp  AgentAffectPromptPreviewResponse
	lastAgentAffectQueue   AgentAffectQueueRequest
	agentAffectQueueResp   AgentAffectQueueResponse
	agentAffectProcessResp AgentAffectProcessOnceResponse
	lastAgentAffectClear   AgentAffectQueueRequest
	agentAffectClearResp   AgentAffectClearFailedResponse
	lastAgentAffectSupers  AgentAffectQueueRequest
	agentAffectSupersResp  AgentAffectSupersedePendingResponse
	uploadAsset            *media.MediaAsset
}

func (f *fakeAdminApp) ListLLMProviders() ([]config.LLMProvider, error) {
	return append([]config.LLMProvider(nil), f.providers...), nil
}
func (f *fakeAdminApp) GetLLMProvider(id string) (*config.LLMProvider, error) {
	for i := range f.providers {
		if f.providers[i].ID == id {
			cp := f.providers[i]
			return &cp, nil
		}
	}
	return nil, apperrors.ErrLLMProviderNotFound
}
func (f *fakeAdminApp) CreateLLMProvider(provider config.LLMProvider) error {
	f.lastProvider = provider
	return f.createErr
}
func (f *fakeAdminApp) UpdateLLMProvider(id string, provider config.LLMProvider) error {
	provider.ID = id
	f.lastProvider = provider
	return nil
}
func (f *fakeAdminApp) DeleteLLMProvider(id string) error { return nil }
func (f *fakeAdminApp) RefreshLLMProviderModels(id string) ([]llm.ModelInfo, error) {
	return []llm.ModelInfo{{ID: "model-a"}}, nil
}
func (f *fakeAdminApp) GetLLMProviderModels(id string) ([]llm.ModelInfo, error) {
	return []llm.ModelInfo{{ID: "model-a"}}, nil
}
func (f *fakeAdminApp) GetLLMProviderEnvStatus(id string) (configcenter.ProviderEnvStatus, error) {
	return f.providerEnvStatus, nil
}
func (f *fakeAdminApp) UploadMedia(ctx context.Context, r io.Reader, meta media.UploadMeta) (*media.MediaAsset, error) {
	if f.uploadAsset != nil {
		return f.uploadAsset, nil
	}
	return &media.MediaAsset{ID: "med_test", Kind: "image", MimeType: "image/png", ByteSize: 68, Width: 1, Height: 1}, nil
}

func TestHandleUploadMedia(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "tiny.png")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write([]byte("png-bytes")); err != nil {
		t.Fatalf("write multipart: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close multipart: %v", err)
	}

	app := &fakeAdminApp{uploadAsset: &media.MediaAsset{ID: "med_123", Kind: "image", MimeType: "image/png", ByteSize: 9, Width: 1, Height: 1}}
	handler := NewAPIHandler(app, slog.Default())
	req := httptest.NewRequest(http.MethodPost, "/api/media", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()

	handler.HandleUploadMedia(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s, want 201", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "storage_uri") || strings.Contains(rr.Body.String(), "path") {
		t.Fatalf("upload response leaked storage path: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"media_asset_id":"med_123"`) {
		t.Fatalf("body = %s, want media_asset_id", rr.Body.String())
	}
}

func TestHandleGetSessionReturnsSafeDisplayParts(t *testing.T) {
	app := &fakeAdminApp{
		sessionDetail: &storage.SessionRecord{ID: "session-1", Persona: "default", Title: "chat"},
		sessionMessages: []storage.MessageRecord{{
			ID:        "msg-1",
			SessionID: "session-1",
			Role:      "user",
			Content:   "look\n[used image]",
			CreatedAt: "2026-06-13T00:00:00Z",
		}},
		messageParts: map[string][]storage.MessagePartRecord{
			"msg-1": {
				{ID: "part-1", SessionID: "session-1", MessageID: "msg-1", Role: "user", Ordinal: 0, PartType: "text", TextContent: "look"},
				{ID: "part-2", SessionID: "session-1", MessageID: "msg-1", Role: "user", Ordinal: 1, PartType: "image", MediaAssetID: "med_1"},
			},
		},
		mediaAssets: map[string]*media.MediaAsset{
			"med_1": {
				ID:         "med_1",
				Kind:       "image",
				MimeType:   "image/png",
				ByteSize:   68,
				Width:      1,
				Height:     1,
				StorageURI: `C:\secret\tiny.png`,
			},
		},
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/session-1", nil)
	req.SetPathValue("id", "session-1")
	rec := httptest.NewRecorder()

	handler.HandleGetSession(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, forbidden := range []string{"storage_uri", "C:", "secret", "data:image", "base64"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("session detail leaked %q in body: %s", forbidden, body)
		}
	}
	for _, want := range []string{
		`"parts"`,
		`"type":"text"`,
		`"text":"look"`,
		`"type":"image"`,
		`"media_asset_id":"med_1"`,
		`"display_url":"/api/sessions/session-1/media/med_1"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body = %s, want %s", body, want)
		}
	}
}

func TestHandleGetSessionMediaServesLinkedImage(t *testing.T) {
	app := &fakeAdminApp{
		openMediaBytes: []byte("png-bytes"),
		openMediaAsset: &media.MediaAsset{
			ID:       "med_1",
			Kind:     "image",
			MimeType: "image/png",
		},
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/session-1/media/med_1", nil)
	req.SetPathValue("id", "session-1")
	req.SetPathValue("media_id", "med_1")
	rec := httptest.NewRecorder()

	handler.HandleGetSessionMedia(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := rec.Header().Get("Content-Disposition"); got != "inline" {
		t.Fatalf("Content-Disposition = %q, want inline", got)
	}
	if got := rec.Body.String(); got != "png-bytes" {
		t.Fatalf("body = %q, want png-bytes", got)
	}
}

func TestHandleGetSessionMediaRejectsUnlinkedOrNonImage(t *testing.T) {
	t.Run("unlinked", func(t *testing.T) {
		handler := NewAPIHandler(&fakeAdminApp{openMediaErr: apperrors.ErrMediaNotFound}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		req := httptest.NewRequest(http.MethodGet, "/api/sessions/session-1/media/med_missing", nil)
		req.SetPathValue("id", "session-1")
		req.SetPathValue("media_id", "med_missing")
		rec := httptest.NewRecorder()

		handler.HandleGetSessionMedia(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d body=%s, want 404", rec.Code, rec.Body.String())
		}
	})
	t.Run("non-image", func(t *testing.T) {
		handler := NewAPIHandler(&fakeAdminApp{
			openMediaBytes: []byte("not image"),
			openMediaAsset: &media.MediaAsset{
				ID:       "med_file",
				Kind:     "file",
				MimeType: "text/plain",
			},
		}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		req := httptest.NewRequest(http.MethodGet, "/api/sessions/session-1/media/med_file", nil)
		req.SetPathValue("id", "session-1")
		req.SetPathValue("media_id", "med_file")
		rec := httptest.NewRecorder()

		handler.HandleGetSessionMedia(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d body=%s, want 404", rec.Code, rec.Body.String())
		}
	})
	t.Run("purged", func(t *testing.T) {
		handler := NewAPIHandler(&fakeAdminApp{
			openMediaBytes: []byte("png-bytes"),
			openMediaAsset: &media.MediaAsset{
				ID:               "med_purged",
				Kind:             "image",
				MimeType:         "image/png",
				VisibilityStatus: "purged",
			},
		}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		req := httptest.NewRequest(http.MethodGet, "/api/sessions/session-1/media/med_purged", nil)
		req.SetPathValue("id", "session-1")
		req.SetPathValue("media_id", "med_purged")
		rec := httptest.NewRecorder()

		handler.HandleGetSessionMedia(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d body=%s, want 404", rec.Code, rec.Body.String())
		}
	})
}
func (f *fakeAdminApp) ListAgentConfigs() ([]config.AgentConfig, error) {
	return append([]config.AgentConfig(nil), f.agentConfigs...), nil
}
func (f *fakeAdminApp) GetAgentConfig(id string) (*config.AgentConfig, error) {
	for i := range f.agentConfigs {
		if f.agentConfigs[i].ID == id {
			cp := f.agentConfigs[i]
			return &cp, nil
		}
	}
	return nil, apperrors.ErrAgentConfigNotFound
}
func (f *fakeAdminApp) GetActiveAgentConfig() (*config.AgentConfig, bool, error) {
	if f.activeAgent == nil {
		return nil, false, nil
	}
	cp := *f.activeAgent
	return &cp, true, nil
}
func (f *fakeAdminApp) CreateAgentConfig(agent config.AgentConfig) error {
	f.lastAgentConfig = agent
	return f.createErr
}
func (f *fakeAdminApp) UpdateAgentConfig(id string, agent config.AgentConfig) error {
	agent.ID = id
	f.lastAgentConfig = agent
	return nil
}
func (f *fakeAdminApp) ActivateAgentConfig(id string) error {
	f.lastActivate = id
	return f.activateErr
}
func (f *fakeAdminApp) DeleteAgentConfig(id string) error        { return nil }
func (f *fakeAdminApp) ListPersonas() map[string]*config.Persona { return f.personas }
func (f *fakeAdminApp) GetPersona(name string) (*config.Persona, bool) {
	p, ok := f.personas[name]
	return p, ok
}
func (f *fakeAdminApp) CreatePersona(key string, p *config.Persona) error {
	f.lastPersonaKey = key
	f.lastPersona = p
	return nil
}
func (f *fakeAdminApp) UpdatePersona(key string, p *config.Persona) error { return nil }
func (f *fakeAdminApp) DeletePersona(key string) error                    { return nil }
func (f *fakeAdminApp) GetProgressPhrases(key string) (map[string][]string, error) {
	if f.progressPhrases == nil {
		return map[string][]string{}, nil
	}
	phrases, ok := f.progressPhrases[key]
	if !ok {
		return nil, apperrors.ErrPersonaNotFound
	}
	out := make(map[string][]string, len(phrases))
	for k, v := range phrases {
		out[k] = append([]string(nil), v...)
	}
	return out, nil
}
func (f *fakeAdminApp) UpdateProgressPhrases(key string, phrases map[string][]string) error {
	f.lastPhrasesKey = key
	f.lastPhrasesValue = make(map[string][]string, len(phrases))
	for k, v := range phrases {
		f.lastPhrasesValue[k] = append([]string(nil), v...)
	}
	if f.progressPhrases == nil {
		f.progressPhrases = map[string]map[string][]string{}
	}
	f.progressPhrases[key] = f.lastPhrasesValue
	return nil
}
func (f *fakeAdminApp) ListSessions(_ context.Context, persona string, limit int) ([]storage.SessionSummary, error) {
	f.lastSessionPersona = persona
	f.lastSessionLimit = limit
	if f.sessionErr != nil {
		return nil, f.sessionErr
	}
	return append([]storage.SessionSummary(nil), f.sessions...), nil
}
func (f *fakeAdminApp) GetLatestSession(_ context.Context, persona string) (*storage.SessionSummary, error) {
	f.lastSessionPersona = persona
	if f.sessionErr != nil {
		return nil, f.sessionErr
	}
	if len(f.sessions) == 0 {
		return nil, nil
	}
	session := f.sessions[0]
	return &session, nil
}
func (f *fakeAdminApp) GetSessionDetail(_ context.Context, id string) (*storage.SessionRecord, []storage.MessageRecord, error) {
	if f.sessionErr != nil {
		return nil, nil, f.sessionErr
	}
	return f.sessionDetail, append([]storage.MessageRecord(nil), f.sessionMessages...), nil
}
func (f *fakeAdminApp) GetSessionMessageParts(_ context.Context, sessionID string) (map[string][]storage.MessagePartRecord, error) {
	if f.messageParts == nil {
		return map[string][]storage.MessagePartRecord{}, nil
	}
	result := make(map[string][]storage.MessagePartRecord, len(f.messageParts))
	for messageID, parts := range f.messageParts {
		result[messageID] = append([]storage.MessagePartRecord(nil), parts...)
	}
	return result, nil
}
func (f *fakeAdminApp) GetMediaAsset(_ context.Context, mediaID string) (*media.MediaAsset, error) {
	if f.mediaAssets == nil || f.mediaAssets[mediaID] == nil {
		return nil, apperrors.ErrMediaNotFound
	}
	asset := *f.mediaAssets[mediaID]
	return &asset, nil
}
func (f *fakeAdminApp) OpenSessionMedia(_ context.Context, sessionID, mediaID string) (io.ReadCloser, *media.MediaAsset, error) {
	if f.openMediaErr != nil {
		return nil, nil, f.openMediaErr
	}
	if f.openMediaAsset == nil {
		return nil, nil, apperrors.ErrMediaNotFound
	}
	asset := *f.openMediaAsset
	return io.NopCloser(bytes.NewReader(f.openMediaBytes)), &asset, nil
}
func (f *fakeAdminApp) DeleteSession(_ context.Context, id string) error {
	f.lastDeleteSessionID = id
	return f.deleteSessionErr
}
func (f *fakeAdminApp) ListSessionApprovals(_ context.Context, sessionID string) ([]protocol.ApprovalRequest, error) {
	f.lastApprovalSession = sessionID
	return append([]protocol.ApprovalRequest(nil), f.approvals...), nil
}
func (f *fakeAdminApp) QueueMemoryExtraction(_ context.Context, req MemoryExtractionRequest) (MemoryExtractionQueueResponse, error) {
	f.lastExtractionReq = req
	return MemoryExtractionQueueResponse{Status: "queued", EnqueuedCount: len(f.extractionJobs), Jobs: append([]storage.MemoryExtractionJob(nil), f.extractionJobs...)}, nil
}
func (f *fakeAdminApp) ListMemoryExtractions(_ context.Context, req MemoryExtractionListRequest) ([]storage.MemoryExtractionJob, error) {
	f.lastExtractionList = req
	return append([]storage.MemoryExtractionJob(nil), f.extractionJobs...), nil
}
func (f *fakeAdminApp) RunNaturalMemory(_ context.Context, req NaturalMemoryRunRequest) (memoryhost.NaturalMemoryRunResponse, error) {
	f.lastNaturalReq = req
	return f.naturalRunResp, f.naturalRunErr
}
func (f *fakeAdminApp) LatestNaturalMemoryRun(_ context.Context) (*memoryhost.NaturalMemoryRunResponse, error) {
	return f.latestNaturalResp, nil
}
func (f *fakeAdminApp) ListMemorySegments(_ context.Context, sessionID string) ([]storage.MemorySegment, error) {
	f.lastSegmentSession = sessionID
	return append([]storage.MemorySegment(nil), f.memorySegments...), nil
}
func (f *fakeAdminApp) GetChatSettings() config.ChatConfig {
	return f.chatSettings
}
func (f *fakeAdminApp) UpdateChatSettings(settings config.ChatConfig) error {
	f.lastChatSettings = settings
	return f.updateChatErr
}
func (f *fakeAdminApp) GetEffectiveConfig(ctx context.Context) (configcenter.EffectiveConfig, error) {
	return f.effectiveConfig, nil
}
func (f *fakeAdminApp) ValidateConfig(ctx context.Context, req configcenter.ValidateRequest) (configcenter.ValidateResponse, error) {
	return configcenter.ValidateResponse{Issues: append([]configcenter.ConfigIssue(nil), f.configIssues...)}, nil
}
func (f *fakeAdminApp) ListConfigIssues(ctx context.Context) ([]configcenter.ConfigIssue, error) {
	return append([]configcenter.ConfigIssue(nil), f.configIssues...), nil
}
func (f *fakeAdminApp) GetMemoryConfig(ctx context.Context) (configcenter.MemoryConfigResponse, error) {
	return f.memoryConfig, nil
}
func (f *fakeAdminApp) UpdateMemoryConfig(ctx context.Context, memory config.MemoryConfig) (configcenter.EffectiveConfig, error) {
	f.lastMemoryConfig = memory
	return f.effectiveConfig, nil
}
func (f *fakeAdminApp) GetMemoryFeatures(ctx context.Context) (configcenter.MemoryConfigResponse, error) {
	return f.memoryConfig, nil
}
func (f *fakeAdminApp) UpdateMemoryFeatures(ctx context.Context, memory config.MemoryConfig) (configcenter.EffectiveConfig, error) {
	f.lastMemoryConfig = memory
	return f.effectiveConfig, nil
}
func (f *fakeAdminApp) GetSidecarStatus(ctx context.Context) (sidecarruntime.Status, error) {
	return f.sidecarStatus, nil
}
func (f *fakeAdminApp) StartSidecar(ctx context.Context) (sidecarruntime.Status, error) {
	f.sidecarStatus.State = sidecarruntime.StateHealthy
	return f.sidecarStatus, nil
}
func (f *fakeAdminApp) StopSidecar(ctx context.Context) (sidecarruntime.Status, error) {
	f.sidecarStatus.State = sidecarruntime.StateStopped
	return f.sidecarStatus, nil
}
func (f *fakeAdminApp) RestartSidecar(ctx context.Context) (sidecarruntime.Status, error) {
	f.sidecarStatus.State = sidecarruntime.StateHealthy
	return f.sidecarStatus, nil
}
func (f *fakeAdminApp) GetSidecarGeneratedConfig(ctx context.Context) (string, error) {
	return f.sidecarConfig, nil
}
func (f *fakeAdminApp) GetSidecarLogs(ctx context.Context, maxBytes int) (string, error) {
	return f.sidecarLogs, nil
}
func (f *fakeAdminApp) GetAgentAffectConfig(ctx context.Context) (AgentAffectConfigResponse, error) {
	return f.agentAffectConfig, nil
}
func (f *fakeAdminApp) UpdateAgentAffectConfig(ctx context.Context, cfg config.AgentAffectConfig) (configcenter.EffectiveConfig, error) {
	f.lastAgentAffectConfig = cfg
	if f.updateAgentAffectErr != nil {
		return configcenter.EffectiveConfig{}, f.updateAgentAffectErr
	}
	return f.effectiveConfig, nil
}
func (f *fakeAdminApp) GetAgentAffectProfile(ctx context.Context, personaID string) (AgentAffectProfileResponse, error) {
	if f.agentAffectProfile.PersonaID == "" {
		f.agentAffectProfile.PersonaID = personaID
	}
	return f.agentAffectProfile, nil
}
func (f *fakeAdminApp) UpdateAgentAffectProfile(ctx context.Context, profile AgentAffectProfileResponse) (AgentAffectProfileResponse, error) {
	f.lastAgentAffectProfile = profile
	return profile, nil
}
func (f *fakeAdminApp) ListAgentAffectHistory(ctx context.Context, req AgentAffectHistoryRequest) (AgentAffectHistoryResponse, error) {
	f.lastAgentAffectHistory = req
	return f.agentAffectHistory, nil
}
func (f *fakeAdminApp) ListAgentAffectPluginWrites(ctx context.Context, req AgentAffectPluginWritesRequest) (AgentAffectPluginWritesResponse, error) {
	f.lastAgentAffectWrites = req
	return f.agentAffectWrites, nil
}
func (f *fakeAdminApp) GetAgentAffectCurrent(ctx context.Context, req AgentAffectCurrentRequest) (AgentAffectCurrentResponse, error) {
	if f.agentAffectCurrent.Mood.PersonaID == "" {
		f.agentAffectCurrent.Mood.PersonaID = req.PersonaID
	}
	return f.agentAffectCurrent, nil
}
func (f *fakeAdminApp) EvaluateAgentAffect(ctx context.Context, req AgentAffectEvaluateRequest) (AgentAffectEvaluateResponse, error) {
	f.lastAgentAffectEval = req
	return f.agentAffectEvalResp, nil
}
func (f *fakeAdminApp) SubmitAgentAffect(ctx context.Context, req AgentAffectSubmitRequest) (AgentAffectSubmitResponse, error) {
	f.lastAgentAffectSubmit = req
	return f.agentAffectSubmitResp, nil
}
func (f *fakeAdminApp) ApplyAgentAffectDelta(ctx context.Context, req AgentAffectDeltaRequest) (AgentAffectDeltaResponse, error) {
	f.lastAgentAffectDelta = req
	return f.agentAffectDeltaResp, nil
}
func (f *fakeAdminApp) ResetAgentAffect(ctx context.Context, req AgentAffectResetRequest) (AgentAffectResetResponse, error) {
	f.lastAgentAffectReset = req
	return f.agentAffectResetResp, nil
}
func (f *fakeAdminApp) PreviewAgentAffectPrompt(ctx context.Context, req AgentAffectPromptPreviewRequest) (AgentAffectPromptPreviewResponse, error) {
	f.lastAgentAffectPrompt = req
	return f.agentAffectPromptResp, nil
}
func (f *fakeAdminApp) GetAgentAffectQueue(ctx context.Context, req AgentAffectQueueRequest) (AgentAffectQueueResponse, error) {
	f.lastAgentAffectQueue = req
	return f.agentAffectQueueResp, nil
}
func (f *fakeAdminApp) ProcessAgentAffectBatchOnce(ctx context.Context) (AgentAffectProcessOnceResponse, error) {
	return f.agentAffectProcessResp, nil
}
func (f *fakeAdminApp) ClearAgentAffectFailedJobs(ctx context.Context, req AgentAffectQueueRequest) (AgentAffectClearFailedResponse, error) {
	f.lastAgentAffectClear = req
	return f.agentAffectClearResp, nil
}
func (f *fakeAdminApp) SupersedeAgentAffectPendingJobs(ctx context.Context, req AgentAffectQueueRequest) (AgentAffectSupersedePendingResponse, error) {
	f.lastAgentAffectSupers = req
	return f.agentAffectSupersResp, nil
}

func TestHandleAgentAffectConfig(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	cfg.Enabled = true
	cfg.Evaluator.Mode = "disabled"
	app := &fakeAdminApp{
		agentAffectConfig: AgentAffectConfigResponse{
			AgentAffect: cfg,
		},
		effectiveConfig: configcenter.EffectiveConfig{
			AgentAffect: cfg,
		},
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	getReq := httptest.NewRequest(http.MethodGet, "/api/agent-affect/config", nil)
	getRec := httptest.NewRecorder()
	handler.HandleGetAgentAffectConfig(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", getRec.Code, getRec.Body.String())
	}
	var getResp AgentAffectConfigResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if !getResp.AgentAffect.Enabled || getResp.AgentAffect.Evaluator.Mode != "disabled" {
		t.Fatalf("get response = %#v", getResp.AgentAffect)
	}

	putReq := httptest.NewRequest(http.MethodPut, "/api/agent-affect/config", bytes.NewBufferString(`{
		"agent_affect": {
			"enabled": true,
			"storage_enabled": true,
			"evaluator": {"mode": "disabled"},
			"context": {"store_raw_inputs": false}
		}
	}`))
	putRec := httptest.NewRecorder()
	handler.HandleUpdateAgentAffectConfig(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("put status = %d body=%s", putRec.Code, putRec.Body.String())
	}
	if !app.lastAgentAffectConfig.Enabled || app.lastAgentAffectConfig.Context.StoreRawInputs {
		t.Fatalf("last agent_affect config = %#v", app.lastAgentAffectConfig)
	}
	var putResp configcenter.EffectiveConfig
	if err := json.Unmarshal(putRec.Body.Bytes(), &putResp); err != nil {
		t.Fatalf("decode put: %v", err)
	}
	if !putResp.AgentAffect.Enabled {
		t.Fatalf("put response = %#v", putResp.AgentAffect)
	}
}

func TestHandleAgentAffectConfigValidationError(t *testing.T) {
	app := &fakeAdminApp{
		updateAgentAffectErr: &configcenter.ValidationError{Issues: []configcenter.ConfigIssue{{
			Path:     "agent_affect.storage_enabled",
			Severity: "error",
			Message:  "agent_affect.enabled requires agent_affect.storage_enabled",
		}}},
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodPut, "/api/agent-affect/config", bytes.NewBufferString(`{
		"agent_affect": {"enabled": true, "storage_enabled": false}
	}`))
	rec := httptest.NewRecorder()
	handler.HandleUpdateAgentAffectConfig(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Issues []configcenter.ConfigIssue `json:"issues"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode validation response: %v", err)
	}
	if len(resp.Issues) != 1 || resp.Issues[0].Path != "agent_affect.storage_enabled" {
		t.Fatalf("issues = %#v", resp.Issues)
	}
}

func TestHandleAgentAffectHistoryProfileResetPromptAndPluginWrites(t *testing.T) {
	app := &fakeAdminApp{
		agentAffectProfile: AgentAffectProfileResponse{
			PersonaID:   "default",
			ProfileName: "default",
			Baseline:    AgentAffectCurrentResponse{}.Mood.Vector,
		},
		agentAffectHistory: AgentAffectHistoryResponse{},
		agentAffectWrites: AgentAffectPluginWritesResponse{{
			PluginID:    "demo",
			Capability:  "agent_affect.submit",
			RequestKind: "submit",
			Accepted:    true,
		}},
		agentAffectResetResp: AgentAffectResetResponse{
			EventID: "event-reset",
		},
		agentAffectPromptResp: AgentAffectPromptPreviewResponse{
			PromptBlock: "[Agent Affect Runtime State]\nmood_vector:\n  valence: 0.100\ncause_summary: test\nattachment_expression: style=gentle_explicit",
		},
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	historyReq := httptest.NewRequest(http.MethodGet, "/api/agent-affect/history?persona_id=default&session_id=s1&kind=both&limit=7", nil)
	historyRec := httptest.NewRecorder()
	handler.HandleListAgentAffectHistory(historyRec, historyReq)
	if historyRec.Code != http.StatusOK {
		t.Fatalf("history status = %d body=%s", historyRec.Code, historyRec.Body.String())
	}
	if app.lastAgentAffectHistory.PersonaID != "default" || app.lastAgentAffectHistory.SessionID != "s1" || app.lastAgentAffectHistory.Limit != 7 {
		t.Fatalf("history request = %#v", app.lastAgentAffectHistory)
	}

	writesReq := httptest.NewRequest(http.MethodGet, "/api/agent-affect/plugin-writes?plugin_id=demo&limit=5", nil)
	writesRec := httptest.NewRecorder()
	handler.HandleListAgentAffectPluginWrites(writesRec, writesReq)
	if writesRec.Code != http.StatusOK {
		t.Fatalf("writes status = %d body=%s", writesRec.Code, writesRec.Body.String())
	}
	if app.lastAgentAffectWrites.PluginID != "demo" || app.lastAgentAffectWrites.Limit != 5 {
		t.Fatalf("writes request = %#v", app.lastAgentAffectWrites)
	}

	profileReq := httptest.NewRequest(http.MethodPut, "/api/agent-affect/profile", bytes.NewBufferString(`{
		"persona_id": "default",
		"profile_name": "default",
		"baseline": {"warmth": 0.7}
	}`))
	profileRec := httptest.NewRecorder()
	handler.HandleUpdateAgentAffectProfile(profileRec, profileReq)
	if profileRec.Code != http.StatusOK {
		t.Fatalf("profile status = %d body=%s", profileRec.Code, profileRec.Body.String())
	}
	if app.lastAgentAffectProfile.Baseline.Warmth != 0.7 {
		t.Fatalf("profile request = %#v", app.lastAgentAffectProfile)
	}

	resetReq := httptest.NewRequest(http.MethodPost, "/api/agent-affect/reset", bytes.NewBufferString(`{"persona_id":"default","session_id":"s1","reason":"smoke"}`))
	resetRec := httptest.NewRecorder()
	handler.HandleResetAgentAffect(resetRec, resetReq)
	if resetRec.Code != http.StatusOK {
		t.Fatalf("reset status = %d body=%s", resetRec.Code, resetRec.Body.String())
	}
	if app.lastAgentAffectReset.Reason != "smoke" || app.lastAgentAffectReset.SessionID != "s1" {
		t.Fatalf("reset request = %#v", app.lastAgentAffectReset)
	}

	promptReq := httptest.NewRequest(http.MethodPost, "/api/agent-affect/prompt-preview", bytes.NewBufferString(`{"persona_id":"default","session_id":"s1"}`))
	promptRec := httptest.NewRecorder()
	handler.HandlePreviewAgentAffectPrompt(promptRec, promptReq)
	if promptRec.Code != http.StatusOK {
		t.Fatalf("prompt status = %d body=%s", promptRec.Code, promptRec.Body.String())
	}
	if !strings.Contains(promptRec.Body.String(), "[Agent Affect Runtime State]") || !strings.Contains(promptRec.Body.String(), "attachment_expression") {
		t.Fatalf("prompt response = %s", promptRec.Body.String())
	}
}

func TestHandleAgentAffectCurrentAndEvaluate(t *testing.T) {
	app := &fakeAdminApp{
		agentAffectCurrent: AgentAffectCurrentResponse{
			Enabled: true,
		},
		agentAffectEvalResp: AgentAffectEvaluateResponse{
			Enabled:      true,
			EvaluationID: "eval-1",
		},
	}
	app.agentAffectCurrent.Mood.PersonaID = "default"
	app.agentAffectCurrent.Mood.Vector.Valence = 0.2
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	currentReq := httptest.NewRequest(http.MethodGet, "/api/agent-affect/current?persona_id=default&session_id=s1&view=plugin_safe", nil)
	currentRec := httptest.NewRecorder()
	handler.HandleGetAgentAffectCurrent(currentRec, currentReq)
	if currentRec.Code != http.StatusOK {
		t.Fatalf("current status = %d", currentRec.Code)
	}
	var currentResp AgentAffectCurrentResponse
	if err := json.Unmarshal(currentRec.Body.Bytes(), &currentResp); err != nil {
		t.Fatalf("decode current: %v", err)
	}
	if currentResp.Mood.PersonaID != "default" || currentResp.Mood.Vector.Valence != 0.2 {
		t.Fatalf("current resp = %#v", currentResp)
	}

	evalReq := httptest.NewRequest(http.MethodPost, "/api/agent-affect/evaluate", bytes.NewBufferString(`{
		"persona_id": "default",
		"session_id": "s1",
		"trigger": {"trigger_type": "debug"},
		"input": {"mode": "summary", "summary": "preview only"}
	}`))
	evalRec := httptest.NewRecorder()
	handler.HandleEvaluateAgentAffect(evalRec, evalReq)
	if evalRec.Code != http.StatusOK {
		t.Fatalf("evaluate status = %d body=%s", evalRec.Code, evalRec.Body.String())
	}
	if app.lastAgentAffectEval.Input.Summary != "preview only" {
		t.Fatalf("last eval req = %#v", app.lastAgentAffectEval)
	}
}

func TestHandleAgentAffectQueueActions(t *testing.T) {
	app := &fakeAdminApp{
		agentAffectQueueResp:   AgentAffectQueueResponse{PendingJobs: 2, RunningJobs: 1, FailedJobs: 3},
		agentAffectProcessResp: AgentAffectProcessOnceResponse{Processed: true},
		agentAffectClearResp:   AgentAffectClearFailedResponse{Cleared: 3},
		agentAffectSupersResp:  AgentAffectSupersedePendingResponse{Superseded: 2},
	}
	handler := NewAPIHandler(app, slog.Default())

	queueReq := httptest.NewRequest(http.MethodGet, "/api/agent-affect/queue?persona_id=default&session_id=s1&limit=5", nil)
	queueRec := httptest.NewRecorder()
	handler.HandleGetAgentAffectQueue(queueRec, queueReq)
	if queueRec.Code != http.StatusOK {
		t.Fatalf("queue status = %d body=%s", queueRec.Code, queueRec.Body.String())
	}
	if app.lastAgentAffectQueue.PersonaID != "default" || app.lastAgentAffectQueue.SessionID != "s1" || app.lastAgentAffectQueue.Limit != 5 {
		t.Fatalf("queue request = %#v", app.lastAgentAffectQueue)
	}

	processReq := httptest.NewRequest(http.MethodPost, "/api/agent-affect/process-once", nil)
	processRec := httptest.NewRecorder()
	handler.HandleProcessAgentAffectBatchOnce(processRec, processReq)
	if processRec.Code != http.StatusOK || !strings.Contains(processRec.Body.String(), `"processed":true`) {
		t.Fatalf("process response status=%d body=%s", processRec.Code, processRec.Body.String())
	}

	clearReq := httptest.NewRequest(http.MethodPost, "/api/agent-affect/clear-failed", bytes.NewBufferString(`{"persona_id":"default","session_id":"s1"}`))
	clearRec := httptest.NewRecorder()
	handler.HandleClearAgentAffectFailedJobs(clearRec, clearReq)
	if clearRec.Code != http.StatusOK || app.lastAgentAffectClear.SessionID != "s1" {
		t.Fatalf("clear status=%d req=%#v body=%s", clearRec.Code, app.lastAgentAffectClear, clearRec.Body.String())
	}

	supersReq := httptest.NewRequest(http.MethodPost, "/api/agent-affect/supersede-pending", bytes.NewBufferString(`{"persona_id":"default","session_id":"s1"}`))
	supersRec := httptest.NewRecorder()
	handler.HandleSupersedeAgentAffectPendingJobs(supersRec, supersReq)
	if supersRec.Code != http.StatusOK || app.lastAgentAffectSupers.PersonaID != "default" {
		t.Fatalf("supersede status=%d req=%#v body=%s", supersRec.Code, app.lastAgentAffectSupers, supersRec.Body.String())
	}
}

func TestHandleAgentAffectSubmitAndDelta(t *testing.T) {
	app := &fakeAdminApp{
		agentAffectSubmitResp: AgentAffectSubmitResponse{
			EvaluationID: "eval-1",
			EventID:      "event-1",
		},
		agentAffectDeltaResp: AgentAffectDeltaResponse{
			EventID: "event-2",
		},
	}
	app.agentAffectDeltaResp.ClampedDelta.Valence = 0.15
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	submitReq := httptest.NewRequest(http.MethodPost, "/api/agent-affect/submit", bytes.NewBufferString(`{
		"persona_id": "default",
		"session_id": "s1",
		"trigger": {"trigger_type": "debug"},
		"input": {"mode": "summary", "summary": "commit this"},
		"commit_mode": "commit_if_allowed"
	}`))
	submitRec := httptest.NewRecorder()
	handler.HandleSubmitAgentAffect(submitRec, submitReq)
	if submitRec.Code != http.StatusOK {
		t.Fatalf("submit status = %d body=%s", submitRec.Code, submitRec.Body.String())
	}
	if app.lastAgentAffectSubmit.CommitMode != "commit_if_allowed" || app.lastAgentAffectSubmit.Input.Summary != "commit this" {
		t.Fatalf("last submit req = %#v", app.lastAgentAffectSubmit)
	}

	deltaReq := httptest.NewRequest(http.MethodPost, "/api/agent-affect/delta", bytes.NewBufferString(`{
		"persona_id": "default",
		"session_id": "s1",
		"trigger": {"trigger_type": "debug"},
		"delta": {"valence": 0.9}
	}`))
	deltaRec := httptest.NewRecorder()
	handler.HandleApplyAgentAffectDelta(deltaRec, deltaReq)
	if deltaRec.Code != http.StatusOK {
		t.Fatalf("delta status = %d body=%s", deltaRec.Code, deltaRec.Body.String())
	}
	var deltaResp AgentAffectDeltaResponse
	if err := json.Unmarshal(deltaRec.Body.Bytes(), &deltaResp); err != nil {
		t.Fatalf("decode delta: %v", err)
	}
	if app.lastAgentAffectDelta.Delta.Valence != 0.9 || deltaResp.ClampedDelta.Valence != 0.15 {
		t.Fatalf("last delta req = %#v resp=%#v", app.lastAgentAffectDelta, deltaResp)
	}
}

func TestHandleCreateLLMProviderNormalizesPayload(t *testing.T) {
	app := &fakeAdminApp{}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	body := bytes.NewBufferString(`{"id":" moonshot ","name":" Moonshot ","preset_id":" moonshot ","protocol":"openai_compatible","base_url":"https://api.moonshot.cn/","api_key_env":" MOONSHOT_API_KEY ","enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/llm-providers", body)
	rec := httptest.NewRecorder()
	handler.HandleCreateLLMProvider(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if app.lastProvider.ID != "moonshot" || app.lastProvider.BaseURL != "https://api.moonshot.cn" {
		t.Fatalf("lastProvider = %#v, want normalized id/base_url", app.lastProvider)
	}
	if app.lastProvider.ModelDiscovery != "manual" {
		t.Fatalf("ModelDiscovery = %q, want manual", app.lastProvider.ModelDiscovery)
	}
	if app.lastProvider.PresetID != "moonshot" {
		t.Fatalf("PresetID = %q, want moonshot", app.lastProvider.PresetID)
	}
}

func TestHandleListLLMProviderPresets(t *testing.T) {
	handler := NewAPIHandler(&fakeAdminApp{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/llm-provider-presets", nil)
	rec := httptest.NewRecorder()
	handler.HandleListLLMProviderPresets(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Presets []llm.ProviderPreset `json:"presets"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	var foundMoonshot bool
	for _, preset := range body.Presets {
		if preset.ID == "moonshot" {
			foundMoonshot = true
			if preset.Admin.MainDefaults.MaxTokens == 0 {
				t.Fatalf("moonshot main defaults missing: %#v", preset.Admin)
			}
		}
	}
	if !foundMoonshot {
		t.Fatalf("moonshot preset missing from response: %#v", body.Presets)
	}
}

func TestHandleGetLLMProviderEnvStatusDoesNotLeakValue(t *testing.T) {
	app := &fakeAdminApp{
		providerEnvStatus: configcenter.ProviderEnvStatus{
			APIKeyEnv: "MOONSHOT_API_KEY",
			Present:   true,
		},
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/llm-providers/moonshot/env-status", nil)
	rec := httptest.NewRecorder()
	handler.HandleGetLLMProviderEnvStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"api_key_env":"MOONSHOT_API_KEY"`) || !strings.Contains(body, `"present":true`) {
		t.Fatalf("body = %s", body)
	}
	if strings.Contains(body, "secret") {
		t.Fatalf("body leaked secret value: %s", body)
	}
}

func TestHandleConfigEffective(t *testing.T) {
	app := &fakeAdminApp{
		effectiveConfig: configcenter.EffectiveConfig{
			AgentAffect: config.AgentAffectConfig{
				Enabled:        true,
				StorageEnabled: true,
				Evaluator:      config.AgentAffectEvaluatorConfig{Mode: "disabled"},
			},
			Providers: []configcenter.ProviderEffective{{
				ID:      "moonshot",
				Enabled: true,
				Env:     configcenter.ProviderEnvStatus{APIKeyEnv: "MOONSHOT_API_KEY", Present: true},
			}},
			Issues: []configcenter.ConfigIssue{{
				Path:     "memory.retrieval.enabled",
				Severity: "warning",
				Message:  "memory retrieval is disabled because memory is disabled",
			}},
		},
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/config/effective", nil)
	rec := httptest.NewRecorder()
	handler.HandleGetConfigEffective(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp configcenter.EffectiveConfig
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(resp.Providers) != 1 || resp.Providers[0].Env.APIKeyEnv != "MOONSHOT_API_KEY" || !resp.Providers[0].Env.Present {
		t.Fatalf("providers = %#v", resp.Providers)
	}
	if !resp.AgentAffect.Enabled || resp.AgentAffect.Evaluator.Mode != "disabled" {
		t.Fatalf("agent_affect = %#v", resp.AgentAffect)
	}
	if len(resp.Issues) != 1 || resp.Issues[0].Path != "memory.retrieval.enabled" {
		t.Fatalf("issues = %#v", resp.Issues)
	}
}

func TestHandleConfigValidateAndIssues(t *testing.T) {
	app := &fakeAdminApp{
		configIssues: []configcenter.ConfigIssue{{
			Path:     "memory.retrieval.use_mirror",
			Severity: "warning",
			Message:  "use_mirror requires sidecar.enabled",
			AutoFix:  &configcenter.AutoFix{Value: false},
		}},
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	validateReq := httptest.NewRequest(http.MethodPost, "/api/config/validate", bytes.NewBufferString(`{}`))
	validateRec := httptest.NewRecorder()
	handler.HandleValidateConfig(validateRec, validateReq)
	if validateRec.Code != http.StatusOK {
		t.Fatalf("validate status = %d, want 200", validateRec.Code)
	}
	var validateResp configcenter.ValidateResponse
	if err := json.NewDecoder(validateRec.Body).Decode(&validateResp); err != nil {
		t.Fatalf("Decode validate: %v", err)
	}
	if len(validateResp.Issues) != 1 || validateResp.Issues[0].AutoFix == nil {
		t.Fatalf("validate issues = %#v", validateResp.Issues)
	}

	issuesReq := httptest.NewRequest(http.MethodGet, "/api/config/issues", nil)
	issuesRec := httptest.NewRecorder()
	handler.HandleListConfigIssues(issuesRec, issuesReq)
	if issuesRec.Code != http.StatusOK {
		t.Fatalf("issues status = %d, want 200", issuesRec.Code)
	}
	var issuesResp struct {
		Issues []configcenter.ConfigIssue `json:"issues"`
	}
	if err := json.NewDecoder(issuesRec.Body).Decode(&issuesResp); err != nil {
		t.Fatalf("Decode issues: %v", err)
	}
	if len(issuesResp.Issues) != 1 || issuesResp.Issues[0].Path != "memory.retrieval.use_mirror" {
		t.Fatalf("issues response = %#v", issuesResp)
	}
}

func TestHandleMemoryConfigAndSidecarEndpoints(t *testing.T) {
	app := &fakeAdminApp{
		memoryConfig: configcenter.MemoryConfigResponse{
			Memory: config.MemoryConfig{Enabled: true, ConfigPath: "./config/memorycore.yaml"},
		},
		effectiveConfig: configcenter.EffectiveConfig{
			Memory: config.MemoryConfig{Enabled: true, ConfigPath: "./config/memorycore.yaml"},
		},
		sidecarStatus: sidecarruntime.Status{State: sidecarruntime.StateStopped, URL: "http://127.0.0.1:8765", Adapter: "trivium"},
		sidecarConfig: "[embedding]\napi_key_env = \"DASHSCOPE_API_KEY\"\n",
		sidecarLogs:   "sidecar log line",
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	getMemoryReq := httptest.NewRequest(http.MethodGet, "/api/memory/config", nil)
	getMemoryRec := httptest.NewRecorder()
	handler.HandleGetMemoryConfig(getMemoryRec, getMemoryReq)
	if getMemoryRec.Code != http.StatusOK {
		t.Fatalf("GET memory status = %d, want 200", getMemoryRec.Code)
	}

	putMemoryReq := httptest.NewRequest(http.MethodPut, "/api/memory/config", bytes.NewBufferString(`{"memory":{"enabled":true,"config_path":"./config/memorycore.yaml"}}`))
	putMemoryRec := httptest.NewRecorder()
	handler.HandleUpdateMemoryConfig(putMemoryRec, putMemoryReq)
	if putMemoryRec.Code != http.StatusOK {
		t.Fatalf("PUT memory status = %d, want 200", putMemoryRec.Code)
	}
	if !app.lastMemoryConfig.Enabled || app.lastMemoryConfig.ConfigPath != "./config/memorycore.yaml" {
		t.Fatalf("lastMemoryConfig = %#v", app.lastMemoryConfig)
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/api/sidecar/status", nil)
	statusRec := httptest.NewRecorder()
	handler.HandleGetSidecarStatus(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("sidecar status code = %d, want 200", statusRec.Code)
	}
	var statusResp sidecarruntime.Status
	if err := json.NewDecoder(statusRec.Body).Decode(&statusResp); err != nil {
		t.Fatalf("Decode sidecar status: %v", err)
	}
	if statusResp.Adapter != "trivium" {
		t.Fatalf("sidecar status = %#v", statusResp)
	}

	configReq := httptest.NewRequest(http.MethodGet, "/api/sidecar/generated-config", nil)
	configRec := httptest.NewRecorder()
	handler.HandleGetSidecarGeneratedConfig(configRec, configReq)
	if configRec.Code != http.StatusOK || !strings.Contains(configRec.Body.String(), "DASHSCOPE_API_KEY") {
		t.Fatalf("generated config response = %d %s", configRec.Code, configRec.Body.String())
	}

	logsReq := httptest.NewRequest(http.MethodGet, "/api/sidecar/logs?max_bytes=100", nil)
	logsRec := httptest.NewRecorder()
	handler.HandleGetSidecarLogs(logsRec, logsReq)
	if logsRec.Code != http.StatusOK || !strings.Contains(logsRec.Body.String(), "sidecar log line") {
		t.Fatalf("logs response = %d %s", logsRec.Code, logsRec.Body.String())
	}

	startReq := httptest.NewRequest(http.MethodPost, "/api/sidecar/start", nil)
	startRec := httptest.NewRecorder()
	handler.HandleStartSidecar(startRec, startReq)
	if startRec.Code != http.StatusOK {
		t.Fatalf("start status = %d, want 200", startRec.Code)
	}
}

func TestHandleCreateAgentConfigParsesBindings(t *testing.T) {
	app := &fakeAdminApp{}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	body := bytes.NewBufferString(`{
		"id":"default",
		"name":"Default",
		"persona_key":"default",
		"emotion":{
			"main":{"provider_id":"moonshot","model":"emotion-main","params":{"max_tokens":111,"temperature":0.2,"stream":true}},
			"summary":{"provider_id":"moonshot","model":"emotion-summary","params":{"max_tokens":222,"temperature":0.1,"stream":false}}
		},
		"work":{
			"main":{"provider_id":"anthropic","model":"work-main","params":{"max_tokens":333,"thinking":{"mode":"enabled","budget_tokens":1024}}},
			"summary":{"provider_id":"moonshot","model":"work-summary","params":{"max_tokens":444}}
		},
		"context_overrides":{"input_budget_tokens":9000}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/agent-configs", body)
	rec := httptest.NewRecorder()
	handler.HandleCreateAgentConfig(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if app.lastAgentConfig.Emotion.Main.Model != "emotion-main" || app.lastAgentConfig.Work.Summary.Model != "work-summary" {
		t.Fatalf("lastAgentConfig = %#v, want parsed models", app.lastAgentConfig)
	}
	if app.lastAgentConfig.Emotion.Main.Params.Temperature == nil || *app.lastAgentConfig.Emotion.Main.Params.Temperature != 0.2 {
		t.Fatalf("emotion main temperature = %#v, want 0.2", app.lastAgentConfig.Emotion.Main.Params.Temperature)
	}
	if app.lastAgentConfig.Work.Main.Params.Thinking == nil || app.lastAgentConfig.Work.Main.Params.Thinking.BudgetTokens == nil || *app.lastAgentConfig.Work.Main.Params.Thinking.BudgetTokens != 1024 {
		t.Fatalf("work thinking = %#v, want budget 1024", app.lastAgentConfig.Work.Main.Params.Thinking)
	}
}

func TestHandleChatSettingsRoundTrip(t *testing.T) {
	app := &fakeAdminApp{chatSettings: config.ChatConfig{RealtimeStreaming: true}}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	getReq := httptest.NewRequest(http.MethodGet, "/api/settings/chat", nil)
	getRec := httptest.NewRecorder()
	handler.HandleGetChatSettings(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", getRec.Code)
	}
	var getResp struct {
		RealtimeStreaming bool `json:"realtime_streaming"`
	}
	if err := json.NewDecoder(getRec.Body).Decode(&getResp); err != nil {
		t.Fatalf("Decode GET: %v", err)
	}
	if !getResp.RealtimeStreaming {
		t.Fatal("GET realtime_streaming = false, want true")
	}

	putReq := httptest.NewRequest(http.MethodPut, "/api/settings/chat", bytes.NewBufferString(`{"realtime_streaming":false}`))
	putRec := httptest.NewRecorder()
	handler.HandleUpdateChatSettings(putRec, putReq)

	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", putRec.Code)
	}
	if app.lastChatSettings.RealtimeStreaming {
		t.Fatal("UpdateChatSettings received realtime_streaming = true, want false")
	}
}

func TestHandleUpdateChatSettingsRejectsInvalidJSON(t *testing.T) {
	app := &fakeAdminApp{}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodPut, "/api/settings/chat", bytes.NewBufferString(`{"realtime_streaming":"yes"}`))
	rec := httptest.NewRecorder()
	handler.HandleUpdateChatSettings(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleCreatePersonaFallsBackToNameAsKey(t *testing.T) {
	app := &fakeAdminApp{}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	body := bytes.NewBufferString(`{"name":"default","description":"desc","tone":"warm"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/personas", body)
	rec := httptest.NewRecorder()
	handler.HandleCreatePersona(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if app.lastPersonaKey != "default" {
		t.Fatalf("lastPersonaKey = %q, want default", app.lastPersonaKey)
	}
	if app.lastPersona == nil || app.lastPersona.Name != "default" {
		t.Fatalf("lastPersona = %#v, want name default", app.lastPersona)
	}
}

func TestHandleListSessions(t *testing.T) {
	app := &fakeAdminApp{
		sessions: []storage.SessionSummary{
			{ID: "session-1", Persona: "default", MessageCount: 2, LastMessage: "hello", UpdatedAt: "2026-03-31T12:00:00Z"},
		},
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/sessions?persona=default&limit=5", nil)
	rec := httptest.NewRecorder()
	handler.HandleListSessions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if app.lastSessionPersona != "default" {
		t.Fatalf("lastSessionPersona = %q, want default", app.lastSessionPersona)
	}
	if app.lastSessionLimit != 5 {
		t.Fatalf("lastSessionLimit = %d, want 5", app.lastSessionLimit)
	}

	var payload struct {
		Sessions []storage.SessionSummary `json:"sessions"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(payload.Sessions) != 1 || payload.Sessions[0].ID != "session-1" {
		t.Fatalf("payload.Sessions = %#v, want session-1", payload.Sessions)
	}
}

func TestHandleDeleteSessionMapsNotFound(t *testing.T) {
	app := &fakeAdminApp{deleteSessionErr: apperrors.ErrSessionNotFound}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/missing", nil)
	req.SetPathValue("id", "missing")
	rec := httptest.NewRecorder()
	handler.HandleDeleteSession(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if app.lastDeleteSessionID != "missing" {
		t.Fatalf("lastDeleteSessionID = %q, want missing", app.lastDeleteSessionID)
	}
}

func TestHandleGetPersonaIncludesWorkProgressPhrases(t *testing.T) {
	app := &fakeAdminApp{
		personas: map[string]*config.Persona{
			"default": {
				Name: "default",
				WorkProgressPhrases: map[string][]string{
					"read_file": {"看看文件"},
				},
			},
		},
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/personas/default", nil)
	req.SetPathValue("name", "default")
	rec := httptest.NewRecorder()
	handler.HandleGetPersona(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	raw, ok := resp["work_progress_phrases"].(map[string]any)
	if !ok {
		t.Fatalf("work_progress_phrases missing: %#v", resp)
	}
	readFile, ok := raw["read_file"].([]any)
	if !ok || len(readFile) != 1 || readFile[0] != "看看文件" {
		t.Fatalf("work_progress_phrases.read_file = %#v, want [看看文件]", raw["read_file"])
	}
}

func TestHandleGetProgressPhrases(t *testing.T) {
	app := &fakeAdminApp{
		progressPhrases: map[string]map[string][]string{
			"default": {
				"read_file": {"看看文件"},
				"_default":  {"处理中"},
			},
		},
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/personas/default/progress-phrases", nil)
	req.SetPathValue("name", "default")
	rec := httptest.NewRecorder()
	handler.HandleGetProgressPhrases(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp map[string]map[string][]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got := resp["phrases"]["read_file"]; len(got) != 1 || got[0] != "看看文件" {
		t.Fatalf("phrases.read_file = %#v, want [看看文件]", got)
	}
}

func TestHandleUpdateProgressPhrases(t *testing.T) {
	app := &fakeAdminApp{}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	body := bytes.NewBufferString(`{"phrases":{"read_file":["看看文件"],"_default":["处理中"]}}`)
	req := httptest.NewRequest(http.MethodPut, "/api/personas/default/progress-phrases", body)
	req.SetPathValue("name", "default")
	rec := httptest.NewRecorder()
	handler.HandleUpdateProgressPhrases(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if app.lastPhrasesKey != "default" {
		t.Fatalf("lastPhrasesKey = %q, want default", app.lastPhrasesKey)
	}
	if got := app.lastPhrasesValue["read_file"]; len(got) != 1 || got[0] != "看看文件" {
		t.Fatalf("lastPhrasesValue.read_file = %#v, want [看看文件]", got)
	}
}

func TestHandleGetDefaultProgressPhrases(t *testing.T) {
	app := &fakeAdminApp{}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/progress-phrases/defaults", nil)
	rec := httptest.NewRecorder()
	handler.HandleGetProgressPhrasesDefaults(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp map[string]map[string][]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(resp["phrases"]) == 0 {
		t.Fatalf("phrases should not be empty: %#v", resp)
	}
}

func TestHandleListSessionApprovals(t *testing.T) {
	app := &fakeAdminApp{
		approvals: []protocol.ApprovalRequest{
			{
				ID:             "approval-1",
				SessionID:      "session-1",
				TaskID:         "task-1",
				Status:         string(protocol.ApprovalStatusPending),
				RejectOptionID: "cancel",
				Options:        []protocol.DecisionOption{{ID: "delete", Summary: "Delete it"}, {ID: "cancel", Summary: "Cancel"}},
				GoalSummary:    "Delete generated files",
				Question:       "Proceed?",
				ExpiresAt:      "2026-04-21T10:00:00Z",
				CreatedAt:      "2026-04-21T09:00:00Z",
				UpdatedAt:      "2026-04-21T09:00:00Z",
			},
		},
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/session-1/approvals", nil)
	req.SetPathValue("id", "session-1")
	rec := httptest.NewRecorder()
	handler.HandleListSessionApprovals(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if app.lastApprovalSession != "session-1" {
		t.Fatalf("lastApprovalSession = %q, want session-1", app.lastApprovalSession)
	}

	var payload struct {
		Approvals []protocol.ApprovalRequest `json:"approvals"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(payload.Approvals) != 1 || payload.Approvals[0].ID != "approval-1" {
		t.Fatalf("payload.Approvals = %#v, want approval-1", payload.Approvals)
	}
}

func TestHandleQueueMemoryExtraction(t *testing.T) {
	app := &fakeAdminApp{
		extractionJobs: []storage.MemoryExtractionJob{{ID: "job-1", SegmentID: "segment-1", Status: storage.MemoryExtractionJobStatusPending}},
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	body := bytes.NewBufferString(`{"session_id":"chat-1","scope":"session","force":true,"mode":"apply"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/memory/extractions", body)
	rec := httptest.NewRecorder()
	handler.HandleQueueMemoryExtraction(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	if app.lastExtractionReq.SessionID != "chat-1" || app.lastExtractionReq.Scope != "session" || !app.lastExtractionReq.Force {
		t.Fatalf("lastExtractionReq = %#v", app.lastExtractionReq)
	}
	var payload MemoryExtractionQueueResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if payload.Status != "queued" || len(payload.Jobs) != 1 || payload.Jobs[0].ID != "job-1" {
		t.Fatalf("payload = %#v, want queued job-1", payload)
	}
}

func TestHandleListMemoryExtractions(t *testing.T) {
	app := &fakeAdminApp{
		extractionJobs: []storage.MemoryExtractionJob{{ID: "job-1", ChatSessionID: "chat-1"}},
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/memory/extractions?session_id=chat-1&limit=5", nil)
	rec := httptest.NewRecorder()
	handler.HandleListMemoryExtractions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if app.lastExtractionList.SessionID != "chat-1" || app.lastExtractionList.Limit != 5 {
		t.Fatalf("lastExtractionList = %#v", app.lastExtractionList)
	}
	var payload struct {
		Jobs []storage.MemoryExtractionJob `json:"jobs"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(payload.Jobs) != 1 || payload.Jobs[0].ID != "job-1" {
		t.Fatalf("payload.Jobs = %#v, want job-1", payload.Jobs)
	}
}

func TestHandleRunNaturalMemoryDryRun(t *testing.T) {
	app := &fakeAdminApp{
		naturalRunResp: memoryhost.NaturalMemoryRunResponse{
			NaturalRun: &memorycore.RunNaturalMemoryCycleResult{
				RunID:     "natural-run-1",
				PersonaID: "default",
				RunKind:   memorycore.NaturalMemoryRunManual,
				DryRun:    true,
				Status:    memorycore.NaturalMemoryRunStatusCompleted,
			},
		},
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	body := bytes.NewBufferString(`{"persona_id":"default","mode":"manual","dry_run":true,"force":false,"explain":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/memory/natural-runs", body)
	rec := httptest.NewRecorder()
	handler.HandleRunNaturalMemory(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if app.lastNaturalReq.PersonaID != "default" || app.lastNaturalReq.Mode != "manual" || !app.lastNaturalReq.DryRun || !app.lastNaturalReq.Explain {
		t.Fatalf("lastNaturalReq = %#v", app.lastNaturalReq)
	}
	var payload memoryhost.NaturalMemoryRunResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if payload.NaturalRun == nil || payload.NaturalRun.RunID != "natural-run-1" || !payload.NaturalRun.DryRun {
		t.Fatalf("payload = %#v, want dry run result", payload)
	}
}

func TestHandleRunNaturalMemoryMirrorSyncFailureReturnsInternalServerError(t *testing.T) {
	app := &fakeAdminApp{naturalRunErr: errors.New("mirror_sync_failed")}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodPost, "/api/memory/natural-runs", bytes.NewBufferString(`{"mode":"manual"}`))
	rec := httptest.NewRecorder()
	handler.HandleRunNaturalMemory(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestHandleLatestNaturalMemoryRun(t *testing.T) {
	app := &fakeAdminApp{
		latestNaturalResp: &memoryhost.NaturalMemoryRunResponse{
			NaturalRun: &memorycore.RunNaturalMemoryCycleResult{
				RunID:  "natural-run-latest",
				Status: memorycore.NaturalMemoryRunStatusCompleted,
			},
		},
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/memory/natural-runs/latest", nil)
	rec := httptest.NewRecorder()
	handler.HandleLatestNaturalMemoryRun(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var payload memoryhost.NaturalMemoryRunResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if payload.NaturalRun == nil || payload.NaturalRun.RunID != "natural-run-latest" {
		t.Fatalf("payload = %#v, want latest natural run", payload)
	}
}

func TestHandleListMemorySegments(t *testing.T) {
	app := &fakeAdminApp{
		memorySegments: []storage.MemorySegment{{ID: "segment-1", ChatSessionID: "chat-1"}},
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/api/memory/segments?session_id=chat-1", nil)
	rec := httptest.NewRecorder()
	handler.HandleListMemorySegments(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if app.lastSegmentSession != "chat-1" {
		t.Fatalf("lastSegmentSession = %q, want chat-1", app.lastSegmentSession)
	}
	var payload struct {
		Segments []storage.MemorySegment `json:"segments"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(payload.Segments) != 1 || payload.Segments[0].ID != "segment-1" {
		t.Fatalf("payload.Segments = %#v, want segment-1", payload.Segments)
	}
}
