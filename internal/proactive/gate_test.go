package proactive

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
)

type stubClient struct {
	resp    *llm.ChatResponse
	err     error
	delay   time.Duration
	lastReq llm.ChatRequest
}

func (c *stubClient) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	c.lastReq = req
	if c.delay > 0 {
		select {
		case <-time.After(c.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return c.resp, c.err
}

func (c *stubClient) ChatStream(ctx context.Context, req llm.ChatRequest, cb llm.StreamCallback) (*llm.ChatResponse, error) {
	return c.Chat(ctx, req)
}

func testGate(t *testing.T, client llm.Client) *Gate {
	t.Helper()
	cfg := config.DefaultProactiveConfig().Gate
	cfg.TimeoutMS = 200
	return NewGate(client, "test-model", llm.RequestParams{}, cfg, nil)
}

func sampleInput() GateInput {
	return GateInput{
		Candidates: []GateCandidate{{
			EventType:  "stuck",
			Summary:    "反复跑 go test，终端持续报错",
			Importance: 0.7,
		}},
		InterruptionCost: GateInterruptionCost{MinutesSinceUserMessage: 37},
	}
}

func TestGateAcceptsSpeakVerdict(t *testing.T) {
	client := &stubClient{resp: &llm.ChatResponse{
		Content: `{"decision":"speak","reason":"卡了很久","urgency":0.72,"hint":"他在调测试"}`,
		Usage:   llm.Usage{TotalTokens: 480},
	}}
	decision := testGate(t, client).Decide(context.Background(), "default", sampleInput())

	if decision.Verdict != VerdictSpeak {
		t.Fatalf("verdict = %q, want speak", decision.Verdict)
	}
	if decision.Hint != "他在调测试" {
		t.Fatalf("hint = %q", decision.Hint)
	}
	if decision.Urgency != 0.72 {
		t.Fatalf("urgency = %v, want 0.72", decision.Urgency)
	}
	if decision.GateTokens != 480 || decision.GateModel != "test-model" {
		t.Fatalf("gate metrics = %#v", decision)
	}
	if !decision.GateConsulted {
		t.Fatal("GateConsulted = false, want true")
	}
}

func TestGateAcceptsSkipVerdict(t *testing.T) {
	client := &stubClient{resp: &llm.ChatResponse{
		Content: `{"decision":"skip","reason":"刚聊完没多久","urgency":0,"hint":""}`,
	}}
	decision := testGate(t, client).Decide(context.Background(), "default", sampleInput())
	if decision.Verdict != VerdictSkip {
		t.Fatalf("verdict = %q, want skip", decision.Verdict)
	}
	if decision.Reason != "刚聊完没多久" {
		t.Fatalf("reason = %q, want the model's reason preserved", decision.Reason)
	}
}

// Models wrap JSON in prose constantly; the parser must dig it out.
func TestGateParsesJSONWrappedInProse(t *testing.T) {
	client := &stubClient{resp: &llm.ChatResponse{
		Content: "让我想想。\n```json\n{\"decision\":\"speak\",\"reason\":\"ok\",\"urgency\":0.5,\"hint\":\"h\"}\n```\n就这样。",
	}}
	decision := testGate(t, client).Decide(context.Background(), "default", sampleInput())
	if decision.Verdict != VerdictSpeak {
		t.Fatalf("verdict = %q, want speak", decision.Verdict)
	}
}

func TestGateClampsUrgencyOutOfRange(t *testing.T) {
	client := &stubClient{resp: &llm.ChatResponse{
		Content: `{"decision":"speak","reason":"r","urgency":9.9,"hint":"h"}`,
	}}
	decision := testGate(t, client).Decide(context.Background(), "default", sampleInput())
	if decision.Urgency != 1 {
		t.Fatalf("urgency = %v, want clamped to 1", decision.Urgency)
	}
}

// Every failure mode below must resolve to silence. An error must never be able
// to cause an unsolicited message.
func TestGateFailsClosedOnCallError(t *testing.T) {
	client := &stubClient{err: errors.New("upstream exploded")}
	decision := testGate(t, client).Decide(context.Background(), "default", sampleInput())
	if decision.Verdict != VerdictSkip || decision.Reason != ReasonGateError {
		t.Fatalf("decision = %#v, want skip/gate_error", decision)
	}
}

func TestGateFailsClosedOnTimeout(t *testing.T) {
	client := &stubClient{
		delay: 2 * time.Second,
		resp:  &llm.ChatResponse{Content: `{"decision":"speak","reason":"r","urgency":1,"hint":"h"}`},
	}
	decision := testGate(t, client).Decide(context.Background(), "default", sampleInput())
	if decision.Verdict != VerdictSkip || decision.Reason != ReasonGateError {
		t.Fatalf("decision = %#v, want skip/gate_error", decision)
	}
}

func TestGateFailsClosedOnMalformedJSON(t *testing.T) {
	client := &stubClient{resp: &llm.ChatResponse{Content: `{"decision": "spea`}}
	decision := testGate(t, client).Decide(context.Background(), "default", sampleInput())
	if decision.Verdict != VerdictSkip || decision.Reason != ReasonGateError {
		t.Fatalf("decision = %#v, want skip/gate_error", decision)
	}
}

func TestGateFailsClosedOnUnknownVerdict(t *testing.T) {
	client := &stubClient{resp: &llm.ChatResponse{
		Content: `{"decision":"maybe","reason":"r","urgency":0.5,"hint":"h"}`,
	}}
	decision := testGate(t, client).Decide(context.Background(), "default", sampleInput())
	if decision.Verdict != VerdictSkip || decision.Reason != ReasonGateError {
		t.Fatalf("decision = %#v, want skip/gate_error", decision)
	}
}

func TestGateFailsClosedOnEmptyResponse(t *testing.T) {
	client := &stubClient{resp: &llm.ChatResponse{}}
	decision := testGate(t, client).Decide(context.Background(), "default", sampleInput())
	if decision.Verdict != VerdictSkip || decision.Reason != ReasonGateError {
		t.Fatalf("decision = %#v, want skip/gate_error", decision)
	}
}

func TestGateFailsClosedOnNilResponse(t *testing.T) {
	client := &stubClient{}
	decision := testGate(t, client).Decide(context.Background(), "default", sampleInput())
	if decision.Verdict != VerdictSkip || decision.Reason != ReasonGateError {
		t.Fatalf("decision = %#v, want skip/gate_error", decision)
	}
}

func TestGateFailsClosedWithoutClient(t *testing.T) {
	gate := NewGate(nil, "m", llm.RequestParams{}, config.DefaultProactiveConfig().Gate, nil)
	decision := gate.Decide(context.Background(), "default", sampleInput())
	if decision.Verdict != VerdictSkip || decision.Reason != ReasonNotConfigured {
		t.Fatalf("decision = %#v, want skip/not_configured", decision)
	}
}

// The gate must never receive raw conversation text: it runs on a different
// model than Emotion, and this agent holds intimate conversations.
func TestGateRequestCarriesOnlyStructuredInput(t *testing.T) {
	client := &stubClient{resp: &llm.ChatResponse{
		Content: `{"decision":"skip","reason":"r","urgency":0,"hint":""}`,
	}}
	gate := testGate(t, client)
	in := sampleInput()
	in.Relationship = GateRelationship{SessionGoal: "调试 turn 管线"}
	gate.Decide(context.Background(), "default", in)

	if len(client.lastReq.Messages) != 1 {
		t.Fatalf("messages = %d, want exactly one structured payload", len(client.lastReq.Messages))
	}
	if client.lastReq.Temperature != 0 {
		t.Fatalf("temperature = %v, want 0 for a classifier", client.lastReq.Temperature)
	}
	if client.lastReq.Stream {
		t.Fatal("gate request must not stream")
	}
}

func TestGateOverridesModelFromConfig(t *testing.T) {
	client := &stubClient{resp: &llm.ChatResponse{
		Content: `{"decision":"skip","reason":"r","urgency":0,"hint":""}`,
	}}
	cfg := config.DefaultProactiveConfig().Gate
	cfg.Model = "cheap-model"
	gate := NewGate(client, "main-model", llm.RequestParams{}, cfg, nil)
	decision := gate.Decide(context.Background(), "default", sampleInput())

	if client.lastReq.Model != "cheap-model" || decision.GateModel != "cheap-model" {
		t.Fatalf("model = %q / %q, want cheap-model", client.lastReq.Model, decision.GateModel)
	}
}
