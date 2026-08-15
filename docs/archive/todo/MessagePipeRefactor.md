## 0. 结论

这一步建议不要直接做插件，也不要先扩展 Hook 套件，而是把当前分散在 WebSocket Handler、`chat.Engine.sendTurn`、MemoryBridge、Work 工具、审批续跑里的逻辑收束成一条 **Turn Pipeline**。这和报告里的方向一致：每个用户消息、审批动作、系统恢复都统一建模为一个 `Turn`，按 `Ingress → Normalize → SessionBind → MemoryPrepare → EmotionPlan → WorkDispatch → ApprovalWait → SynthesizeReply → OutboundCommit → MemoryCommit → Done` 推进。

结合当前代码，最核心的判断是：**EmoAgent 的产品协议已经很完整，但运行时主链还没有显式化。** 现在 `internal/chat/handler.go` 直接识别 `message / approval_action / ping`，并直接调用 `SendMessage`、`ApplyApprovalAction`、`ContinueAfterApproval`；而 `conversationEngine` 接口已经暴露了普通消息、审批动作、审批后续跑等入口，说明消息流天然可以统一，但目前还被拆成了几条 handler 分支。

`chat.Engine.sendTurn` 当前承担了过多职责：存储用户消息、追加 MemoryCore Episode、加载完整历史、更新 running summary、组装 Emotion prompt、检索长期记忆、驱动 LLM tool loop、处理 Work/工具调用、处理审批中断、持久化助手回复、追加助手 Episode。这个函数已经是事实上的“Turn Runtime”，但它是一个大函数，而不是可观测、可回放、可阶段测试的状态机。

MemoryCore 这边已经很适合被做成独立 Stage：它的集成文档明确建议主仓库每轮追加 `AppendEpisode`，需要记忆注入时调用 `Retrieve`，会话结束时调用 `EndSession`，用户遗忘时走 `Forget`。 当前主仓库的 MemoryBridge 也已经把 `AppendUserEpisode / AppendAssistantEpisode / RetrievePromptSnapshot / FinalizeSegment` 封成了边界接口。

所以本 Spec 的目标是：**先把现有消息流“运行时化”，不改变 Emotion 唯一对话者、不改变 Work 后台执行、不改变 MemoryCore 权威边界、不做插件接口套件。**

### 0.1 决策定稿

本阶段明确收敛为 **Turn Pipeline runtime refactor**。目标是把当前 `handler → engine.sendTurn → tool/work/memory/approval` 的隐式大流程显式化成可观测、可回放、可降级的 Turn Runtime；不进入插件系统，不新增第二套消息旁路。

硬性决策如下：

```text
1. 本阶段只实现 Turn Pipeline runtime refactor，不进入插件 / Hook / SDK / capability。
2. Emotion 仍是唯一对话者，Work 仍是后台执行代理，MemoryCore 仍是长期记忆权威边界。
3. WebSocket 协议第一版保持兼容，仅新增 request_id。
4. Turn 定义为一次业务输入处理实例，user_message / approval_action / system_resume 都是 Turn；ping/pong 和 session_ready/greeting 不是 Turn。
5. Handler 最终只做连接生命周期和 adapter，不做业务编排。
6. TurnContext 是单 Turn 内唯一运行期工作副本；DB / PendingRegistry / ApprovalService / MemoryCore 仍是各自领域权威。
7. approval_action 必须作为新 Turn；ApprovalWait 是 terminal turn state。
8. delegate_to_work / resume_work 第一版继续作为 Emotion tools，不重写 Work runtime。
9. MemoryPrepare 固定采用：persist user message → append user episode → retrieve memory excluding current user episode。
10. approval_action 默认不写 user episode，不跑普通 memory retrieval。
11. OutboundEvent 第一版完全映射现有 WSMessage type；Runtime 负责 stream_start/end。
12. Journal 只持久化 sanitized control events、summary、metrics、final refs，不保存 raw prompt/tool/file/hidden memory/token deltas。
13. assistant 已发送但落库失败时，Turn 标记 commit_failed_after_output。
14. 切流必须支持 shadow、enabled、memory_stages、approval_stages、allowlist、percent rollout 和一键回滚。
```

#### 0.1.1 范围边界

- 本次只做 `Turn Pipeline` 运行时化，不做插件、Hook、SDK、capability 权限模型、插件沙箱、manifest 或插件热加载。
- 保持 Emotion 唯一对话者、Work 幕后执行、MemoryCore 权威边界不变。这些是项目不变量，不是可选扩展点。
- 现有前端 WS 消息协议基本不变。第一版只做内部事件模型重构，WS type 继续兼容 `stream_start / stream_delta / stream_end / tool_call_* / reasoning_* / work_progress / approval_*`。
- 允许新增 `request_id`，并作为第一版唯一前端协议变化；用于幂等、防重复点击、断线恢复和未来多平台统一入口。兼容期没有 `request_id` 时，服务端生成临时非持久 key。

#### 0.1.2 核心概念

```text
Message:
  用户可见、可持久化的聊天内容记录。

Turn:
  对一个 InboundEnvelope 的一次运行时处理实例。

Session:
  多个 Message / Turn 共享的长期对话容器。

InboundEnvelope:
  不同入口 WebUI / Telegram / QQ / API 的统一输入协议。
```

