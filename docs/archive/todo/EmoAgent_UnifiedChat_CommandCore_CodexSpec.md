# EmoAgent 统一聊天入口与 Command Core 分阶段实施 Spec

> Document status: Codex implementation spec draft  
> Target repository: `LongYiSang/EmoAgent`  
> Target path suggestion: `docs/architecture/unified_chat_command_core_codex_spec.md`  
> Scope: 统一聊天入口、Origin → Session Binding、内置命令、插件命令扩展、WebUI ChatUI 改造。  
> Explicit non-goal: 本轮不接 NapCat Adapter；仅预留 source/origin 数据模型与策略。  
> Last updated: 2026-07-03

---

## 0. 本 Spec 的实施目标

本次改造的目标是把 EmoAgent 从当前 **显式 Session 选择式聊天入口**，演进为：

```text
统一聊天入口
  + Origin 当前会话绑定
  + 独立 Command Core
  + 命令结果直接返回
  + 插件可扩展命令
```

最终用户体验：

```text
用户进入一个主聊天窗口。
默认继续 webui:local:main 当前绑定的 Session。
用户可以在输入框中使用 /new、/reset、/clear、/compact、/forget、/sid 等命令。
命令输入本身不作为 user message 持久化，也不进入 MemoryCore episode。
命令结果可以作为 command_result 记录和显示，但不进入普通 LLM 上下文。
插件可以注册命令；默认直接返回文本，不走 LLM。
只有命令声明 output_mode=llm_synthesize 时，才由 Host 进行一次受控 LLM 后处理。
```

关键要求：**Session 不删除、不弱化，只从“用户显式入口”下沉为“Origin 当前绑定的逻辑会话”。**

---

## 1. 已确认决策

### 1.1 命令语义

```text
/reset
  清理 LLM 上下文状态。
  不删除消息。
  不删除长期记忆。
  需要设置新的 context reset barrier。
  建议 finalize 当前 Memory segment，并在同一 Session 内开启新 segment。

/clear
  清理当前 Session 的可见消息历史。
  不动 LLM 上下文。
  不删除 messages 表数据。
  不改变 sessions.metadata / running summary / reset barrier。
  建议通过 per-origin/session clear marker 实现 UI 可见清理。

/forget
  进入长期记忆 Forget Manager。
  不把 /forget 命令文本写入普通消息或 MemoryCore。
  根据目标和危险程度决定是否确认。
```

### 1.2 WebUI 默认会话

```text
WebUI 默认 origin_key = webui:local:main。
进入主聊天页时继续该 origin 当前绑定的 Session。
如果还没有 binding，迁移策略：优先绑定 active persona 下最新非空 Session；若无，则创建或懒创建新 Session。
```

### 1.3 外部消息平台策略预留

本轮不接 NapCat Adapter，但数据模型预留：

```text
私聊：独立 Origin / 独立当前 Session。
群聊：共享 Origin / 共享当前 Session。
```

未来 NapCat 可映射为：

```text
napcat:<instance>:private:<user_id>
napcat:<instance>:group:<group_id>
```

暂不实现 group_user_unique。

### 1.4 命令持久化

```text
命令输入不持久化为 user message。
命令输入不追加到 MemoryCore episode。
命令输入不进入 running summary。
命令结果可以记录。
```

建议记录方式：

```text
command_invocations    -- 审计、结果、耗时、状态
conversation_events    -- 可见 timeline 事件，例如 command_result/context_switched
```

不要把 command_result 写进 `messages` 表作为 assistant message，除非同时实现 LLM context 过滤。更干净的方案是新增 timeline event 表，并让 WebUI 把 messages + events 合并显示。

### 1.5 插件命令

```text
插件不能覆盖内置根命令。
插件命令默认直接返回文本，不走 LLM。
插件命令可以声明 output_mode=llm_synthesize，由 Host 对命令结果做一次受控 LLM 后处理。
llm_synthesize 仍然不把命令输入作为普通 user message 写入历史或记忆。
```

### 1.6 Memory segment

```text
/new 必须 finalize 当前 Session 的当前 Memory segment。
/reset 建议也 finalize 当前 Session 的当前 Memory segment，因为它建立新的 LLM context boundary。
/switch 到另一个 Session 时沿用现有 resume/rollover 语义。
```

---

## 2. 当前仓库基线

### 2.1 当前入口状态

现有 WebUI 是 session-centric：

```text
web/src/chat/ChatApp.tsx
web/src/chat/hooks/useChatSession.ts
web/src/chat/hooks/useChatWebSocket.ts
```

当前前端持有：

```ts
contextRef = { personaKey, sessionID }
```

提交消息前，前端会先把用户消息加入本地 timeline，再通过 WS 发送 `type: "message"`。这对命令不合适，因为命令输入不应显示为普通 user message，也不应在 reload 后作为普通消息恢复。

当前 WebSocket URL 使用：

```text
/ws?persona=<persona>&session_id=<session_id>&skip_greeting=1
```

目标改为：

```text
/ws?source=webui&origin_key=webui:local:main
```

兼容保留：

```text
/ws?persona=<persona>&session_id=<session_id>
```

当兼容参数存在时，视为一次 bootstrap switch，并写入当前 origin binding。

### 2.2 当前后端入口

当前后端 `internal/chat/handler.go` 在 WS 握手阶段解析 persona 和 session_id，先 `ResumeSession`，失败就 `StartSession`，然后发 `session_ready`。之后每个 `message` 都直接进入普通 LLM / Turn Pipeline 路径。

