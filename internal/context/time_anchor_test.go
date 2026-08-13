package context

import (
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/promptcenter"
	"github.com/longyisang/emoagent/internal/runtimeenv"
	"github.com/longyisang/emoagent/internal/storage"
)

func TestFormatMessageTimePrefix(t *testing.T) {
	loc := time.FixedZone("Asia/Shanghai", 8*60*60)
	at := time.Date(2026, time.August, 11, 22, 18, 47, 0, loc)

	got := formatMessageTimePrefix(at)
	want := "[2026-08-11 22:18 周二]"
	if got != want {
		t.Fatalf("formatMessageTimePrefix() = %q, want %q", got, want)
	}
}

func TestFormatTimeGapNote(t *testing.T) {
	got := formatTimeGapNote(42*time.Hour - 13*time.Minute)
	want := "（距上一条 41小时47分钟）"
	if got != want {
		t.Fatalf("formatTimeGapNote() = %q, want %q", got, want)
	}
}

// The timeline below is the one from the 2026-08-11 /bad sample: the assistant
// said "都快四点半了" at 04:31 and, 42 hours later, drifted to "都快五点了".
func incidentHistory() []storage.MessageRecord {
	return []storage.MessageRecord{
		{ID: "m1", Role: "user", Content: "晚上好呀", CreatedAt: "2026-08-10T04:31:39.6802854+08:00"},
		{ID: "m2", Role: "assistant", Content: "都快四点半了", CreatedAt: "2026-08-10T04:31:56.7002961+08:00"},
		{ID: "m3", Role: "user", Content: "刚刚看了个视频", CreatedAt: "2026-08-11T22:18:47.0221995+08:00"},
		{ID: "m4", Role: "assistant", Content: "我好像看不了这个视频", CreatedAt: "2026-08-11T22:19:06.8863772+08:00"},
		{ID: "m5", Role: "user", Content: "抱歉抱歉~", CreatedAt: "2026-08-11T22:27:35.0411289+08:00"},
	}
}

func TestBuildTimeAnchorsMarksLargeGap(t *testing.T) {
	loc := time.FixedZone("Asia/Shanghai", 8*60*60)

	anchors := buildTimeAnchors(incidentHistory(), loc)

	// m3 follows a ~41h47m gap and must carry the gap note.
	got := anchors["m3"]
	if !strings.Contains(got, "41小时") {
		t.Fatalf("anchors[m3] = %q, want it to mention the ~41 hour gap", got)
	}
	if !strings.Contains(got, "[2026-08-11 22:18 周二]") {
		t.Fatalf("anchors[m3] = %q, want it to carry the absolute timestamp", got)
	}
}

func TestBuildTimeAnchorsSkipsSmallGap(t *testing.T) {
	loc := time.FixedZone("Asia/Shanghai", 8*60*60)

	anchors := buildTimeAnchors(incidentHistory(), loc)

	// m5 is only ~8 minutes after m4: timestamped, but no gap note.
	got := anchors["m5"]
	if got != "[2026-08-11 22:27 周二]" {
		t.Fatalf("anchors[m5] = %q, want only the timestamp with no gap note", got)
	}
}

func TestBuildTimeAnchorsLeavesAssistantWithoutGapUnannotated(t *testing.T) {
	loc := time.FixedZone("Asia/Shanghai", 8*60*60)

	anchors := buildTimeAnchors(incidentHistory(), loc)

	// m2 is an assistant reply 17 seconds after m1: nothing to say about it.
	if got, ok := anchors["m2"]; ok {
		t.Fatalf("anchors[m2] = %q, want no anchor for a same-minute assistant reply", got)
	}
}

