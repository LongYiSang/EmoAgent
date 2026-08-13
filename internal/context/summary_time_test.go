package context

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/storage"
)

func summaryDeltaFixture() []storage.MessageRecord {
	return []storage.MessageRecord{
		{ID: "s1", Role: "user", Content: "切了个西瓜，爽爽吃", CreatedAt: "2026-07-06T22:22:19.9577838+08:00"},
		{ID: "s2", Role: "assistant", Content: "是什么西瓜呀", CreatedAt: "2026-07-06T22:22:42.3808207+08:00"},
	}
}

// Without created_at the summariser can only describe events relatively ("刚切了
// 西瓜"), and that wording then reads as current for as long as the fact lives.
func TestBuildSummaryRequestCarriesMessageTimestamps(t *testing.T) {
	persona := &config.Persona{Name: "default", SystemPrompt: "You are warm."}
	loc := time.FixedZone("Asia/Shanghai", 8*60*60)
	now := time.Date(2026, time.August, 11, 23, 8, 0, 0, loc)

	req, err := buildSummaryRequestWithParamsAndSystemPrompt(
		"summary-model", llm.RequestParams{}, persona, RunningSummary{}, summaryDeltaFixture(), summarySystemPrompt, now)
	if err != nil {
		t.Fatalf("buildSummaryRequestWithParamsAndSystemPrompt: %v", err)
	}

	var payload struct {
		Messages []struct {
			ID        string `json:"id"`
			CreatedAt string `json:"created_at"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(req.Messages[1].Content), &payload); err != nil {
		t.Fatalf("unmarshal history payload: %v", err)
	}
	if len(payload.Messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(payload.Messages))
	}
	for _, msg := range payload.Messages {
		if msg.CreatedAt == "" {
			t.Fatalf("message %q reached the summariser without created_at", msg.ID)
		}
	}
	if want := "2026-07-06T22:22:19.9577838+08:00"; payload.Messages[0].CreatedAt != want {
		t.Fatalf("messages[0].created_at = %q, want %q", payload.Messages[0].CreatedAt, want)
	}
}

// The summariser prompt carried no clock at all, so it had nothing to resolve
// "刚" against even when it wanted to.
func TestBuildSummaryRequestInjectsCurrentTime(t *testing.T) {
	persona := &config.Persona{Name: "default", SystemPrompt: "You are warm."}
	loc := time.FixedZone("Asia/Shanghai", 8*60*60)
	now := time.Date(2026, time.August, 11, 23, 8, 0, 0, loc)

	req, err := buildSummaryRequestWithParamsAndSystemPrompt(
		"summary-model", llm.RequestParams{}, persona, RunningSummary{}, summaryDeltaFixture(), summarySystemPrompt, now)
	if err != nil {
		t.Fatalf("buildSummaryRequestWithParamsAndSystemPrompt: %v", err)
	}

	if want := "当前时间上下文：2026年8月11日 星期二 23:08"; !strings.Contains(req.System, want) {
		t.Fatalf("System = %q, want it to contain %q", req.System, want)
	}
	if !strings.Contains(req.System, summarySystemPrompt) {
		t.Fatalf("System = %q, want the resolved component text preserved", req.System)
	}
}

// Message timestamps are written in the configured timezone, which need not be
// the host's. If the injected clock used a different offset the summariser would
// be dating facts against a different zone than the messages it reads.
func TestBuildSummaryRequestClockMatchesMessageTimezone(t *testing.T) {
	persona := &config.Persona{Name: "default", SystemPrompt: "You are warm."}
	// Same instant as 2026-08-11 23:08 +08:00, handed over in UTC.
	now := time.Date(2026, time.August, 11, 15, 8, 0, 0, time.UTC)

	req, err := buildSummaryRequestWithParamsAndSystemPrompt(
		"summary-model", llm.RequestParams{}, persona, RunningSummary{}, summaryDeltaFixture(), summarySystemPrompt, now)
	if err != nil {
		t.Fatalf("buildSummaryRequestWithParamsAndSystemPrompt: %v", err)
	}

	if want := "2026年8月11日 星期二 23:08"; !strings.Contains(req.System, want) {
		t.Fatalf("System = %q, want the clock rendered in the messages' +08:00 zone (%q)", req.System, want)
	}
}

// With nothing to anchor to, the host's own zone is the only sensible answer.
func TestBuildSummaryRequestFallsBackToLocalZone(t *testing.T) {
	persona := &config.Persona{Name: "default", SystemPrompt: "You are warm."}
	now := time.Date(2026, time.August, 11, 23, 8, 0, 0, time.Local)
	delta := []storage.MessageRecord{{ID: "s1", Role: "user", Content: "hi", CreatedAt: "not-a-timestamp"}}

	req, err := buildSummaryRequestWithParamsAndSystemPrompt(
		"summary-model", llm.RequestParams{}, persona, RunningSummary{}, delta, summarySystemPrompt, now)
	if err != nil {
		t.Fatalf("buildSummaryRequestWithParamsAndSystemPrompt: %v", err)
	}

	if want := "2026年8月11日 星期二 23:08"; !strings.Contains(req.System, want) {
		t.Fatalf("System = %q, want the clock rendered in the local zone (%q)", req.System, want)
	}
}
