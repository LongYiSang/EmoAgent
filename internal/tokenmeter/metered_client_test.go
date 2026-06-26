package tokenmeter

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/storage"
)

type fakeMeteredLLM struct {
	resp        *llm.ChatResponse
	err         error
	chatCalls   int
	streamCalls int
}

func (f *fakeMeteredLLM) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	f.chatCalls++
	return f.resp, f.err
}

func (f *fakeMeteredLLM) ChatStream(_ context.Context, _ llm.ChatRequest, cb llm.StreamCallback) (*llm.ChatResponse, error) {
	f.streamCalls++
	if cb != nil {
		cb(llm.StreamEvent{Type: "text", Content: "ok"})
		cb(llm.StreamEvent{Done: true})
	}
	return f.resp, f.err
}

type recordingUsageRecorder struct {
	events []storage.LLMUsageEvent
	err    error
}

func (r *recordingUsageRecorder) RecordLLMUsageEvent(_ context.Context, event storage.LLMUsageEvent) error {
	r.events = append(r.events, event)
	return r.err
}

func TestMeteredClientRecordsProviderUsage(t *testing.T) {
	recorder := &recordingUsageRecorder{}
	client := NewMeteredClient(MeteredClientConfig{
		Inner:        &fakeMeteredLLM{resp: &llm.ChatResponse{Content: "ok", Usage: llm.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15, Source: llm.UsageSourceProvider}}},
		Counter:      NewHeuristicCounter(),
		Recorder:     recorder,
		ProviderID:   "deepseek",
		ProviderName: "DeepSeek",
		Protocol:     "openai_compatible",
	})
	ctx := WithUsageScope(context.Background(), UsageScope{Component: "emotion_chat", Operation: "chat_stream", SessionID: "session-1", RequestID: "req-1"})

	resp, err := client.Chat(ctx, llm.ChatRequest{Model: "model-a", Messages: []llm.Message{{Role: llm.RoleUser, Content: "hello"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Usage.UsageEventID == "" {
		t.Fatalf("response usage missing event id: %#v", resp.Usage)
	}
	if len(recorder.events) != 1 {
		t.Fatalf("recorded events = %#v, want one", recorder.events)
	}
	event := recorder.events[0]
	if event.UsageSource != llm.UsageSourceProvider || event.ActualInputTokens != 10 || event.ActualOutputTokens != 5 || event.TotalTokens != 15 {
		t.Fatalf("event = %#v, want provider usage totals", event)
	}
	if event.Component != "emotion_chat" || event.SessionID != "session-1" || event.ProviderID != "deepseek" || event.Model != "model-a" {
		t.Fatalf("event scope = %#v", event)
	}
}

func TestMeteredClientRecordsEstimatedUsageWhenMissing(t *testing.T) {
	recorder := &recordingUsageRecorder{}
	client := NewMeteredClient(MeteredClientConfig{
		Inner:    &fakeMeteredLLM{resp: &llm.ChatResponse{Content: "hello"}},
		Counter:  NewHeuristicCounter(),
		Recorder: recorder,
	})

	resp, err := client.Chat(context.Background(), llm.ChatRequest{Model: "model-a", Messages: []llm.Message{{Role: llm.RoleUser, Content: "hello"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Usage.Source != llm.UsageSourceEstimated || resp.Usage.InputTokens == 0 || resp.Usage.OutputTokens == 0 {
		t.Fatalf("response usage = %#v, want estimated input/output", resp.Usage)
	}
	if len(recorder.events) != 1 || recorder.events[0].UsageSource != llm.UsageSourceEstimated {
		t.Fatalf("events = %#v, want estimated event", recorder.events)
	}
	if recorder.events[0].ActualInputTokens != 0 || recorder.events[0].ActualOutputTokens != 0 {
		t.Fatalf("event = %#v, estimated fallback must not populate actual tokens", recorder.events[0])
	}
}

func TestMeteredClientRecordsErrorEvent(t *testing.T) {
	recorder := &recordingUsageRecorder{}
	wantErr := errors.New("provider down")
	client := NewMeteredClient(MeteredClientConfig{
		Inner:    &fakeMeteredLLM{err: wantErr},
		Counter:  NewHeuristicCounter(),
		Recorder: recorder,
	})

	_, err := client.Chat(context.Background(), llm.ChatRequest{Model: "model-a", Messages: []llm.Message{{Role: llm.RoleUser, Content: "hello"}}})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Chat err = %v, want provider error", err)
	}
	if len(recorder.events) != 1 {
		t.Fatalf("events = %#v, want one error event", recorder.events)
	}
	event := recorder.events[0]
	if event.Status != "error" || event.UsageSource != llm.UsageSourceEstimated || event.EstimatedInputTokens == 0 || event.OutputTokens != 0 {
		t.Fatalf("event = %#v, want estimated error event", event)
	}
	if event.ErrorKind != "error" {
		t.Fatalf("error_kind = %q, want error", event.ErrorKind)
	}
}

func TestMeteredClientRecordsStreamOnce(t *testing.T) {
	recorder := &recordingUsageRecorder{}
	inner := &fakeMeteredLLM{resp: &llm.ChatResponse{Content: "ok", Usage: llm.Usage{InputTokens: 3, OutputTokens: 2}}}
	client := NewMeteredClient(MeteredClientConfig{
		Inner:    inner,
		Counter:  NewHeuristicCounter(),
		Recorder: recorder,
	})

	_, err := client.ChatStream(context.Background(), llm.ChatRequest{Model: "model-a", Messages: []llm.Message{{Role: llm.RoleUser, Content: "hello"}}}, func(llm.StreamEvent) {})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if inner.streamCalls != 1 || len(recorder.events) != 1 {
		t.Fatalf("streamCalls=%d events=%#v, want one stream call and one event", inner.streamCalls, recorder.events)
	}
	if !recorder.events[0].Stream {
		t.Fatalf("stream event = %#v, want stream=true", recorder.events[0])
	}
}

func TestMeteredClientKeepsActualProviderTokensSeparateForHybrid(t *testing.T) {
	recorder := &recordingUsageRecorder{}
	client := NewMeteredClient(MeteredClientConfig{
		Inner: &fakeMeteredLLM{resp: &llm.ChatResponse{
			Content: "hello",
			Usage:   llm.Usage{OutputTokens: 5, Source: llm.UsageSourceProvider},
		}},
		Counter:  NewHeuristicCounter(),
		Recorder: recorder,
	})

	resp, err := client.Chat(context.Background(), llm.ChatRequest{Model: "model-a", Messages: []llm.Message{{Role: llm.RoleUser, Content: "hello"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Usage.Source != llm.UsageSourceHybrid || resp.Usage.InputTokens == 0 || resp.Usage.OutputTokens != 5 {
		t.Fatalf("response usage = %#v, want hybrid with estimated input and provider output", resp.Usage)
	}
	event := recorder.events[0]
	if event.ActualInputTokens != 0 || event.ActualOutputTokens != 5 || event.EstimatedInputTokens == 0 {
		t.Fatalf("event = %#v, want provider actual output separate from estimated input", event)
	}
	if event.EstimatedOutputTokens == 5 {
		t.Fatalf("event = %#v, estimated output must not copy actual output", event)
	}
	if event.EstimatedOutputTokens == 0 {
		t.Fatalf("event = %#v, want estimated output from response text", event)
	}
}

func TestMeteredClientDoesNotReturnUsageEventIDWhenRecordFails(t *testing.T) {
	recorder := &recordingUsageRecorder{err: errors.New("db down")}
	client := NewMeteredClient(MeteredClientConfig{
		Inner:    &fakeMeteredLLM{resp: &llm.ChatResponse{Content: "ok", Usage: llm.Usage{InputTokens: 3, OutputTokens: 2, Source: llm.UsageSourceProvider}}},
		Counter:  NewHeuristicCounter(),
		Recorder: recorder,
	})

	resp, err := client.Chat(context.Background(), llm.ChatRequest{Model: "model-a", Messages: []llm.Message{{Role: llm.RoleUser, Content: "hello"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Usage.UsageEventID != "" {
		t.Fatalf("usage_event_id = %q, want empty when record fails", resp.Usage.UsageEventID)
	}
}

func TestMeteredClientRecordsCancelledChat(t *testing.T) {
	recorder := &recordingUsageRecorder{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := NewMeteredClient(MeteredClientConfig{
		Inner:    &fakeMeteredLLM{err: context.Canceled},
		Counter:  NewHeuristicCounter(),
		Recorder: recorder,
	})

	_, err := client.Chat(ctx, llm.ChatRequest{Model: "model-a", Messages: []llm.Message{{Role: llm.RoleUser, Content: "hello"}}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Chat err = %v, want context canceled", err)
	}
	if len(recorder.events) != 1 {
		t.Fatalf("events = %#v, want one", recorder.events)
	}
	event := recorder.events[0]
	if event.Status != "cancelled" || event.ErrorKind != "cancelled" || !strings.Contains(event.ErrorMessage, "canceled") {
		t.Fatalf("event = %#v, want cancelled status/error_kind/message", event)
	}
}