func TestBuildTimeAnchorsAnnotatesAssistantAfterLargeGap(t *testing.T) {
	loc := time.FixedZone("Asia/Shanghai", 8*60*60)
	history := []storage.MessageRecord{
		{ID: "a1", Role: "user", Content: "在吗", CreatedAt: "2026-08-10T04:31:39+08:00"},
		{ID: "a2", Role: "assistant", Content: "都快四点半了", CreatedAt: "2026-08-12T09:00:00+08:00"},
	}

	anchors := buildTimeAnchors(history, loc)

	got := anchors["a2"]
	if !strings.Contains(got, "距上一条") {
		t.Fatalf("anchors[a2] = %q, want a gap note on the assistant message", got)
	}
	if strings.Contains(got, "[2026-08-12") {
		t.Fatalf("anchors[a2] = %q, want no absolute timestamp on an assistant message", got)
	}
}

func TestBuildTimeAnchorsIgnoresUnparsableTimestamps(t *testing.T) {
	loc := time.FixedZone("Asia/Shanghai", 8*60*60)
	history := []storage.MessageRecord{
		{ID: "b1", Role: "user", Content: "hi", CreatedAt: ""},
		{ID: "b2", Role: "user", Content: "there", CreatedAt: "not-a-timestamp"},
	}

	anchors := buildTimeAnchors(history, loc)

	if len(anchors) != 0 {
		t.Fatalf("buildTimeAnchors() = %v, want no anchors when timestamps are unusable", anchors)
	}
}

func TestApplyTimeAnchorsPrependsByMessageID(t *testing.T) {
	messages := []llm.Message{
		{ID: "m1", Role: llm.RoleUser, Content: "晚上好呀"},
		{ID: "m2", Role: llm.RoleAssistant, Content: "都快四点半了"},
	}

	got := ApplyTimeAnchors(messages, map[string]string{"m1": "[2026-08-10 04:31 周一]"})

	if want := "[2026-08-10 04:31 周一]\n晚上好呀"; got[0].Content != want {
		t.Fatalf("got[0].Content = %q, want %q", got[0].Content, want)
	}
	if got[1].Content != "都快四点半了" {
		t.Fatalf("got[1].Content = %q, want it untouched", got[1].Content)
	}
}

// The running_summary and tool_digest slots are synthesised without an ID and
// must never be annotated as if they were something the user said.
func TestApplyTimeAnchorsLeavesSyntheticSlotsAlone(t *testing.T) {
	messages := []llm.Message{
		{Role: llm.RoleUser, Content: `{"running_summary":{}}`},
		{ID: "m1", Role: llm.RoleUser, Content: "晚上好呀"},
	}

	got := ApplyTimeAnchors(messages, map[string]string{"": "[should not appear]", "m1": "[2026-08-10 04:31 周一]"})

	if got[0].Content != `{"running_summary":{}}` {
		t.Fatalf("got[0].Content = %q, want the synthetic slot untouched", got[0].Content)
	}
}

func TestApplyTimeAnchorsDoesNotMutateInput(t *testing.T) {
	messages := []llm.Message{{ID: "m1", Role: llm.RoleUser, Content: "晚上好呀"}}

	ApplyTimeAnchors(messages, map[string]string{"m1": "[2026-08-10 04:31 周一]"})

	if messages[0].Content != "晚上好呀" {
		t.Fatalf("input was mutated: %q", messages[0].Content)
	}
}

// Media-bearing messages are the ones a Content prefix written at assembly time
// would silently lose, so they must survive this path intact.
func TestApplyTimeAnchorsKeepsContentBlocks(t *testing.T) {
	messages := []llm.Message{{
		ID:            "m1",
		Role:          llm.RoleUser,
		Content:       "看看这张图",
		ContentBlocks: []llm.ContentBlock{{Type: "text"}, {Type: "image"}},
	}}

	got := ApplyTimeAnchors(messages, map[string]string{"m1": "[2026-08-10 04:31 周一]"})

	if len(got[0].ContentBlocks) != 2 {
		t.Fatalf("got %d content blocks, want 2", len(got[0].ContentBlocks))
	}
	if !strings.HasPrefix(got[0].Content, "[2026-08-10 04:31 周一]\n") {
		t.Fatalf("got[0].Content = %q, want the anchor prefix", got[0].Content)
	}
}

