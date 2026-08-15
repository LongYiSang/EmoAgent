# EmoAgent Prompt Center with Agent-Level Overrides Spec

> Document status: Implementation Spec  
> Target path suggestion: `docs/architecture/prompt_center_agent_scope.md`  
> Scope: 将散落在 Go 代码、Persona、Work、Summary、Tool Description、Agent Affect 中的提示词统一登记、展示、覆盖、预览和恢复默认；第一版支持 `global` 与 `agent` 两级覆盖。  
> Non-goals: 不实现结构化边界设置；不修改 MemoryCore；不开放工具 JSON schema 编辑；不改变无 override 时的现有行为。

---

## 0. 一句话定义

Prompt Center 是一个统一提示词管理层：

```text
embedded default prompt
        ↓
global override，可选
        ↓
agent override，可选，按 AgentConfig.ID 生效
        ↓
effective prompt
        ↓
render snapshot，可查看某次请求实际注入内容
```

第一版目标是让用户知道“具体注入了什么提示词”，可以改 global 或某个 Agent 的提示词，并且可以恢复默认。

---

## 1. 当前仓库约束

当前 Agent 是独立配置对象，不等同于 Persona。`config.AgentConfig` 包含 `ID / Name / PersonaKey / Emotion / Work / ContextOverrides`。因此 Agent 级覆盖必须挂在 `AgentConfig.ID`，而不是 `PersonaKey`。

运行时 `ActiveAgentRuntime` 也已经保留 `ID` 和 `PersonaKey`。Prompt Center 的运行时 scope 应从 active agent runtime 传入 Emotion / Summary / Work prompt resolver。

当前无 override 时必须完全保持现有行为：

- Emotion system prompt 仍由 persona、operating contract、runtime context、internal context data policy、pending work 等部分组装；
- Persona system prompt 继续由现有 Persona 编辑逻辑管理；
- Work / Summary / Tool Description 默认文本保持与当前 Go 常量或构造结果一致；
- Tool JSON schema、权限规则、Approval、Work destructive boundary 不进入本次编辑范围。

---

## 2. 核心概念

### 2.1 Prompt Component

一个可管理的提示词单元。

```go
type PromptComponent struct {
    ID           string   `json:"id"`
    Group        string   `json:"group"`        // emotion | context | work | tool | agent_affect
    Name         string   `json:"name"`
    Description  string   `json:"description"`
    Kind         string   `json:"kind"`         // system_section | system_prompt | tool_description | template | static_block
    DefaultText  string   `json:"default_text"`
    Editable     bool     `json:"editable"`
    RiskLevel    string   `json:"risk_level"`   // normal | advanced | protocol_sensitive
    ScopeSupport []string `json:"scope_support"` // global | agent
    MaxChars     int      `json:"max_chars"`
    Order        int      `json:"order"`
}
```

### 2.2 Scope

第一版只支持：

```text
global: 所有 Agent 都继承。
agent: 只对指定 AgentConfig.ID 生效。
```

不做 Persona 级覆盖。Persona 仍然通过现有 Persona 页面编辑。

### 2.3 Override Mode

Agent 级覆盖不能只有“有/无文本”，否则当 global 已覆盖时，Agent 无法单独恢复到内置默认。因此 Agent scope 需要三态：

| Agent 状态 | 存储方式 | 生效结果 |
|---|---|---|
| 继承全局 | 无 agent override 行 | global custom > embedded default |
| 自定义 | `mode = custom` | 使用 agent override text |
| 使用内置默认 | `mode = use_default` | 跳过 global，直接使用 embedded default |

Global scope 只需要：

| Global 状态 | 存储方式 | 生效结果 |
|---|---|---|
| 无覆盖 | 无 global override 行 | embedded default |
| 自定义 | `mode = custom` | 使用 global override text |

Global 恢复默认 = 删除 global override 行。

---

## 3. 解析优先级

给定 `component_id` 和 `PromptScope{AgentID}`：

```text
1. 如果存在 enabled 的 agent override：
   1.1 mode = custom      → effective = agent override text, source = agent_override
   1.2 mode = use_default → effective = embedded default,     source = agent_default

2. 否则如果存在 enabled 的 global override：
   2.1 mode = custom      → effective = global override text, source = global_override

3. 否则：
   effective = embedded default, source = embedded_default
```

伪代码：

