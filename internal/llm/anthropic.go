package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

type anthropicClient struct {
	baseURL      string
	messagesPath string
	apiKey       string
	httpClient   *http.Client
	logger       *slog.Logger
}

// --- Anthropic Messages API types ---

type anthropicToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicRequest struct {
	Model       string             `json:"model"`
	Messages    []anthropicMessage `json:"messages"`
	System      string             `json:"system,omitempty"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature float64            `json:"temperature"`
	Stream      bool               `json:"stream,omitempty"`
	Tools       []anthropicToolDef `json:"tools,omitempty"`
}

// anthropicMessage uses interface{} for Content to support both string and
// structured content block arrays on the wire.
type anthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

// anthropicContentBlock is the wire format for Anthropic content blocks.
type anthropicContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`          // tool_use
	Name      string          `json:"name,omitempty"`        // tool_use
	Input     json.RawMessage `json:"input,omitempty"`       // tool_use
	ToolUseID string          `json:"tool_use_id,omitempty"` // tool_result (wire field name)
	Content   string          `json:"content,omitempty"`     // tool_result
	IsError   bool            `json:"is_error,omitempty"`    // tool_result
}

type anthropicResponse struct {
	ID         string                  `json:"id"`
	Model      string                  `json:"model"`
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type anthropicStreamEvent struct {
	Type         string                 `json:"type"`
	Index        int                    `json:"index,omitempty"`
	Delta        json.RawMessage        `json:"delta,omitempty"`
	ContentBlock *anthropicContentBlock `json:"content_block,omitempty"`
	Usage        *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage,omitempty"`
	Message *anthropicResponse `json:"message,omitempty"`
}

type anthropicDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
}

func (c *anthropicClient) toMessages(req ChatRequest) []anthropicMessage {
	var msgs []anthropicMessage
	for _, m := range req.Messages {
		if len(m.ContentBlocks) > 0 {
			// Structured content: convert ContentBlocks to Anthropic wire format.
			blocks := make([]anthropicContentBlock, 0, len(m.ContentBlocks))
			for _, cb := range m.ContentBlocks {
				ab := anthropicContentBlock{Type: cb.Type}
				switch cb.Type {
				case "text":
					ab.Text = cb.Text
				case "tool_use":
					ab.ID = cb.ID
					ab.Name = cb.Name
					ab.Input = cb.Input
				case "tool_result":
					ab.ToolUseID = cb.ID // ID → tool_use_id on wire
					ab.Content = cb.Content
					ab.IsError = cb.IsError
				}
				blocks = append(blocks, ab)
			}
			msgs = append(msgs, anthropicMessage{
				Role:    string(m.Role),
				Content: blocks,
			})
		} else {
			// Simple text message — backward compatible.
			msgs = append(msgs, anthropicMessage{
				Role:    string(m.Role),
				Content: m.Content,
			})
		}
	}
	return msgs
}

