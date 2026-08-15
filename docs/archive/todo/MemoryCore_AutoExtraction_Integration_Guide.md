# MemoryCore 自动抽取记忆接入 EmoAgent 主项目实施指导

> 适用仓库：
>
> - 主项目：`github.com/LongYiSang/EmoAgent`
> - 记忆核心：`github.com/LongYiSang/EmoAgent-MemoryCore`
>
> 当前时间基准：2026-05-25  
> 当前主项目状态基准：EmoAgent `main` 在 2026-05-24 的 MemoryCore 接入提交之后  
> 目标：在不破坏现有 FTS 检索与 Prompt 注入链路的前提下，把 MemoryCore 的自动抽取运行时接入 EmoAgent 主项目。

---

## 0. 结论

现在应该先做 **自动抽取记忆**，不要继续优先推进“自然语言手动 Forget”。

原因不是 Forget 不重要，而是当前系统还缺少足够稳定、可定位、可验证的长期事实节点。主项目已经完成了 MemoryCore 生命周期 host、Memory Segment 桥接、手动固定记忆、FTS 检索和 Prompt 注入，但当前主仓库侧的手动 forget 入口仍然缺少“目标解析 → exact node → Forget”这条可靠链路。没有自动抽取和稳定 consolidation，Forget 很容易变成：

```text
用户说“别再提这个”
→ 主项目只识别到 forget intent
→ 但无法稳定知道“这个”对应哪条 fact / episode / derived narrative
→ 要么误删，要么删不到，要么检索中仍然出现
```

因此实施顺序应调整为：

```text
Phase A: 自动抽取 session / idle episode → candidate facts
Phase B: 通过 MemoryCore consolidation 写入 facts / links / FTS documents
Phase C: 复用现有 RetrievePromptBlock 注入 prompt
Phase D: 基于真实 fact id 再做 manual forget target resolver
```

最小可行版本不需要重写 retrieval，也不需要先上 TriviumDB。先走：

```text
EmoAgent memoryhost.Bridge
  → MemoryCore AppendEpisode
  → MemoryCore extractionruntime.Runner
  → MemoryCore ConsolidateCandidate
  → MemoryCore SQLite + FTS
  → EmoAgent RetrievePromptBlock
```

---

## 1. 当前两个仓库的实现判断

### 1.1 EmoAgent 主项目现状

主项目最近 Memory 相关提交已经形成了以下顺序：

```text
1. 引入 MemoryCore，本地 replace
2. add MemoryCore lifecycle host
3. 接入 Memory Segment 桥接
4. 接入手动固定记忆、FTS 记忆检索和 Prompt 注入
```

对应到代码结构，主仓库现在的 Memory 层很薄：

```text
internal/memory/memory.go
```

只是把 MemoryCore 的 public facade alias 出来：

```go
type Service = memorycore.Service
type Options = memorycore.Options

func Open(ctx context.Context, opts Options) (Service, error) {
    return memorycore.Open(ctx, opts)
}
```

真正的集成点在：

```text
internal/memoryhost/
  host.go
  bridge.go
  manual_rules.go
```

当前 `Host` 的职责是：

```text
- 从 MemoryCore config 打开 memorycore.Service
- 要求 memorycore config enabled = true
- 要求 auto_migrate = true
- 保存 DBPath / retrievalPolicy / Service
```

当前 `Bridge` 的职责是：

```text
- 把 EmoAgent chat session 映射到 MemoryCore session
- EnsureSegment / RolloverSegment
- AppendUserEpisode / AppendAssistantEpisode
- RetrievePromptBlock
- FinalizeSegment
- 对 user episode 执行简单 manual memory intent 识别
```

当前 `RetrievePromptBlock` 已经能调用：

```go
b.host.Service.Retrieve(ctx, memorycore.RetrievalRequest{
    PersonaID: defaultPersonaID(...),
    SessionID: &memorySessionID,
    QueryText: query,
    Policy: b.retrievalPolicy,
})
```

并用 `FormatMemoryContext` 生成 Prompt 注入块：

```text
[长期记忆上下文]
以下是允许用于当前回复的长期记忆。使用时要自然、克制；
不要主动说明“我记得”，除非用户正在询问记忆或来源。

- ...
```

这说明主项目现在的 **读路径已经打通**，但 **写入长期事实的自动链路尚未接上**。

### 1.2 当前手动 Pin / Forget 的状态

`manual_rules.go` 现在有两类规则：

```text
PinRules:
- 请记住我喜欢...
- 请记住我不喜欢...
- 以后叫我...
- 我的名字是...
- 我更喜欢...
- 我不想再聊...

ForgetPrefixes:
- 忘记
- 别再提
- 删除
```

