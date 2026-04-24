package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestOpenAIClient(serverURL string) *openaiClient {
	return &openaiClient{
		baseURL:    serverURL,
		apiKey:     "test-key",
		httpClient: http.DefaultClient,
		logger:     slog.Default(),
	}
}

func TestOpenAIChat_TextOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing Authorization header")
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"id": "chatcmpl-001",
			"model": "gpt-test",
			"choices": [{
				"message": {"content": "Hello!"},
				"finish_reason": "stop"
			}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 5}
		}`)
	}))
	defer server.Close()

	client := newTestOpenAIClient(server.URL)
	resp, err := client.Chat(context.Background(), ChatRequest{
		Model:       "gpt-test",
		Messages:    []Message{{Role: RoleUser, Content: "Hi"}},
		MaxTokens:   100,
		Temperature: 0.5,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if resp.Content != "Hello!" {
		t.Errorf("Content: got %q", resp.Content)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason: got %q, want %q", resp.StopReason, "end_turn")
	}
	if resp.RawStopReason != "stop" {
		t.Errorf("RawStopReason: got %q, want %q", resp.RawStopReason, "stop")
	}
}

func TestOpenAIChat_MapsRequestParamsAndOmitsNilTemperature(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"chatcmpl-params","model":"gpt-test","choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	client := newTestOpenAIClient(server.URL)
	topP := 0.8
	_, err := client.Chat(context.Background(), ChatRequest{
		Model:    "gpt-test",
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
		Params: RequestParams{
			MaxTokens:       128,
			TopP:            &topP,
			ReasoningEffort: "medium",
			Extra: map[string]any{
				"temperature":      1.5,
				"max_tokens":       999,
				"custom_parameter": "kept",
			},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if _, exists := payload["temperature"]; exists {
		t.Fatalf("temperature was sent for nil RequestParams.Temperature: %#v", payload["temperature"])
	}
	if got := payload["max_tokens"]; got != float64(128) {
		t.Fatalf("max_tokens = %#v, want 128", got)
	}
	if got := payload["top_p"]; got != 0.8 {
		t.Fatalf("top_p = %#v, want 0.8", got)
	}
	if got := payload["reasoning_effort"]; got != "medium" {
		t.Fatalf("reasoning_effort = %#v, want medium", got)
	}
	if got := payload["custom_parameter"]; got != "kept" {
		t.Fatalf("custom_parameter = %#v, want kept", got)
	}
}

func TestDiscoverModelsOpenAICompatible(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %s, want /v1/models", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("authorization header = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":"gpt-a"},{"id":"gpt-b"}]}`)
	}))
	defer server.Close()
	t.Setenv("TEST_OPENAI_MODELS_KEY", "test-key")

	models, err := DiscoverModels(context.Background(), ProviderConfig{
		Protocol:  "openai_compatible",
		BaseURL:   server.URL,
		APIKeyEnv: "TEST_OPENAI_MODELS_KEY",
	})
	if err != nil {
		t.Fatalf("DiscoverModels: %v", err)
	}
	if len(models) != 2 || models[0].ID != "gpt-a" || models[1].ID != "gpt-b" {
		t.Fatalf("models = %#v", models)
	}
}

func TestOpenAIChat_ToolUse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify tools in request.
		var req openaiRequest
		json.NewDecoder(r.Body).Decode(&req)
		if len(req.Tools) != 1 || req.Tools[0].Function.Name != "get_time" {
			t.Errorf("expected 1 tool 'get_time', got %+v", req.Tools)
		}
		if req.Tools[0].Type != "function" {
			t.Errorf("tool type: got %q, want %q", req.Tools[0].Type, "function")
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"id": "chatcmpl-002",
			"model": "gpt-test",
			"choices": [{
				"message": {
					"content": null,
					"tool_calls": [{
						"id": "call_abc",
						"type": "function",
						"function": {"name": "get_time", "arguments": "{\"tz\":\"UTC\"}"}
					}]
				},
				"finish_reason": "tool_calls"
			}],
			"usage": {"prompt_tokens": 20, "completion_tokens": 15}
		}`)
	}))
	defer server.Close()

	client := newTestOpenAIClient(server.URL)
	resp, err := client.Chat(context.Background(), ChatRequest{
		Model:       "gpt-test",
		Messages:    []Message{{Role: RoleUser, Content: "What time?"}},
		MaxTokens:   100,
		Temperature: 0.5,
		Tools: []ToolDef{{
			Name:        "get_time",
			Description: "Get current time",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"tz":{"type":"string"}}}`),
		}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if resp.Content != "" {
		t.Errorf("Content should be empty, got %q", resp.Content)
	}
	if resp.StopReason != "tool_use" {
		t.Errorf("StopReason: got %q, want %q", resp.StopReason, "tool_use")
	}
	if resp.RawStopReason != "tool_calls" {
		t.Errorf("RawStopReason: got %q", resp.RawStopReason)
	}
	if len(resp.ContentBlocks) != 1 {
		t.Fatalf("ContentBlocks: got %d, want 1", len(resp.ContentBlocks))
	}
	if resp.ContentBlocks[0].Type != "tool_use" {
		t.Errorf("block type: got %q", resp.ContentBlocks[0].Type)
	}
	if resp.ContentBlocks[0].ID != "call_abc" {
		t.Errorf("block ID: got %q", resp.ContentBlocks[0].ID)
	}
	if resp.ContentBlocks[0].Name != "get_time" {
		t.Errorf("block Name: got %q", resp.ContentBlocks[0].Name)
	}
}

