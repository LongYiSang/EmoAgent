# EmoAgent 异步记忆抽取与 Sidecar 同步改造 Spec

> Date: 2026-05-30  
> Scope: EmoAgent 主仓库 + EmoAgent-MemoryCore 接入层  
> Goal: 让记忆抽取从“WebSocket 断开/重连触发 + 同步阻塞对话”改为“后台异步、可定时、可手动、可观察、可重试，并在抽取成功后优雅同步 retrieval mirror”。

---

## 1. 当前问题与仓库事实

### 1.1 当前触发链路

现在的真实链路是：

```text
WebSocket 连接带 session_id
  → chat.Handler.ServeHTTP 调 ResumeSession
  → chat.Engine.ResumeSession
  → memory Bridge.RolloverSegment
  → Bridge.FinalizeSegment
  → MemoryCore EndSession
  → EmoAgent memory_segments.finalized_at 更新
  → Bridge.extractFinalizedSegment
  → Host.ExtractSessionEnd
  → MemoryCore Service.RunExtraction
  → Runner.Run
```

这解释了为什么“断开 WebSocket 后重连并发一条消息”能触发记忆抽取：WebSocket handler 会用 URL 中的 `session_id` 调 `ResumeSession`；`ResumeSession` 会 rollover 当前 memory segment；`RolloverSegment` 会 finalize 当前 segment 并启动新 segment；`FinalizeSegment` 结束 MemoryCore session 后同步调用抽取。

### 1.2 Session / Segment 关系

当前项目里的命名可以这样确认：

```text
EmoAgent chat session = UI/产品层的一整个会话。
EmoAgent memory segment = chat session 内的一段记忆切片。
MemoryCore session = 每个 memory segment 对应的底层 MemoryCore Session。
```

证据是 `memory_segments` 同时保存 `chat_session_id` 和 `memory_session_id`，且 `CreateMemorySegment` 会创建/记录一个 `memory_session_id` 并把它设置成当前 chat session 的 current memory session。

### 1.3 现有能力

MemoryCore 侧已经具备很多基础能力，不需要重写抽取器：

- `RunExtraction(ctx, RunExtractionRequest)`：同步执行一次抽取。
- `RunExtractionBatch(ctx, ExtractionBatchRequest)`：可以在不传 SessionIDs 时扫描 eligible sessions。
- `ExtractionBuildSelector`：支持按 `episode_ids`、`session_id`、`since/until`、`limit` 构建抽取窗口。
- `ExtractionRunResult`：已有 status、fingerprint、skipped_by_fingerprint、accepted/review/rejected/applied/failure 等结果字段。
- `RunMirrorSync(ctx, RunMirrorSyncRequest)`：已有 mirror sync worker 入口。
- EmoAgent `memory_segments` 已有 `last_extracted_at` 和 `extraction_status` 字段，但目前基本没有完整使用。

### 1.4 关键缺口

当前缺口不是抽取协议，而是运行形态：

1. 抽取被 `FinalizeSegment` 同步调用，可能阻塞 WebSocket resume / 用户下一轮对话。
2. 触发时机过窄，只依赖 segment finalized。
3. `RunExtractionBatch` 的 `eligibleSessions` 只看 visible/searchable episodes，没有“未抽取 / 已过期 / idle”判断。
4. 没有 durable job queue，因此进程重启、抽取失败、sidecar 暂不可用时不可恢复。
5. sidecar mirror sync 与 extraction completion 没有用户级语义上的闭环。
6. 没有 UI/API 入口让用户主动扫描当前会话或某个 segment。

---

## 2. 设计结论

### 2.1 总体路线

建议采用：

```text
对话热路径只写 Episode + 更新 segment activity
  → 抽取触发器只 enqueue job
  → 后台 worker 调 MemoryCore RunExtraction
  → 抽取成功后触发/提示 mirror sync
  → UI/API 只展示 job 状态，不等待 LLM 抽取完成
```

