package web

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/promptcenter"
)

type promptAPIFakeApp struct {
	fakeAdminApp
	componentsResp  promptcenter.PromptComponentsResponse
	componentDetail promptcenter.PromptComponentDetail
	upsertResp      promptcenter.UpsertOverrideResponse
	previewResp     promptcenter.PromptPreviewResponse
	snapshotsResp   promptcenter.PromptSnapshotListResponse
	snapshotDetail  promptcenter.PromptSnapshotDetail
	lastAgentID     string
	lastUpsert      promptcenter.UpsertOverrideRequest
	lastDelete      promptcenter.DeleteOverrideRequest
	lastPreview     promptcenter.PromptPreviewRequest
}

func (f *promptAPIFakeApp) ListPromptComponents(_ context.Context, agentID string) (promptcenter.PromptComponentsResponse, error) {
	f.lastAgentID = agentID
	return f.componentsResp, nil
}

func (f *promptAPIFakeApp) GetPromptComponent(_ context.Context, id, agentID string) (promptcenter.PromptComponentDetail, error) {
	f.lastAgentID = agentID
	if f.componentDetail.ID == "" {
		f.componentDetail.ID = id
	}
	return f.componentDetail, nil
}

func (f *promptAPIFakeApp) UpsertPromptOverride(_ context.Context, req promptcenter.UpsertOverrideRequest) (promptcenter.UpsertOverrideResponse, error) {
	f.lastUpsert = req
	if !f.upsertResp.OK {
		f.upsertResp.OK = true
	}
	return f.upsertResp, nil
}

func (f *promptAPIFakeApp) DeletePromptOverride(_ context.Context, req promptcenter.DeleteOverrideRequest) error {
	f.lastDelete = req
	return nil
}

func (f *promptAPIFakeApp) PreviewPrompt(_ context.Context, req promptcenter.PromptPreviewRequest) (promptcenter.PromptPreviewResponse, error) {
	f.lastPreview = req
	return f.previewResp, nil
}

func (f *promptAPIFakeApp) ListPromptSnapshots(_ context.Context, req promptcenter.PromptSnapshotListRequest) (promptcenter.PromptSnapshotListResponse, error) {
	f.lastAgentID = req.AgentID
	return f.snapshotsResp, nil
}

func (f *promptAPIFakeApp) GetPromptSnapshot(_ context.Context, id string) (promptcenter.PromptSnapshotDetail, error) {
	if f.snapshotDetail.ID == "" {
		f.snapshotDetail.ID = id
	}
	return f.snapshotDetail, nil
}

