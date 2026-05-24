package memoryhost

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"github.com/longyisang/emoagent/internal/storage"
)

type Bridge struct {
	host   *Host
	db     *storage.DB
	logger *slog.Logger
}

func NewBridge(host *Host, db *storage.DB, logger *slog.Logger) *Bridge {
	if host == nil || host.Service == nil || db == nil {
		return nil
	}
	return &Bridge{host: host, db: db, logger: logger}
}

func (b *Bridge) EnsureSegment(ctx context.Context, chatSessionID string, personaID string) (storage.MemorySegmentRef, error) {
	if b == nil || b.host == nil || b.host.Service == nil || b.db == nil {
		return storage.MemorySegmentRef{}, fmt.Errorf("memory bridge is not configured")
	}
	if strings.TrimSpace(chatSessionID) == "" {
		return storage.MemorySegmentRef{}, fmt.Errorf("chat session id is required")
	}
	personaID = defaultPersonaID(personaID)

	current, err := b.db.GetCurrentMemorySegment(ctx, chatSessionID)
	if err != nil {
		return storage.MemorySegmentRef{}, err
	}
	if current != nil {
		return storage.MemorySegmentRef{SegmentID: current.ID, MemorySessionID: current.MemorySessionID}, nil
	}
	return b.startSegment(ctx, chatSessionID, personaID)
}

func (b *Bridge) RolloverSegment(ctx context.Context, chatSessionID string, personaID string, reason string) (storage.MemorySegmentRef, error) {
	if b == nil || b.host == nil || b.host.Service == nil || b.db == nil {
		return storage.MemorySegmentRef{}, fmt.Errorf("memory bridge is not configured")
	}
	if strings.TrimSpace(chatSessionID) == "" {
		return storage.MemorySegmentRef{}, fmt.Errorf("chat session id is required")
	}
	personaID = defaultPersonaID(personaID)
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return storage.MemorySegmentRef{}, fmt.Errorf("finalize reason is required")
	}

	current, err := b.db.GetCurrentMemorySegment(ctx, chatSessionID)
	if err != nil {
		return storage.MemorySegmentRef{}, err
	}
	if current != nil {
		if err := b.FinalizeSegment(ctx, current.ID, reason, ""); err != nil {
			return storage.MemorySegmentRef{}, err
		}
	}
	return b.startSegment(ctx, chatSessionID, personaID)
}

func (b *Bridge) AppendUserEpisode(ctx context.Context, segmentID string, messageID string, content string) (string, error) {
	return b.appendEpisode(ctx, segmentID, messageID, content, memorycore.RoleUser)
}

func (b *Bridge) AppendAssistantEpisode(ctx context.Context, segmentID string, messageID string, content string) (string, error) {
	return b.appendEpisode(ctx, segmentID, messageID, content, memorycore.RoleAssistant)
}

func (b *Bridge) FinalizeSegment(ctx context.Context, segmentID string, reason string, summary string) error {
	if b == nil || b.host == nil || b.host.Service == nil || b.db == nil {
		return fmt.Errorf("memory bridge is not configured")
	}
	segment, err := b.db.GetMemorySegment(ctx, segmentID)
	if err != nil {
		return err
	}
	if segment == nil {
		return fmt.Errorf("memory segment not found: %s", segmentID)
	}
	if segment.FinalizedAt != "" {
		return nil
	}

	var summaryPtr *string
	if summary != "" {
		summaryCopy := summary
		summaryPtr = &summaryCopy
	}
	if _, err := b.host.Service.EndSession(ctx, memorycore.EndSessionRequest{
		PersonaID: defaultPersonaID(segmentPersona(segment, b.db, ctx)),
		SessionID: segment.MemorySessionID,
		EndedAt:   time.Now().UTC(),
		Summary:   summaryPtr,
	}); err != nil {
		return err
	}
	if err := b.db.FinalizeMemorySegment(ctx, segmentID, reason, summary); err != nil {
		return err
	}
	if b.logger != nil {
		b.logger.Info("memory segment finalized", "chat_session_id", segment.ChatSessionID, "segment_id", segment.ID, "memory_session_id", segment.MemorySessionID, "reason", reason)
	}
	return nil
}

func (b *Bridge) startSegment(ctx context.Context, chatSessionID string, personaID string) (storage.MemorySegmentRef, error) {
	session, err := b.host.Service.StartSession(ctx, memorycore.StartSessionRequest{
		PersonaID: personaID,
		Channel:   memorycore.ChannelAPI,
		StartedAt: time.Now().UTC(),
	})
	if err != nil {
		return storage.MemorySegmentRef{}, err
	}
	segment, err := b.db.CreateMemorySegment(ctx, storage.CreateMemorySegmentParams{
		ID:              uuid.NewString(),
		ChatSessionID:   chatSessionID,
		PersonaID:       personaID,
		MemorySessionID: session.ID,
	})
	if err != nil {
		return storage.MemorySegmentRef{}, err
	}
	if b.logger != nil {
		b.logger.Info("memory segment started", "chat_session_id", chatSessionID, "segment_id", segment.ID, "memory_session_id", segment.MemorySessionID)
	}
	return storage.MemorySegmentRef{SegmentID: segment.ID, MemorySessionID: segment.MemorySessionID}, nil
}

func (b *Bridge) appendEpisode(ctx context.Context, segmentID string, messageID string, content string, role string) (string, error) {
	if b == nil || b.host == nil || b.host.Service == nil || b.db == nil {
		return "", fmt.Errorf("memory bridge is not configured")
	}
	segment, err := b.db.GetMemorySegment(ctx, segmentID)
	if err != nil {
		return "", err
	}
	if segment == nil {
		return "", fmt.Errorf("memory segment not found: %s", segmentID)
	}
	sourceRef := strings.TrimSpace(messageID)
	var sourceRefPtr *string
	if sourceRef != "" {
		sourceRefPtr = &sourceRef
	}

	episode, err := b.host.Service.AppendEpisode(ctx, memorycore.AppendEpisodeRequest{
		PersonaID:  defaultPersonaID(segmentPersona(segment, b.db, ctx)),
		SessionID:  segment.MemorySessionID,
		Role:       role,
		Content:    content,
		OccurredAt: time.Now().UTC(),
		SourceType: memorycore.SourceTypeChat,
		SourceRef:  sourceRefPtr,
	})
	if err != nil {
		return "", err
	}
	if err := b.db.UpdateMemorySegmentEpisode(ctx, segmentID, role, episode.ID); err != nil {
		return "", err
	}
	return episode.ID, nil
}

func defaultPersonaID(personaID string) string {
	personaID = strings.TrimSpace(personaID)
	if personaID == "" {
		return "default"
	}
	return personaID
}

func segmentPersona(segment *storage.MemorySegment, db *storage.DB, ctx context.Context) string {
	if segment == nil || db == nil {
		return "default"
	}
	link, err := db.GetMemoryChatLink(ctx, segment.ChatSessionID)
	if err != nil || link == nil {
		return "default"
	}
	return link.PersonaID
}