换句话说：

```text
RunExtraction 仍然可以是同步函数；
但它只能在后台 worker 中同步执行，不能在用户对话热路径中同步执行。
```

### 2.2 职责边界

推荐边界：

```text
EmoAgent:
  - 拥有 chat session / memory segment 生命周期。
  - 拥有 idle/manual/finalize trigger。
  - 拥有 extraction job queue、scheduler、worker、REST/UI。
  - 调用 MemoryCore 的同步服务 API。

MemoryCore:
  - 继续拥有 extraction protocol、prefilter、LLM runner、quality gate、apply、dedup、forget route。
  - 继续拥有 SQLite authority 与 index_sync_queue。
  - 继续提供 RunExtraction / RunExtractionBatch / RunMirrorSync。
  - 可补强 batch eligibility，但不必第一步承担 EmoAgent segment idle 逻辑。

Python Sidecar / TriviumDB:
  - 继续只作为 retrieval mirror / embedding / dedup / graph search 辅助层。
  - 由 MemoryCore 的 index_sync_queue 和 RunMirrorSync 驱动，不参与权威状态判断。
```

理由：segment 的 idle、手动选择某段、当前 UI 会话状态都在 EmoAgent；抽取质量与写入规则在 MemoryCore。把队列放在 EmoAgent 可以最小改动现有 MemoryCore service，同时保留未来把队列下沉到 MemoryCore 的空间。

---

## 3. 新增运行模型

### 3.1 关键流程图

```text
User message
  ↓
EmoAgent DB messages
  ↓
Memory Bridge AppendEpisode
  ↓
memory_segments.last_activity_at 更新
  ↓
返回对话，不抽取
  ↓
Idle Scheduler / Manual API / Segment Finalize Trigger
  ↓
memory_extraction_jobs enqueue
  ↓
Extraction Worker claim job
  ↓
MemoryCore.RunExtraction(...)
  ↓
MemoryCore SQLite authority write + index_sync_queue
  ↓
Mirror sync signal / RunMirrorSync
  ↓
Update job + segment extraction status
```

### 3.2 状态机

`memory_extraction_jobs.status`：

```text
pending
running
succeeded
skipped
failed
cancelled
```

推荐状态转移：

```text
pending → running → succeeded
pending → running → skipped
pending → running → failed → pending  // retry with backoff
pending/running → cancelled           // admin/manual cancel
```

`memory_segments.extraction_status` 建议使用：

```text
never
pending
running
succeeded
skipped
failed
stale
```

含义：

- `never`: 从未成功抽取。
- `pending`: 有 job 等待执行。
- `running`: 有 worker 正在执行。
- `succeeded`: 最近一次抽取成功，且 `last_extracted_at >= last_activity_at`。
- `skipped`: 最近一次因 fingerprint 或空窗口跳过。
- `failed`: 最近一次失败。
- `stale`: 曾经成功，但之后又有新 episode。

---

## 4. 数据库变更

### 4.1 扩展 `memory_segments`

现有字段保留，新增最小字段：

```sql
ALTER TABLE memory_segments ADD COLUMN last_extracted_until_at TEXT;
ALTER TABLE memory_segments ADD COLUMN last_extracted_user_episode_id TEXT;
ALTER TABLE memory_segments ADD COLUMN last_extracted_assistant_episode_id TEXT;
ALTER TABLE memory_segments ADD COLUMN last_extraction_job_id TEXT;
ALTER TABLE memory_segments ADD COLUMN last_extraction_error_code TEXT;
ALTER TABLE memory_segments ADD COLUMN last_extraction_error_message TEXT;
ALTER TABLE memory_segments ADD COLUMN extraction_attempt_count INTEGER NOT NULL DEFAULT 0;
```

如果希望先小步实现，可只新增：

```sql
last_extracted_until_at
last_extraction_job_id
last_extraction_error_code
```

