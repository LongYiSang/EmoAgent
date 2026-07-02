package replydelivery

import (
	"reflect"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	contextutil "github.com/longyisang/emoagent/internal/context"
)

func TestSplitTextNaturalChinesePunctuation(t *testing.T) {
	cfg := config.DefaultReplyDeliveryConfig().Segment

	got := SplitText(cfg, "嗯，我懂你的意思。这个不要在流式中硬切。放在投递层更稳。")
	want := []string{"嗯，我懂你的意思。", "这个不要在流式中硬切。", "放在投递层更稳。"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("segments = %#v, want %#v", got, want)
	}
}

func TestSplitTextNaturalEnglishPunctuationAndNewlines(t *testing.T) {
	cfg := config.DefaultReplyDeliveryConfig().Segment

	got := SplitText(cfg, "Sure! Let's keep it simple?\nThen ship it.")
	want := []string{"Sure!", "Let's keep it simple?", "Then ship it."}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("segments = %#v, want %#v", got, want)
	}
}

func TestSplitTextCleanupRegex(t *testing.T) {
	cfg := config.DefaultReplyDeliveryConfig().Segment
	cfg.CleanupRegex = `[!！]+$`

	got := SplitText(cfg, "好！继续！")
	want := []string{"好", "继续"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cleanup segments = %#v, want %#v", got, want)
	}
}

func TestSplitTextProtectsURLsCodeBlocksAndTables(t *testing.T) {
	cfg := config.DefaultReplyDeliveryConfig().Segment

	urlSegments := SplitText(cfg, "看这个链接：https://example.com/a?x=1。然后再说。")
	if len(urlSegments) != 2 || urlSegments[0] != "看这个链接：https://example.com/a?x=1。" {
		t.Fatalf("url segments = %#v, want URL kept in first segment", urlSegments)
	}

	code := "可以这样写：\n```go\nfmt.Println(\"你好。这里不应该被切开。\")\n```\n然后再补测试。"
	codeSegments := SplitText(cfg, code)
	if len(codeSegments) != 3 || codeSegments[1] != "```go\nfmt.Println(\"你好。这里不应该被切开。\")\n```" {
		t.Fatalf("code segments = %#v, want fenced block intact", codeSegments)
	}

	table := "表格如下：\n| A | B |\n|---|---|\n| 你好。 | 世界。 |\n结尾。"
	tableSegments := SplitText(cfg, table)
	if len(tableSegments) != 3 || tableSegments[1] != "| A | B |\n|---|---|\n| 你好。 | 世界。 |" {
		t.Fatalf("table segments = %#v, want markdown table intact", tableSegments)
	}
}

func TestSplitTextFallsBackWhenTooManySegments(t *testing.T) {
	cfg := config.DefaultReplyDeliveryConfig().Segment
	cfg.MaxSegments = 2

	text := "一。二。三。"
	got := SplitText(cfg, text)
	if !reflect.DeepEqual(got, []string{text}) {
		t.Fatalf("segments = %#v, want original text when over cap", got)
	}
}

func TestBuildPlanSuppressesLongText(t *testing.T) {
	cfg := config.DefaultReplyDeliveryConfig()
	cfg.Enabled = true
	cfg.Segment.LongTextThreshold = 4

	plan := BuildPlan(cfg, string(contextutil.PromptModeCasualChat), false, "超过四个字。")
	if !plan.Suppressed || plan.SuppressReason != "long_text" || len(plan.Segments) != 1 {
		t.Fatalf("long text plan = %#v, want long_text suppression", plan)
	}
}

func TestSplitTextRegexModeAndInvalidRegexFallback(t *testing.T) {
	cfg := config.DefaultReplyDeliveryConfig().Segment
	cfg.SplitMode = "regex"
	cfg.Regex = `[^,]+,?`

	got := SplitText(cfg, "one,two,three")
	want := []string{"one,", "two,", "three"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("regex segments = %#v, want %#v", got, want)
	}

	cfg.Regex = "["
	got = SplitText(cfg, "A。B。")
	want = []string{"A。", "B。"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("invalid regex fallback = %#v, want %#v", got, want)
	}
}

func TestBuildPlanModeGateAndMetadata(t *testing.T) {
	cfg := config.DefaultReplyDeliveryConfig()
	cfg.Enabled = true
	text := "第一句。第二句。"

	plan := BuildPlan(cfg, string(contextutil.PromptModeCasualChat), false, text)
	if plan.Suppressed || plan.SegmentCount != 2 {
		t.Fatalf("casual plan = %#v, want two unsuppressed segments", plan)
	}
	metadata := plan.Metadata()
	if metadata.SchemaVersion != SchemaVersion || metadata.SegmentCount != 2 || len(metadata.Segments) != 2 {
		t.Fatalf("metadata = %#v, want v0.1 two segments", metadata)
	}

	plan = BuildPlan(cfg, string(contextutil.PromptModeWorkMode), false, text)
	if !plan.Suppressed || plan.SuppressReason != "work_mode" || len(plan.Segments) != 1 {
		t.Fatalf("work mode plan = %#v, want suppressed original reply", plan)
	}

	plan = BuildPlan(cfg, string(contextutil.PromptModeCasualChat), true, text)
	if !plan.Suppressed || plan.SuppressReason != "realtime_streaming" {
		t.Fatalf("realtime plan = %#v, want realtime suppression", plan)
	}
}

func TestWordCountAstrBotCompatible(t *testing.T) {
	if got := WordCount("hello world"); got != 2 {
		t.Fatalf("ASCII word count = %d, want 2", got)
	}
	if got := WordCount("你好，world 123！"); got != 10 {
		t.Fatalf("mixed word count = %d, want 10", got)
	}
}
