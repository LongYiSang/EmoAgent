# 结论先行

95e1132 已经把 Turn Pipeline 的运行时骨架落下来了，但它现在还是 **in-memory、单机、不可恢复、弱 rollout、弱 replay 的骨架**。报告里的前提是“先把消息流固化成可插拔、可回放、可观察、可退化的统一管线，再围绕这条管线做插件 SDK、Hook、权限模型与隔离宿主”。 当前提交也明确标注这是阶段性提交，范围限定在消息流重构，并且 `PASS_WITH_GAPS`。

我的建议是：

```text
先补 Turn Pipeline Runtime Hardening：
  SQLite/JSONL journal
  SQLite idempotency
  rollout allowlist/percent
  duplicate-running busy/status/replay
  memory_stages / approval_stages 真正生效
  bounded outbound + replay summary

然后进入 PluginHost Alpha：
  先 builtin plugin + Typed Hook
  再 process runner
  再 capability / manifest / 热加载 / 灰度
```

---

# 1. 当前代码分析：95e1132 已完成什么

## 1.1 Turn Contract 已经存在

当前新增的 `internal/turn` 已经定义了统一入口来源、输入类型、Turn 状态、Stage 名称、Stage 接口、StageResult 和 OutboundEvent。`InboundKind` 已覆盖 `user_message / approval_action / system_resume`，TurnState 也已覆盖 `created / normalizing / memory_prepared / running_emotion / approval_wait / done / failed / commit_failed_after_output` 等状态。

这说明第一阶段的核心抽象已经基本对齐 Spec。

## 1.2 Runtime 是顺序执行器，但仍是最小骨架

`turn.Runtime` 当前只持有一个 `TurnJournal`，如果没有传入 journal，就默认使用 `NewMemoryJournal()`；`Execute` 会依次执行 stages、记录 transition、发送 outbound，并在 terminal 时 complete turn。

这个设计是正确方向，但它目前还不是生产级 Turn Runtime，因为：

```text
- journal 失败直接使 Turn failed，没有 SQLite → memory fallback 的完整策略；
- 没有 stage timeout / budget；
- 没有 replay 查询接口；
- 没有跨进程/重启恢复能力；
- 没有 duplicate-running attach/status 模型。
```

## 1.3 Journal 当前确实是 in-memory

`MemoryJournal` 使用 `map[string]*TurnSnapshot` 保存 turn、transition 和 event。 它支持 `StartTurn / RecordTransition / RecordEvent / CompleteTurn`，并在 RecordEvent 时调用 `sanitizePayload`。

这说明当前 journal 适合测试和 shadow 骨架，但不适合：

```text
- 重启后 replay；
- duplicate request 精确恢复；
- rollout 行为对比；
- 插件 Hook 审计；
- 插件安全追责；
- E2E transcript fixture。
```

好的一点是，脱敏规则已经有雏形：禁止 payload key 包含 `raw_tool_output / prompt / raw_prompt / hidden_memory / file_content / chain_of_thought / sensitive_reasoning` 等，并对 `SECRET / TOKEN / PASSWORD / API_KEY` 做字符串 redaction。

## 1.4 Idempotency 当前也是 in-memory

`MemoryIdempotencyStore` 也是 map，`Begin` 遇到重复 key 时只返回 `Duplicate / TurnID / Status`，`Complete` 只更新 status。

这带来两个实际问题：

```text
1. 进程重启后幂等丢失。
2. duplicate-running 只能返回结果级 no-op，没有 busy/status/replay。
```

另外当前 `chatTurnRuntime.Execute` 只有在 `execErr == nil` 时才 `ids.Complete`，如果执行失败，idempotency 记录可能停留在 `running`。 这个需要修。

## 1.5 WS Adapter 已经抽出，但 Handler 仍保留旧分支

`wsMessageToInbound` 已经把 WebUI `message` 和 `approval_action` 转成 `InboundEnvelope`，并用 request_id 构造 idempotency key。 OutboundEvent 也能映射回现有 WSMessage type。

Handler 当前根据 `turnConfig.Shadow` 和 `turnConfig.Enabled` 切流：shadow 且未 enabled 时只记录 mock journal；enabled 时走 `turnRuntime.Execute`；否则继续旧 `SendMessage / ContinueAfterApproval` 分支。

