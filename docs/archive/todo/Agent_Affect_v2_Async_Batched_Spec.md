# Agent Affect v2 异步后置与批量合并评估 Spec

> **Document status**: Implementation Spec Draft  
> **Version**: 0.2  
> **Date**: 2026-06-10  
> **Target path suggestion**: `docs/architecture/Agent_Affect_v2_Async_Batched_Spec.md`  
> **Scope**: 本文定义 Agent Affect v2 从“回复前同步 evaluate”改造成“回复前只读已提交 mood、回复后异步入队、后台按 Persona/mood owner 合并连续 job 后一次 evaluate”的实施方案。同时调整 mood 作用域与 Prompt 注入方式：默认 persona-scoped mood，聊天 LLM 默认只接收自然语言 mood prompt，不再接收完整 mood vector。

---

## 0. 背景与问题

当前 Agent Affect v2 MVP 已经具备：

- root-level `agent_affect` 配置；
- SQLite `agent_affect_*` 表；
- `GetCurrentMood / EvaluateMoodImpact / SubmitMoodImpact / ApplyMoodDelta / ResetMood / PreviewPrompt` 等 API；
- Admin 控制台；
- Turn Pipeline 中的 Prompt Block 注入。

实际体验暴露出三个主要问题：

1. **首字延迟过大**：当前正常聊天时，Agent Affect LLM evaluate 在用户发出消息之后、主回复 LLM 开始之前同步执行，导致首字输出前多等一次 affect evaluate。
2. **mood 作用域过窄**：当前 mood 查询按 `persona_id + session_id` 取最新状态，但陪伴型 Agent 的 mood 更应该默认跨 session 延续。
3. **数值型 Prompt 影响有限**：聊天 LLM 看到 mood vector 后不一定能自然转化为表达风格。更适合注入一句自然语言心情描述与原因。

本文目标是把 Agent Affect 从“当前轮同步表达调节器”转成“异步沉淀的 Agent 心境运行时”：

```text
本轮回复使用上一轮已提交 mood；
本轮结束后异步评估完整 turn 对 mood 的影响；
连续 pending turns 可合并成一个 batch，一次 LLM 调用产生一个 mood transition；
下一轮对话使用最新已提交 mood。
```

---

## 1. 设计结论

### 1.1 新主链路

```text
User message
  ↓
memory_prepare / memory_retrieve
  ↓
Agent Affect: GetCurrentMood only
  ↓
Build natural-language Agent Mood Prompt Block
  ↓
Main reply LLM starts immediately
  ↓
assistant reply committed
  ↓
Enqueue agent_affect_job
  ↓
Background worker claims pending jobs by mood owner
  ↓
If multiple contiguous jobs share same mood owner, coalesce into one batch
  ↓
One LLM evaluate for the batch
  ↓
One evaluation + one state + one event committed
  ↓
All jobs in batch marked done
```

### 1.2 核心原则

```text
1. Chat hot path must not wait for Agent Affect LLM evaluate.
2. Agent Affect jobs are durable and retryable.
3. Jobs for the same mood owner are applied in chronological order.
4. Multiple contiguous jobs for the same mood owner may be merged into one LLM request.
5. One merged batch produces exactly one mood transition.
6. Reset/manual delta/config barrier supersedes or breaks older pending jobs.
7. Chat prompt defaults to natural mood text, not numeric vector.
8. Numeric vector remains available for Admin/debug only.
```

---

## 2. Terminology

### 2.1 Mood Owner

`MoodOwner` 是 mood 状态的真正拥有者。第一版默认：

```text
mood_owner_scope = persona
mood_owner_id    = persona:<persona_id>
```

未来如果接入多用户/多平台身份，可扩展为：

```text
mood_owner_scope = persona_user
mood_owner_id    = persona:<persona_id>:user:<stable_user_id>
```

不要把 `session_id` 直接当成默认 mood owner。Session 是事件来源和历史过滤条件，不是 Agent 心情的默认边界。

### 2.2 State Scope

配置：

```yaml
agent_affect:
  state:
    scope: persona        # persona | session
    recent_context_scope: persona
```

语义：

