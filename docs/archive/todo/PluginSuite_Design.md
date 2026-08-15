## Implementation Alignment Decisions

本节为实施前决策记录，优先级高于早期 MessagePipeRefactor 文档。

## 实施前的决策记录：
1. 第一版范围改为 **PluginHost v0.1 + Tool Registration MVP + Memory/Work Advanced Hook Contract**。
2. PluginHost 不注入 `turn.Runtime`，只由 App lifecycle 创建，由 `chatTurnRuntime` 包装 stages/sinks。
3. 插件 Hook 只在 Turn Pipeline 生效；`plugins.enabled=true` 时要求 Turn Pipeline 对目标 session/persona 生效。
4. 所有插件可见对象必须是 typed safe views，不暴露 raw `TurnContext`。
5. `outbound.decorate_text` 必须保证 DB / Memory / Journal / WS 一致；无法保证时禁用实时文本变换。
6. 插件工具使用 namespaced name 和 `TryRegister`，不得 panic，不得覆盖内置工具。
7. Memory 插件只能提交 candidate / request，不能直接写 facts 或执行 purge。
8. 审计采用 TurnJournal + Plugin Audit Log 双层设计。
9. process runner 仍不是 v0.1 实现项，但 Alpha 前必须固定安全边界。

## 具体决策：

当前决策基线仍然是：Emotion 始终是唯一对话者，Work 只在幕后执行；工具噪音不得污染主上下文；最终表达归 Emotion 统一组织。这个原则已写入项目 README 的会话所有权、上下文隔离、记忆边界和表达控制约束。 代码侧，Turn 已有 `InboundEnvelope / TurnState / StageName / TurnContext / StageResult / OutboundEvent` 等稳定契约，可以作为插件 Hook 的挂载主干。  

---

# 插件套件实施前决策记录

## A. 范围与权威文档

| 问题 | 决策                                                                                                                                                                                                                                         |
| -: | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
|  1 | `MessagePipeRefactor.md` 视为旧阶段文档。当前实施以新的 Plugin Suite 设计文档和本决策记录为准。旧文档只作为历史背景，不作为边界约束。                                                                                                                                                     |
|  2 | `Message / Turn / Session / InboundEnvelope` 以当前 `CONTEXT.md` 和代码中的 Turn 契约为唯一消息流术语基线。后续插件文档不得再引入“会话轮次 / 请求 / 事件”等平行术语替代它们。                                                                                                                |
|  3 | 固定术语：`Plugin Host Runtime Contract`、`Plugin Suite`、`HookBus`、`Facade`、`Capability` 进入正式术语表。实现、测试、配置、文档统一使用这些名称。                                                                                                                            |
|  4 | 第一版范围扩大：不是只做推荐 MVP，而是会跨到 Milestone 6/7 的**工具注册、Memory 高级 Hook、Work 高级 Hook 契约与最小实现**。但仍不做第三方进程插件市场、任意前端页面、插件签名和自动依赖安装。                                                                                                                     |
|  5 | 插件不能成为新的对话者。用户可见输出分两类：一是 Emotion 生成或确认后的 assistant final text；二是明确标记为系统/UI 状态的非人格事件。`outbound.decorate` 只能修饰 Emotion 已生成的最终文本；`command.register` 只能返回命令结果给 Emotion 或 UI 系统事件；`on_websocket_event` 只能处理 UI 控制/调试 payload，不能伪造 assistant 回复。 |

---

## B. Turn 与 Hook

| 问题 | 决策                                                                                                                                                                                                                                                                                                              |
| -: | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
|  6 | Hook 名称绑定**稳定语义阶段**，不直接绑定当前代码里的所有实际 stage 名。当前代码没有 `ingress / session_bind / outbound_commit / done` 不影响设计；实现时用映射层把现有 `normalize / memory_prepare / emotion_prepare / message / memory_commit / approval_apply / resume` 映射到公开 Hook。                                                                            |
|  7 | 插件 Hook 只保证在 Turn Pipeline 路径生效。若 `shouldUseTurnPipeline` 让请求走旧 `SendMessage / ContinueAfterApproval`，插件不保证运行。`plugins.enabled=true` 时必须要求对应 session/persona 进入 Turn Pipeline；否则启动或运行期给出明确 warning / config validation error。                                                                                   |
|  8 | `PluginHost` 生命周期由 App 创建和关闭；注入点在 `HandlerOption / chatTurnRuntime`；执行点在 `chatTurnRuntime` 包装 stages 和 outbound sink。`turn.Runtime` 保持纯净，不依赖 plugin 包。                                                                                                                                                          |
|  9 | `before_ingress_normalize` 不允许修改 `EnvelopeID / IdempotencyKey / StartTurn`。这些属于 Turn identity 和审计锚点，必须在插件前稳定生成。未来若需要平台 adapter 层预处理，另设 `before_inbound_envelope`，不纳入当前版本。                                                                                                                                       |
| 10 | 实施前必须定义 typed plugin views。插件永远不能拿原始 `TurnContext`；只能拿 `TurnView / MemoryView / ToolCallView / WorkView / OutboundView` 等受限 DTO。                                                                                                                                                                                |
| 11 | `before_memory_prepare` 和 `after_memory_prepare` 先放在 `prepareInputAndMemoryAnchor` helper 外部，不拆 helper 内部事务。`before_memory_prepare` 只能观察和标注，不改 user content；`after_memory_prepare` 返回 episode/segment 的安全引用、manual notice 状态和安全摘要。                                                                              |
| 12 | manual pin/forget notice 短路 LLM 时仍触发：`normalize`、`before_memory_prepare`、`after_memory_prepare`、`on_forget_requested / on_memory_candidate`、`before_outbound`、`after_outbound`、`after_turn_end`、`on_turn_error`。不触发：`before_memory_retrieve`、`after_memory_retrieve`、`before_llm_request`、`after_llm_response`。 |
| 13 | fail-closed Hook 错误统一映射为 terminal failure：`TurnState=failed`、`TurnResult.Status=failed`、`ErrorKind=plugin_hook_failed`，并细分 reason：`plugin_hook_timeout`、`plugin_capability_denied`、`plugin_patch_conflict`、`plugin_policy_violation`。如果 Hook 语义是审批等待，则进入现有 `approval_wait`，不是 failed。                           |
| 14 | Patch 冲突规则现在定死：低数字 priority 先执行；同 priority 按 plugin id 排序；append 类 patch 去重追加；replace 类 patch 只允许最高优先级一次生效；安全类 patch 取最保守结果；冲突的低优先级 patch 被拒绝并审计。禁止插件返回完整对象覆盖 `TurnContext / ChatRequest / TaskBrief / MemorySnapshot`。                                                                                         |