func incidentContextConfig(keepRecentUserTurns int) config.ContextConfig {
	return config.ContextConfig{
		InputBudgetTokens:    24000,
		SoftCompactRatio:     0.75,
		HardCompactRatio:     0.92,
		ReserveOutputTokens:  4096,
		KeepRecentUserTurns:  keepRecentUserTurns,
		ToolResultSoftTokens: 1000,
		ToolResultHardTokens: 3000,
	}
}

func TestBuildEmotionContextAnchorsTheStaleClockAssertion(t *testing.T) {
	persona := &config.Persona{Name: "default", SystemPrompt: "You are warm."}

	// Keeping 3 user turns pulls the whole incident in, so the stale
	// "都快四点半了" (m2) sits in context right before the 42-hour jump.
	assembled, err := BuildEmotionContext(persona, incidentHistory(), incidentContextConfig(3),
		runtimeenv.Facts{OS: "windows", Timezone: "Asia/Shanghai"})
	if err != nil {
		t.Fatalf("BuildEmotionContext: %v", err)
	}

	if got := assembled.TimeAnchors["m3"]; !strings.Contains(got, "41小时") {
		t.Fatalf("TimeAnchors[m3] = %q, want the gap note that defuses m2", got)
	}
	if got := assembled.TimeAnchors["m5"]; got != "[2026-08-11 22:27 周二]" {
		t.Fatalf("TimeAnchors[m5] = %q, want the plain timestamp", got)
	}
}

// Anchors describe what the model can actually see. A gap before the first kept
// message is invisible to it, so annotating it would assert a jump the model has
// no other evidence for.
func TestBuildEmotionContextSkipsAnchorsOutsideWindow(t *testing.T) {
	persona := &config.Persona{Name: "default", SystemPrompt: "You are warm."}

	assembled, err := BuildEmotionContext(persona, incidentHistory(), incidentContextConfig(1),
		runtimeenv.Facts{OS: "windows", Timezone: "Asia/Shanghai"})
	if err != nil {
		t.Fatalf("BuildEmotionContext: %v", err)
	}

	if got, ok := assembled.TimeAnchors["m3"]; ok {
		t.Fatalf("TimeAnchors[m3] = %q, want nothing for a message outside the window", got)
	}
	if got := assembled.TimeAnchors["m5"]; got != "[2026-08-11 22:27 周二]" {
		t.Fatalf("TimeAnchors[m5] = %q, want the kept message still anchored", got)
	}
}

func TestBuildTimeAnchorsRendersInRequestedLocation(t *testing.T) {
	loc := time.FixedZone("Asia/Shanghai", 8*60*60)
	history := []storage.MessageRecord{
		// Same instant as 2026-08-11 22:18 +08:00, expressed in UTC.
		{ID: "c1", Role: "user", Content: "hi", CreatedAt: "2026-08-11T14:18:47Z"},
	}

	anchors := buildTimeAnchors(history, loc)

	if got := anchors["c1"]; got != "[2026-08-11 22:18 周二]" {
		t.Fatalf("anchors[c1] = %q, want the timestamp converted to the target location", got)
	}
}

// The embedded catalog is what actually gets served; the constants in this
// package are only reached if it fails to load. They drifted apart once
// already, so pin them together rather than trusting a comment.
func TestEmbeddedPromptDefaultsMatchFallbackConstants(t *testing.T) {
	catalog, err := promptcenter.DefaultCatalog()
	if err != nil {
		t.Fatalf("DefaultCatalog: %v", err)
	}
	for componentID, fallback := range map[string]string{
		promptcenter.ComponentEmotionReplyPolicy:               emotionReplyPolicy,
		promptcenter.ComponentMemoryUsagePolicy:                memoryUsagePolicy,
		promptcenter.ComponentEmotionInternalContextDataPolicy: internalContextDataPolicy,
	} {
		component := catalog.MustGet(componentID)
		if strings.TrimSpace(component.DefaultText) != strings.TrimSpace(fallback) {
			t.Errorf("%s: embedded default and fallback constant differ\nembedded:\n%s\n\nconstant:\n%s",
				componentID, component.DefaultText, fallback)
		}
	}
}