| scope | 状态 key | 行为 |
|---|---|---|
| `persona` | `session_id = NULL` 或 state_key=`persona:<persona_id>` | 同一 Persona 跨 session 共享 mood。默认推荐。 |
| `session` | `session_id = <session_id>` 或 state_key=`session:<session_id>` | 保持旧行为，每个 session 独立 mood。 |

### 2.3 Affect Job

`agent_affect_jobs` 是回复后异步生成的待评估任务。一个 job 通常对应一个完整 turn：用户输入 + Agent 回复 + 相关记忆上下文。

### 2.4 Affect Batch

`agent_affect_job_batches` 是 worker 消费时形成的批次。一个 batch 包含同一 mood owner 下按时间连续的一组 jobs。一个 batch 只调用一次 LLM evaluate，并最终只产生一次 mood transition。

### 2.5 Contiguous Jobs

“连续 jobs”定义为：

```text
- same mood_owner_scope
- same mood_owner_id
- same job_type family, e.g. turn_evaluate
- status = pending and run_after <= now
- ordered by queue_seq ascending
- no older pending/running job for same mood owner is skipped
- no reset/manual_delta/config_barrier between first and last job
```

如果中间出现 barrier，batch 必须在 barrier 前截断。barrier 之后的 jobs 等下一轮 claim。

---

## 3. 配置设计

建议扩展 root `agent_affect`：

```yaml
agent_affect:
  enabled: true

  update_mode: async_after_reply
  # sync_before_reply: 旧模式，仅 debug / 对比用
  # async_after_reply: 默认新模式

  state:
    scope: persona
    recent_context_scope: persona

  async:
    enabled: true
    queue_enabled: true
    worker_enabled: true
    worker_concurrency: 1
    poll_interval_ms: 800
    queue_claim_ttl_seconds: 300
    max_attempts: 3
    retry_base_delay_seconds: 30
    retry_max_delay_seconds: 900
    clear_raw_after_done: true

    batch:
      enabled: true
      max_jobs: 6
      max_input_tokens: 12000
      max_age_seconds: 300
      min_wait_ms: 0
      merge_across_sessions: true
      break_on_manual_barrier: true
      summarize_turns_before_llm: false

  prompt:
    mode: natural_summary       # natural_summary | numeric_debug | both
    include_mood_block: true
    include_numeric_values: false
    include_reason: true
    max_prompt_chars: 240
```

默认建议：

```text
update_mode = async_after_reply
state.scope = persona
async.batch.enabled = true
prompt.mode = natural_summary
prompt.include_numeric_values = false
```

---

## 4. 数据库迁移设计

建议新增主仓库 SQLite migration，例如 v22 或下一可用版本。

### 4.1 扩展 `agent_affect_states`

```sql
ALTER TABLE agent_affect_states ADD COLUMN mood_description TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_affect_states ADD COLUMN mood_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_affect_states ADD COLUMN prompt_mood_text TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_affect_states ADD COLUMN mood_owner_scope TEXT NOT NULL DEFAULT 'session';
ALTER TABLE agent_affect_states ADD COLUMN mood_owner_id TEXT NOT NULL DEFAULT '';
```

索引：

```sql
CREATE INDEX IF NOT EXISTS idx_agent_affect_states_owner_current
    ON agent_affect_states(persona_id, mood_owner_scope, mood_owner_id, updated_at DESC);
```

兼容策略：

```text
旧 session-scoped rows 可以保留。
读取时如果 mood_owner_id 为空，则按旧逻辑 fallback。
新写入必须填 mood_owner_scope/mood_owner_id。
```

### 4.2 扩展 `agent_affect_evaluations`

```sql
ALTER TABLE agent_affect_evaluations ADD COLUMN mood_description TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_affect_evaluations ADD COLUMN mood_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_affect_evaluations ADD COLUMN prompt_mood_text TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_affect_evaluations ADD COLUMN batch_id TEXT;
ALTER TABLE agent_affect_evaluations ADD COLUMN mood_owner_scope TEXT NOT NULL DEFAULT 'session';
ALTER TABLE agent_affect_evaluations ADD COLUMN mood_owner_id TEXT NOT NULL DEFAULT '';
```

### 4.3 扩展 `agent_affect_events`