目标：把“解析当前 Session”从 handler 局部变量下沉到 `internal/conversation`：

```text
OriginResolver
BindingService
ConversationGateway
```

Handler 不再长期持有不可变 sessionID。每条 incoming message 都通过 Gateway 获取当前 binding；命令执行后，如果 binding 变化，Gateway 负责发 `context_switched` / `session_ready`。

### 2.3 当前 Turn Pipeline

当前 `internal/turn/contract.go` 已经有这些概念：

```go
InboundSource: SourceWebUI, SourceSystem
InboundKind:   user_message, approval_action, system_resume
StageName:     session_bind, memory_prepare, emotion_prepare, emotion_loop, ...
```

但当前 `internal/chat/turn_runtime.go` 实际 stages 主要是：

```text
normalize
memory_prepare
emotion_prepare
message/emotion_loop
memory_commit
emit_approvals
```

本次设计建议：**命令不作为普通 Turn stage 实现，而作为 ConversationGateway 的 pre-turn route 实现。**

原因：

1. `/new`、`/switch` 会改变当前 binding，handler 必须在下一条消息前看到新 session。
2. 命令必须在 memory_prepare 前终止，不能写入 user message / MemoryCore。
3. 插件命令默认不是新的对话者，也不是 LLM final answer。
4. 命令有自己的 audit 和 permission 体系，不应伪装成普通对话 Turn。

可以保留 `StageSessionBind` 作为未来平台统一 Turn 化的占位，但 P1-P4 不强依赖它。

### 2.4 当前上下文状态

当前 `internal/context/summary.go` 通过 `sessions.metadata` 存取 `ContextState`，其中包含：

```text
running_summary
summary_covered_until_message_id
prompt_route
last_context_stats
keep_recent_user_turns
```

`/reset` 不能只清空 running_summary；还必须增加 **context reset barrier**，否则当前 Engine 仍会从 `GetAllMessages` 读取 reset 前的最近消息进入 LLM 上下文。

### 2.5 当前 Memory segment 能力

`internal/memoryhost/bridge.go` 已有：

```go
EnsureSegment(ctx, chatSessionID, personaID)
RolloverSegment(ctx, chatSessionID, personaID, reason)
FinalizeSegment(ctx, segmentID, reason, summary)
```

`RolloverSegment` 会 finalize 当前 segment 并开始新 segment。因此 `/new` 和 `/reset` 应优先调用该能力，而不是直接操作 MemoryCore 内部状态。

### 2.6 当前插件能力

当前插件系统已有：

```text
internal/plugin/types.go
internal/plugin/manifest_v2.go
internal/plugin/registrar.go
internal/plugin/host.go
internal/app/plugin_service.go
```

已有 capability 包括：

```text
turn.read
memory.read.safe
memory.candidate.submit
memory.forget.request
memory.forget.destructive
tool.register
provider.generate
plugin.kv
plugin.files
...
```

当前 plugin host 主要包装 Turn stages 和 outbound sink。插件工具已经做 namespacing，并通过 tool dispatcher / approval gate。命令扩展应复用 Manifest / Capability / RuntimeSupervisor / FacadeBroker，但不要把命令注册塞成普通 Hook。

---

## 3. 目标架构

### 3.1 模块拆分

新增两个高内聚模块：

```text
internal/conversation/
  origin.go
  binding.go
  gateway.go
  session_ops.go
  timeline.go
  run_registry.go
  store.go
  migrations.go 或 storage methods

internal/command/
  descriptor.go
  registry.go
  parser.go
  permissions.go
  service.go
  builtin.go
  execution.go
  result.go
  audit.go
  plugin_adapter.go
  llm_synthesis.go
```

服务层新增：

```go
type ConversationService struct { ... }
type CommandService struct { ... }
```

`internal/app/kernel.go` 中新增：

```go
Services.Conversation *ConversationService
Services.Commands     *CommandService
```

推荐服务依赖方向：

```text
CommandService
  depends on ConversationService facade
  depends on SessionService / Chat Engine facade where needed
  depends on MemoryService facade for /forget and segment rollover
  depends on WorkService facade for /stop best-effort
  depends on PluginService for plugin descriptors/invocation

ConversationService
  depends on storage.DB
  depends on Chat Engine for StartSession/ResumeSession only through narrow facade
  depends on MemoryService/Bridge for segment rollover/finalize through narrow facade
```

避免反向依赖：

```text
conversation 不依赖 command。
plugin 不直接依赖 web/chat handler。
command 不直接使用 raw sql.DB，除非通过 command.Store。
```

### 3.2 总入口链路

```text
WebUI / future platform adapter
        ↓
ConversationGateway.ResolveInbound
        ↓
OriginResolver.Resolve
        ↓
BindingService.EnsureCurrentBinding
        ↓
CommandService.TryHandle
        ├─ handled: emit command_result / context_switched, stop
        └─ not command: pass to chat TurnRuntime / Engine
```

命令执行成功或失败都走统一 command result：

```json
{
  "type": "command_result",
  "command_id": "builtin.reset",
  "command_name": "reset",
  "status": "success",
  "content": "已重置当前会话上下文。",
  "session_id": "...",
  "persona": "...",
  "payload": {}
}
```

Session 变化事件：

