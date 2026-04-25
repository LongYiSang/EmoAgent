# Work 运行时实现说明

本文档描述当前的 Work 运行时实现。它已经不是旧版的“最小只读代理”，而是一个具备决策升级、任务完成、暂停持久化、恢复执行与进度回调的完整子代理运行时。

---

## 1. 角色与边界

Work 是 EmoAgent 的任务执行子代理，由 Emotion 侧通过 `delegate_to_work` 触发，在隔离上下文中完成一个具体任务，再把结果以 `TaskReport` 返回给 Emotion。

当前实现的关键边界是：

- Work 只使用自己收到的 `TaskBrief` 和运行时环境事实，不继承 Emotion 的对话历史。
- `finish_task` 和 `request_decision` 是 Work 内部控制工具，由运行时拦截，不走普通工具分发。
- Work 会在需要时暂停，把状态持久化到 `PendingRegistry`，等待 Emotion 恢复。
- 运行时保留 journals/logs，用于审计和故障排查。

---

## 2. 权限范围

`permission_scope` 现在支持三档，而不是旧版的单档只读：

| scope | 含义 |
|------|------|
| `read-only` | 只能读取和分析，不可修改工作区，不可执行破坏性操作 |
| `workspace-write` | 允许文件写入、编辑、目录操作，以及在运行时启用时使用 shell |
| `approved-destructive` | 允许与已批准路径一致的破坏性操作；未获批准或不再匹配批准内容时必须停下并重新决策 |

运行时会同时在协议层和执行层校验这个字段，避免只靠提示词约束。

---

## 3. 控制工具

### `finish_task`

Work 完成时必须调用 `finish_task`，而且该轮只能有这一项工具调用。它提交最终结果，运行时将其转换为 `TaskReport`。

可提交的字段只有：

- `status`
- `summary`
- `findings`
- `open_questions`

### `request_decision`

当 Work 自己无法继续时，调用 `request_decision`。它同样必须是该轮唯一的工具调用。

当前决策类别是：

- `auto`
- `emotion_judgment`
- `human_confirmation`

规则要点：

- `auto` 是低风险、运行时可直接处理或自动推进的决策。
- `emotion_judgment` 需要 Emotion 结合语气、偏好、关系上下文做判断。
- `human_confirmation` 需要明确的用户确认，通常是高影响或需要人工拍板但不涉及工具权限提权的场景。
- LLM 不能自己生成 `tool_approval`，这个类别只由运行时在破坏性工具被拦截时自动生成。
- LLM 也不能自己生成 `permission_escalation_required`；当 `workspace-write` 命中破坏性调用时，这个 runtime-only 暂停会由运行时自动生成。

`request_decision` 的 packet 里必须是摘要化信息，尤其是 `relevant_findings` 和 `key_tradeoffs`，不能粘贴原始工具输出。

---

## 4. 运行主循环

Work 的基本执行路径如下：

```text
delegate_to_work
  -> ValidateAndComplete / 任务契约校验
  -> Open journal
  -> Runtime.Run
  -> 可能的 context compression
  -> LLM 工具循环
     - 提取本轮 tool calls
     - 先整体 ClassifyCall
     - request_decision / finish_task 协议工具处理
     - permission_escalation_required 拦截
     - tool_approval 审批门控拦截
     - 普通工具执行
  -> TaskReport 或 PausedWork
```

要点：

- Work 从空消息历史开始，Emotion 历史不会注入。
- 每轮都会检查上下文长度，必要时先压缩，再判断是否超过最大输入限制。
- 如果 LLM 不返回工具调用，则直接解析最终文本为任务结果。
- 每轮真实执行前，运行时会先对本轮所有普通工具调用执行 `Dispatcher.ClassifyCall`，再根据分类结果决定暂停、报错或执行。
- `finish_task` 和 `request_decision` 是协议工具，必须是本轮唯一工具调用；若与其他工具混用，本轮不会执行任何真实工具，只会把协议错误返回给模型。
- 当任一工具调用分类为 `permission_escalation_required` 或 `tool_approval_required` 时，运行时会暂停并保存被拦截的调用，不执行同轮 sibling 工具。

---

## 5. 暂停、恢复与持久化

暂停状态由 `PendingRegistry` 持久化到 SQLite，而不是只保留在内存里。

### `PendingRegistry`

`PendingRegistry` 负责：

