package context

import (
	"testing"
	"time"
)

func TestFormatCurrentTimeContext(t *testing.T) {
	loc := time.FixedZone("Asia/Shanghai", 8*60*60)
	now := time.Date(2026, time.April, 26, 17, 5, 30, 0, loc)

	got := formatCurrentTimeContext(now)
	want := "当前时间上下文：2026年4月26日 星期日 17:05（Asia/Shanghai，UTC+08:00）。"
	if got != want {
		t.Fatalf("formatCurrentTimeContext() = %q, want %q", got, want)
	}
}
