package conversation

import (
	"context"
	"errors"

	"github.com/longyisang/emoagent/internal/storage"
)

type TimelineEvent struct {
	ID               string
	OriginKey        string
	SessionID        string
	PersonaKey       string
	Type             string
	VisibleContent   string
	PayloadJSON      string
	CreatedAt        string
	VisibilityStatus string
}

type TimelineEventStore struct {
	db *storage.DB
}

func NewTimelineEventStore(db *storage.DB) *TimelineEventStore {
	return &TimelineEventStore{db: db}
}

func (s *TimelineEventStore) Append(ctx context.Context, event TimelineEvent) error {
	if s == nil || s.db == nil {
		return errors.New("timeline event store is not configured")
	}
	if event.OriginKey != "" {
		if err := s.db.UpsertConversationOrigin(ctx, storage.ConversationOriginRecord{
			OriginKey:    event.OriginKey,
			SourceType:   DefaultSourceType,
			ChannelType:  DefaultChannel,
			MetadataJSON: "{}",
		}); err != nil {
			return err
		}
	}
	return s.db.AddConversationEvent(ctx, storage.ConversationEventRecord{
		ID:               event.ID,
		OriginKey:        event.OriginKey,
		SessionID:        event.SessionID,
		PersonaKey:       event.PersonaKey,
		EventType:        event.Type,
		VisibleContent:   event.VisibleContent,
		PayloadJSON:      event.PayloadJSON,
		VisibilityStatus: event.VisibilityStatus,
	})
}

func (s *TimelineEventStore) List(ctx context.Context, sessionID string, originKey string, limit int) ([]TimelineEvent, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("timeline event store is not configured")
	}
	records, err := s.db.ListConversationEvents(ctx, sessionID, originKey, limit)
	if err != nil {
		return nil, err
	}
	events := make([]TimelineEvent, 0, len(records))
	for _, record := range records {
		events = append(events, TimelineEvent{
			ID:               record.ID,
			OriginKey:        record.OriginKey,
			SessionID:        record.SessionID,
			PersonaKey:       record.PersonaKey,
			Type:             record.EventType,
			VisibleContent:   record.VisibleContent,
			PayloadJSON:      record.PayloadJSON,
			CreatedAt:        record.CreatedAt,
			VisibilityStatus: record.VisibilityStatus,
		})
	}
	return events, nil
}