```go
func Resolve(componentID string, scope PromptScope) ResolvedPrompt {
    component := catalog.MustGet(componentID)

    if scope.AgentID != "" {
        row, ok := store.GetOverride(componentID, "agent", scope.AgentID)
        if ok && row.Enabled {
            switch row.Mode {
            case "custom":
                return resolved(component, row.OverrideText, "agent_override", row)
            case "use_default":
                return resolved(component, component.DefaultText, "agent_default", row)
            }
        }
    }

    row, ok := store.GetOverride(componentID, "global", "")
    if ok && row.Enabled && row.Mode == "custom" {
        return resolved(component, row.OverrideText, "global_override", row)
    }

    return resolved(component, component.DefaultText, "embedded_default", nil)
}
```

---

## 4. 数据库设计

新增 migration，当前仓库可使用下一版本号，例如 `Version: 21`。

```sql
CREATE TABLE IF NOT EXISTS prompt_overrides (
    id                    TEXT PRIMARY KEY,
    component_id          TEXT NOT NULL,
    scope_type            TEXT NOT NULL CHECK (scope_type IN ('global', 'agent')),
    scope_id              TEXT NOT NULL DEFAULT '',
    mode                  TEXT NOT NULL CHECK (mode IN ('custom', 'use_default')),
    override_text         TEXT NOT NULL DEFAULT '',
    enabled               INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    default_hash_at_edit  TEXT NOT NULL DEFAULT '',
    note                  TEXT NOT NULL DEFAULT '',
    created_at            TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at            TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(component_id, scope_type, scope_id),
    CHECK (
      (scope_type = 'global' AND scope_id = '') OR
      (scope_type = 'agent' AND scope_id <> '')
    ),
    CHECK (
      (mode = 'custom' AND length(override_text) > 0) OR
      (mode = 'use_default' AND override_text = '')
    )
);

CREATE INDEX IF NOT EXISTS idx_prompt_overrides_component
    ON prompt_overrides(component_id, scope_type, scope_id);

CREATE INDEX IF NOT EXISTS idx_prompt_overrides_agent
    ON prompt_overrides(scope_id, component_id)
    WHERE scope_type = 'agent';

CREATE TABLE IF NOT EXISTS prompt_render_snapshots (
    id                TEXT PRIMARY KEY,
    request_id        TEXT NOT NULL DEFAULT '',
    turn_id           TEXT NOT NULL DEFAULT '',
    session_id        TEXT NOT NULL DEFAULT '',
    agent_id          TEXT NOT NULL DEFAULT '',
    persona_key       TEXT NOT NULL DEFAULT '',
    purpose           TEXT NOT NULL,
    model             TEXT NOT NULL DEFAULT '',
    final_hash        TEXT NOT NULL,
    components_json   TEXT NOT NULL DEFAULT '[]',
    rendered_text     TEXT NOT NULL DEFAULT '',
    truncated         INTEGER NOT NULL DEFAULT 0 CHECK (truncated IN (0, 1)),
    created_at        TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_prompt_render_snapshots_session_time
    ON prompt_render_snapshots(session_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_prompt_render_snapshots_agent_time
    ON prompt_render_snapshots(agent_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_prompt_render_snapshots_purpose_time
    ON prompt_render_snapshots(purpose, created_at DESC);
```

说明：

- `scope_id` 在 `scope_type='agent'` 时指向 `agent_configs.id`。因为 SQLite 无法对条件外键做得很优雅，应用层必须验证 Agent 是否存在。
- `mode='use_default'` 只应允许 agent scope；global 恢复默认使用 DELETE。应用层校验即可。
- `default_hash_at_edit` 用来发现代码升级后内置默认提示词已变化，前端显示“默认值已更新，当前覆盖基于旧默认”。

---

## 5. 默认提示词资产

推荐目录：

```text
internal/promptcenter/defaults/
  components.yaml
  emotion.operating_contract.md
  emotion.internal_context_data_policy.md
  context.running_summary.system.md
  context.running_summary.repair.md
  work.runtime_decider.system.md
  work.progress_summary.system.md
  work.progress_summary.repair.md
  tool.delegate_to_work.description.md
  tool.resume_work.description.md
  tool.finish_task.description.md
  tool.request_decision.description.md
```

`components.yaml` 示例：