- `Turn` 是一次进入对话运行时的业务输入处理单元。用户消息、审批动作、系统恢复都算 Turn；`ping/pong`、连接建立、静态历史加载不算 Turn。
- `Session` 是长期会话容器；`Message` 是用户可见聊天记录；`InboundEnvelope` 是平台输入事件的标准化信封；`Turn` 是处理这个输入事件的一次运行实例。
- 一个 Turn 可能产生 0 或 1 条 user message、0 或 1 条 assistant message、若干 outbound events。
- `TurnContext` 是运行期唯一工作副本，不是跨系统唯一事实源。在一个 Turn 内，Stage 只读写 TurnContext；但持久真相仍在 DB、PendingRegistry、ApprovalService、MemoryCore。
- `TurnContext` 放本轮 input、memory snapshot、emotion request、work summary、approval outcome、output draft、diagnostics。DB 放长期 session/message/turn records；PendingRegistry 放 Work paused state；ApprovalService 放审批请求状态；MemoryCore 放长期关系记忆权威状态。
- `Message / Turn / Session / InboundEnvelope` 应写入 `CONTEXT.md` glossary。
- 范围边界、Turn 定义、Handler adapter 化、幂等/replay 策略、Memory 边界、Approval resume 语义、Outbound 持久化策略、Journal 脱敏规则、切流/回滚策略应补 ADR。

#### 0.1.3 入口与 Adapter

- Handler 最终只保留 WebSocket 生命周期与事件适配，业务编排迁入 TurnRuntime。
- `session_ready` / `greeting` 仍是连接级事件，不进入 Turn Pipeline；但每个 Turn 仍要校验 session/persona。
- 每个 Turn 都必须重新校验 session/persona 绑定，作为多平台、断线恢复、审批恢复、防串 session 的基础。
- `ping/pong` 不纳入 Turn，继续在 Handler 层处理。
- 未来 Telegram / QQ / API 与 WebUI 共用 `InboundEnvelope`。不同平台只做 adapter，不创建第二套消息流。

目标结构：

```text
WebSocket Handler / Telegram Adapter / API Handler
        ↓
InboundEnvelope
        ↓
TurnRuntime
        ↓
OutboundEvent
        ↓
Platform Adapter
```

#### 0.1.4 幂等

- `IdempotencyKey` 按 source 区分。WebUI 用 `source + session_id + request_id`；Telegram/QQ 用平台 message/update id；system resume 用 resume token。
- WebUI 兼容期没有 `request_id` 时，服务端生成临时 key。这种 Turn 可以防单进程内重复，但不能保证断线后精确重放。
- duplicate 且 Turn 已完成：优先重放 outbound；若没有持久化 outbound，则返回 no-op + final message ref。第一版可以不重放 token delta，只重放最终 assistant message 或状态摘要。
- duplicate 且 Turn running：第一版返回 busy/status，不挂接到同一个运行中 Turn。挂接同一个流会引入复杂订阅模型，放后续。
- 审批动作重复提交：同时依赖 Turn 幂等表和 ApprovalService 状态。ApprovalService 是破坏性动作消费的权威，Turn 幂等表负责入口去重和 UI 一致性。

#### 0.1.5 Pipeline 阶段

- 第一版采用 Spec 顺序，但允许合并内部阶段。正式状态列表固定，代码实现可先用较粗粒度。
- `WorkDispatchStage` 和 `ToolLoopStage` 第一版合并。建议命名为 `EmotionLoopStage` 或 `ToolLoopStage`，Work 事件作为子事件记录；后续再拆。
- `ApprovalApplyStage / ResumeStage` 必须加入正式 stage 列表。审批动作是标准 Turn，不应继续留在 Handler 分支。
- Tool loop 负责拿到最终内容或内部结果；SynthesizeReplyStage 负责把内部结果变成用户可见表达。terminal TaskReport 之后可以禁用工具，只让 Emotion 组织表达。
- OutboundCommitStage 既负责发送，也负责持久化 sanitized outbound summary。不持久化所有 token delta。
- MemoryCommitStage 负责 assistant message DB 写入和 assistant episode append。虽然名字叫 MemoryCommit，但它是最终输出 commit 的一部分。

第一版普通消息 Stage：

```text
IngressStage
NormalizeStage
SessionBindStage
MemoryPrepareStage
EmotionPrepareStage
EmotionLoopStage
ApprovalWaitStage? / SynthesizeReplyStage
OutboundCommitStage
MemoryCommitStage
DoneStage
```

审批 Turn Stage：

```text
IngressStage
NormalizeStage
SessionBindStage
ApprovalApplyStage
ResumeStage
SynthesizeReplyStage
OutboundCommitStage
MemoryCommitStage
DoneStage
```

#### 0.1.6 Engine 拆分

- 第一版保留 `chat.Engine` 为 facade，后续再拆 service 集合。
- `sendTurn` 第一轮拆到 5 个 helper 粒度：config snapshot、input persist/memory prepare、emotion request prepare、emotion loop run、output/memory commit。
- `PrepareEmotionRequest` 作为第一批抽取接口，用于把 summary/context/memory injection 从 LLM loop 中拆开。
- Runtime config 快照放在 EngineFacade，TurnRuntime 调用它。当前 `sendTurn` 开头已有读锁快照逻辑，迁移时保留这个模式。
- 旧 `SendMessage / ContinueAfterApproval` 至少保留到 Phase 7。新 pipeline 全量默认开启、shadow diff 稳定、测试覆盖后，再改为兼容 wrapper 或删除。

第一轮建议拆出：

```go
SnapshotRuntimeConfig(ctx) RuntimeSnapshot
PrepareInputAndMemory(ctx, *TurnContext) error
PrepareEmotionRequest(ctx, *TurnContext) (*llm.ChatRequest, error)
RunEmotionLoop(ctx, *TurnContext, req *llm.ChatRequest) error
CommitTurnOutput(ctx, *TurnContext) error
```

#### 0.1.7 Memory