```json
{
  "type": "context_switched",
  "reason": "new|switch|reset",
  "origin_key": "webui:local:main",
  "session_id": "...",
  "persona": "default",
  "reload_history": true,
  "reload_memory": true
}
```

---

## 4. 数据模型

### 4.1 Migration A: conversation bindings

建议文件：

```text
internal/storage/migrations/00xx_conversation_bindings.sql
```

表：`conversation_origins`

```sql
CREATE TABLE IF NOT EXISTS conversation_origins (
    id                         TEXT PRIMARY KEY,
    origin_key                 TEXT NOT NULL UNIQUE,
    source_type                TEXT NOT NULL, -- webui | napcat | telegram | api | cli | other
    adapter_instance_id         TEXT NOT NULL DEFAULT '',
    platform_id                TEXT NOT NULL DEFAULT '',
    channel_type               TEXT NOT NULL, -- web | private | group | api | cli | other
    external_conversation_id    TEXT NOT NULL DEFAULT '',
    external_actor_id           TEXT NOT NULL DEFAULT '',
    display_name               TEXT NOT NULL DEFAULT '',
    metadata_json              TEXT NOT NULL DEFAULT '{}',
    created_at                 TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                 TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_conversation_origins_source
    ON conversation_origins(source_type, adapter_instance_id, channel_type, external_conversation_id);
```

表：`conversation_bindings`

```sql
CREATE TABLE IF NOT EXISTS conversation_bindings (
    id                         TEXT PRIMARY KEY,
    origin_key                 TEXT NOT NULL,
    persona_key                TEXT NOT NULL,
    current_session_id          TEXT NOT NULL,
    default_persona_key         TEXT NOT NULL DEFAULT '',
    unique_scope               TEXT NOT NULL DEFAULT 'origin', -- origin | actor, future only
    variables_json             TEXT NOT NULL DEFAULT '{}',
    created_at                 TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                 TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(origin_key) REFERENCES conversation_origins(origin_key) ON DELETE CASCADE,
    FOREIGN KEY(current_session_id) REFERENCES sessions(id) ON DELETE CASCADE,
    UNIQUE(origin_key, persona_key)
);

CREATE INDEX IF NOT EXISTS idx_conversation_bindings_current_session
    ON conversation_bindings(current_session_id);
```

表：`session_clear_markers`

```sql
CREATE TABLE IF NOT EXISTS session_clear_markers (
    id                         TEXT PRIMARY KEY,
    origin_key                 TEXT NOT NULL,
    session_id                 TEXT NOT NULL,
    persona_key                TEXT NOT NULL,
    after_message_id            TEXT NOT NULL DEFAULT '',
    cleared_at                 TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    reason                     TEXT NOT NULL DEFAULT 'command_clear',
    metadata_json              TEXT NOT NULL DEFAULT '{}',
    UNIQUE(origin_key, session_id),
    FOREIGN KEY(origin_key) REFERENCES conversation_origins(origin_key) ON DELETE CASCADE,
    FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_session_clear_markers_session
    ON session_clear_markers(session_id, origin_key);
```

说明：`/clear` 只写此表或更新 marker，不删除 `messages`，不更新 `sessions.metadata`。

表：`conversation_events`

```sql
CREATE TABLE IF NOT EXISTS conversation_events (
    id                         TEXT PRIMARY KEY,
    origin_key                 TEXT NOT NULL,
    session_id                 TEXT NOT NULL,
    persona_key                TEXT NOT NULL,
    event_type                 TEXT NOT NULL, -- command_result | context_switched | system_notice
    visible_content            TEXT NOT NULL DEFAULT '',
    payload_json               TEXT NOT NULL DEFAULT '{}',
    created_at                 TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    visibility_status          TEXT NOT NULL DEFAULT 'visible', -- visible | hidden
    FOREIGN KEY(origin_key) REFERENCES conversation_origins(origin_key) ON DELETE CASCADE,
    FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_conversation_events_session_time
    ON conversation_events(session_id, created_at);
```

### 4.2 Migration B: command config and audit

建议文件：

```text
internal/storage/migrations/00xy_command_core.sql
```

表：`command_configs`

```sql
CREATE TABLE IF NOT EXISTS command_configs (
    command_id                 TEXT PRIMARY KEY,
    provider_kind              TEXT NOT NULL, -- builtin | plugin
    plugin_id                  TEXT NOT NULL DEFAULT '',
    original_name              TEXT NOT NULL,
    effective_name             TEXT NOT NULL,
    aliases_json               TEXT NOT NULL DEFAULT '[]',
    enabled                    INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0, 1)),
    permission                 TEXT NOT NULL DEFAULT 'member', -- everyone | member | admin | owner
    output_mode                TEXT NOT NULL DEFAULT 'direct', -- direct | llm_synthesize
    config_json                TEXT NOT NULL DEFAULT '{}',
    updated_at                 TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_command_configs_effective
    ON command_configs(effective_name, enabled);
```

表：`command_invocations`

