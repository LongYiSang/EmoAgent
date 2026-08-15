# EmoAgent Pre-NapCat Preparation Implementation Spec

> **Document status**: Implementation Spec for Codex  
> **Target path**: `docs/architecture/pre_napcat_step1_step2_implementation_spec.md`  
> **Scope**: 正式接入 NapCat Adapter 之前，先完成 Step1「Command Gateway Stabilization」和 Step2「Platform Adapter Abstraction」。  
> **Non-goal**: 本文不实现 NapCat / OneBot 具体传输协议，不实现 QQ 消息收发，不扩展完整 Forget Manager，不实现 provider/model/persona/admin 等高级命令。  
> **Current repo baseline**: EmoAgent `main` after unified chat command core commit.  

---

## 0. 一句话目标

本轮目标不是“接入 NapCat”，而是让 EmoAgent 已有的统一聊天入口和命令系统具备 **非 WebUI 平台可复用的稳定边界**：

```text
Step1: 让命令控制面在 WebUI 与未来外部平台上语义稳定、可观测、可测试。
Step2: 抽象平台入站/出站、Origin 映射、Actor/权限、消息去重和统一 Gateway，为 NapCat 原生 Adapter 铺路。
```

最终完成后，后续 NapCat Adapter 只需要实现：

```text
NapCat event -> platform.InboundMessage
platform.OutboundEvent -> NapCat send_private_msg / send_group_msg / compatible action
```

而不需要重新实现 session binding、command parsing、context reset、clear marker、run registry、plugin command 或 memory rollover 逻辑。

---

## 1. 当前仓库基线

### 1.1 已有能力

当前主干已经具备以下基础：

1. `Kernel.Services` 中已经有 `Conversation` 和 `Commands`，并且 `Commands` 被注入 `ChatService`，`Plugins` 也持有 `Commands`，说明命令核心已经进入应用服务层。
2. `CommandService.TryHandle` 已经在普通消息进入 LLM 前解析命令，并派发到 `/help /sid /new /switch /reset /clear /compact /forget /stop` 和 plugin command。
3. `conversation_origins / conversation_bindings / session_clear_markers / conversation_events` 已经落库，可以表达 Origin -> current session 的关系、可见历史清理标记和命令结果事件。
4. `command_configs / command_invocations` 已经落库，可以表达命令配置、审计和调用历史。
5. WebSocket handler 已经支持 `origin_key` 和 `source` query 参数，并在普通消息阶段先 `currentSession`、再 `tryHandleCommand`、最后才进入普通回复 run。
6. WebUI 协议类型已经包含 `command_result` 和 `context_switched`。

### 1.2 当前关键缺口

当前缺口不是“命令完全没有实现”，而是以下几类稳定性问题：

1. **保留根命令与已实现命令混在一起**：`set/unset/plugin/plugins/provider/model/memory/config/admin` 被作为 builtin descriptor 注册，但没有对应 handler，用户会感觉像“占位符坏掉”。
2. **`/help` 信息不足**：当前只输出命令名，缺少 summary、usage、实现状态、是否 preview/no-op。
3. **`/sid` 信息不足**：当前只输出 `origin_key/session_id/persona`，未来排查 NapCat 时需要 source/channel/adapter/external actor/conversation。
4. **`Actor` 信息缺失**：WebUI hardcode `member`；平台 adapter 需要传 actor id、display name、role，command invocation 也需要审计 actor id。
5. **WS origin resolver 太薄**：当前只接收 `origin_key` 与 `source`，未显式接收 adapter instance、platform id、channel、external conversation id、external actor id。
6. **尚无平台抽象层**：WebSocket handler 内聚了 WS 读写、origin 解析、session binding、command handling、normal turn run、outbound 写回。未来 NapCat 不应复制这段逻辑。
7. **尚无平台级入站去重**：消息平台常见重连/重复投递，Step2 需要先定义并实现通用 receipt/idempotency 层。

