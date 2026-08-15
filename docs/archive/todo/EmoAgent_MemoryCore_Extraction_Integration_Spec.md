# EmoAgent MemoryCore Extraction Integration Spec

> **Document status**: Codex implementation spec  
> **Version**: 0.1  
> **Date**: 2026-05-28  
> **Target repo**: `LongYiSang/EmoAgent`  
> **Recommended target path**: `docs/specs/emoagent_memorycore_extraction_integration_spec.md`  
> **Depends on**: MemoryCore `Service.RunExtraction` facade from `memorycore_extraction_service_facade_spec.md`。  
> **Scope**: 将 EmoAgent 的 memoryhost extraction 接入从“本地 ExtractionRunner + 单独 DB + direct ConsolidateCandidate”迁移为“只调用 MemoryCore public Service API”。  
> **Non-goals**: 不重做 retrieval 主链路，不实现完整 memory management UI，不实现复杂 topic forget，不实现 Agent Affect Simulation，不强制做 block-aware formatter；block-aware formatter 可作为后续 PR。

---

## 0. 一句话目标

MemoryCore facade 合并后，EmoAgent 应只依赖：

```go
memorycore.Service
```

来完成长期记忆读写。

目标形态：

```text
EmoAgent Bridge / Host
  ├─ AppendEpisode
  ├─ RetrievePromptBlock -> Service.Retrieve
  ├─ FinalizeSegment -> Service.EndSession -> Service.RunExtraction(session_end)
  ├─ manual_pin -> Service.RunExtraction(manual_pin, episode_ids=[last_user_episode])
  └─ manual_forget -> Service.RunExtraction(manual_forget, episode_ids=[last_user_episode]) -> routed deletion intent / future Forget Manager
```

EmoAgent 不再自己打开 extraction SQLite，不再自己创建 `pkg/memorycore/extractionruntime.Runner`，不再绕过 extraction protocol 直接构造 `ConsolidateCandidate`。

---

## 1. 当前代码事实

截至本 Spec 编写时，EmoAgent memoryhost 当前大致是：

```text
internal/memoryhost/host.go
  Host 持有 Service memorycore.Service；
  Host 还有 extractionRunner *ExtractionRunner；
  ExtractSessionEnd 委托 h.extractionRunner.ExtractSessionEnd；
  ExtractionEnabled / extractionTriggerOnFinalizeSegment 都依赖 extractionRunner。

internal/memoryhost/extraction.go
  ExtractionRunner 自己 sql.Open(host.DBPath)；
  自己创建 extractionruntime.NewOpenAICompatibleLLM；
  自己创建 extractionruntime.NewSQLiteAuditStore(db)；
  自己创建 extractionruntime.NewRunner(DB, Service, LLM, AuditStore)；
  ExtractSessionEnd 自己 BuildRequest + runner.Run。

internal/memoryhost/bridge.go
  RetrievePromptBlock 调 Service.Retrieve 后调用 FormatMemoryContext；
  FinalizeSegment 调 Service.EndSession 后触发 extractFinalizedSegment；
  appendEpisode 写 episode 后，user role 会调用 applyManualMemoryIntent；
  manual pin 当前直接 EnsureEntity + ConsolidateCandidate；
  manual forget 当前只 log resolver_not_implemented；
  FormatMemoryContext 当前把 MemoryContext.Blocks flatten 成 bullet list。
```

本次改造重点是收敛 extraction 写入入口，不改变 retrieval 行为。

---

## 2. 必须保持的工程不变量

### 2.1 Memory 属于 Emotion 的关系连续性层

EmoAgent 的长期记忆服务关系连续性，不是 Work 日志库。Work 工具输出、错误栈、搜索页面和中间结果不能自动进入长期记忆。

### 2.2 只有 MemoryCore 决定长期写入

EmoAgent host 不应绕过 MemoryCore extraction gate / consolidation / forget policy。

禁止继续新增类似：

```go
host.Service.ConsolidateCandidate(... manual pin fact ...)
```

作为默认路径。

### 2.3 manual_forget 用户意图优先

用户说“忘掉 / 别记 / 别再提 / 删除”时：