```yaml
- id: emotion.operating_contract
  group: emotion
  name: Emotion / Work 委派契约
  kind: system_section
  risk_level: advanced
  editable: true
  scope_support: [global, agent]
  max_chars: 20000
  order: 100
  default_file: emotion.operating_contract.md
  description: 控制 Emotion 何时委派 Work、如何选择权限、如何处理 Work 结果和暂停决策。
```

默认文件通过 `embed.FS` 打包。禁止运行时覆盖默认文件；用户改动只写入 `prompt_overrides`。

---

## 6. MVP 组件清单

第一批纳入 Prompt Center：

```text
emotion.operating_contract
emotion.internal_context_data_policy
context.running_summary.system
context.running_summary.repair
work.runtime_decider.system
work.progress_summary.system
work.progress_summary.repair
tool.delegate_to_work.description
tool.resume_work.description
tool.finish_task.description
tool.request_decision.description
```

第二批再拆：

```text
work.system.header
work.system.operating_loop
work.system.tool_selection_policy
work.system.verification
work.system.minimal_change_policy
work.system.permission
work.system.protocol_boundaries
work.system.decision_escalation
work.system.completion_contract
agent_affect.prompt.natural_block
agent_affect.prompt.numeric_footer
```

原因：`BuildWorkSystem` 当前包含 Goal / Background / Constraints / Acceptance Criteria / Execution Environment 等动态内容。第一版不要把整个 Work system prompt 变成一个大文本模板；先抽静态规则块，动态字段仍由代码生成。

---

## 7. Go 包设计

新增：

```text
internal/promptcenter/
  component.go
  catalog.go
  defaults_embed.go
  hash.go
  store.go
  resolver.go
  render.go
  snapshot.go
  validate.go
```

核心类型：

```go
type PromptScope struct {
    AgentID    string
    PersonaKey string
}

type OverrideMode string
const (
    OverrideModeCustom     OverrideMode = "custom"
    OverrideModeUseDefault OverrideMode = "use_default"
)

type ResolvedPrompt struct {
    ComponentID       string `json:"component_id"`
    Text              string `json:"text"`
    Source            string `json:"source"` // embedded_default | global_override | agent_override | agent_default
    ScopeType         string `json:"scope_type"`
    ScopeID           string `json:"scope_id"`
    DefaultHash       string `json:"default_hash"`
    EffectiveHash     string `json:"effective_hash"`
    DefaultHashAtEdit string `json:"default_hash_at_edit,omitempty"`
    StaleOverride     bool   `json:"stale_override"`
}
```

Store 接口：

```go
type Store interface {
    GetOverride(ctx context.Context, componentID, scopeType, scopeID string) (*OverrideRecord, error)
    ListOverrides(ctx context.Context) ([]OverrideRecord, error)
    UpsertOverride(ctx context.Context, req UpsertOverrideRequest) error
    DeleteOverride(ctx context.Context, componentID, scopeType, scopeID string) error
    SaveRenderSnapshot(ctx context.Context, snapshot RenderSnapshot) error
    ListRenderSnapshots(ctx context.Context, filter SnapshotFilter) ([]RenderSnapshotSummary, error)
    GetRenderSnapshot(ctx context.Context, id string) (*RenderSnapshot, error)
}
```

Resolver 必须支持 fallback：如果 DB 不可用或 component 未接入，使用内置默认并记录 warning，不应导致聊天不可用。

---

## 8. 运行时接入

### 8.1 Agent ID 传递

当前 `chat.EngineConfig` 和 `chat.Engine` 需要新增：

```go
AgentID string
```

`RuntimeConfig` 和 `UpdateAgentRuntime` 也应保留并更新 AgentID。

`ChatService.BuildEngine` 从 `activeRuntime.ID` 注入 AgentID；`ChatService.UpdateAgentRuntime` 调用 `engine.UpdateAgentRuntime(...)` 时也传入 `runtime.ID`。

### 8.2 Emotion prompt

现状：

```go
wrapSystemSection("operating_contract", delegationGuideline)
wrapSystemSection("internal_context_data_policy", internalContextDataPolicy)
```

改为：

```go
operating := resolver.ResolveText(ctx, "emotion.operating_contract", scope)
policy := resolver.ResolveText(ctx, "emotion.internal_context_data_policy", scope)
```

`BuildEmotionContext...` 系列函数需要接受可选 `PromptResolver` 或 `PromptRenderContext`。为了最小改动，可新增 `BuildEmotionContextWithPromptResolver`，旧函数使用默认 resolver。