---

## 2. 设计原则

### 2.1 高内聚模块边界

保持三个层次清晰：

```text
internal/command
  只做命令注册、解析、descriptor、permission、registry。

internal/conversation
  只做 Origin、Binding、Timeline、RunRegistry、与会话状态相关的业务对象。

internal/platform
  只做平台抽象：InboundMessage、OutboundEvent、Adapter、OriginMapper、Actor、Receipt、Sink。
```

应用编排放在：

```text
internal/app
  CommandService
  ConversationService
  PlatformService
  PlatformGateway 或 ChatGateway
```

不要让 `internal/platform` 反向依赖具体 app services；它应该是协议和接口层。

### 2.2 命令结果直接返回，不走 LLM

保持既有决策：

```text
普通命令和插件命令默认 direct 输出，直接发给用户。
llm_synthesize 只作为插件命令显式配置的可选路径。
```

### 2.3 命令不写聊天 messages

保持既有决策：

```text
命令输入不持久化为 user message。
命令结果可以写 conversation_events / command_invocations。
```

平台 adapter 不应把 `/new` 这类命令作为普通聊天消息交给 LLM。

### 2.4 Platform Adapter 不等于 Plugin

本轮只做平台抽象。未来 NapCat 应作为原生 optional adapter 接入，而不是普通 plugin。普通 plugin 继续负责命令、工具、hook，不负责用户消息入口。

---

## 3. Step1：Command Gateway Stabilization

### 3.1 目标

让内置命令在 WebUI 与未来平台入口中具备明确、稳定、可观测的行为：

```text
/help      可读、只展示已实现命令或明确标注 preview
/sid       输出完整 origin/session/actor 诊断信息
/new       切换新 session，finalize/rollover 当前 memory segment
/switch    切换指定 session
/reset     设置 LLM context reset barrier，不删除聊天记录和长期记忆
/clear     只清当前 origin 的可见历史，不动上下文和长期记忆
/compact   best-effort 压缩上下文，无 summary model 时明确 no-op
/forget    preview-only / no destructive execution
/stop      停止当前 origin/session 的正在运行回复
```

不实现：

```text
/set /unset /plugin /plugins /provider /model /memory /config /admin 的完整功能
```

这些可以继续作为 reserved root，防止插件覆盖，但不应伪装成已实现命令。

### 3.2 修改 1：拆分 implemented builtin roots 与 reserved roots

#### 当前问题

`reservedRootCommands` 同时承担：

```text
A. 插件禁止覆盖的根命令列表
B. BuiltinProvider.Descriptors() 注册的内置命令列表
```

这会导致未实现的 reserved root 也出现在 registry 中。

#### 目标设计

在 `internal/command/reserved.go` 或新增 `builtin_roots.go` 中拆分：

```go
var implementedBuiltinRootCommands = []string{
    "help",
    "sid",
    "new",
    "switch",
    "reset",
    "clear",
    "compact",
    "forget",
    "stop",
}

var reservedRootCommands = []string{
    "help",
    "sid",
    "new",
    "switch",
    "reset",
    "clear",
    "compact",
    "forget",
    "stop",
    "set",
    "unset",
    "plugin",
    "plugins",
    "provider",
    "model",
    "memory",
    "config",
    "admin",
}
```

`BuiltinProvider.Descriptors()` 只注册 `implementedBuiltinRootCommands`。

#### Reserved but unimplemented handling

在 `CommandService.TryHandle` 中：

```go
if descriptor.ID == "" && commandcore.IsReservedRoot(parsed.Name) {
    return exec.finish(ctx, commandResult{
        Status: "not_implemented",
        Content: fmt.Sprintf("/%s 是 EmoAgent 保留命令，但当前版本尚未实现。", parsed.Name),
        ErrorKind: "reserved_not_implemented",
        Payload: map[string]any{"reserved": true},
    }), true, nil
}
```

