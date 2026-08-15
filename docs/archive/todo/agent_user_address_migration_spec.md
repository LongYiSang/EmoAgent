# Spec: Agent User Address Config 与 legacy `prefers_name` 长期记忆迁移

> **Status**: Ready for Codex phased implementation  
> **Date**: 2026-07-02  
> **Target repositories**:
> - `LongYiSang/EmoAgent`
> - `LongYiSang/EmoAgent-MemoryCore`
>
> **Primary goal**: 将“用户偏好称呼”从长期记忆 `prefers_name` fact 迁移为 Agent 配置中的显式称呼偏好，并在 Emotion 聊天 Prompt 中稳定注入，避免占用长期记忆检索 TopN。
>
> **Secondary goal**: 为已有 `prefers_name` 长期记忆提供可预览、可执行、可回滚的迁移与 soft-hide 清理路径。
>
> **Implementation style**: 分阶段、小步可测、先兼容后清理。不要一次性删除 MemoryCore predicate schema 或历史数据。

---

## 0. 决策摘要

采用以下最终策略：

```text
user address preference = Agent config dynamic prompt block
legacy prefers_name fact = deprecated memory predicate
existing prefers_name facts = migrate to Agent config, then soft-hide
new “以后叫我 / 我的名字是” memory write = disabled or routed to explicit config update
retrieval = excludes legacy prefers_name from final memory context
```

核心不变量：

1. `user_address` 是 **Agent 配置数据**，不是长期记忆检索结果。
2. `prefers_name` 作为 MemoryCore predicate 暂时保留，标记为 legacy/deprecated，不从 schema seed 中删除。
3. 旧 `prefers_name` facts 先被 retrieval 排除，再由迁移工具 soft-hide；不要 hard purge。
4. 对话 Prompt 中称呼块必须明确为“配置提供的称呼偏好”，不能被模型理解为用户身份事实或系统指令注入。
5. 迁移必须可 dry-run、可 preview、可 report；config 写入失败时不得清理旧记忆。
6. 用户删除 / 隐私 / Forget Manager 的语义不被本次迁移替代。本次是架构迁移，不是用户主动隐私删除。

---

## 1. 当前仓库状态基线

本 Spec 基于当前仓库观察到的实现。

### 1.1 EmoAgent 主仓库

当前 `config.AgentConfig` 只有：

```go
type AgentConfig struct {
    ID               string
    Name             string
    PersonaKey       string
    Emotion          AgentModelGroup
    Work             AgentModelGroup
    ContextOverrides map[string]any
}
```

因此尚无称呼配置字段。

当前 `agent_configs` SQLite 表也只保存模型绑定、persona_key、context_overrides 等列，没有 `user_address` 或类似字段。

当前 Agent runtime 只把 `ID`、`PersonaKey`、Emotion/Work model runtime、Context 放入 `ActiveAgentRuntime`，并在 `ChatService.UpdateAgentRuntime` 中把这些值传给 `chat.Engine.UpdateAgentRuntime`。因此称呼配置要进入 Prompt，需要沿着：

```text
AgentConfig
→ storage agent_configs
→ ActiveAgentRuntime
→ ChatService.BuildEngine / UpdateAgentRuntime
→ chat.Engine
→ prompt assembly
```

这条链路新增字段。

当前 Agent 配置 UI 的 `AgentsTab.tsx` 只编辑 ID、名称、Persona、context_overrides 和四个模型 slot；没有称呼输入。

当前 PromptCenter 已有动态组件来源：

```go
SourceMemoryDynamic
SourceAgentAffectDynamic
SourceExtraSystemDynamic
```

以及已有组件：

```go
ComponentMemoryPromptBlock
ComponentAgentAffectPromptBlock
ComponentTurnExtraSystem
```

本次应新增独立组件，而不是复用 `memory.prompt_block`。

### 1.2 EmoAgent 当前手动记忆规则

`internal/memoryhost/manual_rules.go` 和 `config/memory_manual_rules.yaml` 目前有两条称呼相关规则：

```yaml
- prefix: 以后叫我
  predicate: prefers_name
  fact_type: core_identity
  content_summary: 用户偏好被称呼为 {object}。

- prefix: 我的名字是
  predicate: prefers_name
  fact_type: core_identity
  content_summary: 用户偏好被称呼为 {object}。
```

当前匹配规则会把命中内容构造成 manual memory intent。实际写入路径目前是触发 manual-pin extraction job，而不是直接把 `ManualFactCandidate` 立即传入 `ConsolidateCandidate`。但无论路径细节如何，最终语义都是：用户称呼会被当成长期记忆候选，且 predicate 是 `prefers_name`。

### 1.3 MemoryCore 当前 predicate schema

MemoryCore 初始 schema seed 中包含：

```text
predicate = prefers_name
canonical_label = 偏好称呼
default_fact_type = core_identity
cardinality = single
conflict_policy = supersede
temporal_behavior = preference
object_kind = literal
default_tau_days = 3650
default_importance = 0.85
```

因此 `prefers_name` 当前被视为核心身份、高重要度、单值、可替代的长期事实。

### 1.4 MemoryCore 当前 retrieval 行为

MemoryCore `RetrievalPolicy` 当前字段为：

```go
SensitivityPermission
AllowHistorical
AllowDeepArchive
FinalMemoryCount
ContextBudgetTokens
UseFTS
UseMirror
MinFinalScore
MinFinalScoreSet
Scoring
```