- MemoryPrepareStage 包含 user message DB 持久化。user message 是后续 memory episode、history、summary、retrieval query 的输入锚点。
- 固定语义：先 append user episode，再 retrieve，并排除当前 user episode。
- `approval_action` 默认不追加 user episode。审批点击是控制动作，不是普通聊天内容。
- `approval_action` 默认不跑普通 memory retrieval。除非 Resume 后需要 Emotion 重新组织表达，且 SynthesizeReplyStage 明确需要。
- manual pin / manual forget notice fast path 第一版放在 MemoryPrepare 内部子步骤，后续可拆 `MemoryIntentStage`。
- Memory retrieve 失败完全遵循现有 `FailOpen`。`FailOpen=true` 继续对话并写 degraded diagnostics；`false` 才阻断。
- assistant episode append 失败只 warning，不阻断用户已收到的回复。
- 预留 compensation queue，但第一版可先 warning + metric。对 assistant episode append、extraction enqueue、mirror sync 失败，后续再补补偿任务。
- thinking blocks / memory pipeline metadata 来源统一改为 TurnContext。DB metadata 由 TurnContext.Output/Memory 生成。

Memory 状态边界：

```text
TurnContext.Memory:
  segment ref
  user_episode_id
  retrieval prompt block
  retrieval diagnostics
  excluded_episode_ids
  memory_degraded bool

MemoryCore:
  authoritative episode/fact/retrieval state
```

#### 0.1.8 Work 与审批

- `delegate_to_work / resume_work` 第一版继续作为 Emotion 工具存在。
- TurnJournal 记录 Work 安全摘要事件：task_start、task_paused、task_resumed、task_end、decision_packet_summary、approval_request_id、task_report_summary、work_journal_ref、progress state。
- Work journal 与 TurnJournal 通过 `turn_id + session_id + task_id + approval_request_id` 关联。Work journal path/hash 可作为 debug ref。
- DecisionPacket / TaskReport 进入 TurnContext 时只保留 sanitized 粒度：category、risk、question、safe options、status、summary、findings summary；不保留 raw Work messages/tool output。
- ApprovalWait 是 terminal turn state。该 Turn 结束在 `approval_wait`，等待用户审批开启新 Turn。
- `errApprovalPending` 应退出主控制流，改为 StageResult 状态。可保留内部 sentinel 过渡，但不让 Handler 识别它。
- `approval_required` 后允许且应该发送 `stream_end`，用于关闭本轮流式 UI。
- approval pending 时绝不持久化 assistant message，避免把半截工具前叙述写入历史。
- `approval_action` 必须建模为新 Turn。
- 审批后系统内部直接 `resume_work` 的语义必须写入 Spec/ADR。当前代码已有 `resumeApprovalDirectly`，这是重要架构约束。
- terminal TaskReport 后建议禁用工具，只让 Emotion 叙述，防止刚完成的 Work 结果又触发二次 Work/tool loop。
- 顶级 Turn state 使用统一 `approval_wait`，再用 `wait_reason = human_confirmation / permission_escalation_required / tool_approval` 区分。

推荐审批状态：

```text
turn.status = approval_wait
turn.wait_reason = tool_approval | human_confirmation | permission_escalation_required
turn.approval_request_id = ...
turn.task_id = ...
```

目标事件顺序：

```text
stream_start
... optional reasoning/tool/work_progress
approval_required
stream_end
```

#### 0.1.9 Outbound

- 第一版 `OutboundEvent` 类型集合完全映射现有 WSMessage type，不引入前端大改。
- 目标由 Runtime 发 `stream_start / stream_end`。迁移期 Handler 可以暂时包一层，但最终 Handler 不负责业务流边界。
- 同一 Turn 内按 seq 单调递增。`stream_start` 最先；reasoning/tool/progress/delta 在中间；approval 或 final delta 后 `stream_end`；`stream_end` 每 Turn 最多一次。
- 引入有界 channel，避免慢客户端拖死 LLM/Work runtime。
- 可丢弃/降采样：work_progress、部分 reasoning_delta。不可丢：stream_start/end、stream_delta、approval_*、tool_call_start/end、final error。
- `stream_delta` 做合并，默认 30ms 或 512 bytes flush，可配置为 20-50ms / 256-512 bytes。
- WS 发送失败 cancel 当前 Turn，同时 journal 写 `outbound_failed`。
- outbound 需要有限可重放。第一版不落所有 token delta，只落控制事件、approval、tool summary、reasoning metadata、final assistant message id、final content hash/summary。

持久化分级：

```text
Must persist:
  stream_start/end
  approval_required/updated
  tool_call_start/end sanitized summary
  reasoning_start/end metadata
  final_message_id
  error/final status
  metrics

Do not persist by default:
  every token delta
  raw reasoning content
  raw tool output
  raw file content
```

#### 0.1.10 Journal / SQLite

- 新增 `turns`、`turn_events`、`turn_outbound_events` 三张表。如果第一版需要降复杂度，至少先建前两张，第三张可只存 control events。
- SQLite 为正式 journal，JSONL 为 debug 可选，in-memory 为降级。
- 不持久化所有 token delta。
- 必须持久化 start/end、state transition、approval、tool summary、reasoning metadata、work summary、final message id、metrics、error。
- journal 初始化失败允许降级 in-memory，但 diagnostics 标记 `journal_degraded`；如果启用了强幂等/replay，可配置为 fail-closed。
- TurnJournal 脱敏规则必须固定成文档。
- 禁止记录 prompt block、hidden memory、raw tool output、文件内容、purged/forgotten memory。只允许 hash、token estimate、safe summary、node count、source count、status。
- outbound payload_json 保存 sanitized payload，不保存完整 raw payload。

脱敏规则：

