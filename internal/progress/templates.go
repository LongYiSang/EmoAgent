package progress

import (
	"math/rand"
	"strings"
)

// DefaultTemplates defines built-in short progress phrases.
var DefaultTemplates = map[string][]string{
	"_start":     {"让我来看看...", "开始处理了..."},
	"_heartbeat": {"还在忙...", "正在处理中...", "稍等一下..."},
	"_finishing": {"快好了...", "整理一下结果..."},
	"_paused":    {"让我想想...", "思考一下..."},
	"read_file":  {"我看看这个文件", "打开文件看看..."},
	"list_dir":   {"看看目录里有什么"},
	"write_file": {"写入文件中..."},
	"edit_file":  {"修改文件中..."},
	"bash":       {"跑个命令看看", "执行一下..."},
	"web_search": {"搜索一下...", "查查看..."},
	"web_fetch":  {"看看网页内容..."},
	"_default":   {"处理中..."},
}

// Resolve maps progress event + persona overrides to one display phrase.
func Resolve(event Event, personaOverrides map[string][]string) string {
	key := eventKey(event)

	candidates := phraseList(personaOverrides[key])
	if len(candidates) == 0 {
		candidates = phraseList(DefaultTemplates[key])
	}
	if len(candidates) == 0 {
		candidates = phraseList(personaOverrides["_default"])
	}
	if len(candidates) == 0 {
		candidates = phraseList(DefaultTemplates["_default"])
	}
	if len(candidates) == 0 {
		return ""
	}
	return candidates[rand.Intn(len(candidates))]
}

func eventKey(event Event) string {
	switch event.Kind {
	case KindStart:
		return "_start"
	case KindHeartbeat:
		return "_heartbeat"
	case KindFinishing:
		return "_finishing"
	case KindPaused:
		return "_paused"
	case KindTool:
		toolName := strings.TrimSpace(strings.ToLower(event.ToolName))
		if toolName != "" {
			return toolName
		}
	}
	return "_default"
}

func phraseList(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	out := make([]string, 0, len(input))
	for _, phrase := range input {
		trimmed := strings.TrimSpace(phrase)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
