package toolapproval

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/tool"
)

func BuildToolApprovalPacket(taskID, goalSummary string, classification tool.CallClassification) protocol.DecisionPacket {
	call := classification.Call
	kind := classification.ApprovalKind
	if kind == "" {
		kind = tool.ApprovalKindDestructiveWrite
	}
	var packetBinding *protocol.ToolApprovalBinding
	if binding, err := tool.BuildApprovalBinding(call, "", kind); err == nil {
		packetBinding = &protocol.ToolApprovalBinding{
			ApprovalKind:        binding.ApprovalKind,
			ToolName:            binding.ToolName,
			NormalizedInputHash: binding.NormalizedInputHash,
			PathDigest:          binding.PathDigest,
			InputPreview:        binding.InputPreview,
			ChangeSetID:         binding.ChangeSetID,
			PlanHash:            binding.PlanHash,
			ResourceID:          binding.ResourceID,
			CanonicalPathHash:   binding.CanonicalPathHash,
			BaselineHash:        binding.BaselineHash,
			BaselineFileID:      binding.BaselineFileID,
			DeleteMode:          binding.DeleteMode,
		}
	}
	return protocol.DecisionPacket{
		TaskID:               strings.TrimSpace(taskID),
		Category:             protocol.CatToolApproval,
		GoalSummary:          firstNonEmpty(goalSummary, "Emotion direct tool call requested by the model"),
		Question:             ToolApprovalQuestion(classification),
		WhyBlocked:           ToolApprovalWhyBlocked(classification),
		Options:              []protocol.DecisionOption{{ID: "allow", Summary: "允许执行"}, {ID: "deny", Summary: "拒绝"}},
		RejectOptionID:       "deny",
		RecommendedOption:    "allow",
		RecommendationReason: ToolApprovalRecommendation(goalSummary),
		SuggestsUserInput:    false,
		ToolApprovalBinding:  packetBinding,
		CreatedAt:            time.Now().UTC(),
	}
}

func ToolApprovalQuestion(classification tool.CallClassification) string {
	call := classification.Call
	if classification.ApprovalKind == tool.ApprovalKindSensitiveRead {
		return sensitiveReadApprovalQuestion(call)
	}
	if classification.ApprovalKind == tool.ApprovalKindPluginInvocation {
		return pluginInvocationApprovalQuestion(call)
	}
	switch strings.TrimSpace(call.Name) {
	case "bash":
		command := bashCommandPreview(call.Input)
		if command == "" {
			command = "<empty>"
		}
		return strings.Join([]string{
			"我准备执行一个受限命令，尚未执行。",
			"",
			"操作：执行 bash 命令",
			"命令：" + command,
			"风险：命令可能删除、覆盖、移动或重置文件。",
			"",
			"确认执行请点击“允许执行”；取消请点击“拒绝”。",
		}, "\n")
	case "write_file":
		target := toolApprovalPathTarget(call)
		return strings.Join([]string{
			"我准备执行一个受限文件操作，尚未执行。",
			"",
			"操作：写入文件",
			"目标：" + target,
			"风险：这可能覆盖已有文件或触碰敏感路径。",
			"影响：文件内容将被替换为本次工具输入中的新内容。",
			"",
			"确认执行请点击“允许执行”；取消请点击“拒绝”。",
		}, "\n")
	case "edit_file":
		target := toolApprovalPathTarget(call)
		risk := "这会修改敏感路径或大范围文件内容。"
		impact := "匹配的 old_string 会被替换为 new_string。"
		if editFileReplaceAll(call.Input) {
			risk = "replace_all=true，可能同时修改多个位置。"
			impact = "所有匹配的 old_string 都会被替换。"
		}
		return strings.Join([]string{
			"我准备执行一个受限文件编辑，尚未执行。",
			"",
			"操作：编辑文件",
			"目标：" + target,
			"风险：" + risk,
			"影响：" + impact,
			"",
			"确认执行请点击“允许执行”；取消请点击“拒绝”。",
		}, "\n")
	default:
		return strings.Join([]string{
			"我准备执行一个受限操作，尚未执行。",
			"",
			"操作：" + toolApprovalOperation(call),
			"目标：" + toolApprovalCallPreview(call),
			"风险：该工具调用需要显式批准。",
			"影响：批准后才会执行这一次工具输入对应的操作。",
			"",
			"确认执行请点击“允许执行”；取消请点击“拒绝”。",
		}, "\n")
	}
}

