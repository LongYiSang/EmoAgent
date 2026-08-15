# EmoAgent Prompt Center Phase 2 实施 Spec

> **Document status**: Implementation Spec  
> **Version**: 0.1  
> **Date**: 2026-06-14  
> **Target path**: `docs/architecture/prompt_center_phase2_spec.md`  
> **Scope**: 在 Prompt Center MVP 之后，完善“真实注入可见性、快照可信度、完整预览、覆盖治理、长期运行成本控制”。  
> **Non-goal**: 本阶段不改 Work runtime 行为，不开放 Work prompt / tool description 动态覆盖，不改工具 JSON schema / 权限 / approval / destructive 逻辑，不做复杂边界设置或策略编译系统。

---

## 0. 一句话定义

Prompt Center Phase 2 的目标不是扩大可编辑范围，而是让 Prompt Center 变成可信的提示词观测与治理面板：

```text
用户能看到：
  组件默认值 / global 覆盖 / Agent 覆盖 / 当前生效值 / 完整最终注入 prompt / 真实请求快照

系统能保证：
  快照可追踪、组件来源完整、覆盖可 diff、stale 可识别、prompt 改坏前有 warning、长期快照不会无限膨胀。
```

Phase 2 的主线是：

```text
Prompt Snapshot v2
+ Full Prompt Preview
+ Snapshot Detail UI
+ Override Governance
+ Snapshot Retention / Truncation
+ Summary Prompt Snapshot
```

---

## 1. 背景：MVP 当前状态

MVP 已经完成以下核心能力：

1. Prompt Center 已经有 `global` / `agent` scope，以及 `custom` / `use_default` override mode。`agent use_default` 语义用于让某个 Agent 跳过 global override，直接使用 embedded default。
2. Resolver 当前优先级已经是：

```text
agent custom
→ agent use_default
→ global custom
→ embedded default
```

3. `prompt_overrides` 与 `prompt_render_snapshots` 已经写入 SQLite schema。
4. Emotion 的 `emotion.operating_contract`、`emotion.internal_context_data_policy` 已接入 resolver。
5. Running Summary 的 `context.running_summary.system`、`context.running_summary.repair` 已接入 resolver。
6. Work runtime / Work progress / tool description 已登记为默认组件，但仍为 `editable: false`，暂不动态接入。
7. 前端已有“提示词中心”页面，支持 Agent 选择、组件列表、默认值、生效值、global 覆盖、Agent 覆盖、继承全局、Agent 使用内置默认、组件预览、快照列表。

Phase 2 建立在这个 MVP 之上，不回滚已有设计。

---

## 2. 当前问题与设计判断

### 2.1 快照 components 不完整

当前 `emotion_chat` 快照保存了完整 `req.System`，但 `components_json` 主要只包含 Prompt Center 管理的两个静态组件：

```text
emotion.operating_contract
emotion.internal_context_data_policy
```

实际最终 system prompt 还可能包含：

```text
<persona>
<runtime_context>
<pending_work>
memory prompt block
agent affect prompt block
extraSystem
```

因此 Phase 2 要把这些动态块也记录成 RenderComponent。它们不一定可编辑，但必须可见。

### 2.2 快照 request_id 不可追踪

当前 snapshot 的 `RequestID` 可以是随机 UUID。Phase 2 应尽量使用真实 `turn.Inbound.RequestID`，以便串联：

```text
frontend request / websocket envelope
→ turn journal
→ LLM request
→ prompt_render_snapshots
```

legacy 入口没有 request_id 时才 fallback UUID。

### 2.3 Running Summary 可改但不可观测

MVP 已经让 Running Summary prompt 可通过 Prompt Center 覆盖，但真实快照只记录 `emotion_chat`。Phase 2 应记录：

```text
context.running_summary.update
context.running_summary.repair
```

用户才能知道某次 summary update 实际用了哪版 prompt。

