package conversation

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/longyisang/emoagent/internal/storage"
)

func TestBindingServiceBindsLatestNonEmptySessionWhenMissing(t *testing.T) {
	ctx := context.Background()
	db := conversationTestDB(t)
	if err := db.CreateSession(ctx, "empty-session", "default"); err != nil {
		t.Fatalf("CreateSession(empty): %v", err)
	}
	if err := db.CreateSession(ctx, "latest-session", "default"); err != nil {
		t.Fatalf("CreateSession(latest): %v", err)
	}
	if err := db.AddMessage(ctx, "msg-1", "latest-session", "user", "hello"); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	service := NewBindingService(db, nil)
	binding, err := service.EnsureCurrent(ctx, Origin{OriginKey: "webui:local:main", SourceType: "webui", ChannelType: "web"}, "default")
	if err != nil {
		t.Fatalf("EnsureCurrent: %v", err)
	}
	if binding.SessionID != "latest-session" || binding.IsNew {
		t.Fatalf("binding = %#v, want existing latest-session", binding)
	}
}

func TestBindingServiceCreatesSessionWhenNoBindingOrHistory(t *testing.T) {
	ctx := context.Background()
	db := conversationTestDB(t)
	starter := &fakeSessionStarter{db: db, sessionID: "created-session"}

	service := NewBindingService(db, starter)
	binding, err := service.EnsureCurrent(ctx, Origin{OriginKey: "webui:local:main", SourceType: "webui", ChannelType: "web"}, "default")
	if err != nil {
		t.Fatalf("EnsureCurrent: %v", err)
	}
	if binding.SessionID != "created-session" || !binding.IsNew {
		t.Fatalf("binding = %#v, want newly created session", binding)
	}
	if starter.calls != 1 {
		t.Fatalf("starter calls = %d, want 1", starter.calls)
	}
}

func TestTimelineEventStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := conversationTestDB(t)
	if err := db.CreateSession(ctx, "session-1", "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	store := NewTimelineEventStore(db)
	if err := store.Append(ctx, TimelineEvent{
		ID:             "event-1",
		OriginKey:      "webui:local:main",
		SessionID:      "session-1",
		PersonaKey:     "default",
		Type:           "command_result",
		VisibleContent: "ok",
		PayloadJSON:    `{}`,
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	events, err := store.List(ctx, "session-1", "webui:local:main", 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(events) != 1 || events[0].ID != "event-1" || events[0].Type != "command_result" {
		t.Fatalf("events = %#v, want command_result", events)
	}
}

type fakeSessionStarter struct {
	db        *storage.DB
	sessionID string
	calls     int
}

func (f *fakeSessionStarter) StartSession(ctx context.Context, personaKey string) (string, error) {
	f.calls++
	return f.sessionID, f.db.CreateSession(ctx, f.sessionID, personaKey)
}

func conversationTestDB(t *testing.T) *storage.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := storage.OpenWithOptions(path, logger, storage.StorageOptions{Timezone: "Asia/Shanghai"})
	if err != nil {
		t.Fatalf("OpenWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