`manualRules.Match()` 会先匹配 forget prefix，再匹配 pin rule。Pin 会构造 `memorycore.ManualFactCandidate`，并在 `Bridge.applyManualMemoryIntent()` 中调用：

```go
b.host.Service.ConsolidateCandidate(...)
```

但 Forget 现在在 `applyManualMemoryIntent()` 里是 no-op：

```go
case ManualMemoryIntentForget:
    return nil
```

这不是坏事，反而说明你之前遇到的 bug 是自然的：当前主仓库没有足够的 target resolver、exact node 定位和级联删除验证，所以贸然做 manual forget 只能靠文本猜测，很容易出现错删、漏删或检索残留。

### 1.3 MemoryCore 当前能力

MemoryCore 当前适合被主仓库作为 Go module 嵌入，而不是作为独立 HTTP 服务使用。它当前提供：

```text
memorycore.Open(ctx, memorycore.Options)
StartSession / AppendEpisode / EndSession
Retrieve
ConsolidateCandidate
Forget
pkg/memorycore/extractionruntime
SQLite + FTS fallback
可选 sidecar / mirror retrieval
```

MemoryCore 文档已经明确：`AppendEpisode` 只记录原始事件，不会自动生成长期事实；检索可返回的事实需要通过抽取 / 整合链路写入。

MemoryCore 也已经提供自动抽取运行时：

```go
import "github.com/longyisang/emoagent-memorycore/pkg/memorycore/extractionruntime"
```

`extractionruntime.Runner` 需要主仓库提供：

```text
- *sql.DB：访问同一个 MemoryCore SQLite 数据库
- memorycore.Service：用于把 accepted facts 写入整合入口
- memorycore.ExtractionLLM：宿主自己的 LLM client，或 OpenAI-compatible client
- AuditStore：通常用 extractionruntime.NewSQLiteAuditStore(sqlDB)
```

这正好适合放进 EmoAgent 主仓库的 `internal/memoryhost`。

---

## 2. 架构对齐：自动抽取应该插在哪里

### 2.1 不要让 EmoAgent 主项目直接写 facts 表

正确边界是：

```text
EmoAgent 主仓库：
  负责对话生命周期、触发时机、LLM 配置、日志、降级策略。

MemoryCore：
  负责 extraction request 构造、JSON schema / gate、consolidation、SQLite 权威写入、FTS 文档更新、audit。
```

不要在 EmoAgent 里手写 SQL 插入 `facts`，也不要把抽取结果直接写 FTS。所有事实都应通过：

```text
extractionruntime.Runner
  → accepted facts
  → memorycore.Service.ConsolidateCandidate
  → SQLite authoritative write
  → FTS / index_sync_queue
```

### 2.2 抽取器只提出候选，不拥有长期事实

架构里的原则是：

```text
Extractor proposes. Consolidation decides. SQLite records. Retrieval activates.
```

也就是说，自动抽取接入后也不要让 LLM 抽取结果直接成为长期记忆。LLM 只输出：

```text
entities[]
facts[]
links[]
affect_events[]
deletion_intents[]
pin_intents[]
correction_hints[]
rejected_candidates[]
gate_summary
```

真正是否写入由 MemoryCore 的 gate + consolidation 决定。

### 2.3 先接 session_end，再接 idle_detect

第一版建议只在 **segment finalize / session_end** 时运行自动抽取。原因：

```text
- 不阻塞当前回复
- episode window 完整
- 抽取上下文更稳定
- 重试和 audit 更容易
- 不容易一轮对话中写入半截事实
```

之后再做 idle：

```text
用户停顿 / assistant 回复后 debounce N 秒
→ 提取最近 N 条 episode
→ 只写高置信高价值 facts
```

但 idle 需要调度器、幂等、防重复、成本控制和更细粒度的 prefilter。它适合第二阶段。

---

## 3. 推荐目标链路

### 3.1 MVP 运行链路

```text
用户消息
  ↓
EmoAgent AppendUserEpisode
  ↓
MemoryCore episodes 只追加，不立刻生成 fact
  ↓
Emotion 正常回复
  ↓
EmoAgent AppendAssistantEpisode
  ↓
会话结束 / segment rollover / 用户显式结束
  ↓
Bridge.FinalizeSegment
  ↓
ExtractionRunner.ExtractSegment(trigger=session_end, mode=apply)
  ↓
MemoryCore BuildRequest 从同一 SQLite 读取 episode window
  ↓
Extraction LLM 输出 strict JSON
  ↓
MemoryCore validation / repair / gate
  ↓
accepted facts 进入 ConsolidateCandidate
  ↓
SQLite facts / entities / links / FTS search docs 更新
  ↓
下一轮 RetrievePromptBlock 可检索并注入 prompt
```

