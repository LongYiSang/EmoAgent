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
	"strings"
	"time"
)

// Client is the interface for LLM providers.
type Client interface {
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	ChatStream(ctx context.Context, req ChatRequest, cb StreamCallback) (*ChatResponse, error)
}

// NewClient creates a Client based on provider connection settings.
func NewClient(cfg ProviderConfig, logger *slog.Logger) (Client, error) {
	apiKeyEnv := cfg.APIKeyEnv
	if apiKeyEnv == "" {
		apiKeyEnv = defaultAPIKeyEnv(cfg.Protocol)
	}
	if apiKeyEnv == "" {
		return nil, fmt.Errorf("unsupported LLM provider: %s", cfg.Protocol)
	}

	apiKey := os.Getenv(apiKeyEnv)
	if apiKey == "" {
		return nil, fmt.Errorf("%s environment variable not set", apiKeyEnv)
	}

	switch cfg.Protocol {
	case "openai", "openai_compatible":
		return &openaiClient{
			providerID: cfg.ID,
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
		return nil, fmt.Errorf("unsupported LLM provider: %s", cfg.Protocol)
	}
}

func defaultAPIKeyEnv(provider string) string {
	switch provider {
	case "openai", "openai_compatible":
		return "OPENAI_API_KEY"
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	default:
		return ""
	}
}

// --- OpenAI compatible implementation ---

type openaiClient struct {
	providerID string
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
	Role             string           `json:"role"`
	Content          *string          `json:"content"`                     // nil for tool_call-only assistant messages
	ReasoningContent *string          `json:"reasoning_content,omitempty"` // thinking/reasoning model output
	ToolCalls        []openaiToolCall `json:"tool_calls,omitempty"`        // assistant tool calls
	ToolCallID       string           `json:"tool_call_id,omitempty"`      // tool result message
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
			Content          *string          `json:"content"`
			ReasoningContent *string          `json:"reasoning_content"`
			ToolCalls        []openaiToolCall `json:"tool_calls,omitempty"`
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
			Content          *string          `json:"content"`
			ReasoningContent *string          `json:"reasoning_content"`
			ToolCalls        []openaiToolCall `json:"tool_calls,omitempty"`
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
			if m.ReasoningContent != "" {
				msg.ReasoningContent = strPtr(m.ReasoningContent)
			}
			msgs = append(msgs, msg)

		default:
			// Simple text message.
			msg := openaiMessage{Role: string(m.Role), Content: strPtr(m.Content)}
			if m.ReasoningContent != "" {
				msg.ReasoningContent = strPtr(m.ReasoningContent)
			}
			msgs = append(msgs, msg)
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
	body, err := json.Marshal(c.openaiPayload(req, false))
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
		return nil, wrapRequestError("openai", "chat", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, wrapStatusError("openai", "chat", resp.StatusCode, string(respBody))
	}

	var oResp openaiResponse
	if err := json.NewDecoder(resp.Body).Decode(&oResp); err != nil {
		return nil, wrapDecodeError("openai", "chat", err)
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

		// Reasoning content (thinking models).
		if choice.Message.ReasoningContent != nil {
			chatResp.ReasoningContent = *choice.Message.ReasoningContent
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
	payload := c.openaiPayload(req, true)
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	c.logger.Debug("llm http request", "model", req.Model, "messages_count", len(req.Messages))

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
		return nil, wrapRequestError("openai", "chat_stream", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		c.logger.Error("llm http error", "status", resp.StatusCode, "body", string(respBody))
		return nil, wrapStatusError("openai", "chat_stream", resp.StatusCode, string(respBody))
	}

	decoder := NewSSEDecoder(resp.Body)
	var accumulated string
	var accumulatedReasoning string
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
			return nil, wrapStreamDecodeError("openai", "chat_stream", err)
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

			// Reasoning content delta (thinking models).
			if choice.Delta.ReasoningContent != nil && *choice.Delta.ReasoningContent != "" {
				delta := *choice.Delta.ReasoningContent
				accumulatedReasoning += delta
				if cb != nil {
					cb(StreamEvent{Type: "reasoning", ReasoningContent: delta})
				}
			}

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
	chatResp.ReasoningContent = accumulatedReasoning
	return &chatResp, nil
}

func (c *openaiClient) openaiPayload(req ChatRequest, stream bool) map[string]any {
	params := effectiveRequestParams(req)
	payload := sanitizedExtra(params.Extra, map[string]struct{}{
		"model": {}, "messages": {}, "max_tokens": {}, "temperature": {}, "top_p": {},
		"presence_penalty": {}, "frequency_penalty": {}, "reasoning_effort": {}, "stream": {}, "tools": {},
	})
	flavor := c.openaiCompatFlavor(req.Model)
	payload["model"] = req.Model
	payload["messages"] = c.toMessages(req)
	if params.MaxTokens > 0 {
		payload["max_tokens"] = params.MaxTokens
	}
	if params.Temperature != nil {
		payload["temperature"] = *params.Temperature
	}
	if params.TopP != nil {
		payload["top_p"] = *params.TopP
	}
	if params.PresencePenalty != nil {
		payload["presence_penalty"] = *params.PresencePenalty
	}
	if params.FrequencyPenalty != nil {
		payload["frequency_penalty"] = *params.FrequencyPenalty
	}
	reasoningEffort := params.ReasoningEffort
	if reasoningEffort == "" && flavor == "deepseek" && params.Thinking != nil {
		reasoningEffort = strings.TrimSpace(params.Thinking.Effort)
	}
	if reasoningEffort != "" {
		payload["reasoning_effort"] = reasoningEffort
	}
	if thinking := providerThinkingPayload(flavor, params.Thinking); thinking != nil {
		payload["thinking"] = thinking
	}
	if stream {
		payload["stream"] = true
	}
	if tools := c.convertTools(req.Tools); len(tools) > 0 {
		payload["tools"] = tools
	}
	return payload
}

func (c *openaiClient) openaiCompatFlavor(model string) string {
	providerID := strings.ToLower(strings.TrimSpace(c.providerID))
	baseURL := strings.ToLower(strings.TrimSpace(c.baseURL))
	model = strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(providerID, "deepseek") || strings.Contains(baseURL, "api.deepseek.com") || strings.HasPrefix(model, "deepseek-"):
		return "deepseek"
	case strings.Contains(providerID, "moonshot") || strings.Contains(providerID, "kimi") || strings.Contains(baseURL, "api.moonshot.cn") || strings.Contains(model, "kimi"):
		return "moonshot"
	default:
		return ""
	}
}

func providerThinkingPayload(flavor string, thinking *ThinkingConfig) map[string]any {
	if flavor != "moonshot" && flavor != "deepseek" {
		return nil
	}
	if thinking == nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(thinking.Mode)) {
	case "enabled", "manual":
		return map[string]any{"type": "enabled"}
	case "disabled":
		return map[string]any{"type": "disabled"}
	default:
		return nil
	}
}

func effectiveRequestParams(req ChatRequest) RequestParams {
	if hasRequestParams(req.Params) {
		return req.Params
	}
	params := RequestParams{
		MaxTokens: req.MaxTokens,
		Stream:    &req.Stream,
	}
	params.Temperature = &req.Temperature
	return params
}

func hasRequestParams(params RequestParams) bool {
	return params.MaxTokens != 0 ||
		params.Temperature != nil ||
		params.TopP != nil ||
		params.PresencePenalty != nil ||
		params.FrequencyPenalty != nil ||
		params.ReasoningEffort != "" ||
		params.Thinking != nil ||
		params.Stream != nil ||
		len(params.Extra) > 0
}

func sanitizedExtra(extra map[string]any, blocked map[string]struct{}) map[string]any {
	payload := make(map[string]any, len(extra))
	for key, value := range extra {
		if _, skip := blocked[key]; skip {
			continue
		}
		payload[key] = value
	}
	return payload
}
