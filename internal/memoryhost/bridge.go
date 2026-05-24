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
	host            *Host
	db              *storage.DB
	logger          *slog.Logger
	manualRules     *ManualRules
	retrievalPolicy memorycore.RetrievalPolicy
}

func NewBridge(host *Host, db *storage.DB, logger *slog.Logger, manualRules *ManualRules, retrievalPolicy ...memorycore.RetrievalPolicy) *Bridge {
	if host == nil || host.Service == nil || db == nil {
		return nil
	}
	policy := host.retrievalPolicy
	if len(retrievalPolicy) > 0 {
		policy = retrievalPolicy[0]
	}
	return &Bridge{host: host, db: db, logger: logger, manualRules: manualRules, retrievalPolicy: policy}
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

func (b *Bridge) RetrievePromptBlock(ctx context.Context, chatSessionID string, query string, excludedEpisodeIDs ...string) (string, error) {
	if b == nil || b.host == nil || b.host.Service == nil || b.db == nil {
		return "", fmt.Errorf("memory bridge is not configured")
	}
	chatSessionID = strings.TrimSpace(chatSessionID)
	if chatSessionID == "" {
		return "", fmt.Errorf("chat session id is required")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return "", nil
	}

	current, err := b.db.GetCurrentMemorySegment(ctx, chatSessionID)
	if err != nil {
		return "", err
	}
	if current == nil {
		return "", nil
	}

	memorySessionID := current.MemorySessionID
	contextResult, err := b.host.Service.Retrieve(ctx, memorycore.RetrievalRequest{
		PersonaID: defaultPersonaID(segmentPersona(current, b.db, ctx)),
		SessionID: &memorySessionID,
		QueryText: query,
		Policy:    b.retrievalPolicy,
	})
	if err != nil {
		return "", err
	}
	return FormatMemoryContext(contextResult, excludedEpisodeIDs...), nil
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
	if role == memorycore.RoleUser {
		if err := b.applyManualMemoryIntent(ctx, segmentID, content); err != nil {
			b.logManualMemoryWarning("manual memory intent", segment.ChatSessionID, err)
		}
	}
	return episode.ID, nil
}

func (b *Bridge) applyManualMemoryIntent(ctx context.Context, segmentID string, content string) error {
	if b == nil || b.manualRules == nil || b.host == nil || b.host.Service == nil || b.db == nil {
		return nil
	}
	intent := b.manualRules.Match(content)
	switch intent.Kind {
	case ManualMemoryIntentNone:
		return nil
	case ManualMemoryIntentForget:
		return nil
	case ManualMemoryIntentPin:
	default:
		return nil
	}

	segment, err := b.db.GetMemorySegment(ctx, segmentID)
	if err != nil {
		return err
	}
	if segment == nil {
		return fmt.Errorf("memory segment not found: %s", segmentID)
	}
	sourceEpisodeID := strings.TrimSpace(segment.LastUserEpisodeID)
	if sourceEpisodeID == "" {
		return fmt.Errorf("last user episode id is required for manual pin")
	}

	personaID := defaultPersonaID(segmentPersona(segment, b.db, ctx))
	user, err := b.host.Service.EnsureEntity(ctx, memorycore.EnsureEntityRequest{
		PersonaID:        personaID,
		CanonicalName:    "用户",
		EntityType:       memorycore.EntityTypeUser,
		SensitivityLevel: memorycore.SensitivityNormal,
	})
	if err != nil {
		return err
	}

	candidate := intent.Candidate
	candidate.SubjectEntityID = user.ID
	candidate.SourceEpisodeIDs = []string{sourceEpisodeID}
	memorySessionID := segment.MemorySessionID
	result, err := b.host.Service.ConsolidateCandidate(ctx, memorycore.ConsolidateCandidateRequest{
		PersonaID: personaID,
		SessionID: &memorySessionID,
		Trigger:   memorycore.ConsolidationTriggerManual,
		Candidate: candidate,
		Policy: memorycore.ConsolidationPolicy{
			Approved: true,
		},
	})
	if err != nil {
		return err
	}
	if result == nil || result.Fact == nil {
		status := ""
		reason := ""
		if result != nil {
			status = result.Status
			reason = firstNonEmptyString(result.RejectedReason, result.NeedsReviewReason)
		}
		return fmt.Errorf("manual pin not applied: status=%s reason=%s", status, reason)
	}
	return nil
}

func (b *Bridge) logManualMemoryWarning(action string, chatSessionID string, err error) {
	if b == nil || b.logger == nil || err == nil {
		return
	}
	b.logger.Warn(action+" failed", "chat_session_id", chatSessionID, "error", err)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func FormatMemoryContext(mc *memorycore.MemoryContext, excludedEpisodeIDs ...string) string {
	if mc == nil || len(mc.Blocks) == 0 {
		return ""
	}
	excluded := excludedEpisodeIDSet(excludedEpisodeIDs)

	var items []string
	for _, block := range mc.Blocks {
		if len(block.Items) == 0 {
			continue
		}

		for _, item := range block.Items {
			if itemOnlyFromExcludedEpisodes(item, excluded) {
				continue
			}
			summary := strings.TrimSpace(item.Summary)
			if summary == "" {
				continue
			}
			items = append(items, "- "+summary)
		}
	}
	if len(items) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("[长期记忆上下文]\n")
	b.WriteString("以下是允许用于当前回复的长期记忆。使用时要自然、克制；\n")
	b.WriteString("不要主动说明“我记得”，除非用户正在询问记忆或来源。\n\n")
	b.WriteString(strings.Join(items, "\n"))
	return strings.TrimSpace(b.String())
}

func excludedEpisodeIDSet(ids []string) map[string]struct{} {
	if len(ids) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			out[id] = struct{}{}
		}
	}
	return out
}

func itemOnlyFromExcludedEpisodes(item memorycore.MemoryContextItem, excluded map[string]struct{}) bool {
	if len(item.SourceRefs) == 0 || len(excluded) == 0 {
		return false
	}
	for _, ref := range item.SourceRefs {
		if _, ok := excluded[strings.TrimSpace(ref.EpisodeID)]; !ok {
			return false
		}
	}
	return true
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
