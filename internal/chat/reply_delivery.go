package chat

import (
	"context"
	"fmt"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/replydelivery"
	"github.com/longyisang/emoagent/internal/turn"
)

func replyDeliveryEnabled(cfg config.ReplyDeliveryConfig, realtimeStreaming bool) bool {
	cfg = config.NormalizeReplyDeliveryConfig(cfg)
	return cfg.Enabled && !(cfg.DisableWhenRealtimeStreaming && realtimeStreaming)
}

func emitReplyDeliverySegments(ctx context.Context, sink turn.OutboundSink, cfg config.ReplyDeliveryConfig, plan replydelivery.Plan, groupID string) (bool, error) {
	if sink == nil || !plan.ShouldEmitSegments() {
		return false, nil
	}
	cfg = config.NormalizeReplyDeliveryConfig(cfg)
	delays := replydelivery.NewDelayCalculator(cfg.Timing, nil)
	total := len(plan.Segments)
	if groupID == "" {
		groupID = "reply"
	}
	for i, segment := range plan.Segments {
		if wait := delays.Delay(segment); wait > 0 && !sleepContext(ctx, wait) {
			return true, nil
		}
		payload := map[string]any{
			"group_id":       groupID,
			"segment_id":     fmt.Sprintf("%s:segment:%d", groupID, i),
			"segment_index":  i,
			"segment_total":  total,
			"reply_strategy": plan.Strategy,
		}
		if err := sink.Emit(ctx, turn.OutboundEvent{
			Type:    turn.EventAssistantSegment,
			TurnID:  groupID,
			Content: segment,
			Payload: payload,
		}); err != nil {
			return true, err
		}
	}
	return true, nil
}

func sleepContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