```sql
ALTER TABLE agent_affect_events ADD COLUMN mood_description TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_affect_events ADD COLUMN mood_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_affect_events ADD COLUMN prompt_mood_text TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_affect_events ADD COLUMN batch_id TEXT;
ALTER TABLE agent_affect_events ADD COLUMN mood_owner_scope TEXT NOT NULL DEFAULT 'session';
ALTER TABLE agent_affect_events ADD COLUMN mood_owner_id TEXT NOT NULL DEFAULT '';
```

### 4.4 新增 `agent_affect_jobs`

```sql
CREATE TABLE IF NOT EXISTS agent_affect_jobs (
    seq INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE,

    persona_id TEXT NOT NULL,
    session_id TEXT,
    turn_id TEXT,

    mood_owner_scope TEXT NOT NULL,
    mood_owner_id TEXT NOT NULL,

    job_type TEXT NOT NULL DEFAULT 'turn_evaluate'
        CHECK (job_type IN ('turn_evaluate','plugin_evaluate','manual_evaluate','barrier')),
    batchable INTEGER NOT NULL DEFAULT 1 CHECK (batchable IN (0,1)),
    barrier_kind TEXT NOT NULL DEFAULT '',

    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','running','done','failed','superseded')),
    priority INTEGER NOT NULL DEFAULT 100,
    run_after TEXT NOT NULL,

    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    claimed_by TEXT,
    claimed_until TEXT,

    trigger_json TEXT NOT NULL DEFAULT '{}',
    input_mode TEXT NOT NULL DEFAULT 'mixed'
        CHECK (input_mode IN ('raw','summary','mixed','none')),
    user_text TEXT,
    assistant_text TEXT,
    input_summary TEXT,
    memory_prompt_block TEXT,

    base_state_id TEXT,
    base_state_updated_at TEXT,

    batch_id TEXT,
    result_evaluation_id TEXT,
    result_event_id TEXT,

    error_message TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    started_at TEXT,
    finished_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_agent_affect_jobs_claim
    ON agent_affect_jobs(status, run_after, priority, seq);

CREATE INDEX IF NOT EXISTS idx_agent_affect_jobs_owner_status
    ON agent_affect_jobs(mood_owner_scope, mood_owner_id, status, seq);

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_affect_jobs_turn_unique
    ON agent_affect_jobs(turn_id, job_type)
    WHERE turn_id IS NOT NULL AND job_type = 'turn_evaluate';
```

### 4.5 新增 `agent_affect_job_batches`

```sql
CREATE TABLE IF NOT EXISTS agent_affect_job_batches (
    id TEXT PRIMARY KEY,
    persona_id TEXT NOT NULL,
    mood_owner_scope TEXT NOT NULL,
    mood_owner_id TEXT NOT NULL,

    job_type TEXT NOT NULL DEFAULT 'turn_evaluate',
    status TEXT NOT NULL DEFAULT 'running'
        CHECK (status IN ('running','done','failed','superseded')),

    job_count INTEGER NOT NULL DEFAULT 0,
    first_job_seq INTEGER NOT NULL,
    last_job_seq INTEGER NOT NULL,
    job_ids_json TEXT NOT NULL DEFAULT '[]',
    session_ids_json TEXT NOT NULL DEFAULT '[]',
    turn_ids_json TEXT NOT NULL DEFAULT '[]',

    batch_input_summary TEXT NOT NULL DEFAULT '',
    context_window_snapshot_json TEXT,

    evaluation_id TEXT,
    affect_event_id TEXT,
    error_message TEXT,

    claimed_by TEXT,
    started_at TEXT NOT NULL,
    finished_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_agent_affect_batches_owner_time
    ON agent_affect_job_batches(mood_owner_scope, mood_owner_id, started_at DESC);
```

---

## 5. Mood Owner Resolver

新增：

```go
type MoodOwner struct {
    Scope string
    ID    string
}

func ResolveMoodOwner(cfg config.AgentAffectConfig, personaID, sessionID string) MoodOwner {
    switch cfg.State.Scope {
    case "session":
        return MoodOwner{Scope: "session", ID: "session:" + sessionID}
    default:
        return MoodOwner{Scope: "persona", ID: "persona:" + personaID}
    }
}
```