### 3.2 手动 Pin 保留

当前手动 pin 是低成本、强意图入口，应保留：

```text
用户说“请记住我喜欢手冲咖啡”
→ manual rule
→ ConsolidateCandidate
```

自动抽取不是替代 manual pin，而是补齐自然对话里的长期记忆。

### 3.3 手动 Forget 暂时改成“识别并路由”，不要文本猜删

当前 manual forget 不应继续 no-op 静默吞掉。建议第一阶段改为：

```text
- 识别到 forget prefix
- 记录 structured log / metric
- 返回一个 MemoryForgetIntentDetected 结果
- 不执行删除，除非 target 是明确 exact node
```

可选用户可见策略：

```text
如果用户说“别再提这件事”，而系统无法确定 target：
  不要假装已删除；
  可以让 Emotion 简短确认“我会避开这个话题”，但不要承诺底层已清理。
```

真正 Forget 在下一阶段做：

```text
forget intent
  → retrieve candidate nodes with high precision
  → ask confirmation if ambiguous
  → svc.Forget(exact_node)
  → verify FTS / mirror filter
```

---

## 4. 需要改的代码位置

### 4.1 新增 `internal/memoryhost/extraction.go`

建议新增：

```text
internal/memoryhost/extraction.go
internal/memoryhost/extraction_config.go
internal/memoryhost/extraction_test.go
```

核心类型：

```go
type ExtractionMode string

const (
    ExtractionModeValidate ExtractionMode = "validate"
    ExtractionModeDryRun   ExtractionMode = "dry_run"
    ExtractionModeApply    ExtractionMode = "apply"
)

type ExtractionConfig struct {
    Enabled                  bool
    Mode                     ExtractionMode
    TriggerOnFinalizeSegment bool
    TriggerOnIdle            bool

    Limit                    int
    Timezone                 string
    AllowInference           bool
    AllowSensitiveExtraction bool
    MaxFacts                 int
    MaxLinks                 int

    ProviderID               string
    ProviderKind             string
    Model                    string
    BaseURL                  string
    APIKeyEnv                string
    Timeout                  time.Duration
    MaxTokens                int
    Temperature              float64

    RepairEnabled            bool
    AuditEnabled             bool
    UsePrefilter             bool
}
```

核心 runner：

```go
type ExtractionRunner struct {
    host   *Host
    db     *sql.DB
    llm    memorycore.ExtractionLLM
    audit  extractionruntime.AuditStore
    cfg    ExtractionConfig
    logger *slog.Logger
}

func NewExtractionRunner(ctx context.Context, host *Host, cfg ExtractionConfig, logger *slog.Logger) (*ExtractionRunner, error)

func (r *ExtractionRunner) ExtractSessionEnd(ctx context.Context, personaID string, memorySessionID string) (*memorycore.ExtractionRunResult, error)

func (r *ExtractionRunner) Close() error
```

### 4.2 修改 `Host`

当前 `Host` 已保存：

```go
Service memorycore.Service
Source string
DBPath string
retrievalPolicy memorycore.RetrievalPolicy
logger *slog.Logger
```

建议扩展为：

```go
type Host struct {
    Service memorycore.Service
    Source string
    DBPath string

    retrievalPolicy memorycore.RetrievalPolicy
    extractionRunner *ExtractionRunner

    logger *slog.Logger
}
```

也可以不把 runner 放进 Host，而是在 `Bridge` 上挂：

```go
type Bridge struct {
    ...
    extractor *ExtractionRunner
}
```

更推荐放在 Host，因为 runner 依赖同一个 DBPath、Service 和 MemoryCore 生命周期。

### 4.3 修改 `OpenFromConfig`

当前 `OpenFromConfig` 只读取 MemoryCore 配置。自动抽取配置可以有两种方案。

方案 A：先用 EmoAgent 主配置注入。

```go
host, err := memoryhost.OpenFromConfig(...)
extractor, err := memoryhost.NewExtractionRunner(ctx, host, appCfg.Memory.Extraction, logger)
host.SetExtractionRunner(extractor)
```

方案 B：扩展 MemoryCore config。这个更整洁，但会牵涉 MemoryCore config schema。MVP 不建议先做。

### 4.4 修改 `Bridge.FinalizeSegment`

当前 `FinalizeSegment` 顺序是：

```go
svc.EndSession(...)
db.FinalizeMemorySegment(...)
log finalized
return nil
```

建议改成：

```go
svc.EndSession(...)
if extraction enabled && trigger_on_finalize_segment {
    run extraction for segment.MemorySessionID
    // 抽取失败只记录 warning，不回滚 EndSession / FinalizeSegment
}
db.FinalizeMemorySegment(...)
log finalized
return nil
```

