package resource

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/config"
)

func TestBrokerReadsConfiguredRootAndRejectsUnconfiguredPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "outside.txt")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}

	broker, err := NewBroker(config.HostResourcesConfig{
		Enabled: true,
		Roots: []config.HostResourceRoot{{
			ID:        "documents",
			Path:      root,
			Access:    "read",
			Recursive: true,
		}},
		MaxReadBytes:     1024,
		MaxSearchResults: 100,
		ProtectedPolicy:  "default",
	})
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}

	data, ref, err := broker.Read(context.Background(), ResourceSelector{Kind: ResourceSelectorAlias, Path: "@documents/note.txt"}, ReadOptions{MaxBytes: 1024})
	if err != nil {
		t.Fatalf("Read configured root: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("data = %q, want hello", data)
	}
	if ref.RootID != "documents" || ref.DisplayPath != "@documents/note.txt" || ref.CanonicalPathHash == "" {
		t.Fatalf("ref = %#v", ref)
	}

	if _, _, err := broker.Read(context.Background(), ResourceSelector{Kind: ResourceSelectorPath, Path: outsideFile}, ReadOptions{MaxBytes: 1024}); err == nil {
		t.Fatal("Read should reject unconfigured external path")
	}
}

func TestBrokerRejectsProtectedPathInsideConfiguredRoot(t *testing.T) {
	root := t.TempDir()
	sshDir := filepath.Join(root, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	broker, err := NewBroker(config.HostResourcesConfig{
		Enabled: true,
		Roots: []config.HostResourceRoot{{
			ID:        "home",
			Path:      root,
			Access:    "read",
			Recursive: true,
		}},
		MaxReadBytes:     1024,
		MaxSearchResults: 100,
		ProtectedPolicy:  "default",
	})
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}

	if _, _, err := broker.Read(context.Background(), ResourceSelector{Kind: ResourceSelectorAlias, Path: "@home/.ssh/config"}, ReadOptions{MaxBytes: 1024}); err == nil {
		t.Fatal("Read should hard-deny protected .ssh path")
	}
}

func TestBrokerMostSpecificDenyRootOverridesBroadReadRoot(t *testing.T) {
	home := t.TempDir()
	privateDir := filepath.Join(home, "private")
	if err := os.MkdirAll(privateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(privateDir, "note.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	broker, err := NewBroker(config.HostResourcesConfig{
		Enabled: true,
		Roots: []config.HostResourceRoot{
			{ID: "home", Path: home, Access: "read", Recursive: true},
			{ID: "private", Path: privateDir, Access: "deny", Recursive: true},
		},
		MaxReadBytes:     1024,
		MaxSearchResults: 100,
		ProtectedPolicy:  "default",
	})
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}
	if _, _, err := broker.Read(context.Background(), ResourceSelector{Kind: ResourceSelectorAlias, Path: "@home/private/note.txt"}, ReadOptions{MaxBytes: 1024}); err == nil {
		t.Fatal("Read should reject path covered by more specific deny root")
	}
}

func TestBrokerListSkipsMoreSpecificDenyAndAskRoots(t *testing.T) {
	home := t.TempDir()
	privateDir := filepath.Join(home, "private")
	askDir := filepath.Join(home, "ask")
	if err := os.MkdirAll(privateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(askDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(privateDir, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(askDir, "pending.txt"), []byte("pending"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "public.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	broker, err := NewBroker(config.HostResourcesConfig{
		Enabled: true,
		Roots: []config.HostResourceRoot{
			{ID: "home", Path: home, Access: "read", Recursive: true},
			{ID: "private", Path: privateDir, Access: "deny", Recursive: true},
			{ID: "ask", Path: askDir, Access: "ask", Recursive: true},
		},
		MaxReadBytes:     1024,
		MaxSearchResults: 100,
		ProtectedPolicy:  "default",
	})
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}

	entries, _, _, err := broker.List(context.Background(), ResourceSelector{Kind: ResourceSelectorAlias, Path: "@home"}, ListOptions{Recursive: true, MaxEntries: 100})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, entry := range entries {
		switch entry.Name {
		case "private", "secret.txt", "ask", "pending.txt":
			t.Fatalf("unapproved child root leaked from list: %#v", entry)
		}
	}
	results, _, _, err := broker.Search(context.Background(), ResourceSelector{Kind: ResourceSelectorAlias, Path: "@home"}, SearchOptions{Query: "secret", MaxResults: 100})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("unapproved child root leaked from search: %#v", results)
	}
}

func TestBrokerAskRootDoesNotReadWithoutGrant(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	broker, err := NewBroker(config.HostResourcesConfig{
		Enabled:          true,
		Roots:            []config.HostResourceRoot{{ID: "documents", Path: root, Access: "ask", Recursive: true}},
		MaxReadBytes:     1024,
		MaxSearchResults: 100,
		ProtectedPolicy:  "default",
	})
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}
	if _, _, err := broker.Read(context.Background(), ResourceSelector{Kind: ResourceSelectorAlias, Path: "@documents/note.txt"}, ReadOptions{MaxBytes: 1024}); err == nil {
		t.Fatal("Read should require grant/approval for ask root")
	}
}

