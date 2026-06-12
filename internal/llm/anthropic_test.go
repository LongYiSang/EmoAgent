package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestAnthropicClient(serverURL string) *anthropicClient {
	return &anthropicClient{
		baseURL:    serverURL,
		apiKey:     "test-key",
		httpClient: http.DefaultClient,
		logger:     slog.Default(),
	}
}

func TestAnthropicChat_TextOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers.
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("missing x-api-key header")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(anthropicResponse{
			ID:    "msg_001",
			Model: "claude-test",
			Content: []anthropicContentBlock{
				{Type: "text", Text: "Hello there!"},
			},
			StopReason: "end_turn",
			Usage: struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			}{InputTokens: 10, OutputTokens: 5},
		})
	}))
	defer server.Close()

	client := newTestAnthropicClient(server.URL)
	resp, err := client.Chat(context.Background(), ChatRequest{
		Model:       "claude-test",
		Messages:    []Message{{Role: RoleUser, Content: "Hi"}},
		MaxTokens:   100,
		Temperature: 0.5,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if resp.Content != "Hello there!" {
		t.Errorf("Content: got %q, want %q", resp.Content, "Hello there!")
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason: got %q, want %q", resp.StopReason, "end_turn")
	}
	if resp.RawStopReason != "end_turn" {
		t.Errorf("RawStopReason: got %q, want %q", resp.RawStopReason, "end_turn")
	}
	if len(resp.ContentBlocks) != 1 || resp.ContentBlocks[0].Type != "text" {
		t.Errorf("ContentBlocks: expected 1 text block, got %+v", resp.ContentBlocks)
	}
}

func TestAnthropicStatusLogRedactsImageData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `provider echoed data:image/png;base64,iVBORw0KGgo=`, http.StatusBadRequest)
	}))
	defer server.Close()

	var logs bytes.Buffer
	client := newTestAnthropicClient(server.URL)
	client.logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	_, err := client.Chat(context.Background(), ChatRequest{
		Model:    "claude-test",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("Chat succeeded, want status error")
	}
	got := logs.String()
	if strings.Contains(got, "data:image") || strings.Contains(got, "base64") || strings.Contains(got, "iVBOR") {
		t.Fatalf("log leaked image data: %s", got)
	}
}

func TestAnthropicChat_MapsThinkingParamsAndExtra(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(anthropicResponse{
			ID:    "msg_params",
			Model: "claude-test",
			Content: []anthropicContentBlock{
				{Type: "text", Text: "ok"},
			},
			StopReason: "end_turn",
		})
	}))
	defer server.Close()

	client := newTestAnthropicClient(server.URL)
	temp := 0.2
	budget := 1024
	_, err := client.Chat(context.Background(), ChatRequest{
		Model:    "claude-test",
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
		Params: RequestParams{
			MaxTokens:   256,
			Temperature: &temp,
			Thinking:    &ThinkingConfig{Mode: "manual", BudgetTokens: &budget},
			Extra: map[string]any{
				"max_tokens":       999,
				"metadata":         map[string]any{"user_id": "u1"},
				"presence_penalty": 1,
			},
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got := payload["max_tokens"]; got != float64(256) {
		t.Fatalf("max_tokens = %#v, want 256", got)
	}
	if got := payload["temperature"]; got != 0.2 {
		t.Fatalf("temperature = %#v, want 0.2", got)
	}
	thinking, ok := payload["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("thinking = %#v, want object", payload["thinking"])
	}
	if thinking["type"] != "enabled" || thinking["budget_tokens"] != float64(1024) {
		t.Fatalf("thinking = %#v, want enabled budget 1024", thinking)
	}
	if _, exists := payload["presence_penalty"]; exists {
		t.Fatalf("unsupported presence_penalty should not be sent for Anthropic")
	}
	if _, exists := payload["metadata"]; !exists {
		t.Fatalf("metadata extra was not preserved")
	}
}

func TestDiscoverModelsAnthropic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %s, want /v1/models", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Fatalf("x-api-key header = %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Fatalf("anthropic-version header missing")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":"claude-a"},{"id":"claude-b"}]}`)
	}))
	defer server.Close()
	t.Setenv("TEST_ANTHROPIC_MODELS_KEY", "test-key")

	models, err := DiscoverModels(context.Background(), ProviderConfig{
		Protocol:  "anthropic",
		BaseURL:   server.URL,
		APIKeyEnv: "TEST_ANTHROPIC_MODELS_KEY",
	})
	if err != nil {
		t.Fatalf("DiscoverModels: %v", err)
	}
	if len(models) != 2 || models[0].ID != "claude-a" || models[1].ID != "claude-b" {
		t.Fatalf("models = %#v", models)
	}
}