func TestOpenAIToMessages_ToolResult(t *testing.T) {
	client := &openaiClient{logger: slog.Default()}

	req := ChatRequest{
		System: "You are helpful.",
		Messages: []Message{
			{Role: RoleUser, Content: "What time?"},
			{
				Role:    RoleAssistant,
				Content: "",
				ContentBlocks: []ContentBlock{
					{Type: "tool_use", ID: "call_abc", Name: "get_time", Input: json.RawMessage(`{"tz":"UTC"}`)},
				},
			},
			{
				Role:       RoleTool,
				Content:    `{"time":"10:00"}`,
				ToolCallID: "call_abc",
			},
		},
	}

	msgs := client.toMessages(req)

	// system + user + assistant + tool = 4 messages
	if len(msgs) != 4 {
		t.Fatalf("messages: got %d, want 4", len(msgs))
	}

	// System message.
	if msgs[0].Role != "system" || *msgs[0].Content != "You are helpful." {
		t.Errorf("msg[0]: %+v", msgs[0])
	}

	// User message.
	if msgs[1].Role != "user" || *msgs[1].Content != "What time?" {
		t.Errorf("msg[1]: %+v", msgs[1])
	}

	// Assistant with tool_calls.
	if msgs[2].Role != "assistant" {
		t.Errorf("msg[2].Role: got %q", msgs[2].Role)
	}
	if len(msgs[2].ToolCalls) != 1 {
		t.Fatalf("msg[2].ToolCalls: got %d, want 1", len(msgs[2].ToolCalls))
	}
	if msgs[2].ToolCalls[0].ID != "call_abc" {
		t.Errorf("tool_call ID: got %q", msgs[2].ToolCalls[0].ID)
	}
	if msgs[2].ToolCalls[0].Function.Name != "get_time" {
		t.Errorf("tool_call Name: got %q", msgs[2].ToolCalls[0].Function.Name)
	}

	// Tool result.
	if msgs[3].Role != "tool" {
		t.Errorf("msg[3].Role: got %q", msgs[3].Role)
	}
	if msgs[3].ToolCallID != "call_abc" {
		t.Errorf("msg[3].ToolCallID: got %q", msgs[3].ToolCallID)
	}
	if *msgs[3].Content != `{"time":"10:00"}` {
		t.Errorf("msg[3].Content: got %q", *msgs[3].Content)
	}
}