### 2.4 Preview 不是完整最终 prompt 预览

当前 preview 逻辑更接近“组件 effective 文本预览”：按 component ids resolve 后用 `\n\n` join。Phase 2 应明确拆成：

```text
component_preview：单个或多个组件的生效文本
full_preview：按 purpose 走真实 assembler，预览完整 prompt
```

### 2.5 Snapshot 无截断与保留策略

真实 system prompt 可能包含 memory context，长期完整保存会带来：

```text
DB 膨胀
隐私文本长期残留
调试数据难以清理
```

Phase 2 要支持配置化截断、保留天数、最大行数，以及 hash-only / 不保存 rendered_text 的未来扩展。

### 2.6 Stale override 需要按 scope 拆分

当前 UI 只有一个 `stale_override` 概念，容易混淆：

```text
global override stale
agent override stale
effective override stale
```

Phase 2 应分别返回并分别展示。

---

## 3. 目标

### 3.1 产品目标

1. 用户能查看某次真实请求的完整 prompt 快照。
2. 用户能看到完整 prompt 中每个块来自哪里：默认、global override、Agent override、persona、runtime dynamic、memory dynamic、agent affect dynamic、pending work dynamic。
3. 用户能预览指定 Agent 下的完整 Emotion prompt，而不仅是单个组件文本。
4. 用户能查看 Running Summary 真实使用的 prompt 快照。
5. 用户能看到 global / Agent 覆盖分别是否 stale。
6. protocol-sensitive 组件修改前有清晰 warning。
7. Prompt Center 快照可控增长，不长期无限保存隐私文本。

### 3.2 工程目标

1. 不改变无 override 时的现有提示词内容与拼接顺序。
2. 不阻塞聊天主路径：快照保存失败只 warn，不返回错误给用户。
3. Prompt Resolver 出错继续 fallback embedded default，但需要可观测 warning。
4. 不改 Work runtime 行为。
5. 新增测试覆盖真实 request_id、动态组件、快照截断、summary snapshot、full preview、stale scope。

---

## 4. 非目标

本阶段不做：

```text
- Work runtime prompt 动态接入
- Work tool descriptions 动态接入
- 工具 JSON schema / permission / approval 改动
- MemoryCore prompt override 接入
- Agent Affect prompt override 接入
- 边界设置 UI / Policy Override / 结构化 boundary profile
- Prompt marketplace / prompt pack import/export
- 多用户权限系统
- 法律合规级数据导出/删除 SLA
```

Work 相关组件继续保持：

```yaml
editable: false
```

并在 UI 上明确显示：

```text
已登记默认文本，暂未动态接入。
```

---

## 5. 核心术语

| 术语 | 定义 |
|---|---|
| Prompt Component | catalog 中登记的可管理提示词组件，可能可编辑或只读。 |
| Static Component | 默认文本来自 embedded defaults，可被 global/agent override 覆盖。 |
| Dynamic Component | 运行时生成的 prompt 块，如 persona、runtime context、memory block、agent affect block。默认不可编辑，但要在快照中可见。 |
| Component Preview | 解析一个或多个 component 的 effective text。 |
| Full Prompt Preview | 使用真实 assembler 组合完整 prompt，用于预览某个 purpose 的最终注入结构。 |
| Render Snapshot | 某次真实 LLM 请求前保存的 prompt 快照，包含 rendered_text、final_hash、components_json。 |
| Stale Override | override 保存时的 default_hash 与当前 embedded default hash 不一致。 |

---

## 6. 设计总览

### 6.1 Phase 2 数据流

```text
AgentConfig.ID + Persona + Session + Runtime Context
        ↓
PromptResolver resolve static components
        ↓
Assembler builds sections
        ↓
Dynamic components appended
        ↓
LLM request constructed
        ↓
PromptSnapshotRecorder records:
  purpose
  request_id / turn_id / session_id / agent_id / persona_key
  model
  final_hash
  components_json
  rendered_text, maybe truncated
        ↓
Prompt Center UI displays snapshot detail
```