```text
- 不应被转换成普通 stable fact。
- 不应只写日志后无动作。
- P0 至少要走 Service.RunExtraction(trigger=manual_forget) 并拿到 routed deletion intents。
- 后续 exact fact / exact episode 才调用 Forget / PreviewForget / ExecuteForget。
```

### 2.4 extraction 失败不阻塞主对话

长期记忆抽取是后台/旁路能力。失败时：

```text
- 不应导致用户消息发送失败。
- 不应导致 segment finalize 失败，除非 EndSession 本身失败。
- 只能记录 sanitized log。
```

### 2.5 不记录 raw private text 到普通日志

日志只能写：

```text
status
trigger
chat_session_id
segment_id
memory_session_id
accepted_count
review_count
rejected_count
routed_count
applied_count
skipped_by_fingerprint
sanitized_error_code
```

不要记录：

```text
raw user message
raw extraction prompt
raw provider response
target_description 原文
敏感删除目标
```

### 2.6 Agent Affect 不在本 PR 实现

本 PR 不实现 Agent Affect Simulation。不要把 Agent 情绪写成 memory fact，也不要让 retrieval/extraction 接入 agent_affect 特殊行为。

---

## 3. 本次改造范围

### 3.1 P0 必做

```text
1. Host 不再持有真实 extractionruntime.Runner。
2. Host.ExtractSessionEnd 改成调用 h.Service.RunExtraction。
3. internal/memoryhost/extraction.go 删除或改成薄兼容 adapter，不再打开 sqlite，不再创建 runner。
4. OpenFromConfig / OpenWithOptions 只通过 memorycore.Options.Extraction 配置 MemoryCore。
5. FinalizeSegment 后仍触发 session_end extraction，但通过 Service.RunExtraction。
6. manual pin 改成 Service.RunExtraction(trigger=manual_pin, episode_ids=[last_user_episode], mode=apply)。
7. manual forget 改成 Service.RunExtraction(trigger=manual_forget, episode_ids=[last_user_episode], mode=dry-run 或 apply) 并处理 RoutedDeletionIntents。
8. extraction disabled / provider disabled / extraction failed 时只写 sanitized log，不阻塞主流程。
9. 增加 memoryhost tests。
```

### 3.2 P1 后续

```text
1. manual_forget exact target resolver 接 Service.PreviewForget / ExecuteForget。
2. block-aware FormatMemoryContext，保留 block_type / usage_guidance / historical_status / do_not_overstate。
3. developer debug panel 或 structured debug endpoint。
4. dogfood metrics：每轮 extraction/retrieval 统计。
```

### 3.3 暂缓

```text
- broad topic forget。
- UI memory management panel。
- Agent Affect Simulation。
- 完整自然语言 forget target resolver。
- retrieval ranking 参数优化。
```

---

## 4. Host 结构改造

### 4.1 当前 Host

当前：

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

### 4.2 目标 Host

目标：

```go
type Host struct {
    Service memorycore.Service
    Source string
    DBPath string

    retrievalPolicy memorycore.RetrievalPolicy
    extractionPolicy ExtractionHostPolicy

    logger *slog.Logger
}
```

新增：

```go
type ExtractionHostPolicy struct {
    Enabled bool

    TriggerOnFinalizeSegment bool
    TriggerOnManualPin bool
    TriggerOnManualForget bool

    SessionEndMode memorycore.ExtractionRunMode
    ManualPinMode memorycore.ExtractionRunMode
    ManualForgetMode memorycore.ExtractionRunMode

    Timezone string
    Limit int
}
```

默认建议：

```go
ExtractionHostPolicy{
    Enabled: false,
    TriggerOnFinalizeSegment: true,
    TriggerOnManualPin: true,
    TriggerOnManualForget: true,
    SessionEndMode: memorycore.ExtractionRunModeApply,
    ManualPinMode: memorycore.ExtractionRunModeApply,
    ManualForgetMode: memorycore.ExtractionRunModeDryRun,
    Timezone: "Asia/Singapore",
    Limit: 50,
}
```

说明：

```text
- Enabled 应来自 MemoryCore config 的 extraction.enabled 或 EmoAgent host config 显式开关。
- provider/model/raw-log 等不再存在于 EmoAgent host policy。
- Host 只控制触发时机和 mode，不控制 provider 细节。
```

### 4.3 兼容策略