```sql
CREATE TABLE IF NOT EXISTS command_invocations (
    id                         TEXT PRIMARY KEY,
    command_id                 TEXT NOT NULL,
    command_name               TEXT NOT NULL,
    provider_kind              TEXT NOT NULL, -- builtin | plugin
    plugin_id                  TEXT NOT NULL DEFAULT '',
    origin_key                 TEXT NOT NULL,
    source_type                TEXT NOT NULL,
    session_id                 TEXT NOT NULL,
    persona_key                TEXT NOT NULL,
    actor_id                   TEXT NOT NULL DEFAULT '',
    actor_role                 TEXT NOT NULL DEFAULT 'member',
    input_hash                 TEXT NOT NULL DEFAULT '',
    argv_json                  TEXT NOT NULL DEFAULT '[]',
    flags_json                 TEXT NOT NULL DEFAULT '{}',
    output_mode                TEXT NOT NULL DEFAULT 'direct',
    status                     TEXT NOT NULL, -- success | failed | denied | needs_confirmation
    result_text                TEXT NOT NULL DEFAULT '',
    payload_json               TEXT NOT NULL DEFAULT '{}',
    error_kind                 TEXT NOT NULL DEFAULT '',
    duration_ms                INTEGER NOT NULL DEFAULT 0,
    created_at                 TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_command_invocations_session_time
    ON command_invocations(session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_command_invocations_origin_time
    ON command_invocations(origin_key, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_command_invocations_command_time
    ON command_invocations(command_id, created_at DESC);
```

---

## 5. Context reset barrier

### 5.1 新增 ContextState 字段

在 `internal/context/types.go` 中给 `ContextState` 增加可选字段：

```go
type ContextResetBarrier struct {
    Epoch          int    `json:"epoch,omitempty"`
    AfterMessageID string `json:"after_message_id,omitempty"`
    ResetAt        string `json:"reset_at,omitempty"`
    Reason         string `json:"reason,omitempty"`
}

type ContextState struct {
    ...existing fields...
    ResetBarrier *ContextResetBarrier `json:"reset_barrier,omitempty"`
}
```

### 5.2 /reset 操作

`/reset` 应：

```text
1. 读取当前 session 最新 message id。
2. LoadSessionState。
3. 创建 defaultContextState。
4. 设置 ResetBarrier:
   epoch = previous_epoch + 1
   after_message_id = latest message id
   reset_at = now
   reason = command_reset
5. 清空 running_summary / prompt_route / summary failure state / last_context_stats。
6. UpdateSessionContextState。
7. memory bridge RolloverSegment(sessionID, personaKey, "command_reset")。
8. 记录 command_invocation + conversation_event。
```

### 5.3 Engine 必须过滤 reset 前历史

当前 Engine 会用：

```go
history, err := e.db.GetAllMessages(ctx, sessionID)
```

需要在加载 `ContextState` 后调用：

```go
historyForLLM := contextutil.FilterHistoryAfterResetBarrier(history, state.ResetBarrier)
```

该过滤结果用于：

```text
running summary update
BuildEmotionContext
prompt route decision
LLM request messages
context stats raw history estimate
```

但 REST 历史接口不受 reset barrier 影响；用户仍能看到旧消息，除非执行 `/clear`。

### 5.4 /clear 与 reset 的区别

```text
/reset 改 LLM context boundary，不改可见历史。
/clear 改 UI visible history marker，不改 LLM context boundary。
```

这两个命令不能互相替代。

---

## 6. Command Core 设计

### 6.1 Descriptor

```go
type CommandDescriptor struct {
    ID              string
    Name            string
    Aliases         []string
    Summary         string
    Usage           string
    Hidden          bool
    Reserved        bool

    ProviderKind    string // builtin | plugin
    PluginID        string

    Permission      CommandPermission // everyone | member | admin | owner
    Scope           CommandScope       // origin | session | global | admin
    Capabilities    []string

    Args            []CommandArgSpec
    OutputMode      CommandOutputMode  // direct | llm_synthesize
    LLM             *CommandLLMSynthesisSpec

    TimeoutMS       int
}
```

### 6.2 Output mode

```go
type CommandOutputMode string

const (
    CommandOutputDirect        CommandOutputMode = "direct"
    CommandOutputLLMSynthesize CommandOutputMode = "llm_synthesize"
)
```

Direct 模式：

```text
命令 handler 返回文本 / payload。
Command Core 直接 emit command_result。
不调用 LLM。
不进入普通聊天历史。
```

LLM synthesize 模式：

```text
命令 handler 先返回结构化结果或信息源。
Command Core 构造 CommandSynthesisRequest。
由 Host 使用受控 LLM 调用生成最终 command_result 文本。
仍然不作为普通 user message。
默认 allow_tools=false。
结果作为 command_result 记录。
```

`llm_synthesize` 的 plugin command 必须满足至少一种条件：

```text
- plugin manifest 声明 provider.generate capability，并且用户已授权；或
- command config 明确允许 host-level llm_synthesize for that plugin command。
```

### 6.3 Parser

命令只在以下条件解析：

```text
1. 文本 trim 后以 '/' 开头。
2. 没有 image/file attachments。
3. 第一个 token 是命令名或别名。
4. `\/` 可用于转义普通以 slash 开头的聊天文本。
```

支持：

```text
/name arg1 arg2
/name --flag value
/name --bool
/name "quoted string"
/name 剩余文本作为 greedy 参数
```

不支持时返回 usage error，不进入 LLM。

### 6.4 CommandContext

```go
type CommandContext struct {
    InvocationID string
    Origin       conversation.Origin
    Binding      conversation.Binding
    SessionID    string
    PersonaKey   string
    SourceType   string

    Actor        ActorInfo
    Args         []string
    Flags        map[string]string
    Raw          string

    Conversation ConversationFacade
    Sessions     SessionFacade
    Memory       MemoryCommandFacade
    Work         WorkCommandFacade
    Plugins      PluginCommandFacade
    Outbound     turn.OutboundSink
}
```