Turn Runtime 当前已经负责按 stage 顺序执行、记录 transition 和 metrics；插件 wrapper 应复用这个执行与审计模型，而不是改写 runtime 主循环。

---

## C. Outbound 与审计

| 问题 | 决策                                                                                                                                                                                                                                                           |
| -: | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 15 | `before_outbound / after_outbound` 观察的是统一 `turn.OutboundEvent`，不是最终 WS 私有结构。实现上通过包装 `OutboundSink` 覆盖 `StageResult.Outbound` 和直接 `tc.Stream.Emit` 两种路径。                                                                                                      |
| 16 | assistant final text 的唯一权威是 `canonical_assistant_content`：即 LLM 输出经过允许的 outbound text patch 后形成的最终内容。DB message、Memory assistant episode、Turn outbound journal、WS 内容必须以它为准。若实时 token streaming 无法保证一致性，启用文本变换插件时必须缓冲最终文本，或者禁用该类文本变换，只允许 payload/debug patch。 |
| 17 | `outbound.add_payload / outbound.emit.safe_debug` 只允许写入 `WSMessage.Payload.plugins[plugin_id]` 命名空间，字段必须是安全 JSON。默认不写入 message metadata；如需持久化，只能写 `plugin_annotations` 的安全摘要。禁止修改 `Tool / Reasoning / Approval` 字段。                                          |
| 18 | 审计两层都要有：TurnJournal 记录 turn-local 最小审计；独立 Plugin Audit Log / table 负责跨 turn 查询、生命周期、状态页和故障分析。MVP 可以先落 TurnJournal，Milestone 后补专用表，但设计上按“两者都有”推进。                                                                                                             |
| 19 | 在现有 `turn_events` 没有专用列时，先把 `plugin_id / invocation_id / hook / status / duration_ms / capability / error_kind` 放入 sanitized payload。后续专用表使用这些字段作为列。                                                                                                         |
| 20 | Hook invocation 幂等状态放在 PluginHost 的 InvocationStore。key 为 `turn_id + plugin_id + hook + stage + seq + input_hash`。有副作用的 Hook 必须持久化 invocation 状态；无副作用 Hook 可只审计。                                                                                             |
| 21 | duplicate replay 时不重新执行 observe / side-effect Hook，只重放已落盘 outbound 和审计结果。若缺少可重放审计，返回 replayed 状态，不补跑插件副作用。                                                                                                                                                   |

当前 TurnJournal 已有事件记录和 outbound 记录机制，适合作为最小审计落点。 现有幂等模型只覆盖 inbound turn key，因此插件副作用必须另建 invocation 级幂等。

---

## D. Tool / Work / Approval

| 问题 | 决策                                                                                                                                                                                                             |
| -: | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 22 | `before_tool_call / after_tool_call` 最终同时覆盖 Emotion 工具和 Work 工具。`ToolCallView` 必须携带 `agent_scope=emotion/work`。插件 capability 可限制只观察 Emotion 或 Work。                                                            |
| 23 | Tool Hook 包在 Dispatcher 边界，而不是 LLM tool loop 外层。流程：基础 registry/schema 预校验 → 构造 ToolCallView → `before_tool_call` → 合并 plugin decision patch → `ClassifyCall / Execute` → `after_tool_call`。                    |
| 24 | 插件工具必须 namespaced，推荐 `plugin.<plugin_id>.<tool_name>`。新增 `TryRegister` 返回 error，插件路径不能使用当前会 panic 的注册方式。重名时插件加载失败，绝不覆盖内置工具。                                                                                    |
| 25 | 插件停用/卸载后，未来 LLM 请求不再暴露其 tool name；inflight turn 进入 draining；pending approval 若绑定已卸载插件，审批后返回 stale / expired / plugin_unavailable，不能执行旧 handler。                                                                |
| 26 | 权限优先级为：Capability 是 Host 外层授权；`tool.Permission / ApprovalClassifier` 是工具执行授权；`permission_scope` 是当前任务授予范围。最终允许条件是三者同时满足，最保守者胜出。Capability 不能抬高 `permission_scope`，`permission_scope` 也不能绕过 Capability。         |
| 27 | `tool.require_approval` 映射到 `CallActionToolApprovalRequired`，并生成/复用现有 approval request / DecisionPacket 路径。`tool.downgrade_permission` 语义改为“为当前 call 设置更低 max_permission 上限”，只会更保守，不能把 destructive 改成 safe。    |
| 28 | `work.dispatch.annotate` 作用于 TaskBrief draft，并在 `ValidateAndComplete` 前合并，但只能追加 `constraints / acceptance hints / background hints`。禁止修改 `TaskID / Goal / PermissionScope / ReadScope`。合并后统一校验；非法 patch 丢弃并审计。 |
| 29 | `on_decision_packet` 在 Work packet 校验、风险归一化、pending/approval 对象创建后触发。它是 observe hook，不改变路由。若未来要改变路由，另设 `before_decision_route`。                                                                                |
| 30 | Approval lifecycle 必须拆三类 Hook：`on_approval_requested`、`on_approval_resolved`、`on_approval_consumed`。resolved 表示用户 approve/reject/expire；consumed 表示 resume 已消费 approval。                                       |
| 31 | 关联关系由 Chat Turn Orchestrator 维护 `CorrelationContext`：`turn_id ↔ task_id ↔ approval_request_id`。Work journal 保存 Work 细节；TurnJournal 保存跨系统关联事件；PluginHost 只读取 correlation view，不成为关联权威。                          |

当前 Work 协议已有 `TaskBrief / TaskReport`，升级路径已有 `DecisionPacket / ApprovalRequest`，插件应挂在这些协议对象周围，而不是发明旁路 Work 协议。   现有工具系统已有 scope、permission、approval classifier 和 fail-closed Dispatcher，插件工具必须复用这套机制。  

---

## E. Memory

