# Agent Affect v2 实施用 Spec 文档

> **Document status**: Implementation Spec Draft  
> **Version**: v2.0-spec-draft  
> **Target path**: `docs/todo/agent_affect_v2_implementation_spec.md`  
> **Target repo**: `LongYiSang/EmoAgent` main repository  
> **Primary goal**: 在 EmoAgent 主仓库中落地 Agent Affect v2 MVP：配置、SQLite 表、`internal/agentaffect` 服务、LLM-first 心情评估、Prompt 注入、插件 facade/capability、调试 API 和测试。  

---

## 0. 施工总目标

实现一个可运行的 Agent Affect v2 MVP：

```text
1. 当前心情可读取。
2. 文本 / 事件 / 插件提交可由 LLM 评估心情变化。
3. Go 对 LLM proposed_delta 做结构校验和限幅。
4. 心情 state / evaluation / event 可持久化到主仓库 SQLite。
5. 回复前把 mood vector + cause_summary + attachment config 注入 Prompt。
6. 插件可通过 capability/facade 读取、分析、提交、写 delta。
7. 提供基础 HTTP debug API。
8. go test ./... 通过。
```

MVP 先做主仓库本地运行时，不修改 `EmoAgent-MemoryCore`。MemoryCore 中已有 Agent Affect placeholder 表和配置可作为参考，但 v2 运行时先落在 EmoAgent 主 DB `data/emo.db`，避免跨仓库同步复杂化。

---

## 1. 当前仓库代码落点分析

### 1.1 Turn Pipeline

当前 Turn Pipeline 在 `internal/chat/turn_runtime.go` 中组织。启用 memory stages 时，顺序是：

```text
normalize
memory_prepare
emotion_prepare
messageStage/emotion_loop
memory_commit
emit_approvals
```

Agent Affect v2 MVP 不必新增 StageName；建议先接在 `emotionPrepareStage` 中，在 memory retrieval 之后计算心情，并把 Prompt block 放入 `tc.Diagnostics`：

```text
tc.Diagnostics["agent_affect_snapshot"]
tc.Diagnostics["agent_affect_prompt_block"]
tc.Diagnostics["agent_affect_evaluation"]
```

`messageStage` 当前已经从 diagnostics 读取 `memory_prompt_block`，作为 `extraSystem` 传给 `sendTurn`。需要改为拼接：

```go
extraSystem := joinSystemBlocks(
    tc.Diagnostics["memory_prompt_block"],
    tc.Diagnostics["agent_affect_prompt_block"],
)
```

### 1.2 Prompt 注入点

`Engine.sendTurn` 已支持 `turnOptions.extraSystem`，并会将其追加到 `assembled.System`。因此 Agent Affect 不需要改 context assembler，只需要在 turn runtime 中构造 `extraSystem`。

### 1.3 LLM 调用能力

`internal/llm.Client` 已提供：

```go
Chat(ctx, req)
ChatStream(ctx, req, cb)
```

`llm.RequestParams` 已支持：

```go
MaxTokens
Temperature
TopP
ReasoningEffort
Thinking
Stream
Extra
```

Agent Affect LLM Evaluator 可使用非流式 `Chat`，并通过配置启用 thinking / reasoning effort。

### 1.4 配置现状

当前主配置结构 `config.Config` 已包含 chat、memory、plugins、agent configs 等。`config.yaml` 中插件默认关闭，hook timeout 很短：

```yaml
plugins:
  enabled: false
  default_timeout_ms: 80
  max_timeout_ms: 1000
```

因此不要把 LLM 心情计算塞进普通 plugin hook。Agent Affect 应是 core service，插件通过 facade 提交事件。

### 1.5 插件系统现状

当前插件系统已有：

```text
Capability
HookName
Manifest validation
Authorizer
HookBus
BuiltinRunner
DefaultBuiltinPlugins
```

但还没有 `agent_affect.*` capabilities 和 hooks。manifest 会拒绝 unknown capability / hook，因此必须先扩展 `internal/plugin/types.go` 和 `internal/config/config.go` 的 known hook validation。

### 1.6 存储现状

主仓库 SQLite migration 写在 `internal/storage/schema.go` 的 `migrations` 切片里，目前已到 version 19。新增 Agent Affect v2 表建议作为 version 20 追加。

主仓库 DB 与 MemoryCore DB 分离：

```text
EmoAgent main DB: data/emo.db
MemoryCore DB: config/memorycore.yaml 默认 data/memory.db
```

因此主仓库可新增 `agent_affect_*` 表，不与 MemoryCore 表直接冲突。

### 1.7 服务 wiring 现状

`internal/app/kernel.go` 的 `Services` 当前包括：

```text
Config, Personas, LLMProviders, AgentRuntime, Sidecar, Memory, Tools, Plugins, Work, Chat, Sessions
```

需要新增：

```go
AgentAffect *AgentAffectService
```

并在 `newServices` 中构造。`ChatService.BuildEngine` 需要把 Agent Affect service 注入 `chat.EngineConfig`。

---

## 2. 推荐实施阶段

## Phase 1：配置与 DTO

### 2.1 新增配置结构

文件：`internal/config/config.go`