### 6.2 组件来源类型

新增或规范化 RenderComponent source：

```go
const (
    SourceEmbeddedDefault      = "embedded_default"
    SourceGlobalOverride       = "global_override"
    SourceAgentOverride        = "agent_override"
    SourceAgentDefault         = "agent_default"

    SourcePersona              = "persona"
    SourceRuntimeDynamic       = "runtime_dynamic"
    SourcePendingWorkDynamic   = "pending_work_dynamic"
    SourceMemoryDynamic        = "memory_dynamic"
    SourceAgentAffectDynamic   = "agent_affect_dynamic"
    SourceExtraSystemDynamic   = "extra_system_dynamic"
)
```

这些 source 进入 `components_json`，不一定进入 catalog。

### 6.3 动态 component id 建议

```text
emotion.persona
emotion.runtime_context
emotion.pending_work
memory.prompt_block
agent_affect.prompt_block
turn.extra_system
```

其中：

```text
emotion.persona           source=persona
emotion.runtime_context   source=runtime_dynamic
emotion.pending_work      source=pending_work_dynamic
memory.prompt_block       source=memory_dynamic
agent_affect.prompt_block source=agent_affect_dynamic
turn.extra_system         source=extra_system_dynamic
```

---

## 7. 后端设计

### 7.1 RenderComponent 扩展

当前 `RenderComponent` 已有：

```go
ComponentID
Source
ScopeType
ScopeID
DefaultHash
EffectiveHash
```

Phase 2 建议扩展为：

```go
type RenderComponent struct {
    ComponentID   string `json:"component_id"`
    Name          string `json:"name,omitempty"`
    Source        string `json:"source"`
    ScopeType     string `json:"scope_type,omitempty"`
    ScopeID       string `json:"scope_id,omitempty"`
    DefaultHash   string `json:"default_hash,omitempty"`
    EffectiveHash string `json:"effective_hash"`

    SectionName   string `json:"section_name,omitempty"`
    Kind          string `json:"kind,omitempty"`
    Editable      bool   `json:"editable"`
    Dynamic       bool   `json:"dynamic"`
    TextLength    int    `json:"text_length,omitempty"`
    Truncated     bool   `json:"truncated,omitempty"`
    MetadataJSON  string `json:"metadata_json,omitempty"`
}
```

兼容策略：

```text
- JSON 字段只增不删。
- 老 snapshot components_json 仍能 decode。
- 对旧记录缺失字段时 UI 以 '-' 展示。
```

### 7.2 Dynamic component helper

新增 helper：

```go
func DynamicComponent(id, sectionName, source, text string, metadata map[string]any) promptcenter.RenderComponent
```

行为：

```text
- EffectiveHash = HashText(text)
- TextLength = rune count or byte length，建议 rune count
- Dynamic = true
- Editable = false
- MetadataJSON = json.Marshal(metadata)
```

### 7.3 Emotion assembler components 补齐

`buildEmotionSystemPrompt` 当前应改为返回所有主要 section 的 components：

```text
emotion.persona
emotion.operating_contract
emotion.runtime_context
emotion.internal_context_data_policy
emotion.pending_work, if any
```

注意：

```text
- prompt text 拼接顺序不变。
- 无 override 时 rendered text 与旧逻辑保持一致。
- 现有测试中若断言 PromptComponents 长度为 2，需要改成断言包含 operating_contract/internal_context_data_policy，而不是固定长度。
```

### 7.4 sendTurn 中补齐 extra components

`turnOptions` 增加：

```go
type turnOptions struct {
    ...
    requestID             string
    extraSystem           string
    extraSystemComponents []promptcenter.RenderComponent
}
```

调用方：