状态查询：

```go
GetCurrentMood(personaID, sessionID):
  owner := ResolveMoodOwner(cfg, personaID, sessionID)
  store.GetLatestStateByOwner(personaID, owner)
```

兼容旧 session_id 查询：

```text
如果新 owner 查询不到，且 cfg.state.scope=session，则 fallback 当前旧 GetLatestState(personaID, sessionID)。
如果 cfg.state.scope=persona，新状态写入 session_id=NULL，真实 session_id 只保留在 evaluations/events/jobs。
```

---

## 6. Turn Pipeline 改造

### 6.1 `emotion_prepare`: 不再同步 evaluate

当前 `emotion_prepare` 应改为：

```text
1. retrieve memory prompt block
2. GetCurrentMood(persona, session)
3. BuildPromptAffectBlock(mood)
4. diagnostics["agent_affect_snapshot"] = mood
5. diagnostics["agent_affect_prompt_block"] = block
```

明确删除热路径：

```go
agentAffect.SubmitMoodImpact(...)
```

除非配置：

```yaml
agent_affect.update_mode: sync_before_reply
```

保留旧模式只用于 debug / A/B 对比，不作为默认。

### 6.2 `memory_commit` 或 turn completion: enqueue job

在助手回复内容可用后入队：

```text
1. assistantContent available
2. userContent available
3. memoryPromptBlock available
4. current mood state id available if possible
5. enqueue agent_affect_job
6. do not wait for worker/evaluator
```

入队 DTO：

```go
type EnqueueTurnEvaluationJobRequest struct {
    PersonaID         string
    SessionID         string
    TurnID            string
    UserText          string
    AssistantText     string
    MemoryPromptBlock string
    Trigger           TriggerDescriptor
    BaseStateID       string
}
```

如果 `agent_affect.async.queue_enabled=false`，则不入队。

---

## 7. 批量合并消费设计

### 7.1 Claim 策略

Worker 每次不是简单 claim 一条，而是：

```text
1. 找到最早 eligible pending job。
2. 取它的 mood_owner_scope / mood_owner_id 作为本轮 owner。
3. 检查同 owner 是否已有 running job；有则跳过。
4. 从该 owner 的 pending jobs 中按 seq 升序取连续 batchable jobs。
5. 遇到 barrier / 非 batchable / 超 token / 超 max_jobs / 超 max_age_seconds 即停止。
6. 创建 agent_affect_job_batches。
7. 把这些 jobs 标记 running + batch_id。
```

伪代码：

```go
func ClaimNextBatch(ctx, workerID string, now time.Time) (*AffectJobBatch, error) {
    tx := begin()
    first := selectOldestEligiblePendingJob(tx)
    if first == nil { commit; return nil }

    if existsRunningJobForOwner(tx, first.Owner) {
        commit
        return nil
    }

    jobs := selectContiguousPendingJobsForOwner(tx, first.Owner, cfg.Batch)
    batch := createBatch(tx, jobs, workerID)
    markJobsRunning(tx, jobs, batch.ID, workerID, claimedUntil)
    commit
    return batch
}
```

### 7.2 合并输入格式

LLM evaluator 输入应明确说明这是 batch：

```text
You are evaluating the combined mood impact of a chronological batch of completed turns.
Do not output per-turn deltas.
Output one consolidated mood transition for the Agent after absorbing the whole batch.
```

User prompt 中提供：

```json
{
  "batch": {
    "job_count": 3,
    "mood_owner_id": "persona:default",
    "turns": [
      {
        "turn_id": "...",
        "session_id": "...",
        "user_text_or_summary": "...",
        "assistant_text_or_summary": "...",
        "memory_context_summary": "..."
      }
    ]
  },
  "current_mood_before_batch": {},
  "recent_evaluations": [],
  "dimension_limits": {}
}
```

### 7.3 Batch 结果

一个 batch 成功后：

```text
- insert one agent_affect_evaluation
- insert one agent_affect_state
- insert one agent_affect_event
- mark batch done
- mark all jobs done with same evaluation_id / event_id / batch_id
```

Evaluation trigger 建议：

