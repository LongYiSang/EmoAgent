package context

import (
	"fmt"
	"time"
)

func formatCurrentTimeContext(now time.Time) string {
	_, offset := now.Zone()
	return fmt.Sprintf(
		"当前时间上下文：%d年%d月%d日 %s %02d:%02d（%s，%s）。",
		now.Year(),
		int(now.Month()),
		now.Day(),
		formatChineseWeekday(now.Weekday()),
		now.Hour(),
		now.Minute(),
		formatTimeZoneName(now),
		formatUTCOffset(offset),
	)
}

func formatTimeZoneName(now time.Time) string {
	if loc := now.Location(); loc != nil {
		name := loc.String()
		if name != "" && name != "Local" {
			return name
		}
	}
	zone, _ := now.Zone()
	if zone != "" {
		return zone
	}
	return "本地时区"
}

func formatUTCOffset(offsetSeconds int) string {
	sign := "+"
	if offsetSeconds < 0 {
		sign = "-"
		offsetSeconds = -offsetSeconds
	}
	hours := offsetSeconds / 3600
	minutes := (offsetSeconds % 3600) / 60
	return fmt.Sprintf("UTC%s%02d:%02d", sign, hours, minutes)
}

func formatChineseWeekday(day time.Weekday) string {
	switch day {
	case time.Sunday:
		return "星期日"
	case time.Monday:
		return "星期一"
	case time.Tuesday:
		return "星期二"
	case time.Wednesday:
		return "星期三"
	case time.Thursday:
		return "星期四"
	case time.Friday:
		return "星期五"
	case time.Saturday:
		return "星期六"
	default:
		return ""
	}
}