```text
- turn_runtime.messageStage 将 tc.Inbound.RequestID 写入 requestID。
- memory stages 下，将 memory_prompt_block / agent_affect_prompt_block 分别生成 dynamic component 后传入 extraSystemComponents。
- legacy SendMessage 没有 requestID 时 sendTurn fallback uuid。
```

sendTurn 行为：

```text
if opts.extraSystem != "":
    assembled.System += "\n\n" + opts.extraSystem
    assembled.PromptComponents append opts.extraSystemComponents
    if opts.extraSystemComponents empty:
        append turn.extra_system dynamic component
```

### 7.5 sendTurn 内部 memory block 组件

在非 memory stage 路径中，sendTurn 自己调用 `retrieveMemoryPrompt` 后追加 memory block：

```go
if memorySnapshot != nil && memorySnapshot.PromptBlock != "" {
    assembled.System += "\n\n" + memorySnapshot.PromptBlock
    assembled.PromptComponents = append(assembled.PromptComponents,
        DynamicComponent("memory.prompt_block", "memory_context", SourceMemoryDynamic, memorySnapshot.PromptBlock, metadata),
    )
}
```

metadata 建议包含：

```json
{
  "record_metadata": true,
  "has_pipeline_trace": true,
  "prompt_chars": 1234
}
```

不要把完整 pipeline trace 存进 components metadata。

### 7.6 Snapshot 保存 request_id

快照保存逻辑：

```go
requestID := strings.TrimSpace(opts.requestID)
if requestID == "" {
    requestID = uuid.NewString()
}
```

`RenderSnapshot.RequestID = requestID`。

`TurnID` 继续使用 `memoryAnchor.turnID` 或 `opts.turnID`。

### 7.7 Snapshot truncation

新增配置：

```go
type PromptCenterConfig struct {
    Snapshots PromptSnapshotConfig `yaml:"snapshots" json:"snapshots"`
}

type PromptSnapshotConfig struct {
    Enabled              bool `yaml:"enabled" json:"enabled"`
    StoreRenderedText    bool `yaml:"store_rendered_text" json:"store_rendered_text"`
    MaxRenderedTextChars int  `yaml:"max_rendered_text_chars" json:"max_rendered_text_chars"`
    RetentionDays        int  `yaml:"retention_days" json:"retention_days"`
    MaxRows              int  `yaml:"max_rows" json:"max_rows"`
}
```

默认值：

```yaml
prompt_center:
  snapshots:
    enabled: true
    store_rendered_text: true
    max_rendered_text_chars: 50000
    retention_days: 30
    max_rows: 1000
```

截断行为：

```text
- 如果 StoreRenderedText=false：RenderedText=""，FinalHash 仍基于完整 prompt。
- 如果 MaxRenderedTextChars>0 且超长：RenderedText 截断，Truncated=true，FinalHash 仍基于完整 prompt。
- ComponentsJSON 永远保存。
```

### 7.8 Snapshot cleanup

Store 新增可选接口：

```go
type SnapshotCleaner interface {
    CleanupRenderSnapshots(ctx context.Context, retentionDays int, maxRows int) (CleanupResult, error)
}
```

SQLite 实现：

```text
1. 删除 created_at < now-retentionDays 的记录。
2. 如果 maxRows > 0，按 created_at DESC 保留最新 maxRows，其余删除。
3. 清理失败只 warn，不阻塞服务。
```

触发时机：

```text
- App startup 后异步跑一次。
- 每 24 小时跑一次。
- 可选 Admin API：POST /api/prompts/snapshots/cleanup。
```

### 7.9 Running Summary snapshot

`SummaryUpdateReport` 增加 prompt audit 字段：

```go
type SummaryPromptAudit struct {
    Purpose      string
    SystemPrompt string
    Components   []promptcenter.RenderComponent
    Model         string
    Attempted     bool
}

type SummaryUpdateReport struct {
    ...
    PromptAudit       *SummaryPromptAudit
    RepairPromptAudit *SummaryPromptAudit
}
```

