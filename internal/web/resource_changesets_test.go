package web

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/resource"
)

type fakeResourceChangeSetAdminApp struct {
	fakeAdminApp
	statuses   []resource.ChangeSetStatus
	changesets []resource.ChangeSet
	gotID      string
	cancelID   string
}

func (f *fakeResourceChangeSetAdminApp) ListHostResourceChangeSets(_ context.Context, statuses []resource.ChangeSetStatus) ([]resource.ChangeSet, error) {
	f.statuses = statuses
	return f.changesets, nil
}

func (f *fakeResourceChangeSetAdminApp) GetHostResourceChangeSet(_ context.Context, id string) (resource.ChangeSet, error) {
	f.gotID = id
	return f.changesets[0], nil
}

func (f *fakeResourceChangeSetAdminApp) CancelHostResourceChangeSet(_ context.Context, id string) (resource.ChangeSet, error) {
	f.cancelID = id
	cs := f.changesets[0]
	cs.Status = resource.ChangeSetStatusCancelled
	return cs, nil
}

func TestHandleListResourceChangeSets(t *testing.T) {
	app := &fakeResourceChangeSetAdminApp{
		changesets: []resource.ChangeSet{sampleChangeSet("cs-1")},
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodGet, "/api/resource-changesets?status=approval_pending,conflict", nil)
	rec := httptest.NewRecorder()

	handler.HandleListResourceChangeSets(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if len(app.statuses) != 2 || app.statuses[0] != resource.ChangeSetStatusApprovalPending || app.statuses[1] != resource.ChangeSetStatusConflict {
		t.Fatalf("statuses = %#v", app.statuses)
	}
	var body struct {
		ChangeSets []resource.ChangeSet `json:"changesets"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.ChangeSets) != 1 || body.ChangeSets[0].ID != "cs-1" {
		t.Fatalf("body = %#v", body)
	}
}

func TestHandleGetAndCancelResourceChangeSet(t *testing.T) {
	app := &fakeResourceChangeSetAdminApp{changesets: []resource.ChangeSet{sampleChangeSet("cs-1")}}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))

	getReq := httptest.NewRequest(http.MethodGet, "/api/resource-changesets/cs-1", nil)
	getReq.SetPathValue("id", "cs-1")
	getRec := httptest.NewRecorder()
	handler.HandleGetResourceChangeSet(getRec, getReq)
	if getRec.Code != http.StatusOK || app.gotID != "cs-1" {
		t.Fatalf("get status=%d id=%q body=%s", getRec.Code, app.gotID, getRec.Body.String())
	}

	cancelReq := httptest.NewRequest(http.MethodPost, "/api/resource-changesets/cs-1/cancel", nil)
	cancelReq.SetPathValue("id", "cs-1")
	cancelRec := httptest.NewRecorder()
	handler.HandleCancelResourceChangeSet(cancelRec, cancelReq)
	if cancelRec.Code != http.StatusOK || app.cancelID != "cs-1" {
		t.Fatalf("cancel status=%d id=%q body=%s", cancelRec.Code, app.cancelID, cancelRec.Body.String())
	}
}

func TestHandleResourceChangeSetsUnavailable(t *testing.T) {
	handler := NewAPIHandler(&fakeAdminApp{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodGet, "/api/resource-changesets", nil)
	rec := httptest.NewRecorder()

	handler.HandleListResourceChangeSets(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d body=%s, want 501", rec.Code, rec.Body.String())
	}
}

func sampleChangeSet(id string) resource.ChangeSet {
	return resource.ChangeSet{
		ID:        id,
		Status:    resource.ChangeSetStatusApprovalPending,
		Operation: resource.ChangeOpOverwriteFile,
		PlanHash:  "sha256:plan",
		Preview:   resource.ChangePreview{Summary: "overwrite @documents/note.txt"},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
}