不要返回“未知命令”，因为这会误导用户和插件开发者。

### 3.3 修改 2：补全 CommandDescriptor metadata

在 `internal/command/descriptor.go` 保持现有字段，但为 builtin descriptors 设置：

```go
Summary
Usage
Hidden
Reserved
ProviderKind
Permission
Scope
OutputMode
```

建议 builtin 元数据：

```text
/help
  summary: 显示可用命令和用法
  usage: /help [command]

/sid
  summary: 显示当前来源、会话、persona 和 actor 诊断信息
  usage: /sid

/new
  summary: 创建并切换到新会话，同时 rollover 当前记忆 segment
  usage: /new

/switch
  summary: 将当前来源绑定到指定会话
  usage: /switch <session_id>

/reset
  summary: 清理当前会话的 LLM 上下文状态，不删除聊天记录和长期记忆
  usage: /reset

/clear
  summary: 清理当前来源窗口中的可见聊天历史，不改 LLM 上下文和长期记忆
  usage: /clear

/compact
  summary: 强制尝试压缩当前会话上下文
  usage: /compact

/forget
  summary: 进入长期记忆遗忘预览，不直接执行删除
  usage: /forget <target>

/stop
  summary: 停止当前来源/会话正在运行的回复
  usage: /stop
```

### 3.4 修改 3：改造 `/help`

#### 当前问题

`/help` 只拼接 `Registry().Descriptors()` 的命令名。

#### 目标行为

`/help` 默认输出：

```text
可用命令：
/help [command] - 显示可用命令和用法
/sid - 显示当前来源、会话、persona 和 actor 诊断信息
/new - 创建并切换到新会话
/reset - 清理 LLM 上下文，不删除聊天记录和长期记忆
/clear - 清理当前窗口可见历史
/compact - 尝试压缩上下文
/forget <target> - 预览长期记忆遗忘候选，不执行删除
/stop - 停止当前回复
```

`/help forget` 输出单个命令详情：

```text
/forget <target>
状态：preview-only
说明：进入长期记忆遗忘预览，不直接执行删除。
```

实现建议：

```go
func (s *CommandService) handleHelp(parsed commandcore.ParsedCommand) commandResult
```

并在 `TryHandle` 中传入 parsed。

排序要求：

```text
builtin implemented commands first, stable order
plugin commands next, alphabetical by command name
hidden commands excluded
reserved unimplemented commands excluded from default /help
```

### 3.5 修改 4：增强 `/sid`

#### 目标输出

`/sid` 应输出：

```text
origin_key=napcat:main:group:123456
source_type=napcat
adapter_instance_id=main
platform_id=qq
channel_type=group
external_conversation_id=123456
external_actor_id=789000
actor_id=789000
actor_role=member
session_id=...
persona=default
```

WebUI 场景输出：

```text
origin_key=webui:local:main
source_type=webui
channel_type=web
session_id=...
persona=default
actor_role=member
```

#### 数据结构修改

扩展 `chat.CommandRequest`：

```go
type CommandRequest struct {
    Content    string
    Origin     conversation.Origin
    SessionID  string
    PersonaKey string
    ActorID    string
    ActorName  string
    ActorRole  string
}
```

`command_invocations.actor_id` 已存在，`persist` 时写入 `ActorID`。

### 3.6 修改 5：稳定 `/compact` 和 `/forget` 的状态语义

#### `/compact`

无 summary model 时返回：

```text
Status: "noop"
Content: "当前没有可用的总结模型，未压缩上下文。"
Payload: {"noop_reason":"summary_model_unavailable"}
```

有 summary model 但无 delta：

```text
Status: "noop"
Content: "当前没有需要压缩的新上下文。"
```

压缩成功：

```text
Status: "success"
Content: "已压缩当前会话上下文。"
```

#### `/forget`

MemoryCore unavailable：

