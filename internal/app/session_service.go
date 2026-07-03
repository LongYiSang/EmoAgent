package app

import (
	"context"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/storage"
)

type SessionService struct {
	infra *Infra
	work  *WorkService
}

func (s *SessionService) List(ctx context.Context, persona string, limit int) ([]storage.SessionSummary, error) {
	return s.infra.DB.ListSessions(ctx, persona, limit)
}

func (s *SessionService) Latest(ctx context.Context, persona string) (*storage.SessionSummary, error) {
	return s.infra.DB.GetLatestSession(ctx, persona)
}

func (s *SessionService) Detail(ctx context.Context, id string) (*storage.SessionRecord, []storage.MessageRecord, error) {
	session, err := s.infra.DB.GetSession(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if session == nil {
		return nil, nil, ErrSessionNotFound
	}
	messages, err := s.infra.DB.GetAllMessages(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	return session, messages, nil
}

func (s *SessionService) DetailForOrigin(ctx context.Context, id string, originKey string) (*storage.SessionRecord, []storage.MessageRecord, []storage.ConversationEventRecord, *storage.SessionClearMarkerRecord, error) {
	session, messages, err := s.Detail(ctx, id)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	originKey = strings.TrimSpace(originKey)
	var marker *storage.SessionClearMarkerRecord
	if originKey != "" {
		marker, err = s.infra.DB.GetSessionClearMarker(ctx, originKey, id)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		messages = filterMessagesAfterClearMarker(messages, marker)
	}
	events, err := s.infra.DB.ListConversationEvents(ctx, id, originKey, 500)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	events = filterEventsAfterClearMarker(events, marker)
	return session, messages, events, marker, nil
}

func (s *SessionService) Delete(ctx context.Context, id string) error {
	session, err := s.infra.DB.GetSession(ctx, id)
	if err != nil {
		return err
	}
	if session == nil {
		return ErrSessionNotFound
	}
	return s.infra.DB.DeleteSession(ctx, id)
}

func (s *SessionService) ListApprovals(ctx context.Context, sessionID string) ([]protocol.ApprovalRequest, error) {
	return s.work.ListSessionApprovals(sessionID), nil
}

func filterMessagesAfterClearMarker(messages []storage.MessageRecord, marker *storage.SessionClearMarkerRecord) []storage.MessageRecord {
	if marker == nil || strings.TrimSpace(marker.AfterMessageID) == "" {
		return messages
	}
	for i := range messages {
		if messages[i].ID == marker.AfterMessageID {
			return append([]storage.MessageRecord(nil), messages[i+1:]...)
		}
	}
	return []storage.MessageRecord{}
}

func filterEventsAfterClearMarker(events []storage.ConversationEventRecord, marker *storage.SessionClearMarkerRecord) []storage.ConversationEventRecord {
	if marker == nil || strings.TrimSpace(marker.ClearedAt) == "" {
		return events
	}
	clearedAt, err := time.Parse(time.RFC3339Nano, marker.ClearedAt)
	if err != nil {
		return events
	}
	out := make([]storage.ConversationEventRecord, 0, len(events))
	for _, event := range events {
		createdAt, err := time.Parse(time.RFC3339Nano, event.CreatedAt)
		if err != nil || !createdAt.Before(clearedAt) {
			out = append(out, event)
		}
	}
	return out
}