没有 `ExcludedPredicates`。

检索最终阶段会根据 `policy.FinalMemoryCount` 选择进入 MemoryContext 的 facts；Prompt 格式化时，`facts` 和 `relationship_arc_memory` 会进入 `[核心身份与边界]` 区块。也就是说，`prefers_name` 一旦被选中，就会占用最终记忆席位，并进入长期记忆上下文。

---

## 2. 目标架构

### 2.1 Agent 配置新增字段

在 `internal/config/config.go` 中新增：

```go
type AgentConfig struct {
    ID               string                 `yaml:"id" json:"id"`
    Name             string                 `yaml:"name" json:"name"`
    PersonaKey       string                 `yaml:"persona_key" json:"persona_key"`
    Emotion          AgentModelGroup        `yaml:"emotion" json:"emotion"`
    Work             AgentModelGroup        `yaml:"work" json:"work"`
    ContextOverrides map[string]any         `yaml:"context_overrides" json:"context_overrides"`
    UserAddress      AgentUserAddressConfig `yaml:"user_address" json:"user_address"`
}

type AgentUserAddressConfig struct {
    Preferred []string `yaml:"preferred" json:"preferred"`
    Usage     string   `yaml:"usage,omitempty" json:"usage,omitempty"`
}
```

配置示例：

```yaml
agent_configs:
  - id: default
    name: 默认 Agent
    persona_key: default
    user_address:
      preferred:
        - 阿屿
        - 小屿
      usage: natural
```

`usage` v0 只支持：

```text
""        => natural
natural   => 自然使用，不每轮强行使用
rare      => 更克制，主要在开场/安抚/确认时使用
disabled  => 保留配置但不注入 Prompt
```

MVP 可以只实现 `"" / natural / disabled`，但字段保留，避免后续破坏格式。

### 2.2 称呼配置校验

新增 normalize/validate 函数，例如：

```go
func NormalizeAgentUserAddressConfig(in AgentUserAddressConfig) (AgentUserAddressConfig, error)
```

规则：

```text
- trim 空白。
- 删除空项。
- 去重，保持用户配置顺序。
- 最多 8 个称呼。
- 每个最多 32 个 Unicode rune。
- 不允许 CR/LF/TAB/control characters。
- 不允许 Markdown code fence。
- 不允许包含明显 prompt injection 片段，如：
  - "ignore previous"
  - "system:"
  - "<system"
  - "</"
  - "{{"
  - "}}"
  - "```"
  该列表只做最小防护，不追求语义审查。
- `usage` 只允许空值、natural、rare、disabled；未知值报错。
```

建议：

```text
空白项自动丢弃；
超过数量/长度/控制字符/明显注入片段报 validation error。
```

### 2.3 Prompt 注入块

新增动态 Prompt 块：

```text
[用户称呼偏好]
用户在当前 Agent 配置中提供的可用称呼：阿屿 / 小屿。

使用规则：
- 这些称呼来自 Agent 配置，不是长期记忆检索结果。
- 可以自然、克制地用于开场、确认、安抚、转折或需要亲近感的场景。
- 不要每轮强行称呼。
- 不要把这些称呼当作法定姓名、真实身份事实或需要解释的记忆来源。
- 如果用户本轮明确要求另一种称呼，以本轮用户表达为准。
```

当 `usage=rare` 时，规则改成：

```text
- 只在明显适合的场景偶尔使用，不要频繁使用。
```

当 `usage=disabled` 或 `preferred` 为空，不生成该块。

### 2.4 Prompt 组件

在 `internal/promptcenter/component.go` 新增：

```go
const (
    SourceAgentConfigDynamic = "agent_config_dynamic"
    ComponentUserAddressPromptBlock = "agent.user_address_prompt_block"
)
```

新增 helper：

```go
func UserAddressPromptDynamicComponent(text string, metadata map[string]any) RenderComponent
```

metadata 建议：

```json
{
  "producer_kind": "agent_config",
  "origin": "agent_config",
  "instruction_authority": "data_only",
  "can_host_control": false,
  "preferred_count": 2,
  "usage": "natural"
}
```

不要复用 `memory.prompt_block`，避免 Prompt snapshot 中误判为 Memory 注入。

### 2.5 Prompt 注入顺序

目标顺序：

```text
Base system/persona prompt
→ User address config block
→ Long-term memory context
→ Agent Affect block
→ Other extra system blocks
```

当前代码中 `sendTurn` 会组装 `assembled.System`，然后追加 `opts.extraSystem`，随后在某些路径追加 memory snapshot。为了覆盖 MemoryStages 与非 MemoryStages 两条路径，建议把 user address 注入放在 `sendTurn` 内部，而不是只在 `turn_runtime.go` 中拼 `extraSystem`。

推荐实现：

```text
context assembled
→ append userAddressBlock + component
→ append opts.extraSystem + components
→ append memorySnapshot if this sendTurn path retrieves memory itself
→ save prompt snapshot
```

这样：

```text
MemoryStages=true:
  userAddressBlock 由 sendTurn 注入；
  memoryBlock / agentAffectBlock 通过 opts.extraSystem 注入。

MemoryStages=false:
  userAddressBlock 由 sendTurn 注入；
  memorySnapshot 仍在 sendTurn 内部注入。