`last_extracted_at` 已存在，继续作为“worker 完成时间”；`last_extracted_until_at` 作为“抽取窗口覆盖到的 conversation activity 时间”。

### 4.2 新增 `memory_extraction_jobs`

```sql
CREATE TABLE IF NOT EXISTS memory_extraction_jobs (
    id                         TEXT PRIMARY KEY,
    persona_id                 TEXT NOT NULL,
    chat_session_id             TEXT,
    segment_id                  TEXT,
    memory_session_id           TEXT,

    trigger                     TEXT NOT NULL,
    -- idle_detect | session_end | manual_pin | manual_scan |
    -- manual_segment_scan | periodic_sweep | reprocess

    scope                       TEXT NOT NULL DEFAULT 'segment',
    -- segment | chat_session | persona

    mode                        TEXT NOT NULL DEFAULT 'apply',
    requested_by                TEXT NOT NULL DEFAULT 'system',
    -- system | user | admin

    priority                    INTEGER NOT NULL DEFAULT 100,
    force                       INTEGER NOT NULL DEFAULT 0,
    episode_ids_json            TEXT NOT NULL DEFAULT '[]',
    since_at                    TEXT,
    until_at                    TEXT,
    episode_limit               INTEGER NOT NULL DEFAULT 50,

    status                      TEXT NOT NULL DEFAULT 'pending',
    attempts                    INTEGER NOT NULL DEFAULT 0,
    max_attempts                INTEGER NOT NULL DEFAULT 3,
    run_after                   TEXT NOT NULL,
    claimed_by                  TEXT,
    claimed_until               TEXT,

    request_json                TEXT,
    result_json                 TEXT,
    mirror_sync_result_json      TEXT,

    error_code                  TEXT,
    error_message               TEXT,

    created_at                  TEXT NOT NULL,
    updated_at                  TEXT NOT NULL,
    started_at                  TEXT,
    finished_at                 TEXT
);

CREATE INDEX IF NOT EXISTS idx_memory_extraction_jobs_claim
    ON memory_extraction_jobs(status, run_after, priority, created_at);

CREATE INDEX IF NOT EXISTS idx_memory_extraction_jobs_segment
    ON memory_extraction_jobs(segment_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_memory_extraction_jobs_chat_session
    ON memory_extraction_jobs(chat_session_id, created_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_memory_extraction_jobs_dedupe_pending
    ON memory_extraction_jobs(persona_id, COALESCE(memory_session_id, ''), trigger, COALESCE(until_at, ''))
    WHERE status IN ('pending', 'running');
```

注意：SQLite expression index 对 `COALESCE` 可用，但若担心兼容性，可以新增 `dedupe_key TEXT` 字段并对它建 partial unique index。

### 4.3 可选：job event 表

用于 UI 与调试：

```sql
CREATE TABLE IF NOT EXISTS memory_extraction_job_events (
    id          TEXT PRIMARY KEY,
    job_id      TEXT NOT NULL,
    event_type  TEXT NOT NULL,
    message     TEXT,
    payload_json TEXT,
    created_at  TEXT NOT NULL
);
```

MVP 可先不做。

---

## 5. 配置变更

在 EmoAgent `config.MemoryExtractionConfig` 增加：

```yaml
memory:
  extraction:
    enabled: true

    # 保留旧字段，但语义从“直接同步抽取”改为“触发 enqueue”
    trigger_on_finalize_segment: true
    trigger_on_manual_pin: true

    async:
      enabled: true
      worker_enabled: true
      worker_concurrency: 1
      queue_claim_ttl_seconds: 300
      max_attempts: 3
      retry_base_delay_seconds: 30
      retry_max_delay_seconds: 900

    idle:
      enabled: true
      idle_after_seconds: 900       # 15min
      sweep_interval_seconds: 60
      min_episode_count: 2
      max_segments_per_sweep: 20
      include_finalized_segments: true
      include_active_segments: true

    manual:
      enabled: true
      mode: apply
      allow_force: true
      allow_segment_selection: true

    mirror_sync:
      after_apply: true
      periodic_enabled: true
      interval_seconds: 60
      limit: 100
      fail_extraction_on_sync_error: false
```