| 问题 | 决策                                                                                                                                                                                                                                                                 |
| -: | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 32 | `safe MemoryView` 是新的插件 DTO，不是原始 `MemoryContext.Blocks`，不是格式化后的 prompt block，也不是完整 `PipelineTrace`。它从权威过滤后的检索结果派生，只含安全摘要、block 类型、usage guidance、可选 node ref 和安全 diagnostics。                                                                                      |
| 33 | `after_memory_retrieve` 发生在 SQLite authority filtering、final ranking、context reconstruction 之后，prompt assembly 之前。也就是：插件看到的是“可进入 Prompt 的候选安全上下文”，但还没有被写进 `assembled.System`。                                                                                      |
| 34 | 插件默认不能看到 `metadata.memory_pipeline.prompt_block`。`memory.read.safe` 只能看 `MemoryView`。若未来开放 debug，只能通过单独 `memory.debug.pipeline_trace` capability，并且返回脱敏、截断、无 raw prompt 的 debug view。                                                                              |
| 35 | `MemoryView` 可以携带 `NodeRef{node_type,node_id}`，但只对已通过授权过滤的节点开放，且不进入用户可见输出。forget preview / exact-node request 可以使用它；向用户展示时必须转成安全摘要。                                                                                                                                |
| 36 | `memory.candidate.submit` 提交的是 `PluginMemoryCandidate` 队列对象，不是 fact、episode 或 extraction job。它包含 summary、evidence_refs、candidate_type、confidence、sensitivity_hint、plugin_id，由 Memory Orchestrator 再路由到 extraction/consolidation。                                   |
| 37 | `forget_request` 必须持久化、可恢复。当前内存态 `pendingForgets` 只能作为过渡。插件级 `memory.forget.request` 不应在持久 store 落地前对第三方插件开放。                                                                                                                                                      |
| 38 | 插件可以表达 requested level：`soft_forget / hard_forget / source_redact / purge`，但只是请求。默认 level 是 `soft_forget`；hard/source/purge 需要额外 destructive memory capability 和 Emotion/User approval；最终级别由 Forget Manager 决定。                                                    |
| 39 | `after_memory_commit` 在 Turn Hook 中只覆盖同步 conversation commit，也就是 assistant episode / segment commit。async extraction job、mirror sync、retention job 使用独立未来 Hook：`on_memory_extraction_completed`、`on_memory_mirror_sync_completed`、`on_memory_retention_completed`。 |
| 40 | 插件可见 extraction/mirror payload 只允许：状态、计数、node refs、safe summary、hash、reason_code、score breakdown。禁止：raw episode content、extraction_reasoning、embedding vector、Trivium raw payload、prompt block、hidden/purged 内容。                                                   |
| 41 | `before_memory_retrieve` 的 query hint 必须继承 current user episode exclusion 规则。插件 hint 只能走 RetrievalRequest builder，不能直接拼检索请求，也不能让当前 user episode 被自身召回。                                                                                                             |
| 42 | `memory.read.safe` 受当前 retrieval policy 约束，默认只能看到 `SensitivityNormal` 允许的结果。`allow_sensitive_extraction` 是写入/抽取配置，不是读取授权，不能让插件读取 sensitive memory。                                                                                                                 |
| 43 | 进程插件即使允许 `network: loopback`，也不能直接访问 MemoryCore sidecar。MemoryCore / sidecar 是 Host 内部依赖，插件只能通过 Host Facade。`loopback` 只允许访问 manifest allowlist 中的插件自有本地服务。                                                                                                        |

MemoryCore / MemoryGraph 的原则已经明确：SQLite 是权威记忆库，检索镜像、sidecar、reranker、插件都只能提供候选或辅助信号，不能绕过权威过滤。这个原则应作为插件 Memory Facade 的硬边界。

---

## F. 配置、生命周期、UI

| 问题 | 决策                                                                                                                                                                                                        |
| -: | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 44 | `plugins` 配置两层都有：主 `config.yaml` 放静态安全边界、目录、默认启用策略；runtime config / DB 放启停、persona/session override、per-plugin config、status。Manifest 只声明插件自身能力和默认配置 schema。                                              |
| 45 | Manifest 必须严格拒绝未知字段和拼写错误。主配置中的 `plugins` 子树也应尽量 strict；如果现有 config loader 不支持全局 strict，至少对 plugin subtree 做严格校验。                                                                                          |
| 46 | `chat.turn_pipeline.rollout_percent` 和 `plugins.rollout_percent` 是两套灰度。有效插件运行条件为：Turn Pipeline 命中 + Plugin rollout 命中 + plugin enabled + capability authorized。                                           |
| 47 | `plugin directories` 相对路径基准为 config 文件目录；manifest 内路径相对插件目录；runtime `working_dir` 默认插件目录；插件 data dir 使用 app data 下的专属目录。不要使用 cwd 作为隐式基准。                                                                  |
| 48 | builtin plugin 也必须提供 Manifest，但可以是 Go struct，不要求 YAML 文件。builtin 和 process plugin 走同一套 manifest validation、capability、hook registration。                                                                  |
| 49 | process runner 的 start/health/reload/shutdown 归 PluginHost 管；RunnerManager 是 PluginHost 子组件。App lifecycle 只调用 `PluginHost.Start/Stop`。                                                                    |
| 50 | hot reload 的 draining / inflight turn 状态由 PluginHost + HookBus 维护。每个插件有 generation；旧 generation draining，新 generation 接新 invocation。                                                                      |
| 51 | per-plugin config、status、audit、persona/session override 持久化到 runtime DB / config_runtime 或专用 plugin 表；manifest-local 文件只读，不写运行时状态。                                                                        |
| 52 | WebUI 插件配置、状态、audit query 使用新增 `/api/plugins/*` namespace。现有 settings API 可以展示入口，但不承载插件管理协议。                                                                                                              |
| 53 | `on_command / on_websocket_event` 需要扩展 WS adapter：把允许的 client payload 放入 `InboundEnvelope.RawMeta["ws"]`；normalize 后的命令解析结果放入 `RawMeta["command"]`。未声明 schema 的任意 payload 不进入 RawMeta。                    |
| 54 | v0.1 前端不允许第三方插件注册任意自定义 WS event 或任意 dashboard JS page。只允许后端内置页面 / Host 生成的 schema-driven panel。第三方页面等 process runner、CSP、权限和签名策略稳定后再开。                                                                    |
| 55 | `compatibility.memorycore >=x.y.z` 需要 HostInfo 提供 MemoryCore version。当前 `go.mod replace v0.0.0` 无法可靠判断时，第三方插件若声明具体 memorycore range 默认不兼容；可用 dev override 临时放行。后续应让 MemoryCore 暴露 build/version。          |
| 56 | process runner Alpha 前必须先定义网络、文件系统、stdout/stderr、资源预算和 Windows 进程清理边界。默认：network none，filesystem plugin_data only，stdout/stderr 限流，env whitelist，Windows 使用 job object 或等价进程树清理。                          |
| 57 | degraded / circuit-open 状态运行时存在 PluginHost 内存，并把 last failure 持久化用于诊断。cooldown 到期 + health check 成功后恢复。默认不跨 app restart 保留 open 状态，但连续启动失败会禁用插件。                                                          |
| 58 | `audit.include_payload` 的安全过滤独立定义为 Plugin Audit Sanitizer，但复用 TurnJournal 的敏感字段红线。默认 false。开启后也只记录脱敏摘要、hash、计数、reason_code，不记录 raw prompt、raw tool output、reasoning、hidden/purged memory。查询默认 admin-only。 |