### 8.3 Summary prompt

将：

```go
summarySystemPrompt
summaryRepairSystemPrompt
```

改为 resolver 获取：

```go
context.running_summary.system
context.running_summary.repair
```

Summary 更新函数需要拿到 AgentID。若短期内不方便传 AgentID，可先使用 global；但目标实现必须支持 Agent scope。

### 8.4 Work / Tool prompt

第一版先接：

```text
work.runtime_decider.system
work.progress_summary.system
work.progress_summary.repair
tool.*.description
```

`NewDelegateToolWithFactory...` 等构造 tool spec 时需要 resolver。如果工具注册早于 active agent 解析，则 description 可先用 global/default；Agent 级 tool description 后续可在每次 LLM request 前根据 agent scope 动态生成 ToolDef。MVP 可以接受 tool description 只支持 global，并在 UI 标记 agent scope 暂未接入；但 Spec 目标是最终支持 agent scope。

### 8.5 Render Snapshot

每次构造 LLM ChatRequest 前，记录：

```text
purpose: emotion_chat | summary_update | work_run | work_progress | runtime_decider
agent_id
persona_key
session_id
model
components_json
final_hash
rendered_text
```

MVP 至少记录 `emotion_chat` 的最终 system prompt。

---

## 9. API 设计

新增到 `AdminApp`：

```go
ListPromptComponents(ctx context.Context, agentID string) (PromptComponentsResponse, error)
GetPromptComponent(ctx context.Context, id, agentID string) (PromptComponentDetail, error)
UpsertPromptOverride(ctx context.Context, req UpsertPromptOverrideRequest) error
DeletePromptOverride(ctx context.Context, req DeletePromptOverrideRequest) error
PreviewPrompt(ctx context.Context, req PromptPreviewRequest) (PromptPreviewResponse, error)
ListPromptSnapshots(ctx context.Context, req PromptSnapshotListRequest) (PromptSnapshotListResponse, error)
GetPromptSnapshot(ctx context.Context, id string) (PromptSnapshotDetail, error)
```

路由：

```http
GET    /api/prompts/components?agent_id={id}
GET    /api/prompts/components/{component_id}?agent_id={id}
PUT    /api/prompts/overrides
DELETE /api/prompts/overrides?component_id=...&scope_type=...&scope_id=...
POST   /api/prompts/preview
GET    /api/prompts/snapshots?agent_id=...&session_id=...&purpose=...&limit=...
GET    /api/prompts/snapshots/{id}
```

`PUT /api/prompts/overrides` 请求：

```json
{
  "component_id": "emotion.operating_contract",
  "scope_type": "agent",
  "scope_id": "coding-agent",
  "mode": "custom",
  "override_text": "...",
  "enabled": true,
  "note": "coding agent wants stricter work delegation"
}
```

Agent 使用内置默认：

```json
{
  "component_id": "emotion.operating_contract",
  "scope_type": "agent",
  "scope_id": "coding-agent",
  "mode": "use_default",
  "override_text": "",
  "enabled": true
}
```

删除 Agent override = 继承 global：

```http
DELETE /api/prompts/overrides?component_id=emotion.operating_contract&scope_type=agent&scope_id=coding-agent
```

删除 global override = global 恢复内置默认：

```http
DELETE /api/prompts/overrides?component_id=emotion.operating_contract&scope_type=global&scope_id=
```

组件列表响应必须同时返回：

```json
{
  "component_id": "emotion.operating_contract",
  "default_text": "...",
  "global_override": {...},
  "agent_override": {...},
  "effective_text": "...",
  "effective_source": "agent_override",
  "default_hash": "...",
  "effective_hash": "...",
  "stale_override": false
}
```

---

## 10. 前端设计

新增 Admin Tab：`提示词中心`。

### 10.1 页面布局

```text
顶部：Agent 选择器
  - 当前 active Agent
  - 可切换查看任意 Agent 的 effective prompt
  - 选项：“只看有覆盖项”

左侧：组件列表
  - Emotion
  - Context Summary
  - Work
  - Tool Description
  - Agent Affect（第二批）

右侧：组件详情
  - ID / 分组 / 风险等级 / 描述
  - 默认提示词
  - Global 覆盖编辑区
  - 当前 Agent 覆盖编辑区
  - 当前 Agent 生效提示词
  - default vs effective diff
  - 保存 global
  - 恢复 global 默认
  - 保存 Agent 自定义
  - Agent 继承全局
  - Agent 使用内置默认

底部：预览与快照
  - 选择 purpose: emotion_chat / summary_update / work_run
  - 预览最终注入 prompt
  - 最近真实注入快照列表
```