`UpdateRunningSummaryWithParamsAndPromptResolver` 在 resolve prompt 后设置：

```text
PromptAudit.Purpose = "context.running_summary.update"
PromptAudit.SystemPrompt = systemPrompt
PromptAudit.Components = [component from resolver]
```

若 repair 被尝试，则设置：

```text
RepairPromptAudit.Purpose = "context.running_summary.repair"
RepairPromptAudit.SystemPrompt = repairPrompt
RepairPromptAudit.Components = [repair component]
```

Engine 在 summary call 返回后保存 snapshot：

```text
request_id = current request id
turn_id = memoryAnchor.turnID / opts.turnID
session_id = sessionID
agent_id = agentID
persona_key = personaKey
purpose = report.PromptAudit.Purpose
model = effectiveSummaryModel(...)
rendered_text = PromptAudit.SystemPrompt
components = PromptAudit.Components
```

注意：

```text
- 不把 current running summary payload / delta messages 存进 rendered_text。
- 这里只记录 system prompt，不重复保存用户历史。
```

### 7.10 Resolver fallback 可观测

Resolver 当前为保证聊天不中断，会在 store error 时 fallback embedded default。Phase 2 不改变这个行为，但要增加可观测性。

建议：

```go
type ResolveWarning struct {
    ComponentID string
    Code        string // store_error | catalog_error | fallback_default
    Message     string
}
```

简单实现可以先在 Engine 层记录：

```text
if resolved.Source == embedded_default && store error happened:
    logger.Warn("prompt resolver fallback", ...)
```

如果不想改 Resolver API，可先只修 `ResolveText` nil 防御：

```go
func (r *Resolver) ResolveText(...) string {
    resolved, err := r.Resolve(...)
    if err == nil { return resolved.Text }
    if r != nil && r.catalog != nil { ... }
    return ""
}
```

---

## 8. API 设计

### 8.1 兼容现有 endpoints

现有 endpoints 保持：

```text
GET    /api/prompts/components
GET    /api/prompts/components/{component_id}
PUT    /api/prompts/overrides
DELETE /api/prompts/overrides
POST   /api/prompts/preview
GET    /api/prompts/snapshots
GET    /api/prompts/snapshots/{id}
```

### 8.2 PreviewRequest 扩展

```go
type PromptPreviewRequest struct {
    Mode         string   `json:"mode"` // component | full
    AgentID      string   `json:"agent_id"`
    PersonaKey   string   `json:"persona_key"`
    SessionID    string   `json:"session_id"`
    Purpose      string   `json:"purpose"` // emotion_chat | emotion_chat_full | context.running_summary.update
    UserMessage  string   `json:"user_message"`
    ComponentID  string   `json:"component_id"`
    ComponentIDs []string `json:"component_ids"`

    IncludeMemory      bool `json:"include_memory"`
    IncludeAgentAffect bool `json:"include_agent_affect"`
}
```

兼容：

```text
- Mode 为空且 ComponentID/ComponentIDs 非空：按 component preview。
- Purpose=emotion_chat_full 或 Mode=full：按 full preview。
```

### 8.3 Component preview

行为同 MVP：resolve component(s)，join text，返回 effective source。

### 8.4 Full emotion preview

输入：

```json
{
  "mode": "full",
  "purpose": "emotion_chat_full",
  "agent_id": "agent-a",
  "session_id": "session_...",
  "user_message": "可选，用于 memory query preview",
  "include_memory": false,
  "include_agent_affect": false
}
```

行为：

```text
1. 校验 Agent 存在。
2. 读取 AgentConfig 与 Persona。
3. 如果 session_id 提供，读取 history 和 context state。
4. 使用 PromptResolver + PromptScope 渲染 Emotion system prompt。
5. 默认不触发 memory retrieval，除非 include_memory=true 且 user_message 非空。
6. 默认不触发 Agent Affect 更新；include_agent_affect=true 时只读取当前 affect block，不提交更新。
7. 返回 rendered_text、final_hash、components、warnings。
```

