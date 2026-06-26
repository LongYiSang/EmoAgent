package tokenmeter

import (
	"context"

	"github.com/longyisang/emoagent/internal/llm"
)

const MethodHeuristicCJK = "heuristic_cjk"

type CountRequest struct {
	ProviderID string
	Protocol   string
	Model      string
	Purpose    string
	System     string
	Messages   []llm.Message
	Tools      []llm.ToolDef
	Params     llm.RequestParams
	BudgetMode bool
}

type CountResult struct {
	InputTokens  int
	OutputTokens int
	Method       string
	Confidence   float64
	Warnings     []string
}

type Counter interface {
	CountText(ctx context.Context, providerID, model, text string) CountResult
	CountMessages(ctx context.Context, providerID, model string, messages []llm.Message) CountResult
	CountChatRequest(ctx context.Context, req CountRequest) CountResult
	CountChatResponse(ctx context.Context, providerID, model string, resp *llm.ChatResponse) CountResult
}