默认建议：

- `async.enabled=true`，只要 `memory.extraction.enabled=true`。
- 旧的 `trigger_on_finalize_segment=true` 保留，但不再同步阻塞。
- `idle.enabled=true` 可以作为默认，但在没有 provider API key 时 extraction 本身仍会失败并重试/记录。
- `mirror_sync.fail_extraction_on_sync_error=false`，因为 SQLite 是权威库，mirror 失败不应阻塞抽取成功。

---

## 6. 触发器设计

### 6.1 对话热路径

`AppendUserEpisode` / `AppendAssistantEpisode` 只做：

1. 调 MemoryCore `AppendEpisode`。
2. 更新 `memory_segments.last_activity_at`。
3. 将 segment extraction status 标成 `stale`，如果它曾经 succeeded/skipped 且新 episode 晚于 `last_extracted_until_at`。
4. 不调用 LLM，不抽取。

### 6.2 Segment finalized

把 `Bridge.FinalizeSegment` 的末尾从：

```go
b.extractFinalizedSegment(ctx, segment, personaID)
```

改为：

```go
b.queueExtraction(ctx, ExtractionEnqueueRequest{
    Trigger: "session_end",
    SegmentID: segment.ID,
    MemorySessionID: segment.MemorySessionID,
    Until: segment.LastActivityAt,
    Priority: 50,
})
```

也就是说，finalize 仍然结束 MemoryCore session、更新 `finalized_at`，但抽取异步执行。

### 6.3 Idle scheduler

新增后台 goroutine：

```text
每 sweep_interval 扫描 memory_segments：
  finalized_at IS NULL 或 include_finalized_segments=true
  last_activity_at <= now - idle_after
  last_extracted_until_at IS NULL OR last_extracted_until_at < last_activity_at
  extraction_status NOT IN ('pending', 'running')
  至少有 min_episode_count 或 last_user_episode_id 非空
  限制 max_segments_per_sweep
  enqueue trigger=idle_detect
```

Idle 抽取不一定 finalize segment；它可以对仍然活跃的 MemoryCore session 做窗口抽取。这样用户 15 分钟不说话后，后台自然抽取；用户回来继续说话时，仍在同一 chat session 和同一个 active segment 中。

### 6.4 Manual scan

新增 REST：

```http
POST /api/memory/extractions
```

请求：

```json
{
  "session_id": "chat-session-id optional",
  "segment_id": "segment-id optional",
  "persona_id": "default optional",
  "mode": "apply",
  "force": false,
  "scope": "unextracted|session|segment|persona",
  "run_now": false
}
```

行为：

- `segment_id` 有值：只 enqueue 该 segment。
- `session_id` 有值：enqueue 当前 session 中 stale/never/failed 的 segments；如果 `force=true`，也可重跑成功过的 segment。
- 都不传：扫描全局符合条件的 segments。
- `run_now=true` 只供 admin/测试用，实际也应先入队再由 worker 立即 claim，避免出现第二条同步路径。

响应：

```json
{
  "status": "queued",
  "enqueued_count": 2,
  "skipped_count": 1,
  "jobs": [
    {"id": "...", "segment_id": "...", "status": "pending"}
  ]
}
```

查询：

```http
GET /api/memory/extractions?session_id=...
GET /api/memory/segments?session_id=...
POST /api/memory/segments/{segment_id}/extract
```

### 6.5 Manual pin

当前 manual pin 是同步 `RunExtraction` 并要求 `AppliedCount > 0`。建议改为：

```text
用户说“记住这个”
  → 同步记录 user episode
  → enqueue high-priority manual_pin extraction job
  → 回复用户：“好，我会把这点记下来。”
```

如果担心“下一轮马上检索不到刚 pin 的内容”，可加配置：

