package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/longyisang/emoagent/internal/config"
)

// Client is the interface for LLM providers.
type Client interface {
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	ChatStream(ctx context.Context, req ChatRequest, cb StreamCallback) (*ChatResponse, error)
}

// NewClient creates a Client based on the provider in config.
func NewClient(cfg config.LLMConfig, logger *slog.Logger) (Client, error) {
	apiKeyEnv := cfg.APIKeyEnv
	if apiKeyEnv == "" {
		apiKeyEnv = defaultAPIKeyEnv(cfg.Provider)
	}
	if apiKeyEnv == "" {
		return nil, fmt.Errorf("unsupported LLM provider: %s", cfg.Provider)
	}

	apiKey := os.Getenv(apiKeyEnv)
	if apiKey == "" {
		return nil, fmt.Errorf("%s environment variable not set", apiKeyEnv)
	}

	switch cfg.Provider {
	case "openai":
		return &openaiClient{
			baseURL:    cfg.BaseURL,
			apiKey:     apiKey,
			httpClient: &http.Client{Timeout: 120 * time.Second},
			logger:     logger,
		}, nil
	case "anthropic":
		return &anthropicClient{
			baseURL:    cfg.BaseURL,
			apiKey:     apiKey,
			httpClient: &http.Client{Timeout: 120 * time.Second},
			logger:     logger,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", cfg.Provider)
	}
}

func defaultAPIKeyEnv(provider string) string {
	switch provider {
	case "openai":
		return "OPENAI_API_KEY"
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	default:
		return ""
	}
}

// --- OpenAI compatible implementation ---

type openaiClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	logger     *slog.Logger
}

// openaiRequest is the OpenAI chat completions request format.
type openaiRequest struct {
	Model       string          `json:"model"`
	Messages    []openaiMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature"`
	Stream      bool            `json:"stream,omitempty"`
	Tools       []openaiTool    `json:"tools,omitempty"`
}

type openaiMessage struct {
	Role       string           `json:"role"`
	Content    *string          `json:"content"`                // nil for tool_call-only assistant messages
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`   // assistant tool calls
	ToolCallID string           `json:"tool_call_id,omitempty"` // tool result message
}

type openaiTool struct {
	Type     string             `json:"type"` // "function"
	Function openaiToolFunction `json:"function"`
}

type openaiToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type openaiToolCall struct {
	ID       string `json:"id"`
	Index    int    `json:"index,omitempty"`
	Type     string `json:"type"` // "function"
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openaiResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content   *string          `json:"content"`
			ToolCalls []openaiToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type openaiStreamChunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content   *string          `json:"content"`
			ToolCalls []openaiToolCall `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func strPtr(s string) *string { return &s }

func (c *openaiClient) convertTools(tools []ToolDef) []openaiTool {
	if len(tools) == 0 {
		return nil
	}
	ot := make([]openaiTool, len(tools))
	for i, t := range tools {
		ot[i] = openaiTool{
			Type: "function",
			Function: openaiToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		}
	}
	return ot
}

func (c *openaiClient) toMessages(req ChatRequest) []openaiMessage {
	var msgs []openaiMessage
	if req.System != "" {
		msgs = append(msgs, openaiMessage{Role: "system", Content: strPtr(req.System)})
	}
	for _, m := range req.Messages {
		switch {
		case m.Role == RoleTool && m.ToolCallID != "":
			// Tool result message.
			msgs = append(msgs, openaiMessage{
				Role:       "tool",
				Content:    strPtr(m.Content),
				ToolCallID: m.ToolCallID,
			})

		case len(m.ContentBlocks) > 0:
			// Structured message — extract tool_use blocks into tool_calls,
			// and collect text blocks into content.
			var textParts []string
			var toolCalls []openaiToolCall
			for _, cb := range m.ContentBlocks {
				switch cb.Type {
				case "text":
					textParts = append(textParts, cb.Text)
				case "tool_use":
					toolCalls = append(toolCalls, openaiToolCall{
						ID:   cb.ID,
						Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{
							Name:      cb.Name,
							Arguments: string(cb.Input),
						},
					})
				}
			}

			msg := openaiMessage{Role: string(m.Role)}
			if len(textParts) > 0 {
				joined := ""
				for _, p := range textParts {
					joined += p
				}
				msg.Content = strPtr(joined)
			}
			if len(toolCalls) > 0 {
				msg.ToolCalls = toolCalls
			}
			msgs = append(msgs, msg)

		default:
			// Simple text message.
			msgs = append(msgs, openaiMessage{Role: string(m.Role), Content: strPtr(m.Content)})
		}
	}
	return msgs
}

func (c *openaiClient) toolCallsToContentBlocks(calls []openaiToolCall) []ContentBlock {
	blocks := make([]ContentBlock, len(calls))
	for i, tc := range calls {
		blocks[i] = ContentBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: json.RawMessage(tc.Function.Arguments),
		}
	}
	return blocks
}

