# Agent Affect Prompt Text 修复：Codex 分阶段实施 Spec

> **Target repository**: `LongYiSang/EmoAgent`  
> **Target branch baseline**: 当前 `main` 现状  
> **Document purpose**: 给 Codex 提供足够上下文、架构约束、分阶段实施步骤、测试要求和验收标准，用于修复 Agent Affect 中机器标签泄露到自然语言 mood prompt 的问题。  
> **Primary invariant**: `label` 是内部机器标签，不是自然语言心情描述；主对话 prompt 只能使用自然语言、安全、短文本的 mood/cause summary。  
> **Compatibility goal**: 保持 `agent_affect.v3.appraisal.v1` compact schema 和 checkpoint/trace 省 token 改造，不恢复旧版 LLM 输出字段，不新增 LLM 调用。

---

## 0. 背景与问题摘要

近期 DB 中出现了类似以下 `prompt_mood_text`：

```text
enthusiastic_caring_sleep_goodnight；好哦快睡～盖好被子明天找我哦
playful_caring_weather_sleep_encourage；关心天气并鼓励早睡
playful_caring_weather_sleep_reminder；俏皮地关心用户，提醒休息。
```

排查结论是：这不是前端展示问题，也不是 DB 编码问题，而是 Go 侧将 v3 LLM 响应中的 `label` 当成自然语言 mood description 拼入了 `prompt_mood_text`。

当前 v3 LLM 响应只要求：

```json
{
  "schema_version": "agent_affect.v3.appraisal.v1",
  "appraisal": {...},
  "delta": {...},
  "label": "playful_caring_weather_sleep_reminder",
  "cause": {
    "code": "...",
    "summary": "...",
    "visible_summary": "...",
    "tags": []
  },
  "confidence": 0.8
}
```

这正是省 token 改造的目标：由 LLM 输出紧凑 appraisal/delta/cause，不再输出旧版的 `mood_description` / `prompt_mood_text`。问题在于 parser 没有把“机器字段”和“自然语言字段”分层。

---

## 1. 当前仓库事实依据

### 1.1 当前 parser 的问题点

`internal/agentaffect/evaluator.go` 中，`ParseLLMResponse` 目前：

1. 解析 v3 字段：`schema_version / appraisal / delta / label / cause / confidence`。
2. 如果 `label` 为空，用 `deriveLabel(delta)` 生成内部标签。
3. 取 `cause.visible_summary || cause.summary` 作为 `moodReason`。
4. 将 `MoodDescription` 直接设置成 `label`。
5. 将 `PromptMoodText` 设置成 `buildPromptMoodTextFallback(label, moodReason)`。

等价行为：

```go
Label           = label
MoodDescription = label
MoodReason      = cause.VisibleSummary || cause.Summary
PromptMoodText  = label + "；" + MoodReason
```

这会把 `playful_caring_weather_sleep_reminder` 这类 compact machine tag 直接注入主对话 prompt。

### 1.2 prompt block 优先使用 `PromptMoodText`

`internal/agentaffect/prompt_block.go` 中，`promptMoodText(mood)` 当前优先级：

```go
1. mood.PromptMoodText
2. buildPromptMoodTextFallback(mood.MoodDescription, mood.MoodReason)
3. buildPromptMoodTextFallback(mood.Label, visible/cause summary)
```

因此旧 DB 中已经写坏的 `prompt_mood_text` 会继续污染主对话，即使未来 parser 修好了，历史 latest state 仍可能被读出来。

### 1.3 测试当前固化了错误行为

`internal/agentaffect/evaluator_llm_test.go` 中当前测试期望：

```go
result.MoodDescription == "steady"
result.MoodReason == "Safe visible cause summary."
result.PromptMoodText == "steady；Safe visible cause summary."
```

这需要改成新的不变量：

```text
Label 可以是 steady / playful_caring_xxx 等内部标签；
PromptMoodText 不得包含 label 前缀；
PromptMoodText 应优先来自 cause.visible_summary，缺失时回退 cause.summary。
```

### 1.4 当前 v3 prompt 是紧凑 schema

`internal/agentaffect/prompt_v3.go` 已经明确：

```text
Return strict JSON only with schema_version "agent_affect.v3.appraisal.v1".
The response object must contain: schema_version, appraisal, delta, label, cause, confidence.
cause contains code, summary, visible_summary, tags; keep summaries short and safe.
```