```json
{
  "trigger_type": "customize",
  "custom_type": "turn_batch",
  "custom_type_desc": "Coalesced chronological completed chat turns for one mood owner.",
  "source_kind": "agent_affect_job_batch",
  "source_ref_type": "agent_affect_job_batch",
  "source_ref_id": "<batch_id>"
}
```

Event 的 `session_id` 可为空或使用第一条 job 的 session_id。建议：

```text
agent_affect_events.session_id = NULL if batch spans multiple sessions.
event.turn_id = NULL if batch spans multiple turns.
batch table stores exact turn_ids/session_ids.
```

---

## 8. Barrier 与乱序处理

### 8.1 Barrier 来源

以下操作应产生 barrier 或 supersede pending jobs：

```text
manual reset
manual ApplyMoodDelta
profile baseline change
state scope change
config change that affects limits/prompt/evaluator semantics
admin supersede pending
```

第一版推荐：

```text
ResetMood / ApplyMoodDelta 默认把同 owner 的 pending jobs 标记 superseded。
Running jobs 在提交前检查 batch 是否已被 superseded；如已 superseded，不写 state/event。
```

### 8.2 Running job 提交检查

Worker 在 commit 前检查：

```text
1. batch.status still running
2. all jobs status still running
3. no newer reset barrier affects this owner since batch.started_at
4. latest state is still compatible enough with batch base state
```

第一版可以先实现 1–3。后续再加 optimistic `base_state_id` check。

### 8.3 用户连续发消息时的语义

如果新消息来时旧 batch 未完成：

```text
- 当前回复仍使用最新 committed mood。
- 不等待 pending batch。
- diagnostics/admin 可显示 pending affect jobs。
- worker 后台追赶，下一轮或再下一轮使用更新后的 mood。
```

这是预期行为，不是 bug。

---

## 9. LLM Evaluator 输出升级

新增字段：

```json
{
  "schema_version": "agent_affect.v2.evaluation.v2",
  "delta": {
    "valence": 0,
    "arousal": 0,
    "dominance": 0,
    "energy": 0,
    "warmth": 0,
    "concern": 0,
    "curiosity": 0,
    "playfulness": 0,
    "attachment": 0,
    "frustration": 0,
    "uncertainty": 0
  },
  "label": "steady",
  "mood_description": "心情平稳、温和",
  "mood_reason": "没有明显改变心情的事件",
  "prompt_mood_text": "当前模拟心情：平稳、温和，没有明显额外波动。",
  "cause_summary": "Internal audit summary.",
  "visible_cause_summary": "Safe visible cause summary.",
  "confidence": 0.5
}
```

字段语义：

| 字段 | 用途 |
|---|---|
| `delta` | 内部 mood vector 更新。 |
| `label` | Admin/debug 标签。 |
| `mood_description` | 人类可读心情形容。 |
| `mood_reason` | 心情原因。 |
| `prompt_mood_text` | 默认唯一注入聊天 LLM 的核心内容。 |
| `cause_summary` | 内部审计原因。 |
| `visible_cause_summary` | 可见安全原因。 |

Parser 要兼容旧字段：

```text
若缺 prompt_mood_text，则用 mood_description + mood_reason 生成 fallback。
若缺 mood_description，则由 label / vector derive。
```

---

## 10. Prompt 注入新样式

默认 Prompt Block：

```text
[Agent Mood]
当前模拟心情：{{prompt_mood_text}}

这是内部表达背景：不要逐字复述，不要提到 mood 系统、内部状态表或数值；只让它自然影响措辞、节奏和亲近感。
```

不再默认注入：

```text
valence
arousal
warmth
attachment
frustration
完整 mood_vector
```

Debug 模式：

```yaml
agent_affect:
  prompt:
    mode: numeric_debug
    include_numeric_values: true
```

才输出旧的数值 block。

---

## 11. Admin UI 改造

### 11.1 当前 Mood 区域

默认显示：

```text
当前心情：{{mood_description}}
原因：{{mood_reason 或 visible_cause_summary}}
Prompt 注入：{{prompt_mood_text}}
Mood Owner: persona:default
更新时间
```

数值 mood vector 放到折叠的 Debug 区域。

### 11.2 Queue / Batch 状态

新增：

