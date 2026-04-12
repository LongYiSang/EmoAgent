package llm

import (
	"encoding/json"
	"testing"
)

func TestContentBlockJSON(t *testing.T) {
	tests := []struct {
		name  string
		block ContentBlock
	}{
		{
			name:  "text block",
			block: ContentBlock{Type: "text", Text: "hello"},
		},
		{
			name: "tool_use block",
			block: ContentBlock{
				Type:  "tool_use",
				ID:    "call_123",
				Name:  "get_time",
				Input: json.RawMessage(`{"timezone":"UTC"}`),
			},
		},
		{
			name: "tool_result block",
			block: ContentBlock{
				Type:    "tool_result",
				ID:      "call_123",
				Content: "2026-04-12T10:00:00Z",
			},
		},
		{
			name: "tool_result error",
			block: ContentBlock{
				Type:    "tool_result",
				ID:      "call_456",
				Content: "permission denied",
				IsError: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.block)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			var got ContentBlock
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			if got.Type != tt.block.Type {
				t.Errorf("Type: got %q, want %q", got.Type, tt.block.Type)
			}
			if got.Text != tt.block.Text {
				t.Errorf("Text: got %q, want %q", got.Text, tt.block.Text)
			}
			if got.ID != tt.block.ID {
				t.Errorf("ID: got %q, want %q", got.ID, tt.block.ID)
			}
			if got.Name != tt.block.Name {
				t.Errorf("Name: got %q, want %q", got.Name, tt.block.Name)
			}
			if got.Content != tt.block.Content {
				t.Errorf("Content: got %q, want %q", got.Content, tt.block.Content)
			}
			if got.IsError != tt.block.IsError {
				t.Errorf("IsError: got %v, want %v", got.IsError, tt.block.IsError)
			}
			if string(got.Input) != string(tt.block.Input) {
				t.Errorf("Input: got %s, want %s", got.Input, tt.block.Input)
			}
		})
	}
}

func TestToolDefJSON(t *testing.T) {
	def := ToolDef{
		Name:        "get_weather",
		Description: "Get current weather for a location",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
	}

	data, err := json.Marshal(def)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got ToolDef
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Name != def.Name {
		t.Errorf("Name: got %q, want %q", got.Name, def.Name)
	}
	if got.Description != def.Description {
		t.Errorf("Description: got %q, want %q", got.Description, def.Description)
	}
	if string(got.InputSchema) != string(def.InputSchema) {
		t.Errorf("InputSchema: got %s, want %s", got.InputSchema, def.InputSchema)
	}
}

func TestMessageBackwardCompat(t *testing.T) {
	// Old-style message with only Content should still work.
	msg := Message{Role: RoleUser, Content: "hello"}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Role != RoleUser {
		t.Errorf("Role: got %q, want %q", got.Role, RoleUser)
	}
	if got.Content != "hello" {
		t.Errorf("Content: got %q, want %q", got.Content, "hello")
	}
	if len(got.ContentBlocks) != 0 {
		t.Errorf("ContentBlocks should be empty, got %d", len(got.ContentBlocks))
	}
	if got.ToolCallID != "" {
		t.Errorf("ToolCallID should be empty, got %q", got.ToolCallID)
	}

	// Verify omitempty: content_blocks and tool_call_id should not appear in JSON.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["content_blocks"]; ok {
		t.Error("content_blocks should be omitted when empty")
	}
	if _, ok := raw["tool_call_id"]; ok {
		t.Error("tool_call_id should be omitted when empty")
	}
}

func TestMessageWithContentBlocks(t *testing.T) {
	msg := Message{
		Role:    RoleAssistant,
		Content: "Let me check that for you.",
		ContentBlocks: []ContentBlock{
			{Type: "text", Text: "Let me check that for you."},
			{Type: "tool_use", ID: "call_1", Name: "get_time", Input: json.RawMessage(`{"tz":"UTC"}`)},
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(got.ContentBlocks) != 2 {
		t.Fatalf("ContentBlocks: got %d, want 2", len(got.ContentBlocks))
	}
	if got.ContentBlocks[0].Type != "text" {
		t.Errorf("block[0].Type: got %q, want %q", got.ContentBlocks[0].Type, "text")
	}
	if got.ContentBlocks[1].Type != "tool_use" {
		t.Errorf("block[1].Type: got %q, want %q", got.ContentBlocks[1].Type, "tool_use")
	}
	if got.ContentBlocks[1].Name != "get_time" {
		t.Errorf("block[1].Name: got %q, want %q", got.ContentBlocks[1].Name, "get_time")
	}
}

func TestMessageToolResult(t *testing.T) {
	// OpenAI-style tool result message.
	msg := Message{
		Role:       RoleTool,
		Content:    `{"time":"2026-04-12T10:00:00Z"}`,
		ToolCallID: "call_1",
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Role != RoleTool {
		t.Errorf("Role: got %q, want %q", got.Role, RoleTool)
	}
	if got.ToolCallID != "call_1" {
		t.Errorf("ToolCallID: got %q, want %q", got.ToolCallID, "call_1")
	}
}

func TestChatRequestWithTools(t *testing.T) {
	req := ChatRequest{
		Model: "test-model",
		Messages: []Message{
			{Role: RoleUser, Content: "What time is it?"},
		},
		MaxTokens:   1024,
		Temperature: 0.7,
		Tools: []ToolDef{
			{
				Name:        "get_time",
				Description: "Get the current time",
				InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
			},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got ChatRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(got.Tools) != 1 {
		t.Fatalf("Tools: got %d, want 1", len(got.Tools))
	}
	if got.Tools[0].Name != "get_time" {
		t.Errorf("Tools[0].Name: got %q, want %q", got.Tools[0].Name, "get_time")
	}
}

func TestChatRequestWithoutTools(t *testing.T) {
	// Tools should be omitted when empty.
	req := ChatRequest{
		Model:       "test-model",
		Messages:    []Message{{Role: RoleUser, Content: "hi"}},
		MaxTokens:   1024,
		Temperature: 0.7,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["tools"]; ok {
		t.Error("tools should be omitted when empty")
	}
}

func TestNormalizeStopReason(t *testing.T) {
	tests := []struct {
		provider string
		raw      string
		want     string
	}{
		{"anthropic", "end_turn", "end_turn"},
		{"anthropic", "tool_use", "tool_use"},
		{"anthropic", "max_tokens", "max_tokens"},
		{"anthropic", "stop_sequence", ""},
		{"anthropic", "pause_turn", ""},
		{"openai", "stop", "end_turn"},
		{"openai", "tool_calls", "tool_use"},
		{"openai", "length", "max_tokens"},
		{"openai", "content_filter", "content_filter"},
		{"openai", "unknown_value", ""},
		{"unknown_provider", "stop", ""},
	}

	for _, tt := range tests {
		t.Run(tt.provider+"_"+tt.raw, func(t *testing.T) {
			got := NormalizeStopReason(tt.provider, tt.raw)
			if got != tt.want {
				t.Errorf("NormalizeStopReason(%q, %q) = %q, want %q", tt.provider, tt.raw, got, tt.want)
			}
		})
	}
}