这个设计应该保留。不要为了修 bug 把 LLM 输出重新扩回旧版字段。

### 1.5 状态字段已经存在，不需要 schema 改造

`MoodSnapshot` 已经有：

```go
Label
MoodDescription
MoodReason
PromptMoodText
CauseSummary
VisibleCauseSummary
```

SQLite migration 21 也已经给 states/evaluations/events 加了：

```sql
mood_description
mood_reason
prompt_mood_text
```

所以本次修复不需要新增列。

---

## 2. 设计目标

### 2.1 必须达成

1. `label` 保持内部状态标签，允许是 compact machine tag。
2. `PromptMoodText` 不再拼接 `label`。
3. 新评估写入的 `MoodDescription / MoodReason / PromptMoodText` 必须是自然语言安全文本，优先取 `cause.visible_summary`。
4. 旧 DB 中已经存在的 `label；summary` 格式，在主对话 prompt block 生成时必须被读时净化。
5. 保持 `agent_affect.v3.appraisal.v1` schema 不变。
6. 不新增 LLM 输出字段，不增加 LLM 调用次数。
7. 保持 checkpoint/trace/batch/context budget 省 token 改造。
8. 保留 `numeric_debug` 模式中显示 `label` 的能力，因为那是 debug，而不是 natural mood prompt。

### 2.2 不在本次做

1. 不做 v4 schema。
2. 不要求 LLM 返回 `mood_description` 或 `prompt_mood_text`。
3. 不重写 Agent Affect 状态机。
4. 不改 `agent_affect_states` / `agent_affect_evaluations` / `agent_affect_events` 表结构。
5. 不改主 chat pipeline 的调用顺序。
6. 不做复杂自然语言 mood 生成器。
7. 不把旧 `response_json` 改写掉；原始 LLM 输出应作为审计保留。

---

## 3. 核心语义边界

### 3.1 字段归属

| 字段 | 新语义 | 可进入 natural prompt | 说明 |
|---|---|---:|---|
| `Label` | 内部 compact machine tag | 否 | debug、state checkpoint、numeric_debug 可用。 |
| `Cause.Code` | 内部 cause code | 否 | cause trace / debug / tags。 |
| `Cause.Tags` | 内部 tag | 否 | 检索或审计标签。 |
| `Cause.Summary` | 内部或审计摘要 | 可作为 fallback | 应短、安全；可比 visible 更内部。 |
| `Cause.VisibleSummary` | 面向 prompt/UI 的自然语言摘要 | 是 | `PromptMoodText` 首选来源。 |
| `MoodDescription` | 自然语言展示摘要 | 是 | v3 下由 visible summary 派生；不能等于机器 label。 |
| `MoodReason` | 可选自然语言原因/内部摘要 | 是，谨慎 | 不应重复 visible summary。 |
| `PromptMoodText` | 主对话 mood block 使用文本 | 是 | 不得包含 label 前缀。 |

### 3.2 新不变量

```text
Agent affect label is an internal state tag, not natural-language prompt text.
```

中文：

```text
Agent Affect 的 label 是内部状态标签，不是自然语言心情描述。
```

### 3.3 v3 compact schema 的解释

本次不改变 schema，只调整 Go 侧投影：

```text
LLM compact output:
  label + cause.visible_summary + cause.summary

Go projection:
  label -> internal Label
  cause.visible_summary || cause.summary -> natural PromptMoodText
```

---

## 4. 总体实施方案

分为 5 个阶段。Codex 应按顺序实施，前一阶段测试通过后再进入下一阶段。

```text
Phase 1: Parser 投影修复，新写入不再产生坏格式。
Phase 2: Prompt block 读时净化，旧 DB 坏数据不再污染主对话。
Phase 3: Prompt v3 文案小幅澄清，降低 LLM 输出 reply-like visible_summary 的概率。
Phase 4: 测试矩阵更新，固化新语义和旧数据兼容。
Phase 5: 可选数据修复工具/SQL 文档，不作为主修复阻塞项。
```

---

## 5. Phase 1：Parser 投影修复

### 5.1 修改文件

```text
internal/agentaffect/evaluator.go
```

### 5.2 新增/修改 helper

建议添加以下 helper，放在 `evaluator.go` 中，与 `buildPromptMoodTextFallback` 附近即可。