如果当前还有调用：

```go
h.SetExtractionRunner(runner)
```

建议 P0 这样处理：

```text
- 标记 deprecated。
- 方法保留但不再推荐使用。
- 如果测试依赖，可让它只设置 extractionPolicy.Enabled=true，不保存 runner。
- 更好做法是同步更新测试，删除 SetExtractionRunner 使用。
```

不要保留一个会继续打开 sqlite 的 runner 路径。

---

## 5. Host.ExtractSessionEnd 改造

### 5.1 目标实现

```go
func (h *Host) ExtractSessionEnd(
    ctx context.Context,
    personaID string,
    memorySessionID string,
) (*memorycore.ExtractionRunResult, error) {
    if h == nil || h.Service == nil || !h.ExtractionEnabled() {
        return nil, nil
    }

    memorySessionID = strings.TrimSpace(memorySessionID)
    if memorySessionID == "" {
        return nil, sanitizedExtractionError("missing_session", "")
    }

    personaID = defaultPersonaID(personaID)
    sessionID := memorySessionID

    result, err := h.Service.RunExtraction(ctx, memorycore.RunExtractionRequest{
        PersonaID: personaID,
        SessionID: &sessionID,
        Trigger: memorycore.ExtractionTriggerSessionEnd,
        Timezone: h.extractionPolicy.timezoneOrDefault(),
        Mode: h.extractionPolicy.sessionEndModeOrDefault(),
        Build: &memorycore.ExtractionBuildSelector{
            SessionID: &sessionID,
            Limit: h.extractionPolicy.limitOrDefault(),
        },
    })
    if err != nil {
        return result, sanitizedExtractionError(
            extractionErrorCode(result, err),
            extractionErrorMessage(result, err),
        )
    }
    return result, nil
}
```

### 5.2 ExtractionEnabled

目标：

```go
func (h *Host) ExtractionEnabled() bool {
    return h != nil && h.Service != nil && h.extractionPolicy.Enabled
}

func (h *Host) extractionTriggerOnFinalizeSegment() bool {
    return h != nil && h.ExtractionEnabled() && h.extractionPolicy.TriggerOnFinalizeSegment
}
```

不要再依赖 `h.extractionRunner != nil`。

---

## 6. internal/memoryhost/extraction.go 处理

### 6.1 推荐处理

如果 MemoryCore facade 已可用，`internal/memoryhost/extraction.go` 中的旧 `ExtractionRunner` 应删除。

如果为了减小 diff 保留文件，则改成薄 adapter，不再打开 DB 或创建 runner：

```go
type ExtractionRunner struct {
    host *Host
    cfg ExtractionConfig
    logger *slog.Logger
}

func NewExtractionRunner(ctx context.Context, host *Host, cfg ExtractionConfig, logger *slog.Logger) (*ExtractionRunner, error) {
    if !cfg.Enabled {
        return nil, nil
    }
    if host == nil || host.Service == nil {
        return nil, fmt.Errorf("memory host is not configured")
    }
    return &ExtractionRunner{host: host, cfg: cfg.normalized(), logger: logger}, nil
}

func (r *ExtractionRunner) ExtractSessionEnd(ctx context.Context, personaID string, memorySessionID string) (*memorycore.ExtractionRunResult, error) {
    if r == nil || r.host == nil {
        return nil, nil
    }
    return r.host.ExtractSessionEnd(ctx, personaID, memorySessionID)
}
```

但更推荐直接删除 runner concept，让 Host 自己管理 policy。

### 6.2 必须删除的依赖

改造后，EmoAgent 不应再 import：

```go
"database/sql"
"github.com/longyisang/emoagent-memorycore/pkg/memorycore/extractionruntime"
_ "modernc.org/sqlite"
```

除非其他 unrelated 文件确实需要。

---

## 7. Config 接入

### 7.1 原则

EmoAgent 不再复制 MemoryCore extraction provider 配置。provider、model、audit、raw_log、response_format 等应由 MemoryCore config 进入：

```go
runtime, err := cfg.Runtime()
svc, err := memorycore.Open(ctx, runtime.Options)
```

`runtime.Options.Extraction` 应由 MemoryCore config package 负责填好。

### 7.2 EmoAgent 只保留 host trigger policy