- 保存 paused work 的 resume blob
- 记录 pending / stale / expired_open / auto_rejected / resolved 等生命周期状态
- 对可恢复任务做 claim，避免并发恢复
- 把终态任务归档到 `archived_decisions`

它的持久化内容包括：

- `PausedWork`
- `DecisionPacket`
- 进度摘要
- pending tool call（如果是审批拦截导致的暂停）

### `resume_work`

`resume_work` 是恢复 Work 的统一入口。它在工具注册上仍属于 Emotion scope，但实际调用方分两类：

- 普通暂停 / 提权暂停：由 Emotion 在理解用户答复后调用
- 审批门控暂停：由系统执行层在收到 `approval_action` 后直接携带 `approval_request_id` 调用

- 普通暂停：提供 `task_id` + `decision` / `reason` / `constraints_delta`
- 提权暂停：对 `permission_escalation_required` 提供 `task_id` + 用户的 `decision` / `reason`；若用户批准，还要附带 `permission_scope_override="approved-destructive"`
- 审批门控暂停：提供 `task_id` + `approval_request_id`

审批门控恢复时，运行时会根据审批结果决定：

- `approved`：执行之前冻结的 `PendingToolCall`
- `rejected`：注入拒绝结果，不执行那个工具调用

这就是“持有破坏性调用，等审批回来再继续”的实现方式。当前 WebSocket / chat engine 会在用户点击审批后直接走这条恢复链路，而不是再等待一轮 Emotion LLM 生成 `resume_work` 调用。

如果这次内部恢复已经返回终态 `TaskReport`（`completed` / `failed` / `partial`），Chat Engine 后续只让 Emotion 组织对外表达，并禁用该叙述轮的工具集，避免模型在已经完成的任务上再次调用 `delegate_to_work`。

---

## 6. 破坏性工具提权与审批

工具权限判定现在只有一个入口：`Dispatcher.ClassifyCall`。它一次完成：

- registry spec / handler lookup
- JSON Schema 参数校验
- `Spec.DestructiveClassifier` 输入级破坏性判定
- `permission_scope` 与工具实际所需权限比较
- active approval context 检查

`ClassifyCall` 返回 `CallClassification`，其中 `CallAction` 决定后续行为：执行、普通错误、权限拒绝、`permission_escalation_required` 暂停，或 `tool_approval_required` 暂停。旧的 `WouldNeedApproval` / `WouldNeedPermissionEscalation` 双预判链路已经移除。

破坏性规则由工具自己声明，而不是由 dispatcher 硬编码工具名。当前 `bash` 在 `builtin/bash.go` 内声明 `DestructiveClassifier`，会把删除、覆盖、移动等命令升级为 `approved-destructive`；`write_file` / `edit_file` 暂未新增输入级 classifier。

当 Work 产生的普通工具调用需要破坏性权限，而当前 scope 只有 `workspace-write` 时，运行时不会让 LLM 自己发明一个 `request_decision` 或 `tool_approval`。

运行时会直接：

- 识别被阻塞的工具调用
- 生成 runtime-only 的 `permission_escalation_required` 暂停
- 把被阻塞的调用保存到 `PendingToolCall`
- 将 paused work 写入 `PendingRegistry`
- 等待 Emotion 先向用户发起人工决策请求，再通过 `resume_work` 恢复

恢复时：

- 如果用户拒绝，运行时会向 Work 注入一个“未执行”的错误工具结果
- 如果用户批准，Emotion 必须带 `permission_scope_override="approved-destructive"` 恢复；运行时会更新本次任务的有效 scope，并带着批准上下文执行冻结的工具调用

只有任务已经处于 `approved-destructive` 路径，并且具体工具调用仍然需要审批门控时，运行时才会生成 `tool_approval`。这一类暂停会创建 `approval_request_id`，走独立的审批恢复链路：审批请求由交互层直接展示，用户点击后由系统执行层按 `approval_request_id` 直接恢复 Work，再把恢复结果交回 Emotion 做对外表达。

这样可以保证：

- 提权是否继续，始终由用户拍板，Emotion 只负责转述和恢复
- `tool_approval` 仍然保留给真正的审批门控场景
- `tool_approval` 的恢复不再依赖额外一轮 Emotion LLM，从而减少审批窗口出现后的点击等待
- 模型不会把权限升级伪装成普通 `human_confirmation`

---

## 7. 进度回调