是否先 `db.FinalizeMemorySegment` 再抽取？

推荐顺序：

```text
1. MemoryCore EndSession
2. 主仓库 DB finalize segment
3. 自动抽取
```

原因：

```text
- 即使抽取失败，segment 生命周期也已经闭合
- 抽取是派生任务，不应阻止 session finalization
- 后续可以根据 finalized segment 找到可重试对象
```

伪代码：

```go
func (b *Bridge) FinalizeSegment(ctx context.Context, segmentID string, reason string, summary string) error {
    ...
    if _, err := b.host.Service.EndSession(...); err != nil {
        return err
    }

    if err := b.db.FinalizeMemorySegment(ctx, segmentID, reason, summary); err != nil {
        return err
    }

    if b.host.ExtractorEnabled() {
        result, err := b.host.ExtractSessionEnd(ctx, personaID, segment.MemorySessionID)
        if err != nil {
            b.logger.Warn("memory extraction failed",
                "chat_session_id", segment.ChatSessionID,
                "segment_id", segment.ID,
                "memory_session_id", segment.MemorySessionID,
                "error", err,
            )
        } else {
            b.logger.Info("memory extraction completed",
                "chat_session_id", segment.ChatSessionID,
                "segment_id", segment.ID,
                "memory_session_id", segment.MemorySessionID,
                "status", result.Status,
                "accepted", result.AcceptedFactCount,
                "applied", result.AppliedFactCount,
            )
        }
    }

    return nil
}
```

如果 `ExtractionRunResult` 里字段名和当前 MemoryCore 不完全一致，按实际 public API 调整。

---

## 5. ExtractionRunner 实现要点

### 5.1 打开同一个 MemoryCore SQLite DB

MemoryCore integration guide 要求 runner 传入访问同一 SQLite 数据库的 `*sql.DB`。

建议：

```go
sqlDB, err := sql.Open("sqlite", host.DBPath)
sqlDB.SetMaxOpenConns(1)
```

注意：

```text
- 使用 modernc.org/sqlite driver
- 与 memorycore.Service 指向同一个 DBPath
- 不要另建内存 DB
- 不要从主项目业务 DB 读 episode，episode 权威在 MemoryCore SQLite
```

### 5.2 构造 BuildRequest

session_end MVP：

```go
req, err := extractionruntime.BuildRequest(ctx, sqlDB, extractionruntime.BuildRequestOptions{
    PersonaID:                 personaID,
    SessionID:                 &memorySessionID,
    Trigger:                   memorycore.ExtractionTriggerSessionEnd,
    Limit:                     cfg.Limit,       // 50 起步
    Timezone:                  cfg.Timezone,    // 默认 Asia/Shanghai 或从 user/persona 配置取
    AllowInference:            cfg.AllowInference,
    AllowSensitiveExtraction:  cfg.AllowSensitiveExtraction, // 默认 false
    MaxFacts:                  cfg.MaxFacts,    // 12
    MaxLinks:                  cfg.MaxLinks,    // 20
    Now:                       time.Now(),
})
```

默认值建议：

```yaml
limit: 50
timezone: Asia/Shanghai
allow_inference: true
allow_sensitive_extraction: false
max_facts: 12
max_links: 20
```

### 5.3 LLM Provider 选择

第一版有两个可选方案。

#### 方案 A：OpenAI-compatible provider

优点：

```text
- 接入最快
- 与 MemoryCore 文档一致
- 不改 EmoAgent 现有 LLM client
```

缺点：

```text
- LLM 配置与 EmoAgent 主 LLM 配置割裂
```

示例：

```go
llm := extractionruntime.NewOpenAICompatibleLLM(extractionruntime.OpenAICompatibleOptions{
    BaseURL:   cfg.BaseURL,
    APIKeyEnv: cfg.APIKeyEnv, // MEMORYCORE_LLM_API_KEY
    Model:     cfg.Model,
    Timeout:   cfg.Timeout,
    MaxTokens: cfg.MaxTokens,
})
```

#### 方案 B：封装 EmoAgent 现有 LLM client

优点：

```text
- 复用主项目 provider / preset / model 配置
- 后续可以统一日志、token 预算、代理、重试
```

缺点：

```text
- 需要写 adapter
- 要保证输出 strict JSON，不要把 CoT 或普通文本写入 audit
```

建议先用方案 A 打通链路，之后替换为方案 B。

### 5.4 Runner.Run 参数

```go
result, err := runner.Run(ctx, memorycore.ExtractionRunRequest{
    Request:       req,
    Mode:          memorycore.ExtractionRunModeApply,
    ProviderID:    cfg.ProviderID,
    ProviderKind:  cfg.ProviderKind,
    Model:         cfg.Model,
    Temperature:   0,
    MaxTokens:     cfg.MaxTokens,
    Timeout:       cfg.Timeout,
    RepairEnabled: cfg.RepairEnabled,
    Audit:         memorycore.ExtractionAuditOn,
})
```