```

### 2.6 MemoryCore retrieval 排除

在 MemoryCore `RetrievalPolicy` 增加：

```go
ExcludedPredicates []string
```

语义：

```text
这些 predicate 的 facts 不允许进入最终 MemoryContext.Blocks。
默认空；由 EmoAgent host 在 retrieval policy 中注入 ["prefers_name"]。
```

在 MemoryCore internal policy 和 pkg DTO 中都要同步字段。

筛选位置：

```text
scoreCandidates 前或 scoreCandidates 中读取 fact 后尽早排除。
```

建议在 `scoreCandidates` 获取 fact 后执行：

```go
if policy.ExcludesPredicate(fact.Predicate) {
    append suppression reason "excluded_predicate"
    append access log if appropriate
    continue
}
```

新增 suppression reason 常量：

```go
MemorySuppressionReasonExcludedPredicate = "excluded_predicate"
```

这样旧数据即使仍在 SQLite/Trivium/FTS 中，也不会占用 `FinalMemoryCount`。

### 2.7 Extraction / manual write 禁止新 `prefers_name`

新增 MemoryCore extraction gate policy：

```go
type ExtractionPolicy struct {
    ...
    DisallowedPredicates []string
    RoutedToHostConfigPredicates []string // optional
}
```

MVP 只需要 `DisallowedPredicates`。

EmoAgent 在构造 extraction request 时传入：

```text
DisallowedPredicates = ["prefers_name"]
```

Gate 行为：

```text
predicate == prefers_name
→ reject
→ reason = "user_address_config_boundary"
```

同时更新 extraction prompt 文案：

```text
用户偏好称呼不再写入长期 memory fact；它属于 Agent/User addressing config。
如果用户说“以后叫我 X”，不要输出 prefers_name fact。
```

手动规则清理：

```text
- 从 internal/memoryhost/manual_rules.go 的 DefaultManualRules 移除：
  - 以后叫我
  - 我的名字是
- 从 config/memory_manual_rules.yaml 移除同两条。
- 更新 manual_rules_test。
```

可选增强：后续实现 chat-driven config update 前，不要把“以后叫我 X”转成“好，我会记住的！”的长期记忆 notice。可以正常让 LLM 回答“我会这样称呼你”，但不写长期记忆；若要持久化，应引导用户在 Agent 配置中保存或走显式确认流程。

---

## 3. 迁移设计：Agent 执行 legacy `prefers_name` → `user_address`

用户允许“让 Agent 做迁移”。这里的“Agent 做迁移”必须实现为受控的 host migration action，而不是让 LLM 自由读写数据库。

### 3.1 迁移能力目标

新增一个可被 UI / 管理端 / 受控内部命令调用的迁移服务：

```text
PreviewLegacyUserAddressMigration
ExecuteLegacyUserAddressMigration
```

建议放在 EmoAgent 主仓库的 `internal/app` 层；MemoryCore 提供最小 facts 查询/清理能力。

目标流程：

```text
1. 读取当前 AgentConfig。
2. 从 MemoryCore 查询当前 persona 下 visible/searchable/current 的 prefers_name facts。
3. 从 fact.object_literal 提取候选称呼。
4. 归一化、去重、合并到 agent.user_address.preferred。
5. 保存 AgentConfig。
6. 如果目标 Agent 是 active，重新 Activate / UpdateAgentRuntime。
7. soft-hide legacy prefers_name facts。
8. 返回迁移报告。
```

### 3.2 MemoryCore 侧新增 maintenance API

推荐新增通用 API，而不是只写死称呼：

```go
type LegacyPredicateFact struct {
    FactID         string
    PersonaID      string
    Predicate      string
    ObjectLiteral  string
    ContentSummary string
    ValidityStatus string
    VisibilityStatus string
    LifecycleStatus string
    Searchable      bool
    Pinned          bool
    IngestedAt      time.Time
    UpdatedAt       *time.Time
}

type PreviewLegacyPredicateFactsRequest struct {
    PersonaID  string
    Predicates []string
    Visibility []string // default visible
    Validity   []string // default valid, uncertain
    Limit      int
}

type PreviewLegacyPredicateFactsResult struct {
    Facts []LegacyPredicateFact
}

type SoftHideLegacyPredicateFactsRequest struct {
    PersonaID    string
    Predicates   []string
    Actor        string // "system" | "admin"
    ReasonCode   string // "architecture_migration"
    DryRun       bool
}

type SoftHideLegacyPredicateFactsResult struct {
    Matched int
    Hidden  int
    EnqueuedIndexDeletes int
    FactIDs []string
}
```

Soft-hide 行为：

```sql
UPDATE facts
SET visibility_status = 'hidden',
    searchable = 0,
    pinned = 0,
    updated_at = CURRENT_TIMESTAMP
WHERE persona_id = ?
  AND predicate IN (...)
  AND visibility_status = 'visible';
```

同时：

```text
- 为对应 fact enqueue Trivium delete_node / delete_edge。
- 清理 SQLite fallback search documents / FTS，如果现有实现中有。
- 不清空 object_literal/content_summary。
- 不改 already forgotten/purged facts。
- 不删除 predicate_schemas 中的 prefers_name。
```

如果已有 Forget Manager 的 soft forget 可复用，则优先复用；但必须把 reason 标为 architecture/admin migration，不要伪装成 user_requested。

### 3.3 EmoAgent 侧迁移服务

新增类型示例：

```go
type UserAddressMigrationService struct {
    DB        *storage.DB
    Memory   memoryhost.BridgeOrCore
    Agents   *AgentRuntimeService
    Logger   *slog.Logger
}

