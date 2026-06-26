package rerank

import (
	"context"

	"github.com/longyisang/emoagent/internal/storage"
)

type Provider interface {
	Name() string
	Rerank(ctx context.Context, req Request) (Response, error)
}

type UsageRecorder interface {
	RecordLLMUsageEvent(context.Context, storage.LLMUsageEvent) error
}

type Request struct {
	Query     string
	Model     string
	Documents []Document
	TopK      int
}

type Document struct {
	ID       string
	Index    int
	Text     string
	Metadata map[string]string
}

type Response struct {
	Results []Result
	Usage   Usage
}

type Result struct {
	ID    string
	Index int
	Score float64
}

type Usage struct {
	Documents   int
	InputTokens int
	TotalTokens int
}
