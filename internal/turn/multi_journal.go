package turn

import (
	"context"
	"fmt"
)

type outboundRecorder interface {
	RecordOutbound(ctx context.Context, turnID string, event OutboundEvent) error
}

type outboundLister interface {
	ListOutbound(ctx context.Context, turnID string) ([]OutboundEvent, error)
}

type MultiJournal struct {
	primary TurnJournal
	mirrors []TurnJournal
}

func NewMultiJournal(primary TurnJournal, mirrors ...TurnJournal) *MultiJournal {
	return &MultiJournal{primary: primary, mirrors: mirrors}
}

func (j *MultiJournal) StartTurn(ctx context.Context, record TurnRecord) error {
	if j == nil || j.primary == nil {
		return fmt.Errorf("primary journal is required")
	}
	if err := j.primary.StartTurn(ctx, record); err != nil {
		return err
	}
	for _, mirror := range j.mirrors {
		if mirror != nil {
			_ = mirror.StartTurn(ctx, record)
		}
	}
	return nil
}

func (j *MultiJournal) RecordTransition(ctx context.Context, turnID string, from, to TurnState, metrics StageMetrics) error {
	if j == nil || j.primary == nil {
		return fmt.Errorf("primary journal is required")
	}
	if err := j.primary.RecordTransition(ctx, turnID, from, to, metrics); err != nil {
		return err
	}
	for _, mirror := range j.mirrors {
		if mirror != nil {
			_ = mirror.RecordTransition(ctx, turnID, from, to, metrics)
		}
	}
	return nil
}

func (j *MultiJournal) RecordEvent(ctx context.Context, turnID string, event JournalEvent) error {
	if j == nil || j.primary == nil {
		return fmt.Errorf("primary journal is required")
	}
	if err := j.primary.RecordEvent(ctx, turnID, event); err != nil {
		return err
	}
	for _, mirror := range j.mirrors {
		if mirror != nil {
			_ = mirror.RecordEvent(ctx, turnID, event)
		}
	}
	return nil
}

func (j *MultiJournal) RecordOutbound(ctx context.Context, turnID string, event OutboundEvent) error {
	if j == nil || j.primary == nil {
		return fmt.Errorf("primary journal is required")
	}
	if primary, ok := j.primary.(outboundRecorder); ok {
		if err := primary.RecordOutbound(ctx, turnID, event); err != nil {
			return err
		}
	} else if err := j.primary.RecordEvent(ctx, turnID, JournalEvent{Stage: StageOutboundCommit, Type: event.Type, Payload: outboundEventPayload(event)}); err != nil {
		return err
	}
	for _, mirror := range j.mirrors {
		if mirror == nil {
			continue
		}
		if recorder, ok := mirror.(outboundRecorder); ok {
			_ = recorder.RecordOutbound(ctx, turnID, event)
			continue
		}
		_ = mirror.RecordEvent(ctx, turnID, JournalEvent{Stage: StageOutboundCommit, Type: event.Type, Payload: outboundEventPayload(event)})
	}
	return nil
}

func (j *MultiJournal) CompleteTurn(ctx context.Context, turnID, status, errorKind string) error {
	if j == nil || j.primary == nil {
		return fmt.Errorf("primary journal is required")
	}
	if err := j.primary.CompleteTurn(ctx, turnID, status, errorKind); err != nil {
		return err
	}
	for _, mirror := range j.mirrors {
		if mirror != nil {
			_ = mirror.CompleteTurn(ctx, turnID, status, errorKind)
		}
	}
	return nil
}

func (j *MultiJournal) ListOutbound(ctx context.Context, turnID string) ([]OutboundEvent, error) {
	if j == nil || j.primary == nil {
		return nil, fmt.Errorf("primary journal is required")
	}
	lister, ok := j.primary.(outboundLister)
	if !ok {
		return nil, fmt.Errorf("primary journal does not support outbound replay")
	}
	return lister.ListOutbound(ctx, turnID)
}

func (j *MultiJournal) GetTurn(ctx context.Context, turnID string) (TurnSnapshot, bool, error) {
	if j == nil || j.primary == nil {
		return TurnSnapshot{}, false, fmt.Errorf("primary journal is required")
	}
	reader, ok := j.primary.(interface {
		GetTurn(context.Context, string) (TurnSnapshot, bool, error)
	})
	if !ok {
		return TurnSnapshot{}, false, fmt.Errorf("primary journal does not support turn replay")
	}
	return reader.GetTurn(ctx, turnID)
}
