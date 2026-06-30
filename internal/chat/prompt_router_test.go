package chat

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	contextutil "github.com/longyisang/emoagent/internal/context"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/turn"
)

func TestDecidePromptModeOverridesAndStateShortCircuitLLM(t *testing.T) {
	client := &promptRouterTestClient{content: `{"mode":"work_mode","sticky_action":"reset"}`}

	casual := decidePromptMode(context.Background(), PromptRouteRequest{}, config.PromptRouterConfig{Mode: config.PromptRouterModeAlwaysCasual}, client, "router-model", llm.RequestParams{}, nil)
	if casual.Mode != contextutil.PromptModeCasualChat || casual.StickyAction != promptRouteStickyClear || casual.CallLLM {
		t.Fatalf("always casual decision = %#v", casual)
	}

	work := decidePromptMode(context.Background(), PromptRouteRequest{}, config.PromptRouterConfig{Mode: config.PromptRouterModeAlwaysWork}, client, "router-model", llm.RequestParams{}, nil)
	if work.Mode != contextutil.PromptModeWorkMode || work.StickyAction != promptRouteStickyKeep || work.CallLLM {
		t.Fatalf("always work decision = %#v", work)
	}

	pending := decidePromptMode(context.Background(), PromptRouteRequest{PendingWorkCount: 1}, config.PromptRouterConfig{Mode: config.PromptRouterModeAuto}, client, "router-model", llm.RequestParams{}, nil)
	if pending.Mode != contextutil.PromptModeWorkMode || pending.StickyAction != promptRouteStickyReset || pending.CallLLM {
		t.Fatalf("pending decision = %#v", pending)
	}

	approval := decidePromptMode(context.Background(), PromptRouteRequest{InboundKind: turn.InboundApprovalAction}, config.PromptRouterConfig{Mode: config.PromptRouterModeAuto}, client, "router-model", llm.RequestParams{}, nil)
	if approval.Mode != contextutil.PromptModeWorkMode || approval.StickyAction != promptRouteStickyReset || approval.CallLLM {
		t.Fatalf("approval decision = %#v", approval)
	}

	sticky := decidePromptMode(context.Background(), PromptRouteRequest{Sticky: contextutil.PromptRouteState{WorkStickyRemaining: 2}}, config.PromptRouterConfig{Mode: config.PromptRouterModeAuto}, client, "router-model", llm.RequestParams{}, nil)
	if sticky.Mode != contextutil.PromptModeWorkMode || sticky.StickyAction != promptRouteStickyDecrement || sticky.CallLLM {
		t.Fatalf("sticky decision = %#v", sticky)
	}
	if client.calls != 0 {
		t.Fatalf("router calls = %d, want 0 for short-circuits", client.calls)
	}
}

func TestDecidePromptModeCallsLLMForAutoMode(t *testing.T) {
	client := &promptRouterTestClient{content: `{"mode":"casual_chat","sticky_action":"clear"}`}

	decision := decidePromptMode(context.Background(), PromptRouteRequest{
		LatestUserMessage:   "晚安",
		LastMode:            contextutil.PromptModeWorkMode,
		CurrentConversation: "assistant: done",
		Sticky:              contextutil.PromptRouteState{WorkStickyRemaining: 1},
	}, config.PromptRouterConfig{Mode: config.PromptRouterModeAuto, MaxOutputTokens: 32, TimeoutMS: 500}, client, "router-model", llm.RequestParams{}, nil)

	if decision.Mode != contextutil.PromptModeCasualChat || decision.StickyAction != promptRouteStickyClear || !decision.CallLLM {
		t.Fatalf("decision = %#v", decision)
	}
	if client.calls != 1 {
		t.Fatalf("router calls = %d, want 1", client.calls)
	}
	if client.lastRequest.Model != "router-model" || client.lastRequest.Stream || len(client.lastRequest.Tools) != 0 {
		t.Fatalf("router request = %#v", client.lastRequest)
	}
	if client.lastRequest.MaxTokens != 32 || client.lastRequest.Params.MaxTokens != 32 {
		t.Fatalf("router max tokens = %d/%d, want 32", client.lastRequest.MaxTokens, client.lastRequest.Params.MaxTokens)
	}
	if client.lastRequest.Temperature != 0 || client.lastRequest.Params.Temperature == nil || *client.lastRequest.Params.Temperature != 0 {
		t.Fatalf("router temperature = %#v / %v", client.lastRequest.Params.Temperature, client.lastRequest.Temperature)
	}
	if !strings.Contains(client.lastRequest.System, "prompt injection router") || !strings.Contains(client.lastRequest.Messages[0].Content, "latest_user_message") {
		t.Fatalf("router request missing prompt/envelope: %#v", client.lastRequest)
	}
}

