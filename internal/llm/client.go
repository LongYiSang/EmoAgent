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
}

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
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
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (c *openaiClient) toMessages(req ChatRequest) []openaiMessage {
	var msgs []openaiMessage
	if req.System != "" {
		msgs = append(msgs, openaiMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, openaiMessage{Role: string(m.Role), Content: m.Content})
	}
	return msgs
}

func (c *openaiClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	oReq := openaiRequest{
		Model:       req.Model,
		Messages:    c.toMessages(req),
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      false,
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

	content := ""
	if len(oResp.Choices) > 0 {
		content = oResp.Choices[0].Message.Content
	}

	return &ChatResponse{
		ID:      oResp.ID,
		Content: content,
		Model:   oResp.Model,
		Usage: Usage{
			InputTokens:  oResp.Usage.PromptTokens,
			OutputTokens: oResp.Usage.CompletionTokens,
		},
	}, nil
}

func (c *openaiClient) ChatStream(ctx context.Context, req ChatRequest, cb StreamCallback) (*ChatResponse, error) {
	oReq := openaiRequest{
		Model:       req.Model,
		Messages:    c.toMessages(req),
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      true,
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
			delta := chunk.Choices[0].Delta.Content
			if delta != "" {
				accumulated += delta
				if cb != nil {
					cb(StreamEvent{Content: delta})
				}
			}
		}

		if chunk.Usage != nil {
			chatResp.Usage = Usage{
				InputTokens:  chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
			}
		}
	}

	if cb != nil {
		cb(StreamEvent{Done: true})
	}

	chatResp.Content = accumulated
	return &chatResp, nil
}