这很好，说明切流边界没有破坏旧路径。但现在缺少 allowlist / percent rollout，只能全局开关。

## 1.6 `memory_stages / approval_stages` 目前只是配置字段

配置里确实只有四个 bool：`shadow / enabled / memory_stages / approval_stages`。

但 `chatTurnRuntime.stages()` 当前并没有根据 `memory_stages` 或 `approval_stages` 选择不同 stage 切分；普通消息仍是 `normalize → messageStage → emitApprovals`，审批动作是 `normalize → approvalApply → resume → emitApprovals`。

也就是说：

```text
enabled = 是否走 TurnRuntime
memory_stages = 尚未真正拆 MemoryPrepare / MemoryCommit
approval_stages = 尚未真正控制审批 stage 是否启用
```

## 1.7 Engine 拆分已有第一刀，但还没有真正 Stage 化

`sendTurn` 已经抽出了：

```text
prepareInputAndMemoryAnchor
retrieveMemoryPrompt
commitTurnOutput
```

`prepareInputAndMemoryAnchor` 负责 user message 落库、ensure memory segment、append user episode、session timestamp、title、manual memory notice。 `retrieveMemoryPrompt` 负责 FailOpen 语义与排除当前 user episode。 `commitTurnOutput` 负责 assistant message 落库和 append assistant episode。

这是很重要的进展。但这些 helper 仍在 `sendTurn` 内部使用，`chatTurnRuntime` 的普通消息 stage 仍直接调用 `engine.SendMessage`。 因此插件还不能稳定挂到 `MemoryPrepare / EmotionPrepare / MemoryCommit` 这些细粒度阶段上。

## 1.8 Outbound 类型已统一，但发送仍是直写

OutboundEvent 类型已经覆盖现有 WS 事件：`stream_start / stream_delta / stream_end / tool_call_* / reasoning_* / work_progress / approval_* / error`。

但 `OutboundSink` 只是一个同步 `Emit` 接口。 Handler 里的 WS sink 直接写 WS，失败就 cancel。 这还没有 bounded channel、delta 合并、backpressure、replay buffer。

---

# 2. 是否需要继续实施这些 gap？

需要。优先级如下：

| Gap                                  |     是否阻塞插件代码实现 | 原因                                                      |
| ------------------------------------ | -------------: | ------------------------------------------------------- |
| SQLite Journal                       |         **阻塞** | 插件 Hook 需要可审计、可追责、可 replay 的 Host Runtime。in-memory 不够。 |
| SQLite Idempotency                   |         **阻塞** | 插件执行可能产生副作用，重复消息不能只靠内存防重。                               |
| allowlist / percent rollout          |       **阻塞灰度** | 插件和 Turn Pipeline 都需要按 persona/session/百分比切流。           |
| memory_stages / approval_stages 真正生效 | **阻塞 Hook 语义** | 如果 Stage 不真实，Hook 名称就是假的。                               |
| duplicate-running busy/status/replay |         **重要** | 没有它，前端重复提交、断线重连和插件副作用防重都会弱。                             |
| JSONL journal                        |    **非阻塞但建议做** | 对本地调试、fixture、回归 diff 很有价值。                             |
| bounded outbound / delta coalescing  |         **重要** | 插件进入 outbound 前后链后，直写 WS 会扩大性能风险。                       |

我的判断是：

```text
可以开始写插件接口设计文档和 ADR。
不建议直接实现第三方 process plugin runner。
可以做一个 builtin-only PluginHost Alpha，但前提是先补最小持久化 journal/idempotency 和 rollout。
```

---

# 3. Turn Pipeline gap 继续实施方案

## Phase A：SQLite Journal + Idempotency

### 目标

把当前 `MemoryJournal` / `MemoryIdempotencyStore` 扩展为生产可用的持久化实现，同时保留 in-memory fallback。

### 新增 migration v17

建议在当前 schema v16 后新增 migration。当前 migrations 到 v16，最后是 `memory_extraction_jobs`。

建议新增：