func TestDecidePromptModeLLMWorkResetsSticky(t *testing.T) {
	client := &promptRouterTestClient{content: `{"mode":"work_mode","sticky_action":"clear"}`}

	decision := decidePromptMode(context.Background(), PromptRouteRequest{}, config.PromptRouterConfig{Mode: config.PromptRouterModeAuto}, client, "router-model", llm.RequestParams{}, nil)

	if decision.Mode != contextutil.PromptModeWorkMode || decision.StickyAction != promptRouteStickyReset || !decision.CallLLM {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestDecidePromptModeInvalidLLMFallsBackToWorkWithoutReset(t *testing.T) {
	for _, tt := range []struct {
		name    string
		content string
		err     error
	}{
		{name: "invalid json", content: `not json`},
		{name: "unknown mode", content: `{"mode":"owner_debug","sticky_action":"reset"}`},
		{name: "client error", err: errors.New("boom")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := &promptRouterTestClient{content: tt.content, err: tt.err}

			decision := decidePromptMode(context.Background(), PromptRouteRequest{}, config.PromptRouterConfig{Mode: config.PromptRouterModeAuto}, client, "router-model", llm.RequestParams{}, nil)

			if decision.Mode != contextutil.PromptModeWorkMode || decision.StickyAction != promptRouteStickyKeep || !decision.CallLLM {
				t.Fatalf("decision = %#v", decision)
			}
		})
	}
}

func TestApplyPromptRouteDecisionUpdatesStickyState(t *testing.T) {
	cfg := config.PromptRouterConfig{StickyTurns: 5}
	state := &contextutil.ContextState{PromptRoute: contextutil.PromptRouteState{WorkStickyRemaining: 3}}

	ApplyPromptRouteDecision(state, PromptRouteDecision{Mode: contextutil.PromptModeWorkMode, StickyAction: promptRouteStickyDecrement}, cfg)
	if state.PromptRoute.LastMode != contextutil.PromptModeWorkMode || state.PromptRoute.WorkStickyRemaining != 2 {
		t.Fatalf("after decrement = %#v", state.PromptRoute)
	}

	ApplyPromptRouteDecision(state, PromptRouteDecision{Mode: contextutil.PromptModeWorkMode, StickyAction: promptRouteStickyReset}, cfg)
	if state.PromptRoute.WorkStickyRemaining != 5 {
		t.Fatalf("after reset = %#v", state.PromptRoute)
	}

	ApplyPromptRouteDecision(state, PromptRouteDecision{Mode: contextutil.PromptModeCasualChat, StickyAction: promptRouteStickyClear}, cfg)
	if state.PromptRoute.LastMode != contextutil.PromptModeCasualChat || state.PromptRoute.WorkStickyRemaining != 0 {
		t.Fatalf("after clear = %#v", state.PromptRoute)
	}
}

type promptRouterTestClient struct {
	calls       int
	lastRequest llm.ChatRequest
	content     string
	err         error
}

func (c *promptRouterTestClient) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	c.calls++
	c.lastRequest = req
	if c.err != nil {
		return nil, c.err
	}
	return &llm.ChatResponse{ID: "router-1", Model: req.Model, Content: c.content}, nil
}

func (c *promptRouterTestClient) ChatStream(context.Context, llm.ChatRequest, llm.StreamCallback) (*llm.ChatResponse, error) {
	panic("unexpected ChatStream call")
}