如果 EmoAgent 需要自己的配置，只保留：

```yaml
memory_host:
  extraction:
    enabled: true
    trigger_on_finalize_segment: true
    trigger_on_manual_pin: true
    trigger_on_manual_forget: true
    session_end_mode: apply
    manual_pin_mode: apply
    manual_forget_mode: dry-run
    limit: 50
    timezone: Asia/Singapore
```

不要再配置：

```text
provider.base_url
provider.api_key_env
provider.model
provider.temperature
provider.max_tokens
raw_log.directory
audit.enabled
```

这些属于 MemoryCore。

### 7.3 OpenFromConfig

当前 `OpenFromConfig` 会加载 MemoryCore config 并调用 `cfg.Runtime()`。目标：

```text
- 从 runtime.Options.Extraction.Enabled 推导 host.extractionPolicy.Enabled。
- 从 config 中可选的 host trigger policy 覆盖 trigger/mode/limit/timezone。
- 如果没有 EmoAgent host trigger policy，则使用安全默认。
```

如果当前 config package 不包含 EmoAgent-specific host policy，则 P0 可简单：

```go
host.extractionPolicy = ExtractionHostPolicy{
    Enabled: runtime.Options.Extraction.Enabled,
    TriggerOnFinalizeSegment: true,
    TriggerOnManualPin: true,
    TriggerOnManualForget: true,
    SessionEndMode: runtime.Options.Extraction.Defaults.Mode,
    ManualPinMode: memorycore.ExtractionRunModeApply,
    ManualForgetMode: memorycore.ExtractionRunModeDryRun,
    Timezone: runtime.Options.Extraction.Defaults.Timezone,
    Limit: 50,
}
```

---

## 8. Bridge.FinalizeSegment 改造

### 8.1 保留当前主流程

当前 `FinalizeSegment` 流程是正确的：

```text
GetMemorySegment
Service.EndSession
db.FinalizeMemorySegment
log finalized
extractFinalizedSegment
```

本次只改变 `extractFinalizedSegment` 的实现路径。

### 8.2 extractFinalizedSegment 目标

```go
func (b *Bridge) extractFinalizedSegment(ctx context.Context, segment *storage.MemorySegment, personaID string) {
    if b == nil || b.host == nil || !b.host.extractionTriggerOnFinalizeSegment() || segment == nil {
        return
    }

    result, err := b.host.ExtractSessionEnd(ctx, personaID, segment.MemorySessionID)
    if err != nil {
        b.logExtractionResult("memory extraction failed", segment, result, err)
        return
    }
    b.logExtractionResult("memory extraction completed", segment, result, nil)
}
```

### 8.3 Sanitized log fields

成功：

```go
logger.Info("memory extraction completed",
    "chat_session_id", segment.ChatSessionID,
    "segment_id", segment.ID,
    "memory_session_id", segment.MemorySessionID,
    "trigger", memorycore.ExtractionTriggerSessionEnd,
    "status", result.Status,
    "accepted", result.AcceptedCount,
    "review", result.ReviewCount,
    "rejected", result.RejectedCount,
    "routed", result.RoutedCount,
    "applied", result.AppliedCount,
    "skipped_by_fingerprint", result.SkippedByFingerprint,
)
```

失败：

```go
logger.Warn("memory extraction failed",
    "chat_session_id", segment.ChatSessionID,
    "segment_id", segment.ID,
    "memory_session_id", segment.MemorySessionID,
    "trigger", memorycore.ExtractionTriggerSessionEnd,
    "status", safeStatus(result),
    "error_code", safeErrorCode(result, err),
)
```

不要记录 `result.SanitizedErrorMessage` 如果它可能包含 provider 细节。只记录 code。

---

## 9. Manual pin 改造

### 9.1 当前问题

当前 manual pin 直接：

```text
manualRules.Match(content)
→ EnsureEntity(user)
→ 构造 candidate
→ Service.ConsolidateCandidate(... Approved=true)
```

这绕过了：

```text
LLM extraction schema
Go extraction gate
source/provenance gate
sensitive review
pin_intents
assistant-only/hypothetical/tool-noise 防线
```

### 9.2 目标行为

用户消息写入 episode 后：

