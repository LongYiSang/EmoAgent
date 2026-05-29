package memoryhost

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
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
	manualMu        sync.Mutex
	pendingForgets  map[string]manualForgetPreview
	manualNotices   map[string]string
}

type manualForgetPreview struct {
	Request memorycore.ForgetPreviewRequest
	Preview memorycore.ForgetPreviewResult
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
	return FormatMemoryContextForPrompt(contextResult, excludedEpisodeIDs...), nil
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
	personaID := defaultPersonaID(segmentPersona(segment, b.db, ctx))
	if _, err := b.host.Service.EndSession(ctx, memorycore.EndSessionRequest{
		PersonaID: personaID,
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
	b.extractFinalizedSegment(ctx, segment, personaID)
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
	segment, err := b.db.GetMemorySegment(ctx, segmentID)
	if err != nil {
		return err
	}
	if segment == nil {
		return fmt.Errorf("memory segment not found: %s", segmentID)
	}
	if handled, err := b.applyPendingManualForgetDecision(ctx, segment, content); handled || err != nil {
		return err
	}
	intent := b.manualRules.Match(content)
	if intent.Kind == ManualMemoryIntentNone {
		return nil
	}
	sourceEpisodeID := strings.TrimSpace(segment.LastUserEpisodeID)
	if sourceEpisodeID == "" {
		return fmt.Errorf("last user episode id is required for manual memory intent")
	}

	switch intent.Kind {
	case ManualMemoryIntentPin:
		return b.applyManualPinIntent(ctx, segment, sourceEpisodeID)
	case ManualMemoryIntentForget:
		return b.applyManualForgetIntent(ctx, segment, intent.ForgetQuery)
	default:
		return nil
	}
}

func (b *Bridge) applyManualPinIntent(ctx context.Context, segment *storage.MemorySegment, sourceEpisodeID string) error {
	if b == nil || b.host == nil || b.host.Service == nil || segment == nil {
		return nil
	}
	if !b.host.ExtractionEnabled() || !b.host.extractionPolicy.TriggerOnManualPin {
		return nil
	}
	sourceEpisodeID = strings.TrimSpace(sourceEpisodeID)
	if sourceEpisodeID == "" {
		return fmt.Errorf("last user episode id is required for manual pin")
	}
	personaID := defaultPersonaID(segmentPersona(segment, b.db, ctx))
	memorySessionID := segment.MemorySessionID
	result, err := b.host.Service.RunExtraction(ctx, memorycore.RunExtractionRequest{
		PersonaID: personaID,
		SessionID: &memorySessionID,
		Trigger:   memorycore.ExtractionTriggerManualPin,
		Timezone:  b.host.extractionPolicy.timezoneOrDefault(),
		Mode:      b.host.extractionPolicy.manualPinModeOrDefault(),
		Build: &memorycore.ExtractionBuildSelector{
			EpisodeIDs: []string{sourceEpisodeID},
			SessionID:  &memorySessionID,
			Limit:      1,
		},
		Policy: memorycore.ExtractionPolicyOverride{
			ManualPin:      boolPtr(true),
			AllowInference: boolPtr(true),
		},
		SemanticDedup: b.host.extractionPolicy.SemanticDedup,
	})
	b.logExtractionResult("manual memory pin extraction", segment, result, err, memorycore.ExtractionTriggerManualPin)
	if err != nil {
		return sanitizedExtractionError(extractionErrorCode(result, err), "")
	}
	if result == nil || result.AppliedCount == 0 {
		return fmt.Errorf("manual pin not applied: status=%s accepted=%d review=%d rejected=%d", safeExtractionStatus(result), safeExtractionCount(result, "accepted"), safeExtractionCount(result, "review"), safeExtractionCount(result, "rejected"))
	}
	return nil
}

func (b *Bridge) applyManualForgetIntent(ctx context.Context, segment *storage.MemorySegment, query string) error {
	if b == nil || b.host == nil || b.host.Service == nil || segment == nil {
		return nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	personaID := defaultPersonaID(segmentPersona(segment, b.db, ctx))
	requestID := "manual_forget_" + uuid.NewString()
	previewReq := memorycore.ForgetPreviewRequest{
		RequestID:           requestID,
		PersonaID:           personaID,
		Actor:               memorycore.ForgetActorUser,
		RequestedLevel:      memorycore.ForgetLevelSoft,
		ScopeMode:           memorycore.ForgetScopeSemanticQuery,
		SessionID:           segment.MemorySessionID,
		Limit:               5,
		SemanticQuery:       &query,
		RequireConfirmation: true,
	}
	preview, err := b.host.Service.PreviewForget(ctx, previewReq)
	if err != nil {
		b.queueManualMemoryNotice(segment.ChatSessionID, "我暂时无法生成可删除候选，未执行删除。")
		return nil
	}
	if preview == nil || len(preview.Targets) == 0 {
		b.clearPendingForget(segment.ChatSessionID)
		b.queueManualMemoryNotice(segment.ChatSessionID, "我没有找到可安全删除的候选，未执行删除。")
		return nil
	}
	if strings.TrimSpace(preview.PreviewHash) == "" {
		b.clearPendingForget(segment.ChatSessionID)
		b.queueManualMemoryNotice(segment.ChatSessionID, "删除预览缺少校验信息，未执行删除。")
		return nil
	}
	if strings.TrimSpace(preview.RequestID) == "" {
		preview.RequestID = requestID
	}
	if strings.TrimSpace(preview.RequestedLevel) == "" {
		preview.RequestedLevel = memorycore.ForgetLevelSoft
	}
	if strings.TrimSpace(preview.ScopeMode) == "" {
		preview.ScopeMode = memorycore.ForgetScopeSemanticQuery
	}
	b.storePendingForget(segment.ChatSessionID, manualForgetPreview{
		Request: previewReq,
		Preview: *preview,
	})
	b.queueManualMemoryNotice(segment.ChatSessionID, buildManualForgetPreviewNotice(*preview))
	return nil
}

func (b *Bridge) applyPendingManualForgetDecision(ctx context.Context, segment *storage.MemorySegment, content string) (bool, error) {
	if b == nil || segment == nil {
		return false, nil
	}
	text := strings.TrimSpace(content)
	if text == "" || !b.hasPendingForget(segment.ChatSessionID) {
		return false, nil
	}
	if isManualForgetCancel(text) {
		b.clearPendingForget(segment.ChatSessionID)
		b.queueManualMemoryNotice(segment.ChatSessionID, "已取消删除，未更改长期记忆。")
		return true, nil
	}
	if !isManualForgetConfirm(text) {
		return false, nil
	}
	pending, ok := b.pendingForget(segment.ChatSessionID)
	if !ok {
		return false, nil
	}
	targets := exactForgetTargets(pending.Preview.Targets)
	if len(targets) == 0 {
		b.clearPendingForget(segment.ChatSessionID)
		b.queueManualMemoryNotice(segment.ChatSessionID, "没有可执行的 exact-node 删除目标，未更改长期记忆。")
		return true, nil
	}
	level := strings.TrimSpace(pending.Preview.RequestedLevel)
	if level == "" {
		level = memorycore.ForgetLevelSoft
	}
	personaID := defaultPersonaID(segmentPersona(segment, b.db, ctx))
	result, err := b.host.Service.ExecuteForget(ctx, memorycore.ForgetExecuteRequest{
		PersonaID:        personaID,
		Actor:            memorycore.ForgetActorUser,
		ReasonCode:       memorycore.ForgetReasonUserRequested,
		Level:            level,
		PreviewRequest:   pending.Request,
		Preview:          pending.Preview,
		PreviewHash:      pending.Preview.PreviewHash,
		ConfirmedTargets: targets,
		Confirmed:        true,
	})
	if err != nil {
		b.queueManualMemoryNotice(segment.ChatSessionID, "删除执行失败，长期记忆未确认更改。")
		return true, nil
	}
	b.clearPendingForget(segment.ChatSessionID)
	b.queueManualMemoryNotice(segment.ChatSessionID, buildManualForgetExecutedNotice(result))
	return true, nil
}

func (b *Bridge) TakeManualMemoryNotice(chatSessionID string) (string, bool) {
	if b == nil {
		return "", false
	}
	chatSessionID = strings.TrimSpace(chatSessionID)
	if chatSessionID == "" {
		return "", false
	}
	b.manualMu.Lock()
	defer b.manualMu.Unlock()
	if len(b.manualNotices) == 0 {
		return "", false
	}
	notice := strings.TrimSpace(b.manualNotices[chatSessionID])
	if notice == "" {
		return "", false
	}
	delete(b.manualNotices, chatSessionID)
	return notice, true
}

func (b *Bridge) storePendingForget(chatSessionID string, pending manualForgetPreview) {
	chatSessionID = strings.TrimSpace(chatSessionID)
	if b == nil || chatSessionID == "" {
		return
	}
	b.manualMu.Lock()
	defer b.manualMu.Unlock()
	if b.pendingForgets == nil {
		b.pendingForgets = make(map[string]manualForgetPreview)
	}
	b.pendingForgets[chatSessionID] = pending
}

func (b *Bridge) pendingForget(chatSessionID string) (manualForgetPreview, bool) {
	chatSessionID = strings.TrimSpace(chatSessionID)
	if b == nil || chatSessionID == "" {
		return manualForgetPreview{}, false
	}
	b.manualMu.Lock()
	defer b.manualMu.Unlock()
	pending, ok := b.pendingForgets[chatSessionID]
	return pending, ok
}

func (b *Bridge) hasPendingForget(chatSessionID string) bool {
	_, ok := b.pendingForget(chatSessionID)
	return ok
}

func (b *Bridge) clearPendingForget(chatSessionID string) {
	chatSessionID = strings.TrimSpace(chatSessionID)
	if b == nil || chatSessionID == "" {
		return
	}
	b.manualMu.Lock()
	defer b.manualMu.Unlock()
	delete(b.pendingForgets, chatSessionID)
}

func (b *Bridge) queueManualMemoryNotice(chatSessionID string, notice string) {
	chatSessionID = strings.TrimSpace(chatSessionID)
	notice = strings.TrimSpace(notice)
	if b == nil || chatSessionID == "" || notice == "" {
		return
	}
	b.manualMu.Lock()
	defer b.manualMu.Unlock()
	if b.manualNotices == nil {
		b.manualNotices = make(map[string]string)
	}
	b.manualNotices[chatSessionID] = notice
}

func exactForgetTargets(targets []memorycore.ForgetResolvedTarget) []memorycore.ExactNodeRef {
	out := make([]memorycore.ExactNodeRef, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		nodeType := strings.TrimSpace(target.NodeType)
		nodeID := strings.TrimSpace(target.NodeID)
		if nodeType == "" || nodeID == "" {
			continue
		}
		key := nodeType + "\x00" + nodeID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, memorycore.ExactNodeRef{NodeType: nodeType, NodeID: nodeID})
	}
	return out
}

func buildManualForgetPreviewNotice(preview memorycore.ForgetPreviewResult) string {
	var lines []string
	for _, target := range preview.Targets {
		summary := strings.TrimSpace(target.SafeSummary)
		if summary == "" {
			continue
		}
		lines = append(lines, "- "+summary)
	}
	if len(lines) == 0 {
		return "我找到了候选，但没有可展示的安全摘要。未执行删除。"
	}
	return "我找到了以下可删除候选，尚未执行删除：\n" + strings.Join(lines, "\n") + "\n\n确认删除请回复“确认删除”；取消请回复“取消”。"
}

func buildManualForgetExecutedNotice(result *memorycore.ForgetExecuteResult) string {
	if result == nil || result.Executed == 0 {
		return "没有执行删除。"
	}
	return fmt.Sprintf("已删除 %d 条确认的长期记忆。", result.Executed)
}

func isManualForgetConfirm(text string) bool {
	switch strings.TrimSpace(text) {
	case "确认", "确认删除", "删除", "是的", "对", "可以":
		return true
	default:
		return false
	}
}

func isManualForgetCancel(text string) bool {
	switch strings.TrimSpace(text) {
	case "取消", "不要", "不用了", "先别删", "别删":
		return true
	default:
		return false
	}
}

func (b *Bridge) logManualMemoryWarning(action string, chatSessionID string, err error) {
	if b == nil || b.logger == nil || err == nil {
		return
	}
	b.logger.Warn(action+" failed", "chat_session_id", chatSessionID, "error_code", safeErrorCode(nil, err))
}

func (b *Bridge) extractFinalizedSegment(ctx context.Context, segment *storage.MemorySegment, personaID string) {
	if b == nil || b.host == nil || !b.host.ExtractionEnabled() || !b.host.extractionTriggerOnFinalizeSegment() || segment == nil {
		return
	}
	result, err := b.host.ExtractSessionEnd(ctx, personaID, segment.MemorySessionID)
	b.logExtractionResult("memory extraction", segment, result, err, memorycore.ExtractionTriggerSessionEnd)
}

func (b *Bridge) logExtractionResult(action string, segment *storage.MemorySegment, result *memorycore.ExtractionRunResult, err error, trigger string) {
	if b == nil || b.logger == nil || segment == nil {
		return
	}
	fields := []any{
		"chat_session_id", segment.ChatSessionID,
		"segment_id", segment.ID,
		"memory_session_id", segment.MemorySessionID,
		"trigger", trigger,
		"status", safeExtractionStatus(result),
		"accepted", safeExtractionCount(result, "accepted"),
		"review", safeExtractionCount(result, "review"),
		"rejected", safeExtractionCount(result, "rejected"),
		"routed", safeExtractionCount(result, "routed"),
		"not_applied", safeExtractionCount(result, "not_applied"),
		"applied", safeExtractionCount(result, "applied"),
		"failure", safeExtractionCount(result, "failure"),
		"skipped_by_fingerprint", result != nil && result.SkippedByFingerprint,
	}
	if err != nil {
		fields = append(fields, "error_code", safeErrorCode(result, err))
		b.logger.Warn(action+" failed", fields...)
		return
	}
	b.logger.Info(action+" completed", fields...)
}

func safeExtractionStatus(result *memorycore.ExtractionRunResult) string {
	if result == nil {
		return ""
	}
	return string(result.Status)
}

func safeExtractionCount(result *memorycore.ExtractionRunResult, field string) int {
	if result == nil {
		return 0
	}
	switch field {
	case "accepted":
		return result.AcceptedCount
	case "review":
		return result.ReviewCount
	case "rejected":
		return result.RejectedCount
	case "routed":
		return result.RoutedCount
	case "not_applied":
		return result.NotAppliedCount
	case "applied":
		return result.AppliedCount
	case "failure":
		return result.FailureCount
	default:
		return 0
	}
}

func safeErrorCode(result *memorycore.ExtractionRunResult, err error) string {
	code := extractionErrorCode(result, err)
	if code == "" {
		return "unknown"
	}
	return code
}

func boolPtr(value bool) *bool {
	return &value
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

func FormatMemoryContextForPrompt(mc *memorycore.MemoryContext, excludedEpisodeIDs ...string) string {
	if mc == nil || len(mc.Blocks) == 0 {
		return ""
	}
	excluded := excludedEpisodeIDSet(excludedEpisodeIDs)
	sections := map[string][]string{
		"[核心身份与边界]":  {},
		"[当前相关记忆]":   {},
		"[因果/历史上下文]": {},
	}
	usageLines := []string{
		"不要主动说明“我记得”，除非用户询问来源。",
		"历史事实不能当当前事实说。",
		"低置信度记忆只可柔和使用。",
	}
	seenGuidance := make(map[string]struct{}, len(usageLines))
	for _, line := range usageLines {
		seenGuidance[line] = struct{}{}
	}
	summaryByNodeID := make(map[string]string)
	suppressedNodeIDs := make(map[string]struct{}, len(mc.DoNotMention))
	for _, suppression := range mc.DoNotMention {
		nodeID := strings.TrimSpace(suppression.NodeID)
		if nodeID != "" {
			suppressedNodeIDs[nodeID] = struct{}{}
		}
	}
	validItems := 0

	for _, block := range mc.Blocks {
		section := promptSectionTitle(block.BlockType)
		for _, item := range block.Items {
			if itemOnlyFromExcludedEpisodes(item, excluded) {
				continue
			}
			summary := strings.TrimSpace(item.Summary)
			if summary == "" {
				continue
			}
			nodeID := strings.TrimSpace(item.NodeID)
			if nodeID != "" {
				summaryByNodeID[nodeID] = summary
			}
			if guidance := strings.TrimSpace(item.UsageGuidance); guidance != "" {
				if _, ok := seenGuidance[guidance]; !ok {
					seenGuidance[guidance] = struct{}{}
					usageLines = append(usageLines, guidance)
				}
			}
			if _, suppressed := suppressedNodeIDs[nodeID]; suppressed {
				continue
			}
			validItems++
			sections[section] = append(sections[section], "- "+formatPromptMemoryItem(item, summary))
		}
	}

	var doNotMention []string
	for _, suppression := range mc.DoNotMention {
		summary := summaryByNodeID[strings.TrimSpace(suppression.NodeID)]
		if summary == "" {
			continue
		}
		reason := strings.TrimSpace(suppression.Reason)
		if reason != "" {
			doNotMention = append(doNotMention, "- "+summary+" ("+promptSuppressionReason(reason)+")")
			continue
		}
		doNotMention = append(doNotMention, "- "+summary)
	}

	if validItems == 0 && len(doNotMention) == 0 {
		return ""
	}

	usageItems := make([]string, 0, len(usageLines))
	for _, line := range usageLines {
		line = strings.TrimSpace(line)
		if line != "" {
			usageItems = append(usageItems, "- "+line)
		}
	}

	var b strings.Builder
	writePromptSection(&b, "[长期记忆上下文：使用约束]", usageItems)
	for _, title := range []string{"[核心身份与边界]", "[当前相关记忆]", "[因果/历史上下文]"} {
		writePromptSection(&b, title, sections[title])
	}
	writePromptSection(&b, "[不要主动提及]", doNotMention)
	return strings.TrimSpace(b.String())
}

func promptSectionTitle(blockType string) string {
	switch strings.TrimSpace(blockType) {
	case memorycore.MemoryBlockTypeFacts, memorycore.MemoryBlockTypeRelationshipArcMemory:
		return "[核心身份与边界]"
	case memorycore.MemoryBlockTypeHistoricalTransitionMemory, memorycore.MemoryBlockTypeProvenanceMemory, memorycore.MemoryBlockTypePremiseCheckMemory:
		return "[因果/历史上下文]"
	default:
		return "[当前相关记忆]"
	}
}

func formatPromptMemoryItem(item memorycore.MemoryContextItem, summary string) string {
	line := summary
	switch strings.TrimSpace(item.HistoricalStatus) {
	case memorycore.MemoryHistoricalStatusHistorical:
		line += " [historical]"
	case memorycore.MemoryHistoricalStatusSuperseded:
		line += " [superseded]"
	}
	var notes []string
	if guidance := strings.TrimSpace(item.UsageGuidance); guidance != "" {
		notes = append(notes, guidance)
	}
	if item.DoNotOverstate {
		notes = append(notes, "不要夸大")
	}
	if len(notes) > 0 {
		line += " (" + strings.Join(notes, "；") + ")"
	}
	return line
}

func promptSuppressionReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case memorycore.MemorySuppressionReasonFatigue:
		return "近期已多次使用，避免主动提及"
	case memorycore.MemorySuppressionReasonMMRDuplicate:
		return "与已选记忆重复，避免主动提及"
	case memorycore.MemorySuppressionReasonContextBudget:
		return "受上下文预算限制，避免主动提及"
	default:
		return "避免主动提及"
	}
}

func writePromptSection(b *strings.Builder, title string, lines []string) {
	if len(lines) == 0 {
		return
	}
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString(strings.Join(lines, "\n"))
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
