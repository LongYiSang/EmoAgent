package proactive

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/storage"
)

// End-to-end over the decision chain: propose → valves → target → gate →
// recorded verdict. Delivery itself is covered in internal/app; what this pins
// down is that a candidate cannot become a message without passing every gate,
// and that every outcome leaves a record.

func gateReturning(t *testing.T, body string) *Gate {
	t.Helper()
	return NewGate(&stubClient{resp: &llm.ChatResponse{Content: body, Usage: llm.Usage{TotalTokens: 500}}},
		"test-model", llm.RequestParams{}, config.DefaultProactiveConfig().Gate, nil)
}

func TestEndToEndSpeakPathRecordsEverything(t *testing.T) {
	db := runnerTestDB(t)
	ctx := context.Background()
	seedReachableUser(t, db, time.Now().Add(-time.Hour))
	seedCandidate(t, db, "cand-1")

	gate := gateReturning(t, `{"decision":"speak","reason":"卡了 40 分钟","urgency":0.8,"hint":"他在调测试"}`)
	runner := NewRunner(db, gate, fakePresence{}, fakeQuiet{}, nil, nil)

	eval, err := runner.Evaluate(ctx, enabledRunnerConfig(), "default")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if eval.Decision.Verdict != VerdictSpeak {
		t.Fatalf("verdict = %q (%s)", eval.Decision.Verdict, eval.Decision.Reason)
	}

	decisions, err := db.ListProactiveDecisions(ctx, "default", 10)
	if err != nil {
		t.Fatalf("ListProactiveDecisions: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("decisions = %d, want 1", len(decisions))
	}
	record := decisions[0]
	if record.Decision != storage.ProactiveDecisionSpeak {
		t.Fatalf("decision = %q, want speak", record.Decision)
	}
	if record.OriginKey != "onebot:private:123" {
		t.Fatalf("origin_key = %q, want the resolved target", record.OriginKey)
	}
	if record.Hint != "他在调测试" || record.Urgency != 0.8 {
		t.Fatalf("record = %#v, want the gate's hint and urgency", record)
	}
	if record.GateModel != "test-model" || record.GateTokens != 500 {
		t.Fatalf("gate metrics = %#v, want them recorded for tuning", record)
	}
	if len(record.CandidateIDs) != 1 || record.CandidateIDs[0] != "cand-1" {
		t.Fatalf("candidate_ids = %#v, want the backing candidate", record.CandidateIDs)
	}
}

func TestEndToEndGateSkipIsFullyAudited(t *testing.T) {
	db := runnerTestDB(t)
	ctx := context.Background()
	seedReachableUser(t, db, time.Now().Add(-time.Hour))
	seedCandidate(t, db, "cand-1")

	gate := gateReturning(t, `{"decision":"skip","reason":"他好像正专注","urgency":0,"hint":""}`)
	runner := NewRunner(db, gate, fakePresence{}, fakeQuiet{}, nil, nil)

	eval, err := runner.Evaluate(ctx, enabledRunnerConfig(), "default")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if eval.Decision.Verdict != VerdictSkip {
		t.Fatalf("verdict = %q, want skip", eval.Decision.Verdict)
	}

	decisions, _ := db.ListProactiveDecisions(ctx, "default", 10)
	if len(decisions) != 1 || decisions[0].Reason != "他好像正专注" {
		t.Fatalf("decisions = %#v, want the model's reason preserved", decisions)
	}

	// The candidate survives for the injector and for the next gate pass.
	candidates, _ := db.ListProactiveCandidates(ctx, storage.ProactiveCandidateFilter{
		PersonaKey: "default",
		Statuses:   []string{storage.ProactiveCandidateStatusSkipped},
	})
	if len(candidates) != 1 || candidates[0].SkipCount != 1 {
		t.Fatalf("candidates = %#v, want the candidate kept with skip_count 1", candidates)
	}
}

// A gate that breaks must produce silence, not a message.
func TestEndToEndBrokenGateStaysSilent(t *testing.T) {
	db := runnerTestDB(t)
	ctx := context.Background()
	seedReachableUser(t, db, time.Now().Add(-time.Hour))
	seedCandidate(t, db, "cand-1")

	runner := NewRunner(db, gateReturning(t, "服务器炸了，这不是 JSON"), fakePresence{}, fakeQuiet{}, nil, nil)
	eval, err := runner.Evaluate(ctx, enabledRunnerConfig(), "default")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if eval.Decision.Verdict != VerdictSkip || eval.Decision.Reason != ReasonGateError {
		t.Fatalf("decision = %#v, want skip/gate_error", eval.Decision)
	}
}

// The skipped candidate is not wasted: it becomes context for the next turn the
// user starts. This is the value the feature delivers even when it never speaks.
func TestEndToEndSkippedCandidateBecomesAmbientContext(t *testing.T) {
	db := runnerTestDB(t)
	ctx := context.Background()
	seedReachableUser(t, db, time.Now().Add(-time.Hour))
	seedCandidate(t, db, "cand-1")

	runner := NewRunner(db, gateReturning(t, `{"decision":"skip","reason":"不打扰","urgency":0,"hint":""}`),
		fakePresence{}, fakeQuiet{}, nil, nil)
	if _, err := runner.Evaluate(ctx, enabledRunnerConfig(), "default"); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	block := NewInjector(db).Block(ctx, config.DefaultProactiveConfig(), "default")
	if block == "" {
		t.Fatal("ambient block is empty; the skipped candidate produced no value at all")
	}
	if !strings.Contains(block, "反复跑 go test") {
		t.Fatalf("block = %q, want the skipped activity", block)
	}
}
