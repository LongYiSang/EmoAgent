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

func TestFormatTimeContextLabel(t *testing.T) {
	loc := time.FixedZone("Asia/Shanghai", 8*60*60)
	now := time.Date(2026, time.April, 25, 21, 10, 30, 0, loc)

	got := formatTimeContext("上一条用户消息时间", now)
	want := "上一条用户消息时间：2026年4月25日 星期六 21:10（Asia/Shanghai，UTC+08:00）。"
	if got != want {
		t.Fatalf("formatTimeContext() = %q, want %q", got, want)
	}
}

func TestFormatPreviousUserMessageRelativeMinutes(t *testing.T) {
	loc := time.FixedZone("Asia/Shanghai", 8*60*60)
	now := time.Date(2026, time.June, 28, 23, 7, 0, 0, loc)
	previous := time.Date(2026, time.June, 28, 22, 42, 0, 0, loc)

	got := formatPreviousUserMessageRelative(now, previous)
	want := "上一条用户消息：约25分钟之前（2026年6月28日 星期日 22:42）。"
	if got != want {
		t.Fatalf("formatPreviousUserMessageRelative() = %q, want %q", got, want)
	}
}

func TestFormatPreviousUserMessageRelativeDaysHours(t *testing.T) {
	loc := time.FixedZone("Asia/Shanghai", 8*60*60)
	now := time.Date(2026, time.June, 28, 23, 6, 0, 0, loc)
	previous := time.Date(2026, time.June, 25, 18, 51, 0, 0, loc)

	got := formatPreviousUserMessageRelative(now, previous)
	want := "上一条用户消息：约3天4小时之前（2026年6月25日 星期四 18:51）。"
	if got != want {
		t.Fatalf("formatPreviousUserMessageRelative() = %q, want %q", got, want)
	}
}

func TestFormatPreviousUserMessageRelativeJustNow(t *testing.T) {
	loc := time.FixedZone("Asia/Shanghai", 8*60*60)
	now := time.Date(2026, time.June, 28, 23, 6, 40, 0, loc)
	previous := time.Date(2026, time.June, 28, 23, 6, 0, 0, loc)

	got := formatPreviousUserMessageRelative(now, previous)
	want := "上一条用户消息：刚刚（2026年6月28日 星期日 23:06）。"
	if got != want {
		t.Fatalf("formatPreviousUserMessageRelative() = %q, want %q", got, want)
	}
}