### 6.5 CommandResult

```go
type CommandResult struct {
    Status          string // success | failed | denied | needs_confirmation
    Content         string
    Payload         map[string]any
    Events          []turn.OutboundEvent
    ChangedSession  bool
    NewSessionID    string
    PersonaKey      string
    ReloadHistory   bool
    ReloadMemory    bool
    PersistVisible  bool // default true for result, false for command input
    OutputMode      CommandOutputMode
}
```

---

## 7. Built-in commands

### 7.1 `/help`

```text
列出当前 actor 可见且 enabled 的命令。
包括 builtin commands 和已启用插件命令。
不显示 hidden 命令。
```

### 7.2 `/sid`

返回：

```text
origin_key
source_type
channel_type
actor_id
persona_key
current_session_id
binding updated_at
context reset epoch
clear marker
```

不得泄漏敏感外部平台 token 或 raw adapter payload。

### 7.3 `/new`

语义：

```text
1. 停止当前 origin/session active run，best effort。
2. 对旧 session 调用 memory.RolloverSegment(oldSessionID, personaKey, "command_new")。
3. 创建新 chat session: engine.StartSession(ctx, personaKey)。
4. 绑定 origin_key/persona_key -> new_session_id。
5. emit context_switched(reason="new")。
6. 记录 command_result。
```

结果示例：

```text
已切换到新会话。
```

### 7.4 `/switch <session_id>`

语义：

```text
1. 校验 session 存在且 persona 匹配或可推断 persona。
2. 更新 origin binding。
3. 调用 engine.ResumeSession 或 memory.RolloverSegment 以沿用现有 resume 语义。
4. emit context_switched(reason="switch", reload_history=true)。
```

### 7.5 `/reset`

语义：

```text
1. 停止当前 active run，best effort。
2. 写 ContextResetBarrier。
3. 清空 running_summary / prompt_route / last_context_stats。
4. 不删除 messages。
5. 不删除长期记忆。
6. 调用 memory.RolloverSegment(sessionID, personaKey, "command_reset")。
7. emit command_result + context_stats empty/updated。
```

结果示例：

```text
已重置当前会话的 LLM 上下文。聊天记录和长期记忆没有删除。
```

### 7.6 `/clear`

语义：

```text
1. 写 session_clear_markers(origin_key, session_id, after_message_id=latest message)。
2. 不删除 messages。
3. 不更新 sessions.metadata。
4. 不影响 Engine 后续上下文。
5. WebUI reload history 时只显示 clear marker 之后的 messages/events。
```

结果示例：

```text
已清理当前窗口可见历史。上下文和记忆没有改变。
```

### 7.7 `/compact`

语义：

```text
1. 加载当前 ContextState 和 messages after reset barrier。
2. 调用 contextutil.UpdateRunningSummaryWithParamsAndPromptResolver。
3. 写回 sessions.metadata。
4. 不删除消息。
5. 不改变 binding。
6. 返回 summary update report 的安全摘要。
```

如果没有可压缩 delta：

```text
当前没有需要压缩的新上下文。
```

### 7.8 `/forget <target>`

语义：

```text
1. 不把命令文本写入 messages 或 MemoryCore episode。
2. 将 target 交给 Memory / Forget Manager。
3. 若目标明确且 policy 允许，执行 soft/hard/purge/source_redact。
4. 若目标宽泛或危险，返回 needs_confirmation。
```

P1/P2 可先做最小版：

```text
调用现有 MemoryService / MemoryHost 手动遗忘能力或排队 Forget job。
如果底层 Forget Manager 不足，则返回“已识别遗忘请求，但当前实现需要后续 Forget Manager 接入”。
```

但 API/DTO 必须预留完整结果。

### 7.9 `/stop`

语义：

```text
停止当前 origin/session 的 active run，best effort。
```

当前仓库没有全局 active event registry；需要新增：

```text
conversation.RunRegistry
  Register(sessionID/originKey, cancelFunc, kind)
  Stop(originKey/sessionID) count
```

WS handler 在执行普通 LLM turn 时注册 cancel func。`/stop` 取消该 context。对已经 paused 的 Work，可返回“没有正在运行的任务；有 pending approval/decision 请处理”。

---

## 8. Conversation Gateway 与 WS 集成

### 8.1 HandlerOption

给 `internal/chat.Handler` 增加：

```go
func WithConversationGateway(gateway *conversation.Gateway) HandlerOption
func WithCommandService(commands *command.Service) HandlerOption
```

或让 `Gateway` 内含 `CommandService`。

### 8.2 WS bootstrap

`ServeHTTP` 新流程：

```text
1. Accept websocket。
2. Resolve origin from query:
   source=webui default
   origin_key=webui:local:main default
3. Resolve persona:
   active agent persona default
   query persona override for compatibility
4. If query session_id exists:
   validate and bind origin to that session.
5. Else ensure binding:
   if binding exists -> use it
   else bind latest session or create new session
6. Emit session_ready with origin_key/session_id/persona/is_new.
```

### 8.3 Per-message flow

```text
for each WS message:
  if type=message:
    current = gateway.CurrentBinding(origin, persona)
    if CommandService.TryHandle(...):
        emit command_result/context_switched
        continue
    else:
        execute normal chat with current.sessionID

  if type=approval_action:
    current = gateway.CurrentBinding(origin, persona)
    execute approval for current.sessionID
```