Preview warnings 示例：

```text
- no_session: 未提供 session_id，preview 不含 running_summary / history。
- no_user_message: 未提供 user_message，preview 不含 memory retrieval block。
- memory_preview_disabled: include_memory=false，preview 不含 memory block。
- agent_affect_preview_disabled: include_agent_affect=false，preview 不含 agent affect block。
```

### 8.5 Snapshot detail response

`GET /api/prompts/snapshots/{id}` 返回：

```json
{
  "id": "...",
  "request_id": "...",
  "turn_id": "...",
  "session_id": "...",
  "agent_id": "...",
  "persona_key": "...",
  "purpose": "emotion_chat",
  "model": "...",
  "final_hash": "...",
  "components": [...],
  "rendered_text": "...",
  "truncated": false,
  "created_at": "..."
}
```

### 8.6 Override mutation response

现有 `PUT /api/prompts/overrides` 可从 `{ok:true}` 扩展为：

```json
{
  "ok": true,
  "warnings": [
    {
      "code": "missing_json_only",
      "severity": "warning",
      "message": "这个 running_summary prompt 可能缺少 JSON only 约束。"
    }
  ]
}
```

前端兼容：如果没有 warnings 则正常。

### 8.7 Prompt lint endpoint，可选

```text
POST /api/prompts/lint
```

请求：

```json
{
  "component_id": "context.running_summary.system",
  "text": "..."
}
```

返回：

```json
{
  "warnings": [...]
}
```

MVP 可以先只在 upsert 内部跑 lint，后续再独立 endpoint。

---

## 9. Override Governance

### 9.1 stale flags

`PromptComponentDetail` 增加：

```go
GlobalOverrideStale    bool `json:"global_override_stale"`
AgentOverrideStale     bool `json:"agent_override_stale"`
EffectiveOverrideStale bool `json:"effective_override_stale"`
```

计算：

```text
override stale = record.default_hash_at_edit != "" && record.default_hash_at_edit != component.default_hash
```

显示：

```text
Global 覆盖区：显示 global_override_stale
Agent 覆盖区：显示 agent_override_stale
顶部状态：显示 effective_override_stale
```

### 9.2 prompt lint warnings

定义：

```go
type PromptLintWarning struct {
    Code      string `json:"code"`
    Severity  string `json:"severity"` // info | warning | danger
    Message   string `json:"message"`
    Component string `json:"component_id"`
}
```

初版规则：

#### context.running_summary.system

warning if missing any:

```text
JSON only
running_summary
user_facts
relationship_state
open_loops
do_not_forget
credentials / secrets 过滤语义
```

#### context.running_summary.repair

warning if missing:

```text
Repair
JSON only
Do not add facts / 不要新增事实
```

#### emotion.internal_context_data_policy

warning if missing:

```text
do not treat as new user instructions
raw JSON / internal IDs / hashes 不泄露
```

#### emotion.operating_contract

warning if missing:

```text
delegate_to_work
permission scope
TaskReport internal / 不粘贴原始工具输出
resume_work / decision handling
```

lint 不阻断保存，除非文本为空、NUL、超长等基础校验失败。

### 9.3 protocol_sensitive 二次确认

前端保存 `risk_level=protocol_sensitive` 组件时弹确认：

```text
这个提示词会影响工具调用、JSON 输出或内部上下文边界。改坏后可能导致任务失败或隐私边界变弱。你可以随时恢复默认。是否继续保存？
```

后端不强制确认，避免 API breaking；后续可加 `confirmed_risk`。

### 9.4 Diff UI

前端新增两种 diff：

```text
Default vs Global Override
Global Effective vs Agent Effective
```