```text
Allowed:
  IDs, status, category, safe summary, duration, token estimate,
  hash, size, truncated flag, approval status, final message id.

Forbidden:
  raw prompt, raw tool output, file contents, secret values,
  hidden/forgotten/purged memory, raw destructive input,
  sensitive reasoning, full chain-of-thought.
```

#### 0.1.11 错误与状态

- StageResult 组合规则：`Err=nil + Terminal=false` 继续；`Err=nil + Terminal=true` 正常终止；`Err!=nil + Terminal=true` 失败/取消/等待类终止；不要使用 `Err!=nil + Terminal=false` 表达普通降级，降级放 diagnostics。
- DB user message 写失败直接失败，不进入 LLM，不进入 Memory，不产生 assistant 输出。
- DB assistant message 写失败时状态为 `commit_failed_after_output`。用户可能已收到回复，但未落库；journal 记录 final content hash、错误、补偿建议。
- 状态命名区分 status 与 diagnostics。`done / failed / canceled / approval_wait / commit_failed_after_output` 是 Turn status；`memory_degraded / sidecar_degraded / work_failed / outbound_failed / journal_degraded` 是 diagnostics/error kind。
- 各阶段 timeout 配置化。至少有 memory、summary、llm、work、approval resume、outbound write timeout。
- 需要显式状态转移表，并在后续测试中按状态转移做断言。

推荐状态机：

```text
created
→ normalizing
→ session_bound
→ memory_prepared
→ emotion_prepared
→ running_emotion
→ synthesizing
→ outbound_committing
→ memory_committing
→ done

分支：
running_emotion → approval_wait
any → failed
any → canceled
outbound_committing/memory_committing → commit_failed_after_output
```

ErrorKind：

```text
validation_error
session_error
db_user_message_failed
memory_degraded
memory_failed
llm_failed
work_failed
approval_failed
outbound_failed
db_assistant_message_failed
canceled
timeout
```

#### 0.1.12 切流与验收

- Phase 0 冻结样例：普通消息、realtime on/off、tool call、reasoning、work progress、delegate completed、needs decision、approval approve/reject、manual memory notice、memory fail-open、WS cancel。
- 基线用事件序列测试、fake LLM transcript fixture、少量真实手动 transcript，不只做 snapshot。
- feature flag 采用 Spec 四个，并增加 rollout 配置。
- shadow journal 不允许明显影响热路径。目标：异步/批量写，失败降级；不要阻塞 LLM streaming。
- 新旧路径输出一致率分层定义。fake LLM 要求事件序列和 final content 精确一致；真实 LLM 只比较协议状态、事件顺序、落库/审批/Memory 行为，不比较自然语言逐字。
- 切流策略：全局开关 + persona/session allowlist + 百分比。先 allowlist，再百分比。
- 必须一键回旧路径。回滚不能要求迁移 DB 回滚；新表可以留着不用。
- Phase 7 后才允许删除 Handler 旧业务分支。条件：新 pipeline 默认开启、shadow diff 稳定、E2E 通过、至少一轮实际使用无严重回归。

推荐 flags：

```yaml
chat:
  turn_pipeline:
    shadow: true
    enabled: false
    memory_stages: false
    approval_stages: false
    rollout_percent: 0
    allow_personas: []
    allow_sessions: []
```

Phase 0 样例清单：

```text
T001 normal_message_non_streaming
T002 normal_message_realtime_streaming
T003 emotion_tool_get_time
T004 reasoning_events
T005 work_progress
T006 delegate_to_work_completed
T007 delegate_to_work_needs_emotion_decision
T008 tool_approval_required
T009 approval_action_approve_resume
T010 approval_action_reject_resume
T011 manual_memory_pin_notice
T012 manual_forget_preview_notice
T013 memory_retrieve_fail_open
T014 work_failed_task_report
T015 websocket_disconnect_cancel
```

---

## 1. 当前消息流现状分析

### 1.1 入口层现状

当前 WebSocket Handler 做了三类工作：

第一类是连接级工作：升级 WebSocket、解析 persona、恢复或创建 session、发送 `session_ready` 和 greeting。

第二类是消息回合工作：收到 `message` 后发送 `stream_start`，把一个 WS writer 塞进 `context.Context`，再调用 `engine.SendMessage`，期间由 callback 推送 `stream_delta`，最后发送 `stream_end` 和审批事件。

第三类是审批恢复工作：收到 `approval_action` 后先调用 `ApplyApprovalAction`，再发送 `approval_updated`，然后调用 `ContinueAfterApproval` 继续生成。

这说明现在的问题不是功能缺失，而是入口层承担了太多状态编排：什么时候 start/end stream、什么时候 emit approval、错误如何转用户可见消息，都散在 Handler 中。

### 1.2 Engine 现状

`Engine` 当前是消息流事实中心。它持有 LLM、DB、工具注册表、Dispatcher、PendingRegistry、ApprovalService、MemoryBridge、MemoryRetrieval 配置等依赖。

`sendTurn` 内部已经完成完整回合：

```text
持久化 user message
→ Memory append user episode
→ session title / timestamp
→ manual memory notice fast path
→ load history
→ update running summary
→ build Emotion context
→ pending decision injection
→ retrieve memory prompt block
→ build LLM request
→ LLM streaming
→ tool loop / Work delegation / approval interrupt
→ persist assistant message
→ Memory append assistant episode
```

这些步骤在逻辑上正好对应报告建议的 Pipeline Stage，但目前它们都耦合在一个函数中。尤其是 MemoryPrepare 和 MemoryCommit 已经有清晰位置：用户消息写库后追加用户 Episode，检索时排除当前用户 Episode，助手最终回复后追加助手 Episode。

### 1.3 Work / 审批现状

