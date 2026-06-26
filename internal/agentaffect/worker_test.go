package agentaffect

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
)

type recordingEvaluator struct {
	result   LLMEvaluationResult
	err      error
	calls    int
	requests []LLMEvaluationRequest
	onCall   func()
}

func (f *recordingEvaluator) Evaluate(ctx context.Context, req LLMEvaluationRequest) (LLMEvaluationResult, error) {
	f.calls++
	f.requests = append(f.requests, req)
	if f.onCall != nil {
		f.onCall()
	}
	if f.err != nil {
		return LLMEvaluationResult{}, f.err
	}
	return f.result, nil
}

func TestProcessNextBatchMergesThreeSamePersonaJobsIntoOneEvaluation(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	cfg.Enabled = true
	evaluator := &recordingEvaluator{result: LLMEvaluationResult{
		Delta:           MoodVector{Warmth: 0.2, Curiosity: 0.1},
		Label:           "engaged",
		MoodDescription: "更投入",
		MoodReason:      "连续几轮对话都很顺畅。",
		PromptMoodText:  "当前模拟心情：更投入，因为连续几轮对话都很顺畅。",
		Confidence:      0.85,
		Status:          EvaluationStatusPreview,
	}}
	rt, db := newTestRuntime(t, cfg, evaluator)

	for i, sessionID := range []string{"session-1", "session-2", "session-3"} {
		_, err := rt.EnqueueTurnEvaluationJob(context.Background(), EnqueueTurnEvaluationJobRequest{
			PersonaID:     "default",
			SessionID:     sessionID,
			TurnID:        "turn-" + string(rune('1'+i)),
			UserText:      "user message",
			AssistantText: "assistant reply",
			Trigger:       TriggerDescriptor{TriggerType: "user_message"},
		})
		if err != nil {
			t.Fatalf("EnqueueTurnEvaluationJob: %v", err)
		}
	}

	processed, err := rt.ProcessNextBatch(context.Background(), "worker-1")
	if err != nil {
		t.Fatalf("ProcessNextBatch: %v", err)
	}
	if !processed {
		t.Fatal("ProcessNextBatch processed = false, want true")
	}
	if evaluator.calls != 1 {
		t.Fatalf("evaluator calls = %d, want 1", evaluator.calls)
	}
	if len(evaluator.requests) != 1 || evaluator.requests[0].EventBatch.TurnCount != 3 {
		t.Fatalf("batch evaluator request = %#v", evaluator.requests)
	}
	var payload struct {
		SchemaVersion string `json:"schema_version"`
		JobCount      int    `json:"job_count"`
		MoodOwnerID   string `json:"mood_owner_id"`
		EventBatch    struct {
			TurnCount int `json:"turn_count"`
			Turns     []struct {
				Ordinal   int    `json:"ordinal"`
				User      string `json:"user"`
				Assistant string `json:"assistant"`
			} `json:"turns"`
		} `json:"event_batch"`
	}
	if err := json.Unmarshal([]byte(evaluator.requests[0].Input.Summary), &payload); err != nil {
		t.Fatalf("batch evaluator summary is not structured JSON: %v\n%s", err, evaluator.requests[0].Input.Summary)
	}
	if payload.SchemaVersion != agentAffectSchemaVersion || payload.JobCount != 3 || payload.MoodOwnerID != "persona:default" || len(payload.EventBatch.Turns) != 3 {
		t.Fatalf("batch payload = %#v", payload)
	}
	if payload.EventBatch.Turns[0].Ordinal != 1 || payload.EventBatch.Turns[2].Ordinal != 3 {
		t.Fatalf("batch ordinals = %#v", payload.EventBatch.Turns)
	}
	for _, forbidden := range []string{"current_mood_before_batch", "recent_evaluations", "previous_evaluations", "dimension_limits", "turn-1", "session-1"} {
		if strings.Contains(evaluator.requests[0].Input.Summary, forbidden) {
			t.Fatalf("batch summary contains forbidden %q: %s", forbidden, evaluator.requests[0].Input.Summary)
		}
	}
	for table, want := range map[string]int{
		"agent_affect_evaluations": 1,
		"agent_affect_states":      1,
		"agent_affect_events":      1,
	} {
		var got int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if got != want {
			t.Fatalf("%s count = %d, want %d", table, got, want)
		}
	}
	var doneJobs, doneBatches int
	if err := db.QueryRow("SELECT COUNT(*) FROM agent_affect_jobs WHERE status = 'done' AND result_evaluation_id <> '' AND result_event_id <> ''").Scan(&doneJobs); err != nil {
		t.Fatalf("count done jobs: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM agent_affect_job_batches WHERE status = 'done' AND evaluation_id <> '' AND affect_event_id <> ''").Scan(&doneBatches); err != nil {
		t.Fatalf("count done batches: %v", err)
	}
	if doneJobs != 3 || doneBatches != 1 {
		t.Fatalf("done jobs/batches = %d/%d, want 3/1", doneJobs, doneBatches)
	}
	var rawCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM agent_affect_jobs WHERE COALESCE(user_text, '') <> '' OR COALESCE(assistant_text, '') <> '' OR COALESCE(memory_prompt_block, '') <> ''").Scan(&rawCount); err != nil {
		t.Fatalf("count raw payload: %v", err)
	}
	if rawCount != 0 {
		t.Fatalf("raw payload rows = %d, want cleared by worker", rawCount)
	}
	var evalBatchID, eventBatchID string
	if err := db.QueryRow("SELECT COALESCE(batch_id, '') FROM agent_affect_evaluations").Scan(&evalBatchID); err != nil {
		t.Fatalf("read eval batch_id: %v", err)
	}
	if err := db.QueryRow("SELECT COALESCE(batch_id, '') FROM agent_affect_events").Scan(&eventBatchID); err != nil {
		t.Fatalf("read event batch_id: %v", err)
	}
	if evalBatchID == "" || evalBatchID != eventBatchID {
		t.Fatalf("batch ids eval=%q event=%q, want same non-empty", evalBatchID, eventBatchID)
	}
}