```go
func naturalPromptMoodTextFromCause(cause AffectCauseProposal) string {
    if text := strings.TrimSpace(cause.VisibleSummary); text != "" {
        return text
    }
    if text := strings.TrimSpace(cause.Summary); text != "" {
        return text
    }
    return ""
}

func stripPromptMoodLabelPrefix(label, text string) string {
    label = strings.TrimSpace(label)
    text = strings.TrimSpace(text)
    if label == "" || text == "" {
        return text
    }
    for _, sep := range []string{"；", ";", "：", ":", " - "} {
        prefix := label + sep
        if strings.HasPrefix(text, prefix) {
            return strings.TrimSpace(strings.TrimPrefix(text, prefix))
        }
    }
    return text
}

func naturalMoodFieldsFromCause(label string, cause AffectCauseProposal) (description string, reason string, promptText string) {
    promptText = naturalPromptMoodTextFromCause(cause)
    promptText = stripPromptMoodLabelPrefix(label, promptText)

    description = promptText

    // Keep MoodReason only when it adds information and does not duplicate prompt text.
    summary := strings.TrimSpace(cause.Summary)
    visible := strings.TrimSpace(cause.VisibleSummary)
    if summary != "" && summary != visible && summary != promptText {
        reason = summary
    }
    return description, reason, promptText
}
```

命名可以调整，但语义必须保持。

### 5.3 修改 `ParseLLMResponse`

将当前：

```go
moodReason := cause.VisibleSummary
if moodReason == "" {
    moodReason = cause.Summary
}
return LLMEvaluationResult{
    ...
    Label:           label,
    MoodDescription: label,
    MoodReason:      moodReason,
    PromptMoodText:  buildPromptMoodTextFallback(label, moodReason),
    ...
}
```

改为：

```go
moodDescription, moodReason, promptMoodText := naturalMoodFieldsFromCause(label, cause)

return LLMEvaluationResult{
    Delta:               delta,
    Appraisal:           *parsed.Appraisal,
    HasAppraisal:        true,
    Cause:               cause,
    Label:               label,
    MoodDescription:     moodDescription,
    MoodReason:          moodReason,
    PromptMoodText:      promptMoodText,
    CauseSummary:        cause.Summary,
    VisibleCauseSummary: cause.VisibleSummary,
    AffectTags:          cause.Tags,
    Confidence:          confidence,
    RawResponseJSON:     object,
    Status:              EvaluationStatusPreview,
}
```

### 5.4 `NoChangeResult` 的建议调整

当前 `NoChangeResult` 只设置 `Label="steady"` 和 cause summaries。如果后续被写入 state，prompt block 可能用 cause/error/debug reason 生成 mood text。

建议改为：

```go
func NoChangeResult(reason string, status string) LLMEvaluationResult {
    if status == "" {
        status = EvaluationStatusPreview
    }
    promptText := "平稳、接近基线。"
    return LLMEvaluationResult{
        Delta:               MoodVector{},
        Label:               "steady",
        MoodDescription:     promptText,
        MoodReason:          "",
        PromptMoodText:      promptText,
        CauseSummary:        reason,
        VisibleCauseSummary: reason,
        Confidence:          0.5,
        Fallback:            true,
        Status:              status,
    }
}
```

如果担心改变 fallback 行为，可以至少保证 `PromptMoodText` 不拼 `steady；reason`。

### 5.5 Phase 1 验收

给定 LLM 输出：

```json
{
  "label": "playful_caring_weather_sleep_reminder",
  "cause": {
    "summary": "Internal audit summary.",
    "visible_summary": "俏皮地关心用户，提醒休息。"
  }
}
```

期望：

```go
result.Label == "playful_caring_weather_sleep_reminder"
result.PromptMoodText == "俏皮地关心用户，提醒休息。"
!strings.Contains(result.PromptMoodText, result.Label)
result.MoodDescription == "俏皮地关心用户，提醒休息。"
```

---

## 6. Phase 2：Prompt block 读时净化

### 6.1 修改文件

```text
internal/agentaffect/prompt_block.go
```

### 6.2 修改 `promptMoodText`

当前最后兜底会把 `Label` 拼入 prompt：

```go
return buildPromptMoodTextFallback(mood.Label, reason)
```

这应被移除。

建议实现：

