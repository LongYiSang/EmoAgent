
# 2. 如何融入 EmoAgent 主仓库

## 2.1 接入原则

Natural Memory 在 EmoAgent 里应该是：

```text
Memory 后台维护能力
不是聊天热路径
不是 Emotion 回复前同步任务
不是 Work 任务
不是 Retention 页面里的子项
```

EmoAgent 现在已经有清晰的定位：Emotion 是唯一对话者，Memory 是 Emotion 的长期状态层，Work 只在幕后执行，长期记忆是关系载体而不是日志。

所以 Natural Memory 的接入位置应该是：

```text
EmoAgent MemoryHost
  → 启动 MemoryCore
  → 启动自然记忆 sleep scheduler
  → 定期调用 MemoryCore.RunNaturalMemoryTick
  → 如有 mirror update，触发 RunMirrorSync
  → Admin/API 可手动 dry-run 或 force-run
```

不要放到：

```text
Emotion 每轮回复前
Work runtime
Retention job 页面
Extraction worker 内部
```

---

## 2.2 推荐架构图

```text
EmoAgent
├── Emotion Root
│   ├── 每轮对话前：RetrievePromptBlock
│   └── 回复后：AppendEpisode / queue extraction
│
├── MemoryHost Bridge
│   ├── MemoryCore Service
│   ├── Extraction Queue / Worker
│   ├── Mirror Sync Worker
│   └── NaturalMemoryRunner  ← 新增
│
├── NaturalMemoryRunner
│   ├── ticker / startup tick
│   ├── RunNaturalMemoryTick
│   ├── optional RunMirrorSync
│   ├── sanitized logging
│   └── manual API bridge
│
└── Admin UI / API
    ├── natural memory config
    ├── dry-run preview
    ├── force manual run
    └── latest run status
```

---

## 2.3 不建议用 CLI 子进程集成

虽然 MemoryCore 已经有 `memoryctl natural-memory-run`，但 EmoAgent 内部不应 shell out 到 CLI。

原因：

```text
1. EmoAgent 已经通过 memoryhost.OpenFromConfigWithOptions 打开 MemoryCore Service。
2. Host.Service 已经是 memorycore.Service。
3. Natural Memory 已经暴露 RunNaturalMemoryCycle / RunNaturalMemoryTick。
4. CLI 适合运维，不适合主服务内部调用。
```

EmoAgent 的 `Host` 当前保存了：

```go
Service memorycore.Service
```

并通过 `memorycore.Open(ctx, opts)` 打开服务。

所以主仓库应该直接调用：

```go
host.Service.RunNaturalMemoryTick(...)
host.Service.RunNaturalMemoryCycle(...)
```

---

## 2.4 配置接入

EmoAgent 现在是运行时配置中心：`config.yaml` 是 seed，runtime settings / Provider Center 是运行时来源；MemoryCore 保持 standalone，EmoAgent 打开 MemoryCore 时通过 overrides 注入配置。

因此我建议 EmoAgent 新增：

```yaml
memory:
  natural_memory:
    enabled: true

    sleep_cycle:
      enabled: true
      scheduler_enabled: true
      tick_interval_seconds: 60
      local_time: "03:30"
      timezone: ""                 # 空则跟 memorycore/core timezone
      run_missed_on_start: false
      min_interval_hours: 20
      jitter_minutes: 0

    manual:
      enabled: true
      allow_dry_run: true
      allow_force: true

    mirror_sync:
      after_run: true
      fail_on_sync_error: false

    observability:
      log_runs: true
      expose_admin_api: true
```

然后在 `memoryhost.OpenFromConfigWithOptions` 前，把 EmoAgent 的 `memory.natural_memory` 合成到 MemoryCore `ConfigOverrides` 或 runtime options 的 `NaturalMemory` 字段。MemoryCore 的 `Options` 已经有 `NaturalMemory NaturalMemoryOptions` 字段。

---

## 2.5 新增 `NaturalMemoryRunner`

建议新增文件：

```text
internal/memoryhost/natural_memory.go
```

职责类似 ExtractionRunner，但更轻：

```go
type NaturalMemoryRunner struct {
    host   *Host
    cfg    NaturalMemoryHostConfig
    logger *slog.Logger

    stop chan struct{}
}
```

核心方法：