---



## 0. 总体结论

EmoAgent 现在已经具备设计插件接口套件的合适时机，但**第一版不应直接做“第三方插件随意改消息流”**，而应先把插件系统做成一层稳定的 **Plugin Host Runtime Contract**：插件只能通过 Manifest 声明身份、版本、Hook、Capability、资源预算和运行方式；运行时只暴露受限 Facade；所有 Hook 都挂在现有 Turn Pipeline、Tool Dispatcher、Work Runtime、Memory Bridge 的稳定边界上。

这个方向与报告中的“插件接口套件设计”一致：学习 AstrBot 的统一事件 / Hook 平台能力，但不要继承同进程共享依赖、权限弱边界的问题；EmoAgent 应直接走 **Manifest + Typed Hook + Capability 权限 + 隔离运行优先 + 热加载/灰度启停** 的路线。

当前仓库的关键事实是：EmoAgent 的产品原则已经明确要求 Emotion 始终拥有会话、Work 只在幕后执行、执行噪音不得污染主上下文、最终表达由 Emotion 统一组织。插件系统必须服务这个原则，而不是变成第二套旁路会话系统。

---

## 1. 当前代码现状判断

### 1.1 Turn Pipeline 已经可以成为插件 Hook 主干

`internal/turn/contract.go` 已经定义了标准化的 `InboundEnvelope`，其中包含 `EnvelopeID / Source / SourceEventID / IdempotencyKey / Kind / PersonaKey / SessionID / RequestID / Content / UserMessage / Approval / Traceparent / RawMeta` 等字段，这已经足够支撑平台消息、用户消息、审批动作、系统恢复等入口统一进入 Turn。

同时，Turn 状态机已经显式列出 `created / normalizing / memory_prepared / emotion_prepared / running_emotion / approval_wait / done / failed` 等状态，以及 `normalize / memory_prepare / emotion_prepare / emotion_loop / outbound_commit / memory_commit / approval_apply / resume` 等 Stage。 这意味着插件 Hook 不需要另起事件总线旁路，而是可以直接围绕这些 Stage 做“前置、后置、错误、观测、变换”。

`TurnContext` 与 `StageResult` 也已经有清晰的执行上下文、诊断区、Outbound 事件和 Stage Metrics。 运行时会逐个执行 Stage、记录 transition 和 duration metrics。 这正好可以扩展为 Hook 级指标、插件超时、插件熔断和回放诊断。

### 1.2 Journal 与 Idempotency 已经具备插件审计基础

Turn Journal 已经定义 `StartTurn / RecordTransition / RecordEvent / CompleteTurn`，并有 `TurnRecord / TurnTransition / JournalEvent / TurnSnapshot` 等审计对象。 插件系统应复用这条审计链，记录 `plugin_hook_started / plugin_hook_done / plugin_hook_failed / plugin_hook_skipped / plugin_capability_denied`，而不是单独写散乱日志。

Idempotency 已经能根据 inbound source、session、kind、request id、approval action 等生成幂等键。 插件副作用也应绑定同样的 Turn ID 与 Invocation ID，避免 WebSocket 重放、审批恢复、重复请求时重复执行插件副作用。

### 1.3 Tool Dispatcher 已经有权限模型雏形

当前工具系统已经区分 `emotion / work / both` Scope，以及 `read-only / workspace-write / approved-destructive` 权限级别。 Dispatcher 也明确是 fail-closed：未知工具、输入非法、权限不足都应返回错误结果。

插件系统不应绕过这个 Dispatcher。插件注册工具时，只能注册成普通 `tool.Spec` 的扩展来源，并继续走 schema validation、permission classification、approval binding、tool approval gate。

### 1.4 Work / Approval 协议已经能挂插件观测点

`TaskBrief` 和 `TaskReport` 已经是 Emotion ↔ Work 的协议对象。 `DecisionPacket` 已经承载结构化升级请求，并包含 category、goal summary、question、why blocked、options、tradeoffs、recommendation、tool approval binding 等字段。 `ApprovalRequest` 也已经持久化了审批请求的 session、task、category、risk、options、status、actor、expires、tool approval binding 等字段。

因此，插件接口应优先提供 `BeforeWorkDispatch / AfterWorkReport / OnDecisionPacket / OnApprovalRequested / OnApprovalResolved` 等 Hook，但第一版建议大多是**只读观测或附加提示**，不要允许插件直接替 Emotion 做审批决定。

### 1.5 Memory 边界必须保持权威过滤

MemoryCore 的设计已经明确：SQLite 是权威记忆库，TriviumDB / sidecar / graph activation / reranker 都只是候选或排序信号；最终 Prompt 注入前必须经 SQLite authority filter。 上传的长期记忆架构也要求 Memory 属于 Emotion 的长期状态层，Work 只能提交候选，工具 / 插件事件通常只能作为 evidence，不能直接写长期记忆。

所以插件的 Memory 能力必须设计成：

```text
允许：
- 读取安全过滤后的 Memory Context
- 在 Hook 中建议增加 / 降权 / 标注 prompt block
- 提交 memory_candidate
- 提交 forget_request / pin_request

禁止：
- 直接写 facts
- 直接改 visibility / lifecycle / validity
- 直接绕过 Forget / Purge Manager
- 直接访问 purged / hidden / forgotten 原文
```

---

## 2. 插件接口套件的目标形态

### 2.1 一句话定义

```text
EmoAgent Plugin Suite
= Manifest-driven Plugin Host
+ Typed Hook Bus
+ Capability-based Host Facades
+ Turn Pipeline integration
+ Tool / Memory / Work / UI extension points
+ Hot reload / gray rollout / audit / isolation
```

中文定义：

```text
插件不是在主流程旁边乱插回调，
而是在 EmoAgent Host 明确授权下，
通过类型化 Hook 和受限 Facade，
参与 Turn、Memory、Tool、Work、UI 的可审计扩展。
```

### 2.2 核心原则

第一，**Emotion 会话所有权不可破坏**。插件不能成为新的对话者，不能绕过 Emotion 直接向用户输出最终答复。插件最多影响上下文、工具、记忆候选、UI 扩展或 outbound decoration，最终表达仍由 Emotion 组织。

第二，**Hook 必须类型化**。不要做 `map[string]any` 满天飞的泛型事件系统。每个 Hook 都要定义输入 DTO、允许返回的 Patch 类型、是否可变更、超时、失败策略。

