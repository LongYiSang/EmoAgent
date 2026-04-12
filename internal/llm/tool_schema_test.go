package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIChat_SendsNonNullToolSchema(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		tools, ok := req["tools"].([]any)
		if !ok || len(tools) != 1 {
			t.Fatalf("tools = %#v, want one tool", req["tools"])
		}
		tool, ok := tools[0].(map[string]any)
		if !ok {
			t.Fatalf("tool = %#v", tools[0])
		}
		fn, ok := tool["function"].(map[string]any)
		if !ok {
			t.Fatalf("function = %#v", tool["function"])
		}
		if _, ok := fn["parameters"].(map[string]any); !ok {
			t.Fatalf("parameters should be object, got %#v", fn["parameters"])
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"chatcmpl-001","model":"gpt-test","choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	}))
	defer server.Close()

	client := newTestOpenAIClient(server.URL)
	_, err := client.Chat(context.Background(), ChatRequest{
		Model:       "gpt-test",
		Messages:    []Message{{Role: RoleUser, Content: "Hi"}},
		MaxTokens:   16,
		Temperature: 0,
		Tools: []ToolDef{{
			Name:        "get_current_time",
			Description: "Get current time",
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestAnthropicChat_SendsNonNullToolSchema(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		tools, ok := req["tools"].([]any)
		if !ok || len(tools) != 1 {
			t.Fatalf("tools = %#v, want one tool", req["tools"])
		}
		tool, ok := tools[0].(map[string]any)
		if !ok {
			t.Fatalf("tool = %#v", tools[0])
		}
		if _, ok := tool["input_schema"].(map[string]any); !ok {
			t.Fatalf("input_schema should be object, got %#v", tool["input_schema"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(anthropicResponse{
			ID:         "msg_001",
			Model:      "claude-test",
			Content:    []anthropicContentBlock{{Type: "text", Text: "ok"}},
			StopReason: "end_turn",
			Usage: struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			}{InputTokens: 1, OutputTokens: 1},
		})
	}))
	defer server.Close()

	client := newTestAnthropicClient(server.URL)
	_, err := client.Chat(context.Background(), ChatRequest{
		Model:       "claude-test",
		Messages:    []Message{{Role: RoleUser, Content: "Hi"}},
		MaxTokens:   16,
		Temperature: 0,
		Tools: []ToolDef{{
			Name:        "get_current_time",
			Description: "Get current time",
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}