```go
func NewNaturalMemoryRunner(host *Host, cfg NaturalMemoryHostConfig, logger *slog.Logger) *NaturalMemoryRunner

func (r *NaturalMemoryRunner) Start(ctx context.Context)
func (r *NaturalMemoryRunner) Stop(ctx context.Context) error

func (r *NaturalMemoryRunner) Tick(ctx context.Context, now time.Time) (*memorycore.RunNaturalMemoryCycleResult, error)

func (r *NaturalMemoryRunner) RunManual(ctx context.Context, req NaturalMemoryManualRunRequest) (*memorycore.RunNaturalMemoryCycleResult, error)
```

`Tick` 内部：

```go
result, err := r.host.Service.RunNaturalMemoryTick(ctx, memorycore.RunNaturalMemoryTickRequest{
    PersonaID: personaID,
    Now:       now,
    Force:     false,
    Explain:   false,
})
```

如果：

```go
result.MirrorUpdatesEnqueued > 0
```

且配置允许：

```go
r.host.Service.RunMirrorSync(ctx, memorycore.RunMirrorSyncRequest{
    PersonaID: personaID,
    Limit:     cfg.MirrorSyncLimit,
})
```

这里要复用现有理念：抽取 apply 成功后默认触发 `RunMirrorSync`，sidecar 失败只 degraded，不影响 SQLite 权威成功。

---

## 2.6 与现有抽取队列的关系

现有热路径设计是：聊天时只 append episode；抽取不在聊天热路径同步执行，而由 EmoAgent 的 `memory_extraction_jobs` 队列驱动。

Natural Memory 应该同样遵守这个原则：

```text
聊天热路径：
  RetrievePromptBlock
  AppendUserEpisode
  AppendAssistantEpisode
  queue extraction if needed

后台路径：
  extraction worker
  mirror sync
  natural memory sleep tick
```

建议 natural sleep cycle 的运行条件加一个软门控：

```text
如果当前有大量 pending/running extraction jobs：
  允许延迟 natural sleep cycle
```

理由：

```text
自然遗忘应该尽量在当天记忆抽取、consolidation、mirror sync 之后执行。
否则它可能在新 facts 还没进入权威库前提前跑完。
```

推荐策略：

```text
03:30 到点：
  1. 检查 memory_extraction_jobs 是否有 running。
  2. 如果有，最多延迟到 04:30。
  3. 超过窗口仍有积压，则照跑，但记录 degraded_reason="extraction_backlog"。
```

---

## 2.7 与 Retrieval / Prompt 注入的关系

Natural Memory 不需要改 Emotion 的 prompt 逻辑。EmoAgent 现在已经通过：

```go
Bridge.RetrievePromptBlock
→ MemoryCore.Retrieve
→ FormatMemoryContextForPrompt
```

把长期记忆注入 system prompt。

Natural Memory 只改 `memory_search_documents.search_tier`，所以效果应该自然体现在下一次检索中：

```text
hot    → 更容易被普通检索/主动召回
warm   → 需要更强相关性
cold   → 通常只在明确相关时出现
deep_cold → 只在历史/溯源/明确召回中出现
```

如果检索排序目前还没有强使用 `search_tier` multiplier，则需要在 MemoryCore Retrieval 再补一层：

```text
search_tier multiplier:
  hot = 1.00
  warm = 0.72
  cold = 0.40
  deep_cold = 0.12
```

EmoAgent 侧不应该自己解释 tier；这个逻辑应该继续留在 MemoryCore Retrieval 内。

---

## 2.8 Admin / API 设计

建议新增 API：

```text
POST /api/memory/natural-runs
GET  /api/memory/natural-runs/latest
GET  /api/memory/natural-runs
POST /api/memory/natural-runs/dry-run
```

第一版可以先只做：

```http
POST /api/memory/natural-runs
```

请求：

```json
{
  "mode": "manual",
  "dry_run": true,
  "force": false,
  "explain": true,
  "mark_sleep_cycle": false
}
```

响应直接透传 MemoryCore result：

```json
{
  "run_id": "...",
  "run_kind": "manual",
  "status": "completed",
  "dry_run": true,
  "evaluated_nodes": 120,
  "search_tier_updates": 31,
  "mirror_updates_enqueued": 8,
  "compression_candidates": 3,
  "explain": []
}
```

Admin UI 放在 Memory 页面下，但不要放在 Retention 子页。建议独立卡片：

```text
Memory → Natural Memory
  - Enabled
  - Sleep cycle local time
  - Last run
  - Next due
  - Dry-run preview
  - Run now
```

---

## 2.9 与 sidecar / mirror 的关系

