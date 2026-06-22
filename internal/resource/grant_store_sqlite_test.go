package resource

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/storage"
)

func newTestGrantStore(t *testing.T) *SQLiteGrantStore {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "emo.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewSQLiteGrantStore(db.SqlDB())
}

func testGrant(now time.Time) GrantEnvelope {
	return GrantEnvelope{
		ID:         "grant-1",
		Principal:  PrincipalRef{Kind: PrincipalWorkTask, ID: "task-1"},
		Capability: CapabilityHostFSRead,
		Resource: ResourceSelector{
			Kind:        ResourceSelectorAlias,
			DisplayPath: "@documents/note.txt",
			Path:        "@documents/note.txt",
		},
		Operations:  []string{OperationRead},
		Constraints: GrantConstraints{MaxBytes: 1024},
		Lifetime:    GrantLifetimeOnce,
		Status:      GrantStatusActive,
		BindingHash: "sha256:binding",
		IssuedBy:    GrantIssuedByPolicy,
		CreatedAt:   now,
	}
}

func TestSQLiteGrantStoreCreateGetList(t *testing.T) {
	store := newTestGrantStore(t)
	now := time.Unix(100, 0).UTC()
	grant, err := store.Create(t.Context(), testGrant(now))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if grant.ID != "grant-1" {
		t.Fatalf("grant ID = %q", grant.ID)
	}
	got, ok, err := store.Get(t.Context(), "grant-1")
	if err != nil || !ok {
		t.Fatalf("Get = %#v %v %v", got, ok, err)
	}
	if got.Resource.DisplayPath != "@documents/note.txt" || got.Operations[0] != OperationRead || got.Constraints.MaxBytes != 1024 {
		t.Fatalf("round trip grant = %#v", got)
	}
	list, err := store.List(t.Context(), GrantListFilter{
		Principal:  &PrincipalRef{Kind: PrincipalWorkTask, ID: "task-1"},
		Status:     GrantStatusActive,
		Capability: CapabilityHostFSRead,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != "grant-1" {
		t.Fatalf("List = %#v", list)
	}
	if eventCount(t, store) != 1 {
		t.Fatalf("event count = %d, want 1", eventCount(t, store))
	}
}

func TestSQLiteGrantStoreConsumeRevokeExpire(t *testing.T) {
	store := newTestGrantStore(t)
	now := time.Unix(200, 0).UTC()
	grant := testGrant(now)
	expires := now.Add(-time.Second)
	grant.ExpiresAt = &expires
	if _, err := store.Create(t.Context(), grant); err != nil {
		t.Fatalf("Create: %v", err)
	}

	expired, err := store.Expire(t.Context(), now)
	if err != nil {
		t.Fatalf("Expire: %v", err)
	}
	if expired != 1 {
		t.Fatalf("expired = %d, want 1", expired)
	}
	if _, err := store.Consume(t.Context(), "grant-1", grant.Principal); err == nil {
		t.Fatal("Consume expired grant should fail")
	}

	grant2 := testGrant(now)
	grant2.ID = "grant-2"
	grant2.ExpiresAt = nil
	if _, err := store.Create(t.Context(), grant2); err != nil {
		t.Fatalf("Create grant2: %v", err)
	}
	if _, err := store.Consume(t.Context(), "grant-2", PrincipalRef{Kind: PrincipalWorkTask, ID: "other"}); err == nil {
		t.Fatal("Consume with mismatched principal should fail")
	}
	consumed, err := store.Consume(t.Context(), "grant-2", grant2.Principal)
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if consumed.Status != GrantStatusConsumed {
		t.Fatalf("status = %q, want consumed", consumed.Status)
	}
	if _, err := store.Consume(t.Context(), "grant-2", grant2.Principal); err == nil {
		t.Fatal("Consume twice should fail")
	}

	grant3 := testGrant(now)
	grant3.ID = "grant-3"
	if _, err := store.Create(t.Context(), grant3); err != nil {
		t.Fatalf("Create grant3: %v", err)
	}
	revoked, err := store.Revoke(t.Context(), "grant-3", grant3.Principal)
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if revoked.Status != GrantStatusRevoked {
		t.Fatalf("status = %q, want revoked", revoked.Status)
	}
	if eventCount(t, store) < 5 {
		t.Fatalf("event count = %d, want audit events", eventCount(t, store))
	}
}

func eventCount(t *testing.T, store *SQLiteGrantStore) int {
	t.Helper()
	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM resource_grant_events").Scan(&count); err != nil {
		t.Fatalf("count events: %v", err)
	}
	return count
}
