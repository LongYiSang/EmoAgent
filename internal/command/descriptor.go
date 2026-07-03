package command

type CommandProviderKind string

const (
	CommandProviderBuiltin CommandProviderKind = "builtin"
	CommandProviderPlugin  CommandProviderKind = "plugin"
)

type CommandPermission string

const (
	CommandPermissionEveryone CommandPermission = "everyone"
	CommandPermissionMember   CommandPermission = "member"
	CommandPermissionAdmin    CommandPermission = "admin"
	CommandPermissionOwner    CommandPermission = "owner"
)

type CommandScope string

const (
	CommandScopeOrigin  CommandScope = "origin"
	CommandScopeSession CommandScope = "session"
	CommandScopeGlobal  CommandScope = "global"
	CommandScopeAdmin   CommandScope = "admin"
)

type CommandOutputMode string

const (
	CommandOutputDirect        CommandOutputMode = "direct"
	CommandOutputLLMSynthesize CommandOutputMode = "llm_synthesize"
)

type CommandArgSpec struct {
	Name   string
	Greedy bool
}

type CommandLLMSynthesisSpec struct {
	Prompt string
}

type CommandDescriptor struct {
	ID           string
	Name         string
	Aliases      []string
	Summary      string
	Usage        string
	Hidden       bool
	Reserved     bool
	ProviderKind CommandProviderKind
	PluginID     string
	Handler      string
	Permission   CommandPermission
	Scope        CommandScope
	Capabilities []string
	Args         []CommandArgSpec
	OutputMode   CommandOutputMode
	LLM          *CommandLLMSynthesisSpec
	TimeoutMS    int
}

type ParsedCommand struct {
	Raw   string
	Name  string
	Args  []string
	Flags map[string]string
}