### 10.2 UI 文案

Agent override 三个按钮文案：

```text
保存为此 Agent 自定义
此 Agent 继承全局设置
此 Agent 使用内置默认（忽略全局覆盖）
```

Global override 两个按钮文案：

```text
保存全局覆盖
恢复全局为内置默认
```

风险提示：

```text
protocol_sensitive：这个提示词会影响工具调用、JSON 输出或 Work 协议。改坏后可能导致任务无法完成。你可以随时恢复默认。
```

---

## 11. 校验规则

后端校验：

1. `component_id` 必须存在于 catalog。
2. 组件 `Editable=false` 时拒绝写入 override。
3. `scope_type` 只能是 `global` 或 `agent`。
4. `scope_type=global` 时 `scope_id` 必须为空。
5. `scope_type=agent` 时 `scope_id` 必须是存在的 `agent_configs.id`。
6. `mode=use_default` 只允许 agent scope。
7. `mode=custom` 时 `override_text` 不能为空，长度不超过 `MaxChars`。
8. 文本保留用户原始换行，但拒绝 NUL 字符。
9. `default_hash_at_edit` 由后端计算，不信任前端。
10. 默认 hash 变化只提示 stale，不阻止使用。

---

## 12. 测试计划

### 12.1 Go 单元测试

```text
internal/promptcenter
  - catalog loads defaults
  - hash stable with same text
  - resolve default without override
  - resolve global override
  - resolve agent custom above global
  - resolve agent use_default bypasses global
  - delete agent override falls back to global
  - stale override detection
  - validation rejects bad scope / missing agent / use_default global
```

### 12.2 集成测试

```text
internal/context
  - no override 输出与迁移前 golden prompt 一致
  - global override 生效
  - agent override 只对指定 Agent 生效

internal/storage
  - migration creates tables and indexes
  - CRUD prompt_overrides
  - snapshot insert/list/get

internal/web
  - list components
  - upsert/delete global override
  - upsert/delete agent override
  - preview returns effective_source
```

### 12.3 前端检查

```bash
cd web && npm run typecheck
cd web && npm run build
```

### 12.4 全量检查

```bash
go test ./...
```

---

## 13. 实施顺序

### Phase 1: Prompt Center 基础包

- 新增 `internal/promptcenter`。
- 添加 embedded defaults 与 catalog。
- 添加 resolver 与 in-memory test store。
- 不接运行时，不改变行为。

### Phase 2: SQLite store + API

- 新增 migration。
- 实现 DB helper。
- 扩展 App / AdminApp / API routes。
- 加 API tests。

### Phase 3: 前端 Prompt Center

- 新增 `web/src/admin/protocol/promptsApi.ts`。
- 新增 hook 与 tab。
- 接入 Agent selector。
- 支持 global 与 agent 的保存/恢复。

### Phase 4: Emotion + Summary 接入

- Engine / RuntimeConfig / UpdateAgentRuntime 增加 AgentID。
- `BuildEmotionContext` 接 resolver。
- Summary prompt 接 resolver。
- 记录 emotion_chat snapshot。

### Phase 5: Work / Tool 接入

- `work.runtime_decider.system`、`work.progress_summary.*` 接 resolver。
- Tool description 接 resolver；若动态 agent tool description 变更较大，可先 global，UI 标记 agent 暂不接入。

---

## 14. 完成标准

1. 无 override 时，现有聊天、summary、Work 相关测试全部通过，默认行为不变。
2. 用户可以在前端看到每个组件的默认、global 覆盖、agent 覆盖和最终生效文本。
3. 用户可以保存 global override。
4. 用户可以为某个 Agent 保存 custom override。
5. 用户可以让某个 Agent 继承 global。
6. 用户可以让某个 Agent 使用内置默认并跳过 global。
7. 用户可以恢复 global 为内置默认。
8. Preview 能显示指定 Agent 下的 effective prompt 和 component source。
9. 至少 emotion_chat 真实请求会记录 prompt_render_snapshot。
10. `go test ./...` 与 `cd web && npm run typecheck` 通过。
