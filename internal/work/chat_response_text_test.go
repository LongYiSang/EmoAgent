package work

import (
	"testing"

	"github.com/longyisang/emoagent/internal/llm"
)

func TestChatResponseTextPreservesDirectContent(t *testing.T) {
	resp := &llm.ChatResponse{
		Content: "  direct content\n",
		ContentBlocks: []llm.ContentBlock{
			{Type: "text", Text: "block content"},
		},
	}

	if got := chatResponseText(resp); got != "  direct content\n" {
		t.Fatalf("chatResponseText() = %q, want direct content with whitespace", got)
	}
}

func TestChatResponseTextConcatenatesTextBlocks(t *testing.T) {
	resp := &llm.ChatResponse{
		ContentBlocks: []llm.ContentBlock{
			{Type: "text", Text: "  part one\n"},
			{Type: "tool_use", Name: "ignored"},
			{Type: "text", Text: "part two  "},
		},
	}

	if got := chatResponseText(resp); got != "part one\npart two" {
		t.Fatalf("chatResponseText() = %q, want concatenated text blocks", got)
	}
}