新增顶层字段：

```go
type Config struct {
    ...
    AgentAffect AgentAffectConfig `yaml:"agent_affect" json:"agent_affect"`
}
```

不要把 v2 放在 `memory.agent_affect` 下。原因：v2 是 Emotion runtime 的心情系统，不是 MemoryCore retrieval placeholder。

新增结构：

```go
type AgentAffectConfig struct {
    Enabled        bool `yaml:"enabled" json:"enabled"`
    StorageEnabled bool `yaml:"storage_enabled" json:"storage_enabled"`

    Evaluator AgentAffectEvaluatorConfig `yaml:"evaluator" json:"evaluator"`
    Context   AgentAffectContextConfig   `yaml:"context" json:"context"`
    Dimensions AgentAffectDimensionsConfig `yaml:"dimensions" json:"dimensions"`
    Externalization AgentAffectExternalizationConfig `yaml:"externalization" json:"externalization"`
    PluginAPI AgentAffectPluginAPIConfig `yaml:"plugin_api" json:"plugin_api"`
    Limits AgentAffectLimitsConfig `yaml:"limits" json:"limits"`
    Features AgentAffectFeaturesConfig `yaml:"features" json:"features"`
    Prompt AgentAffectPromptConfig `yaml:"prompt" json:"prompt"`
}
```

最小 evaluator 配置：

```go
type AgentAffectEvaluatorConfig struct {
    Mode            string            `yaml:"mode" json:"mode"` // llm | disabled
    ProviderID      string            `yaml:"provider_id" json:"provider_id"`
    Model           string            `yaml:"model" json:"model"`
    ThinkingEnabled bool              `yaml:"thinking_enabled" json:"thinking_enabled"`
    ReasoningEffort string            `yaml:"reasoning_effort" json:"reasoning_effort"`
    TimeoutMS       int               `yaml:"timeout_ms" json:"timeout_ms"`
    MaxOutputTokens int               `yaml:"max_output_tokens" json:"max_output_tokens"`
    Temperature     float64           `yaml:"temperature" json:"temperature"`
    StoreHiddenThinking bool          `yaml:"store_hidden_thinking" json:"store_hidden_thinking"`
}
```

### 2.2 默认配置

在 `DefaultConfig()` 中加入：

```go
AgentAffect: AgentAffectConfig{
    Enabled: false,
    StorageEnabled: true,
    Evaluator: AgentAffectEvaluatorConfig{
        Mode: "llm",
        TimeoutMS: 30000,
        MaxOutputTokens: 4096,
        Temperature: 0.2,
    },
    Context: AgentAffectContextConfig{
        Mode: "raw_window",
        RawKeepLastRequests: 20,
        RawKeepLastTokens: 12000,
        IncludePreviousEvaluations: true,
        PreviousEvaluationKeepLast: 30,
        SummaryEnabled: false,
        StoreRawInputs: true,
        StorePromptSnapshot: false,
    },
    Externalization: AgentAffectExternalizationConfig{
        Attachment: ExternalizedDimensionConfig{Enabled: true, DefaultStyle: "gentle_explicit", MaxVisibleIntensity: 0.65},
        Frustration: ExternalizedDimensionConfig{Enabled: false},
    },
    Prompt: AgentAffectPromptConfig{
        IncludeMoodBlock: true,
        IncludeReason: true,
        IncludeExpressionGuidance: false,
        IncludeNumericValues: true,
    },
}
```

### 2.3 config.yaml 示例

在 `config.yaml` 新增顶层：

```yaml
agent_affect:
  enabled: false
  storage_enabled: true
  evaluator:
    mode: llm
    provider_id: deepseek
    model: deepseek-v4-flash
    thinking_enabled: true
    reasoning_effort: medium
    temperature: 0.2
    timeout_ms: 30000
    max_output_tokens: 4096
    store_hidden_thinking: false
  context:
    mode: raw_window
    raw_keep_last_requests: 20
    raw_keep_last_tokens: 12000
    include_previous_evaluations: true
    previous_evaluation_keep_last: 30
    summary_enabled: false
    store_raw_inputs: true
    store_prompt_snapshot: false
  externalization:
    attachment:
      enabled: true
      default_style: gentle_explicit
      max_visible_intensity: 0.65
    frustration:
      enabled: false
  plugin_api:
    enabled: true
    plugin_safe_include_reason: true
    plugin_safe_include_raw_text: false
    ordinary_plugins_can_commit: true
    ordinary_plugins_can_write_delta: true
    trusted_plugins_can_write_target: true
  limits:
    per_request_delta:
      valence: 0.15
      arousal: 0.18
      dominance: 0.12
      energy: 0.12
      warmth: 0.15
      concern: 0.18
      curiosity: 0.18
      playfulness: 0.15
      attachment: 0.08
      frustration: 0.08
      uncertainty: 0.12
    absolute:
      attachment_max: 0.75
      frustration_max: 0.35
  prompt:
    include_mood_block: true
    include_reason: true
    include_expression_guidance: false
    include_numeric_values: true
```

---

## Phase 2：SQLite migration

### 2.1 修改文件

文件：`internal/storage/schema.go`

追加 migration：