func TestPromptComponentsHandlers(t *testing.T) {
	app := &promptAPIFakeApp{
		componentsResp: promptcenter.PromptComponentsResponse{
			AgentID: "agent-a",
			Components: []promptcenter.PromptComponentDetail{
				{
					ID:              promptcenter.ComponentEmotionOperatingContract,
					Group:           "emotion",
					DefaultText:     "default",
					EffectiveText:   "agent custom",
					EffectiveSource: promptcenter.SourceAgentOverride,
					DefaultHash:     "default-hash",
					EffectiveHash:   "effective-hash",
				},
			},
		},
		componentDetail: promptcenter.PromptComponentDetail{
			ID:              promptcenter.ComponentEmotionOperatingContract,
			DefaultText:     "default",
			EffectiveText:   "global",
			EffectiveSource: promptcenter.SourceGlobalOverride,
		},
	}
	handler := NewAPIHandler(app, slog.Default())

	listReq := httptest.NewRequest(http.MethodGet, "/api/prompts/components?agent_id=agent-a", nil)
	listRR := httptest.NewRecorder()
	handler.HandleListPromptComponents(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRR.Code, listRR.Body.String())
	}
	if app.lastAgentID != "agent-a" {
		t.Fatalf("lastAgentID = %q", app.lastAgentID)
	}
	if !strings.Contains(listRR.Body.String(), `"effective_source":"agent_override"`) {
		t.Fatalf("list body missing effective_source: %s", listRR.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/prompts/components/"+promptcenter.ComponentEmotionOperatingContract+"?agent_id=agent-a", nil)
	getReq.SetPathValue("component_id", promptcenter.ComponentEmotionOperatingContract)
	getRR := httptest.NewRecorder()
	handler.HandleGetPromptComponent(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", getRR.Code, getRR.Body.String())
	}
	if !strings.Contains(getRR.Body.String(), `"effective_source":"global_override"`) {
		t.Fatalf("get body missing effective_source: %s", getRR.Body.String())
	}
}

func TestPromptOverrideHandlers(t *testing.T) {
	app := &promptAPIFakeApp{upsertResp: promptcenter.UpsertOverrideResponse{
		OK:       true,
		Warnings: []promptcenter.PromptLintWarning{{ComponentID: promptcenter.ComponentEmotionOperatingContract, Code: "missing_json_only", Severity: "warning", Message: "missing"}},
	}}
	handler := NewAPIHandler(app, slog.Default())

	body := bytes.NewBufferString(`{
		"component_id":"emotion.operating_contract",
		"scope_type":"agent",
		"scope_id":"agent-a",
		"mode":"use_default",
		"override_text":"",
		"enabled":true
	}`)
	upsertReq := httptest.NewRequest(http.MethodPut, "/api/prompts/overrides", body)
	upsertRR := httptest.NewRecorder()
	handler.HandleUpsertPromptOverride(upsertRR, upsertReq)
	if upsertRR.Code != http.StatusOK {
		t.Fatalf("upsert status = %d body=%s", upsertRR.Code, upsertRR.Body.String())
	}
	if app.lastUpsert.ComponentID != promptcenter.ComponentEmotionOperatingContract || app.lastUpsert.Mode != promptcenter.OverrideModeUseDefault || app.lastUpsert.ScopeID != "agent-a" {
		t.Fatalf("lastUpsert = %#v", app.lastUpsert)
	}
	if !strings.Contains(upsertRR.Body.String(), `"warnings"`) {
		t.Fatalf("upsert body missing warnings: %s", upsertRR.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/prompts/overrides?component_id=emotion.operating_contract&scope_type=agent&scope_id=agent-a", nil)
	deleteRR := httptest.NewRecorder()
	handler.HandleDeletePromptOverride(deleteRR, deleteReq)
	if deleteRR.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", deleteRR.Code, deleteRR.Body.String())
	}
	if app.lastDelete.ComponentID != promptcenter.ComponentEmotionOperatingContract || app.lastDelete.ScopeType != promptcenter.ScopeAgent || app.lastDelete.ScopeID != "agent-a" {
		t.Fatalf("lastDelete = %#v", app.lastDelete)
	}
}

func TestPromptPreviewAndSnapshotHandlers(t *testing.T) {
	app := &promptAPIFakeApp{
		previewResp: promptcenter.PromptPreviewResponse{
			AgentID:      "agent-a",
			Purpose:      "emotion_chat",
			RenderedText: "rendered",
			FinalHash:    "hash",
			Components: []promptcenter.RenderComponent{
				{ComponentID: promptcenter.ComponentEmotionOperatingContract, Source: promptcenter.SourceGlobalOverride},
			},
			Warnings: []promptcenter.PromptPreviewWarning{{Code: "no_session", Severity: "info", Message: "no session"}},
		},
		snapshotsResp: promptcenter.PromptSnapshotListResponse{
			Snapshots: []promptcenter.RenderSnapshotSummary{{ID: "snap-1", AgentID: "agent-a", Purpose: "emotion_chat"}},
		},
		snapshotDetail: promptcenter.PromptSnapshotDetail{
			RenderSnapshot: promptcenter.RenderSnapshot{ID: "snap-1", AgentID: "agent-a", Purpose: "emotion_chat", RenderedText: "system"},
		},
	}
	handler := NewAPIHandler(app, slog.Default())

	previewReq := httptest.NewRequest(http.MethodPost, "/api/prompts/preview", bytes.NewBufferString(`{
		"agent_id":"agent-a",
		"purpose":"emotion_chat",
		"mode":"full",
		"session_id":"session-1",
		"user_message":"hello",
		"component_ids":["emotion.operating_contract"],
		"include_memory":true,
		"include_agent_affect":true
	}`))
	previewRR := httptest.NewRecorder()
	handler.HandlePreviewPrompt(previewRR, previewReq)
	if previewRR.Code != http.StatusOK {
		t.Fatalf("preview status = %d body=%s", previewRR.Code, previewRR.Body.String())
	}
	if app.lastPreview.AgentID != "agent-a" || app.lastPreview.Mode != "full" || app.lastPreview.SessionID != "session-1" || app.lastPreview.UserMessage != "hello" || !app.lastPreview.IncludeMemory || !app.lastPreview.IncludeAgentAffect || len(app.lastPreview.ComponentIDs) != 1 {
		t.Fatalf("lastPreview = %#v", app.lastPreview)
	}
	if !strings.Contains(previewRR.Body.String(), `"warnings"`) {
		t.Fatalf("preview body missing warnings: %s", previewRR.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/prompts/snapshots?agent_id=agent-a&purpose=emotion_chat&limit=5", nil)
	listRR := httptest.NewRecorder()
	handler.HandleListPromptSnapshots(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("list snapshots status = %d body=%s", listRR.Code, listRR.Body.String())
	}
	var listBody promptcenter.PromptSnapshotListResponse
	if err := json.Unmarshal(listRR.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode list body: %v", err)
	}
	if len(listBody.Snapshots) != 1 || listBody.Snapshots[0].ID != "snap-1" {
		t.Fatalf("listBody = %#v", listBody)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/prompts/snapshots/snap-1", nil)
	getReq.SetPathValue("id", "snap-1")
	getRR := httptest.NewRecorder()
	handler.HandleGetPromptSnapshot(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get snapshot status = %d body=%s", getRR.Code, getRR.Body.String())
	}
	if !strings.Contains(getRR.Body.String(), `"rendered_text":"system"`) {
		t.Fatalf("get snapshot body = %s", getRR.Body.String())
	}
}
