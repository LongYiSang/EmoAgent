# EmoAgent 接入 Spec：接入 MemoryCore 语义抽取去重与语义遗忘预览

> Status: Implementation Spec v0.1  
> Date: 2026-05-29  
> Target repo: `EmoAgent`  
> Depends on: `MemoryCore 改造 Spec` 中的 public facade：`RunExtraction` semantic dedup options、`PreviewForget`、`ExecuteForget`、`Forget` exact-node executor。

## 0. 取舍结论

EmoAgent 不直接接 Python Sidecar、Trivium、Embedding Provider，也不实现自己的向量检索 / 删除候选逻辑。EmoAgent 只做三件事：

1. 继续通过 `memoryhost -> memorycore.Service` 使用 MemoryCore。
2. 在会话结束 / idle extraction 时，把 semantic dedup 配置传给 `RunExtraction`。
3. 在用户表达“忘记 / 删除 / 别再提”时，调用 `PreviewForget`，让用户确认候选，再调用 `ExecuteForget`。

核心边界：

```text
EmoAgent owns conversation flow. MemoryCore owns memory authority.
```

## 1. 为什么 EmoAgent 不该重做一套

EmoAgent 现有接缝已经足够：`memoryhost` 会在会话切段时 `StartSession`，每条消息写入时 `AppendEpisode`，检索时 `Retrieve`，会话结束时 `RunExtraction`。此外，manual memory intent 入口已经能识别 pin / forget 类意图。

因此，EmoAgent 侧不应新增 Sidecar client，也不应访问 MemoryCore internal package。所有新增能力都通过 MemoryCore public facade 暴露。

## 2. 明确非目标

本阶段 EmoAgent 不做：

- 不直接调用 `/memory/dedup-search` 或 `/memory/delete-candidates`。
- 不直接读写 Trivium `.tdb`。
- 不自行计算 embedding。
- 不自行决定 fact merge / reinforce / delete。
- 不在对话层实现 hard forget / purge 级联。
- 不把 Work 执行日志自动写长期记忆。
- 不接 Agent Affect 模块。
- 不实现复杂记忆管理 UI；只做最小确认流和日志。

## 3. 接入点

### 3.1 启动配置

新增或扩展 memory config：

```yaml
memory:
  semantic_dedup:
    enabled: true
    shadow: true
    enforce: false
    threshold_profile: default_v0
    candidate_limit: 12
  semantic_forget:
    preview_enabled: true
    execute_enabled: true
    require_confirmation: true
    default_fact_level: soft_forget
    default_episode_level: source_redact
  diagnostics:
    log_semantic_memory_events: true
```

初始建议：

```text
semantic_dedup.shadow = true
semantic_dedup.enforce = false
semantic_forget.preview_enabled = true
semantic_forget.execute_enabled = false 或仅开发环境开启
```

等 MemoryCore 回归集稳定后再打开 execute。

### 3.2 会话写入路径

保持现状：

```text
user / assistant message
→ memoryhost.AppendEpisode
→ MemoryCore authoritative episode store
```

EmoAgent 不需要在每条消息后做 embedding，也不需要等待 Sidecar。

### 3.3 检索路径

保持现状：

```text
Emotion before response
→ memoryhost.Retrieve
→ memorycore.Retrieve
→ prompt memory block
```

可增加 diagnostics 日志，但不得改变 prompt 注入安全边界。

建议记录：

```text
request_id
persona_id
session_id
sidecar_status
semantic_source
candidate_count
filtered_count
```

日志中不保存原始敏感内容。

### 3.4 抽取路径

在 session end / idle extraction 调用 `RunExtraction` 时传入 `SemanticDedupOptions`。

伪代码：