```go
{
    Version: 20,
    SQL: `...agent affect v2 tables...`,
}
```

### 2.2 表：`agent_affect_profiles`

```sql
CREATE TABLE IF NOT EXISTS agent_affect_profiles (
    id TEXT PRIMARY KEY,
    persona_id TEXT NOT NULL,
    profile_name TEXT NOT NULL DEFAULT 'default',

    baseline_valence REAL NOT NULL DEFAULT 0.0 CHECK (baseline_valence >= -1.0 AND baseline_valence <= 1.0),
    baseline_arousal REAL NOT NULL DEFAULT 0.2 CHECK (baseline_arousal >= 0.0 AND baseline_arousal <= 1.0),
    baseline_dominance REAL NOT NULL DEFAULT 0.0 CHECK (baseline_dominance >= -1.0 AND baseline_dominance <= 1.0),
    baseline_energy REAL NOT NULL DEFAULT 0.5 CHECK (baseline_energy >= 0.0 AND baseline_energy <= 1.0),
    baseline_warmth REAL NOT NULL DEFAULT 0.6 CHECK (baseline_warmth >= 0.0 AND baseline_warmth <= 1.0),
    baseline_concern REAL NOT NULL DEFAULT 0.3 CHECK (baseline_concern >= 0.0 AND baseline_concern <= 1.0),
    baseline_curiosity REAL NOT NULL DEFAULT 0.3 CHECK (baseline_curiosity >= 0.0 AND baseline_curiosity <= 1.0),
    baseline_playfulness REAL NOT NULL DEFAULT 0.2 CHECK (baseline_playfulness >= 0.0 AND baseline_playfulness <= 1.0),
    baseline_attachment REAL NOT NULL DEFAULT 0.0 CHECK (baseline_attachment >= 0.0 AND baseline_attachment <= 1.0),
    baseline_frustration REAL NOT NULL DEFAULT 0.0 CHECK (baseline_frustration >= 0.0 AND baseline_frustration <= 1.0),
    baseline_uncertainty REAL NOT NULL DEFAULT 0.1 CHECK (baseline_uncertainty >= 0.0 AND baseline_uncertainty <= 1.0),

    dimension_config_json TEXT NOT NULL DEFAULT '{}',
    externalization_config_json TEXT NOT NULL DEFAULT '{}',
    llm_config_json TEXT NOT NULL DEFAULT '{}',
    context_policy_json TEXT NOT NULL DEFAULT '{}',
    clamp_policy_json TEXT NOT NULL DEFAULT '{}',

    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT,
    UNIQUE(persona_id, profile_name)
);
```

### 2.3 表：`agent_affect_states`

```sql
CREATE TABLE IF NOT EXISTS agent_affect_states (
    id TEXT PRIMARY KEY,
    persona_id TEXT NOT NULL,
    session_id TEXT,
    profile_id TEXT,

    valence REAL NOT NULL DEFAULT 0.0 CHECK (valence >= -1.0 AND valence <= 1.0),
    arousal REAL NOT NULL DEFAULT 0.2 CHECK (arousal >= 0.0 AND arousal <= 1.0),
    dominance REAL NOT NULL DEFAULT 0.0 CHECK (dominance >= -1.0 AND dominance <= 1.0),
    energy REAL NOT NULL DEFAULT 0.5 CHECK (energy >= 0.0 AND energy <= 1.0),
    warmth REAL NOT NULL DEFAULT 0.0 CHECK (warmth >= 0.0 AND warmth <= 1.0),
    concern REAL NOT NULL DEFAULT 0.0 CHECK (concern >= 0.0 AND concern <= 1.0),
    curiosity REAL NOT NULL DEFAULT 0.0 CHECK (curiosity >= 0.0 AND curiosity <= 1.0),
    playfulness REAL NOT NULL DEFAULT 0.0 CHECK (playfulness >= 0.0 AND playfulness <= 1.0),
    attachment REAL NOT NULL DEFAULT 0.0 CHECK (attachment >= 0.0 AND attachment <= 1.0),
    frustration REAL NOT NULL DEFAULT 0.0 CHECK (frustration >= 0.0 AND frustration <= 1.0),
    uncertainty REAL NOT NULL DEFAULT 0.0 CHECK (uncertainty >= 0.0 AND uncertainty <= 1.0),

    label TEXT,
    confidence REAL NOT NULL DEFAULT 0.5 CHECK (confidence >= 0.0 AND confidence <= 1.0),
    state_vector_json TEXT NOT NULL DEFAULT '{}',
    cause_summary TEXT NOT NULL DEFAULT '',
    visible_cause_summary TEXT NOT NULL DEFAULT '',
    cause_stack_json TEXT NOT NULL DEFAULT '[]',
    last_evaluation_id TEXT,

    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at TEXT,
    visibility_status TEXT NOT NULL DEFAULT 'visible' CHECK (visibility_status IN ('visible','hidden','purged')),
    searchable INTEGER NOT NULL DEFAULT 0 CHECK (searchable IN (0,1))
);
```

### 2.4 表：`agent_affect_evaluations`