EmoAgent 当前 sidecar 是可降级 loopback 增强依赖，`memory.sidecar.enabled=true` 时会检查 managed/external sidecar；失败且 fail_open 时关闭 mirror/sidecar，保留 SQLite/FTS 路径。

Natural Memory 的规则应类似：

```text
Natural run 成功与否以 SQLite 写入为准。
mirror sync 失败只能 degraded，不应回滚 natural_state/search_tier。
```

推荐 result 扩展或 EmoAgent 包装 result：

```json
{
  "natural_run": {...},
  "mirror_sync": {
    "attempted": true,
    "status": "degraded",
    "error_code": "sidecar_unavailable"
  }
}
```

---

## 2.10 与 go.mod / 版本管理的关系 (不用做)

EmoAgent 现在依赖：

```go
github.com/longyisang/emoagent-memorycore v0.0.0
```

并且本地 replace 到：

```text
D:/Dev/Project/Agent/EmoAgent-MemoryCore
```



如果你继续本地双仓开发，这没问题；但要注意：

```text
EmoAgent 集成 natural_memory 前，必须保证本地 replace 指向包含 95b8cc0c 的 MemoryCore。
CI / 其他机器需要改成 git pseudo-version 或 tag。
```

建议打一个轻量 tag：

```text
emoagent-memorycore v0.0.0-20260605-natural-memory-v1
```

或者等功能稳定后：

```text
v0.1.0
```

---

# 3. 推荐落地步骤

## Step 1：MemoryCore 小修 (已完成)

先修两个最容易误用的点：

```text
1. memoryctl natural-memory-run 默认 --mode manual。
2. --mode sleep_cycle 时走 RunNaturalMemoryTick 或同等 due guard。
```

同时补：

```text
- run_missed_on_start / jitter / night_window 的实际行为或明确标记为 reserved。
- graph_significance 进入 τ。
- invalidated facts 的 eligibility/cap 测试。
```

## Step 2：EmoAgent 更新 MemoryCore 依赖

```text
- 更新本地 replace 或 go module 版本。
- 确认 EmoAgent 能编译到新的 memorycore.Service 接口。
- 确认 config/memorycore.yaml 包含 natural_memory block。
```

## Step 3：新增 EmoAgent 配置

在 `config.yaml` 的 `memory:` 下新增：

```yaml
natural_memory:
  enabled: true
  scheduler_enabled: true
  tick_interval_seconds: 60
  mirror_sync_after_run: true
  fail_on_sync_error: false
  manual:
    enabled: true
    allow_dry_run: true
    allow_force: true
```

然后将它映射到 MemoryCore `NaturalMemoryOptions`。

## Step 4：实现 `NaturalMemoryRunner`

```text
internal/memoryhost/natural_memory.go
internal/memoryhost/natural_memory_test.go
```

启动时：

```text
memory.enabled && natural_memory.enabled && scheduler_enabled
→ start ticker
→ every tick call RunNaturalMemoryTick(force=false)
```

## Step 5：接 Admin API

```text
POST /api/memory/natural-runs
GET /api/memory/natural-runs/latest
```

第一版只需要手动 run / dry-run / explain；列表能力可以等 MemoryCore 增加 `ListNaturalMemoryRuns` 后再做。

## Step 6：接 mirror sync

如果 natural result 里：

```text
MirrorUpdatesEnqueued > 0
```

且配置允许：

```text
RunMirrorSync
```

失败只 degraded，不回滚 Natural Memory。

## Step 7：测试

EmoAgent 侧至少加这些测试：

```text
NaturalMemoryRunner_Tick_NotDue_NoRun
NaturalMemoryRunner_Tick_Due_RunsOnce
NaturalMemoryRunner_ManualDryRun_NoWrites
NaturalMemoryRunner_ManualForce_AdminOnly
NaturalMemoryRunner_MirrorSyncAfterTierUpdates
NaturalMemoryAPI_DryRun_ReturnsResult
NaturalMemoryAPI_ForceRejectedWhenDisabled
```

---

## 最终建议

```text
EmoAgent 集成：
7. 通过 memoryhost 直接调用 RunNaturalMemoryTick，不要走 CLI。
8. 放后台 scheduler，不进聊天热路径。
9. Natural run 后按需 RunMirrorSync。
10. Admin Memory 页新增 Natural Memory 独立卡片，不放进 Retention。
```

这样接入后，Natural Memory 在 EmoAgent 中的位置会很清楚：**它是 MemoryHost 的后台记忆动力学维护器，负责让记忆自然变冷或重新巩固；Emotion 只消费它对检索层产生的结果，不直接操心它的运行。**