func ToolApprovalWhyBlocked(classification tool.CallClassification) string {
	if classification.ApprovalKind == tool.ApprovalKindSensitiveRead {
		if reason := strings.TrimSpace(classification.ApprovalReason); reason != "" {
			return fmt.Sprintf(`Tool %q requires explicit sensitive-read approval before execution: %s.`, classification.Call.Name, reason)
		}
		return fmt.Sprintf(`Tool %q requires explicit sensitive-read approval before execution.`, classification.Call.Name)
	}
	if classification.ApprovalKind == tool.ApprovalKindPluginInvocation {
		return fmt.Sprintf(`Tool %q requires explicit third-party plugin invocation approval before execution.`, classification.Call.Name)
	}
	if reason := strings.TrimSpace(classification.ApprovalReason); reason != "" {
		return reason
	}
	return fmt.Sprintf(`Tool %q requires explicit human approval before execution.`, classification.Call.Name)
}

func ToolApprovalRecommendation(goalSummary string) string {
	goal := strings.TrimSpace(goalSummary)
	if goal == "" {
		return "该工具尚未执行；批准后只会执行当前输入绑定的一次调用。"
	}
	return fmt.Sprintf("若要继续“%s”，可允许执行；批准后只会执行当前输入绑定的一次调用。", goal)
}

func pluginInvocationApprovalQuestion(call tool.Call) string {
	pluginID, toolName := pluginToolIdentity(call.Name)
	if pluginID == "" {
		pluginID = "unknown"
	}
	if toolName == "" {
		toolName = strings.TrimSpace(call.Name)
	}
	return strings.Join([]string{
		fmt.Sprintf("即将调用第三方 Python 插件 `%s` 的工具 `%s`。该插件作为当前用户身份下的本地代码运行。", pluginID, toolName),
		"",
		"该工具尚未执行。",
		"影响：批准后才会执行这一次工具输入对应的操作。",
		"",
		"确认执行请点击“允许执行”；取消请点击“拒绝”。",
	}, "\n")
}

func pluginToolIdentity(name string) (string, string) {
	trimmed := strings.TrimSpace(name)
	if strings.HasPrefix(trimmed, "plugin.") {
		rest := strings.TrimPrefix(trimmed, "plugin.")
		idx := strings.LastIndex(rest, ".")
		if idx <= 0 || idx == len(rest)-1 {
			return rest, ""
		}
		return strings.ReplaceAll(rest[:idx], "_", "."), rest[idx+1:]
	}
	if strings.HasPrefix(trimmed, "plugin_") {
		rest := strings.TrimPrefix(trimmed, "plugin_")
		idx := strings.LastIndex(rest, "_")
		if idx <= 0 || idx == len(rest)-1 {
			return rest, ""
		}
		return strings.ReplaceAll(rest[:idx], "_", "."), rest[idx+1:]
	}
	return trimmed, ""
}

func sensitiveReadApprovalQuestion(call tool.Call) string {
	operation := "读取文件"
	if strings.TrimSpace(call.Name) == "list_dir" {
		operation = "列出目录"
	}
	return strings.Join([]string{
		"我准备执行一次敏感读取，尚未执行。",
		"",
		"操作：" + operation,
		"目标：" + toolApprovalPathTarget(call),
		"原因：目标位于敏感路径或可能包含凭据、密钥、令牌、账号配置等信息。",
		"影响：确认后，我会把该文件/目录内容作为本次任务证据读取；不会修改任何文件。",
		"",
		"确认读取请点击“允许执行”；取消请点击“拒绝”。",
	}, "\n")
}

func toolApprovalOperation(call tool.Call) string {
	if strings.EqualFold(strings.TrimSpace(call.Name), "bash") {
		return "执行 bash 命令"
	}
	if name := strings.TrimSpace(call.Name); name != "" {
		return "执行工具 " + name
	}
	return "执行受限操作"
}

func toolApprovalCallPreview(call tool.Call) string {
	if preview := strings.TrimSpace(tool.InputPreviewForCall(call)); preview != "" {
		return preview
	}
	if name := strings.TrimSpace(call.Name); name != "" {
		return name
	}
	return "<unknown>"
}

func toolApprovalPathTarget(call tool.Call) string {
	var payload struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(call.Input, &payload); err == nil && strings.TrimSpace(payload.Path) != "" {
		return strings.TrimSpace(payload.Path)
	}
	return toolApprovalCallPreview(call)
}

func editFileReplaceAll(input json.RawMessage) bool {
	var payload struct {
		ReplaceAll bool `json:"replace_all"`
	}
	_ = json.Unmarshal(input, &payload)
	return payload.ReplaceAll
}

func bashCommandPreview(input json.RawMessage) string {
	var payload struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Command)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