```go
func promptMoodText(mood MoodSnapshot) string {
    label := strings.TrimSpace(mood.Label)

    if text := stripPromptMoodLabelPrefix(label, mood.PromptMoodText); strings.TrimSpace(text) != "" {
        return strings.TrimSpace(text)
    }

    if text := buildPromptMoodTextFallback(mood.MoodDescription, mood.MoodReason); strings.TrimSpace(text) != "" {
        return strings.TrimSpace(stripPromptMoodLabelPrefix(label, text))
    }

    reason := strings.TrimSpace(mood.VisibleCauseSummary)
    if reason == "" {
        reason = strings.TrimSpace(mood.CauseSummary)
    }
    return strings.TrimSpace(stripPromptMoodLabelPrefix(label, reason))
}
```

注意：

1. 旧 DB 里 `PromptMoodText = label + "；" + reason` 时，应返回 `reason`。
2. 如果没有任何自然语言文本，返回空字符串，由 `formatNaturalPromptAffectBlock` 继续使用默认：`平稳、接近基线。`
3. `numeric_debug` 不受影响，仍可打印 label。

### 6.3 旧数据兼容示例

输入：

```go
MoodSnapshot{
    Label: "playful_caring_weather_sleep_reminder",
    PromptMoodText: "playful_caring_weather_sleep_reminder；俏皮地关心用户，提醒休息。",
}
```

输出：

```text
俏皮地关心用户，提醒休息。
```

### 6.4 Phase 2 验收

`FormatPromptAffectBlock` natural mode 生成：

```text
[Agent Mood]
当前模拟心情：俏皮地关心用户，提醒休息。

这是内部表达背景：...
```

不得包含：

```text
playful_caring_weather_sleep_reminder；
```

---

## 7. Phase 3：v3 prompt 文案澄清

### 7.1 修改文件

```text
internal/agentaffect/prompt_v3.go
```

### 7.2 修改目的

当前 system prompt 只说：

```text
cause contains code, summary, visible_summary, tags; keep summaries short and safe.
```

建议改为更明确但仍紧凑的版本：

```text
label is a compact internal machine tag, not prompt prose.
cause.visible_summary is the short natural-language internal mood/cause summary for the Agent Mood block; do not write it as a user-facing reply.
cause contains code, summary, visible_summary, tags; keep summaries short and safe.
```

也可以合并成一行减少 token：

```text
label is a compact internal tag, not prose; cause.visible_summary is short natural-language internal mood/cause text for the Agent Mood block, not a user-facing reply.
```

### 7.3 不改变 schema

不要新增：

```text
mood_description
prompt_mood_text
response_advice
```

现有 forbidden fields 仍应保持拒绝。

### 7.4 Phase 3 验收

现有测试仍应能看到 prompt 中包含：

```text
"schema_version":"agent_affect.v3.appraisal.v1"
"state_checkpoint"
"event_batch"
"dimension_limits"
```

并且仍不包含：

```text
previous_evaluations
recent_evaluations
agent_affect.v2.evaluation
```

---

## 8. Phase 4：测试更新与新增

### 8.1 修改文件

```text
internal/agentaffect/evaluator_llm_test.go
```

如项目已有或可新增：

```text
internal/agentaffect/prompt_block_test.go
```

也可把 prompt block 测试放在现有 evaluator 测试文件中，同 package 即可。

### 8.2 替换错误固化测试

将当前 `TestParseLLMResponseRequiresV3SchemaAndDerivesNaturalMoodFields` 的期望从：

```go
MoodDescription == "steady"
PromptMoodText == "steady；Safe visible cause summary."
```

改成：

```go
Label == "steady"
MoodDescription == "Safe visible cause summary."
PromptMoodText == "Safe visible cause summary."
```

并增加断言：

```go
if strings.Contains(result.PromptMoodText, result.Label+"；") {
    t.Fatalf("prompt_mood_text leaks machine label: %q", result.PromptMoodText)
}
```

### 8.3 新增测试：underscore label 不泄露