```sql
CREATE TABLE IF NOT EXISTS agent_affect_evaluations (
    id TEXT PRIMARY KEY,
    persona_id TEXT NOT NULL,
    session_id TEXT,
    turn_id TEXT,

    trigger_type TEXT NOT NULL,
    custom_type TEXT,
    custom_type_desc TEXT,
    source_kind TEXT NOT NULL DEFAULT '',
    source_ref_type TEXT,
    source_ref_id TEXT,
    source_ref_hash TEXT,
    plugin_id TEXT,

    input_mode TEXT NOT NULL DEFAULT 'raw' CHECK (input_mode IN ('raw','summary','mixed','none')),
    input_text TEXT,
    input_summary TEXT,
    context_window_policy_json TEXT NOT NULL DEFAULT '{}',
    context_window_snapshot_json TEXT,

    before_state_id TEXT,
    before_state_json TEXT NOT NULL DEFAULT '{}',

    llm_provider TEXT,
    llm_model TEXT,
    llm_thinking_enabled INTEGER NOT NULL DEFAULT 0 CHECK (llm_thinking_enabled IN (0,1)),
    prompt_version TEXT NOT NULL DEFAULT 'agent_affect_v2.prompt.v1',
    prompt_hash TEXT NOT NULL DEFAULT '',
    prompt_snapshot TEXT,
    response_json TEXT,

    proposed_delta_json TEXT NOT NULL DEFAULT '{}',
    clamped_delta_json TEXT NOT NULL DEFAULT '{}',
    predicted_state_json TEXT NOT NULL DEFAULT '{}',

    cause_summary TEXT NOT NULL DEFAULT '',
    visible_cause_summary TEXT NOT NULL DEFAULT '',
    confidence REAL NOT NULL DEFAULT 0.5 CHECK (confidence >= 0.0 AND confidence <= 1.0),
    clamp_notes_json TEXT NOT NULL DEFAULT '[]',
    status TEXT NOT NULL DEFAULT 'preview' CHECK (status IN ('preview','committed','rejected','failed')),

    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    visibility_status TEXT NOT NULL DEFAULT 'visible' CHECK (visibility_status IN ('visible','hidden','purged')),
    searchable INTEGER NOT NULL DEFAULT 0 CHECK (searchable IN (0,1))
);
```

### 2.5 表：`agent_affect_events`

```sql
CREATE TABLE IF NOT EXISTS agent_affect_events (
    id TEXT PRIMARY KEY,
    persona_id TEXT NOT NULL,
    session_id TEXT,
    turn_id TEXT,

    evaluation_id TEXT,
    trigger_type TEXT NOT NULL,
    custom_type TEXT,
    plugin_id TEXT,

    before_state_id TEXT,
    after_state_id TEXT,

    proposed_delta_json TEXT NOT NULL DEFAULT '{}',
    clamped_delta_json TEXT NOT NULL DEFAULT '{}',
    committed_delta_json TEXT NOT NULL DEFAULT '{}',

    label_before TEXT,
    label_after TEXT,
    cause_summary TEXT NOT NULL DEFAULT '',
    significance REAL NOT NULL DEFAULT 0.5 CHECK (significance >= 0.0 AND significance <= 1.0),
    confidence REAL NOT NULL DEFAULT 0.5 CHECK (confidence >= 0.0 AND confidence <= 1.0),
    committed_by TEXT NOT NULL DEFAULT 'core' CHECK (committed_by IN ('core','plugin','user_debug','system')),

    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    visibility_status TEXT NOT NULL DEFAULT 'visible' CHECK (visibility_status IN ('visible','hidden','purged')),
    searchable INTEGER NOT NULL DEFAULT 0 CHECK (searchable IN (0,1))
);
```

### 2.6 表：`agent_affect_plugin_writes`

```sql
CREATE TABLE IF NOT EXISTS agent_affect_plugin_writes (
    id TEXT PRIMARY KEY,
    persona_id TEXT NOT NULL,
    session_id TEXT,
    turn_id TEXT,

    plugin_id TEXT NOT NULL,
    capability TEXT NOT NULL,
    request_kind TEXT NOT NULL CHECK (request_kind IN ('submit','write_delta','write_target','configure')),
    request_json TEXT NOT NULL DEFAULT '{}',

    accepted INTEGER NOT NULL DEFAULT 0 CHECK (accepted IN (0,1)),
    rejection_reason TEXT,
    clamp_notes_json TEXT NOT NULL DEFAULT '[]',

    evaluation_id TEXT,
    affect_event_id TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
```

### 2.7 索引

```sql
CREATE INDEX IF NOT EXISTS idx_agent_affect_profiles_persona
    ON agent_affect_profiles(persona_id, profile_name);
CREATE INDEX IF NOT EXISTS idx_agent_affect_states_current
    ON agent_affect_states(persona_id, session_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_affect_evaluations_session
    ON agent_affect_evaluations(persona_id, session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_affect_evaluations_trigger
    ON agent_affect_evaluations(persona_id, trigger_type, custom_type, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_affect_events_session
    ON agent_affect_events(persona_id, session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_affect_plugin_writes_plugin
    ON agent_affect_plugin_writes(plugin_id, created_at DESC);
```

---