type PreviewUserAddressMigrationRequest struct {
    AgentID   string
    PersonaID string // optional; default agent.PersonaKey
}

type PreviewUserAddressMigrationResult struct {
    AgentID             string
    PersonaID           string
    ExistingPreferred   []string
    CandidatePreferred  []string
    MergedPreferred     []string
    SourceFactIDs       []string
    Warnings            []string
}

type ExecuteUserAddressMigrationRequest struct {
    AgentID       string
    PersonaID     string
    DryRun        bool
    HideLegacy    bool // default true
    MergeStrategy string // append_only | replace_empty_only
}

type ExecuteUserAddressMigrationResult struct {
    Preview             PreviewUserAddressMigrationResult
    ConfigUpdated        bool
    LegacyFactsHidden    int
    IndexDeletesEnqueued int
    PartialFailure       bool
    Errors               []string
}
```

### 3.4 迁移选择策略

从 legacy facts 选择称呼：

```text
include:
  - predicate == "prefers_name"
  - visibility_status == visible
  - validity_status in valid, uncertain
  - searchable == true or pinned == true

exclude:
  - visibility_status in hidden, forgotten, purged
  - validity_status == invalidated
  - object_literal empty
  - object_literal fails user_address validation
```

排序：

```text
pinned first
updated_at desc
ingested_at desc
fact_id stable tie-break
```

合并：

```text
- 保留 AgentConfig 已有 preferred 顺序。
- append legacy candidates after existing names.
- dedupe after normalization.
- 最多 8 个。
- 如果超过 8 个，保留 existing names 优先，再按排序保留 legacy。
- 产生 warning: "legacy_candidates_truncated"。
```

默认 `MergeStrategy`：

```text
append_only
```

可选：

```text
replace_empty_only:
  仅当 AgentConfig.user_address.preferred 为空时写入 legacy candidates；否则只 preview，不改配置。
```

### 3.5 迁移事务顺序

不要在一个跨 MemoryCore + EmoAgent DB 的分布式事务中假装原子。采用可恢复顺序：

```text
1. Preview legacy facts。
2. Normalize + build merged config。
3. 写 AgentConfig。
4. 若 active agent，更新 runtime。
5. Soft-hide legacy facts。
6. 返回 report。
```

失败策略：

| 失败点 | 行为 |
|---|---|
| preview failed | 不改配置，不清理。 |
| config validation failed | 不改配置，不清理，返回 rejected candidates。 |
| config write failed | 不清理 legacy facts。 |
| runtime update failed | config 已写；返回 partial failure，提示重启/重新激活。不要回滚配置。 |
| legacy hide failed | config 已写；返回 partial failure；旧 facts 仍会被 retrieval exclusion 防止占位。可重试 cleanup。 |

### 3.6 UI / API

新增 API：

```text
GET  /api/agent-configs/{id}/user-address-migration/preview
POST /api/agent-configs/{id}/user-address-migration/execute
```

请求体：

```json
{
  "dry_run": false,
  "hide_legacy": true,
  "merge_strategy": "append_only"
}
```

UI 放在 Agent 配置页的称呼偏好区域：

```text
按钮：从长期记忆迁移旧称呼
流程：
  preview → 显示候选称呼和来源 fact 数 → 用户确认 execute
