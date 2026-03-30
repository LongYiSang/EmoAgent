package storage

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSessionCRUD(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	sessionID := "session-1"
	if err := db.CreateSession(ctx, sessionID, "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	session, err := db.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if session == nil {
		t.Fatal("GetSession returned nil")
	}
	if session.ID != sessionID {
		t.Fatalf("session.ID = %q, want %q", session.ID, sessionID)
	}
	if session.Persona != "default" {
		t.Fatalf("session.Persona = %q, want default", session.Persona)
	}
	if session.CreatedAt == "" || session.UpdatedAt == "" {
		t.Fatalf("session timestamps should not be empty: %#v", session)
	}
}

func TestGetSessionNotFound(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	session, err := db.GetSession(ctx, "missing")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if session != nil {
		t.Fatalf("GetSession = %#v, want nil", session)
	}
}

func TestGetRecentMessagesReturnsAscendingOrder(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	sessionID := "session-2"
	if err := db.CreateSession(ctx, sessionID, "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	messages := []struct {
		id      string
		role    string
		content string
	}{
		{id: "msg-1", role: "user", content: "hello"},
		{id: "msg-2", role: "assistant", content: "hi"},
		{id: "msg-3", role: "user", content: "how are you"},
	}

	for _, msg := range messages {
		if err := db.AddMessage(ctx, msg.id, sessionID, msg.role, msg.content); err != nil {
			t.Fatalf("AddMessage(%s): %v", msg.id, err)
		}
		time.Sleep(time.Millisecond)
	}

	got, err := db.GetRecentMessages(ctx, sessionID, 10)
	if err != nil {
		t.Fatalf("GetRecentMessages: %v", err)
	}
	if len(got) != len(messages) {
		t.Fatalf("len(messages) = %d, want %d", len(got), len(messages))
	}

	for i, msg := range messages {
		if got[i].ID != msg.id {
			t.Fatalf("messages[%d].ID = %q, want %q", i, got[i].ID, msg.id)
		}
		if got[i].Role != msg.role {
			t.Fatalf("messages[%d].Role = %q, want %q", i, got[i].Role, msg.role)
		}
		if got[i].Content != msg.content {
			t.Fatalf("messages[%d].Content = %q, want %q", i, got[i].Content, msg.content)
		}
	}
}

func TestGetRecentMessagesRespectsLimit(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	sessionID := "session-3"
	if err := db.CreateSession(ctx, sessionID, "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	for i, msgID := range []string{"msg-1", "msg-2", "msg-3"} {
		if err := db.AddMessage(ctx, msgID, sessionID, "user", msgID); err != nil {
			t.Fatalf("AddMessage(%s): %v", msgID, err)
		}
		if i < 2 {
			time.Sleep(time.Millisecond)
		}
	}

	got, err := db.GetRecentMessages(ctx, sessionID, 2)
	if err != nil {
		t.Fatalf("GetRecentMessages: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(got))
	}
	if got[0].ID != "msg-2" || got[1].ID != "msg-3" {
		t.Fatalf("GetRecentMessages IDs = [%s %s], want [msg-2 msg-3]", got[0].ID, got[1].ID)
	}
}

func TestAddMessageRejectsMissingSession(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	err := db.AddMessage(ctx, "msg-1", "missing-session", "user", "hello")
	if err == nil {
		t.Fatal("AddMessage should fail for missing session")
	}
	if !strings.Contains(strings.ToUpper(err.Error()), "FOREIGN KEY") {
		t.Fatalf("AddMessage error = %v, want foreign key failure", err)
	}
}

func TestUpdateSessionTimestamp(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	sessionID := "session-4"
	if err := db.CreateSession(ctx, sessionID, "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	before, err := db.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSession(before): %v", err)
	}

	time.Sleep(time.Millisecond)

	if err := db.UpdateSessionTimestamp(ctx, sessionID); err != nil {
		t.Fatalf("UpdateSessionTimestamp: %v", err)
	}

	after, err := db.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSession(after): %v", err)
	}
	if after.UpdatedAt <= before.UpdatedAt {
		t.Fatalf("updated_at = %q, want > %q", after.UpdatedAt, before.UpdatedAt)
	}
}