```yaml
manual_pin_hot_path: false
```

第一版建议仍异步，保持一致性。后续可以做“轻量 pin intent marker”作为短期上下文补偿，但不要让 LLM 抽取阻塞对话。

---

## 7. Worker 设计

### 7.1 Claim

新增 repository 方法：

```go
ClaimExtractionJobs(ctx, workerID string, limit int, claimTTL time.Duration) ([]Job, error)
CompleteExtractionJob(ctx, jobID string, result JobResult) error
FailExtractionJob(ctx, jobID string, err JobError, nextRunAfter time.Time) error
```

Claim 条件：

```sql
status = 'pending'
AND run_after <= now
AND (claimed_until IS NULL OR claimed_until < now)
ORDER BY priority ASC, created_at ASC
LIMIT ?
```

运行中 job 超过 `claimed_until` 可被重新 claim。

### 7.2 执行

Job → `memorycore.RunExtractionRequest` 映射：

```go
req := memorycore.RunExtractionRequest{
    PersonaID: job.PersonaID,
    SessionID: &job.MemorySessionID,
    Trigger: triggerMap(job.Trigger),
    Timezone: policy.Timezone,
    Mode: job.Mode,
    Build: &memorycore.ExtractionBuildSelector{
        SessionID: &job.MemorySessionID,
        EpisodeIDs: job.EpisodeIDs,
        Since: job.SinceAt,
        Until: job.UntilAt,
        Limit: job.EpisodeLimit,
    },
    Policy: memorycore.ExtractionPolicyOverride{
        AllowInference: boolPtr(config.AllowInference),
        AllowSensitiveExtraction: boolPtr(config.AllowSensitiveExtraction),
        MaxFacts: intPtr(config.MaxFacts),
        MaxLinks: intPtr(config.MaxLinks),
    },
    Runtime: ...,
    SemanticDedup: ...,
    Force: job.Force,
}
```

Trigger 映射：

```text
idle_detect           → memorycore.ExtractionTriggerIdleDetect
session_end           → memorycore.ExtractionTriggerSessionEnd
manual_pin            → memorycore.ExtractionTriggerManualPin
manual_scan           → reprocess 或 periodic_consolidation
manual_segment_scan   → reprocess
periodic_sweep        → periodic_consolidation
reprocess             → reprocess
```

如果 MemoryCore 当前没有某个 trigger 常量，先用已有合法 trigger 并补常量/validation。

### 7.3 成功判定

成功状态沿用现有 helper 语义：

```text
applied
nothing_applied
skipped
dry_run
validated
```

其中：

- `skipped_by_fingerprint=true` → job status `skipped`。
- `mode=apply` 且 `applied_count > 0` → job status `succeeded`。
- `nothing_applied` 也算成功，只是没有新 fact。

### 7.4 Segment 状态更新

成功后：

```sql
UPDATE memory_segments
SET last_extracted_at = now,
    last_extracted_until_at = job.until_at or previous segment.last_activity_at,
    last_extraction_job_id = job.id,
    extraction_status = CASE WHEN skipped THEN 'skipped' ELSE 'succeeded' END,
    last_extraction_error_code = NULL,
    last_extraction_error_message = NULL
WHERE id = job.segment_id;
```

失败后：

```sql
UPDATE memory_segments
SET extraction_status = 'failed',
    extraction_attempt_count = extraction_attempt_count + 1,
    last_extraction_error_code = sanitized_code,
    last_extraction_error_message = sanitized_message
WHERE id = job.segment_id;
```

如果失败但可重试，job 回到 `pending` 且 `run_after=now+backoff`；否则 `failed`。

---

## 8. Sidecar / Mirror 同步设计

### 8.1 原则

```text
Extraction apply 写 SQLite authority。
Consolidation / apply 产生 index_sync_queue。
Mirror sync worker 读取 index_sync_queue 并调用 sidecar/Trivium adapter。
Mirror 同步失败不回滚 SQLite authority。
Retrieval 仍必须 SQLite authority filter。
```