### 8.4 WS message types

Add to `web/src/chat/protocol/wsTypes.ts` and `internal/chat.WSMessage`:

```text
Incoming:
  command_result
  context_switched

Outgoing:
  message remains same
```

Suggested backend struct additions:

```go
OriginKey string         `json:"origin_key,omitempty"`
CommandID string         `json:"command_id,omitempty"`
CommandName string       `json:"command_name,omitempty"`
Payload map[string]any   `json:"payload,omitempty"`
ReloadHistory bool       `json:"reload_history,omitempty"`
ReloadMemory bool        `json:"reload_memory,omitempty"`
```

---

## 9. WebUI 改造

### 9.1 ChatApp 行为

当前 `submitMessage` 会先本地加入 user message。需要改为：

```text
如果 composer 文本是 command 且无 attachment：
  不 dispatch ADD_MESSAGE(role=user)。
  可显示 transient pending command indicator。
  发送 WS message。
  等 backend command_result/context_switched。

否则：
  保持普通消息逻辑。
```

### 9.2 Session hook 重命名建议

不要求一次性大重命名，但建议逐步从：

```text
useChatSession
```

演进到：

```text
useConversationBinding
```

内部仍可复用 session API。

### 9.3 URL 策略

主入口：

```text
/chat
```

默认：

```text
origin_key=webui:local:main
```

历史深链兼容：

```text
/chat?session_id=<id>
```

含义：打开时执行一次 switch binding，而不是永久绕开 binding 系统。

### 9.4 Timeline

新增 timeline item：

```ts
type CommandResultTimelineItem = {
  kind: 'command_result';
  id: string;
  commandID: string;
  commandName: string;
  status: string;
  content: string;
  createdAt: string;
};
```

`GET /api/sessions/{id}` 可新增：

```json
{
  "messages": [...],
  "events": [...]
}
```

前端合并排序。P3 前也可以只显示 WS 实时 command_result，不做 reload 恢复；但最终要持久化事件。

### 9.5 SessionSidebar

SessionSidebar 不再代表“当前入口必须先选 Session”，而是历史抽屉。

行为：

```text
New 按钮 -> 执行 /new 或调用 conversation API。
Open session -> 执行 /switch <session_id> 或调用 binding API。
Delete session -> 仍用现有 DELETE /api/sessions/{id}，但如果删除当前 binding，需要自动 /new 或清 binding。
```

---

## 10. Plugin command extension

### 10.1 ManifestV2 扩展

在 `internal/plugin/manifest_v2.go` 增加：

```go
type ManifestV2 struct {
    ...existing...
    Commands []ManifestV2Command `json:"commands,omitempty" yaml:"commands"`
}

type ManifestV2Command struct {
    Name        string   `json:"name" yaml:"name"`
    RootName    string   `json:"root_name,omitempty" yaml:"root_name"`
    Aliases     []string `json:"aliases,omitempty" yaml:"aliases"`
    Summary     string   `json:"summary" yaml:"summary"`
    Usage       string   `json:"usage,omitempty" yaml:"usage"`
    Permission  string   `json:"permission,omitempty" yaml:"permission"`
    Handler     string   `json:"handler" yaml:"handler"`
    OutputMode  string   `json:"output_mode,omitempty" yaml:"output_mode"`
    TimeoutMS   int      `json:"timeout_ms,omitempty" yaml:"timeout_ms"`
}
```

Validation：

```text
name 必须安全：^[a-zA-Z][a-zA-Z0-9_-]{0,63}$
handler 必须非空。
output_mode 默认 direct，只能 direct | llm_synthesize。
root_name 如果存在，不能是 reserved builtin root command。
root_name 冲突则拒绝，P4 不做自动改名。
llm_synthesize 需要 provider.generate capability 或 host config allow。
```

新增 capability：

```go
CapabilityCommandRegister Capability = "command.register"
```

如果不想新增 capability，也可以复用 manifest `commands` 的存在作为静态声明；但建议仍新增 capability，便于 Admin UI 明示授权。

### 10.2 Plugin command canonical ID

```text
builtin.new
builtin.reset
plugin.<plugin_id>.<command_name>
```

默认有效命令名：

```text
/<safe_plugin_id>.<command_name>
```

可选 root alias：

```text
/root_name
```

但 root alias 不能与内置命令、其他 enabled command 冲突。

### 10.3 Runtime invoke

Process plugin JSON-RPC 新增：

```text
invoke_command
```

Host → plugin：

```json
{
  "method": "invoke_command",
  "params": {
    "command_id": "plugin.com.example.weather.weather",
    "handler": "weather.query",
    "args": ["上海"],
    "flags": {},
    "context": {
      "origin_key": "webui:local:main",
      "session_id": "...",
      "persona_key": "default",
      "actor_role": "member"
    }
  }
}
```

Plugin → Host：

```json
{
  "status": "success",
  "content": "上海今天多云。",
  "payload": {},
  "output_mode": "direct"
}
```

插件命令 handler 不得返回 assistant final message；它返回 command result。

### 10.4 Plugin command safety

插件命令只能通过 Facade 访问数据：

```text
memory.safe_context
memory.forget.request
provider.generate
plugin.kv
plugin.files
web.search/fetch if enabled
```

不得给插件：

```text
raw DB handle
raw prompt
raw hidden memory
MemoryCore sidecar direct access
provider API key
```