第三，**Capability 权限优先于 Hook 名称**。声明 `BeforeToolCall` 不代表能执行工具，声明 `AfterMemoryRetrieve` 不代表能读全部记忆。插件实际能访问什么，由 CapabilityAuthorizer 按 manifest、persona、session、runtime state、当前 hook、当前 request 共同决定。

第四，**默认可观测、可回放、可退化**。插件失败不能把陪伴主链打崩；高风险 Hook fail-closed，装饰类 Hook fail-open；所有插件调用都写入 Turn Journal 或 Plugin Audit Log。

第五，**先内置 / 同进程，后进程隔离**。MVP 阶段可以先支持 compiled builtin plugins，打通 HookBus 和 Capability；第三方插件不要一开始就 Go plugin 动态加载，因为 Go plugin ABI 和依赖冲突都不适合长期插件生态。第三方插件建议走 process runner / gRPC / JSON-RPC。

---

## 3. 建议代码结构

建议新增：

```text
internal/plugin/
  manifest.go          # emoagent-plugin.yaml schema / validation
  capability.go        # Capability enum + Authorizer
  host.go              # PluginHost lifecycle
  registry.go          # PluginRegistry, HookRegistry
  hook.go              # HookName, HookEnvelope, HookResult, Patch
  hookbus.go           # dispatch, timeout, priority, parallel/serial
  audit.go             # plugin invocation audit
  config_store.go      # per-plugin config
  budget.go            # timeout / concurrency / circuit breaker
  facade/
    turn.go            # TurnFacade, TurnView
    memory.go          # MemoryFacade, safe memory views
    tool.go            # ToolFacade, plugin-owned tools
    work.go            # WorkFacade, read-only work views
    outbound.go        # OutboundFacade
    ui.go              # dashboard / command facade
  runner/
    builtin.go         # compiled plugin runner
    process.go         # future JSON-RPC/gRPC runner
    protocol.go        # runner request/response DTO

internal/plugin/builtin/
  outbound_guard/
  memory_context_debug/
  turn_audit/

pkg/pluginapi/          # 后期对第三方稳定暴露，MVP 可先不建
```

尽量不要把插件类型直接塞进 `internal/turn`。`internal/turn` 应保持纯状态机和运行时。插件编排可以在 `internal/chat/turn_runtime.go` 里通过 stage wrapper 接入，或新增一个轻量 `turn.Middleware`，但不要让底层 Turn Runtime 依赖具体 pluginhost。

推荐第一版采用：

```text
chatTurnRuntime
  -> build stages
  -> pluginhost.WrapStages(stages)
  -> turn.Runtime.Execute(...)
```

这样不会污染通用 Turn Runtime，也方便 shadow / rollout。

---

## 4. Manifest 设计

文件名建议：

```text
emoagent-plugin.yaml
```

示例：

```yaml
schema_version: emoagent.plugin.v0.1

id: com.emoagent.plugins.memory-context-debug
name: Memory Context Debug
version: 0.1.0
author: EmoAgent
description: Show safe memory retrieval diagnostics in dev mode.
sdk_version: ">=0.1.0 <0.2.0"

runtime:
  kind: builtin        # builtin | process | grpc
  entrypoint: memory_context_debug
  command: []
  working_dir: ""

compatibility:
  emoagent: ">=0.1.0"
  memorycore: ">=0.5.0"

capabilities:
  - turn.read
  - turn.annotate
  - memory.read.safe
  - outbound.emit.safe_debug

hooks:
  - name: after_memory_retrieve
    priority: 100
    timeout_ms: 80
    mode: serial
    failure_policy: fail_open
  - name: on_turn_error
    priority: 100
    timeout_ms: 80
    mode: parallel
    failure_policy: fail_open

config_schema:
  type: object
  properties:
    enabled:
      type: boolean
      default: false
    include_score_breakdown:
      type: boolean
      default: false

resource_limits:
  max_concurrency: 4
  default_timeout_ms: 100
  max_timeout_ms: 1000
  max_output_bytes: 65536

scope:
  personas: ["*"]
  sessions: ["*"]

isolation:
  network: none        # none | loopback | allowlist
  filesystem: none     # none | plugin_data | workspace_readonly
```

Manifest 必填字段建议：

```text
schema_version
id
name
version
sdk_version
runtime.kind
capabilities
hooks
resource_limits
```

Manifest 验证规则：

```text
id 必须反向域名或安全 slug；
version 使用 semver；
sdk_version 使用 semver range；
hook name 必须在 Host 支持清单中；
capability 必须在 Host 支持清单中；
每个 hook 的 timeout <= manifest.resource_limits.max_timeout_ms；
process/grpc runtime 必须声明 command 或 endpoint；
默认禁用第三方 plugin，必须显式启用。
```

---

## 5. Capability 模型

### 5.1 Capability 清单 v0.1

```text
turn.read
turn.annotate
turn.emit_event

memory.read.safe
memory.retrieve.augment
memory.candidate.submit
memory.forget.request

llm.request.decorate
llm.response.observe

outbound.decorate
outbound.emit.safe_debug

tool.register
tool.call.readonly
tool.call.workspace_write
tool.destructive.request_approval

work.observe
work.dispatch.annotate
approval.observe

config.read.own
config.write.own

ui.dashboard.page
command.register
```

### 5.2 明确禁止或暂缓的 Capability

v0.1 不开放：

```text
memory.write.authoritative
memory.visibility.modify
memory.purge.execute
approval.resolve
work.resume.execute
tool.destructive.execute
session.impersonate
raw_prompt.read
raw_tool_output.read
reasoning_content.read
```

原因很简单：这些能力要么会破坏 MemoryCore 的 SQLite 权威边界，要么会破坏 Emotion 的会话所有权，要么会绕过当前工具审批体系。

### 5.3 Capability Authorizer 判定维度

```go
type CapabilityRequest struct {
    PluginID    string
    Capability  Capability
    Hook        HookName
    TurnID      string
    SessionID   string
    PersonaKey  string
    ResourceRef string
    Action      string
}
```

判定逻辑：

```text
manifest 是否声明 capability
plugin 是否 enabled
hook 是否允许使用该 capability
当前 persona/session 是否在 scope 内
当前 Turn 状态是否允许该 action
resource policy 是否允许
高风险 action 是否需要 approval
插件是否处于 degraded / circuit-open
```

---

## 6. Hook Suite 设计

### 6.1 Hook 类型分级

| 类型              | 含义                               | 默认执行          | 失败策略                          |
| --------------- | -------------------------------- | ------------- | ----------------------------- |
| Observe Hook    | 只读观测，不改变主链                       | 并行            | fail-open                     |
| Transform Hook  | 返回受限 Patch                       | 按 priority 串行 | 默认 fail-open，安全类可 fail-closed |
| Decision Hook   | 可要求 block / approval / downgrade | 串行            | fail-closed                   |
| Provider Hook   | 注册工具 / 命令 / 页面                   | 生命周期期执行       | fail-closed                   |
| SideEffect Hook | 写插件自身状态 / 外部动作                   | 异步或延后         | fail-open + audit             |