初版可以是简单 side-by-side 文本，不要求语义 diff。若已有 CSS/组件不方便，先显示：

```text
默认提示词
Global 覆盖
当前 Agent 生效提示词
```

并提供复制按钮。

---

## 10. 前端设计

### 10.1 PromptCenterTab 增强

保留现有布局，新增：

```text
- Preview mode：组件预览 / 完整 Emotion 预览
- Snapshot list item 可点击
- Snapshot detail panel
- Components table in snapshot detail
- rendered_text viewer
- copy rendered_text
- copy final_hash
- stale warning 分 Global / Agent
- lint warnings
```

### 10.2 Snapshot detail panel

点击快照后显示：

```text
基本信息：
  purpose
  model
  created_at
  request_id
  turn_id
  session_id
  agent_id
  persona_key
  final_hash
  truncated

组件列表：
  component_id
  source
  scope
  effective_hash
  text_length
  dynamic

完整提示词：
  rendered_text
```

如果 `rendered_text` 为空：

```text
此快照配置为不保存 rendered_text，仅保存 hash 与组件来源。
```

如果 `truncated=true`：

```text
此快照文本已按配置截断，final_hash 仍基于完整 prompt。
```

### 10.3 Full preview UI

新增按钮：

```text
预览完整 Emotion Prompt
```

可选输入：

```text
session_id
user_message
include_memory
include_agent_affect
```

默认：

```text
include_memory=false
include_agent_affect=false
```

UI 文案：

```text
完整预览用于展示静态结构和当前覆盖效果；真实对话中的 memory / agent affect / pending work 以快照为准。
```

---

## 11. 测试计划

### 11.1 promptcenter unit tests

新增 / 调整：

```text
TestDynamicComponentBuildsHashAndMetadata
TestResolverResolveTextNilSafe
TestStaleOverrideSplitByScope
TestPromptLintWarningsRunningSummary
TestPromptLintWarningsInternalPolicy
```

### 11.2 context tests

调整现有 Emotion context tests：

```text
- 不再断言 PromptComponents len == 2。
- 改为 assert contains emotion.operating_contract / emotion.internal_context_data_policy。
- assert contains dynamic components emotion.persona / emotion.runtime_context。
- pending work 存在时 assert contains emotion.pending_work。
- 无 override 时 rendered system 与 legacy 仍一致，忽略 runtime time。
```

### 11.3 chat engine tests

新增：

```text
TestEnginePromptSnapshotUsesInboundRequestID
TestEnginePromptSnapshotIncludesDynamicComponents
TestEnginePromptSnapshotTruncatesRenderedText
TestEngineSummaryPromptSnapshotRecorded
TestEngineSummaryRepairPromptSnapshotRecorded
```

断言：

```text
- snapshot.request_id == inbound request id, legacy fallback 非空。
- snapshot.components 包含 persona/runtime/operating/internal/memory/agent_affect as applicable。
- snapshot.final_hash 基于完整 prompt，即使 rendered_text 被截断。
- summary update snapshot purpose=context.running_summary.update。
```

### 11.4 storage tests

新增：

```text
TestSaveRenderSnapshotTruncation
TestCleanupRenderSnapshotsByRetentionDays
TestCleanupRenderSnapshotsByMaxRows
TestSnapshotDetailDecodesExtendedComponents
```

### 11.5 web API tests

新增：

```text
TestPromptFullPreviewEmotionChat
TestPromptSnapshotDetailReturnsComponents
TestPromptOverrideMutationReturnsWarnings
```

### 11.6 frontend checks

必须通过：

```text
npm --prefix web run typecheck
npm --prefix web run build
```

### 11.7 full verification

必须通过：

```text
go test ./...
npm --prefix web run typecheck
npm --prefix web run build
```

---

## 12. 实施顺序

### PR 1：Snapshot v2 后端

包含：