```go
opts := memorycore.RunExtractionOptions{
    Trigger: trigger,
    Mode: mode,
    SemanticDedup: memorycore.SemanticDedupOptions{
        Enabled: cfg.Memory.SemanticDedup.Enabled,
        Shadow: cfg.Memory.SemanticDedup.Shadow,
        Enforce: cfg.Memory.SemanticDedup.Enforce,
        CandidateLimit: cfg.Memory.SemanticDedup.CandidateLimit,
        ThresholdProfile: cfg.Memory.SemanticDedup.ThresholdProfile,
    },
}
res, err := h.core.RunExtraction(ctx, opts)
```

EmoAgent 只记录 `DedupDiagnostics`，不解释或覆盖 MemoryCore 决策。

### 3.5 用户主动遗忘路径

现有 `applyManualMemoryIntent` / manual rules 已能识别“忘记”“别再提”“删除”等前缀。本阶段将 forget intent 接到 MemoryCore 的 preview / execute。

目标流程：

```text
用户：忘掉我之前说过不吃香菜
→ applyManualMemoryIntent detects forget
→ memoryhost.PreviewForget
→ MemoryCore semantic delete candidates + SQLite recheck
→ EmoAgent 展示安全候选摘要并要求确认
→ 用户确认某几条
→ memoryhost.ExecuteForget
→ MemoryCore exact-node Forget executor
→ EmoAgent 回复已按确认处理
```

## 4. Manual forget UX

### 4.1 候选确认文案

候选预览返回后，Emotion 应以克制方式确认，不复述敏感原文。

示例：

```text
我找到了几条可能相关的记忆。为了避免误删，我先让你确认一下：
1. 你不吃香菜这一偏好。
2. 一条最近对话里的相关原文记录。
你想删除哪一条？也可以说“都不要删”。
```

如果 `risk_flags` 包含 high sensitivity / broad topic / batch / pinned / purge：

```text
这个删除范围可能会影响多条记忆。我不会直接执行。请你明确选择要删除的项目。
```

### 4.2 单候选低风险策略

即使只有一个低风险候选，默认仍建议要求确认，直到回归集稳定。

允许后续改为：

```text
single candidate + low risk + exact paraphrase → 可用轻确认
```

但不能无确认执行 broad topic 或 purge。

### 4.3 not found 策略

如果没有候选：

```text
我没有找到明确对应的长期记忆，因此没有删除任何内容。你可以换个说法，或者告诉我要删的是哪段对话/哪类信息。
```

不要把用户提供的敏感删除目标写入新的长期记忆。

### 4.4 用户取消

如果用户说“不删了 / 算了”：

```text
不调用 ExecuteForget。
只记录 conversation event，不写 deletion_event。
```

## 5. MemoryHost facade

在 `internal/memoryhost` 增加薄封装，不暴露 MemoryCore internal：

```go
type ForgetPreview struct {
    RequestID string
    PreviewHash string
    RequiresConfirmation bool
    Candidates []ForgetCandidateCard
    RiskFlags []string
}

type ForgetCandidateCard struct {
    NodeType string
    NodeID string
    SafeSummary string
    RecommendedLevel string
    RiskFlags []string
}

func (h *Host) PreviewForget(ctx context.Context, req ManualForgetRequest) (ForgetPreview, error)
func (h *Host) ExecuteForget(ctx context.Context, preview ForgetPreview, selected []ExactNodeRef) error
```

`memoryhost` 只做 DTO 转换和日志，不做候选搜索。

## 6. Conversation state

为了支持多轮确认，EmoAgent 需要在短期会话状态中保存 pending forget preview：

```go
type PendingForgetPreview struct {
    RequestID string
    PersonaID string
    SessionID string
    PreviewHash string
    Candidates []ForgetCandidateCard
    CreatedAt time.Time
    ExpiresAt time.Time
}
```

要求：

- pending preview 不进入长期记忆。
- 过期后必须重新 preview。
- 用户确认时必须匹配当前 pending preview hash。
- pending preview 中只保存 safe summary，不保存原始敏感文本。

## 7. 与 Work 的边界

Work Agent 仍不能直接写或删长期记忆。

允许：

```text
WorkReport 中带 privacy_signal / memory_candidate
→ Emotion 审批
→ MemoryHost 调用 MemoryCore
```