func TestBrokerAskRootReadsWithMatchingGrant(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	principal := PrincipalRef{Kind: PrincipalWorkTask, ID: "task-1"}
	store := &fakeGrantStore{grant: GrantEnvelope{
		ID:         "grant-1",
		Principal:  principal,
		Capability: CapabilityHostFSRead,
		Resource: ResourceSelector{
			Kind:        ResourceSelectorAlias,
			DisplayPath: "@documents/note.txt",
			Path:        "@documents/note.txt",
		},
		Operations:  []string{OperationRead},
		Constraints: GrantConstraints{},
		Lifetime:    GrantLifetimeOnce,
		Status:      GrantStatusActive,
		BindingHash: "sha256:binding",
		CreatedAt:   time.Now().UTC(),
	}}
	broker, err := NewBrokerWithGrantStore(config.HostResourcesConfig{
		Enabled:          true,
		Roots:            []config.HostResourceRoot{{ID: "documents", Path: root, Access: "ask", Recursive: true}},
		MaxReadBytes:     1024,
		MaxSearchResults: 100,
		ProtectedPolicy:  "default",
	}, store)
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}
	data, _, err := broker.Read(WithGrant(context.Background(), GrantContext{ID: "grant-1", Principal: principal}), ResourceSelector{Kind: ResourceSelectorAlias, Path: "@documents/note.txt"}, ReadOptions{MaxBytes: 1024})
	if err != nil {
		t.Fatalf("Read with grant: %v", err)
	}
	if string(data) != "hello" || store.consumed != 1 {
		t.Fatalf("data=%q consumed=%d", data, store.consumed)
	}
}

func TestBrokerListAndSearchDoNotReturnProtectedPaths(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".ssh", "config"), []byte("host *"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "public.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	broker, err := NewBroker(config.HostResourcesConfig{
		Enabled:          true,
		Roots:            []config.HostResourceRoot{{ID: "home", Path: root, Access: "read", Recursive: true}},
		MaxReadBytes:     1024,
		MaxSearchResults: 100,
		ProtectedPolicy:  "default",
	})
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}
	entries, _, _, err := broker.List(context.Background(), ResourceSelector{Kind: ResourceSelectorAlias, Path: "@home"}, ListOptions{Recursive: true, MaxEntries: 100})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, entry := range entries {
		if entry.Name == ".ssh" || entry.Name == "config" || entry.Ref.DisplayPath == "@home/.ssh/config" {
			t.Fatalf("protected entry leaked from list: %#v", entry)
		}
	}
	results, _, _, err := broker.Search(context.Background(), ResourceSelector{Kind: ResourceSelectorAlias, Path: "@home"}, SearchOptions{Query: "config", MaxResults: 100})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("protected search result leaked: %#v", results)
	}
}

func TestBrokerRecursiveFalseRootDoesNotSearchDeepFiles(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "deep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	broker, err := NewBroker(config.HostResourcesConfig{
		Enabled:          true,
		Roots:            []config.HostResourceRoot{{ID: "root", Path: root, Access: "read", Recursive: false}},
		MaxReadBytes:     1024,
		MaxSearchResults: 100,
		ProtectedPolicy:  "default",
	})
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}
	entries, _, _, err := broker.List(context.Background(), ResourceSelector{Kind: ResourceSelectorAlias, Path: "@root"}, ListOptions{Recursive: true, MaxEntries: 100})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, entry := range entries {
		if entry.Name == "deep.txt" {
			t.Fatalf("recursive false root leaked deep file: %#v", entries)
		}
	}
	results, _, _, err := broker.Search(context.Background(), ResourceSelector{Kind: ResourceSelectorAlias, Path: "@root"}, SearchOptions{Query: "deep", MaxResults: 100})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("recursive false root search leaked deep file: %#v", results)
	}
}

func TestBrokerCopyToWorkspaceRejectsSymlinkTargetEscape(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "note.txt")
	if err := os.WriteFile(source, []byte("safe"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "link.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	broker, err := NewBroker(config.HostResourcesConfig{
		Enabled:          true,
		Roots:            []config.HostResourceRoot{{ID: "documents", Path: root, Access: "read", Recursive: true}},
		MaxReadBytes:     1024,
		MaxSearchResults: 100,
		ProtectedPolicy:  "default",
	})
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}
	if _, _, err := broker.CopyToWorkspace(context.Background(), ResourceSelector{Kind: ResourceSelectorAlias, Path: "@documents/note.txt"}, workspace, "link.txt", CopyOptions{MaxBytes: 1024}); err == nil {
		t.Fatal("CopyToWorkspace should reject symlink target")
	}
	data, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "outside" {
		t.Fatalf("outside file was modified: %q", data)
	}
}

type fakeGrantStore struct {
	grant    GrantEnvelope
	consumed int
}

func (s *fakeGrantStore) Create(context.Context, GrantEnvelope) (GrantEnvelope, error) {
	return GrantEnvelope{}, nil
}

func (s *fakeGrantStore) Get(context.Context, string) (GrantEnvelope, bool, error) {
	return s.grant, true, nil
}

func (s *fakeGrantStore) List(context.Context, GrantListFilter) ([]GrantEnvelope, error) {
	return []GrantEnvelope{s.grant}, nil
}

func (s *fakeGrantStore) Consume(_ context.Context, id string, principal PrincipalRef) (GrantEnvelope, error) {
	if id != s.grant.ID || principal != s.grant.Principal || s.grant.Status != GrantStatusActive {
		return GrantEnvelope{}, os.ErrPermission
	}
	s.consumed++
	return s.grant, nil
}

func (s *fakeGrantStore) Revoke(context.Context, string, PrincipalRef) (GrantEnvelope, error) {
	return GrantEnvelope{}, nil
}

func (s *fakeGrantStore) Expire(context.Context, time.Time) (int, error) {
	return 0, nil
}