```go
func TestParseLLMResponseKeepsMachineLabelOutOfPromptMoodText(t *testing.T) {
    result, err := ParseLLMResponse(`{
        "schema_version": "agent_affect.v3.appraisal.v1",
        "appraisal": {
            "event_significance": 0.3,
            "novelty": 0.1,
            "goal_relevance": 0.1,
            "relationship_impact": 0.2,
            "boundary_impact": 0,
            "uncertainty": 0.1
        },
        "delta": {"valence": 0.02, "warmth": 0.03},
        "label": "playful_caring_weather_sleep_reminder",
        "cause": {
            "code": "sleep_reminder",
            "summary": "Internal audit summary.",
            "visible_summary": "俏皮地关心用户，提醒休息。",
            "tags": ["sleep"]
        },
        "confidence": 0.6
    }`)
    if err != nil {
        t.Fatalf("ParseLLMResponse: %v", err)
    }
    if result.Label != "playful_caring_weather_sleep_reminder" {
        t.Fatalf("label = %q", result.Label)
    }
    if result.PromptMoodText != "俏皮地关心用户，提醒休息。" {
        t.Fatalf("prompt_mood_text = %q", result.PromptMoodText)
    }
    if strings.Contains(result.PromptMoodText, result.Label) {
        t.Fatalf("prompt_mood_text leaks label: %q", result.PromptMoodText)
    }
}
```

### 8.4 新增测试：visible_summary 缺失时回退 summary

```go
func TestParseLLMResponseFallsBackToCauseSummaryForPromptMoodText(t *testing.T) {
    // cause.visible_summary = ""; cause.summary = "安全的自然语言摘要。"
    // expect PromptMoodText == "安全的自然语言摘要。"
}
```

### 8.5 新增测试：旧 DB prompt text 读时清理

```go
func TestPromptMoodTextStripsLegacyLabelPrefix(t *testing.T) {
    got := promptMoodText(MoodSnapshot{
        Label:          "playful_caring_weather_sleep_reminder",
        PromptMoodText: "playful_caring_weather_sleep_reminder；俏皮地关心用户，提醒休息。",
    })
    if got != "俏皮地关心用户，提醒休息。" {
        t.Fatalf("prompt mood text = %q", got)
    }
}
```

### 8.6 新增测试：final fallback 不再用 label 拼 reason

```go
func TestPromptMoodTextFallsBackToVisibleCauseWithoutLabel(t *testing.T) {
    got := promptMoodText(MoodSnapshot{
        Label:               "steady",
        VisibleCauseSummary: "No meaningful affective change.",
    })
    if got != "No meaningful affective change." {
        t.Fatalf("prompt mood text = %q", got)
    }
}
```

### 8.7 新增测试：numeric_debug 保留 label

```go
func TestFormatPromptAffectBlockNumericDebugKeepsLabel(t *testing.T) {
    cfg := config.DefaultConfig().AgentAffect
    cfg.Prompt.Mode = "numeric_debug"
    cfg.Prompt.IncludeMoodBlock = true
    block := FormatPromptAffectBlock(cfg, MoodSnapshot{Label: "playful_caring_weather_sleep_reminder", Confidence: 0.8})
    if !strings.Contains(block, "label: playful_caring_weather_sleep_reminder") {
        t.Fatalf("numeric debug missing label: %s", block)
    }
}
```

### 8.8 建议运行命令

Codex 修改后运行：

```bash
go test ./internal/agentaffect/...
```

如时间允许，再运行：

```bash
go test ./internal/chat/... ./internal/app/... ./internal/storage/...
```

---

## 9. Phase 5：可选数据修复

本阶段不是 Phase 1–4 的阻塞项。Phase 2 的读时净化已经能保护主对话 prompt。

### 9.1 不要修改 `response_json`

`response_json` 是原始 LLM 响应审计，应保留原样。

### 9.2 运行前 dry-run

以 states 为例：

```sql
SELECT id, updated_at, label, mood_description, mood_reason,
       prompt_mood_text, visible_cause_summary, cause_summary
FROM agent_affect_states
WHERE label IS NOT NULL
  AND label <> ''
  AND prompt_mood_text LIKE label || '；%';
```

### 9.3 states 修复 SQL

```sql
UPDATE agent_affect_states
SET
  prompt_mood_text = TRIM(SUBSTR(prompt_mood_text, LENGTH(label) + 2)),
  mood_description = CASE
    WHEN mood_description = label THEN TRIM(SUBSTR(prompt_mood_text, LENGTH(label) + 2))
    ELSE mood_description
  END
WHERE label IS NOT NULL
  AND label <> ''
  AND prompt_mood_text LIKE label || '；%';
```

注意：SQLite `SUBSTR` 起始位置从 1 开始；`LENGTH(label)+2` 针对一个中文分号字符 `；` 后的内容。执行前务必 dry-run。

### 9.4 events 修复 SQL

`agent_affect_events` 有 `label_after` 和 `prompt_mood_text`，可类似修复：

