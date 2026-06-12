package llm

import (
	"strings"
	"testing"
)

func TestRenderMessagesForHistoryCollapsesImages(t *testing.T) {
	messages := []Message{{
		Role:    RoleUser,
		Content: "look\n[used image]",
		ContentBlocks: []ContentBlock{
			{Type: string(PartText), Text: "look"},
			{Type: string(PartImage), Media: &MediaPart{MediaAssetID: "med_abc", Kind: "image", MimeType: "image/png", Data: []byte("iVBORw0KGgo=")}},
		},
	}}

	rendered := RenderMessages(messages, RenderForHistory, RenderPolicy{})

	if len(rendered) != 1 {
		t.Fatalf("len(rendered) = %d, want 1", len(rendered))
	}
	if rendered[0].Content != "look\n[used image]" {
		t.Fatalf("content = %q, want text plus placeholder", rendered[0].Content)
	}
	if len(rendered[0].ContentBlocks) != 0 {
		t.Fatalf("history content blocks = %#v, want collapsed text only", rendered[0].ContentBlocks)
	}
	if strings.Contains(rendered[0].Content, "iVBOR") || strings.Contains(rendered[0].Content, "med_abc") {
		t.Fatalf("history render leaked media data/id: %q", rendered[0].Content)
	}
}

func TestRenderMessagesForMemoryStripsMediaIdentifiersAndPreservesText(t *testing.T) {
	messages := []Message{{
		Role: RoleUser,
		ContentBlocks: []ContentBlock{
			{Type: string(PartText), Text: "记住这张图里的字不算，我只是问你这是什么花"},
			{Type: string(PartImage), Media: &MediaPart{
				MediaAssetID: "med_secret",
				Kind:         "image",
				MimeType:     "image/jpeg",
				StorageURI:   "C:/secret/photo.jpg",
				ProviderRef:  "file-abc",
				Data:         []byte("raw-image-bytes"),
			}},
		},
	}}

	rendered := RenderMessages(messages, RenderForMemory, RenderPolicy{})
	got := rendered[0].Content

	if !strings.Contains(got, "记住这张图里的字不算，我只是问你这是什么花") {
		t.Fatalf("memory render lost adjacent user text: %q", got)
	}
	if !strings.Contains(got, "[used image]") {
		t.Fatalf("memory render missing placeholder: %q", got)
	}
	for _, forbidden := range []string{"med_secret", "C:/secret/photo.jpg", "file-abc", "raw-image-bytes", "base64"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("memory render leaked %q in %q", forbidden, got)
		}
	}
	if len(rendered[0].ContentBlocks) != 0 {
		t.Fatalf("memory content blocks = %#v, want collapsed text only", rendered[0].ContentBlocks)
	}
}

func TestRenderMessagesForCurrentTurnKeepsOnlyCurrentImages(t *testing.T) {
	messages := []Message{
		{
			Role:   RoleUser,
			TurnID: "turn-old",
			ContentBlocks: []ContentBlock{
				{Type: string(PartText), Text: "old"},
				{Type: string(PartImage), Media: &MediaPart{MediaAssetID: "med_old", Kind: "image", MimeType: "image/png"}},
			},
		},
		{
			Role:   RoleUser,
			TurnID: "turn-current",
			ContentBlocks: []ContentBlock{
				{Type: string(PartText), Text: "now"},
				{Type: string(PartImage), Media: &MediaPart{MediaAssetID: "med_now", Kind: "image", MimeType: "image/png"}},
			},
		},
	}

	rendered := RenderMessages(messages, RenderForCurrentLLMTurn, RenderPolicy{CurrentTurnID: "turn-current"})

	if rendered[0].Content != "old\n[used image]" || len(rendered[0].ContentBlocks) != 0 {
		t.Fatalf("old message = %#v, want placeholder only", rendered[0])
	}
	if len(rendered[1].ContentBlocks) != 2 {
		t.Fatalf("current blocks = %d, want text+image", len(rendered[1].ContentBlocks))
	}
	if rendered[1].ContentBlocks[1].Media == nil || rendered[1].ContentBlocks[1].Media.MediaAssetID != "med_now" {
		t.Fatalf("current media block = %#v, want med_now kept", rendered[1].ContentBlocks[1])
	}
}