禁止：

```text
Work Runtime 直接调用 PreviewForget / ExecuteForget / RunExtraction
Work 工具日志自动进入长期记忆
```

## 8. 错误与降级

### 8.1 MemoryCore sidecar degraded

如果 `PreviewForget` 返回 degraded 但仍有 SQLite fallback candidates：正常展示，并提示确认。

如果完全失败：

```text
我现在不能可靠地定位要删除的记忆，所以没有执行删除。你可以稍后再试，或更具体地描述要删除的内容。
```

不要声称已删除。

### 8.2 preview changed

`ExecuteForget` 返回 `preview_changed`：

```text
这组候选刚刚发生了变化。为了避免误删，我需要重新确认一次。
```

然后重新调用 `PreviewForget`。

### 8.3 execute disabled

如果 config 中 `execute_enabled=false`：

```text
只展示预览，不执行删除。用于 shadow / staging。
```

用户文案在开发环境可说明“当前只预览不执行”；生产环境不应开启这种半功能状态。

## 9. 日志与观测

EmoAgent 侧只记录对话流程日志：

```text
manual_forget_detected
forget_preview_requested
forget_preview_returned
forget_preview_confirmed
forget_execute_requested
forget_execute_completed
forget_execute_failed
```

字段：

```text
request_id
persona_id
session_id
candidate_count
risk_flags
sidecar_status
error_code
```

禁止记录：

```text
raw sensitive target
provider rationale
deleted original text
```

## 10. 执行顺序

1. 升级 MemoryCore 依赖版本，确保新 facade 可用。
2. 在 EmoAgent config 中接入 semantic_dedup / semantic_forget 开关。
3. `RunExtraction` 传入 `SemanticDedupOptions`，先 shadow。
4. 为 manual forget 添加 `PreviewForget` 调用和 pending preview 状态。
5. 增加确认回复与用户选择解析。
6. 接入 `ExecuteForget`，先开发环境 / staging，再生产。
7. 补 e2e 测试：记住 → 检索 → 忘记预览 → 确认删除 → 再检索不可见。

## 11. 验收标准

- EmoAgent 仍只依赖 MemoryCore public package，不 import MemoryCore internal。
- 不新增 Python Sidecar / Trivium / Embedding Provider direct client。
- 正常聊天写入和检索行为不受影响。
- Session end extraction 能传入 semantic dedup options，并记录 diagnostics。
- 用户说“忘记 / 删除 / 别再提”时，会进入 preview 而不是直接删除。
- Preview 候选以 safe summary 展示，不复述敏感原文。
- 用户确认前不会调用 `ExecuteForget`。
- `ExecuteForget` 使用 preview hash 和 exact node IDs。
- preview 过期或 hash changed 时重新确认。
- 删除后再次检索同一内容，不进入 prompt memory block。
- Sidecar degraded 时，EmoAgent 不声称已经删除。
- Work Agent 不能直接写入或删除长期记忆。

## 12. 最小 e2e 测试

### 12.1 抽取去重 shadow

```text
输入：用户两次用不同说法表达同一稳定偏好。
预期：第二次 RunExtraction 返回 DedupDiagnostics；shadow 模式不改变原写入决策。
```

### 12.2 语义遗忘预览

```text
输入：已有 fact “用户不吃香菜”；用户说“忘掉我之前说过的那个忌口”。
预期：PreviewForget 返回该 fact 的 safe summary，EmoAgent 请求确认，不执行删除。
```

### 12.3 确认执行

```text
输入：用户确认删除第 1 条。
预期：ExecuteForget 成功；后续 Retrieve 不再注入该 fact。
```

### 12.4 not found

```text
输入：用户要求删除不存在的记忆。
预期：不调用 ExecuteForget；回复未找到明确长期记忆；不把删除目标写成新 fact。
```

### 12.5 preview changed

```text
输入：确认时 preview hash 已变。
预期：EmoAgent 重新预览并请求确认。
```
