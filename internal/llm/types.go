package llm

import "encoding/json"

// Role represents a message role in the conversation.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool" // OpenAI tool result messages
)

// ContentBlock represents a single block in a structured message content array.
//
// This is an internal normalized model. Provider request bodies must perform
// explicit field mapping in their toMessages() — never json.Marshal this struct
// directly into a provider wire format. For example, Anthropic's tool_result
// uses "tool_use_id" on the wire, but this struct uses "ID".
type ContentBlock struct {
	Type    string          `json:"type"`               // "text", "tool_use", "tool_result"
	Text    string          `json:"text,omitempty"`     // for type="text"
	ID      string          `json:"id,omitempty"`       // tool_use ID / tool_result's referenced tool_use_id
	Name    string          `json:"name,omitempty"`     // tool name (tool_use)
	Input   json.RawMessage `json:"input,omitempty"`    // tool input JSON (tool_use)
	Content string          `json:"content,omitempty"`  // tool result content (tool_result)
	IsError bool            `json:"is_error,omitempty"` // tool_result error flag
}

// ToolDef is a provider-agnostic tool definition passed in ChatRequest.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ProviderConfig contains the connection settings needed to build an LLM client.
type ProviderConfig struct {
	ID        string
	Protocol  string
	BaseURL   string
	APIKeyEnv string
}

// RequestParams is the provider-agnostic set of generation parameters.
type RequestParams struct {
	MaxTokens        int             `yaml:"max_tokens,omitempty" json:"max_tokens,omitempty"`
	Temperature      *float64        `yaml:"temperature,omitempty" json:"temperature,omitempty"`
	TopP             *float64        `yaml:"top_p,omitempty" json:"top_p,omitempty"`
	PresencePenalty  *float64        `yaml:"presence_penalty,omitempty" json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64        `yaml:"frequency_penalty,omitempty" json:"frequency_penalty,omitempty"`
	ReasoningEffort  string          `yaml:"reasoning_effort,omitempty" json:"reasoning_effort,omitempty"`
	Thinking         *ThinkingConfig `yaml:"thinking,omitempty" json:"thinking,omitempty"`
	Stream           *bool           `yaml:"stream,omitempty" json:"stream,omitempty"`
	Extra            map[string]any  `yaml:"extra,omitempty" json:"extra,omitempty"`
}

type ThinkingConfig struct {
	Mode         string `yaml:"mode,omitempty" json:"mode,omitempty"`
	BudgetTokens *int   `yaml:"budget_tokens,omitempty" json:"budget_tokens,omitempty"`
	Effort       string `yaml:"effort,omitempty" json:"effort,omitempty"`
}

// Message is a single message in a conversation.
//
// When ContentBlocks is non-empty, it represents the full structured content
// of the message (text, tool_use, and tool_result blocks). The Content field
// still holds concatenated text for backward compatibility.
//
// Invariant: ContentBlocks carries ALL structured content including tool_use
// calls from assistant messages. Each provider's toMessages() extracts and
// converts blocks to its wire format (e.g., OpenAI extracts tool_use blocks
// into its native tool_calls array).
type Message struct {
	Role             Role           `json:"role"`
	Content          string         `json:"content"`
	ContentBlocks    []ContentBlock `json:"content_blocks,omitempty"`    // structured content (takes precedence when non-empty)
	ToolCallID       string         `json:"tool_call_id,omitempty"`      // OpenAI tool result message-level field
	ReasoningContent string         `json:"reasoning_content,omitempty"` // thinking/reasoning model output (OpenAI-compatible)
}

// ChatRequest is a unified request to any LLM provider.
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []Message     `json:"messages"`
	System      string        `json:"system,omitempty"`
	Params      RequestParams `json:"params,omitempty"`
	MaxTokens   int           `json:"max_tokens"`
	Temperature float64       `json:"temperature"`
	Stream      bool          `json:"stream"`
	Tools       []ToolDef     `json:"tools,omitempty"`
}

// ChatResponse is a unified response from any LLM provider.
type ChatResponse struct {
	ID               string         `json:"id"`
	Content          string         `json:"content"`        // concatenated text (backward compat)
	ContentBlocks    []ContentBlock `json:"content_blocks"` // full structured response
	Model            string         `json:"model"`
	Usage            Usage          `json:"usage"`
	StopReason       string         `json:"stop_reason"`                 // normalized: "end_turn", "tool_use", "max_tokens", "content_filter", "error"
	RawStopReason    string         `json:"raw_stop_reason"`             // provider's original value
	ReasoningContent string         `json:"reasoning_content,omitempty"` // thinking/reasoning model output
}

// Usage tracks token consumption.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// StreamEvent represents a single event during streaming.
type StreamEvent struct {
	Type         string        // "text", "tool_use", "" (for backward compat)
	Content      string        // text delta
	ContentBlock *ContentBlock // completed tool_use block (set on content_block_stop)
	Done         bool          // true when stream is complete
}

// StreamCallback is called for each streaming chunk.
type StreamCallback func(event StreamEvent)

// NormalizeStopReason maps provider-specific stop reasons to unified values.
func NormalizeStopReason(provider, raw string) string {
	switch provider {
	case "anthropic":
		switch raw {
		case "end_turn":
			return "end_turn"
		case "tool_use":
			return "tool_use"
		case "max_tokens":
			return "max_tokens"
		default:
			return "" // stop_sequence, pause_turn, etc. — caller checks RawStopReason
		}
	case "openai":
		switch raw {
		case "stop":
			return "end_turn"
		case "tool_calls":
			return "tool_use"
		case "length":
			return "max_tokens"
		case "content_filter":
			return "content_filter"
		default:
			return ""
		}
	default:
		return ""
	}
}
