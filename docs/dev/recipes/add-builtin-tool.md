# 配方：加一个内置工具

## 何时用

要加的能力属于核心链路、必须随二进制发布、或需要访问宿主内部状态时。

**大多数情况下你要的是插件，不是内置工具** —— 插件可独立开发、独立升级、不用重编二进制。先读 [`add-plugin.md`](add-plugin.md) 判断一下。

## 锚点文件

| 锚点 | 必须做什么 | 漏了会怎样 |
|---|---|---|
| `internal/tool/builtin/register.go` | `registry.Register(spec, handler)`；有外部依赖或配置开关的走独立 `registerXxx` 函数（照 `registerWebSearch` / `registerBash` 的写法） | 工具根本不存在 |
| `internal/progress/templates.go` | 在 `DefaultTemplates`（键是工具名）加中文进度短语 | **不报错**：调用时进度区落到 `_default` 的"处理中..."，体验断裂 |
| `internal/work/context.go` | Work 侧提示词里补该工具的使用说明 | **不报错**：Work agent 不知道有这个工具，等于对 Work 不可用 |
| `internal/tool/spec.go` | 定 `Scope` / `Permission` / `RoutingClass` | 见 [`../invariants.md`](../invariants.md) 第 6 条：闲聊里永不触发 |
| `config.yaml` + `internal/config` | 需要开关的工具（`bash`、`web_search`、`web_fetch`、`host_*` 都是）要有配置项，并在注册处判断 | 无法关闭；或在未配置环境下启动即失败 |

**最容易漏的是中间两个** —— `progress/templates.go` 和 `work/context.go` 都不会导致编译失败，只会让工具"看起来装好了但行为不对"。

## 步骤

### 1. 写 Spec 和 handler

新建 `internal/tool/builtin/<name>.go`。Spec 的字段见 `internal/tool/spec.go` 的 `type Spec struct`：

```go
var MyToolSpec = tool.Spec{
	Name:        "my_tool",
	Description: "写清楚什么时候该调用——LLM 靠它决定调不调",
	Parameters: json.RawMessage(`{
		"type":"object",
		"properties":{"query":{"type":"string"}},
		"required":["query"],
		"additionalProperties":false
	}`),
	Scope:      tool.ScopeWork,
	Permission: tool.PermReadOnly,
}
```

### 2. 决定它要不要在闲聊里可见

**默认是不可见的。** 要让 Emotion 在闲聊中能调用，三个字段必须同时满足：

```go
Scope:        tool.ScopeBoth,            // 或 tool.ScopeEmotion
Permission:   tool.PermReadOnly,
RoutingClass: tool.RoutingClassCasual,   // 不写即为 work
```

现成参照：`internal/tool/builtin/web_search.go` —— 它是目前唯一一个满足完整三元组的内置工具。

### 3. 注册

在 `internal/tool/builtin/register.go` 里加一行。无条件注册的直接写在 `RegisterAllWithFactsDBAndUsageRecorder` 里：

```go
registry.Register(MyToolSpec, myToolHandler)
```

依赖配置开关或外部服务的，另写 `registerMyTool(registry, cfg, logger)` 并在其中判断，照 `registerWebSearch` / `registerBash` 的结构。

### 4. 补进度短语（别漏）

`internal/progress/templates.go` 的 `DefaultTemplates`：

```go
"my_tool": {"正在查一下...", "让我看看..."},
```

键就是工具名。没有对应条目会静默落到 `_default`。

### 5. 补 Work 侧说明（别漏）

`internal/work/context.go` 里组装 Work 提示词的地方，补一句该工具的用法。照既有写法，讲清"什么时候用它、和别的工具怎么配合"。

Work agent 只从这段提示词知道有哪些工具可用 —— 注册了但没在这里写，Work 就不会去调它。

## 验证

```bash
go test ./internal/tool/... ./internal/progress/... ./internal/work/...
```

确认两个易漏点都改到了（**两个文件都必须有输出**）：

```bash
grep -n "my_tool" internal/progress/templates.go internal/work/context.go
```

要在闲聊中可见的话，编译后在 WebUI 正常聊天里说一句该触发它的话，确认真的被调用 —— 三元组只要有一项没配对，这里就会毫无反应且不报错。

```bash
go build -o ./bin/emoagent ./cmd/emoagent
```
