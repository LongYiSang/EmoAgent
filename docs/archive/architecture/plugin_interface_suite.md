# Plugin Suite Phase 1 Runtime Contract

本文记录 EmoAgent Plugin Suite 第一阶段的实现边界。旧 `MessagePipeRefactor` 文档只作为历史背景；当前以 `docs/todo/PluginSuite_Design.md` 和本决策记录为准。

## 术语基线

- 消息流术语只使用 `Message` / `Turn` / `Session` / `InboundEnvelope`。
- 固定术语：`Plugin Host Runtime Contract`、`Plugin Suite`、`HookBus`、`Facade`、`Capability`。
- 插件不是新的对话者，也不能绕过 Emotion 直接生成 assistant final text。

## Phase 1 范围

Phase 1 覆盖 `PluginHost v0.1 + Tool Registration MVP + Memory/Work Advanced Hook Contract`：

- `internal/plugin` 提供 `Manifest`、`Capability`、`PluginRegistry` 等效注册入口、`HookBus`、`PluginHost`、`BuiltinRunner`、TurnJournal 最小审计、typed safe views。
- `PluginHost` 由 App lifecycle 创建和关闭，通过 `HandlerOption / chatTurnRuntime` 注入，只包装 Turn stages 和 `OutboundSink`。
- `turn.Runtime` 保持纯净，不依赖 plugin 包。
- 插件 Hook 只在 Turn Pipeline 路径生效；`plugins.enabled=true` 时，目标 `Session` / persona 必须命中 `chat.turn_pipeline`。
- 当前 MVP Hook 包括 normalize、memory_prepare、memory_retrieve、outbound、turn_error、turn_end。
- Tool Registration MVP 只允许插件注册 namespaced 工具，推荐 `plugin.<plugin_id>.<tool_name>`；注册走 `TryRegister`，重名返回 error，不覆盖内置工具。
- 插件工具仍走 `tool.Dispatcher`、`tool.Permission`、`ApprovalClassifier` 和既有 approval gate。
- Memory/Work 高级 Hook 契约先落 DTO、Capability 和安全 stub：`memory.candidate.submit`、`memory.forget.request`、`work.dispatch.annotate`、`on_decision_packet`、`on_approval_requested`、`on_approval_resolved`、`on_approval_consumed`。

## Safe View 边界

所有插件输入只能来自 typed safe views：

- `TurnView`：Turn、Session、persona、request 等标识和安全摘要。
- `MemoryView`：authority-filtered safe DTO，只能包含安全摘要、类型、usage guidance、授权 `NodeRef` 和安全 diagnostics。
- `ToolCallView`：工具名、call id、schema/permission 摘要、输入 hash/bytes，不暴露 raw tool output。
- `WorkView`：Task/Decision/Approval 的安全摘要。
- `OutboundView`：outbound 类型、内容 hash/bytes、approval/tool 安全摘要。

禁止暴露 raw prompt、raw tool output、reasoning content、hidden/forgotten/purged memory、MemoryCore sidecar 直连。

## HookBus 与审计

`HookBus` v0.1 固定以下行为：

- 按 priority 升序执行，同 priority 按 plugin id 排序。
- 支持 per-hook timeout、fail-open / fail-closed。
- panic recovery 必须转成 `plugin_hook_failed`。
- patch merge 只接受安全类型；低优先级 replace 冲突转成 `plugin_patch_conflict` 并审计。
- 每次 invocation 审计字段至少包含 `plugin_id`、`invocation_id`、`hook`、`status`、`duration_ms`、`error_kind`。
- MVP 先写 TurnJournal 的 `plugin_invocation` 记录；独立 Plugin Audit Log / table 是保留设计，后续用于跨 Turn 查询、状态页和故障分析。

duplicate replay 不重新执行 observe / side-effect Hook，只重放已经落盘的 outbound / audit 结果。

## Outbound 一致性

`canonical_assistant_content` 是 DB Message、Memory assistant episode、Turn outbound journal、WS final text 的唯一权威。

当前实现无法在实时 token streaming 与现有 DB/Memory commit 顺序下证明文本变换一致，因此 v0.1 禁用 `outbound.decorate_text` 的文本修改，只允许 payload/debug patch 写入 `Payload.plugins[plugin_id]` 命名空间。

## Capability 与权限

`Capability` 是 Plugin Host 外层授权；`tool.Permission / ApprovalClassifier` 是工具执行授权；Work `permission_scope` 是当前任务授权范围。最终允许条件必须三者同时满足，最保守者胜出。

插件不能通过 `Capability` 抬高 `permission_scope`，也不能通过 `permission_scope` 绕过 `Capability`。

`memory.forget.request` 只表达请求。`hard_forget`、`source_redact`、`purge` 等 destructive level 需要额外 Capability，最终仍由 Forget Manager / approval 决定。

## Alpha 安全边界

以下内容不在 Phase 1 实现范围内，但边界现在固定：

- 不实现 process runner；后续 runner 必须由 `PluginHost.Start/Stop` 管理，并先定义网络、文件系统、stdout/stderr、资源预算和 Windows 进程清理边界。
- 不允许任意第三方前端页面或任意 dashboard JS；Phase 1 只允许后端内置页面或 Host 生成的 schema-driven panel。
- 不实现插件签名和自动依赖安装；第三方插件开放前必须先有签名、依赖锁定、运行隔离和审核策略。
- process 插件即使未来允许 loopback network，也不能直连 MemoryCore sidecar；插件只能通过 Host `Facade`。
