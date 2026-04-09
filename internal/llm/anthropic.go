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
	baseURL    string
	apiKey     string
	httpClient *http.Client
	logger     *slog.Logger
}

// --- Anthropic Messages API types ---

type anthropicRequest struct {
	Model       string             `json:"model"`
	Messages    []anthropicMessage `json:"messages"`
	System      string             `json:"system,omitempty"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature float64            `json:"temperature"`
	Stream      bool               `json:"stream,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type anthropicStreamEvent struct {
	Type  string          `json:"type"`
	Index int             `json:"index,omitempty"`
	Delta json.RawMessage `json:"delta,omitempty"`
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage,omitempty"`
	Message *anthropicResponse `json:"message,omitempty"`
}

type anthropicDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (c *anthropicClient) toMessages(req ChatRequest) []anthropicMessage {
	var msgs []anthropicMessage
	for _, m := range req.Messages {
		msgs = append(msgs, anthropicMessage{Role: string(m.Role), Content: m.Content})
	}
	return msgs
}

func (c *anthropicClient) doRequest(ctx context.Context, body []byte, stream bool) (*http.Response, error) {
	client := c.httpClient
	if stream {
		client = &http.Client{} // no timeout for streaming
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		c.logger.Error("llm http error", "status", resp.StatusCode, "body", string(respBody))
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	return resp, nil
}

func (c *anthropicClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	aReq := anthropicRequest{
		Model:       req.Model,
		Messages:    c.toMessages(req),
		System:      req.System,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      false,
	}

	body, err := json.Marshal(aReq)
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
		return nil, fmt.Errorf("decode response: %w", err)
	}

	content := ""
	for _, block := range aResp.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}

	return &ChatResponse{
		ID:      aResp.ID,
		Content: content,
		Model:   aResp.Model,
		Usage: Usage{
			InputTokens:  aResp.Usage.InputTokens,
			OutputTokens: aResp.Usage.OutputTokens,
		},
	}, nil
}

func (c *anthropicClient) ChatStream(ctx context.Context, req ChatRequest, cb StreamCallback) (*ChatResponse, error) {
	aReq := anthropicRequest{
		Model:       req.Model,
		Messages:    c.toMessages(req),
		System:      req.System,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      true,
	}

	body, err := json.Marshal(aReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	c.logger.Debug("llm http request", "model", aReq.Model, "messages_count", len(aReq.Messages))

	resp, err := c.doRequest(ctx, body, true)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	decoder := NewSSEDecoder(resp.Body)
	var accumulated string
	var chatResp ChatResponse

	for {
		event, err := decoder.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("sse decode: %w", err)
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
			}

		case "content_block_delta":
			var delta anthropicDelta
			if err := json.Unmarshal(se.Delta, &delta); err == nil && delta.Text != "" {
				accumulated += delta.Text
				if cb != nil {
					cb(StreamEvent{Content: delta.Text})
				}
			}

		case "message_delta":
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

	chatResp.Content = accumulated
	return &chatResp, nil
}