func (c *openaiClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	oReq := openaiRequest{
		Model:       req.Model,
		Messages:    c.toMessages(req),
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      false,
		Tools:       c.convertTools(req.Tools),
	}

	body, err := json.Marshal(oReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var oResp openaiResponse
	if err := json.NewDecoder(resp.Body).Decode(&oResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	chatResp := &ChatResponse{
		ID:    oResp.ID,
		Model: oResp.Model,
		Usage: Usage{
			InputTokens:  oResp.Usage.PromptTokens,
			OutputTokens: oResp.Usage.CompletionTokens,
		},
	}

	if len(oResp.Choices) > 0 {
		choice := oResp.Choices[0]

		// Stop reason.
		chatResp.RawStopReason = choice.FinishReason
		chatResp.StopReason = NormalizeStopReason("openai", choice.FinishReason)

		// Content.
		if choice.Message.Content != nil {
			chatResp.Content = *choice.Message.Content
			chatResp.ContentBlocks = append(chatResp.ContentBlocks, ContentBlock{
				Type: "text",
				Text: *choice.Message.Content,
			})
		}

		// Tool calls.
		if len(choice.Message.ToolCalls) > 0 {
			chatResp.ContentBlocks = append(chatResp.ContentBlocks,
				c.toolCallsToContentBlocks(choice.Message.ToolCalls)...)
		}
	}

	return chatResp, nil
}

func (c *openaiClient) ChatStream(ctx context.Context, req ChatRequest, cb StreamCallback) (*ChatResponse, error) {
	oReq := openaiRequest{
		Model:       req.Model,
		Messages:    c.toMessages(req),
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      true,
		Tools:       c.convertTools(req.Tools),
	}

	body, err := json.Marshal(oReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	c.logger.Debug("llm http request", "model", oReq.Model, "messages_count", len(oReq.Messages))

	// Use a client without timeout for streaming — context handles cancellation.
	streamClient := &http.Client{}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := streamClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		c.logger.Error("llm http error", "status", resp.StatusCode, "body", string(respBody))
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	decoder := NewSSEDecoder(resp.Body)
	var accumulated string
	var chatResp ChatResponse

	// State for accumulating tool calls during streaming.
	// OpenAI streams tool calls with index-based interleaving.
	type pendingToolCall struct {
		id      string
		name    string
		argsBuf string
	}
	pendingCalls := make(map[int]*pendingToolCall)

	for {
		event, err := decoder.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("sse decode: %w", err)
		}

		var chunk openaiStreamChunk
		if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
			c.logger.Debug("skip malformed chunk", "data", event.Data, "error", err)
			continue
		}

		chatResp.ID = chunk.ID
		chatResp.Model = chunk.Model

		if len(chunk.Choices) > 0 {
			choice := chunk.Choices[0]

			// Text delta.
			if choice.Delta.Content != nil && *choice.Delta.Content != "" {
				delta := *choice.Delta.Content
				accumulated += delta
				if cb != nil {
					cb(StreamEvent{Type: "text", Content: delta})
				}
			}

			// Tool call deltas.
			for _, tc := range choice.Delta.ToolCalls {
				idx := tc.Index
				p, ok := pendingCalls[idx]
				if !ok {
					p = &pendingToolCall{}
					pendingCalls[idx] = p
				}
				if tc.ID != "" {
					p.id = tc.ID
				}
				if tc.Function.Name != "" {
					p.name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					p.argsBuf += tc.Function.Arguments
				}
			}

			// Finish reason.
			if choice.FinishReason != nil {
				chatResp.RawStopReason = *choice.FinishReason
				chatResp.StopReason = NormalizeStopReason("openai", *choice.FinishReason)
			}
		}

		if chunk.Usage != nil {
			chatResp.Usage = Usage{
				InputTokens:  chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
			}
		}
	}

	// Build ContentBlocks from accumulated state.
	if accumulated != "" {
		chatResp.ContentBlocks = append(chatResp.ContentBlocks, ContentBlock{
			Type: "text",
			Text: accumulated,
		})
	}

	// Finalize pending tool calls.
	indexes := make([]int, 0, len(pendingCalls))
	for idx := range pendingCalls {
		indexes = append(indexes, idx)
	}
	sort.Ints(indexes)
	for _, idx := range indexes {
		p := pendingCalls[idx]
		input := json.RawMessage(p.argsBuf)
		if p.argsBuf == "" {
			input = json.RawMessage("{}")
		}
		block := ContentBlock{
			Type:  "tool_use",
			ID:    p.id,
			Name:  p.name,
			Input: input,
		}
		chatResp.ContentBlocks = append(chatResp.ContentBlocks, block)
		if cb != nil {
			cb(StreamEvent{Type: "tool_use", ContentBlock: &block})
		}
	}

	if cb != nil {
		cb(StreamEvent{Done: true})
	}

	chatResp.Content = accumulated
	return &chatResp, nil
}
