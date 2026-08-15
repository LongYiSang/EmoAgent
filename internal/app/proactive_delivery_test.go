package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/platform"
	"github.com/longyisang/emoagent/internal/storage"
)

// These tests exercise the last mile — Tick → gate → Emotion turn → platform
// sink — which every other test stops short of. Without them the pipeline is
// only proven piece by piece, never end to end.

// fakeOutboundAdapter stands in for a running OneBot adapter. PlatformService
// finds sinks by type-asserting for OutboundSink(), so this is all it takes.
type fakeOutboundAdapter struct {
	sink *platform.BufferedPlatformSink
}

func (a *fakeOutboundAdapter) Start(context.Context, platform.InboundHandler) error { return nil }
func (a *fakeOutboundAdapter) Stop(context.Context) error                           { return nil }
func (a *fakeOutboundAdapter) OutboundSink() (platform.OutboundSink, bool) {
	return a.sink, true
}

type proactiveDeliveryFixture struct {
	app  *App
	db   *storage.DB
	sink *platform.BufferedPlatformSink
}

// newProactiveDeliveryFixture wires a full app with a reachable QQ origin, an
// LLM that answers both the gate and Emotion, and a buffered platform sink.
func newProactiveDeliveryFixture(t *testing.T, gateVerdict string, emotionReply string) proactiveDeliveryFixture {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		// The gate is a non-streaming classifier; the Emotion turn streams. Answer
		// each in its own protocol, or the reply silently comes back empty.
		if strings.Contains(body, `"stream":true`) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(`data: {"id":"resp-1","model":"bound-model","choices":[{"delta":{"content":` +
				jsonQuote(emotionReply) + `}}]}` + "\n\n"))
			_, _ = w.Write([]byte(`data: {"id":"resp-1","model":"bound-model","choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n\n"))
			return
		}
		content := emotionReply
		if strings.Contains(body, "打断时机") {
			content = gateVerdict
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-1","model":"bound-model","choices":[{"message":{"role":"assistant","content":` +
			jsonQuote(content) + `},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(server.Close)

	app, db, _, _ := newTestPlatformGateway(t)
	cfg := testConfig(app)
	cfg.Chat.TurnPipeline.Enabled = true
	cfg.Chat.TurnPipeline.MemoryStages = true
	cfg.Chat.PromptRouter.Mode = config.PromptRouterModeAlwaysCasual
	cfg.Chat.ReplyDelivery.Enabled = false
	cfg.Proactive = config.DefaultProactiveConfig()
	cfg.Proactive.Enabled = true
	cfg.Proactive.QuietHours = nil
	cfg.AgentConfigs = []config.AgentConfig{{ID: "default-agent", PersonaKey: "default"}}
	upsertPlatformGatewayAgent(t, app, "default-agent", "default", server.URL)

	if err := app.kernel.Services.AgentRuntime.Activate("default-agent"); err != nil {
		t.Fatalf("AgentRuntime.Activate: %v", err)
	}

	sink := &platform.BufferedPlatformSink{}
	app.kernel.Services.Platforms.enabled = true
	app.kernel.Services.Platforms.Manager().Register("onebot-1", &fakeOutboundAdapter{sink: sink})

	seedProactiveReachableOrigin(t, db)
	app.kernel.Services.Proactive.Configure(cfg.Proactive)

	return proactiveDeliveryFixture{app: app, db: db, sink: sink}
}

func seedProactiveReachableOrigin(t *testing.T, db *storage.DB) {
	t.Helper()
	ctx := context.Background()
	sessionID := "session-proactive"

	if err := db.CreateSession(ctx, sessionID, "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := db.UpsertConversationOrigin(ctx, storage.ConversationOriginRecord{
		ID:                     "origin-1",
		OriginKey:              "onebot:private:10001",
		SourceType:             "platform",
		AdapterInstanceID:      "onebot-1",
		PlatformID:             "qq",
		ChannelType:            "private",
		ExternalConversationID: "10001",
		ExternalActorID:        "10001",
		MetadataJSON:           "{}",
	}); err != nil {
		t.Fatalf("UpsertConversationOrigin: %v", err)
	}
	if err := db.UpsertConversationBinding(ctx, storage.ConversationBindingRecord{
		ID:               "binding-1",
		OriginKey:        "onebot:private:10001",
		PersonaKey:       "default",
		CurrentSessionID: sessionID,
		VariablesJSON:    "{}",
	}); err != nil {
		t.Fatalf("UpsertConversationBinding: %v", err)
	}
	// A proactive message is never the first thing said on a channel.
	if err := db.AddMessage(ctx, "msg-1", sessionID, "user", "在忙啥"); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
}

func seedDeliveryCandidate(t *testing.T, db *storage.DB) {
	t.Helper()
	now := time.Now()
	err := db.InsertProactiveCandidate(context.Background(), storage.ProactiveCandidateRecord{
		ID:           "cand-1",
		PersonaKey:   "default",
		EventType:    "stuck",
		Summary:      "反复跑 go test，终端持续报错",
		ObservedFrom: now.Add(-40 * time.Minute).Format(time.RFC3339Nano),
		ObservedTo:   now.Format(time.RFC3339Nano),
		Importance:   0.7,
		ExpiresAt:    now.Add(6 * time.Hour).Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("InsertProactiveCandidate: %v", err)
	}
}

func jsonQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// The whole point: a candidate becomes a real message on a real sink.
func TestProactiveTickDeliversToPlatformSink(t *testing.T) {
	fixture := newProactiveDeliveryFixture(t,
		`{"decision":"speak","reason":"卡了 40 分钟","urgency":0.8,"hint":"他在调测试"}`,
		"卡这么久啦，要不要歇会儿")
	seedDeliveryCandidate(t, fixture.db)
	ctx := context.Background()

	decision, err := fixture.app.kernel.Services.Proactive.Tick(ctx, "default")
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if string(decision.Verdict) != "speak" {
		t.Fatalf("verdict = %q (%s), want speak", decision.Verdict, decision.Reason)
	}

	if len(fixture.sink.Events) == 0 {
		t.Fatal("no events reached the platform sink; the last mile never ran")
	}
	var delivered string
	for _, event := range fixture.sink.Events {
		if event.Origin.OriginKey != "onebot:private:10001" {
			t.Fatalf("event origin = %q, want the resolved QQ origin", event.Origin.OriginKey)
		}
		delivered += event.Content
	}
	if !strings.Contains(delivered, "歇会儿") {
		t.Fatalf("delivered = %q, want Emotion's reply", delivered)
	}

	decisions, err := fixture.db.ListProactiveDecisions(ctx, "default", 10)
	if err != nil {
		t.Fatalf("ListProactiveDecisions: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("decisions = %d, want 1", len(decisions))
	}
	if decisions[0].TurnID == "" || decisions[0].SilencedByEmotion {
		t.Fatalf("decision = %#v, want a delivered turn recorded", decisions[0])
	}

	// A delivered message consumes its candidates so they are not re-proposed.
	consumed, _ := fixture.db.ListProactiveCandidates(ctx, storage.ProactiveCandidateFilter{
		PersonaKey: "default",
		Statuses:   []string{storage.ProactiveCandidateStatusConsumed},
	})
	if len(consumed) != 1 {
		t.Fatalf("consumed candidates = %#v, want the backing candidate consumed", consumed)
	}
}

// Emotion's veto must stop the message at the last moment, after the gate said
// yes and the turn already ran.
func TestProactiveTickSilencedByEmotionSendsNothing(t *testing.T) {
	fixture := newProactiveDeliveryFixture(t,
		`{"decision":"speak","reason":"卡了 40 分钟","urgency":0.8,"hint":"他在调测试"}`,
		"<silent/>")
	seedDeliveryCandidate(t, fixture.db)
	ctx := context.Background()

	if _, err := fixture.app.kernel.Services.Proactive.Tick(ctx, "default"); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	if len(fixture.sink.Events) != 0 {
		t.Fatalf("sink events = %#v, want none after Emotion declined", fixture.sink.Events)
	}
	decisions, _ := fixture.db.ListProactiveDecisions(ctx, "default", 10)
	if len(decisions) != 1 || !decisions[0].SilencedByEmotion {
		t.Fatalf("decisions = %#v, want silenced_by_emotion recorded", decisions)
	}
}

// A gate refusal must not reach the sink or run an Emotion turn at all.
func TestProactiveTickGateSkipSendsNothing(t *testing.T) {
	fixture := newProactiveDeliveryFixture(t,
		`{"decision":"skip","reason":"他好像正专注","urgency":0,"hint":""}`,
		"不该出现的回复")
	seedDeliveryCandidate(t, fixture.db)
	ctx := context.Background()

	decision, err := fixture.app.kernel.Services.Proactive.Tick(ctx, "default")
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if string(decision.Verdict) != "skip" {
		t.Fatalf("verdict = %q, want skip", decision.Verdict)
	}
	if len(fixture.sink.Events) != 0 {
		t.Fatalf("sink events = %#v, want none after a gate refusal", fixture.sink.Events)
	}
}

// The feature must stay silent when it was never switched on, even with a
// candidate waiting and a reachable user.
func TestProactiveTickDisabledSendsNothing(t *testing.T) {
	fixture := newProactiveDeliveryFixture(t,
		`{"decision":"speak","reason":"r","urgency":1,"hint":"h"}`,
		"不该出现的回复")
	seedDeliveryCandidate(t, fixture.db)
	testConfig(fixture.app).Proactive.Enabled = false
	ctx := context.Background()

	decision, err := fixture.app.kernel.Services.Proactive.Tick(ctx, "default")
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if decision.Reason != "disabled" {
		t.Fatalf("reason = %q, want disabled", decision.Reason)
	}
	if len(fixture.sink.Events) != 0 {
		t.Fatalf("sink events = %#v, want none while disabled", fixture.sink.Events)
	}
}