```text
manualRules.Match(content) == ManualMemoryIntentPin
  ↓
Service.RunExtraction(trigger=manual_pin, episode_ids=[last_user_episode], mode=apply)
  ↓
MemoryCore extraction gate + consolidation 决定是否写入
```

### 9.3 目标代码

```go
func (b *Bridge) applyManualPinIntent(ctx context.Context, segment *storage.MemorySegment, sourceEpisodeID string) error {
    if b == nil || b.host == nil || b.host.Service == nil || segment == nil {
        return nil
    }
    if !b.host.extractionPolicy.TriggerOnManualPin || !b.host.ExtractionEnabled() {
        return nil
    }

    personaID := defaultPersonaID(segmentPersona(segment, b.db, ctx))
    memorySessionID := segment.MemorySessionID

    result, err := b.host.Service.RunExtraction(ctx, memorycore.RunExtractionRequest{
        PersonaID: personaID,
        SessionID: &memorySessionID,
        Trigger: memorycore.ExtractionTriggerManualPin,
        Timezone: b.host.extractionPolicy.timezoneOrDefault(),
        Mode: b.host.extractionPolicy.manualPinModeOrDefault(),
        Build: &memorycore.ExtractionBuildSelector{
            EpisodeIDs: []string{sourceEpisodeID},
            SessionID: &memorySessionID,
            Limit: 1,
        },
        Policy: memorycore.ExtractionPolicyOverride{
            ManualPin: boolPtr(true),
            AllowInference: boolPtr(true),
        },
    })
    b.logManualExtractionResult("manual memory pin", segment, result, err)
    if err != nil {
        return err
    }
    if result == nil || result.AppliedCount == 0 {
        return fmt.Errorf("manual pin not applied: status=%s accepted=%d review=%d rejected=%d",
            safeStatus(result), safeAccepted(result), safeReview(result), safeRejected(result))
    }
    return nil
}
```

### 9.4 ManualRules 的角色

`ManualRules` 仍可作为 cheap trigger detector，但不再负责构造 fact candidate。

它只做：

```text
None | Pin | Forget
```

不要再把 manual rule 的 parsed candidate 直接写入 MemoryCore。若需要，可以把 rule candidate 作为 future hint 传给 MemoryCore，但 P0 不做。

---

## 10. Manual forget 改造

### 10.1 当前问题

当前 manual forget 只写：

```text
status = resolver_not_implemented
```

这对用户信任很差。即使 P0 不执行删除，也至少要进入 MemoryCore 的 deletion_intents route。

### 10.2 P0 目标行为

用户消息写入 episode 后：

```text
manualRules.Match(content) == ManualMemoryIntentForget
  ↓
Service.RunExtraction(trigger=manual_forget, episode_ids=[last_user_episode], mode=dry-run)
  ↓
facts 被 Go gate reject
  ↓
deletion_intents route-only
  ↓
EmoAgent log sanitized summary
```

### 10.3 目标代码

```go
func (b *Bridge) applyManualForgetIntent(ctx context.Context, segment *storage.MemorySegment, sourceEpisodeID string) error {
    if b == nil || b.host == nil || b.host.Service == nil || segment == nil {
        return nil
    }
    if !b.host.extractionPolicy.TriggerOnManualForget || !b.host.ExtractionEnabled() {
        return nil
    }

    personaID := defaultPersonaID(segmentPersona(segment, b.db, ctx))
    memorySessionID := segment.MemorySessionID

    result, err := b.host.Service.RunExtraction(ctx, memorycore.RunExtractionRequest{
        PersonaID: personaID,
        SessionID: &memorySessionID,
        Trigger: memorycore.ExtractionTriggerManualForget,
        Timezone: b.host.extractionPolicy.timezoneOrDefault(),
        Mode: b.host.extractionPolicy.manualForgetModeOrDefault(),
        Build: &memorycore.ExtractionBuildSelector{
            EpisodeIDs: []string{sourceEpisodeID},
            SessionID: &memorySessionID,
            Limit: 1,
        },
        Policy: memorycore.ExtractionPolicyOverride{
            ManualForget: boolPtr(true),
            ApplyAcceptedFacts: boolPtr(false),
            ExecuteDeletionIntents: boolPtr(false),
        },
    })
    b.logManualExtractionResult("manual memory forget", segment, result, err)

    if err != nil {
        return err
    }
    if result == nil || result.RoutedCount == 0 {
        return fmt.Errorf("manual forget not routed: status=%s", safeStatus(result))
    }
    return nil
}
```

