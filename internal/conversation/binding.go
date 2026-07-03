package conversation

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/longyisang/emoagent/internal/storage"
)

type SessionStarter interface {
	StartSession(ctx context.Context, personaKey string) (string, error)
}

type Binding struct {
	OriginKey  string
	PersonaKey string
	SessionID  string
	IsNew      bool
}

type BindingService struct {
	db      *storage.DB
	starter SessionStarter
}

func NewBindingService(db *storage.DB, starter SessionStarter) *BindingService {
	return &BindingService{db: db, starter: starter}
}

func (s *BindingService) SetSessionStarter(starter SessionStarter) {
	if s == nil {
		return
	}
	s.starter = starter
}

func (s *BindingService) EnsureCurrent(ctx context.Context, origin Origin, personaKey string) (Binding, error) {
	if s == nil || s.db == nil {
		return Binding{}, errors.New("conversation binding service is not configured")
	}
	if err := ValidateOriginKey(origin.OriginKey); err != nil {
		return Binding{}, err
	}
	personaKey = firstNonEmpty(personaKey, "default")
	if err := s.ensureOrigin(ctx, origin); err != nil {
		return Binding{}, err
	}

	current, err := s.db.GetConversationBinding(ctx, origin.OriginKey, personaKey)
	if err != nil {
		return Binding{}, err
	}
	if current != nil {
		return Binding{OriginKey: origin.OriginKey, PersonaKey: personaKey, SessionID: current.CurrentSessionID}, nil
	}

	latest, err := s.db.GetLatestSession(ctx, personaKey)
	if err != nil {
		return Binding{}, err
	}
	if latest != nil {
		return s.BindSession(ctx, origin, personaKey, latest.ID, false)
	}
	if s.starter == nil {
		return Binding{}, errors.New("session starter is required when no binding or history exists")
	}
	sessionID, err := s.starter.StartSession(ctx, personaKey)
	if err != nil {
		return Binding{}, err
	}
	return s.BindSession(ctx, origin, personaKey, sessionID, true)
}

func (s *BindingService) BindSession(ctx context.Context, origin Origin, personaKey string, sessionID string, isNew bool) (Binding, error) {
	if s == nil || s.db == nil {
		return Binding{}, errors.New("conversation binding service is not configured")
	}
	if err := ValidateOriginKey(origin.OriginKey); err != nil {
		return Binding{}, err
	}
	personaKey = firstNonEmpty(personaKey, "default")
	if err := s.ensureOrigin(ctx, origin); err != nil {
		return Binding{}, err
	}
	if err := s.db.UpsertConversationBinding(ctx, storage.ConversationBindingRecord{
		ID:                uuid.NewString(),
		OriginKey:         origin.OriginKey,
		PersonaKey:        personaKey,
		CurrentSessionID:  sessionID,
		DefaultPersonaKey: personaKey,
		UniqueScope:       "origin",
		VariablesJSON:     "{}",
	}); err != nil {
		return Binding{}, err
	}
	return Binding{OriginKey: origin.OriginKey, PersonaKey: personaKey, SessionID: sessionID, IsNew: isNew}, nil
}

func (s *BindingService) ensureOrigin(ctx context.Context, origin Origin) error {
	return s.db.UpsertConversationOrigin(ctx, storage.ConversationOriginRecord{
		ID:                     uuid.NewString(),
		OriginKey:              origin.OriginKey,
		SourceType:             firstNonEmpty(origin.SourceType, DefaultSourceType),
		AdapterInstanceID:      origin.AdapterInstanceID,
		PlatformID:             origin.PlatformID,
		ChannelType:            firstNonEmpty(origin.ChannelType, DefaultChannel),
		ExternalConversationID: origin.ExternalConversationID,
		ExternalActorID:        origin.ExternalActorID,
		DisplayName:            origin.DisplayName,
		MetadataJSON:           "{}",
	})
}