func (c *anthropicClient) convertTools(tools []ToolDef) []anthropicToolDef {
	if len(tools) == 0 {
		return nil
	}
	at := make([]anthropicToolDef, len(tools))
	for i, t := range tools {
		at[i] = anthropicToolDef{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}
	return at
}

func (c *anthropicClient) parseContentBlocks(content []anthropicContentBlock) (string, []ContentBlock) {
	var text string
	var blocks []ContentBlock
	for _, ab := range content {
		switch ab.Type {
		case "text":
			text += ab.Text
			blocks = append(blocks, ContentBlock{Type: "text", Text: ab.Text})
		case "tool_use":
			blocks = append(blocks, ContentBlock{
				Type:  "tool_use",
				ID:    ab.ID,
				Name:  ab.Name,
				Input: ab.Input,
			})
		}
	}
	return text, blocks
}

func (c *anthropicClient) doRequest(ctx context.Context, body []byte, stream bool) (*http.Response, error) {
	client := c.httpClient
	if stream {
		client = &http.Client{} // no timeout for streaming
	}

	messagesPath := c.messagesPath
	if messagesPath == "" {
		messagesPath = "/v1/messages"
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpointURL(c.baseURL, messagesPath), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, wrapRequestError("anthropic", "messages", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		c.logger.Error("llm http error", "status", resp.StatusCode, "body", string(respBody))
		return nil, wrapStatusError("anthropic", "messages", resp.StatusCode, string(respBody))
	}

	return resp, nil
}

func (c *anthropicClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	body, err := json.Marshal(c.anthropicPayload(req, false))
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := c.doRequest(ctx, body, false)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var aResp anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&aResp); err != nil {
		return nil, wrapDecodeError("anthropic", "messages", err)
	}

	content, contentBlocks := c.parseContentBlocks(aResp.Content)

	return &ChatResponse{
		ID:            aResp.ID,
		Content:       content,
		ContentBlocks: contentBlocks,
		Model:         aResp.Model,
		Usage: Usage{
			InputTokens:  aResp.Usage.InputTokens,
			OutputTokens: aResp.Usage.OutputTokens,
		},
		StopReason:    NormalizeStopReason("anthropic", aResp.StopReason),
		RawStopReason: aResp.StopReason,
	}, nil
}

func (c *anthropicClient) ChatStream(ctx context.Context, req ChatRequest, cb StreamCallback) (*ChatResponse, error) {
	body, err := json.Marshal(c.anthropicPayload(req, true))
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	c.logger.Debug("llm http request", "model", req.Model, "messages_count", len(req.Messages))

	resp, err := c.doRequest(ctx, body, true)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	decoder := NewSSEDecoder(resp.Body)
	var accumulated string
	var chatResp ChatResponse

	// State for accumulating tool_use blocks during streaming.
	type pendingToolUse struct {
		id       string
		name     string
		inputBuf string // raw JSON accumulated from input_json_delta
	}
	var currentBlock *pendingToolUse

	for {
		event, err := decoder.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, wrapStreamDecodeError("anthropic", "messages_stream", err)
		}

		var se anthropicStreamEvent
		if err := json.Unmarshal([]byte(event.Data), &se); err != nil {
			c.logger.Debug("skip malformed event", "data", event.Data, "error", err)
			continue
		}

		switch se.Type {
		case "message_start":
			if se.Message != nil {
				chatResp.ID = se.Message.ID
				chatResp.Model = se.Message.Model
				if se.Message.Usage.InputTokens > 0 {
					chatResp.Usage.InputTokens = se.Message.Usage.InputTokens
				}
			}

		case "content_block_start":
			if se.ContentBlock != nil && se.ContentBlock.Type == "tool_use" {
				currentBlock = &pendingToolUse{
					id:   se.ContentBlock.ID,
					name: se.ContentBlock.Name,
				}
			}

		case "content_block_delta":
			var delta anthropicDelta
			if err := json.Unmarshal(se.Delta, &delta); err != nil {
				continue
			}
			switch delta.Type {
			case "text_delta":
				if delta.Text != "" {
					accumulated += delta.Text
					if cb != nil {
						cb(StreamEvent{Type: "text", Content: delta.Text})
					}
				}
			case "input_json_delta":
				if currentBlock != nil && delta.PartialJSON != "" {
					currentBlock.inputBuf += delta.PartialJSON
				}
			}

		case "content_block_stop":
			if currentBlock != nil {
				// Finalize the accumulated tool_use block.
				block := ContentBlock{
					Type:  "tool_use",
					ID:    currentBlock.id,
					Name:  currentBlock.name,
					Input: json.RawMessage(currentBlock.inputBuf),
				}
				if currentBlock.inputBuf == "" {
					block.Input = json.RawMessage("{}")
				}
				chatResp.ContentBlocks = append(chatResp.ContentBlocks, block)
				if cb != nil {
					cb(StreamEvent{Type: "tool_use", ContentBlock: &block})
				}
				currentBlock = nil
			}

		case "message_delta":
			var delta anthropicDelta
			if err := json.Unmarshal(se.Delta, &delta); err == nil {
				if delta.StopReason != "" {
					chatResp.RawStopReason = delta.StopReason
					chatResp.StopReason = NormalizeStopReason("anthropic", delta.StopReason)
				}
			}
			if se.Usage != nil {
				chatResp.Usage = Usage{
					InputTokens:  se.Usage.InputTokens,
					OutputTokens: se.Usage.OutputTokens,
				}
			}

		case "message_stop":
			// Stream complete.
		}
	}

	if cb != nil {
		cb(StreamEvent{Done: true})
	}

	// Add text block to ContentBlocks if there was accumulated text.
	if accumulated != "" {
		textBlock := ContentBlock{Type: "text", Text: accumulated}
		// Prepend text block before any tool_use blocks.
		chatResp.ContentBlocks = append([]ContentBlock{textBlock}, chatResp.ContentBlocks...)
	}

	chatResp.Content = accumulated
	return &chatResp, nil
}

func (c *anthropicClient) anthropicPayload(req ChatRequest, stream bool) map[string]any {
	params := effectiveRequestParams(req)
	payload := sanitizedExtra(params.Extra, map[string]struct{}{
		"model": {}, "messages": {}, "system": {}, "max_tokens": {}, "temperature": {}, "top_p": {},
		"presence_penalty": {}, "frequency_penalty": {}, "reasoning_effort": {}, "thinking": {},
		"output_config": {}, "stream": {}, "tools": {},
	})
	payload["model"] = req.Model
	payload["messages"] = c.toMessages(req)
	if req.System != "" {
		payload["system"] = req.System
	}
	if params.MaxTokens > 0 {
		payload["max_tokens"] = params.MaxTokens
	}
	if params.Temperature != nil {
		payload["temperature"] = *params.Temperature
	}
	if params.TopP != nil {
		payload["top_p"] = *params.TopP
	}
	if params.Thinking != nil {
		switch params.Thinking.Mode {
		case "manual":
			thinking := map[string]any{"type": "enabled"}
			if params.Thinking.BudgetTokens != nil {
				thinking["budget_tokens"] = *params.Thinking.BudgetTokens
			}
			payload["thinking"] = thinking
		case "adaptive":
			payload["thinking"] = map[string]any{"type": "adaptive"}
			if params.Thinking.Effort != "" {
				payload["output_config"] = map[string]any{"effort": params.Thinking.Effort}
			}
		}
	}
	if stream {
		payload["stream"] = true
	}
	if tools := c.convertTools(req.Tools); len(tools) > 0 {
		payload["tools"] = tools
	}
	return payload
}