### 10.4 P1 exact forget

等 MemoryCore 支持 exact deletion intent execution 后，EmoAgent 可以升级：

```text
RunExtraction(trigger=manual_forget)
  ↓
if exact target resolved and requires_confirmation=false:
    Service.ExecuteForget(...)
else if preview available:
    ask confirmation / store pending action
else:
    safe not_found / ask user for more specific target
```

P0 不做 broad topic delete。

### 10.5 用户可见回复原则

如果主对话层要回用户确认，建议：

```text
manual pin 成功：我会记住这个。
manual pin review/blocked：这条我先不会自动长期记住。
manual forget route-only：我不会把这条当作新的长期记忆；具体删除范围我会按记忆系统的安全规则处理。
manual forget target unclear：我可以帮你忘掉，但需要更具体地说明是哪条记忆。
```

不要复述敏感删除目标。

---

## 11. applyManualMemoryIntent 改造

### 11.1 当前流程

当前：

```go
intent := b.manualRules.Match(content)
switch intent.Kind {
case Forget: logManualForgetDetected
case Pin: direct ConsolidateCandidate
}
```

### 11.2 目标流程

```go
func (b *Bridge) applyManualMemoryIntent(ctx context.Context, segmentID string, content string) error {
    if b == nil || b.manualRules == nil || b.host == nil || b.host.Service == nil || b.db == nil {
        return nil
    }

    intent := b.manualRules.Match(content)
    if intent.Kind == ManualMemoryIntentNone {
        return nil
    }

    segment, err := b.db.GetMemorySegment(ctx, segmentID)
    if err != nil {
        return err
    }
    if segment == nil {
        return fmt.Errorf("memory segment not found: %s", segmentID)
    }

    sourceEpisodeID := strings.TrimSpace(segment.LastUserEpisodeID)
    if sourceEpisodeID == "" {
        return fmt.Errorf("last user episode id is required for manual memory intent")
    }

    switch intent.Kind {
    case ManualMemoryIntentPin:
        return b.applyManualPinIntent(ctx, segment, sourceEpisodeID)
    case ManualMemoryIntentForget:
        return b.applyManualForgetIntent(ctx, segment, sourceEpisodeID)
    default:
        return nil
    }
}
```

### 11.3 appendEpisode 行为保持

`appendEpisode` 里：

```go
if role == memorycore.RoleUser {
    if err := b.applyManualMemoryIntent(ctx, segmentID, content); err != nil {
        b.logManualMemoryWarning("manual memory intent", segment.ChatSessionID, err)
    }
}
```

这条旁路失败不应让 episode append 失败。

---

## 12. Retrieval / FormatMemoryContext

### 12.1 本 PR 不强制改 retrieval 主链路

保留：

```go
contextResult, err := b.host.Service.Retrieve(ctx, memorycore.RetrievalRequest{...})
return FormatMemoryContext(contextResult, excludedEpisodeIDs...), nil
```

### 12.2 P1 建议：block-aware formatter

当前 `FormatMemoryContext` flatten 所有 block/item summary。后续建议改为 block-aware，但不要阻塞本 PR。

目标格式示例：

```text
[长期记忆上下文：使用约束]
- 使用时自然、克制，不要主动说明“我记得”。
- 历史事实不能当当前事实说。
- 低置信度记忆只可柔和使用。

[核心身份与边界]
...

[当前相关记忆]
...

[因果/历史上下文]
...

[不要主动提及]
...
```

等 extraction facade 集成稳定后另起 PR。

---

## 13. Logging 与 observability

### 13.1 统一 helper

新增：

```go
func (b *Bridge) logExtractionResult(
    message string,
    segment *storage.MemorySegment,
    result *memorycore.ExtractionRunResult,
    err error,
)
```

字段：

```text
chat_session_id
segment_id
memory_session_id
status
accepted
review
rejected
routed
not_applied
applied
failure
skipped_by_fingerprint
error_code
```

### 13.2 helper 函数