### 6.2 Turn / Message Hook

| Hook                       | 接入点                           |  是否可变更 | 用途                           |
| -------------------------- | ----------------------------- | -----: | ---------------------------- |
| `before_ingress_normalize` | `StageNormalize` 前            |   是，受限 | 平台消息清洗、命令识别、metadata 标注      |
| `after_ingress_normalize`  | `StageNormalize` 后            | 否 / 标注 | 记录 inbound 标准化结果             |
| `before_turn_start`        | Turn 建立后                      |      否 | audit、trace、插件 session state |
| `after_turn_end`           | Turn complete 后               |      否 | metrics、debug、异步侧效           |
| `on_turn_error`            | Stage error / terminal failed |      否 | 插件诊断、降级提示                    |

### 6.3 Memory Hook

| Hook                     | 接入点                        |    是否可变更 | 用途                            |
| ------------------------ | -------------------------- | -------: | ----------------------------- |
| `before_memory_prepare`  | 写入 user episode 前          |        否 | 观测、输入安全标注                     |
| `after_memory_prepare`   | episode anchor 后           |        否 | 插件看到 episode id，但不看隐藏原文       |
| `before_memory_retrieve` | retrieval request 前        |     是，受限 | 添加 query hint，不直接读库           |
| `after_memory_retrieve`  | SQLite authority filter 后  |     是，受限 | 添加安全提示、重排建议、debug block       |
| `on_memory_candidate`    | 抽取候选进入前                    |     是，候选 | 插件提交候选，不直接写 facts             |
| `before_memory_commit`   | assistant episode commit 前 |   否 / 标注 | 记录待提交状态                       |
| `after_memory_commit`    | commit 成功后                 |        否 | audit、异步提取触发                  |
| `on_forget_requested`    | 用户遗忘意图识别后                  | 否 / 辅助定位 | 帮助 target resolver，但不执行 purge |

### 6.4 Emotion / LLM Hook

| Hook                     | 接入点                  |  是否可变更 | 用途                                      |
| ------------------------ | -------------------- | -----: | --------------------------------------- |
| `before_emotion_prepare` | memory prompt 组装前    |   是，受限 | 添加安全表达 hint                             |
| `after_emotion_prepare`  | prompt context ready |      否 | debug、metrics                           |
| `before_llm_request`     | ChatRequest 发送前      |   是，受限 | 添加 system appendix、tool visibility hint |
| `after_llm_response`     | LLM response 后       | 否 / 标注 | 统计、检测异常                                 |
| `before_outbound`        | 输出给用户前               |   是，受限 | 格式化、引用、UI payload                       |
| `after_outbound`         | 输出完成后                |      否 | metrics、side effect                     |

### 6.5 Tool Hook

| Hook                        | 接入点                    |    是否可变更 | 用途           |
| --------------------------- | ---------------------- | -------: | ------------ |
| `on_tool_registered`        | plugin tool 注册         |        否 | audit        |
| `before_tool_call`          | Dispatcher classify 前后 | Decision | 额外审批、降级、观测   |
| `after_tool_call`           | tool result snip 后     |        否 | metrics、安全摘要 |
| `on_tool_approval_required` | approval gate 命中       |        否 | 插件 UI 增强、通知  |
| `on_tool_error`             | tool handler failed    |        否 | diagnostics  |

插件工具必须继续走当前 `tool.Spec`、permission、approval classifier、Dispatcher fail-closed 管道，不能直接执行。

### 6.6 Work / Approval Hook

| Hook                    | 接入点                    |  是否可变更 | 用途                                    |
| ----------------------- | ---------------------- | -----: | ------------------------------------- |
| `before_work_dispatch`  | Emotion 生成 TaskBrief 后 |   是，受限 | 添加非人格化 constraints / acceptance hints |
| `after_work_report`     | TaskReport 回来后         | 否 / 标注 | 报告审计、结构化展示                            |
| `on_decision_packet`    | Work 阻塞升级              | 否 / 建议 | UI 展示、风险提示                            |
| `on_approval_requested` | ApprovalRequest 创建     |      否 | 通知、dashboard                          |
| `on_approval_resolved`  | 审批完成                   |      否 | audit、插件状态同步                          |

第一版建议只开放 `work.observe` 和 `work.dispatch.annotate`，不要开放 `work.resume.execute`。

### 6.7 UI / Command Hook

| Hook                 | 接入点                 | 用途                      |
| -------------------- | ------------------- | ----------------------- |
| `on_command`         | 标准化 inbound 后       | slash command / 管理命令    |
| `on_dashboard_page`  | WebUI 管理页           | 插件配置、状态、诊断              |
| `on_websocket_event` | WS 入站 / 出站 envelope | 平台扩展、debug，仅限安全 payload |

---

## 7. Hook DTO 与 Patch 设计

### 7.1 HookEnvelope

```go
type HookName string
type HookMode string
type FailurePolicy string

type HookEnvelope struct {
    InvocationID string
    Hook         HookName
    PluginID     string
    TurnID       string
    Stage        turn.StageName
    State        turn.TurnState
    SessionID    string
    PersonaKey   string
    Traceparent  string
    Deadline     time.Time
    Capabilities []Capability
}
```

`InvocationID` 建议：

```text
plugin_id + hook_name + turn_id + stage + seq
```

用于插件幂等、副作用去重、审计回放。

### 7.2 HookContext

不要把完整 `turn.TurnContext` 直接给插件，而是给只读 View：

```go
type HookContext struct {
    Envelope HookEnvelope
    Turn     TurnView
    Memory   *MemoryView
    Tool     *ToolCallView
    Work     *WorkView
    Outbound *OutboundView
    Config   map[string]any
}
```

### 7.3 HookResult

```go
type HookResult struct {
    Annotations map[string]any
    Patches     []Patch
    Decisions   []DecisionPatch
    Events      []PluginEvent
    Metrics     HookMetrics
}
```

### 7.4 Patch 类型

```text
turn.annotate
memory.add_query_hint
memory.add_safe_context_block
memory.suppress_context_block
llm.add_system_appendix
llm.add_tool_hint
outbound.decorate_text
outbound.add_payload
tool.require_approval
tool.downgrade_permission
work.add_constraint_hint
work.add_acceptance_hint
```

所有 Patch 必须是**白名单类型**。不要允许插件返回任意修改后的 `TurnContext`、`ChatRequest`、`TaskBrief` 或 `MemorySnapshot`。