### 8.2 同步时机

建议两个层级同时存在：

1. **Extraction worker 成功后轻推一次：**

```go
if mode == apply && cfg.MirrorSync.AfterApply {
    result, err := svc.RunMirrorSync(ctx, RunMirrorSyncRequest{
        PersonaID: job.PersonaID,
        Limit: cfg.MirrorSync.Limit,
    })
    record mirror_sync_result_json or degraded error
}
```

2. **独立 periodic mirror sync worker：**

```text
每 mirror_sync.interval_seconds 扫描/运行 RunMirrorSync
用于处理 extraction 后未 drain 完、delete、retention、manual forget 产生的队列。
```

### 8.3 错误处理

- `MirrorAdapter required` / sidecar down / mirror state not ready：记录到 job 的 `mirror_sync_result_json`，不要把 extraction job 标失败，除非 `fail_extraction_on_sync_error=true`。
- 触发 breaker 或持续失败时，UI 显示“记忆已写入，检索镜像同步延迟”。
- 检索路径继续依赖 SQLite fallback / authority filter。

---

## 9. API / UI 设计

### 9.1 API

新增：

```http
GET  /api/memory/segments?session_id={id}
POST /api/memory/extractions
GET  /api/memory/extractions?session_id={id}&limit=20
POST /api/memory/segments/{segment_id}/extract
```

可选 admin：

```http
POST /api/memory/extractions/sweep
POST /api/memory/mirror-sync
```

### 9.2 UI

建议把按钮先放在会话右上角，点击后展开详细列表和状态：

- “扫描并抽取记忆”按钮：当前 session 级 manual scan。
- Segment 列表：
  - segment index
  - started_at / last_activity_at / finalized_at
  - extraction_status
  - last_extracted_at
  - last error
  - “抽取本段”按钮
- Job 状态：
  - pending / running / succeeded / skipped / failed
  - applied / accepted / rejected / failure counts
  - mirror sync status

### 9.3 用户文案

用户无感默认不提示。

手动按钮可提示：

```text
已提交记忆扫描，会在后台处理。你可以继续对话。
```

manual pin 可提示：

```text
好，我会记住的！
```

避免提示：

```text
正在调用 LLM 抽取长期记忆……
```

---

## 10. 和现有 MemoryCore Batch 的关系

MemoryCore 的 `RunExtractionBatch` 可以保留并作为 CLI/admin 能力，但本次不要直接把它暴露为主要 idle scheduler，因为当前 `eligibleSessions` 不知道 EmoAgent 的 `memory_segments.last_extracted_at / last_activity_at`，只会按 visible/searchable episodes 分组，容易重复扫描所有 session。

本次建议：

```text
MVP: EmoAgent 扫 segment → enqueue job → worker 调 RunExtraction。
后续: MemoryCore 增强 eligibleSessions，加 ExtractionCursor / last_extracted_at 后，再让 RunExtractionBatch 成为更强的底层函数。
```

---

## 11. 测试计划

### 11.1 Unit tests

1. `FinalizeSegment` 不再同步调用 `ExtractSessionEnd`，只 enqueue job。
2. `ResumeSession` 不会因 extraction provider 慢/失败而阻塞。
3. `AppendUserEpisode` / `AppendAssistantEpisode` 会把 succeeded segment 标为 stale。
4. Idle scanner 能找到：
   - active idle stale segment
   - finalized unextracted segment
   - failed but retryable segment
5. Idle scanner 不会找到：
   - running/pending segment
   - last_extracted_until_at >= last_activity_at 的 segment
   - 未达到 idle_after 的 segment
6. Manual scan API：
   - segment_id 精确入队
   - session_id 入队该会话未抽取/stale segments
   - 空参数扫描全局 eligible segments
