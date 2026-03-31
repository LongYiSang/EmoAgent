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

func TestListSessionsExcludesEmptySessions(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	if err := db.CreateSession(ctx, "empty-session", "default"); err != nil {
		t.Fatalf("CreateSession(empty): %v", err)
	}
	if err := db.CreateSession(ctx, "filled-session", "default"); err != nil {
		t.Fatalf("CreateSession(filled): %v", err)
	}
	if err := db.AddMessage(ctx, "msg-1", "filled-session", "user", "hello"); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	sessions, err := db.ListSessions(ctx, "default", 10)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("len(sessions) = %d, want 1", len(sessions))
	}
	if sessions[0].ID != "filled-session" {
		t.Fatalf("sessions[0].ID = %q, want filled-session", sessions[0].ID)
	}
	if sessions[0].MessageCount != 1 {
		t.Fatalf("sessions[0].MessageCount = %d, want 1", sessions[0].MessageCount)
	}
	if sessions[0].LastMessage != "hello" {
		t.Fatalf("sessions[0].LastMessage = %q, want hello", sessions[0].LastMessage)
	}
}

func TestListSessionsByPersonaKey(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	if err := db.CreateSession(ctx, "default-session", "default"); err != nil {
		t.Fatalf("CreateSession(default): %v", err)
	}
	if err := db.AddMessage(ctx, "msg-default", "default-session", "user", "hello"); err != nil {
		t.Fatalf("AddMessage(default): %v", err)
	}
	time.Sleep(time.Millisecond)
	if err := db.CreateSession(ctx, "neko-session", "neko"); err != nil {
		t.Fatalf("CreateSession(neko): %v", err)
	}
	if err := db.AddMessage(ctx, "msg-neko", "neko-session", "assistant", "meow"); err != nil {
		t.Fatalf("AddMessage(neko): %v", err)
	}

	sessions, err := db.ListSessions(ctx, "neko", 10)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("len(sessions) = %d, want 1", len(sessions))
	}
	if sessions[0].ID != "neko-session" {
		t.Fatalf("sessions[0].ID = %q, want neko-session", sessions[0].ID)
	}
	if sessions[0].Persona != "neko" {
		t.Fatalf("sessions[0].Persona = %q, want neko", sessions[0].Persona)
	}
}

func TestGetLatestSessionExcludesEmptySessions(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	if err := db.CreateSession(ctx, "old-filled", "default"); err != nil {
		t.Fatalf("CreateSession(old-filled): %v", err)
	}
	if err := db.AddMessage(ctx, "msg-old", "old-filled", "user", "first"); err != nil {
		t.Fatalf("AddMessage(old-filled): %v", err)
	}
	time.Sleep(time.Millisecond)
	if err := db.CreateSession(ctx, "latest-empty", "default"); err != nil {
		t.Fatalf("CreateSession(latest-empty): %v", err)
	}

	session, err := db.GetLatestSession(ctx, "default")
	if err != nil {
		t.Fatalf("GetLatestSession: %v", err)
	}
	if session == nil {
		t.Fatal("GetLatestSession returned nil")
	}
	if session.ID != "old-filled" {
		t.Fatalf("session.ID = %q, want old-filled", session.ID)
	}
}

func TestDeleteSessionRemovesSessionAndMessages(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	if err := db.CreateSession(ctx, "session-delete", "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := db.AddMessage(ctx, "msg-delete", "session-delete", "user", "bye"); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	if err := db.DeleteSession(ctx, "session-delete"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	session, err := db.GetSession(ctx, "session-delete")
	if err != nil {
		t.Fatalf("GetSession(after delete): %v", err)
	}
	if session != nil {
		t.Fatalf("GetSession(after delete) = %#v, want nil", session)
	}

	messages, err := db.GetRecentMessages(ctx, "session-delete", 10)
	if err != nil {
		t.Fatalf("GetRecentMessages(after delete): %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("len(messages) = %d, want 0", len(messages))
	}
}