Work 已经有比较成熟的协议对象：`TaskBrief`、`TaskReport`、`DecisionPacket`、`ApprovalRequest`、`DecisionSummary` 都在 `internal/protocol/types.go` 中定义。

Work runtime 也已经是小型状态机：`RunOutcome` 要么是 `Report`，要么是 `Paused`；`PausedWork` 保存 brief、messages、progress、pending tool call、decision packet、round 等恢复所需状态；`Run` 从空历史开始，`Resume` 将决策作为 tool result 续跑。

当前 Work 是通过 Emotion-facing 工具 `delegate_to_work` 和 `resume_work` 接入主循环的。`delegate_to_work` 明确要求 Emotion 给 Work “outcome, not script”，并返回 `TaskReport` 或 `needs_emotion_decision`。 `resume_work` 支持普通决策、权限升级和 `approval_request_id` 审批续跑。

所以 Work 不需要重写；第一步只需要把 Work 的这些事件显式挂进 TurnJournal 和 TurnState。

### 1.4 MemoryCore 现状

MemoryCore 已经明确使用 SQLite 作为权威库，TriviumDB / sidecar 只产出候选、activation 或 rerank signal，SQLite authority filter 是 Prompt 注入前最终安全边界。

主仓库 MemoryBridge 也已经体现了这个边界：`RetrievePromptSnapshot` 调用 `b.host.Service.Retrieve`，然后用 `FormatMemoryContextForPrompt` 生成 prompt block；`AppendEpisode` 只追加原始事件，不直接生成长期事实。

这意味着消息流改造时，Memory Stage 应保持 “fail-open 可降级，authority 不绕过” 的原则。

---

## 2. 目标架构：Turn Pipeline

### 2.1 新主链

建议的目标主链如下：

```text
InboundEnvelope
  ↓
TurnRuntime.Execute
  ↓
IngressStage
  ↓
NormalizeStage
  ↓
SessionBindStage
  ↓
MemoryPrepareStage
  ↓
EmotionPlanStage
  ↓
WorkDispatchStage / ToolLoopStage
  ↓
ApprovalWaitStage? 或 SynthesizeReplyStage
  ↓
OutboundCommitStage
  ↓
MemoryCommitStage
  ↓
Done
```

其中 `WorkDispatchStage` 不一定第一版就把 Work 从 LLM tool loop 中拆出来。第一版可以保留 `delegate_to_work / resume_work` 作为工具，但必须把 Work 相关事件写入 TurnContext 和 TurnJournal。这样风险最低。

### 2.2 核心设计原则

**第一，Handler 只做 Adapter。**
`internal/chat/handler.go` 以后只负责 WebSocket 读写、连接生命周期、把 WSMessage 转成 `InboundEnvelope`，再把 `OutboundEvent` 写回 WS。它不再决定消息回合的状态流。

**第二，Engine 降级为能力集合。**
`chat.Engine` 不再是巨型回合编排器，而是给 Stage 使用的服务集合：session store、context assembler、LLM runner、tool loop runner、approval service、memory bridge。

**第三，TurnContext 是一回合内唯一事实来源。**
不要让状态散落在 callback、context.Value、局部变量、DB metadata、pending registry 中。所有 Stage 只读写 TurnContext，并把关键变更 append 到 TurnJournal。

**第四，Outbound 统一事件化。**
当前 `stream_delta`、`tool_call_start/end`、`reasoning_start/delta/end`、`work_progress`、`approval_required` 都是 WSMessage。改造后应统一为 `turn.OutboundEvent`，WS 只是其中一个 adapter。已有测试已经覆盖这些事件时序，可作为迁移基线。

**第五，失败策略进入状态机。**
MemoryCore retrieve 失败、sidecar degraded、Work 失败、审批等待、发送失败、DB 写入失败都必须在 StageResult 中显式表达，而不是靠分散的 `if err != nil`。

---

## 3. 推荐包结构

建议新增：

```text
internal/turn/
  contract.go          // InboundEnvelope, TurnContext, StageResult, OutboundEvent
  state.go             // TurnState, TurnKind, ErrorKind
  runtime.go           // Pipeline executor
  journal.go           // TurnJournal interface + sqlite/jsonl implementation
  diagnostics.go       // StageMetrics, timings, trace ids
  stream.go            // OutboundSink, buffered emitter, backpressure policy
  idempotency.go       // Source + SourceEventID 去重

internal/turn/stages/
  ingress.go
  normalize.go
  session_bind.go
  memory_prepare.go
  emotion_plan.go
  work_dispatch.go
  approval_wait.go
  synthesize_reply.go
  outbound_commit.go
  memory_commit.go

internal/chat/
  handler.go           // 保留 WS adapter，但删除大部分业务编排
  adapter_ws.go        // WSMessage <-> turn.InboundEnvelope / OutboundEvent
  engine_services.go   // 给 turn stages 使用的 EngineFacade

internal/storage/
  turn_records.go      // turns / turn_events / outbound_events 持久化
```

第一版不建议把包名叫 `plugin`、`hook`、`bus`。现在不做插件接口，叫 `turn` 更干净。

---

## 4. 核心契约设计

### 4.1 InboundEnvelope

```go
type InboundKind string

const (
    InboundUserMessage    InboundKind = "user_message"
    InboundApprovalAction InboundKind = "approval_action"
    InboundSystemResume   InboundKind = "system_resume"
)

type InboundEnvelope struct {
    EnvelopeID     string
    Source         string // webui | telegram | qq | api | system
    SourceEventID  string
    IdempotencyKey string

    Kind       InboundKind
    ReceivedAt time.Time

    PersonaKey string
    SessionID  string

    UserMessage *InboundUserMessage
    Approval    *InboundApprovalAction

    Traceparent string
    RawMeta     map[string]any
}

type InboundUserMessage struct {
    Content string
}

type InboundApprovalAction struct {
    RequestID string
    Action    string // approve | reject
    OptionID  string
}
```