```text
Status: "preview_unavailable"
Content: "已识别遗忘请求，但当前 Forget Manager 未可用；没有执行删除。"
```

Preview 成功：

```text
Status: "preview"
Content: BuildManualForgetPreviewNotice(preview)
Payload: operation_id, preview_hash, target, destructive=false
```

不要返回普通 `success`，避免用户误以为已经遗忘。

### 3.7 修改 6：命令结果事件统一 payload

所有 `command_result` 与 `context_switched` payload 至少包含：

```json
{
  "command_id": "builtin.reset",
  "command_name": "reset",
  "status": "success",
  "error_kind": "",
  "origin_key": "webui:local:main",
  "source_type": "webui",
  "session_id": "...",
  "persona": "default",
  "actor_id": "",
  "actor_role": "member",
  "reload_history": false,
  "reload_memory": true
}
```

注意：

```text
command_invocations.input_hash 可以保留；不要把原始命令全文写入 payload。
```

### 3.8 修改 7：非 WebUI origin 单元测试

新增测试文件建议：

```text
internal/app/command_service_platform_test.go
internal/chat/handler_origin_test.go
internal/conversation/origin_test.go
```

测试矩阵：

```text
1. /sid with synthetic napcat private origin
2. /sid with synthetic napcat group origin
3. /help excludes reserved unimplemented roots
4. /set returns reserved_not_implemented, not unknown
5. /new updates conversation_bindings for non-webui origin
6. /reset writes ResetBarrier and returns reload_memory=true
7. /clear writes session_clear_markers scoped by origin_key/session_id
8. /compact no summary model returns noop
9. /forget unavailable returns preview_unavailable, not success
10. /stop stops run registered by same origin/session
```

### 3.9 Step1 验收标准

Step1 完成时：

```text
- /help 只展示已实现命令和 plugin commands，不展示 set/provider/model/admin 等保留未实现 root。
- /set 等保留未实现命令返回 reserved_not_implemented。
- /sid 能输出完整 Origin + Actor + Binding 诊断。
- /forget 不再以 success 暗示已删除；只表达 preview 或 unavailable。
- 命令结果和 context switch 事件 payload 统一。
- command_invocations.actor_id 能被写入。
- 所有核心命令对 synthetic napcat origin 的单测通过。
```

---

## 4. Step2：Platform Adapter Abstraction

### 4.1 目标

建立平台无关的入站/出站与 Gateway 契约，让未来 NapCat Adapter 不需要复制 WebSocket handler 的业务逻辑。

Step2 只做抽象和 fake adapter 测试，不接 NapCat。

### 4.2 新增包结构

建议新增：

```text
internal/platform/
  types.go
  actor.go
  origin.go
  receipts.go
  sink.go
  adapter.go
  manager.go
  gateway.go      // 可选：如果 gateway 放在 platform 层

internal/app/
  platform_service.go
  platform_gateway.go
  platform_gateway_test.go
```

更推荐将业务编排放在 `internal/app/platform_gateway.go`，让 `internal/platform` 保持纯接口和 DTO。

### 4.3 Platform DTO

#### Actor

```go
type ActorRole string

const (
    ActorRoleMember ActorRole = "member"
    ActorRoleAdmin  ActorRole = "admin"
    ActorRoleOwner  ActorRole = "owner"
)

type Actor struct {
    ID          string
    DisplayName string
    Role        ActorRole
    IsBot       bool
    Raw         map[string]any
}
```

#### InboundMessage

```go
type InboundMessage struct {
    ID                     string // EmoAgent internal id, optional before receipt
    ExternalMessageID      string
    SourceType             string // webui | napcat | telegram | api | ...
    AdapterInstanceID      string
    PlatformID             string // qq | telegram | web | ...
    ChannelType            string // web | private | group | guild | api
    ExternalConversationID string // user_id or group_id
    ExternalActorID        string // sender_id
    PersonaKey             string
    Text                   string
    Parts                  []llm.ContentBlock
    Actor                  Actor
    Timestamp              time.Time
    RawEventHash           string
    Raw                    map[string]any
}
```