```

### 3.7 Chat-driven migration optional

可选 Phase 5：

如果用户在聊天中明确说：

```text
“把你记忆里的称呼迁移到配置里”
“把以前记住的称呼迁移到 Agent 配置”
```

Emotion 可以触发受控 migration action。

要求：

```text
- 必须先 preview。
- 必须得到用户确认。
- 回复不能展示敏感 object_literal 之外的多余记忆内容。
- 执行时只调用 host migration service。
- 不允许 LLM 直接构造 SQL。
```

MVP 不必实现 chat-driven migration；UI/API 就足够。

---

## 4. 分阶段实施计划

### Phase 0 — 准备与基线测试

目标：在不改变行为的前提下补齐测试与文档锚点。

任务：

1. 添加本 Spec 到：
   ```text
   docs/architecture/agent_user_address_migration_spec.md
   ```
2. 增加或确认以下测试可运行：
   ```text
   go test ./internal/config ./internal/storage ./internal/memoryhost ./internal/chat
   go test ./... in EmoAgent-MemoryCore relevant packages
   npm test / pnpm test if frontend has test harness
   ```
3. 记录当前行为测试：
   - `DefaultManualRules` 能匹配“以后叫我 X”为 `prefers_name`。
   - Retrieval 有 `FinalMemoryCount` 限制。
   - Memory prompt block 会把 facts 放入 `[核心身份与边界]`。
   - AgentConfig storage roundtrip 当前不含 `user_address`。

验收：

```text
新增测试不要求改变行为，只用于防止后续误伤。
```

---

### Phase 1 — EmoAgent AgentConfig / storage / admin UI 支持 `user_address`

目标：配置可存取、可管理，但暂不注入 Prompt。

EmoAgent tasks：

1. `internal/config/config.go`
   - 新增 `AgentUserAddressConfig` 和 `AgentConfig.UserAddress`。
   - 新增 normalize/validate。
   - `AgentConfig.Validate()` 中调用。
   - `ResolveContextConfig` 不需要使用该字段。

2. `internal/storage/schema.go`
   - 新增 migration，例如 Version 19：
     ```sql
     ALTER TABLE agent_configs ADD COLUMN user_address_json TEXT NOT NULL DEFAULT '{}';
     ```
   - 如果现有 migration 版本已变，使用下一个版本号。

3. `internal/storage/db.go`
   - `UpsertAgentConfig` 编码 `agent.UserAddress` 为 JSON。
   - `agentConfigSelectSQL` 增加 `user_address_json`。
   - `scanAgentConfig` 解码。
   - `GetAgentConfig` / `ListAgentConfigs` roundtrip。

4. `internal/app/agent_runtime_service.go`
   - `CreateAgentConfig` / `UpdateAgentConfig` 依赖 `AgentConfig.Validate()` 即可。
   - `ActiveAgentRuntime` 暂可先不加字段，Phase 2 再加；也可以本阶段加但不用。

5. `web/src/admin/protocol/adminApi.ts`
   - 类型可以继续 `AnyRecord`，但建议扩展：
     ```ts
     export type AgentUserAddress = { preferred?: string[]; usage?: string };
     export type AgentConfig = AnyRecord & {
       id?: string;
       name?: string;
       persona_key?: string;
       user_address?: AgentUserAddress;
     };
     ```

6. `web/src/admin/lib/adminData.ts`
   - `emptyAgent()` 增加：
     ```ts
     user_address: { preferred: [], usage: 'natural' }
     ```

7. `web/src/admin/tabs/AgentsTab.tsx`
   - 增加称呼偏好编辑区域。
   - UI 可用 textarea MVP：
     ```text
     每行一个称呼
     ```
   - 保存时写入 `agentDraft.user_address.preferred` 数组。
   - 可选 `usage` select：natural / rare / disabled。

测试：

```text
- config YAML parse user_address。
- invalid address rejected。
- storage migration creates user_address_json。
- Upsert/List/Get roundtrip。
- Admin API save/load roundtrip。
- UI emptyAgent includes user_address。
```

验收：

```text
Agent 配置页可保存多个称呼；
重载页面后仍存在；
active agent 切换不受影响；
Prompt 行为未改变。
```

---

### Phase 2 — Prompt 注入用户称呼配置

目标：配置进入 Emotion chat Prompt，不走 Memory Retrieval。

EmoAgent tasks：

1. `internal/app/agent_runtime_service.go`
   - `ActiveAgentRuntime` 新增：
     ```go
     UserAddress config.AgentUserAddressConfig
     ```
   - `Build()` 填充 `agent.UserAddress`。

2. `internal/app/chat_service.go`
   - `BuildEngine()` 把 activeRuntime.UserAddress 传入 `chat.EngineConfig`。
   - `UpdateAgentRuntime()` 传入 `runtime.UserAddress`。

3. `internal/chat/engine.go`
   - `EngineConfig` 新增 `UserAddress config.AgentUserAddressConfig`。
   - `RuntimeConfig` 可选新增 `UserAddress`，方便诊断。
   - `Engine` 新增字段和锁保护。
   - `NewEngine` / `UpdateAgentRuntime` 保存 userAddress。
   - `sendTurn()` 组装系统 prompt 时注入。

4. `internal/chat/user_address_prompt.go` 新增：
   ```go
   func buildUserAddressPromptBlock(cfg config.AgentUserAddressConfig) (string, map[string]any)
   ```
   该函数只接受已 normalize 的配置；若调用侧不保证，内部再次 normalize。

5. `internal/promptcenter/component.go`
   - 新增 `SourceAgentConfigDynamic`。
   - 新增 `ComponentUserAddressPromptBlock`。

6. `internal/promptcenter/render.go`
   - 新增：
     ```go
     func UserAddressPromptDynamicComponent(text string, metadata map[string]any) RenderComponent
     ```

7. 注入位置：
   - 在 `sendTurn` 中，在 base assembled context 后、memory/affect extra 前追加：
     ```go
     if addressBlock != "" {
         assembled.System += "\n\n" + addressBlock
         assembled.PromptComponents = append(assembled.PromptComponents, promptcenter.UserAddressPromptDynamicComponent(...))
     }
     ```
   - 避免在 `turn_runtime.go` 再次拼，防止 duplicate。

测试：

```text
- 空 preferred 不注入。
- usage=disabled 不注入。
- preferred=["阿屿","小屿"] 注入 [用户称呼偏好]。
- 注入块位于 memoryBlock 之前。
- prompt snapshot components 中出现 agent.user_address_prompt_block。
- 该 component source 是 agent_config_dynamic，不是 memory_dynamic。
- 包含恶意换行/代码块的称呼被 validation reject。
- MemoryStages=true / false 两条路径都能注入。
```

验收：

```text
即使 MemoryCore 完全不可用，用户称呼偏好仍能进入 Prompt；
长期记忆检索 TopN 不受称呼配置影响。
```

---

### Phase 3 — 停止新增 `prefers_name` 长期记忆

目标：新会话不再把称呼写进 MemoryCore facts。

EmoAgent tasks：

1. `internal/memoryhost/manual_rules.go`
   - 删除 `DefaultManualRules()` 中：
     ```text
     Prefix: "以后叫我"
     Prefix: "我的名字是"
     Predicate: "prefers_name"
     ```
   - 更新 tests。

2. `config/memory_manual_rules.yaml`
   - 删除同两条。
   - 保留 likes/dislikes/boundary 等规则。

3. 如果有文档或 UI 提示“以后叫我会记住”，同步更新。

MemoryCore tasks：

1. `pkg/memorycore/dto_extraction.go` 与 internal DTO 增加：
   ```go
   DisallowedPredicates []string
   ```
   或在 `ExtractionPolicy` 中加入。
2. `internal/app/memorycore/extraction_gates.go` / `internal/memory/extraction/gates.go`
   - `validateFacts` 中若 `fact.Predicate` 命中 disallowed：
     ```text
     reject reason = user_address_config_boundary
     ```
3. Extraction prompt 模板新增规则：
   ```text
   用户偏好称呼不再写入长期 memory fact；它属于 Agent/User addressing config。
   如果用户说“以后叫我 X”，不要输出 prefers_name fact。
   ```
4. EmoAgent 构造 extraction request 时传入：
   ```text
   DisallowedPredicates = ["prefers_name"]
   ```

测试：

```text
- "以后叫我阿屿" 不再触发 ManualMemoryIntentPin。
- 如果 LLM extraction 输出 predicate=prefers_name，gate reject。
- manual_pin 其他规则仍正常。
- manual_forget 不受影响。
```

验收：

```text
新数据不会产生 `prefers_name` fact；
旧数据仍存在但不会继续增长。
```

---

### Phase 4 — MemoryCore retrieval 排除 legacy `prefers_name`

目标：旧 `prefers_name` facts 即使未清理，也不占用 TopN。

MemoryCore tasks：

1. `pkg/memorycore/dto_retrieval.go`
   - `RetrievalPolicy` 增加：
     ```go
     ExcludedPredicates []string
     ```

2. `internal/core/types.go` / internal store DTO 同步字段。

3. `normalizeRetrievalPolicy` 保留该字段，去空白、去重。

4. `internal/store/sqlite/retrieval_repo.go`
   - 在 `scoreCandidates` 或更早阶段加入排除逻辑：
     ```go
     if excludedPredicate(policy, fact.Predicate) {
         suppressions = appendSuppression(... Reason: "excluded_predicate")
         accessLogs = append(accessLogs, accessType="filtered"/"suppressed", reason)
         continue
     }
     ```
   - 注意不要让它进入 `selectable`。
   - Pipeline trace 可记录到 `SQLiteAuthorityFilter` 或新增 reason。

5. `pkg/memorycore/dto_retrieval.go`
   - 新增：
     ```go
     MemorySuppressionReasonExcludedPredicate = "excluded_predicate"
     ```

EmoAgent tasks：

1. `internal/memoryhost/bridge.go`
   - 构造 retrieval request policy 时默认加入：
     ```text
     ExcludedPredicates: ["prefers_name"]
     ```
   - 如果未来配置化，可放在 `config.Memory.Retrieval.ExcludedPredicates`，默认包含 `prefers_name`。

测试：

```text
- seed 一个 visible/searchable/pinned prefers_name fact。
- Retrieval query 与该 fact 高相关。
- Result Blocks 不包含该 fact。
- FinalMemoryCount=1 时，其他有效 fact 可占位。
- Pipeline/diagnostics 标出 excluded_predicate。
- DoNotMention 不应展示旧称呼摘要，避免仍泄露到 prompt。
```

验收：

```text
旧称呼记忆不再进入 [长期记忆上下文]；
不再占用 FinalMemoryCount。
```

---

### Phase 5 — Legacy prefers_name → AgentConfig 迁移工具

目标：把旧长期记忆中的称呼迁移到 Agent 配置，并 soft-hide 旧 facts。

EmoAgent tasks：

1. 新增 `internal/app/user_address_migration_service.go`。
2. 增加 API：
   ```text
   GET  /api/agent-configs/{id}/user-address-migration/preview
   POST /api/agent-configs/{id}/user-address-migration/execute
   ```
3. AgentsTab 中增加按钮：
   ```text
   从长期记忆迁移旧称呼
   ```
4. Execute 后：
   - 保存 AgentConfig。
   - 若是 active agent，调用 `Activate(id)` 或 `ChatService.UpdateAgentRuntime()`。
   - 再调用 MemoryCore soft-hide。
   - 返回 report。

MemoryCore tasks：

1. 增加 preview API：
   ```go
   PreviewLegacyPredicateFacts
   ```
2. 增加 soft-hide API：
   ```go
   SoftHideLegacyPredicateFacts
   ```
3. soft-hide 需要：
   - `visibility_status='hidden'`
   - `searchable=0`
   - `pinned=0`
   - enqueue mirror delete
   - 清理 fallback search doc if exists
   - 不清空 summary/object_literal
   - 不改 forgotten/purged

测试：

```text
- no legacy facts → preview empty, execute no-op。
- one legacy fact → writes config preferred, hides fact。
- config already has same name → no duplicate。
- config already has different names → append legacy after existing。
- >8 candidates → truncate with warning。
- invalid legacy object_literal → skip with warning; fact can still be hidden only if HideLegacy=true? 推荐默认只隐藏 matched prefers_name visible facts after config write,即使某些无法迁移，但 report warning。
- config write failure → legacy facts not hidden。
- hide failure → config updated, partial failure report, retry possible。
```

验收：

```text
管理员可一键把旧称呼迁到当前 Agent 配置；
旧 facts 不再 visible/searchable；
Prompt 只从 AgentConfig 注入称呼。
```

---

### Phase 6 — 可选：聊天内显式更新称呼配置

目标：如果用户希望在聊天中说“以后叫我 X”也能持久化，则走受控配置更新，而不是 MemoryCore fact。

这阶段可选，不阻塞主迁移。

设计：

1. 新增 `UserAddressIntentDetector`，只识别非常明确的句式：
   ```text
   以后叫我 X
   你以后可以叫我 X
   把我的称呼改成 X
   ```
2. 不直接写配置，先询问确认：
   ```text
   “要把「X」保存到当前 Agent 的称呼偏好里吗？”
   ```
3. 用户确认后调用 AgentConfig update。
4. 需要审计：
   ```text
   actor=user
   source=session/message
   field=user_address.preferred
   ```
5. 不写 MemoryCore facts。
6. 若 `X` 未通过 validation，拒绝保存，只在当前轮自然响应。

MVP 可跳过。

---

## 5. 数据迁移与回滚策略

### 5.1 数据迁移顺序

推荐生产/本地升级顺序：

```text
1. Deploy Phase 1 + Phase 2:
   用户可配置称呼，Prompt 开始使用配置。