```sql
UPDATE agent_affect_events
SET
  prompt_mood_text = TRIM(SUBSTR(prompt_mood_text, LENGTH(label_after) + 2)),
  mood_description = CASE
    WHEN mood_description = label_after THEN TRIM(SUBSTR(prompt_mood_text, LENGTH(label_after) + 2))
    ELSE mood_description
  END
WHERE label_after IS NOT NULL
  AND label_after <> ''
  AND prompt_mood_text LIKE label_after || '；%';
```

### 9.5 evaluations 修复建议

`agent_affect_evaluations` 当前没有单独 `label` 列，只有 `response_json` 内有 label。建议不在自动 migration 中硬修 evaluations，避免依赖 SQLite JSON1 或误伤历史审计展示。

如果需要修 UI 展示，可以优先在 API/UI 层使用同一 `stripPromptMoodLabelPrefix` 逻辑；或者新增一条手动脚本，解析 `response_json` 后逐行修复。

### 9.6 是否做 migration

默认不建议把数据修复直接塞进现有 migration，除非项目维护者确认自动修历史数据是期望行为。

推荐顺序：

```text
1. 先落 Phase 1–4。
2. 观察新写入是否正确。
3. 如管理页历史仍影响使用，再执行手动 SQL 或新增显式 admin repair command。
```

---

## 10. Codex 分阶段任务清单

### Task A：Parser helper 与投影修复

**Files**:

```text
internal/agentaffect/evaluator.go
```

**Steps**:

1. 添加 `naturalPromptMoodTextFromCause`。
2. 添加 `stripPromptMoodLabelPrefix`。
3. 添加 `naturalMoodFieldsFromCause`。
4. 修改 `ParseLLMResponse`，让 `MoodDescription/MoodReason/PromptMoodText` 来自 cause，而不是 label。
5. 可选修改 `NoChangeResult`，避免技术 reason 注入自然 mood prompt。

**Acceptance**:

```text
go test ./internal/agentaffect -run TestParseLLMResponse
```

### Task B：Prompt block 旧数据净化

**Files**:

```text
internal/agentaffect/prompt_block.go
```

**Steps**:

1. 在 `promptMoodText` 读取 `PromptMoodText` 时调用 `stripPromptMoodLabelPrefix`。
2. 在 fallback `MoodDescription/MoodReason` 组合后调用 `stripPromptMoodLabelPrefix`。
3. 最后 fallback 只返回 visible/cause summary，不再拼 `Label`。
4. 确认 `numeric_debug` 不受影响。

**Acceptance**:

```text
旧格式 label；summary 在 natural_summary 下被清理；numeric_debug 仍显示 label。
```

### Task C：Prompt v3 澄清

**Files**:

```text
internal/agentaffect/prompt_v3.go
```

**Steps**:

1. 在 `stableAffectSystemPrompt` 中补一句：`label` 是 compact internal tag，不是 prose。
2. 说明 `cause.visible_summary` 是 Agent Mood block 的短自然语言内部摘要，不是用户回复。
3. 不改变 response object 必填字段。

**Acceptance**:

```text
现有 prompt budget 测试仍通过；不新增 schema 字段。
```

### Task D：测试更新

**Files**:

```text
internal/agentaffect/evaluator_llm_test.go
internal/agentaffect/prompt_block_test.go  // optional new file
```

**Steps**:

1. 更新旧测试期望。
2. 新增 machine label 不泄露测试。
3. 新增 visible_summary 缺失 fallback 测试。
4. 新增旧 DB `label；summary` 清理测试。
5. 新增 numeric_debug 保留 label 测试。

**Acceptance**:

```bash
go test ./internal/agentaffect/...
```

### Task E：可选数据修复文档或工具

**Files**:

可选：

```text
docs/ops/agent_affect_prompt_mood_text_repair.md
cmd/agent-affect-repair/...
```

**Steps**:

1. 提供 dry-run 查询。
2. 提供 states/events 修复 SQL。
3. 明确不修改 `response_json`。
4. 对 evaluations 只做手动/脚本化建议，不自动 migration。

**Acceptance**:

```text
主修复不依赖 Task E；Task E 只用于管理页历史整洁或运维清理。
```

---

## 11. 验收标准总表

