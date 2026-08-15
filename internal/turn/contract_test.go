package turn

import (
	"testing"

	"github.com/longyisang/emoagent/internal/llm"
)

func TestInboundEnvelopeUserAccessorsHandleNilUserMessage(t *testing.T) {
	// System-initiated turns (proactive prompts, resumes) carry no UserMessage.
	// The accessors must stay safe so pipeline stages never dereference nil.
	env := InboundEnvelope{Source: SourceSystem, Kind: InboundSystemResume}

	if got := env.UserContent(); got != "" {
		t.Fatalf("UserContent() = %q, want empty", got)
	}
	if got := env.UserParts(); got != nil {
		t.Fatalf("UserParts() = %v, want nil", got)
	}
}

func TestInboundEnvelopeUserAccessorsReturnUserMessage(t *testing.T) {
	parts := []llm.ContentBlock{{Type: "text", Text: "hello"}}
	env := InboundEnvelope{
		Source:      SourceWebUI,
		Kind:        InboundUserMessage,
		UserMessage: &UserMessageInput{Content: "hello", Parts: parts},
	}

	if got := env.UserContent(); got != "hello" {
		t.Fatalf("UserContent() = %q, want %q", got, "hello")
	}
	if got := env.UserParts(); len(got) != 1 || got[0].Text != "hello" {
		t.Fatalf("UserParts() = %v, want one text block", got)
	}
}