#### OutboundEvent

```go
type OutboundEvent struct {
    Type        string // message | command_result | context_switched | typing | error | debug
    Origin      conversation.Origin
    SessionID   string
    PersonaKey  string
    Content     string
    Status      string
    ErrorKind   string
    Payload     map[string]any
    ReplyToExternalMessageID string
}
```

### 4.4 Origin mapping

#### 新增 OriginScope

```go
type OriginScope string

const (
    OriginScopePrivate        OriginScope = "private"
    OriginScopeGroupShared    OriginScope = "group_shared"
    OriginScopeGroupUserUnique OriginScope = "group_user_unique"
)
```

#### BuildOriginKey

新增：

```go
func BuildOriginKey(req OriginBuildRequest) (string, error)
```

```go
type OriginBuildRequest struct {
    SourceType             string
    AdapterInstanceID      string
    PlatformID             string
    ChannelType            string
    ExternalConversationID string
    ExternalActorID        string
    Scope                  OriginScope
}
```

规则：

```text
private:
  <source>:<instance>:private:<external_actor_id or external_conversation_id>

group_shared:
  <source>:<instance>:group:<external_conversation_id>

group_user_unique:
  <source>:<instance>:group_user:<external_conversation_id>:<external_actor_id>
```

未来 NapCat 默认：

```text
私聊独立 -> private
群聊共享 -> group_shared
```

#### Sanitization

Origin key 只允许：

```text
A-Z a-z 0-9 : . _ -
```

外部 ID 进入 origin key 前统一做：

```text
trim
replace unsupported chars with _
collapse repeated _
max segment length
```

### 4.5 扩展 conversation.Origin

当前 `conversation.Origin` 已经具备 platform 相关字段。Step2 要求所有 platform inbound 都必须填充：

```go
conversation.Origin{
    OriginKey:              ...,
    SourceType:             inbound.SourceType,
    AdapterInstanceID:      inbound.AdapterInstanceID,
    PlatformID:             inbound.PlatformID,
    ChannelType:            inbound.ChannelType,
    ExternalConversationID: inbound.ExternalConversationID,
    ExternalActorID:        inbound.ExternalActorID,
    DisplayName:            ...,
}
```

### 4.6 Platform Message Receipt / Idempotency

#### 新增 schema migration

建议新增 migration version 39：

```sql
CREATE TABLE IF NOT EXISTS platform_message_receipts (
    id                         TEXT PRIMARY KEY,
    source_type                TEXT NOT NULL,
    adapter_instance_id        TEXT NOT NULL DEFAULT '',
    platform_id                TEXT NOT NULL DEFAULT '',
    external_message_id        TEXT NOT NULL,
    origin_key                 TEXT NOT NULL,
    session_id                 TEXT NOT NULL DEFAULT '',
    persona_key                TEXT NOT NULL DEFAULT '',
    message_hash               TEXT NOT NULL DEFAULT '',
    status                     TEXT NOT NULL DEFAULT 'processing'
        CHECK(status IN ('processing','handled','duplicate','failed','ignored')),
    result_type                TEXT NOT NULL DEFAULT '',
    error_message              TEXT NOT NULL DEFAULT '',
    received_at                TEXT NOT NULL DEFAULT (datetime('now')),
    handled_at                 TEXT,
    UNIQUE(source_type, adapter_instance_id, external_message_id)
);

CREATE INDEX IF NOT EXISTS idx_platform_message_receipts_origin_time
    ON platform_message_receipts(origin_key, received_at DESC);

CREATE INDEX IF NOT EXISTS idx_platform_message_receipts_status
    ON platform_message_receipts(status, received_at DESC);
```

#### Store API