| 验收点 | 必须通过 |
|---|---:|
| 新 LLM v3 输出中的 `label` 不进入 `PromptMoodText` | 是 |
| `PromptMoodText` 优先使用 `cause.visible_summary` | 是 |
| `visible_summary` 缺失时回退 `cause.summary` | 是 |
| 旧 DB `label；summary` 被 natural prompt 读时清理 | 是 |
| `numeric_debug` 仍能显示 label | 是 |
| v3 schema 不新增字段 | 是 |
| 不恢复旧 schema | 是 |
| 不增加 LLM 调用 | 是 |
| 不改表结构 | 是 |
| forbidden fields 仍被拒绝 | 是 |
| prompt budget/compact checkpoint 设计不被破坏 | 是 |

---

## 12. 回归风险与规避

### 12.1 风险：UI 中“当前心情”字段变成 cause summary

v3 schema 没有独立 natural mood description。短期将 `MoodDescription` 派生为 `visible_summary` 是最小修复。它比机器 tag 更适合展示和 prompt，但可能不是纯 mood phrase。

长期如果需要更精细展示，可在 v4 schema 或 Go side humanizer 中引入单独自然语言 mood description。本次不做。

### 12.2 风险：visible_summary 仍像回复句子

通过 Phase 3 prompt 澄清降低概率：`visible_summary` 应是内部 mood/cause summary，不是 user-facing reply。

不要用 Go 侧强行重写，因为这会增加复杂度和误删风险。

### 12.3 风险：旧数据太多，管理页仍显示坏格式

Phase 2 已保证主对话不被污染。管理页历史整洁可用 Phase 5 修复。

### 12.4 风险：NoChangeResult 技术 reason 进入 prompt

建议 Phase 1 同时修 `NoChangeResult`，保持 fallback prompt 为“平稳、接近基线。”，技术 reason 只保留在 cause summary / debug。

---

## 13. 最小 diff 轮廓

### evaluator.go

```diff
- moodReason := cause.VisibleSummary
- if moodReason == "" {
-     moodReason = cause.Summary
- }
+ moodDescription, moodReason, promptMoodText := naturalMoodFieldsFromCause(label, cause)
  return LLMEvaluationResult{
      ...
      Label:               label,
-     MoodDescription:     label,
-     MoodReason:          moodReason,
-     PromptMoodText:      buildPromptMoodTextFallback(label, moodReason),
+     MoodDescription:     moodDescription,
+     MoodReason:          moodReason,
+     PromptMoodText:      promptMoodText,
      CauseSummary:        cause.Summary,
      VisibleCauseSummary: cause.VisibleSummary,
      ...
  }
```

### prompt_block.go

```diff
 func promptMoodText(mood MoodSnapshot) string {
-    if text := strings.TrimSpace(mood.PromptMoodText); text != "" {
-        return text
+    label := strings.TrimSpace(mood.Label)
+    if text := stripPromptMoodLabelPrefix(label, mood.PromptMoodText); strings.TrimSpace(text) != "" {
+        return strings.TrimSpace(text)
     }
-    if text := buildPromptMoodTextFallback(mood.MoodDescription, mood.MoodReason); text != "" {
-        return text
+    if text := buildPromptMoodTextFallback(mood.MoodDescription, mood.MoodReason); strings.TrimSpace(text) != "" {
+        return strings.TrimSpace(stripPromptMoodLabelPrefix(label, text))
     }
     reason := mood.VisibleCauseSummary
     if reason == "" {
         reason = mood.CauseSummary
     }
-    return buildPromptMoodTextFallback(mood.Label, reason)
+    return strings.TrimSpace(stripPromptMoodLabelPrefix(label, reason))
 }
```

---

## 14. 给 Codex 的最终指令

请基于当前仓库 `main` 实施上述 Phase 1–4。优先保证最小、清晰、可测试的修复：

1. 不改变 `agent_affect.v3.appraisal.v1` JSON schema。
2. 不新增 LLM 输出字段。
3. 不新增数据库列。
4. 不改变主对话 pipeline。
5. 只在 Go 侧重新定义 v3 输出到 natural mood fields 的投影逻辑。
6. 用读时净化保护历史坏数据。
7. 用测试明确固化：`label` 不得进入 natural prompt。

完成后运行：

```bash
go test ./internal/agentaffect/...
```

如涉及 prompt 文案或 service 行为，尽量再运行：

```bash
go test ./internal/chat/... ./internal/app/...
```

最终提交建议标题：

```text
fix(agent-affect): keep machine label out of natural mood prompt
```