Work 现在支持进度回调，事件类型包括：

- `start`
- `tool`
- `heartbeat`
- `finishing`
- `paused`
- `end`

典型语义是：

- `start`：任务开始
- `tool`：执行普通工具
- `heartbeat`：长任务运行中，向外部发出存活信号
- `finishing`：即将提交最终结果
- `paused`：任务暂停等待决策
- `end`：任务完成并结束生命周期

这部分让上层可以稳定感知 Work 的执行阶段，而不是只等最终返回。

---

## 8. 上下文压缩

当前实现不再依赖简单的 `MaxInputTokens` 截断，而是会在接近上限时先做内部压缩。

### 两层压缩

- **执行期压缩**：在每轮模型调用前，运行时会尝试把较早轮次压成结构化 `WorkProgress`
- **暂停前压缩**：在生成 `PausedWork` 之前，还会把暂停快照压缩到可恢复的预算内

### 结果

- 历史对话不会无限增长
- 重要的进展会被保留在结构化进度里
- 暂停后的恢复包更轻，SQLite 负担更小

如果压缩后仍超过 `MaxInputTokens`，运行时会返回 `partial`，而不是硬塞一个超限上下文。

---

## 9. 日志与审计

journals/logs 仍然存在，用于保留 Work 的运行轨迹。

当前 journal 以 JSONL 形式落盘，按日分目录保存，文件路径形如：

`logs/work/YYYY-MM-DD/<task_id>.jsonl`

常见事件包括：

- `task_start`
- `tool_call`
- `tool_result`
- `decision_request`
- `decision_response_runtime`
- `decision_response_emotion`
- `permission_escalation_intercepted`
- `tool_approval_intercepted`
- `work_context_compressed`
- `task_paused`
- `task_resumed`
- `task_end`
- `task_error`

日志里的工具结果会做摘要截断，避免把超长内容直接灌进审计文件。

---

## 10. 主要文件映射

### `internal/work/`

| 文件 | 作用 |
|------|------|
| [`runtime.go`](../../internal/work/runtime.go) | 主执行循环、工具调度、决策路由、审批拦截 |
| [`context.go`](../../internal/work/context.go) | Work system prompt 和权限边界 |
| [`finish_task.go`](../../internal/work/finish_task.go) | `finish_task` 控制工具与完成负载解析 |
| [`request_decision.go`](../../internal/work/request_decision.go) | `request_decision` 控制工具 |
| [`resume_tool.go`](../../internal/work/resume_tool.go) | `resume_work` 的 Emotion 侧恢复入口 |
| [`pending.go`](../../internal/work/pending.go) | SQLite 版暂停注册表与生命周期管理 |
| [`pending_store.go`](../../internal/work/pending_store.go) | 暂停快照、审批摘要、状态模型 |
| [`approval.go`](../../internal/work/approval.go) | 审批请求的创建、批准、拒绝、消费 |
| [`compact.go`](../../internal/work/compact.go) | 暂停快照压缩与决策 packet 压缩 |
| [`progress.go`](../../internal/work/progress.go) | Work 进度摘要结构与回调事件 |
| [`journal.go`](../../internal/work/journal.go) | JSONL 日志写入 |

### `internal/tool/`

| 文件 | 作用 |
|------|------|
| [`dispatch.go`](../../internal/tool/dispatch.go) | `ClassifyCall` 单入口分类、权限判定、执行分发 |
| [`spec.go`](../../internal/tool/spec.go) | 工具定义、权限等级、`DestructiveClassifier` 扩展点 |

### `internal/tool/builtin/`

Work 现在可以接入更完整的工具集，按注册表和权限范围分配能力，而不再只是单一只读文件工具。

- [`bash.go`](../../internal/tool/builtin/bash.go) 声明 bash 工具及其破坏性命令 classifier。

### `internal/chat/`

| 文件 | 作用 |
|------|------|
| [`engine.go`](../../internal/chat/engine.go) | 用户消息主循环、审批动作后的内部恢复、终态报告后的无工具叙述轮 |

---

## 11. 结论

当前 Work 已经是一个可恢复、可决策、可审批、可审计的运行时。它的核心能力不只是“读文件并返回文本”，而是：

- 在受限权限下执行任务
- 在需要时主动升级决策
- 在审批门控下安全暂停
- 通过 SQLite 保持跨轮恢复能力
- 通过 progress callbacks 和 journals 保持可观测性
