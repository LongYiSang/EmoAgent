package turn

import (
	"context"
	"time"
)

type RuntimeConfig struct {
	Journal TurnJournal
}

type Runtime struct {
	journal TurnJournal
}

type TurnResult struct {
	TurnID    string
	State     TurnState
	Status    string
	ErrorKind string
}

func NewRuntime(config RuntimeConfig) *Runtime {
	journal := config.Journal
	if journal == nil {
		journal = NewMemoryJournal()
	}
	return &Runtime{journal: journal}
}

func (r *Runtime) Execute(ctx context.Context, tc TurnContext, stages []Stage) (TurnResult, error) {
	if tc.State == "" {
		tc.State = StateCreated
	}
	if tc.StartedAt.IsZero() {
		tc.StartedAt = time.Now()
	}
	if tc.Journal == nil {
		tc.Journal = r.journal
	}
	if tc.Stream == nil {
		tc.Stream = OutboundSinkFromContext(ctx)
	}

	if err := tc.Journal.StartTurn(ctx, TurnRecord{
		TurnID:         tc.TurnID,
		IdempotencyKey: tc.Inbound.IdempotencyKey,
		Kind:           tc.Inbound.Kind,
		SessionID:      tc.Inbound.SessionID,
		PersonaKey:     tc.Inbound.PersonaKey,
		State:          tc.State,
		StartedAt:      tc.StartedAt,
	}); err != nil {
		return TurnResult{TurnID: tc.TurnID, State: tc.State, Status: "failed", ErrorKind: "journal_failed"}, err
	}

	current := tc.State
	for _, stage := range stages {
		startedAt := time.Now()
		result, stageErr := stage.Run(ctx, &tc)
		if stageErr == nil {
			stageErr = result.Err
		}

		next := result.NextState
		if next == "" {
			next = current
		}
		metrics := result.Metrics
		if metrics.Stage == "" {
			metrics.Stage = stage.Name()
		}
		if metrics.DurationMS == 0 {
			metrics.DurationMS = time.Since(startedAt).Milliseconds()
		}
		_ = tc.Journal.RecordTransition(ctx, tc.TurnID, current, next, metrics)

		tc.State = next
		current = next

		if err := r.emitOutbound(ctx, &tc, result.Outbound); err != nil && stageErr == nil {
			stageErr = err
			result.ErrorKind = "outbound_failed"
			result.Status = "failed"
			current = StateFailed
			tc.State = StateFailed
		}

		if stageErr != nil && !isApprovalWait(result, current) {
			status := result.Status
			if status == "" {
				status = "failed"
			}
			errorKind := result.ErrorKind
			if errorKind == "" {
				errorKind = "stage_error"
			}
			state := current
			if state == "" || state == StateApprovalWait {
				state = StateFailed
			}
			_ = tc.Journal.CompleteTurn(ctx, tc.TurnID, status, errorKind)
			return TurnResult{TurnID: tc.TurnID, State: state, Status: status, ErrorKind: errorKind}, stageErr
		}

		if result.Terminal {
			status := result.Status
			if status == "" {
				status = statusForState(current)
			}
			_ = tc.Journal.CompleteTurn(ctx, tc.TurnID, status, result.ErrorKind)
			return TurnResult{TurnID: tc.TurnID, State: current, Status: status, ErrorKind: result.ErrorKind}, nil
		}
	}

	status := statusForState(current)
	_ = tc.Journal.CompleteTurn(ctx, tc.TurnID, status, "")
	return TurnResult{TurnID: tc.TurnID, State: current, Status: status}, nil
}

func (r *Runtime) emitOutbound(ctx context.Context, tc *TurnContext, events []OutboundEvent) error {
	if len(events) == 0 || tc.Stream == nil {
		return nil
	}
	for i := range events {
		event := events[i]
		if event.TurnID == "" {
			event.TurnID = tc.TurnID
		}
		if event.CreatedAt.IsZero() {
			event.CreatedAt = time.Now()
		}
		if err := tc.Stream.Emit(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func isApprovalWait(result StageResult, state TurnState) bool {
	return result.Terminal && (state == StateApprovalWait || result.Status == "approval_wait")
}

func statusForState(state TurnState) string {
	switch state {
	case StateApprovalWait:
		return "approval_wait"
	case StateFailed:
		return "failed"
	case StateCanceled:
		return "canceled"
	case StateCommitFailedAfterOutput:
		return "commit_failed_after_output"
	case StateDone:
		return "done"
	default:
		return string(state)
	}
}
