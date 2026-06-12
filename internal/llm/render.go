package llm

import "strings"

const UsedImagePlaceholder = "[used image]"

type RenderMode string

const (
	RenderForCurrentLLMTurn RenderMode = "current_llm_turn"
	RenderForHistory        RenderMode = "history"
	RenderForMemory         RenderMode = "memory"
	RenderForSummary        RenderMode = "summary"
)

type RenderPolicy struct {
	CurrentTurnID       string
	ReactivatedMediaIDs map[string]bool
	Placeholder         string
}

func RenderMessages(messages []Message, mode RenderMode, policy RenderPolicy) []Message {
	rendered := make([]Message, len(messages))
	for i, msg := range messages {
		rendered[i] = RenderMessage(msg, mode, policy)
	}
	return rendered
}

func RenderMessage(msg Message, mode RenderMode, policy RenderPolicy) Message {
	placeholder := strings.TrimSpace(policy.Placeholder)
	if placeholder == "" {
		placeholder = UsedImagePlaceholder
	}
	if len(msg.ContentBlocks) == 0 {
		return msg
	}

	if mode == RenderForCurrentLLMTurn && shouldKeepMediaForCurrentTurn(msg, policy) {
		cp := msg
		cp.ContentBlocks = cloneBlocksWithoutPrivateRefs(msg.ContentBlocks)
		cp.Content = renderBlocksText(msg.ContentBlocks, placeholder)
		return cp
	}

	cp := msg
	cp.Content = renderBlocksText(msg.ContentBlocks, placeholder)
	cp.ContentBlocks = nil
	return cp
}

func shouldKeepMediaForCurrentTurn(msg Message, policy RenderPolicy) bool {
	if strings.TrimSpace(policy.CurrentTurnID) != "" && msg.TurnID == policy.CurrentTurnID {
		return true
	}
	if len(policy.ReactivatedMediaIDs) == 0 {
		return false
	}
	for _, block := range msg.ContentBlocks {
		if block.Media != nil && policy.ReactivatedMediaIDs[block.Media.MediaAssetID] {
			return true
		}
	}
	return false
}

func renderBlocksText(blocks []ContentBlock, placeholder string) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		switch ContentPartType(block.Type) {
		case PartText:
			if strings.TrimSpace(block.Text) != "" {
				parts = append(parts, block.Text)
			}
		case PartImage, PartAudio, PartVideo, PartFile:
			parts = append(parts, placeholder)
		case PartToolResult:
			if strings.TrimSpace(block.Content) != "" {
				parts = append(parts, block.Content)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func cloneBlocksWithoutPrivateRefs(blocks []ContentBlock) []ContentBlock {
	cloned := make([]ContentBlock, len(blocks))
	for i, block := range blocks {
		cloned[i] = block
		if block.Media != nil {
			media := *block.Media
			media.StorageURI = ""
			media.ProviderRef = ""
			cloned[i].Media = &media
		}
	}
	return cloned
}