---

## 8. 运行时设计

### 8.1 MVP：Builtin Runner

第一阶段只支持编译进主程序的 builtin plugin：

```go
type BuiltinPlugin interface {
    Manifest() Manifest
    Register(ctx context.Context, registrar Registrar) error
    Shutdown(ctx context.Context) error
}
```

优点：

```text
实现成本低；
可以先把 HookBus / Capability / Audit / Config 打通；
适合做官方内置插件、回归测试插件、debug 插件。
```

### 8.2 Alpha：Process Runner

第三方插件建议使用独立进程：

```text
Host <-> Plugin Runner
JSON-RPC / gRPC over stdio or localhost
```

Runner 协议：

```text
Initialize(manifest, config, host_info)
RegisterHooks()
InvokeHook(envelope, context)
Health()
ReloadConfig()
Shutdown()
```

隔离策略：

```text
每插件独立进程；
每插件独立配置目录；
默认无网络；
默认无文件系统；
stdout/stderr 限流；
调用超时；
内存和并发预算；
心跳失败自动熔断。
```

### 8.3 Hot Reload 状态机

```text
Discovered
  → ManifestValidated
  → CapabilityReviewed
  → ConfigLoaded
  → RunnerStarted
  → HooksRegistered
  → Warmed
  → Active
  → Draining
  → Stopped
```

升级流程：

```text
start new runner
→ health check
→ register hooks in shadow
→ cut over new invocations
→ drain old inflight
→ shutdown old runner
```

卸载流程：

```text
mark draining
→ stop accepting new invocations
→ wait inflight or timeout
→ deregister hooks
→ call shutdown
→ revoke capabilities
→ audit stopped
```

---

## 9. 与现有 Turn Runtime 的接入方案

### 9.1 Stage Wrapper

在 `chatTurnRuntime.stages(...)` 返回 stages 后包装：

```go
func (h *PluginHost) WrapStages(stages []turn.Stage) []turn.Stage {
    wrapped := make([]turn.Stage, 0, len(stages))
    for _, stage := range stages {
        wrapped = append(wrapped, h.WrapStage(stage))
    }
    return wrapped
}
```

`WrapStage` 做：

```text
before_<stage>
→ original stage.Run
→ after_<stage>
→ on_stage_error
```

但对外暴露的 Hook 名称不必完全等于 Stage 名，建议做一层映射：

```text
StageNormalize       -> before_ingress_normalize / after_ingress_normalize
StageMemoryPrepare   -> before_memory_prepare / after_memory_prepare
StageEmotionPrepare  -> before_memory_retrieve / after_memory_retrieve / before_emotion_prepare
StageEmotionLoop     -> before_llm_request / after_llm_response / before_tool_call / after_tool_call
StageMemoryCommit    -> before_memory_commit / after_memory_commit
StageApprovalApply   -> on_approval_resolved
StageResume          -> after_work_resume
```

### 9.2 OutboundSink Wrapper

当前 Turn Runtime 会在 `emitOutbound` 中发 OutboundEvent。 插件系统可以包装 `OutboundSink`：

```text
before_outbound
→ emit original event
→ after_outbound
```

注意：`before_outbound` 只能改安全内容，例如文本装饰、payload 增补、debug block；不能伪造审批状态，不能直接泄漏 tool preview / reasoning。

### 9.3 Tool Dispatcher Wrapper

不要让插件自己执行工具。新增：

```go
type PluginAwareDispatcher struct {
    base *tool.Dispatcher
    host *plugin.Host
}
```

流程：

```text
ExtractToolCalls
→ before_tool_call hooks
→ base.ClassifyCall / Execute
→ after_tool_call hooks
```

若插件要求 `tool.require_approval`，只允许升级到当前已有 approval 机制，不能让插件自行弹窗审批。

---

## 10. 实施方案 Spec

### Milestone 1：文档与配置骨架

目标：

```text
新增 docs/architecture/plugin_interface_suite.md
新增 config.PluginConfig
默认 plugin.enabled=false
```

配置建议：

```yaml
plugins:
  enabled: false
  directories:
    - data/plugins
  builtin_enabled:
    - com.emoagent.plugins.turn-audit
  rollout_percent: 0
  default_timeout_ms: 80
  max_timeout_ms: 1000
  fail_closed_hooks:
    - before_tool_call
    - before_memory_commit
  audit:
    enabled: true
    include_payload: false
```

验收：

```text
go test ./internal/config/...
默认配置下行为与现有主链完全一致
```

### Milestone 2：Manifest / Registry / Capability

目标：

```text
internal/plugin/manifest.go
internal/plugin/registry.go
internal/plugin/capability.go
```

实现：

```text
Manifest struct + Validate()
Capability enum + Authorizer
HookSpec validation
PluginRegistry 支持 register/unregister/list
```

验收：

```text
非法 semver range 拒绝
未知 hook 拒绝
未知 capability 拒绝
timeout 超限拒绝
未启用 plugin 不注册 hook
```

### Milestone 3：HookBus v0

目标：

```text
internal/plugin/hook.go
internal/plugin/hookbus.go
internal/plugin/audit.go
```

实现：

```text
HookBus.Dispatch(ctx, hookName, context) HookResult
按 priority 排序
observe hooks 并行
transform hooks 串行
context deadline / timeout
panic recover
per-plugin circuit breaker
audit event 写 Turn Journal
```

验收：

```text
hook 按 priority 执行
timeout 后插件被标记 degraded
fail_open hook 不影响主链
fail_closed hook 返回明确 error_kind
Turn Journal 能看到 plugin hook start/done/fail
```

### Milestone 4：接入 Turn Stage Wrapper

目标：

```text
chatTurnRuntime 支持 PluginHost 注入
```

实现：

```text
EngineConfig / HandlerOption 增加 PluginHost
chatTurnRuntime.stages() 后 wrap
在 normalize / memory_prepare / emotion_prepare / message / memory_commit / approval_apply / resume 周围派发 hook
```

验收：

```text
plugins.enabled=false 时现有测试不变
plugins.enabled=true 但无 plugin 时现有测试不变
测试插件能观测 stage 顺序
插件失败符合 fail_open / fail_closed 规则
```

### Milestone 5：Builtin Plugin MVP

建议内置三个插件：

```text
turn-audit
  hooks: after_turn_end, on_turn_error
  capabilities: turn.read, turn.annotate

memory-context-debug
  hooks: after_memory_retrieve
  capabilities: memory.read.safe, outbound.emit.safe_debug

outbound-guard
  hooks: before_outbound
  capabilities: outbound.decorate
```

验收：