```sql
CREATE TABLE IF NOT EXISTS turns (
    id                TEXT PRIMARY KEY,
    idempotency_key   TEXT UNIQUE,
    source            TEXT NOT NULL DEFAULT '',
    source_event_id   TEXT NOT NULL DEFAULT '',
    kind              TEXT NOT NULL,
    session_id        TEXT NOT NULL DEFAULT '',
    persona_key       TEXT NOT NULL DEFAULT '',
    state             TEXT NOT NULL,
    status            TEXT NOT NULL,
    error_kind        TEXT NOT NULL DEFAULT '',
    error_message     TEXT NOT NULL DEFAULT '',
    started_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL,
    completed_at      TEXT
);

CREATE INDEX IF NOT EXISTS idx_turns_session_started
    ON turns(session_id, started_at DESC);

CREATE INDEX IF NOT EXISTS idx_turns_status_updated
    ON turns(status, updated_at DESC);

CREATE TABLE IF NOT EXISTS turn_events (
    id             TEXT PRIMARY KEY,
    turn_id        TEXT NOT NULL REFERENCES turns(id) ON DELETE CASCADE,
    seq            INTEGER NOT NULL,
    stage          TEXT NOT NULL,
    event_type     TEXT NOT NULL,
    payload_json   TEXT NOT NULL DEFAULT '{}',
    created_at     TEXT NOT NULL,
    UNIQUE(turn_id, seq)
);

CREATE INDEX IF NOT EXISTS idx_turn_events_turn_seq
    ON turn_events(turn_id, seq);

CREATE TABLE IF NOT EXISTS turn_outbound_events (
    id              TEXT PRIMARY KEY,
    turn_id          TEXT NOT NULL REFERENCES turns(id) ON DELETE CASCADE,
    seq              INTEGER NOT NULL,
    event_type       TEXT NOT NULL,
    payload_json     TEXT NOT NULL DEFAULT '{}',
    delivery_status  TEXT NOT NULL DEFAULT 'pending',
    created_at       TEXT NOT NULL,
    delivered_at     TEXT,
    UNIQUE(turn_id, seq)
);

CREATE INDEX IF NOT EXISTS idx_turn_outbound_turn_seq
    ON turn_outbound_events(turn_id, seq);

CREATE TABLE IF NOT EXISTS turn_idempotency (
    idempotency_key TEXT PRIMARY KEY,
    turn_id         TEXT NOT NULL REFERENCES turns(id) ON DELETE CASCADE,
    status          TEXT NOT NULL,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);
```

### Go 结构

```text
internal/turn/
  sqlite_journal.go
  sqlite_idempotency.go
  multi_journal.go
```

接口扩展：

```go
type ReplayableJournal interface {
    TurnJournal
    GetTurn(ctx context.Context, turnID string) (TurnSnapshot, bool, error)
    ListOutbound(ctx context.Context, turnID string, policy ReplayPolicy) ([]OutboundEvent, error)
}

type IdempotencyStore interface {
    Begin(ctx context.Context, key, turnID string) (IdempotencyResult, error)
    Complete(ctx context.Context, key, status string) error
}
```

### 注意点

`Begin` 必须用 SQLite 原子写入实现：

```text
INSERT idempotency_key
成功：当前请求拥有执行权
冲突：读取已有 turn_id/status
```

不要先查再插，否则并发重复提交仍可能双跑。

### 修复当前 bug

`chatTurnRuntime.Execute` 应该无论成功失败都更新 idempotency status：

```go
defer func() {
    _ = r.ids.Complete(env.IdempotencyKey, finalStatus)
}()
```

失败状态至少不能永久停留在 `running`。

---

## Phase B：JSONL Journal

### 目标

提供本地调试、回归 fixture、人工 diff 友好的 append-only 日志。

### 设计

```text
logs/turns/YYYY-MM-DD/<turn_id>.jsonl
```

每行：

```json
{
  "ts": "...",
  "turn_id": "...",
  "seq": 12,
  "stage": "emotion_loop",
  "type": "tool_call_end",
  "payload": { "tool": "delegate_to_work", "status": "completed" }
}
```

### 策略

```text
SQLite = 权威持久化
JSONL = debug / fixture
Memory = fallback / tests
```

实现 `MultiJournal`：

```text
StartTurn / RecordEvent / CompleteTurn:
  先写 SQLite
  JSONL 异步写
  JSONL 失败只 warning
```