```go
func extractionErrorCode(result *memorycore.ExtractionRunResult, err error) string {
    if result != nil && strings.TrimSpace(result.SanitizedErrorCode) != "" {
        return result.SanitizedErrorCode
    }
    if err != nil {
        return "runner_failed"
    }
    return ""
}
```

不要记录 `err.Error()` 到普通 info log。warn log 可以记录 sanitized error code，必要时 debug mode 才记录 message。

---

## 14. Tests

### 14.1 Fake service

新增 test fake：

```go
type fakeMemoryService struct {
    memorycore.Service
    runExtractionCalls []memorycore.RunExtractionRequest
    runExtractionResult *memorycore.ExtractionRunResult
    runExtractionErr error
    consolidateCalls []memorycore.ConsolidateCandidateRequest
}

func (f *fakeMemoryService) RunExtraction(ctx context.Context, req memorycore.RunExtractionRequest) (*memorycore.ExtractionRunResult, error) {
    f.runExtractionCalls = append(f.runExtractionCalls, req)
    return f.runExtractionResult, f.runExtractionErr
}

func (f *fakeMemoryService) ConsolidateCandidate(ctx context.Context, req memorycore.ConsolidateCandidateRequest) (*memorycore.ConsolidationResult, error) {
    f.consolidateCalls = append(f.consolidateCalls, req)
    return nil, fmt.Errorf("ConsolidateCandidate should not be called by manual pin path")
}
```

### 14.2 Host tests

```text
TestHostExtractSessionEndUsesServiceRunExtraction
  - h.extractionPolicy.Enabled=true
  - call h.ExtractSessionEnd
  - assert fake.RunExtraction called once with trigger=session_end, mode=apply, Build.SessionID set。

TestHostExtractSessionEndDisabledNoop
  - Enabled=false
  - assert no RunExtraction call。

TestExtractionErrorDoesNotPanic
  - fake returns result+err
  - h.ExtractSessionEnd returns sanitized error；Bridge.extractFinalizedSegment logs and does not panic。
```

### 14.3 Bridge manual pin tests

```text
TestManualPinUsesRunExtraction
  - append user episode with manual pin content
  - manualRules.Match returns Pin
  - assert RunExtraction called with trigger=manual_pin, episode_ids=[last_user_episode], mode=apply。
  - assert ConsolidateCandidate was not called。

TestManualPinExtractionFailureOnlyWarns
  - RunExtraction returns error
  - appendEpisode still returns episode id；warning logged。
```

### 14.4 Bridge manual forget tests

```text
TestManualForgetUsesRunExtraction
  - manualRules.Match returns Forget
  - assert RunExtraction called with trigger=manual_forget, episode_ids=[last_user_episode], mode=dry-run。
  - assert no ConsolidateCandidate call。

TestManualForgetRouteOnlyAccepted
  - fake result RoutedCount=1
  - no error。

TestManualForgetNoRouteWarns
  - fake result RoutedCount=0
  - warning but appendEpisode still succeeds。
```

### 14.5 FinalizeSegment tests

```text
TestFinalizeSegmentTriggersExtractionOnce
  - Service.EndSession succeeds
  - storage finalize succeeds
  - extraction policy enabled
  - assert RunExtraction once after EndSession。

TestFinalizeSegmentExtractionFailureDoesNotFailFinalize
  - Service.EndSession succeeds
  - RunExtraction returns error
  - FinalizeSegment returns nil。
```

---

## 15. Migration order

### PR 1: MemoryCore dependency bump

```text
- Update go.mod to MemoryCore version/commit that includes Service.RunExtraction.
- Fix compile errors from Service interface expansion in test fakes.
```

### PR 2: Host extraction policy

```text
- Replace extractionRunner field with ExtractionHostPolicy.
- Update OpenFromConfig/OpenWithOptions/open.
- Implement ExtractionEnabled/extractionTriggerOnFinalizeSegment based on policy.
```

### PR 3: ExtractSessionEnd facade

```text
- Update Host.ExtractSessionEnd to call Service.RunExtraction.
- Delete or thin-adapt internal/memoryhost/extraction.go.
- Remove database/sql and extractionruntime imports from EmoAgent.
```

### PR 4: Manual pin / forget facade