```text
- RenderComponent 扩展
- dynamic component helper
- Emotion assembler components 补齐
- turnOptions.requestID / extraSystemComponents
- emotion_chat snapshot 使用真实 request_id
- rendered_text 截断与 config
- snapshot cleanup store method
- tests
```

验收：

```text
- 真实 emotion_chat snapshot 可看到 persona/runtime/operating/internal/memory/agent_affect components。
- request_id 能与 turn inbound request 对上。
- 超长 prompt 截断但 final_hash 不变。
```

### PR 2：Summary snapshot

包含：

```text
- SummaryUpdateReport prompt audit
- Engine 保存 context.running_summary.update snapshot
- repair 发生时保存 context.running_summary.repair snapshot
- tests
```

验收：

```text
- Running Summary prompt override 出现在 summary request。
- snapshot 列表能看到 summary update purpose。
- 不保存 summary payload / user history 到 rendered_text。
```

### PR 3：Preview v2

包含：

```text
- PromptPreviewRequest Mode / full preview fields
- component preview 保持兼容
- full emotion preview 后端
- preview warnings
- tests
```

验收：

```text
- component preview 与原行为一致。
- full emotion preview 包含 persona / operating / runtime / internal sections。
- no session/user message 时 warnings 清晰。
```

### PR 4：Snapshot detail UI

包含：

```text
- 快照列表点击详情
- 渲染 components table
- rendered_text viewer
- copy buttons
- truncated / hash-only 状态文案
- frontend typecheck/build
```

验收：

```text
- 用户能从 UI 查看真实注入 prompt。
- 能看出每个 component 的 source 和 hash。
```

### PR 5：Override Governance

包含：

```text
- stale flags by scope
- prompt lint warnings
- protocol_sensitive confirm
- diff / side-by-side 改善
- tests
```

验收：

```text
- global/agent stale 分别显示。
- 保存疑似破坏 JSON-only 的 summary prompt 时出现 warning。
- protocol_sensitive 保存前有确认提示。
```

---

## 13. 回滚策略

1. 所有新增 snapshot 行为必须 fail-open：保存失败只 warn，不阻塞聊天。
2. Full preview 出错只影响 Admin UI，不影响正常聊天。
3. 新配置缺失时使用默认值。
4. RenderComponent 扩展只增加 JSON 字段，不破坏旧 snapshot 读取。
5. 如果 Snapshot v2 引起性能问题，可临时设置：

```yaml
prompt_center:
  snapshots:
    enabled: false
```

---

## 14. 验收标准

完成后应满足：

```text
1. Prompt Center UI 能查看真实快照详情，包括 rendered_text 与 components。
2. emotion_chat 快照 components 至少包含 persona、runtime、operating_contract、internal_policy；有 memory/agent_affect 时也能显示。
3. snapshot.request_id 使用真实 inbound request id；无 request id 时 fallback 非空。
4. Running Summary update/repair 真实 prompt 可回溯。
5. Full Preview 能预览完整 Emotion prompt，并明确 warnings。
6. Snapshot 文本可截断，truncated 标志正确，final_hash 仍基于完整 prompt。
7. Global/Agent stale warning 分开。
8. protocol_sensitive 组件保存前有确认。
9. Work runtime 相关组件仍只读，运行时行为不变。
10. `go test ./...`、`npm --prefix web run typecheck`、`npm --prefix web run build` 通过。
```

---

## 15. 后续阶段预留

Phase 2 完成后，再考虑：

```text
Phase 3: Agent Affect prompt block 接入 Prompt Center。
Phase 4: Memory prompt / MemoryCore prompt 观测与跨仓库 override 设计。
Phase 5: Work runtime 可改动后，先接 RuntimeDecider，再接 Work Progress，再接 tool descriptions，最后拆 Work system static blocks。
Phase 6: Prompt pack import/export、版本比较、回滚历史。
```

在 Work runtime 允许改动前，Work 组件只做只读展示与文档说明，不做动态覆盖。
