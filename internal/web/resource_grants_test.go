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

type fakeResourceGrantAdminApp struct {
	fakeAdminApp
	grants      []resource.GrantEnvelope
	listFilter  resource.GrantListFilter
	revokedID   string
	revokeGrant resource.GrantEnvelope
	revokeErr   error
}

func (f *fakeResourceGrantAdminApp) ListResourceGrants(_ context.Context, filter resource.GrantListFilter) ([]resource.GrantEnvelope, error) {
	f.listFilter = filter
	return f.grants, nil
}

func (f *fakeResourceGrantAdminApp) RevokeResourceGrant(_ context.Context, id string) (resource.GrantEnvelope, error) {
	f.revokedID = id
	return f.revokeGrant, f.revokeErr
}

func TestHandleListResourceGrants(t *testing.T) {
	createdAt := time.Date(2026, 6, 20, 1, 2, 3, 0, time.UTC)
	app := &fakeResourceGrantAdminApp{
		grants: []resource.GrantEnvelope{{
			ID:          "grant-1",
			Principal:   resource.PrincipalRef{Kind: resource.PrincipalWorkTask, ID: "task-1"},
			Capability:  resource.CapabilityHostFSRead,
			Resource:    resource.ResourceSelector{Kind: resource.ResourceSelectorAlias, Path: "@documents/note.txt"},
			Operations:  []string{resource.OperationRead},
			Status:      resource.GrantStatusActive,
			Lifetime:    resource.GrantLifetimeOnce,
			BindingHash: "sha256:binding",
			IssuedBy:    resource.GrantIssuedByUser,
			CreatedAt:   createdAt,
		}},
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodGet, "/api/resource-grants?status=active&capability=host.fs.read&principal_kind=work_task&principal_id=task-1", nil)
	rec := httptest.NewRecorder()

	handler.HandleListResourceGrants(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if app.listFilter.Status != resource.GrantStatusActive || app.listFilter.Capability != resource.CapabilityHostFSRead {
		t.Fatalf("filter = %#v", app.listFilter)
	}
	if app.listFilter.Principal == nil || *app.listFilter.Principal != (resource.PrincipalRef{Kind: resource.PrincipalWorkTask, ID: "task-1"}) {
		t.Fatalf("principal filter = %#v", app.listFilter.Principal)
	}
	var body struct {
		Grants []resource.GrantEnvelope `json:"grants"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Grants) != 1 || body.Grants[0].ID != "grant-1" {
		t.Fatalf("body = %#v", body)
	}
}

func TestHandleRevokeResourceGrant(t *testing.T) {
	app := &fakeResourceGrantAdminApp{
		revokeGrant: resource.GrantEnvelope{
			ID:          "grant-1",
			Principal:   resource.PrincipalRef{Kind: resource.PrincipalWorkTask, ID: "task-1"},
			Capability:  resource.CapabilityHostFSRead,
			Status:      resource.GrantStatusRevoked,
			Lifetime:    resource.GrantLifetimeOnce,
			BindingHash: "sha256:binding",
			IssuedBy:    resource.GrantIssuedByUser,
			CreatedAt:   time.Now().UTC(),
		},
	}
	handler := NewAPIHandler(app, slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodPost, "/api/resource-grants/grant-1/revoke", nil)
	req.SetPathValue("id", "grant-1")
	rec := httptest.NewRecorder()

	handler.HandleRevokeResourceGrant(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if app.revokedID != "grant-1" {
		t.Fatalf("revokedID = %q", app.revokedID)
	}
}

func TestHandleResourceGrantsUnavailable(t *testing.T) {
	handler := NewAPIHandler(&fakeAdminApp{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodGet, "/api/resource-grants", nil)
	rec := httptest.NewRecorder()

	handler.HandleListResourceGrants(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d body=%s, want 501", rec.Code, rec.Body.String())
	}
}
