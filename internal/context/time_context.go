package context

import (
	"fmt"
	"time"
)

func formatCurrentTimeContext(now time.Time) string {
	return formatTimeContext("当前时间上下文", now)
}

func formatPreviousUserMessageRelative(now, previous time.Time) string {
	delta := now.Sub(previous)
	if delta < 0 {
		delta = 0
	}
	fullTime := formatFullLocalTime(previous)
	if delta < 90*time.Second {
		return fmt.Sprintf("上一条用户消息：刚刚（%s）。", fullTime)
	}
	return fmt.Sprintf("上一条用户消息：约%s之前（%s）。", humanizeDurationZH(delta), fullTime)
}

func humanizeDurationZH(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < 90*time.Second {
		return "刚刚"
	}
	if d < time.Hour {
		minutes := int(d / time.Minute)
		if minutes < 1 {
			minutes = 1
		}
		return fmt.Sprintf("%d分钟", minutes)
	}
	if d < 48*time.Hour {
		hours := int(d / time.Hour)
		minutes := int((d % time.Hour) / time.Minute)
		if minutes == 0 {
			return fmt.Sprintf("%d小时", hours)
		}
		return fmt.Sprintf("%d小时%d分钟", hours, minutes)
	}
	days := int(d / (24 * time.Hour))
	hours := int((d % (24 * time.Hour)) / time.Hour)
	if hours == 0 {
		return fmt.Sprintf("%d天", days)
	}
	return fmt.Sprintf("%d天%d小时", days, hours)
}

func formatFullLocalTime(now time.Time) string {
	return fmt.Sprintf(
		"%d年%d月%d日 %s %02d:%02d",
		now.Year(),
		int(now.Month()),
		now.Day(),
		formatChineseWeekday(now.Weekday()),
		now.Hour(),
		now.Minute(),
	)
}

func formatTimeContext(label string, now time.Time) string {
	_, offset := now.Zone()
	return fmt.Sprintf(
		"%s：%d年%d月%d日 %s %02d:%02d（%s，%s）。",
		label,
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