### 10.5 Command Admin API

新增路由：

```text
GET /api/commands
GET /api/commands/conflicts
PUT /api/commands/{id}
GET /api/commands/invocations?session_id=&origin_key=&limit=
```

P4 可先只实现 list + update enabled/root alias/permission/output_mode。

---

## 11. 分阶段实施计划

## Phase 1: Conversation + Command 基础骨架

目标：新增模块、表、接口和测试，不改变默认 WebUI 行为。

### 任务

1. 新增 `internal/conversation`：
   - `Origin`
   - `Binding`
   - `OriginResolver`
   - `BindingService`
   - `TimelineEventStore`
   - `RunRegistry`

2. 新增 `internal/command`：
   - `CommandDescriptor`
   - `Registry`
   - `Parser`
   - `PermissionChecker`
   - `CommandService`
   - `BuiltinProvider` skeleton

3. 新增 storage migrations：
   - `conversation_origins`
   - `conversation_bindings`
   - `session_clear_markers`
   - `conversation_events`
   - `command_configs`
   - `command_invocations`

4. 在 `internal/app/kernel.go` 增加服务 wiring。

5. 单元测试：
   - origin key validate
   - binding create/update/current
   - command parser quotes/flags/greedy
   - command registry conflict detection
   - plugin root command cannot override builtin reserved names

### 验收

```text
go test ./internal/conversation/... ./internal/command/... ./internal/storage/...
go test ./...
```

默认聊天行为不变。

---

## Phase 2: WS Gateway 集成 + 内置命令

目标：在 WebUI 当前 WS 中可用内置命令。

### 任务

1. `chat.Handler` 接入 `ConversationGateway` 和 `CommandService`。
2. WS bootstrap 使用 `origin_key=webui:local:main`，兼容 `session_id`。
3. 实现内置命令：
   - `/help`
   - `/sid`
   - `/new`
   - `/switch <session_id>`
   - `/reset`
   - `/clear`
   - `/compact`
   - `/forget <target>` minimal
   - `/stop` best effort
4. 新增 WS incoming events：
   - `command_result`
   - `context_switched`
5. 实现 `ContextResetBarrier` 并修改 Engine 历史过滤。
6. `/new` 和 `/reset` 调用 Memory Bridge RolloverSegment。
7. command input 不进入 messages / MemoryCore / running summary。

### 测试

1. `/new`：
   - binding session 变化。
   - old segment finalized or rollover attempted。
   - command input not in messages。
   - command result recorded。

2. `/reset`：
   - messages still exist。
   - sessions.metadata reset barrier epoch increases。
   - Engine LLM history excludes pre-reset messages。
   - long-term memory not deleted。

3. `/clear`：
   - session_clear_marker written。
   - messages still exist。
   - Engine LLM history unchanged。
   - REST detail with origin applies marker。

4. `/compact`：
   - running_summary updated when delta exists。
   - no user message inserted。

5. `/sid`：
   - returns origin/session/persona without secrets。

6. `/stop`：
   - cancels active run in RunRegistry。

### 验收

```text
go test ./internal/chat/... ./internal/app/... ./internal/conversation/... ./internal/command/...
go test ./...
```

---

## Phase 3: WebUI 主入口改造

目标：ChatUI 从显式 Session 入口改成统一聊天入口，SessionSidebar 变成历史抽屉。

### 任务

1. `useChatWebSocket`：
   - 默认 URL 使用 `/ws?origin_key=webui:local:main`。
   - 保留 session_id 深链 bootstrap switch。

2. `submitMessage`：
   - 若文本为 command 且无 attachments，不先 ADD_MESSAGE(role=user)。
   - 等待 `command_result` / `context_switched`。

3. `wsTypes.ts`：
   - 增加 `command_result`、`context_switched`。

4. Chat reducer：
   - 支持 `ADD_COMMAND_RESULT`。
   - context_switched 后 reload history / memory / approvals。

5. Session API：
   - `loadSessionDetail(id, originKey?)` 支持 events 与 clear marker。

6. SessionSidebar：
   - New -> `/new` 或 conversation API。
   - Open -> `/switch <session_id>` 或 binding API。

### 验收

```text
npm --prefix web run build
go test ./...
```

手动验证：

```text
打开 /chat 继续 webui:local:main current session。
输入 /sid 不出现 user bubble，只出现 command result。
输入 /new 切到新会话并清空当前 timeline。
输入 /reset 后旧聊天记录仍可见，后续 LLM 不引用 reset 前消息。
输入 /clear 后当前窗口历史清空，但 /sid 仍显示同一 session。
刷新页面后继续当前 binding。
```

---

## Phase 4: Plugin command extension

目标：插件可声明和执行命令；默认直接返回；可选 LLM synthesis。

### 任务

1. 扩展 `plugin.ManifestV2`：
   - `commands[]`
   - validation
   - reserved conflict rejection

2. 新增 capability：
   - `command.register`

3. PluginService load enabled plugins 时收集 command descriptors。
4. `CommandService` 注册 plugin descriptors。
5. Process runtime 增加 `invoke_command` JSON-RPC。
6. Builtin plugin runner 增加 command handler 注册 facade，若需要。
7. 实现 direct output。
8. 实现 `output_mode=llm_synthesize`：
   - provider.generate capability / host grant check。
   - no tools by default。
   - result still command_result。
9. Admin API：
   - list commands
   - update command config
   - list invocations
   - conflict diagnostics

