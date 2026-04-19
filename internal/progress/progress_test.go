package progress

import (
	"context"
	"testing"
	"time"
)

func TestContextHelpers(t *testing.T) {
	ctx := context.Background()
	if got := CallbackFromContext(ctx); got != nil {
		t.Fatalf("CallbackFromContext() = %#v, want nil", got)
	}

	called := false
	cb := func(Event) { called = true }
	ctx = WithCallback(ctx, cb)

	got := CallbackFromContext(ctx)
	if got == nil {
		t.Fatal("CallbackFromContext() returned nil")
	}
	got(Event{Kind: KindStart})
	if !called {
		t.Fatal("callback was not invoked")
	}
}

func TestResolvePrefersPersonaOverride(t *testing.T) {
	event := Event{Kind: KindTool, ToolName: "read_file"}
	phrase := Resolve(event, map[string][]string{
		"read_file": {"override phrase"},
	})
	if phrase != "override phrase" {
		t.Fatalf("Resolve() = %q, want override phrase", phrase)
	}
}

func TestResolveFallsBackToDefaults(t *testing.T) {
	event := Event{Kind: KindTool, ToolName: "read_file"}
	phrase := Resolve(event, nil)
	if phrase == "" {
		t.Fatal("Resolve() returned empty phrase")
	}
	if phrase == "override phrase" {
		t.Fatal("Resolve() unexpectedly used override")
	}
}

func TestResolveUsesDefaultFallback(t *testing.T) {
	event := Event{Kind: KindTool, ToolName: "unknown_tool"}
	phrase := Resolve(event, map[string][]string{
		"_default": {"fallback phrase"},
	})
	if phrase != "fallback phrase" {
		t.Fatalf("Resolve() = %q, want fallback phrase", phrase)
	}
}

func TestResolveMapsLifecycleKeys(t *testing.T) {
	cases := []struct {
		name   string
		event  Event
		key    string
		phrase string
	}{
		{name: "start", event: Event{Kind: KindStart}, key: "_start", phrase: "start phrase"},
		{name: "heartbeat", event: Event{Kind: KindHeartbeat}, key: "_heartbeat", phrase: "heartbeat phrase"},
		{name: "finishing", event: Event{Kind: KindFinishing}, key: "_finishing", phrase: "finishing phrase"},
		{name: "paused", event: Event{Kind: KindPaused}, key: "_paused", phrase: "paused phrase"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			phrase := Resolve(tc.event, map[string][]string{tc.key: {tc.phrase}})
			if phrase != tc.phrase {
				t.Fatalf("Resolve() = %q, want %q", phrase, tc.phrase)
			}
		})
	}
}

func TestThrottlerDedupesToolEventsWithinInterval(t *testing.T) {
	now := time.Unix(1700000000, 0)
	th := newThrottlerWithClock(3*time.Second, func() time.Time { return now })
	event := Event{Kind: KindTool, ToolName: "read_file"}

	if !th.ShouldEmit(event) {
		t.Fatal("first tool event should emit")
	}
	if th.ShouldEmit(event) {
		t.Fatal("second tool event within interval should be throttled")
	}

	now = now.Add(4 * time.Second)
	if !th.ShouldEmit(event) {
		t.Fatal("tool event after interval should emit")
	}
}

func TestThrottlerAlwaysEmitsLifecycleAndHeartbeat(t *testing.T) {
	now := time.Unix(1700000000, 0)
	th := newThrottlerWithClock(3*time.Second, func() time.Time { return now })

	events := []Event{
		{Kind: KindStart},
		{Kind: KindHeartbeat},
		{Kind: KindHeartbeat},
		{Kind: KindFinishing},
		{Kind: KindPaused},
		{Kind: KindEnd},
	}

	for i, event := range events {
		if !th.ShouldEmit(event) {
			t.Fatalf("event[%d] kind=%q should emit", i, event.Kind)
		}
	}
}