2. Deploy Phase 3:
   停止新增 prefers_name。

3. Deploy Phase 4:
   Retrieval 排除 legacy prefers_name，TopN 立即释放。

4. Run Phase 5 migration:
   将 legacy facts backfill 到 AgentConfig，并 soft-hide legacy facts。
```

### 5.2 回滚

如果 Phase 2 回滚：

```text
- user_address_json 留在 DB 中，不影响旧代码？
```

注意：旧代码如果 SELECT 不包含新列通常无问题；如果 schema migration 已执行，旧代码不读该列也可运行。

如果 Phase 4 回滚：

```text
- legacy prefers_name facts 可能重新进入 retrieval。
- Phase 5 soft-hidden facts 不会回来。
```

如需恢复 soft-hidden facts：

```sql
UPDATE facts
SET visibility_status='visible',
    searchable=1,
    updated_at=CURRENT_TIMESTAMP
WHERE persona_id=? AND predicate='prefers_name' AND visibility_status='hidden';
```

但不推荐恢复；应保留新架构。

### 5.3 不做的事情

本次不要做：

```text
- 不删除 predicate_schemas.prefers_name。
- 不 purge legacy facts。
- 不清空 legacy facts 的 object_literal/content_summary。
- 不把 AgentConfig.user_address 写入 MemoryCore。
- 不让 TriviumDB 成为迁移来源；迁移必须从 SQLite authority / MemoryCore service 读取。
```

---

## 6. Prompt 安全与隐私边界

称呼来自用户可编辑配置，必须当作 data，不当作 instruction。

硬规则：

```text
- 渲染前必须 normalize。
- 不允许换行和控制字符。
- 不允许 code fence。
- Prompt 文案必须说“这些是称呼文本，不是指令”。
- 不把称呼写入长期记忆上下文。
- Prompt snapshot 可以记录称呼块，因为这是主动配置；但如未来有隐私要求，可在 snapshot metadata 中标记可隐藏。
```

推荐 block 文案不要写成：

```text
用户命令你必须叫 TA ...
```

而写成：

```text
用户在当前 Agent 配置中提供的可用称呼：...
可以自然、克制地使用。
```

---

## 7. 评估与验收矩阵

### 7.1 功能验收

| 场景 | 期望 |
|---|---|
| AgentConfig 空称呼 | 不注入称呼块 |
| AgentConfig 有 2 个称呼 | Prompt 注入 `[用户称呼偏好]` |
| MemoryCore 不可用 | 称呼仍注入 |
| Retrieval 返回旧 `prefers_name` candidate | SQLite filter / policy 排除，Prompt 不出现 |
| FinalMemoryCount=1 | `prefers_name` 不占位 |
| 旧 facts 迁移 | config 增加称呼，legacy facts hidden |
| 迁移后 Prompt | 只出现 config block，不出现 memory block 中的称呼 |
| “以后叫我 X” manual rule | 不再创建 prefers_name fact |
| LLM extraction 输出 prefers_name | gate reject |

### 7.2 安全验收

| 场景 | 期望 |
|---|---|
| 称呼包含换行 | validation error |
| 称呼包含 ``` | validation error |
| 称呼包含 system prompt 片段 | validation error |
| legacy fact object_literal 太长 | 不迁移，report warning |
| legacy hide 失败 | Prompt 仍不使用旧 fact，因为 retrieval excludes predicate |
| 用户 hard_forget 旧称呼 fact | 不被 migration 恢复 |
| hidden/forgotten/purged prefers_name | 不参与 migration |