MVP 灰度顺序：

```text
dev:      dry_run
local:    dry_run + compare result
staging:  apply, only session_end
prod:     apply, session_end, low volume
future:   idle_detect
```

### 5.5 结果处理

把这些状态视为成功：

```text
ExtractionRunStatusApplied
ExtractionRunStatusNothingApplied
ExtractionRunStatusSkipped
```

把其他状态作为 warning / metric，但不要影响主对话：

```go
switch result.Status {
case memorycore.ExtractionRunStatusApplied,
     memorycore.ExtractionRunStatusNothingApplied,
     memorycore.ExtractionRunStatusSkipped:
    return nil
default:
    return fmt.Errorf("memory extraction status=%s code=%s", result.Status, result.SanitizedErrorCode)
}
```

### 5.6 幂等与重复抽取

MemoryCore extraction runtime 已经有 audit / fingerprint 方向，但主仓库仍应加一层运行策略：

```text
- 同一个 memory_session_id + trigger=session_end 不重复 apply
- RolloverSegment 调用 FinalizeSegment 时不要重复抽取
- 如果 dry_run 后切 apply，允许重跑
- LLM 失败可重试，但不要在每轮对话同步重试
```

建议做一个主仓库级轻量 check：

```text
memory_extraction_runs 或直接依赖 MemoryCore extraction_runs audit
```

如果 MemoryCore 已有 `extraction_runs` audit store，就优先复用，不在主仓库业务 DB 另建表。

---

## 6. 配置建议

### 6.1 EmoAgent 主配置

建议新增：

```yaml
memory:
  enabled: true
  memorycore_config: "./configs/memorycore.yaml"

  extraction:
    enabled: true
    mode: "dry_run" # validate | dry_run | apply

    trigger_on_finalize_segment: true
    trigger_on_idle: false

    limit: 50
    timezone: "Asia/Shanghai"
    allow_inference: true
    allow_sensitive_extraction: false
    max_facts: 12
    max_links: 20

    provider:
      kind: "openai-compatible"
      id: "memory_extractor"
      base_url: "https://api.example.com/v1"
      api_key_env: "MEMORYCORE_LLM_API_KEY"
      model: "memory-extractor"
      timeout_seconds: 60
      max_tokens: 4096
      temperature: 0

    repair_enabled: true
    audit_enabled: true
    use_prefilter: true

    failure_policy:
      block_chat: false
      log_sanitized_error: true
      retry_on_next_finalize: true
```

### 6.2 MemoryCore 配置

保持现有：

```yaml
enabled: true
core:
  db_path: "./data/memory.db"
  auto_migrate: true
  enable_fts: true
```

不要在主项目里绕过 MemoryCore config 去操作 migrations。

---

## 7. Prompt 注入侧需要小改吗？

MVP 不必大改。

当前 `FormatMemoryContext` 已经能把 MemoryCore 返回的 `MemoryContext` 转成 prompt block。它的问题是比较扁平：

```text
- 只输出 item.Summary
- 没有保留 block_type
- 没有 usage_guidance / historical_status
```

自动抽取接入后，第一版可以先不改，确认事实能写入和检索。第二阶段建议改为保留：

```text
block.BlockType
item.Summary
item.UsageGuidance
item.HistoricalStatus
```

推荐格式：

```text
[长期记忆上下文]
这些是允许用于当前回复的长期记忆。自然使用，不要炫耀记得；不要把历史事实说成当前事实。

[Relevant active memories]
- 用户喜欢手冲咖啡。使用约束：可用于饮品建议，不要主动展开购买记录。

[Boundaries]
- 用户不想再聊某话题。使用约束：避开，不要复述原话。
```

---

## 8. 为什么自动抽取会改善当前 Forget 和检索质量

### 8.1 检索质量差的主要原因

当前 FTS 检索能搜到的内容取决于已经写入的 facts / search documents。只有手动 pin 的少量规则事实时，检索质量必然不稳定：

```text
自然对话 episode 很多
长期 facts 很少
FTS 可搜文档少
MemoryContext 不够丰富
Forget 没有准确 target
```

自动抽取补的是：

```text
episode → facts / links / affect_events → searchable facts
```

也就是说，它让 FTS 有“经过压缩和结构化的对象”可搜，而不是指望 raw chat log 直接承担长期记忆。

### 8.2 Forget 需要 fact id

MemoryCore 当前 Forget 的稳定支持范围是 exact node：

```text
soft_forget: fact
hard_forget: fact
source_redact: episode
purge: fact 或 episode
scope_mode: exact_node
```

