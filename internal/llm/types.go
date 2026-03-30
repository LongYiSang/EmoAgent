package llm

// Role represents a message role in the conversation.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is a single message in a conversation.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// ChatRequest is a unified request to any LLM provider.
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	System      string    `json:"system,omitempty"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature float64   `json:"temperature"`
	Stream      bool      `json:"stream"`
}

// ChatResponse is a unified response from any LLM provider.
type ChatResponse struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Model   string `json:"model"`
	Usage   Usage  `json:"usage"`
}

// Usage tracks token consumption.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// StreamEvent represents a single event during streaming.
type StreamEvent struct {
	Content string // text delta
	Done    bool   // true when stream is complete
}

// StreamCallback is called for each streaming chunk.
type StreamCallback func(event StreamEvent)