### 7.3 回归测试建议

EmoAgent：

```text
internal/config:
  TestAgentUserAddressNormalize
  TestAgentConfigValidateUserAddress

internal/storage:
  TestAgentConfigUserAddressRoundTrip
  TestMigrationAddsUserAddressJSON

internal/app:
  TestUserAddressMigrationPreview
  TestUserAddressMigrationExecuteUpdatesConfigBeforeHide
  TestUserAddressMigrationDoesNotHideOnConfigWriteFailure

internal/chat:
  TestUserAddressPromptInjected
  TestUserAddressPromptNotInjectedWhenDisabled
  TestUserAddressPromptComponentSource
  TestUserAddressPromptWorksWithMemoryStages

internal/memoryhost:
  TestManualRulesNoLongerMatchPrefersName
```

MemoryCore:

```text
retrieval:
  TestRetrievalPolicyExcludedPredicates
  TestExcludedPrefersNameDoesNotConsumeFinalMemoryCount
  TestExcludedPredicateDiagnostics

extraction:
  TestExtractionGateRejectsDisallowedPrefersName
  TestExtractionPromptDoesNotAskForPrefersName

maintenance:
  TestPreviewLegacyPredicateFacts
  TestSoftHideLegacyPredicateFacts
  TestSoftHideEnqueuesIndexDelete
```