```text
- applyManualMemoryIntent dispatches pin/forget to RunExtraction.
- Remove direct EnsureEntity + ConsolidateCandidate manual pin default path.
- manual forget no longer only logs resolver_not_implemented。
```

### PR 5: Tests and logs

```text
- Add fake service tests.
- Add sanitized logging helper.
- Ensure go test ./... passes。
```

### PR 6: Optional P1 formatter

```text
- block-aware FormatMemoryContext。
- Keep separate from extraction facade integration if possible。
```

---

## 16. Acceptance criteria

P0 完成后，EmoAgent 必须满足：

```text
1. internal/memoryhost 不再直接 import pkg/memorycore/extractionruntime。
2. internal/memoryhost 不再为 extraction 单独 sql.Open(host.DBPath)。
3. Host.ExtractSessionEnd 调用 Service.RunExtraction。
4. FinalizeSegment 后 session_end extraction 仍会触发。
5. manual pin 默认走 RunExtraction(trigger=manual_pin)，不再 direct ConsolidateCandidate。
6. manual forget 默认走 RunExtraction(trigger=manual_forget)，不再只 log resolver_not_implemented。
7. extraction disabled 时不调用 RunExtraction。
8. extraction error 不阻塞 appendEpisode / FinalizeSegment 主流程。
9. ordinary logs 不包含 raw user text、target_description、raw provider response。
10. go test ./... 通过。
```

---

## 17. Codex implementation prompt

下面可以直接作为 Codex 任务提示：

```text
你要在 EmoAgent 中接入 MemoryCore 新增的 Service.RunExtraction facade，并删除 EmoAgent 自己组装 extraction runtime 的默认路径。

背景：
- internal/memoryhost/Host 当前同时持有 memorycore.Service 和 extractionRunner。
- internal/memoryhost/extraction.go 当前自己 sql.Open(host.DBPath)、创建 extractionruntime LLM/audit/Runner。
- Bridge.FinalizeSegment 当前通过 Host.ExtractSessionEnd 间接调用 extractionRunner。
- manual pin 当前 direct EnsureEntity + ConsolidateCandidate。
- manual forget 当前只 log resolver_not_implemented。

目标：
- EmoAgent host 只依赖 memorycore.Service public API。
- session_end / manual_pin / manual_forget 都通过 Service.RunExtraction。
- 不再在 EmoAgent 中持有 *sql.DB 或 extractionruntime.Runner。

必须保持：
1. Service.Retrieve 主链路不改变。
2. FinalizeSegment 中 EndSession / DB finalize 仍是主流程；extraction 失败只记录 warning，不阻塞。
3. manual pin 不再绕过 MemoryCore extraction gate。
4. manual forget 不再只 log；至少 route deletion_intents。
5. 普通日志不得记录 raw private text、target_description、provider raw response。
6. extraction disabled 时所有 extraction trigger 都是 noop。

实施步骤：
1. 修改 Host struct：移除 extractionRunner 或标记 deprecated；新增 ExtractionHostPolicy，只保留 trigger/mode/limit/timezone。
2. OpenFromConfig/OpenWithOptions/open 根据 MemoryCore Options.Extraction.Enabled 初始化 host.extractionPolicy。
3. Host.ExtractSessionEnd 改成调用 h.Service.RunExtraction，Build selector 使用 SessionID。
4. 删除或薄化 internal/memoryhost/extraction.go；不要再 import database/sql、pkg/memorycore/extractionruntime、modernc sqlite。
5. Bridge.extractFinalizedSegment 保持触发，但使用新的 Host.ExtractSessionEnd，并记录 sanitized summary。
6. Bridge.applyManualMemoryIntent 改成：manual_pin -> Service.RunExtraction(trigger=manual_pin, episode_ids=[last_user_episode], mode=apply)；manual_forget -> Service.RunExtraction(trigger=manual_forget, episode_ids=[last_user_episode], mode=dry-run)。
7. 删除 direct manual pin ConsolidateCandidate 默认路径。
8. manual forget result.RoutedCount 或 RoutedDeletionIntents 非空时视为 route success；否则 warning。
9. 增加 fake memory service tests，覆盖 finalize/manual pin/manual forget/disabled/error no-blocking。
10. 确保 go test ./... 通过。
```