## Phase 3：新增 `internal/agentaffect` 包

### 3.1 目录结构

```text
internal/agentaffect/
  service.go
  dto.go
  dimensions.go
  config.go
  store.go
  store_sqlite.go
  evaluator.go
  evaluator_llm.go
  prompt_builder.go
  response_parser.go
  transition.go
  clamp.go
  context_window.go
  prompt_block.go
  plugin_api.go
  audit.go
  errors.go
```

### 3.2 `dto.go`

核心 DTO：

```go
type MoodVector struct {
    Valence     float64 `json:"valence"`
    Arousal     float64 `json:"arousal"`
    Dominance   float64 `json:"dominance"`
    Energy      float64 `json:"energy"`
    Warmth      float64 `json:"warmth"`
    Concern     float64 `json:"concern"`
    Curiosity   float64 `json:"curiosity"`
    Playfulness float64 `json:"playfulness"`
    Attachment  float64 `json:"attachment"`
    Frustration float64 `json:"frustration"`
    Uncertainty float64 `json:"uncertainty"`
}

type MoodSnapshot struct {
    StateID             string     `json:"state_id"`
    PersonaID           string     `json:"persona_id"`
    SessionID           string     `json:"session_id,omitempty"`
    Vector              MoodVector `json:"vector"`
    Label               string     `json:"label"`
    Confidence          float64    `json:"confidence"`
    CauseSummary        string     `json:"cause_summary,omitempty"`
    VisibleCauseSummary string     `json:"visible_cause_summary,omitempty"`
    CauseStack          []CauseContributor `json:"cause_stack,omitempty"`
    UpdatedAt           time.Time  `json:"updated_at"`
}
```

Trigger：

```go
type TriggerDescriptor struct {
    TriggerType    string `json:"trigger_type"`
    CustomType     string `json:"custom_type,omitempty"`
    CustomTypeDesc string `json:"custom_type_desc,omitempty"`
    SourceKind     string `json:"source_kind,omitempty"`
    SourceRefType  string `json:"source_ref_type,omitempty"`
    SourceRefID    string `json:"source_ref_id,omitempty"`
    SourceRefHash  string `json:"source_ref_hash,omitempty"`
    PluginID       string `json:"plugin_id,omitempty"`
}
```

Input：

```go
type MoodImpactInput struct {
    Mode    string `json:"mode"` // raw | summary | mixed | none
    Text    string `json:"text,omitempty"`
    Summary string `json:"summary,omitempty"`
}
```

### 3.3 `service.go`

```go
type Service interface {
    GetCurrentMood(ctx context.Context, req GetCurrentMoodRequest) (GetCurrentMoodResponse, error)
    EvaluateMoodImpact(ctx context.Context, req EvaluateMoodImpactRequest) (EvaluateMoodImpactResponse, error)
    SubmitMoodImpact(ctx context.Context, req SubmitMoodImpactRequest) (SubmitMoodImpactResponse, error)
    ApplyMoodDelta(ctx context.Context, req ApplyMoodDeltaRequest) (ApplyMoodDeltaResponse, error)
    BuildPromptAffectBlock(ctx context.Context, req BuildPromptAffectBlockRequest) (string, error)
}
```

实现：

```go
type Runtime struct {
    cfg       config.AgentAffectConfig
    store     Store
    evaluator Evaluator
    logger    *slog.Logger
    now       func() time.Time
}
```

### 3.4 `store.go`

```go
type Store interface {
    EnsureProfile(ctx context.Context, personaID string) (AffectProfile, error)
    GetLatestState(ctx context.Context, personaID string, sessionID string) (*MoodSnapshot, error)
    InsertState(ctx context.Context, state MoodSnapshot) error
    InsertEvaluation(ctx context.Context, eval AffectEvaluationRecord) error
    MarkEvaluationCommitted(ctx context.Context, evaluationID string, afterStateID string) error
    InsertEvent(ctx context.Context, event AffectEventRecord) error
    InsertPluginWrite(ctx context.Context, write PluginWriteRecord) error
    ListRecentEvaluations(ctx context.Context, q RecentEvaluationsQuery) ([]AffectEvaluationRecord, error)
}
```

### 3.5 `evaluator.go`

```go
type Evaluator interface {
    Evaluate(ctx context.Context, req LLMEvaluationRequest) (LLMEvaluationResult, error)
}
```

只实现 `LLMEvaluator` 和 `DisabledEvaluator`。

`DisabledEvaluator` 行为：返回 no-change，不模拟情绪。

### 3.6 `clamp.go`

职责：

```text
- 每个 delta 按 per_request_delta 限幅。
- state 按 absolute bounds 限幅。
- plugin delta 可乘 plugin_delta_multiplier。
- 记录 clamp_notes。
```

---

## Phase 4：LLM Evaluator

### 4.1 LLM client 来源

MVP 推荐在 `AgentAffectService` 构造时传入：

```go
activeRuntime.EmotionSummary.Client
```

或按 `agent_affect.evaluator.provider_id/model` 通过 `AgentRuntimeService` 构造单独 client。

推荐实现顺序：