```text
turn-audit 能记录 turn summary
memory-context-debug 只能看到 SQLite 过滤后的 safe memory block
outbound-guard 只能改 assistant outbound text，不能改 approval/tool 状态
```

### Milestone 6：Plugin Tool 注册

目标：

```text
插件可以注册工具，但工具仍走现有 tool.Registry / Dispatcher / approval gate
```

实现：

```text
ToolFacade.Register(spec, handler)
spec.Name 自动加 plugin namespace 或强校验唯一性
plugin tool manifest 必须声明 tool.register
执行时注入 plugin ID
工具权限不得超过 manifest capability
```

验收：

```text
无 tool.register capability 注册失败
workspace-write 工具无授权执行失败
destructive 工具触发现有 approval
未知插件工具 fail-closed
```

### Milestone 7：Memory / Work 高级 Hook

目标：

```text
开放 memory candidate、work observe、approval observe
```

实现：

```text
MemoryFacade.SubmitCandidate()
MemoryFacade.RequestForget()
WorkFacade.ObserveTaskBrief()
WorkFacade.AnnotateTaskBrief()
ApprovalFacade.Observe()
```

验收：

```text
插件提交 memory candidate 不会直接写 fact
插件 request forget 进入 Forget Manager 队列
插件不能 resolve approval
插件不能 resume work
```

### Milestone 8：Process Runner Alpha

目标：

```text
支持 data/plugins/<id>/emoagent-plugin.yaml + process runner
```

实现：

```text
plugin discovery
manifest validate
process start / health / invoke / shutdown
JSON-RPC over stdio or localhost
bounded payload
per-plugin timeout / max concurrency / circuit breaker
```

验收：

```text
进程崩溃不拖垮主链
runner 超时被熔断
reload 能 drain inflight
插件只能访问被授权 facade
```

### Milestone 9：WebUI / Config Schema

目标：

```text
插件配置可视化、启停、状态页
```

实现：

```text
config_schema 渲染
plugin status API
plugin audit query
dashboard page registry
session/persona level enable override
```

验收：

```text
可查看 loaded/active/degraded/stopped
可修改 own config 并触发 ReloadConfig
可按 persona/session 灰度启用
```

---

## 11. 性能优化设计

### 11.1 Dispatch 快路径

```text
HookRegistry 预编译 map[HookName][]RegisteredHook
无插件或 hook empty 时 O(1) 返回
plugins.enabled=false 时不分配 HookContext
```

### 11.2 Payload 最小化

插件只拿 View，不拿原始对象：

```text
TurnView      只含 turn id、state、persona、session、kind、safe content summary
MemoryView    只含 SQLite authority 后的 safe blocks
ToolCallView  默认不含 raw tool output
OutboundView  默认不含 reasoning content
```

这与现有 Journal 的安全思路一致：敏感 payload key 会被过滤，tool preview 与 reasoning content 也会被清空或脱敏。

### 11.3 超时与熔断

建议默认：

```text
observe hook: 50-100ms
transform hook: 100-300ms
tool / memory decision hook: 300-800ms
process runner startup: 3s
shutdown drain: 2s
```

熔断策略：

```text
连续 3 次 timeout -> plugin degraded
连续 5 次 error -> circuit open 60s
安全类 hook fail-closed
装饰类 / debug 类 hook fail-open
```

### 11.4 并发策略

```text
Observe hooks 并行执行，结果只进 audit
Transform hooks 按 priority 串行执行，避免 patch 冲突
SideEffect hooks 进入 bounded async queue
每插件 max_concurrency 独立限制
每 Turn 总插件预算，例如 500ms
```

### 11.5 Patch 合并规则

```text
同一字段只允许一个 high-priority patch 修改
后续 patch 只能 append，不可覆盖
冲突进入 audit，并拒绝低优先级 patch
所有 patch 必须带 reason_code
```

---

## 12. 安全与隐私边界

### 12.1 Memory 安全

```text
Trivium / sidecar / plugin 都不是记忆权威。
插件只能看到 MemoryCore 已授权的 safe memory context。
插件 memory_candidate 必须走 Emotion / Memory policy。
插件 forget_request 必须走 Forget / Pin / Purge Manager。
```

### 12.2 Tool 安全

```text
插件工具不直接执行破坏性动作。
插件工具必须声明 schema。
插件工具必须走 Dispatcher。
destructive / sensitive read 必须进入当前 approval gate。
```

### 12.3 Prompt 安全

```text
插件不得读取 raw_prompt。
插件不得读取 hidden_memory。
插件不得读取 reasoning_content。
插件只能通过 llm.add_system_appendix 追加受限 system appendix。
appendix 有 token budget 和安全审计。
```

### 12.4 Output 安全

```text
before_outbound 只能修改 assistant final text 或附加安全 payload。
不能伪造 approval_updated。
不能伪造 tool result。
不能发送绕过 Turn Journal 的用户可见消息。
```

---

## 13. 推荐第一版交付范围

第一版不要贪多。建议目标是：

```text
PluginHost v0.1
- Manifest validation
- CapabilityAuthorizer
- HookBus
- BuiltinRunner
- Turn Stage Wrapper
- before_outbound
- after_memory_retrieve
- on_turn_error
- plugin audit in Turn Journal
- 1 个 debug 插件
- 1 个 outbound guard 插件
```

不做：

```text
第三方进程插件
WebUI 插件市场
插件签名
插件自动安装依赖
完整 Dashboard Page
插件直接注册复杂工具
插件参与审批决策
```

这样能先验证报告中的“插件接口套件设计”主干，同时风险最低。

---

## 14. 验收标准总表

| 类别      | 验收标准                                                                                |
| ------- | ----------------------------------------------------------------------------------- |
| 兼容性     | `plugins.enabled=false` 时，现有 Turn、Tool、Work、Memory 行为完全不变                           |
| Hook 顺序 | normalize → memory_prepare → emotion_prepare → message → memory_commit 的 Hook 顺序可测试 |
| 权限      | 未声明 capability 的插件无法访问对应 Facade                                                     |
| 安全      | 插件无法读取 raw prompt、raw tool output、reasoning、hidden/purged memory                    |
| 失败处理    | fail-open hook 超时不影响主回复；fail-closed hook 返回明确 error_kind                            |
| 审计      | 每次 Hook 调用都有 invocation_id、plugin_id、hook、duration、status                           |
| 幂等      | duplicate turn replay 不重复执行插件 side effect                                           |
| 工具      | 插件注册工具仍走 schema validation、permission、approval gate                                 |
| Memory  | 插件只能提交 memory candidate / forget request，不能直接写 facts                                |
| 性能      | 无插件时开销接近 0；有空 HookBus 时单 Turn 额外开销可忽略；有插件时受总预算控制                                    |

---