如果 SQLite 初始化失败：

```text
默认 fail-open 到 MemoryJournal，并打 journal_degraded
可配置 fail_closed=true，用于严格测试/生产审计模式
```

---

## Phase C：Rollout allowlist / percent

### 配置建议

当前只有四个 bool。建议扩展为：

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
    deny_sessions: []

    journal:
      mode: sqlite        # memory | sqlite | jsonl | sqlite_jsonl
      jsonl_dir: ./logs/turns
      fail_closed: false

    idempotency:
      mode: sqlite        # memory | sqlite
      duplicate_done: replay_summary
      duplicate_running: busy
```

### 切流算法

```go
func ShouldUsePipeline(cfg, persona, sessionID, requestID string) bool {
    if sessionID in deny_sessions:
        return false
    if persona in allow_personas || sessionID in allow_sessions:
        return true
    if !cfg.Enabled:
        return false
    return stableHash(persona + ":" + sessionID) % 100 < rollout_percent
}
```

### 验收

```text
- allow_sessions 命中时 enabled=false 也能切入新路径；
- deny_sessions 永远回旧路径；
- rollout_percent 对同一 session 稳定；
- shadow 不影响主路径响应。
```

---

## Phase D：让 `memory_stages / approval_stages` 真正生效

### 当前问题

目前 `stages()` 不读这两个字段。

### 建议语义

```text
enabled=false:
  永远旧路径。

enabled=true, memory_stages=false:
  TurnRuntime 包住旧 engine.SendMessage，保持当前行为。

enabled=true, memory_stages=true:
  使用真实 MemoryPrepare / EmotionPrepare / EmotionLoop / MemoryCommit stage。

approval_stages=false:
  approval_action 继续旧 Handler 分支。

approval_stages=true:
  approval_action 进入 ApprovalApply / Resume / ApprovalWait stage。
```

### 需要补的接口

当前 `conversationEngine` 太偏旧接口。建议引入：

```go
type TurnEngineFacade interface {
    PrepareInputAndMemory(ctx, sessionID string, input UserInput) (MemoryAnchor, error)
    PrepareEmotionRequest(ctx, sessionID string, persona *config.Persona, anchor MemoryAnchor) (PreparedEmotionRequest, error)
    RunEmotionLoop(ctx context.Context, prepared PreparedEmotionRequest, sink turn.OutboundSink) (EmotionLoopResult, error)
    CommitTurnOutput(ctx context.Context, output TurnOutput) error
    ApplyApprovalAction(...)
    ResumeAfterApproval(...)
}
```

短期可以在 `chat.Engine` 内实现，避免大拆包。

---

## Phase E：duplicate-running / replay

### 当前行为

重复请求只返回 `TurnID / Status`，没有 busy event 或 replay。

### 建议行为

```text
duplicate + status=running:
  返回 WS error/status:
    type = "turn_status"
    status = "busy"
    turn_id = existing_turn_id
  不挂接同一个运行中流。

duplicate + status=done:
  replay sanitized outbound summary。
  如果无 outbound 持久化，则返回 final_message_id / no-op。

duplicate + approval_wait:
  replay approval_required。

duplicate + failed:
  返回 previous_failed + error_kind。
```

### 新增事件

可以先不改前端大协议，用 `error` 兼容；但建议加：

```json
{
  "type": "turn_status",
  "turn_id": "...",
  "status": "busy|done|approval_wait|failed"
}
```

---

## Phase F：Outbound hardening

### 当前问题

OutboundSink 同步直写。

### 建议补充

```text
internal/turn/outbound_buffer.go
```

功能：

```text
- 有界 channel；
- stream_delta 30ms 或 512 bytes 合并；
- work_progress 降采样；
- approval_required / stream_end / error 不丢；
- WS 失败 cancel turn；
- outbound summary 写 journal。
```

默认策略：

| 事件                  | channel 满时          |
| ------------------- | ------------------- |
| `stream_delta`      | 合并，必要时 backpressure |
| `work_progress`     | 可丢弃最新或降采样           |
| `reasoning_delta`   | 可降采样，除非 debug 开启    |
| `approval_required` | 不丢                  |
| `stream_end`        | 不丢                  |
| `error`             | 不丢                  |

---

