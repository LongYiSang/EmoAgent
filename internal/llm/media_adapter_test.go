package llm

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestOpenAIToMessagesMapsImagePartsToImageURL(t *testing.T) {
	client := &openaiClient{logger: slog.Default()}
	req := ChatRequest{Messages: []Message{{
		Role: RoleUser,
		ContentBlocks: []ContentBlock{
			{Type: string(PartText), Text: "帮我看看这张图"},
			{Type: string(PartImage), Media: &MediaPart{Kind: "image", MimeType: "image/jpeg", Detail: "auto", Data: []byte{1, 2, 3}}},
		},
	}}}

	body, err := json.Marshal(client.openaiPayload(req, false))
	if err != nil {
		t.Fatalf("Marshal payload: %v", err)
	}
	payload := string(body)

	if !strings.Contains(payload, `"type":"image_url"`) {
		t.Fatalf("payload missing OpenAI image_url part: %s", payload)
	}
	if !strings.Contains(payload, `"url":"data:image/jpeg;base64,AQID"`) {
		t.Fatalf("payload missing data URL image bytes: %s", payload)
	}
	if !strings.Contains(payload, `"text":"帮我看看这张图"`) {
		t.Fatalf("payload missing text part: %s", payload)
	}
}

func TestKimiOpenAICompatibleUsesDataURLForImageParts(t *testing.T) {
	client := &openaiClient{providerID: "moonshot", logger: slog.Default()}
	req := ChatRequest{Messages: []Message{{
		Role: RoleUser,
		ContentBlocks: []ContentBlock{
			{Type: string(PartText), Text: "看图"},
			{Type: string(PartImage), Media: &MediaPart{Kind: "image", MimeType: "image/png", Data: []byte{4, 5, 6}}},
		},
	}}}

	body, err := json.Marshal(client.openaiPayload(req, false))
	if err != nil {
		t.Fatalf("Marshal payload: %v", err)
	}
	payload := string(body)

	if !strings.Contains(payload, `"url":"data:image/png;base64,BAUG"`) {
		t.Fatalf("Kimi-compatible payload should use data URL, got: %s", payload)
	}
	if strings.Contains(payload, `"http://`) || strings.Contains(payload, `"https://`) {
		t.Fatalf("Kimi-compatible payload should not use ordinary remote URLs: %s", payload)
	}
}

func TestAnthropicToMessagesMapsImagePartsToBase64Source(t *testing.T) {
	client := &anthropicClient{}
	req := ChatRequest{Messages: []Message{{
		Role: RoleUser,
		ContentBlocks: []ContentBlock{
			{Type: string(PartImage), Media: &MediaPart{Kind: "image", MimeType: "image/png", Data: []byte{7, 8, 9}}},
			{Type: string(PartText), Text: "请描述图片内容"},
		},
	}}}

	body, err := json.Marshal(client.anthropicPayload(req, false))
	if err != nil {
		t.Fatalf("Marshal payload: %v", err)
	}
	payload := string(body)

	if !strings.Contains(payload, `"type":"image"`) {
		t.Fatalf("payload missing Anthropic image block: %s", payload)
	}
	if !strings.Contains(payload, `"source":{"type":"base64","media_type":"image/png","data":"BwgJ"}`) {
		t.Fatalf("payload missing Anthropic base64 source: %s", payload)
	}
	if !strings.Contains(payload, `"text":"请描述图片内容"`) {
		t.Fatalf("payload missing text block: %s", payload)
	}
}