```go
type ReceiptStore interface {
    BeginInbound(ctx context.Context, msg platform.InboundMessage, origin conversation.Origin) (ReceiptResult, error)
    CompleteInbound(ctx context.Context, receiptID string, sessionID string, resultType string) error
    FailInbound(ctx context.Context, receiptID string, sessionID string, err error) error
}
```

Semantics：

```text
- 第一次 external_message_id: status=processing，继续处理。
- 已 handled: 返回 duplicate，adapter 不再重复回复。
- processing 且未超时: 返回 duplicate/in_progress。
- processing 过期: 可 claim/retry，后续再完善。
```

MVP 可以只做 handled duplicate，不做复杂 claim。

### 4.7 PlatformGateway

#### 位置

建议放在：

```text
internal/app/platform_gateway.go
```

因为它需要访问：

```text
ConversationService
CommandService
ChatService.Engine
PersonaService/App persona resolver
RunRegistry
Logger
```

#### 接口

```go
type PlatformGateway struct {
    infra        *Infra
    conversation *ConversationService
    commands     *CommandService
    chat         *ChatService
    personas     *PersonaService
    receipts     platform.ReceiptStore
}

func (g *PlatformGateway) HandleInbound(ctx context.Context, in platform.InboundMessage, sink platform.OutboundSink) (platform.HandleResult, error)
```

#### Flow

```text
1. Validate inbound text/parts.
2. Build/validate Origin.
3. Begin receipt/idempotency if ExternalMessageID exists.
4. Resolve persona: inbound.PersonaKey -> active/default persona.
5. Ensure current binding via Conversation.Bindings().EnsureCurrent.
6. Build CommandRequest with Origin, SessionID, PersonaKey, ActorID, ActorRole.
7. Try command.
   - if handled: emit command_result/context_switched to sink; complete receipt; return.
8. If normal message:
   - register run in RunRegistry with origin/session.
   - call engine path.
   - convert stream/segments into platform.OutboundEvent.
   - complete receipt.
9. On error: emit error event; fail receipt.
```

#### MVP normal message behavior

为了降低风险，Step2 的 `PlatformGateway` 可以先只支持：

```text
- text-only input
- direct non-stream final reply OR buffered stream to final text
- command handling
```

不需要在 Step2 支持：

```text
- image/audio/file message parts
- typing indicator
- group mention parsing
- quote/reply/thread context
- platform native markdown/card
```

### 4.8 BufferedPlatformSink

新增：

```go
type BufferedPlatformSink struct {
    Events []platform.OutboundEvent
}
```

用途：

```text
- 单元测试 PlatformGateway。
- 未来 NapCat 默认可以先使用 buffered final text，避免 token streaming 在 QQ 中刷屏。
```

事件转换规则：

| Internal event | Platform event |
|---|---|
| `command_result` | `command_result` with direct content |
| `context_switched` | `context_switched` plus optional user-visible content |
| `stream_delta` | buffer text |
| `assistant_segment` | append as segment or buffer, depending capability |
| `stream_end` | emit final `message` |
| `error` | emit `error` |
| `approval_required` | emit `message` or `approval_required` placeholder; platform interaction later |

Step2 可以只做 text buffer。

### 4.9 PlatformService / Manager

新增 `Services.Platforms`：

```go
type PlatformService struct {
    infra   *Infra
    gateway *PlatformGateway
    manager *platform.Manager
}
```

Kernel 初始化：

```go
services.Platforms = &PlatformService{...}
services.Platforms.gateway = NewPlatformGateway(...)
```

Step2 不启动任何真实 adapter，只注册 fake/local adapter in tests。

### 4.10 Config 预留

当前 config strict unmarshal 的 allowed keys 不包含 `platforms`。Step2 需要新增：

```go
type Config struct {
    ...
    Platforms PlatformsConfig `yaml:"platforms" json:"platforms"`
}
```

并把 `platforms` 加入 allowed map。

建议配置：