`IdempotencyKey` 规则：

```text
webui:    source + session_id + client_request_id
telegram: source + platform_update_id
qq:       source + message_id
system:   source + resume_token
```

当前 `WSMessage` 已有 `RequestID` 字段但没有用于普通 message 去重。建议 WebUI 后续必须带 `request_id`；兼容期没有时由服务端生成非幂等 key。

### 4.2 TurnContext

```go
type TurnContext struct {
    TurnID          string
    Kind            TurnKind
    State           TurnState
    StartedAt       time.Time
    Deadlines       TurnDeadlines

    Inbound         InboundEnvelope
    Persona         *config.Persona
    Session         TurnSession

    Input           TurnInput
    Memory          TurnMemory
    Emotion         TurnEmotion
    Work            TurnWork
    Approval        TurnApproval
    Output          TurnOutput

    Journal         TurnJournal
    Stream          OutboundSink
    Diagnostics     TurnDiagnostics
}
```

关键约束：

```text
TurnContext 不跨 turn 复用。
Stage 顺序执行，默认不并发写 TurnContext。
并发任务只能通过 StageResult 或受控 channel 回写。
Journal 中不写 raw tool output、敏感 file content、完整 hidden memory。
```

### 4.3 StageResult

```go
type StageResult struct {
    NextState   TurnState
    Terminal    bool
    Err         error
    ErrorKind   string

    Outbound    []OutboundEvent
    RetryHint   *RetryHint
    Metrics     StageMetrics

    Commit      []CommitIntent
}
```

`CommitIntent` 用来把 DB 写入、Memory 写入、outbound 持久化等动作集中到 commit stage，避免前面每个 stage 自己乱写。

### 4.4 OutboundEvent

```go
type OutboundEvent struct {
    Seq       int64
    TurnID    string
    Type      string // stream_start, stream_delta, tool_call_start...
    Content   string

    Tool      *ToolActivity
    Reasoning *ReasoningActivity
    Approval  *protocol.ApprovalRequest

    CreatedAt time.Time
    Safe      bool
}
```

第一版可以直接映射到现有 WSMessage type，保证前端不用大改。

---

## 5. Stage 详细设计

### 5.1 IngressStage

职责：

```text
创建 turn_id
生成或校验 idempotency_key
初始化 TurnJournal
写入 turn_started 事件
绑定 outbound sink
```

失败策略：

```text
idempotency duplicate 且已 done：直接重放最终 outbound 或返回 no-op
idempotency duplicate 且 running：返回 busy / already_processing
journal 初始化失败：允许内存 journal，但 diagnostics 标记 degraded
```

### 5.2 NormalizeStage

职责：

```text
trim 用户消息
校验 message 不能为空
校验 approval_action 必填 request_id/action
规范 action = approve/reject
清理超长 RawMeta
生成 normalized input hash
```

当前 Handler 中这些校验分散在 `message` 和 `approval_action` 分支里，迁移后放到这里。

### 5.3 SessionBindStage

职责：

```text
解析 persona
恢复或创建 session
确保 session/persona 匹配
需要时发送 session_ready / greeting
```

建议拆分连接级与回合级：

```text
WebSocket connect:
  仍可提前执行 session_ready/greeting，保证 UI 体验。

Turn Pipeline:
  每个 turn 再校验 session/persona，并写入 TurnContext。
```

后续接 Telegram / QQ 时，没有长连接初始化，也可以直接通过本 Stage 绑定 session。

### 5.4 MemoryPrepareStage

职责：

```text
普通 user_message:
  持久化 user message
  UpdateSessionTimestamp
  Ensure memory segment
  AppendUserEpisode
  处理 manual pin / manual forget notice fast path
  执行 MemoryCore Retrieve
  把 memory prompt block 放入 TurnContext.Memory

approval_action:
  不追加 user episode
  不跑普通 retrieval，除非后续 synthesis 需要
```

注意保留当前语义：用户当前 episode 已写入，但检索时排除当前 userEpisodeID，避免刚说的话又作为长期记忆注入。当前代码已有这个设计。

失败策略：

```text
AppendEpisode 失败：记录 warning，当前对话继续。
Retrieve 失败：
  cfg.Memory.Retrieval.FailOpen=true  → 继续，无 memory block，diagnostics=memory_degraded
  FailOpen=false → 返回 stage error
Manual memory notice 命中：直接进入 SynthesizeReply/OutboundCommit，不跑 LLM。
```

### 5.5 EmotionPlanStage

职责：

```text
加载 history
加载/更新 running summary
注入 pending decisions summary
组装 Emotion system/messages
注入 MemoryPrepareStage 的 memory block
创建 llm.ChatRequest
```

这里先不要追求把 LLM streaming 也拆得过细。建议第一轮实现时把 `sendTurn` 中的 context assembly 先抽成：

```go
PrepareEmotionRequest(ctx, TurnContext) (*llm.ChatRequest, *PreparedEmotionContext, error)
```

### 5.6 WorkDispatchStage / ToolLoopStage

职责：

```text
运行 Emotion LLM tool loop
捕获 tool_call_start/end
捕获 delegate_to_work / resume_work 事件
捕获 TaskReport / DecisionPacket / NeedsEmotionDecision
将 Work journal task_id 与 turn_id 关联
```

第一版允许内部仍使用现有 tool registry 和 dispatcher。不要立刻重写 Work runtime，因为它已经支持 `Run / Resume / PausedWork`。

