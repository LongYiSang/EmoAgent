package turn

import (
	"context"
	"errors"
	"testing"
)

func TestRuntimeExecutesStagesInOrderAndRecordsMetrics(t *testing.T) {
	journal := NewMemoryJournal()
	runtime := NewRuntime(RuntimeConfig{Journal: journal})
	var order []string

	stages := []Stage{
		StageFunc{
			NameValue: StageNormalize,
			RunFunc: func(ctx context.Context, tc *TurnContext) (StageResult, error) {
				order = append(order, "normalize")
				return StageResult{NextState: StateNormalizing}, nil
			},
		},
		StageFunc{
			NameValue: StageDone,
			RunFunc: func(ctx context.Context, tc *TurnContext) (StageResult, error) {
				order = append(order, "done")
				return StageResult{NextState: StateDone, Terminal: true, Status: "done"}, nil
			},
		},
	}

	result, err := runtime.Execute(context.Background(), TurnContext{
		TurnID: "turn-1",
		Inbound: InboundEnvelope{
			Kind:           InboundUserMessage,
			Source:         SourceWebUI,
			SessionID:      "session-1",
			IdempotencyKey: "key-1",
		},
		State: StateCreated,
	}, stages)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != "done" || result.State != StateDone {
		t.Fatalf("result = %#v, want done", result)
	}
	if len(order) != 2 || order[0] != "normalize" || order[1] != "done" {
		t.Fatalf("order = %#v", order)
	}
	snapshot, ok := journal.GetTurn("turn-1")
	if !ok {
		t.Fatal("journal missing turn")
	}
	if len(snapshot.Transitions) != 2 {
		t.Fatalf("transitions = %#v, want 2", snapshot.Transitions)
	}
}

func TestRuntimeTerminalApprovalWaitDoesNotReturnError(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{Journal: NewMemoryJournal()})

	result, err := runtime.Execute(context.Background(), TurnContext{
		TurnID: "turn-approval",
		Inbound: InboundEnvelope{
			Kind:           InboundUserMessage,
			Source:         SourceWebUI,
			SessionID:      "session-1",
			IdempotencyKey: "key-approval",
		},
		State: StateCreated,
	}, []Stage{
		StageFunc{
			NameValue: StageApprovalWait,
			RunFunc: func(ctx context.Context, tc *TurnContext) (StageResult, error) {
				return StageResult{
					NextState: StateApprovalWait,
					Terminal:  true,
					Status:    "approval_wait",
					ErrorKind: "tool_approval",
				}, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("Execute returned error = %v, want nil", err)
	}
	if result.Status != "approval_wait" || result.State != StateApprovalWait {
		t.Fatalf("result = %#v, want approval_wait", result)
	}
}

func TestRuntimeReturnsStageErrorAndMarksFailed(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{Journal: NewMemoryJournal()})
	stageErr := errors.New("boom")

	result, err := runtime.Execute(context.Background(), TurnContext{
		TurnID: "turn-failed",
		Inbound: InboundEnvelope{
			Kind:           InboundUserMessage,
			Source:         SourceWebUI,
			SessionID:      "session-1",
			IdempotencyKey: "key-failed",
		},
		State: StateCreated,
	}, []Stage{
		StageFunc{
			NameValue: StageNormalize,
			RunFunc: func(ctx context.Context, tc *TurnContext) (StageResult, error) {
				return StageResult{NextState: StateFailed, Terminal: true, Status: "failed", ErrorKind: "validation_error"}, stageErr
			},
		},
	})
	if !errors.Is(err, stageErr) {
		t.Fatalf("error = %v, want %v", err, stageErr)
	}
	if result.Status != "failed" || result.ErrorKind != "validation_error" {
		t.Fatalf("result = %#v, want failed validation_error", result)
	}
}