```go
type PlatformsConfig struct {
    Enabled bool `yaml:"enabled" json:"enabled"`
    Common  PlatformCommonConfig `yaml:"common" json:"common"`
    Adapters map[string]PlatformAdapterConfig `yaml:"adapters" json:"adapters"`
}

type PlatformCommonConfig struct {
    DefaultPersona string   `yaml:"default_persona" json:"default_persona"`
    CommandPrefixes []string `yaml:"command_prefixes" json:"command_prefixes"`
}

type PlatformAdapterConfig struct {
    Enabled bool `yaml:"enabled" json:"enabled"`
    Kind string `yaml:"kind" json:"kind"` // napcat later
    InstanceID string `yaml:"instance_id" json:"instance_id"`
    PlatformID string `yaml:"platform_id" json:"platform_id"`
    ConfigJSON map[string]any `yaml:"config" json:"config"`
}
```

Default config：

```yaml
platforms:
  enabled: false
  common:
    default_persona: ""
    command_prefixes: ["/"]
  adapters: {}
```

不要在 Step2 加 NapCat 专属字段；正式 Step3 再加：

```text
napcat.endpoint
napcat.access_token_env
napcat.mode
napcat.group_session_mode
napcat.allow_groups
```

### 4.11 WebUI 是否迁移到 PlatformGateway？

Step2 推荐 **不强制重写 WebSocket handler**，避免风险过大。

但需要做到：

```text
- PlatformGateway 单测覆盖与 WS handler 等价的核心业务流程。
- WS handler 与 PlatformGateway 共用 command/origin/receipt DTO 或 helper。
- 后续可逐步将 WS handler 内的 command/session/run 编排迁出。
```

可以先把 `resolveWSOrigin` 扩展为：

```go
func resolveWSOrigin(r *http.Request) (conversation.Origin, error) {
    query := r.URL.Query()
    return conversation.ResolveOrigin(conversation.OriginRequest{
        OriginKey:              strings.TrimSpace(query.Get("origin_key")),
        SourceType:             strings.TrimSpace(query.Get("source")),
        AdapterInstanceID:      strings.TrimSpace(query.Get("adapter_instance_id")),
        PlatformID:             strings.TrimSpace(query.Get("platform_id")),
        ChannelType:            strings.TrimSpace(query.Get("channel_type")),
        ExternalConversationID: strings.TrimSpace(query.Get("external_conversation_id")),
        ExternalActorID:        strings.TrimSpace(query.Get("external_actor_id")),
        DisplayName:            strings.TrimSpace(query.Get("display_name")),
    })
}
```

WebUI 默认仍然是 `webui:local:main`。

### 4.12 Step2 测试矩阵

新增：

```text
internal/platform/origin_test.go
internal/platform/receipts_test.go
internal/app/platform_gateway_test.go
internal/chat/handler_origin_test.go
internal/config/platforms_config_test.go
```

测试：

```text
1. BuildOriginKey private -> napcat:main:private:10001
2. BuildOriginKey group_shared -> napcat:main:group:20002
3. BuildOriginKey group_user_unique -> napcat:main:group_user:20002:10001
4. Invalid external id is sanitized or rejected according to policy.
5. ResolveOrigin stores all fields into conversation_origins.
6. PlatformGateway handles /sid and emits command_result with full origin and actor.
7. PlatformGateway handles /new and updates binding for synthetic napcat origin.
8. PlatformGateway duplicate ExternalMessageID does not double invoke command.
9. PlatformGateway normal text with fake engine emits message event.
10. WS origin resolver accepts adapter_instance_id/platform_id/channel_type/external ids without breaking default WebUI.
11. Config accepts platforms key and defaults disabled.
```

### 4.13 Step2 验收标准

Step2 完成时：