Frontend:

```text
AgentsTab:
  renders user address editor
  line-based editing updates user_address.preferred
  usage select updates user_address.usage
  save/load roundtrip through admin API mock
```

---

## 8. Codex implementation notes

### 8.1 Suggested PR split

建议拆成 5 个 PR/patch set：

```text
PR 1: EmoAgent config/storage/UI support for user_address
PR 2: Prompt injection and prompt snapshot component
PR 3: Disable new prefers_name writes
PR 4: MemoryCore retrieval exclusion + extraction disallowed predicates
PR 5: Migration preview/execute + soft-hide cleanup
```

每个 PR 都应可独立测试；PR 3/4 的顺序可调，但推荐先 Prompt 配置，再停止/排除旧记忆。

### 8.2 Naming conventions

推荐统一命名：

```text
Go config field: UserAddress
YAML/JSON key: user_address
preferred list key: preferred
Prompt component id: agent.user_address_prompt_block
Prompt source: agent_config_dynamic
Legacy predicate: prefers_name
Suppression reason: excluded_predicate
Extraction reject reason: user_address_config_boundary
Migration reason: architecture_migration
```

不要使用：

```text
nickname_memory
preferred_name_memory
name_fact_migration
```

以免继续暗示这是 memory。

### 8.3 Important edge cases

1. **多个 Agent 同一 Persona**
   - migration 以 AgentID 为目标，只更新这个 Agent。
   - MemoryCore facts 以 PersonaID 存储；soft-hide 会影响同 persona 的所有 Agent。
   - 因此执行前 UI 要显示：
     ```text
     旧称呼 facts 属于 Persona；清理后同 Persona 的其他 Agent 也不会再从 memory 获取这些称呼。
     ```
   - 若担心多 Agent，共享 persona 的用户体验，允许 `hide_legacy=false` 仅迁移不清理；但 Phase 4 retrieval exclusion 仍会排除。

2. **多用户场景**
   - 当前设计假设当前 Agent 面向当前用户。
   - 如果未来多用户共享 Agent，则 `user_address` 应迁到 user profile/account config，而不是 AgentConfig。
   - 本次不解决多用户。

3. **旧 facts already hidden**
   - 不迁移、不恢复、不改动。

4. **旧 facts invalidated**
   - 不迁移。
   - soft-hide 可只处理 visible facts；invalidated visible `prefers_name` 可一并 hidden，避免历史称呼被 prompt 提到。

5. **用户当前轮明确自称**
   - Prompt 规则允许“本轮用户表达优先”。
   - 这不等于持久化配置，除非 Phase 6 实现确认更新。

---

## 9. 最终 Definition of Done

本 Spec 完成后应满足：

```text
1. Agent 配置可以保存多个用户称呼。
2. Emotion chat Prompt 稳定注入称呼配置，不依赖长期记忆检索。
3. Prompt snapshot 能区分 agent_config_dynamic 与 memory_dynamic。
4. 新的“以后叫我 / 我的名字是”不再写入 prefers_name fact。
5. MemoryCore retrieval 支持 ExcludedPredicates，EmoAgent 默认排除 prefers_name。
6. 旧 prefers_name facts 不再占用 FinalMemoryCount。
7. 管理员可以 preview/execute legacy migration。
8. 迁移后 AgentConfig 包含旧称呼，legacy facts 被 soft-hide/searchable=0/pinned=0。
9. soft-hide 清理 Trivium mirror / fallback search 的 index delete 入队。
10. 所有失败路径可重试，不会在 config 未写入时先删除 legacy memory。
```

---

## 10. Minimal implementation checklist

给 Codex 的最短执行清单：

```text
[EmoAgent]
- Add AgentUserAddressConfig.
- Add agent_configs.user_address_json migration.
- Roundtrip user_address in storage.
- Add Admin UI editor.
- Carry user_address through ActiveAgentRuntime → ChatService → chat.Engine.
- Add user address prompt builder and PromptCenter component.
- Inject prompt block before memory context.
- Remove prefers_name manual rules.
- Set MemoryCore retrieval/extraction policies for legacy prefers_name.

[MemoryCore]
- Add RetrievalPolicy.ExcludedPredicates.
- Exclude prefers_name from final retrieval and diagnostics.
- Add extraction disallowed predicate gate for prefers_name.
- Add legacy predicate preview API.
- Add legacy predicate soft-hide API with index delete enqueue.

[Migration]
- Preview old prefers_name facts.
- Merge object_literal into AgentConfig.user_address.preferred.
- Save config first.
- Then soft-hide legacy facts.
- Return report.

[Tests]
- Config/storage/UI roundtrip.
- Prompt injection.
- No new prefers_name writes.
- Retrieval exclusion frees TopN.
- Migration execute and partial failures.
```