```text
v1: 复用 activeRuntime.EmotionSummary.Client，model 可由 config 指定；若为空用 summary model。
v2: 支持 evaluator.provider_id 独立 provider。
```

### 4.2 ChatRequest

```go
req := llm.ChatRequest{
    Model: model,
    System: prompt.System,
    Messages: []llm.Message{{Role: llm.RoleUser, Content: prompt.User}},
    Params: llm.RequestParams{
        MaxTokens: cfg.Evaluator.MaxOutputTokens,
        Temperature: ptr(cfg.Evaluator.Temperature),
        Stream: ptr(false),
        ReasoningEffort: cfg.Evaluator.ReasoningEffort,
        Thinking: buildThinking(cfg.Evaluator),
    },
    Stream: false,
}
```

### 4.3 输出解析

`response_parser.go`：

```text
1. 提取 JSON object。
2. json.Unmarshal 到 llmAffectResponse。
3. 校验 required fields。
4. 缺失 delta 维度按 0。
5. label 为空则由简单派生函数生成。
6. confidence 缺失按 0.5。
```

MVP 不引入 JSON Schema 库也可以；用 Go struct + 手动校验。

### 4.4 Prompt 模板

System：

```text
You are EmoAgent Affect Evaluator.

You do not write user-facing replies.
You do not produce conversation guidance.
You only estimate how the given event changes the Agent's simulated mood state.

The Agent has a persistent simulated mood vector.
You must update it based on:
- current mood
- persona affect profile
- the event text or summary
- recent affect context
- previous affect evaluations
- dimension limits

Output strict JSON only.

Important:
- attachment is allowed to be a visible emotional dimension when configured.
- frustration can exist as an internal mood dimension.
- Do not claim the Agent has biological or human emotions.
- Do not create user facts.
- Do not change memory permissions.
- Do not include hidden reasoning.
- Generate cause_summary yourself as part of the evaluation.
- Do not generate response instructions such as tone, validation_first, or advice strategy.
```

User：

```text
<persona_affect_profile>
{{profile_json}}
</persona_affect_profile>

<current_mood>
{{current_mood_json}}
</current_mood>

<trigger>
{{trigger_json}}
</trigger>

<input mode="{{input_mode}}">
{{input_text_or_summary}}
</input>

<recent_affect_context mode="{{context_mode}}">
{{recent_context_json}}
</recent_affect_context>

<previous_evaluations>
{{previous_evaluations_json}}
</previous_evaluations>

<dimension_limits>
{{limits_json}}
</dimension_limits>

Return JSON matching this schema:
{{schema_json}}
```

---

## Phase 5：App wiring

### 5.1 新增服务

文件：`internal/app/kernel.go`

```go
type Services struct {
    ...
    AgentAffect *AgentAffectService
}
```

在 `newServices` 中：

```go
services.AgentAffect = &AgentAffectService{infra: infra, agentRuntime: services.AgentRuntime, plugins: services.Plugins}
```

### 5.2 App service wrapper

新增文件：`internal/app/agent_affect_service.go`

职责：

```text
- 根据 config 构造 agentaffect.Runtime。
- 提供 Runtime()。
- 在 AgentRuntime 激活或 provider 更新后可重建 evaluator client。
- 封装 HTTP facade 调用。
```

最小结构：

```go
type AgentAffectService struct {
    infra *Infra
    agentRuntime *AgentRuntimeService
    plugins *PluginService
    mu sync.RWMutex
    runtime agentaffect.Service
}
```

### 5.3 Chat Engine 注入

文件：`internal/chat/engine.go`

```go
type AgentAffectRuntime interface {
    SubmitMoodImpact(ctx context.Context, req agentaffect.SubmitMoodImpactRequest) (agentaffect.SubmitMoodImpactResponse, error)
    BuildPromptAffectBlock(ctx context.Context, req agentaffect.BuildPromptAffectBlockRequest) (string, error)
}

type EngineConfig struct {
    ...
    AgentAffect AgentAffectRuntime
}

type Engine struct {
    ...
    agentAffect AgentAffectRuntime
}
```

文件：`internal/app/chat_service.go`

```go
AgentAffect: s.agentAffect.Runtime(),
```

需要给 `ChatService` 增加字段：

```go
agentAffect *AgentAffectService
```

并在 `newServices` 构造时传入。

---

## Phase 6：Turn integration

### 6.1 修改 `emotionPrepareStage`

在 memory retrieval 后：

```go
if engine.agentAffect != nil && tc.Inbound.Kind == turn.InboundUserMessage {
    affectResp, err := engine.agentAffect.SubmitMoodImpact(ctx, agentaffect.SubmitMoodImpactRequest{
        PersonaID: tc.Inbound.PersonaKey,
        SessionID: tc.Inbound.SessionID,
        TurnID: tc.TurnID,
        Trigger: agentaffect.TriggerDescriptor{
            TriggerType: "user_message",
            SourceKind: "turn",
            SourceRefType: "episode",
            SourceRefID: anchor.userEpisodeID,
        },
        Input: agentaffect.MoodImpactInput{Mode: "raw", Text: tc.Inbound.UserMessage.Content},
        MemoryPromptBlock: snapshot.PromptBlock,
        CommitMode: "commit_if_allowed",
    })
    if err != nil {
        tc.Diagnostics["agent_affect_error"] = err.Error()
    } else {
        tc.Diagnostics["agent_affect_snapshot"] = affectResp.Mood
        block, _ := engine.agentAffect.BuildPromptAffectBlock(ctx, agentaffect.BuildPromptAffectBlockRequest{...})
        tc.Diagnostics["agent_affect_prompt_block"] = block
    }
}
```