```text
- internal/platform 存在稳定 DTO、OriginBuilder、Actor、OutboundSink、ReceiptStore 接口。
- Config 支持 platforms 但默认关闭。
- conversation.Origin 可以从 platform.InboundMessage 完整构造。
- PlatformGateway 可以在 fake platform 测试中处理 /sid /new /reset /clear /stop 和普通 text。
- 平台入站消息有 receipt/idempotency，重复 external_message_id 不重复执行命令或回复。
- WebUI 行为不回退，既有 /ws 流程仍可用。
- 未实现 NapCat 传输，不引入 NapCat 依赖。
```

---

## 5. 推荐提交拆分

### PR/Commit 1：Builtin command metadata + reserved handling

文件：

```text
internal/command/reserved.go
internal/command/builtin.go
internal/command/descriptor.go
internal/app/command_service.go
```

验收：

```text
/help readable
/set reserved_not_implemented
implemented builtins only in default help
```

### PR/Commit 2：Command diagnostics and event payload

文件：

```text
internal/chat/handler.go
internal/app/command_service.go
internal/storage/db.go
internal/storage/types.go
web/src/chat/protocol/wsTypes.ts
```

验收：

```text
/sid full origin + actor
command_invocations.actor_id written
command_result payload normalized
```

### PR/Commit 3：Command non-WebUI origin tests and behavior polish

文件：

```text
internal/app/command_service_platform_test.go
internal/conversation/origin_test.go
internal/storage/db_test.go
```

验收：

```text
synthetic napcat origin tests pass
/compact noop
/forget preview statuses
```

### PR/Commit 4：Platform DTO + OriginBuilder

文件：

```text
internal/platform/types.go
internal/platform/actor.go
internal/platform/origin.go
internal/platform/sink.go
internal/platform/adapter.go
internal/platform/origin_test.go
```

验收：

```text
origin keys stable
private/group mapping verified
```

### PR/Commit 5：Platform receipts schema + store

文件：

```text
internal/storage/schema.go
internal/storage/platform_receipts.go
internal/platform/receipts.go
internal/platform/receipts_test.go
```

验收：

```text
duplicate external message id is idempotent
```

### PR/Commit 6：PlatformGateway fake adapter integration

文件：

```text
internal/app/platform_service.go
internal/app/platform_gateway.go
internal/app/platform_gateway_test.go
internal/app/kernel.go
```

验收：

```text
fake inbound /sid works
fake inbound /new updates binding
fake normal text emits final message through buffered sink
```

### PR/Commit 7：Config platforms skeleton

文件：

```text
internal/config/config.go
internal/config/defaults.go or equivalent
config.example.yaml if exists
internal/config/platforms_config_test.go
```

验收：

```text
platforms key accepted
default disabled
unknown adapter kind not started
```

---

## 6. Post-Step2 下一步边界

完成 Step1 + Step2 后，正式 NapCat 接入应只新增：

```text
internal/platform/napcat/
  config.go
  adapter.go
  onebot_event.go
  onebot_action.go
  mapper.go
  sender.go
  reconnect.go
```

并在 `PlatformService` 中注册：

```text
kind = napcat
```

不要在 NapCat Adapter 中实现：

```text
session binding
command parsing
/reset barrier
/clear marker
memory rollover
plugin command invocation
LLM turn orchestration
```

这些必须复用 Step1/Step2 完成后的平台无关 Gateway。

---

## 7. Definition of Done

本 Spec 完成的最终判定：

```text
1. WebUI 现有聊天、命令、context switch 行为不回退。
2. 非 WebUI synthetic origin 可以通过 service-level gateway 完整执行命令。
3. /help 和 reserved command 语义清晰，没有“看似有命令但不能用”的体验。
4. /sid 足以排查未来 NapCat 的私聊/群聊 origin 绑定问题。
5. 平台入站消息具备去重和审计能力。
6. PlatformGateway 已能处理 text-only fake adapter，且不依赖 WS。
7. Config 中 platforms 默认关闭，项目启动行为不变。
8. 没有引入 NapCat 传输依赖，没有提前实现 OneBot 细节。
```
