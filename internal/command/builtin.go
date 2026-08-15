package command

type BuiltinProvider struct{}

func NewBuiltinProvider() BuiltinProvider {
	return BuiltinProvider{}
}

func (BuiltinProvider) Descriptors() []CommandDescriptor {
	descriptors := make([]CommandDescriptor, 0, len(implementedBuiltinRootCommands))
	for _, name := range implementedBuiltinRootCommands {
		metadata := builtinMetadata[name]
		descriptor := CommandDescriptor{
			ID:           "builtin." + name,
			Name:         name,
			Summary:      metadata.summary,
			Usage:        metadata.usage,
			Reserved:     true,
			ProviderKind: CommandProviderBuiltin,
			Permission:   CommandPermissionMember,
			Scope:        CommandScopeOrigin,
			OutputMode:   CommandOutputDirect,
		}
		descriptors = append(descriptors, descriptor)
	}
	return descriptors
}

type builtinCommandMetadata struct {
	summary string
	usage   string
}

var builtinMetadata = map[string]builtinCommandMetadata{
	"help": {
		summary: "显示可用命令和用法",
		usage:   "/help [command]",
	},
	"sid": {
		summary: "显示当前来源、会话、persona 和 actor 诊断信息",
		usage:   "/sid",
	},
	"new": {
		summary: "创建并切换到新会话，同时 rollover 当前记忆 segment",
		usage:   "/new",
	},
	"switch": {
		summary: "将当前来源绑定到指定会话",
		usage:   "/switch <session_id>",
	},
	"reset": {
		summary: "清理当前会话的 LLM 上下文状态，不删除聊天记录和长期记忆",
		usage:   "/reset",
	},
	"clear": {
		summary: "清理当前来源窗口中的可见聊天历史，不改 LLM 上下文和长期记忆",
		usage:   "/clear",
	},
	"compact": {
		summary: "强制尝试压缩当前会话上下文",
		usage:   "/compact",
	},
	"forget": {
		summary: "进入长期记忆遗忘预览，不直接执行删除",
		usage:   "/forget <target>",
	},
	"stop": {
		summary: "停止当前来源/会话正在运行的回复",
		usage:   "/stop",
	},
	"quiet": {
		summary: "暂停主动消息一段时间，默认 2 小时",
		usage:   "/quiet [小时数|off]",
	},
	"bad": {
		summary: "标记一次不满意的回复并冻结当时现场",
		usage:   "/bad <哪里不对>",
	},
}