```text
pending jobs
running jobs
failed jobs
latest batch
batch job count
last worker error
```

按钮：

```text
刷新队列
立即处理一次 pending
清理 failed
Supersede pending for this mood owner
```

### 11.3 Session ID 语义调整

当 `state.scope=persona`：

```text
Session ID 输入框改名为 Session Filter，可为空。
当前 mood 不再由 session_id 决定。
```

---

## 12. 测试计划

### 12.1 Go 单元测试

必须覆盖：

```text
ResolveMoodOwner persona/session mode
FormatPromptAffectBlock natural_summary no numeric vector
ParseLLMResponse accepts mood_description/mood_reason/prompt_mood_text
Clamp still applies to batch delta
```

### 12.2 Store / migration 测试

必须覆盖：

```text
agent_affect_jobs table created
agent_affect_job_batches table created
enqueue job assigns increasing seq
claim batch merges contiguous same-owner jobs
different owners do not merge
barrier breaks batch
mark done updates all jobs in batch
reset supersedes pending jobs
```

### 12.3 Turn runtime 测试

必须覆盖：

```text
emotion_prepare does not call SubmitMoodImpact in async_after_reply mode
emotion_prepare calls GetCurrentMood + BuildPromptAffectBlock
successful assistant reply enqueues one job
failed/no-output turn does not enqueue
sync_before_reply compatibility mode still works if retained
```

### 12.4 Worker 测试

必须覆盖：

```text
three pending jobs same persona -> one LLM call -> one evaluation/state/event
jobs from two personas -> two batches / two LLM calls
running older job blocks newer same owner claim
failed evaluate retries with backoff
clear_raw_after_done removes user_text/assistant_text if configured
```

### 12.5 Frontend 测试

必须覆盖：

```text
web typecheck
Agent Affect tab displays mood_description/prompt_mood_text
state.scope selector exists
prompt.mode selector exists
queue status loads
numeric vector debug collapsible or secondary
```

---

## 13. 实施顺序

### Step 1: Config + DTO + Prompt natural summary

- 增加 `state`, `async`, `prompt.mode`, `prompt.max_prompt_chars` config。
- 增加 `mood_description`, `mood_reason`, `prompt_mood_text` DTO。
- Parser 支持新 evaluator JSON。
- Prompt Block 默认 natural summary。

### Step 2: Persona-scoped mood

- 增加 `MoodOwner` resolver。
- `GetCurrentMood` 使用 mood owner 查询。
- 新 state 写入 mood_owner_scope / mood_owner_id。
- 保留 session mode 兼容。

### Step 3: Queue schema + store

- 新增 `agent_affect_jobs` / `agent_affect_job_batches`。
- 实现 enqueue / claim batch / mark done / mark failed / supersede。

### Step 4: Turn Pipeline 改造

- `emotion_prepare` 改为只读 mood。
- 回复后 enqueue job。
- 不等待 worker。

### Step 5: Worker + batch evaluator

- 后台 worker 启动/关闭。
- 合并 contiguous jobs。
- batch 一次 LLM evaluate。
- 一次 commit state/evaluation/event。

### Step 6: Admin UI

- 显示 natural mood。
- 显示 mood owner scope。
- 显示 queue/batch status。
- 数值区降级为 debug。

---

## 14. 验收标准

功能验收：

```text
1. 正常聊天首字不等待 Agent Affect evaluate。
2. 回复后产生 agent_affect_job。
3. Worker 能把同 Persona 连续 pending jobs 合并成一个 batch。
4. 一个 batch 只产生一个 evaluation/state/event。
5. 同 Persona 不同 session 默认共享 mood。
6. session scope 仍可通过配置恢复旧行为。
7. 聊天 Prompt 默认只注入自然语言 mood prompt。
8. Admin 能看到 queue/batch/history/current mood。
```

命令验收：

```bash
gofmt -w <changed go files>
go test ./...
go build ./cmd/emoagent
npm --prefix web run typecheck
npm --prefix web run build
```

---

## 15. 非目标

本轮不做：

```text
Agent Life Timeline
Daily News Digest
Autonomous Workspace
屏幕/进程观察
MemoryCore schema 修改
复杂情绪心理模型
多模态 affect
```