func TestOpenAIChatStream_ToolUse(t *testing.T) {
	sseEvents := []string{
		`data: {"id":"chatcmpl-003","model":"gpt-test","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_xyz","type":"function","function":{"name":"get_time","arguments":""}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-003","model":"gpt-test","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"tz\":"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-003","model":"gpt-test","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"UTC\"}"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-003","model":"gpt-test","choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":15,"completion_tokens":10}}`,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range sseEvents {
			fmt.Fprintln(w, e)
			fmt.Fprintln(w)
		}
	}))
	defer server.Close()

	client := newTestOpenAIClient(server.URL)

	var toolBlocks []ContentBlock
	var eventOrder []string

	resp, err := client.ChatStream(context.Background(), ChatRequest{
		Model:       "gpt-test",
		Messages:    []Message{{Role: RoleUser, Content: "time?"}},
		MaxTokens:   100,
		Temperature: 0.5,
		Tools: []ToolDef{{
			Name:        "get_time",
			Description: "Get time",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}},
	}, func(event StreamEvent) {
		if event.Done {
			eventOrder = append(eventOrder, "done")
		}
		if event.Type == "tool_use" && event.ContentBlock != nil {
			toolBlocks = append(toolBlocks, *event.ContentBlock)
			eventOrder = append(eventOrder, "tool_use")
		}
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	if resp.Content != "" {
		t.Errorf("Content should be empty, got %q", resp.Content)
	}
	if resp.StopReason != "tool_use" {
		t.Errorf("StopReason: got %q", resp.StopReason)
	}
	if got, want := strings.Join(eventOrder, ","), "tool_use,done"; got != want {
		t.Errorf("event order: got %q, want %q", got, want)
	}
	if len(toolBlocks) != 1 {
		t.Fatalf("tool blocks: got %d, want 1", len(toolBlocks))
	}
	if toolBlocks[0].ID != "call_xyz" {
		t.Errorf("tool ID: got %q", toolBlocks[0].ID)
	}
	if toolBlocks[0].Name != "get_time" {
		t.Errorf("tool Name: got %q", toolBlocks[0].Name)
	}
	if string(toolBlocks[0].Input) != `{"tz":"UTC"}` {
		t.Errorf("tool Input: got %s", toolBlocks[0].Input)
	}
}

func TestOpenAIChatStream_ToolUseInterleavedByIndex(t *testing.T) {
	sseEvents := []string{
		`data: {"id":"chatcmpl-005","model":"gpt-test","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"get_time","arguments":""}},{"index":1,"id":"call_b","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-005","model":"gpt-test","choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"city\":\"To"}},{"index":0,"function":{"arguments":"{\"tz\":\"UT"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-005","model":"gpt-test","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"C\"}"}},{"index":1,"function":{"arguments":"kyo\"}"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-005","model":"gpt-test","choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":30,"completion_tokens":20}}`,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range sseEvents {
			fmt.Fprintln(w, e)
			fmt.Fprintln(w)
		}
	}))
	defer server.Close()

	client := newTestOpenAIClient(server.URL)

	var toolBlocks []ContentBlock
	resp, err := client.ChatStream(context.Background(), ChatRequest{
		Model:       "gpt-test",
		Messages:    []Message{{Role: RoleUser, Content: "time and weather?"}},
		MaxTokens:   100,
		Temperature: 0.5,
	}, func(event StreamEvent) {
		if event.Type == "tool_use" && event.ContentBlock != nil {
			toolBlocks = append(toolBlocks, *event.ContentBlock)
		}
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	if len(resp.ContentBlocks) != 2 {
		t.Fatalf("ContentBlocks: got %d, want 2", len(resp.ContentBlocks))
	}
	if len(toolBlocks) != 2 {
		t.Fatalf("tool blocks: got %d, want 2", len(toolBlocks))
	}

	if toolBlocks[0].ID != "call_a" || toolBlocks[0].Name != "get_time" || string(toolBlocks[0].Input) != `{"tz":"UTC"}` {
		t.Errorf("toolBlocks[0]: got %+v", toolBlocks[0])
	}
	if toolBlocks[1].ID != "call_b" || toolBlocks[1].Name != "get_weather" || string(toolBlocks[1].Input) != `{"city":"Tokyo"}` {
		t.Errorf("toolBlocks[1]: got %+v", toolBlocks[1])
	}
}

func TestOpenAIToMessages_FromOpenAIResultsToMessages(t *testing.T) {
	results := []struct {
		CallID  string
		Content string
	}{
		{CallID: "call_1", Content: `{"time":"10:00"}`},
		{CallID: "call_2", Content: `{"weather":"sunny"}`},
	}

	messages := []Message{
		{Role: RoleUser, Content: "Need results"},
	}
	for _, result := range results {
		messages = append(messages, Message{
			Role:       RoleTool,
			ToolCallID: result.CallID,
			Content:    result.Content,
		})
	}

	client := &openaiClient{logger: slog.Default()}
	req := ChatRequest{Messages: messages}
	msgs := client.toMessages(req)

	if len(msgs) != 3 {
		t.Fatalf("messages: got %d, want 3", len(msgs))
	}
	if msgs[1].Role != "tool" || msgs[1].ToolCallID != "call_1" || msgs[1].Content == nil || *msgs[1].Content != `{"time":"10:00"}` {
		t.Errorf("msg[1]: %+v", msgs[1])
	}
	if msgs[2].Role != "tool" || msgs[2].ToolCallID != "call_2" || msgs[2].Content == nil || *msgs[2].Content != `{"weather":"sunny"}` {
		t.Errorf("msg[2]: %+v", msgs[2])
	}
}

func TestOpenAIChatStream_TextOnly(t *testing.T) {
	sseEvents := []string{
		`data: {"id":"chatcmpl-004","model":"gpt-test","choices":[{"delta":{"content":"Hi"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-004","model":"gpt-test","choices":[{"delta":{"content":"!"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-004","model":"gpt-test","choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req openaiRequest
		json.NewDecoder(r.Body).Decode(&req)
		if len(req.Tools) != 0 {
			t.Errorf("expected no tools, got %d", len(req.Tools))
		}

		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range sseEvents {
			fmt.Fprintln(w, e)
			fmt.Fprintln(w)
		}
	}))
	defer server.Close()

	client := newTestOpenAIClient(server.URL)

	var chunks []string
	resp, err := client.ChatStream(context.Background(), ChatRequest{
		Model:       "gpt-test",
		Messages:    []Message{{Role: RoleUser, Content: "Hi"}},
		MaxTokens:   100,
		Temperature: 0.5,
	}, func(event StreamEvent) {
		if event.Content != "" {
			chunks = append(chunks, event.Content)
		}
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	if resp.Content != "Hi!" {
		t.Errorf("Content: got %q", resp.Content)
	}
	if strings.Join(chunks, "") != "Hi!" {
		t.Errorf("chunks: got %q", strings.Join(chunks, ""))
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason: got %q", resp.StopReason)
	}
}
