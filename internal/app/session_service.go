package app

import (
	"context"

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