默认 fail-open。Agent Affect 失败不能让 turn 失败。

### 6.2 修改 `messageStage`

当前只读取 memory block：

```go
extraSystem, _ := tc.Diagnostics["memory_prompt_block"].(string)
```

改为：

```go
extraSystem := joinSystemBlocks(
    stringDiagnostic(tc, "memory_prompt_block"),
    stringDiagnostic(tc, "agent_affect_prompt_block"),
)
```

### 6.3 helper

```go
func joinSystemBlocks(blocks ...string) string {
    var out []string
    for _, block := range blocks {
        block = strings.TrimSpace(block)
        if block != "" {
            out = append(out, block)
        }
    }
    return strings.Join(out, "\n\n")
}
```

---

## Phase 7：插件能力与 Facade

### 7.1 修改 `internal/plugin/types.go`

新增 capabilities：

```go
CapabilityAgentAffectRead        Capability = "agent_affect.read"
CapabilityAgentAffectReadReason  Capability = "agent_affect.read.reason"
CapabilityAgentAffectEvaluate    Capability = "agent_affect.evaluate"
CapabilityAgentAffectSubmit      Capability = "agent_affect.submit"
CapabilityAgentAffectWriteDelta  Capability = "agent_affect.write_delta"
CapabilityAgentAffectWriteTarget Capability = "agent_affect.write_target"
CapabilityAgentAffectConfigure   Capability = "agent_affect.configure"
CapabilityAgentAffectObserve     Capability = "agent_affect.observe"
```

加入 `KnownCapability`。

### 7.2 新增 hooks

```go
HookBeforeAgentAffectEvaluate HookName = "before_agent_affect_evaluate"
HookAfterAgentAffectEvaluate  HookName = "after_agent_affect_evaluate"
HookBeforeAgentAffectCommit   HookName = "before_agent_affect_commit"
HookAfterAgentAffectCommit    HookName = "after_agent_affect_commit"
HookAgentAffectGetState       HookName = "agent_affect_get_state"
```

加入：

```go
KnownHook
knownPluginHookName
```

### 7.3 Facade 暴露方式

MVP 中外部 process plugin runner 还不完整，先支持 builtin / in-process 插件通过 Registrar 获取 facade：

```go
type Registrar struct {
    ...
    AgentAffect AgentAffectPluginAPI
}
```

如果当前 `Registrar` 构造不方便，MVP 可先只完成 capability 和 hook 定义，facade 在 `internal/agentaffect/plugin_api.go` 中实现，等下一步接进 `PluginService.Configure`。

---

## Phase 8：HTTP Debug API

### 8.1 App facade 方法

文件：`internal/app/app.go`

新增：

```go
func (a *App) GetAgentAffectCurrent(ctx context.Context, req web.AgentAffectCurrentRequest) (web.AgentAffectCurrentResponse, error)
func (a *App) EvaluateAgentAffect(ctx context.Context, req web.AgentAffectEvaluateRequest) (web.AgentAffectEvaluateResponse, error)
func (a *App) SubmitAgentAffect(ctx context.Context, req web.AgentAffectSubmitRequest) (web.AgentAffectSubmitResponse, error)
func (a *App) ApplyAgentAffectDelta(ctx context.Context, req web.AgentAffectDeltaRequest) (web.AgentAffectDeltaResponse, error)
```

### 8.2 Web handler

文件：`internal/web/api.go` 或新增 `internal/web/agent_affect.go`

新增 handlers：

```text
GET  /api/agent-affect/current
POST /api/agent-affect/evaluate
POST /api/agent-affect/submit
POST /api/agent-affect/delta
GET  /api/agent-affect/evaluations
GET  /api/agent-affect/events
POST /api/agent-affect/reset
```

### 8.3 Route 注册

文件：`internal/app/server.go`

追加：

```go
mux.HandleFunc("GET /api/agent-affect/current", api.HandleGetAgentAffectCurrent)
mux.HandleFunc("POST /api/agent-affect/evaluate", api.HandleEvaluateAgentAffect)
mux.HandleFunc("POST /api/agent-affect/submit", api.HandleSubmitAgentAffect)
mux.HandleFunc("POST /api/agent-affect/delta", api.HandleApplyAgentAffectDelta)
```

---

## Phase 9：测试

### 9.1 Unit tests

新增：

```text
internal/agentaffect/clamp_test.go
internal/agentaffect/prompt_block_test.go
internal/agentaffect/response_parser_test.go
internal/agentaffect/store_sqlite_test.go
```

测试点：

```text
- delta 超出配置后被 clamp。
- attachment absolute max 生效。
- plugin multiplier 生效。
- invalid JSON response 返回 error。
- no-change fallback 不改 state。
- BuildPromptAffectBlock 包含 mood vector、cause_summary、attachment_expression。
- plugin_safe view 包含 reason，不包含 raw input。
```

