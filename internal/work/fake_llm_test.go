package work

import (
	"context"
	"fmt"

	"github.com/longyisang/emoagent/internal/llm"
)

// scriptedLLM is a test-only llm.Client that returns scripted responses in
// order and records each request for assertions.
type scriptedLLM struct {
	responses []*llm.ChatResponse
	errs      []error
	calls     []llm.ChatRequest
	index     int
}

func (s *scriptedLLM) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	return s.ChatStream(ctx, req, nil)
}

func (s *scriptedLLM) ChatStream(_ context.Context, req llm.ChatRequest, _ llm.StreamCallback) (*llm.ChatResponse, error) {
	s.calls = append(s.calls, req)
	if s.index >= len(s.responses) && s.index >= len(s.errs) {
		return nil, fmt.Errorf("scriptedLLM: no scripted response for call %d", s.index)
	}

	var resp *llm.ChatResponse
	var err error
	if s.index < len(s.responses) {
		resp = s.responses[s.index]
	}
	if s.index < len(s.errs) {
		err = s.errs[s.index]
	}
	s.index++
	return resp, err
}

func textResp(text string) *llm.ChatResponse {
	return &llm.ChatResponse{
		ID:         "resp-final",
		Content:    text,
		StopReason: "end_turn",
		ContentBlocks: []llm.ContentBlock{
			{Type: "text", Text: text},
		},
	}
}

func toolUseResp(callID, name, inputJSON string) *llm.ChatResponse {
	return &llm.ChatResponse{
		ID:         "resp-tool",
		StopReason: "tool_use",
		ContentBlocks: []llm.ContentBlock{
			{Type: "tool_use", ID: callID, Name: name, Input: []byte(inputJSON)},
		},
	}
}