func TestProcessNextBatchDoesNotCommitIfMoodChangedDuringEvaluation(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	cfg.Enabled = true
	var rt *Runtime
	evaluator := &recordingEvaluator{
		result: LLMEvaluationResult{Delta: MoodVector{Warmth: 0.2}, Label: "warmer", Confidence: 0.7},
		onCall: func() {
			if _, err := rt.ResetMood(context.Background(), ResetMoodRequest{PersonaID: "default", SessionID: "session-1", Reason: "manual reset during evaluate"}); err != nil {
				t.Fatalf("ResetMood during evaluate: %v", err)
			}
		},
	}
	var db *sql.DB
	rt, db = newTestRuntime(t, cfg, evaluator)
	if _, err := rt.EnqueueTurnEvaluationJob(context.Background(), EnqueueTurnEvaluationJobRequest{
		PersonaID:     "default",
		SessionID:     "session-1",
		TurnID:        "turn-1",
		UserText:      "user",
		AssistantText: "assistant",
		Trigger:       TriggerDescriptor{TriggerType: "user_message"},
	}); err != nil {
		t.Fatalf("EnqueueTurnEvaluationJob: %v", err)
	}

	processed, err := rt.ProcessNextBatch(context.Background(), "worker-1")
	if err == nil {
		t.Fatal("ProcessNextBatch error = nil, want stale state error")
	}
	if !processed {
		t.Fatal("ProcessNextBatch processed = false")
	}
	var evaluations, states, events, failedJobs int
	if err := db.QueryRow("SELECT COUNT(*) FROM agent_affect_evaluations").Scan(&evaluations); err != nil {
		t.Fatalf("count evaluations: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM agent_affect_states").Scan(&states); err != nil {
		t.Fatalf("count states: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM agent_affect_events").Scan(&events); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM agent_affect_jobs WHERE status = 'failed'").Scan(&failedJobs); err != nil {
		t.Fatalf("count failed jobs: %v", err)
	}
	if evaluations != 0 || states != 1 || events != 1 || failedJobs != 1 {
		t.Fatalf("eval/state/event/failedJobs = %d/%d/%d/%d, want 0/1/1/1", evaluations, states, events, failedJobs)
	}
}

func TestProcessNextBatchDoesNotCommitIfClaimExpiredAndReclaimed(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	cfg.Enabled = true
	cfg.Async.QueueClaimTTLSeconds = 1
	var rt *Runtime
	evaluator := &recordingEvaluator{
		result: LLMEvaluationResult{Delta: MoodVector{Warmth: 0.2}, Label: "warmer", Confidence: 0.7},
		onCall: func() {
			batch, err := rt.store.ClaimNextBatch(context.Background(), "worker-2", rt.now().Add(2*time.Second), ClaimBatchOptions{MaxJobs: 1, ClaimTTL: time.Minute})
			if err != nil {
				t.Fatalf("ClaimNextBatch after expiry: %v", err)
			}
			if batch == nil {
				t.Fatal("ClaimNextBatch after expiry = nil, want re-claimed batch")
			}
		},
	}
	var db *sql.DB
	rt, db = newTestRuntime(t, cfg, evaluator)
	if _, err := rt.EnqueueTurnEvaluationJob(context.Background(), EnqueueTurnEvaluationJobRequest{
		PersonaID:     "default",
		SessionID:     "session-1",
		TurnID:        "turn-1",
		UserText:      "user",
		AssistantText: "assistant",
		Trigger:       TriggerDescriptor{TriggerType: "user_message"},
	}); err != nil {
		t.Fatalf("EnqueueTurnEvaluationJob: %v", err)
	}

	processed, err := rt.ProcessNextBatch(context.Background(), "worker-1")
	if !errors.Is(err, ErrAffectBatchNotRunning) {
		t.Fatalf("ProcessNextBatch error = %v, want ErrAffectBatchNotRunning", err)
	}
	if !processed {
		t.Fatal("ProcessNextBatch processed = false")
	}
	var evaluations, states, events, failedBatches, runningJobs int
	for table, ptr := range map[string]*int{
		"agent_affect_evaluations": &evaluations,
		"agent_affect_states":      &states,
		"agent_affect_events":      &events,
	} {
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(ptr); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM agent_affect_job_batches WHERE status = 'failed'").Scan(&failedBatches); err != nil {
		t.Fatalf("count failed batches: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM agent_affect_jobs WHERE status = 'running'").Scan(&runningJobs); err != nil {
		t.Fatalf("count running jobs: %v", err)
	}
	if evaluations != 0 || states != 0 || events != 0 || failedBatches != 1 || runningJobs != 1 {
		t.Fatalf("eval/state/event/failedBatches/runningJobs = %d/%d/%d/%d/%d, want 0/0/0/1/1", evaluations, states, events, failedBatches, runningJobs)
	}
}

func TestProcessNextBatchDoesNotMergeDifferentPersonas(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	cfg.Enabled = true
	evaluator := &recordingEvaluator{result: LLMEvaluationResult{Delta: MoodVector{Warmth: 0.1}, Label: "warmer", Confidence: 0.7}}
	rt, db := newTestRuntime(t, cfg, evaluator)

	for _, personaID := range []string{"default", "other"} {
		if _, err := rt.EnqueueTurnEvaluationJob(context.Background(), EnqueueTurnEvaluationJobRequest{
			PersonaID:     personaID,
			SessionID:     personaID + "-session",
			TurnID:        personaID + "-turn",
			UserText:      "user",
			AssistantText: "assistant",
			Trigger:       TriggerDescriptor{TriggerType: "user_message"},
		}); err != nil {
			t.Fatalf("EnqueueTurnEvaluationJob: %v", err)
		}
	}

	for i := 0; i < 2; i++ {
		processed, err := rt.ProcessNextBatch(context.Background(), "worker-1")
		if err != nil {
			t.Fatalf("ProcessNextBatch %d: %v", i, err)
		}
		if !processed {
			t.Fatalf("ProcessNextBatch %d processed = false", i)
		}
	}
	if evaluator.calls != 2 {
		t.Fatalf("evaluator calls = %d, want 2", evaluator.calls)
	}
	var batches int
	if err := db.QueryRow("SELECT COUNT(*) FROM agent_affect_job_batches WHERE status = 'done'").Scan(&batches); err != nil {
		t.Fatalf("count batches: %v", err)
	}
	if batches != 2 {
		t.Fatalf("done batches = %d, want 2", batches)
	}
}

func TestClaimNextBatchRunningOlderJobBlocksSameOwner(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Date(2026, 6, 10, 10, 0, 0, 0, time.UTC)
	enqueueTurnJob(t, store, "default", "session-1", "turn-1", now)
	first, err := store.ClaimNextBatch(context.Background(), "worker-1", now.Add(time.Second), ClaimBatchOptions{MaxJobs: 1, ClaimTTL: time.Minute})
	if err != nil {
		t.Fatalf("ClaimNextBatch first: %v", err)
	}
	if first == nil {
		t.Fatal("first batch = nil")
	}
	enqueueTurnJob(t, store, "default", "session-1", "turn-2", now.Add(2*time.Second))

	second, err := store.ClaimNextBatch(context.Background(), "worker-2", now.Add(3*time.Second), ClaimBatchOptions{MaxJobs: 1, ClaimTTL: time.Minute})
	if err != nil {
		t.Fatalf("ClaimNextBatch second: %v", err)
	}
	if second != nil {
		t.Fatalf("second batch = %#v, want nil while older same-owner job is running", second)
	}
}

func TestProcessNextBatchFailedEvaluateRetriesWithBackoff(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	cfg.Enabled = true
	cfg.Async.RetryBaseDelaySeconds = 30
	cfg.Async.RetryMaxDelaySeconds = 900
	evaluator := &recordingEvaluator{err: errors.New("llm down")}
	rt, db := newTestRuntime(t, cfg, evaluator)

	if _, err := rt.EnqueueTurnEvaluationJob(context.Background(), EnqueueTurnEvaluationJobRequest{
		PersonaID:     "default",
		SessionID:     "session-1",
		TurnID:        "turn-1",
		UserText:      "user",
		AssistantText: "assistant",
		Trigger:       TriggerDescriptor{TriggerType: "user_message"},
		MaxAttempts:   3,
	}); err != nil {
		t.Fatalf("EnqueueTurnEvaluationJob: %v", err)
	}

	processed, err := rt.ProcessNextBatch(context.Background(), "worker-1")
	if err == nil {
		t.Fatal("ProcessNextBatch error = nil, want evaluator error after marking retry")
	}
	if !processed {
		t.Fatal("ProcessNextBatch processed = false, want true")
	}
	var status, batchID, runAfter string
	var attempts int
	if err := db.QueryRow("SELECT status, attempts, COALESCE(batch_id, ''), run_after FROM agent_affect_jobs WHERE turn_id = 'turn-1'").Scan(&status, &attempts, &batchID, &runAfter); err != nil {
		t.Fatalf("read job: %v", err)
	}
	if status != AffectJobStatusPending || attempts != 1 || batchID != "" {
		t.Fatalf("job status/attempts/batch = %q/%d/%q, want pending/1/empty", status, attempts, batchID)
	}
	if !parseDBTime(runAfter).After(rt.now()) {
		t.Fatalf("run_after = %q, want after now", runAfter)
	}
}

func TestLongRunThousandBatchesKeepsPromptBoundedAndRawCleared(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	cfg.Enabled = true
	cfg.Context.StoreRawInputs = false
	cfg.Context.MaxInputTokens = 1200
	cfg.Context.MaxUserCharsPerTurn = 80
	cfg.Context.MaxAssistantCharsPerTurn = 80
	cfg.Context.MaxMemoryContextChars = 80
	cfg.Async.Batch.MaxJobs = 1
	client := &fakeLLMClient{resp: &llm.ChatResponse{
		Model: "fake-affect",
		Usage: llm.Usage{InputTokens: 321, OutputTokens: 22},
		Content: `{
			"schema_version": "agent_affect.v3.appraisal.v1",
			"appraisal": {
				"event_significance": 0.2,
				"novelty": 0.1,
				"goal_relevance": 0.1,
				"relationship_impact": 0.1,
				"boundary_impact": 0,
				"uncertainty": 0.1
			},
			"delta": {
				"valence": 0,
				"arousal": 0,
				"dominance": 0,
				"energy": 0,
				"warmth": 0.01,
				"concern": 0,
				"curiosity": 0,
				"playfulness": 0,
				"attachment": 0,
				"frustration": 0,
				"uncertainty": 0
			},
			"label": "steady",
			"cause": {
				"code": "ordinary_batch",
				"summary": "Compact batch appraisal.",
				"visible_summary": "Compact batch appraisal.",
				"tags": ["ordinary"]
			},
			"confidence": 0.8
		}`,
	}}
	rt, db := newTestRuntime(t, cfg, NewLLMEvaluator(client, cfg))

	for i := 0; i < 1000; i++ {
		turnID := fmt.Sprintf("turn-%04d", i)
		if _, err := rt.EnqueueTurnEvaluationJob(context.Background(), EnqueueTurnEvaluationJobRequest{
			PersonaID:         "default",
			SessionID:         "session-1",
			TurnID:            turnID,
			UserText:          strings.Repeat("用户输入", 80),
			AssistantText:     strings.Repeat("助手回复", 80),
			MemoryPromptBlock: strings.Repeat("记忆片段", 80),
			Trigger:           TriggerDescriptor{TriggerType: "user_message"},
		}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
		processed, err := rt.ProcessNextBatch(context.Background(), "worker-1")
		if err != nil {
			t.Fatalf("ProcessNextBatch %d: %v", i, err)
		}
		if !processed {
			t.Fatalf("ProcessNextBatch %d processed = false", i)
		}
	}
	if len(client.requests) != 1000 {
		t.Fatalf("llm calls = %d, want 1000", len(client.requests))
	}
	minPrompt := 1 << 30
	maxPrompt := 0
	for _, req := range client.requests {
		prompt := req.System + "\n" + req.Messages[0].Content
		if strings.Contains(prompt, "previous_evaluations") || strings.Contains(prompt, "recent_evaluations") {
			t.Fatalf("prompt contains recursive history keys: %s", prompt)
		}
		size := len([]rune(prompt))
		if size < minPrompt {
			minPrompt = size
		}
		if size > maxPrompt {
			maxPrompt = size
		}
		if estimateTokens(prompt) > cfg.Context.MaxInputTokens {
			t.Fatalf("prompt estimate = %d, limit = %d", estimateTokens(prompt), cfg.Context.MaxInputTokens)
		}
	}
	if maxPrompt-minPrompt > 600 {
		t.Fatalf("prompt size drift = %d, min=%d max=%d", maxPrompt-minPrompt, minPrompt, maxPrompt)
	}
	t.Logf("prompt budget sample: strategy=checkpoint_trace_v1 min_prompt_chars=%d max_prompt_chars=%d max_estimated_tokens<=%d calls=%d actual_input_tokens=321 actual_output_tokens=22", minPrompt, maxPrompt, cfg.Context.MaxInputTokens, len(client.requests))

	var causeStackJSON string
	if err := db.QueryRow("SELECT cause_stack_json FROM agent_affect_states ORDER BY updated_at DESC LIMIT 1").Scan(&causeStackJSON); err != nil {
		t.Fatalf("read cause stack: %v", err)
	}
	var stack []CauseContributor
	if err := json.Unmarshal([]byte(causeStackJSON), &stack); err != nil {
		t.Fatalf("decode cause stack: %v", err)
	}
	if len(stack) > 5 {
		t.Fatalf("trace items = %d, want <= 5", len(stack))
	}

	var rawJobs, rawBatches, rawEvals int
	if err := db.QueryRow("SELECT COUNT(*) FROM agent_affect_jobs WHERE COALESCE(user_text, '') <> '' OR COALESCE(assistant_text, '') <> '' OR COALESCE(input_summary, '') <> '' OR COALESCE(memory_prompt_block, '') <> ''").Scan(&rawJobs); err != nil {
		t.Fatalf("count raw jobs: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM agent_affect_job_batches WHERE COALESCE(batch_input_summary, '') <> '' OR COALESCE(context_window_snapshot_json, '') <> ''").Scan(&rawBatches); err != nil {
		t.Fatalf("count raw batches: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM agent_affect_evaluations WHERE COALESCE(input_text, '') <> '' OR COALESCE(input_summary, '') <> '' OR COALESCE(prompt_snapshot, '') <> ''").Scan(&rawEvals); err != nil {
		t.Fatalf("count raw evals: %v", err)
	}
	if rawJobs != 0 || rawBatches != 0 || rawEvals != 0 {
		t.Fatalf("raw residual jobs/batches/evals = %d/%d/%d, want 0", rawJobs, rawBatches, rawEvals)
	}

	var evalsWithUsage int
	if err := db.QueryRow("SELECT COUNT(*) FROM agent_affect_evaluations WHERE prompt_chars > 0 AND estimated_input_tokens > 0 AND actual_input_tokens = 321 AND actual_output_tokens = 22 AND context_strategy = 'checkpoint_trace_v1'").Scan(&evalsWithUsage); err != nil {
		t.Fatalf("count usage evals: %v", err)
	}
	if evalsWithUsage != 1000 {
		t.Fatalf("evals with usage = %d, want 1000", evalsWithUsage)
	}
}