### 测试

1. Plugin command manifest validation。
2. Plugin command cannot register `/new`, `/reset`, `/clear`, `/sid` etc.
3. Direct plugin command emits command_result and does not call LLM。
4. llm_synthesize plugin command calls fake LLM exactly once and emits command_result。
5. Missing capability denies llm_synthesize。
6. Disabled plugin command unavailable。
7. Plugin command invocation audited。

### 验收

```text
go test ./internal/plugin/... ./internal/command/... ./internal/app/...
go test ./...
```

---

## 12. Reserved root commands

内置保留根命令：

```text
help
sid
new
switch
reset
clear
compact
forget
stop
set
unset
plugin
plugins
provider
model
memory
config
admin
```

插件命令：

```text
- 不能覆盖 reserved roots。
- 不能与 enabled builtin command 冲突。
- P4 不做自动改名；冲突直接 validation error。
- 未来可做 admin rename / alias resolution。
```

---

## 13. Failure policy

### 13.1 Command failure

命令失败不进入普通 LLM：

```json
{
  "type": "command_result",
  "status": "failed",
  "content": "命令执行失败：...",
  "error_kind": "validation_error|permission_denied|plugin_failed|internal_error"
}
```

### 13.2 Plugin failure

```text
direct mode plugin timeout -> failed command_result。
llm_synthesize plugin handler failed -> 不调用 LLM，直接 failed。
LLM synthesis failed -> failed command_result，记录 provider usage if applicable。
```

### 13.3 Memory rollover failure

`/new` 和 `/reset` 的 Memory segment rollover 失败时：

```text
不要阻止 session/context 操作。
返回 warning payload。
写 command_invocations.status=success_with_warning 或 payload.warnings。
记录 logger warning。
```

原因：MemoryCore 是增强依赖，聊天入口不应因 segment finalize 失败完全不可用。

---

## 14. Security and privacy invariants

1. Command input is never persisted as a normal user message.
2. Command input is never appended to MemoryCore episodes.
3. Command results are not ordinary assistant messages unless explicitly filtered from LLM context.
4. Plugins cannot override builtin root commands.
5. Plugin commands cannot access raw DB / raw MemoryCore / provider API keys.
6. Plugin command LLM synthesis requires explicit capability/grant.
7. `/forget` must not convert deletion intent into a normal user fact.
8. `/clear` is presentation-only and must not imply deletion.
9. `/reset` is context-only and must not imply memory deletion.
10. `/new` creates a new Session and finalizes/rolls over old Memory segment best effort.
11. Future NapCat group messages default to group-shared origin; private messages default to private origin.

---

## 15. Files likely touched

Backend:

```text
internal/app/kernel.go
internal/app/server.go
internal/app/chat_service.go
internal/chat/handler.go
internal/chat/adapter_ws.go
internal/chat/turn_runtime.go               -- only if adding event constants or result mapping
internal/turn/contract.go                   -- optional InboundSource expansion
internal/turn/stream.go                     -- command_result/context_switched constants optional
internal/context/types.go
internal/context/summary.go
internal/chat/engine.go                     -- filter history after reset barrier
internal/storage/db.go
internal/storage/schema.go or migration files
internal/plugin/types.go
internal/plugin/manifest_v2.go
internal/plugin/registrar.go
internal/app/plugin_service.go
internal/plugin/runtime_supervisor.go / process adapter files
```

New backend packages:

```text
internal/conversation/...
internal/command/...
```

Frontend:

```text
web/src/chat/ChatApp.tsx
web/src/chat/hooks/useChatSession.ts
web/src/chat/hooks/useChatWebSocket.ts
web/src/chat/protocol/wsTypes.ts
web/src/chat/protocol/sessionApi.ts
web/src/chat/state/chatTypes.ts
web/src/chat/state/chatReducer.ts
web/src/chat/components/VirtualTimeline.tsx
web/src/chat/components/SessionSidebar.tsx
```

Docs:

```text
docs/architecture/unified_chat_command_core_codex_spec.md
```

---

## 16. Implementation notes for Codex

1. Prefer additive migrations and backward-compatible metadata fields.
2. Do not rewrite MemoryCore integration.
3. Do not rewrite plugin runtime; extend manifest and invocation path only.
4. Keep built-in Command Core independent of plugins; built-in commands must work when `plugins.enabled=false`.
5. Keep existing `/api/sessions` APIs compatible; add optional origin-aware behavior rather than breaking current callers.
6. Keep `/ws?session_id=` compatibility for deep links.
7. Add tests before broad refactors; avoid large untested frontend/backend simultaneous changes.
8. Run `go test ./...` after every phase; run `npm --prefix web run build` after Phase 3.

---

## 17. Minimal success definition

After P1-P4, the following must be true:

```text
/chat opens the unified WebUI entry and resumes webui:local:main current Session.
/new creates a new Session and changes current binding.
/reset resets LLM context boundary without deleting messages or long-term memory.
/clear hides visible history for current WebUI origin/session without changing LLM context.
/compact updates running summary on demand.
/forget routes to memory forget flow without persisting command text.
/sid shows current origin/session/persona.
Command input is not saved as user message.
Command result is recorded and shown as command_result.
Plugin commands can be declared, invoked, audited, and cannot override built-in roots.
Plugin command direct output does not call LLM.
Plugin command llm_synthesize output calls LLM only when configured and authorized.
No NapCat adapter is implemented in this batch.
```
