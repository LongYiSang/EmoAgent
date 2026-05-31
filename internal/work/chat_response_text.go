package work

import (
	"strings"

	"github.com/longyisang/emoagent/internal/llm"
)

func chatResponseText(resp *llm.ChatResponse) string {
	if resp == nil {
		return ""
	}
	if strings.TrimSpace(resp.Content) != "" {
		return resp.Content
	}
	var b strings.Builder
	for _, block := range resp.ContentBlocks {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			b.WriteString(block.Text)
		}
	}
	return strings.TrimSpace(b.String())
}