需要改的是事件出口：当前通过 `context.Value(wsWriterCtxKey)` 把 WS writer 传入 Engine。 建议替换成：

```go
ctx = turn.WithOutboundSink(ctx, turn.Stream)
ctx = progress.WithCallback(ctx, turn.ProgressCallback())
```

避免 WebSocket 细节污染 Engine。

### 5.7 ApprovalWaitStage

职责：

```text
当 Work 返回审批需求：
  停止当前 turn 的最终回复持久化
  发送 approval_required
  写入 turn state = approval_wait
  允许 stream_end，但不写 assistant message
  保留 Work PendingRegistry 状态
```

当前 `errApprovalPending` 是隐式状态，Handler 只把它当成非错误处理。 新设计中它应变成：

```go
StageResult{
    NextState: StateApprovalWait,
    Terminal: true,
    Err: ErrApprovalPending,
}
```

### 5.8 ApprovalApplyStage / ResumeStage

审批动作也要是 Turn，而不是 Handler 分支。

流程：

```text
InboundApprovalAction
  → Normalize
  → SessionBind
  → ApplyApprovalAction
  → ResumeWork
  → SynthesizeReply
  → OutboundCommit
  → MemoryCommit
```

当前 Engine 的 `ContinueAfterApproval` 已经会优先尝试 `resumeApprovalDirectly`，直接用 `approval_request_id` 调用 `resume_work`，再把结果作为内部 note 交给 Emotion 继续组织表达。

迁移后建议把这段逻辑放进 `ResumeStage`，不要留在 Handler。

### 5.9 SynthesizeReplyStage

职责：

```text
生成最终 assistantContent
确保 Emotion 统一表达 Work 结果
如果 Work terminal report 已经足够明确，可选择 disableTools=true 只做最终表达
```

保持当前不泄漏内部 ID 的要求：`buildApprovalOutcomeNote` 已经明确要求不要暴露 raw JSON、internal IDs、approval plumbing。

### 5.10 OutboundCommitStage

职责：

```text
统一发送 stream_start / stream_delta / stream_end
发送 tool/reasoning/work_progress/approval 事件
记录 outbound seq
处理发送失败
```

建议引入有界 channel：

```go
type OutboundSink interface {
    Emit(ctx context.Context, event OutboundEvent) error
}
```

性能与可靠性策略：

```text
stream_delta 可合并小片段，例如 20–50ms flush 或 256–512 bytes flush
tool/progress/reasoning/approval 不合并，保持顺序
channel 满时：
  普通 work_progress 可丢弃或降采样
  stream_delta / approval 不丢弃，触发 backpressure 或 cancel
发送失败时：
  cancel turn context
  journal 写 outbound_failed
```

### 5.11 MemoryCommitStage

职责：

```text
最终 assistantContent 持久化 DB
AppendAssistantEpisode
更新 session timestamp
将 thinking blocks / memory pipeline metadata 写入 message metadata
必要时 queue extraction job
```

当前代码已经把 thinking blocks 和 memory pipeline 写进 assistant message metadata。 迁移后保留这点，但把 metadata 来源改成 TurnContext。

失败策略：

```text
DB assistant message 写入失败：当前 turn 标记 failed_commit，返回用户可能已收到但未落库。
AppendAssistantEpisode 失败：warning + compensation queue，不影响用户已收到回复。
Memory extraction enqueue 失败：warning，不影响 turn done。
```

---

## 6. SQLite / Journal 建议

建议新增最小表：

```sql
CREATE TABLE turns (
  id TEXT PRIMARY KEY,
  idempotency_key TEXT UNIQUE,
  source TEXT NOT NULL,
  source_event_id TEXT,
  kind TEXT NOT NULL,
  session_id TEXT,
  persona_key TEXT,
  state TEXT NOT NULL,
  status TEXT NOT NULL, -- running | done | approval_wait | failed | canceled
  error_kind TEXT,
  error_message TEXT,
  started_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  completed_at TEXT
);

CREATE TABLE turn_events (
  id TEXT PRIMARY KEY,
  turn_id TEXT NOT NULL,
  seq INTEGER NOT NULL,
  stage TEXT NOT NULL,
  event_type TEXT NOT NULL,
  payload_json TEXT,
  created_at TEXT NOT NULL,
  UNIQUE(turn_id, seq)
);

CREATE TABLE turn_outbound_events (
  id TEXT PRIMARY KEY,
  turn_id TEXT NOT NULL,
  seq INTEGER NOT NULL,
  event_type TEXT NOT NULL,
  payload_json TEXT,
  delivery_status TEXT NOT NULL, -- pending | sent | failed | skipped
  created_at TEXT NOT NULL,
  delivered_at TEXT,
  UNIQUE(turn_id, seq)
);
```

第一版不需要把所有 token delta 都持久化，可以只持久化：

```text
stream_start
first_delta_at
stream_end
approval_required
tool_call_start/end summary
reasoning_start/end metadata
final assistant message id
stage metrics
```

这样不会造成 SQLite 写放大。

---

## 7. 性能优化要点

1. **Stage 顺序执行，但外部 I/O 有明确 timeout。** Memory retrieval、summary update、LLM、Work runtime、outbound write 各自有 stage timeout，避免一个阶段拖死整个 turn。

2. **快照 Engine runtime config。** 当前 `sendTurn` 开头已经在读锁中复制 LLM、model、params、registry、dispatcher 等运行时配置。 新 Pipeline 继续保留这个模式，避免长时间持锁。

3. **Memory retrieval fail-open。** MemoryCore 已经把 sidecar / mirror 作为增强，SQLite authority 是最终边界；消息流也应保持“增强失败不阻塞对话”的策略。