7. Worker：
   - 正确组装 `RunExtractionRequest.Build.SessionID/Since/Until/Limit`
   - 成功更新 job/segment
   - fingerprint skip 标为 skipped
   - 失败按 backoff 重试
8. Mirror sync：
   - apply 成功后调用 `RunMirrorSync`
   - mirror 失败不导致 extraction 失败
   - periodic mirror worker 可继续 drain

### 11.2 Integration tests

使用 fake MemoryCore service：

- fake `RunExtraction` sleep 1s，验证 WebSocket/send/resume 不等待。
- fake `RunExtraction` 返回 Applied，验证 job succeeded。
- fake sidecar down，验证 extraction succeeded + mirror degraded。
- 进程重启模拟：pending/running expired job 可重新 claim。

### 11.3 Regression tests

保留现有 manual forget、manual pin、retrieval tests，确保：

- 用户删除优先；
- hidden/forgotten/purged 仍不会因异步任务复活；
- stale mirror hit 仍被 SQLite filter 拦截。

---

## 12. 实施顺序

### Phase 1: Queue + 状态

- 增加 config 字段与默认值。
- 增加 migrations。
- 新增 `internal/storage/memory_extraction_jobs.go`。
- 实现 enqueue / claim / complete / fail / list。

### Phase 2: 改同步触发为 enqueue

- 修改 `memoryhost.Bridge.FinalizeSegment`。
- 修改 manual pin：同步抽取改为高优先级 job。
- 保留旧 helper 但只供 worker 调用。

### Phase 3: Worker

- 新增 `internal/memoryhost/extraction_worker.go` 或 `internal/memoryjobs`。
- 在 `App.Run` 启动 worker goroutine。
- job 执行调用 MemoryCore `RunExtraction`。
- 写回 job/segment 状态。

### Phase 4: Idle scheduler

- 新增 scanner：按 idle/stale/unextracted 选择 segment。
- 定期 enqueue `idle_detect`。
- 支持 `max_segments_per_sweep` 与 pending/running 去重。

### Phase 5: Mirror sync

- 抽取成功后可选 `RunMirrorSync`。
- 独立 periodic mirror worker。
- 记录 mirror sync result。

### Phase 6: REST / UI

- 新增 memory extraction API。
- UI 加按钮与状态展示。
- 手动 scan 返回 job ids，不等待完成。

### Phase 7: Docs / Eval

- 更新 README / docs。
- 加 fixture：idle、manual、mirror degradation、retry。
- 明确“不会再要求 WebSocket 重连触发抽取”。

---

## 13. 非目标

本次不做：

- 重写 extraction prompt / schema。
- 重写 consolidation。
- 把 Python Sidecar 变成权威写入方。
- 把所有历史 messages 重新导入 MemoryCore。
- 做复杂多 worker 分布式调度。
- 做细粒度 token-level incremental extraction。
- 实现 Agent Affect 新算法。

---

## 14. 验收标准

1. 用户正常聊天时，不会因为记忆抽取 LLM 调用变慢而卡住对话。
2. WebSocket 断开/重连不再是唯一触发抽取的方法。
3. idle 超过配置时间后，未抽取或 stale 的 segment 会自动进入 extraction job queue。
4. 手动按钮/API 可以对当前 session 或指定 segment 提交抽取。
5. 同一 segment 不会在 pending/running 状态下重复入队。
6. job 失败会记录错误并按配置重试，不会影响聊天主流程。
7. 抽取成功后，`memory_segments.last_extracted_at / extraction_status` 正确更新。
8. apply 模式成功后会触发 mirror sync 或至少记录 mirror degraded。
9. mirror sync 失败不破坏 SQLite authority；检索仍能通过 SQLite fallback/authority filter 安全运行。
10. 单元测试覆盖 finalize enqueue、idle enqueue、manual enqueue、worker success/failure、mirror sync degradation。
11. UI/API 可以看到 job 状态，手动触发立即返回。
12. 文档明确 Session/Segment/MemoryCore Session 的关系和新的触发策略。