func TestAnthropicChat_ToolUse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify tools are sent in request.
		var req anthropicRequest
		json.NewDecoder(r.Body).Decode(&req)
		if len(req.Tools) != 1 || req.Tools[0].Name != "get_time" {
			t.Errorf("expected 1 tool 'get_time', got %+v", req.Tools)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(anthropicResponse{
			ID:    "msg_002",
			Model: "claude-test",
			Content: []anthropicContentBlock{
				{Type: "text", Text: "Let me check the time."},
				{Type: "tool_use", ID: "toolu_01", Name: "get_time", Input: json.RawMessage(`{"timezone":"UTC"}`)},
			},
			StopReason: "tool_use",
			Usage: struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			}{InputTokens: 20, OutputTokens: 15},
		})
	}))
	defer server.Close()

	client := newTestAnthropicClient(server.URL)
	resp, err := client.Chat(context.Background(), ChatRequest{
		Model:       "claude-test",
		Messages:    []Message{{Role: RoleUser, Content: "What time is it?"}},
		MaxTokens:   100,
		Temperature: 0.5,
		Tools: []ToolDef{{
			Name:        "get_time",
			Description: "Get current time",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"timezone":{"type":"string"}}}`),
		}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if resp.Content != "Let me check the time." {
		t.Errorf("Content: got %q", resp.Content)
	}
	if resp.StopReason != "tool_use" {
		t.Errorf("StopReason: got %q, want %q", resp.StopReason, "tool_use")
	}
	if len(resp.ContentBlocks) != 2 {
		t.Fatalf("ContentBlocks: got %d, want 2", len(resp.ContentBlocks))
	}
	if resp.ContentBlocks[1].Type != "tool_use" {
		t.Errorf("block[1].Type: got %q", resp.ContentBlocks[1].Type)
	}
	if resp.ContentBlocks[1].ID != "toolu_01" {
		t.Errorf("block[1].ID: got %q", resp.ContentBlocks[1].ID)
	}
	if resp.ContentBlocks[1].Name != "get_time" {
		t.Errorf("block[1].Name: got %q", resp.ContentBlocks[1].Name)
	}
}

func TestAnthropicChatStream_ToolUse(t *testing.T) {
	// Simulate Anthropic streaming response with tool_use.
	sseEvents := []string{
		`data: {"type":"message_start","message":{"id":"msg_003","model":"claude-test","content":[],"stop_reason":null,"usage":{"input_tokens":25,"output_tokens":0}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Checking "}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"time."}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_02","name":"get_time"}}`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"time"}}`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"zone\":\"UTC\"}"}}`,
		`data: {"type":"content_block_stop","index":1}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"input_tokens":25,"output_tokens":20}}`,
		`data: {"type":"message_stop"}`,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range sseEvents {
			fmt.Fprintln(w, e)
			fmt.Fprintln(w)
		}
	}))
	defer server.Close()

	client := newTestAnthropicClient(server.URL)

	var textChunks []string
	var toolBlocks []ContentBlock
	var gotDone bool

	resp, err := client.ChatStream(context.Background(), ChatRequest{
		Model:       "claude-test",
		Messages:    []Message{{Role: RoleUser, Content: "time?"}},
		MaxTokens:   100,
		Temperature: 0.5,
		Tools: []ToolDef{{
			Name:        "get_time",
			Description: "Get time",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}},
	}, func(event StreamEvent) {
		switch {
		case event.Done:
			gotDone = true
		case event.Type == "text" && event.Content != "":
			textChunks = append(textChunks, event.Content)
		case event.Type == "tool_use" && event.ContentBlock != nil:
			toolBlocks = append(toolBlocks, *event.ContentBlock)
		}
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	// Verify accumulated text.
	if resp.Content != "Checking time." {
		t.Errorf("Content: got %q, want %q", resp.Content, "Checking time.")
	}
	if joined := strings.Join(textChunks, ""); joined != "Checking time." {
		t.Errorf("streamed text: got %q", joined)
	}

	// Verify tool_use block.
	if len(toolBlocks) != 1 {
		t.Fatalf("tool blocks: got %d, want 1", len(toolBlocks))
	}
	if toolBlocks[0].ID != "toolu_02" {
		t.Errorf("tool ID: got %q", toolBlocks[0].ID)
	}
	if toolBlocks[0].Name != "get_time" {
		t.Errorf("tool Name: got %q", toolBlocks[0].Name)
	}
	if string(toolBlocks[0].Input) != `{"timezone":"UTC"}` {
		t.Errorf("tool Input: got %s", toolBlocks[0].Input)
	}

	// Verify response metadata.
	if resp.StopReason != "tool_use" {
		t.Errorf("StopReason: got %q", resp.StopReason)
	}
	if resp.RawStopReason != "tool_use" {
		t.Errorf("RawStopReason: got %q", resp.RawStopReason)
	}
	if !gotDone {
		t.Error("did not receive Done event")
	}

	// Verify ContentBlocks on response (text + tool_use).
	if len(resp.ContentBlocks) != 2 {
		t.Fatalf("ContentBlocks: got %d, want 2", len(resp.ContentBlocks))
	}
	if resp.ContentBlocks[0].Type != "text" {
		t.Errorf("block[0].Type: got %q", resp.ContentBlocks[0].Type)
	}
	if resp.ContentBlocks[1].Type != "tool_use" {
		t.Errorf("block[1].Type: got %q", resp.ContentBlocks[1].Type)
	}
}

func TestAnthropicToMessages_ToolResult(t *testing.T) {
	client := &anthropicClient{logger: slog.Default()}

	req := ChatRequest{
		Messages: []Message{
			{Role: RoleUser, Content: "What time is it?"},
			{
				Role:    RoleAssistant,
				Content: "Let me check.",
				ContentBlocks: []ContentBlock{
					{Type: "text", Text: "Let me check."},
					{Type: "tool_use", ID: "toolu_01", Name: "get_time", Input: json.RawMessage(`{}`)},
				},
			},
			{
				Role: RoleUser,
				ContentBlocks: []ContentBlock{
					{Type: "tool_result", ID: "toolu_01", Content: `{"time":"10:00"}`, IsError: false},
				},
			},
		},
	}

	msgs := client.toMessages(req)

	if len(msgs) != 3 {
		t.Fatalf("messages: got %d, want 3", len(msgs))
	}

	// First: simple text.
	if msgs[0].Content != "What time is it?" {
		t.Errorf("msg[0].Content: got %v", msgs[0].Content)
	}

	// Second: assistant with tool_use — structured content.
	assistantBlocks, ok := msgs[1].Content.([]anthropicContentBlock)
	if !ok {
		t.Fatalf("msg[1].Content should be []anthropicContentBlock, got %T", msgs[1].Content)
	}
	if len(assistantBlocks) != 2 {
		t.Fatalf("assistant blocks: got %d, want 2", len(assistantBlocks))
	}
	if assistantBlocks[1].Type != "tool_use" || assistantBlocks[1].ID != "toolu_01" {
		t.Errorf("assistant tool_use block: %+v", assistantBlocks[1])
	}

	// Third: tool_result — structured content with tool_use_id on wire.
	resultBlocks, ok := msgs[2].Content.([]anthropicContentBlock)
	if !ok {
		t.Fatalf("msg[2].Content should be []anthropicContentBlock, got %T", msgs[2].Content)
	}
	if len(resultBlocks) != 1 {
		t.Fatalf("result blocks: got %d, want 1", len(resultBlocks))
	}
	if resultBlocks[0].Type != "tool_result" {
		t.Errorf("result block type: got %q", resultBlocks[0].Type)
	}
	if resultBlocks[0].ToolUseID != "toolu_01" {
		t.Errorf("result block tool_use_id: got %q, want %q", resultBlocks[0].ToolUseID, "toolu_01")
	}
	if resultBlocks[0].Content != `{"time":"10:00"}` {
		t.Errorf("result block content: got %q", resultBlocks[0].Content)
	}

	// Verify the wire JSON has "tool_use_id" not "id".
	wireJSON, _ := json.Marshal(resultBlocks[0])
	if !strings.Contains(string(wireJSON), `"tool_use_id"`) {
		t.Errorf("wire JSON should contain 'tool_use_id', got: %s", wireJSON)
	}
	if strings.Contains(string(wireJSON), `"id"`) {
		// "id" should not appear because it's empty (omitempty).
		t.Errorf("wire JSON should not contain 'id' field, got: %s", wireJSON)
	}
}

func TestAnthropicChatStream_TextOnly(t *testing.T) {
	// Verify backward compatibility: streaming without tools works as before.
	sseEvents := []string{
		`data: {"type":"message_start","message":{"id":"msg_004","model":"claude-test","content":[],"stop_reason":null,"usage":{"input_tokens":5,"output_tokens":0}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello!"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":5,"output_tokens":3}}`,
		`data: {"type":"message_stop"}`,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify no tools in request.
		var req anthropicRequest
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

	client := newTestAnthropicClient(server.URL)

	var chunks []string
	resp, err := client.ChatStream(context.Background(), ChatRequest{
		Model:       "claude-test",
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

	if resp.Content != "Hello!" {
		t.Errorf("Content: got %q", resp.Content)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason: got %q", resp.StopReason)
	}
}