4. **Outbound 小片段合并。** LLM SSE delta 可能很碎，WS adapter 应做轻量 buffer flush，降低锁竞争和 JSON 序列化次数。

5. **Journal 采样与脱敏。** TurnJournal 记录状态、耗时、ID、摘要、hash，不记录完整工具输出、敏感文件内容、hidden memory block。

6. **幂等优先。** 用户重复点击发送、平台重复投递、审批按钮重复点击都应由 `IdempotencyKey` 和 approval status 防重。

7. **Work 进度降采样。** 当前已有 `progress.NewThrottler(3 * time.Second)` 控制进度推送。 新设计保留，并把阈值配置化。

---

## 8. 分步实施方案

### Phase 0：冻结现有行为基线

目标：不改运行逻辑，只补测试和样例。

工作：

```text
收集 20–30 个典型 turn：
- 普通消息
- realtime streaming on/off
- tool call start/end
- reasoning events
- Work progress
- delegate_to_work completed
- needs_emotion_decision
- approval_action approve/reject
- manual memory pin notice
- memory retrieval fail-open
```

验收：

```text
go test ./internal/chat ./internal/work ./internal/memoryhost ...
现有 handler 流式事件顺序保持不变。
审批事件不泄漏 raw content。
```

已有测试已经覆盖普通 streaming、tool/reasoning/progress、approval continuation 和 approval binding 不泄漏 raw content，可直接扩展。

### Phase 1：落 Turn Contract + Journal，先 shadow

目标：新增 `internal/turn`，不接管现有路径。

工作：

```text
定义 InboundEnvelope / TurnContext / Stage / StageResult / OutboundEvent。
实现 in-memory journal + sqlite/jsonl journal。
在 Handler 收到 message/approval_action 时生成 envelope，并 shadow 记录 turn_started / normalized / done_mock。
```

验收：

```text
feature flag 关闭时行为完全不变。
每个 WS message / approval_action 都能产生 TurnJournal。
journal 不包含用户敏感工具输出。
```

### Phase 2：Handler Adapter 化

目标：让 Handler 只做 WS adapter。

工作：

```text
新增 TurnRuntime.Execute(ctx, envelope, sink)。
Handler 把 WSMessage 转 envelope。
Runtime 第一版内部仍调用旧 engine.SendMessage / ContinueAfterApproval。
stream_start/end 仍可暂留 Handler，但事件出口走 OutboundSink。
```

验收：

```text
普通对话、approval_action 流程测试全部通过。
Handler 不再直接感知 engine 内部 approval pending 细节。
```

### Phase 3：拆 MemoryPrepare / MemoryCommit

目标：把 Memory 读写从 `sendTurn` 抽出。

工作：

```text
新增 EngineFacade：
- PersistUserMessage
- EnsureMemorySegment
- AppendUserEpisode
- RetrieveMemoryPrompt
- PersistAssistantMessage
- AppendAssistantEpisode

sendTurn 拆成：
- PrepareTurnInput
- GenerateEmotionReply
- CommitAssistantOutput
```

验收：

```text
普通消息仍能写 chat messages。
Memory user episode 写入后 retrieval 排除当前 episode。
assistant final reply 才写 assistant episode。
Memory 失败按 FailOpen 策略退化。
```

### Phase 4：统一 OutboundEvent

目标：替换 `wsWriterCtxKey`。

工作：

```text
turn.WithOutboundSink(ctx, sink)
progress.WithCallback(ctx, turn.ProgressCallback)
Tool/reasoning/work progress/approval 全部 Emit OutboundEvent。
WS adapter 负责 OutboundEvent -> WSMessage。
```

验收：

```text
现有前端消息 type 不变。
stream_delta 顺序稳定。
WS 断开能 cancel turn。
work_progress 仍被 throttling。
```

### Phase 5：审批并轨

目标：`approval_action` 变成标准 Turn。

工作：

```text
ApprovalApplyStage 调 ApplyApprovalAction。
ResumeStage 调 resumeApprovalDirectly / resume_work。
ApprovalWaitStage 替代 errApprovalPending 的 handler 特判。
```

验收：

```text
approval_required 后 turn 状态为 approval_wait。
用户 approve/reject 后新 turn 能恢复 Work。
重复 approval_action 不会重复执行破坏性操作。
```

### Phase 6：WorkDispatch 可观测化

目标：不重写 Work，但让 Work 对 Turn 可见。

工作：

```text
Work Journal 增加 turn_id/session_id。
delegate_to_work / resume_work 事件写入 TurnJournal。
DecisionPacket / TaskReport 以 sanitized summary 进入 TurnContext.Work。
```

验收：

```text
能从 turn_id 追踪到 task_id、approval_request_id、Work journal。
Work raw tool output 不进入主 TurnJournal。
```

### Phase 7：切流与清理

目标：新 Pipeline 接管主路径。

工作：

```text
feature flag:
- chat.turn_pipeline.shadow
- chat.turn_pipeline.enabled
- chat.turn_pipeline.memory_stages
- chat.turn_pipeline.approval_stages

先 shadow，再按 persona/session 百分比切流。
删除旧 Handler 分支中的业务逻辑。
```

验收：

```text
新旧路径输出一致率达到预设阈值。
关键测试和手动 E2E 全通过。
出现故障可一键回旧路径。
```

---

## 9. 不做的事

这一步明确不做：

```text
不设计第三方插件 manifest。
不开放 Hook SDK。
不做 capability 权限模型。
不做插件隔离 runner。
不改变 MemoryCore 的事实权威地位。
不重写 Work runtime。
不改变前端 WS 消息协议，除非增加 request_id。
```

这保证本次改造的边界足够小：只修主路，不开扩展面。

---