### 9.2 Storage migration test

复用现有 storage test 风格，新增断言：

```sql
SELECT name FROM sqlite_master WHERE type='table' AND name='agent_affect_states';
```

并检查 `schema_version` 最新为 20。

### 9.3 Turn integration test

修改 / 新增 `internal/chat/turn_runtime_test.go`：

```text
- mock AgentAffectRuntime 返回固定 prompt block。
- 启用 memory_stages。
- 执行 user turn。
- 断言 sendTurn 收到的 system 包含 [Agent Affect Runtime State]。
- Agent Affect error 时 turn 仍成功。
```

### 9.4 Plugin manifest test

新增：

```text
- Manifest with agent_affect.read passes validation。
- Manifest with unknown agent_affect.bad fails。
- KnownHook accepts after_agent_affect_commit。
```

### 9.5 API test

可先做 handler 层轻量测试：

```text
GET /api/agent-affect/current 返回 200 和 mood JSON。
POST /api/agent-affect/evaluate preview 不写 state。
POST /api/agent-affect/delta 写 state 并返回 clamped_delta。
```

---

## 10. 验证命令

基础：

```bash
gofmt -w internal/agentaffect internal/app internal/chat internal/config internal/plugin internal/storage internal/web
go test ./...
go build ./cmd/emoagent
```

手动 API：

```bash
go run ./cmd/emoagent -config ./config.yaml
curl "http://127.0.0.1:8080/api/agent-affect/current?persona_id=default&view=plugin_safe"
```

SQLite：

```bash
sqlite3 ./data/emo.db ".schema agent_affect_states"
sqlite3 ./data/emo.db "select version from schema_version order by version desc limit 1;"
```

---

## 11. 推荐最小 PR 切分

### PR 1：配置 + DB + package skeleton

```text
- config.AgentAffectConfig
- config.yaml 示例
- storage migration v20
- internal/agentaffect DTO / Store / clamp / prompt block
- tests: config load, migration, clamp
```

### PR 2：LLM evaluator + service

```text
- LLMEvaluator
- prompt builder
- response parser
- SubmitMoodImpact / EvaluateMoodImpact
- NoChangeFallback
- tests with fake llm.Client
```

### PR 3：Turn Pipeline 接入

```text
- EngineConfig.AgentAffect
- ChatService wiring
- emotionPrepareStage submit mood
- messageStage 拼接 affect prompt block
- tests
```

### PR 4：Plugin capabilities + facade

```text
- agent_affect.* capabilities
- hooks
- plugin write audit
- builtin example optional
- tests
```

### PR 5：HTTP debug API

```text
- web request/response DTO
- App facade methods
- server routes
- handler tests
```

---

## 12. 施工边界

必须保持：

```text
- Agent Affect 不写 MemoryCore facts / narratives / insights。
- 插件不直接写 SQLite，只通过 service/facade。
- LLM evaluator 输出必须经过 Go clamp。
- Agent Affect 失败不阻塞普通聊天。
- 默认 agent_affect.enabled=false。
- 默认 summary_enabled=false，避免额外 LLM 成本。
- 默认不保存 hidden thinking。
- 默认不把 agent_affect_* 作为 Memory Retrieval candidate。
```

暂不做：

```text
- Agent Life Timeline。
- Daily News Digest。
- Autonomous Workspace。
- Local Context Sensor。
- WebUI 完整可视化面板。
- MemoryCore 跨仓库 schema 修改。
- Python sidecar Agent Affect。
```

---

## 13. 风险与处理

| 风险 | 处理 |
|---|---|
| LLM 输出不合法 JSON | response_parser 返回 error，NoChangeFallback |
| 心情数值跳动过大 | Go clamp，记录 clamp_notes |
| 插件乱改心情 | capability + plugin_writes audit + delta multiplier |
| Prompt 暴露内部数值 | Prompt block 写明不要直接暴露；前端 debug 可显示 |
| attachment 表达过强 | externalization max_visible_intensity |
| 成本过高 | raw_window 可调；summary 默认关闭；evaluator 可 disabled |
| 与 MemoryCore 边界混乱 | v2 先落主 DB，不写 MemoryCore fact |
| 插件 hook timeout 不适合 LLM | LLM 只在 AgentAffectService 内跑，不在 hook 80ms 内跑 |

---

## 14. Definition of Done

MVP 完成标准：

```text
1. `agent_affect.enabled=false` 时系统行为不变。
2. `agent_affect.enabled=true` 且 evaluator disabled 时 GetCurrentMood 返回 baseline/no-change。
3. `SubmitMoodImpact` 可用 fake LLM 提交心情变化，写 state/evaluation/event。
4. delta 超限会被 clamp。
5. Prompt 中出现 Agent Affect Runtime State block。
6. `plugin_safe` mood 视图包含 reason，但不含 raw input。
7. Manifest 可声明 `agent_affect.read` 等 capability。
8. HTTP current/evaluate/submit/delta API 可用。
9. `go test ./...` 通过。
10. `go build ./cmd/emoagent` 通过。
```