所以主仓库要想可靠处理用户说“忘掉这个偏好”，必须先能稳定定位：

```text
fact_id
episode_id
derived nodes
```

自动抽取能让系统产生可定位 fact id；之后的 manual forget 才能做 target resolver。

---

## 9. 安全边界和不变量

实施时不要破坏这些不变量：

```text
1. Episode 是证据，不是事实。
2. Fact 是节点，不是边。
3. Extractor 只提出候选；Consolidation 决定写入。
4. SQLite 是权威库；FTS / mirror / sidecar 不是权威。
5. Work 原始日志不能自动进入长期记忆。
6. Work 只能提交 memory_candidates，且需要 Emotion / Memory 审批。
7. Agent Affect 不能写成用户 fact。
8. 用户删除 / 隐藏 / purge 优先于 pin、importance、检索分数。
9. 抽取失败不能阻塞聊天。
10. 检索注入必须经过 MemoryCore Retrieve 的 authority filters。
11. Sensitive extraction 默认关闭或进入 review。
12. 不要把助手建议、玩笑、假设场景固化为用户事实。
```

---

## 10. 测试计划

### 10.1 主仓库单元测试

新增：

```text
internal/memoryhost/extraction_test.go
```

用 fake LLM / fake MemoryCore DB 测：

```text
1. session_end 会调用 extractionruntime.Runner。
2. dry_run 不写 fact，但有 audit。
3. apply 会把 accepted fact 写入 MemoryCore。
4. 同一 segment finalize 两次不会重复抽取。
5. extraction 失败只记录 warning，不让 FinalizeSegment 失败。
6. allow_sensitive_extraction=false 时敏感候选不自动写入。
```

### 10.2 主仓库集成测试

构造：

```text
用户：我喜欢手冲咖啡，尤其是浅烘。
助手：记下了...
FinalizeSegment
Extraction apply
新 session query: 给我推荐一个下午饮品
RetrievePromptBlock
```

断言：

```text
- MemoryCore facts 表出现 stable_preference
- memory_search_documents / FTS 可搜到“手冲咖啡”
- RetrievePromptBlock 包含“用户喜欢手冲咖啡”
- Prompt block 不包含本轮 excluded episode 原文
```

### 10.3 Forget 暂缓测试

现在不要求“别再提”直接删除。只要求：

```text
- ManualMemoryIntentForget 能被识别
- 不误写成 has_boundary fact，除非用户明确说“请记住我不想聊 X”
- 有日志说明当前未执行 exact-node Forget
```

### 10.4 MemoryCore 侧回归

在 MemoryCore 仓库跑：

```bash
go test ./...
go run ./cmd/memory-eval --suite retrieval --mode full
```

主仓库接入前先用 MemoryCore CLI 离线验证：

```bash
go run ./cmd/memoryctl init-db --db ./data/memory.db
go run ./cmd/memoryctl extract-run ...
go run ./cmd/memoryctl retrieve ...
```

具体 flags 以 MemoryCore 当前 `memoryctl --help` 为准。

---

## 11. 实施阶段拆分

### Stage 1：离线验证

目标：

```text
不用改 EmoAgent 主流程，先用 MemoryCore CLI 对真实或合成 session episode 做 extract-run / retrieve。
```

产出：

```text
- 1 个 synthetic conversation fixture
- 1 组 accepted facts
- 1 次 retrieve 命中
```

### Stage 2：主仓库接入 dry_run

目标：

```text
FinalizeSegment 后自动跑 extraction dry_run。
```

不写 facts，只看 audit 和 log。

验收：

```text
- 不影响聊天
- 不影响 segment finalize
- extraction JSON schema 稳定
- sensitive / tool noise 被拒绝
```

### Stage 3：主仓库接入 apply

目标：

```text
session_end 自动写入低风险 facts。
```

验收：

```text
- stable_preference / core_identity / commitment 能写入
- RetrievePromptBlock 下一轮能召回
- 重复 finalize 不重复写入
```

### Stage 4：idle extraction

目标：

```text
对长会话做轻量增量抽取。
```

策略：

```text
- debounce
- limit 最近 N 条
- max_facts 更低，例如 4
- 只允许 explicit high-confidence
- failure 不阻塞
```

### Stage 5：manual forget target resolver

目标：

```text
把“忘掉这个偏好”解析到 fact id，并调用 svc.Forget(exact_node)。
```

依赖：

```text
- facts 数量和质量稳定
- retrieve 能稳定返回候选
- confirmation UX 准备好
```

---

## 12. 建议提交粒度

建议拆成 5 个 PR / commits：

```text
1. feat(memory): add extraction config and host wiring
2. feat(memory): add MemoryCore extraction runner wrapper
3. feat(memory): run session-end extraction on finalized memory segment
4. test(memory): add extraction integration tests with fake LLM
5. docs(memory): document automatic extraction flow and rollout
```

不要把 forget resolver、idle scheduler、LLM provider 重构塞进同一个提交。

---

# 13. Codex `/goal` 模式提示词

下面这段可以直接贴给 Codex `/goal`。

```text
/goal
在 EmoAgent 主仓库中把 EmoAgent-MemoryCore 的自动抽取记忆运行时接入当前 MemoryCore 生命周期桥接、FTS 检索和 Prompt 注入链路。

背景：
- 主仓库：github.com/LongYiSang/EmoAgent
- MemoryCore 仓库：github.com/LongYiSang/EmoAgent-MemoryCore
- 主仓库最近已完成：
  1. 引入 MemoryCore，本地 replace
  2. add MemoryCore lifecycle host
  3. 接入 Memory Segment 桥接
  4. 接入手动固定记忆、FTS 记忆检索和 Prompt 注入
- 当前主仓库 `internal/memory/memory.go` 只是 MemoryCore public facade 的 alias / forwarding。
- 当前主仓库主要集成点是 `internal/memoryhost/host.go`、`bridge.go`、`manual_rules.go`。
- 当前 `Bridge` 已经负责：
  - EnsureSegment / RolloverSegment
  - AppendUserEpisode / AppendAssistantEpisode
  - RetrievePromptBlock
  - FinalizeSegment
  - manual pin 通过 `ConsolidateCandidate` 写入 MemoryCore
- 当前 manual forget 只识别 prefix，但 `applyManualMemoryIntent` 中 `ManualMemoryIntentForget` 是 no-op。不要在本任务中实现自然语言 Forget target resolver。
- MemoryCore 当前提供 public 子包：
  `github.com/longyisang/emoagent-memorycore/pkg/memorycore/extractionruntime`
- MemoryCore `extractionruntime.Runner` 需要：
  - 访问同一个 MemoryCore SQLite DB 的 `*sql.DB`
  - `memorycore.Service`
  - `memorycore.ExtractionLLM`
  - `AuditStore`
- MemoryCore integration guide 中已给出 `BuildRequest` + `NewOpenAICompatibleLLM` + `NewRunner` + `runner.Run` 的接入形态。

核心目标：
在主仓库 `internal/memoryhost` 中新增自动抽取记忆能力，使得 memory segment finalize / session_end 后可以自动从该 MemoryCore session 的 episodes 中抽取长期记忆候选，并通过 MemoryCore consolidation 写入 SQLite + FTS。下一轮 `RetrievePromptBlock` 应能检索并注入这些自动抽取出来的 facts。

强约束：
1. 不要直接写 MemoryCore 的 facts / FTS / memory_search_documents 表。
2. 不要 import MemoryCore 的 `internal/...` 包，只用 public API：
   - `pkg/memorycore`
   - `pkg/memorycore/extractionruntime`
3. 不要改变现有 RetrievePromptBlock 的基本行为。
4. 不要把 extraction 失败变成聊天或 FinalizeSegment 的硬失败；默认只记录 warning。
5. 不要在本任务中实现 manual forget 的自然语言目标解析。
6. 不要让 Work 原始日志自动进入长期记忆。
7. 不要让 Agent Affect 或助手自己的情绪写成用户 fact。
8. Sensitive extraction 默认关闭或由 MemoryCore gate review。
9. 保留 SQLite authority：任何检索注入仍走 `memorycore.Service.Retrieve`。
10. 保持现有测试通过。

具体任务：
1. 在 `internal/memoryhost` 新增 extraction runner 包装：
   - 文件建议：`extraction.go`、`extraction_config.go`
   - 定义 `ExtractionConfig`
   - 定义 `ExtractionRunner`
   - `NewExtractionRunner(ctx, host, cfg, logger)` 打开同一个 `host.DBPath` 的 SQLite DB
   - 使用 `modernc.org/sqlite`
   - `sqlDB.SetMaxOpenConns(1)`
   - 创建 `extractionruntime.NewSQLiteAuditStore(sqlDB)`
   - 支持 OpenAI-compatible provider：
     `extractionruntime.NewOpenAICompatibleLLM`
   - 预留将来用主仓库 LLM client 实现 `memorycore.ExtractionLLM` 的接口位置

2. 给 `ExtractionRunner` 实现方法：
   - `ExtractSessionEnd(ctx context.Context, personaID string, memorySessionID string) (*memorycore.ExtractionRunResult, error)`
   - 内部调用 `extractionruntime.BuildRequest`
   - BuildRequestOptions：
     - PersonaID
     - SessionID
     - Trigger: `memorycore.ExtractionTriggerSessionEnd`
     - Limit 默认 50
     - Timezone 默认 `Asia/Shanghai`
     - AllowInference 默认 true
     - AllowSensitiveExtraction 默认 false
     - MaxFacts 默认 12
     - MaxLinks 默认 20
     - Now: time.Now()
   - 调用 `runner.Run`：
     - Mode 从 config 读取，支持 validate / dry_run / apply
     - Temperature 0
     - MaxTokens 默认 4096
     - Timeout 默认 60s
     - RepairEnabled 默认 true
     - Audit 默认 on
   - 将以下状态视为非错误：
     - Applied
     - NothingApplied
     - Skipped
   - 其他状态返回 sanitized error，不包含用户原文

3. 修改 `Host`：
   - 保存可选 `extractionRunner`
   - `Close()` 时关闭 extraction runner 的 sqlDB
   - 提供方法：
     - `ExtractionEnabled() bool`
     - `ExtractSessionEnd(ctx, personaID, memorySessionID string) (*memorycore.ExtractionRunResult, error)`
   - 不破坏现有 `OpenFromConfig` / `OpenWithOptions`

4. 修改主项目配置：
   - 增加 `memory.extraction` 配置结构
   - 字段至少包括：
     - enabled
     - mode: validate | dry_run | apply
     - trigger_on_finalize_segment
     - limit
     - timezone
     - allow_inference
     - allow_sensitive_extraction
     - max_facts
     - max_links
     - provider.kind
     - provider.id
     - provider.base_url
     - provider.api_key_env
     - provider.model
     - provider.timeout_seconds
     - provider.max_tokens
     - provider.temperature
     - repair_enabled
     - audit_enabled
   - 默认 enabled=false 或 mode=dry_run，避免意外写库

5. 修改 `Bridge.FinalizeSegment`：
   - 保持原有 EndSession 和主仓库 segment finalize 行为
   - 在 segment 成功 finalize 后，如果 extraction enabled 且 trigger_on_finalize_segment=true：
     - 调用 `host.ExtractSessionEnd(ctx, personaID, segment.MemorySessionID)`
     - 成功记录 info log：status、memory_session_id、segment_id
     - 失败记录 warning log：sanitized error，不返回失败
   - 不要同步阻塞过久；使用 config timeout
   - 不要重复抽取已经 finalized 的 segment。如果当前 MemoryCore audit 已提供 idempotency/fingerprint，优先复用；否则至少保证同一次 FinalizeSegment 重入不会重复触发

6. 保持 manual pin 行为：
   - 当前 manual pin 仍通过 `ConsolidateCandidate`
   - 不要删除或重构 manual_rules
   - manual forget 仍不要执行猜测性删除；可以增加 warning/info log 表示 detected but target resolver not implemented

7. 测试：
   - 新增 `internal/memoryhost/extraction_test.go`
   - 用 fake/mock `memorycore.ExtractionLLM` 返回合法 extraction JSON
   - 覆盖：
     1. extraction disabled 时 FinalizeSegment 不调用 runner
     2. dry_run 不阻塞 finalize
     3. apply 后事实可通过 MemoryCore Retrieve / FTS 检索到
     4. extraction 失败不使 FinalizeSegment 返回错误
     5. repeated FinalizeSegment 不重复写入
     6. manual forget prefix 不会误写入普通 fact
   - 跑 `go test ./...`

验收标准：
- `go test ./...` 通过。
- 在本地启用 `memory.extraction.enabled=true`、`mode=apply` 后：
  1. 启动 EmoAgent
  2. 创建一个 session
  3. 用户说“我喜欢手冲咖啡，尤其是浅烘。”
  4. assistant 正常回复
  5. finalize / rollover segment
  6. extraction runner 成功 applied 或 nothing_applied
  7. 新一轮问“下午喝点什么好？”
  8. `RetrievePromptBlock` 能注入类似“用户喜欢手冲咖啡/浅烘”的长期记忆
- 如果 extraction LLM 失败，用户聊天、session finalization 和 retrieval 不应失败。
- 不应新增任何直接写 MemoryCore 内部表的代码。
- 不应新增对 MemoryCore `internal/...` 包的 import。
```

---

## 14. 实施后下一步

自动抽取接入稳定后，再开始 manual Forget：

```text
自然语言 forget
  → 精确识别 target description
  → 高精度 retrieve candidate facts
  → 若唯一高置信，调用 svc.Forget(exact_node)
  → 若多候选，询问用户确认
  → verify SQLite / FTS / mirror 不再召回
```

这时 Forget 的成功率会明显高于现在，因为系统已经有结构化 facts、source_episode_ids、memory_links 和可检索 search documents 作为目标锚点。
