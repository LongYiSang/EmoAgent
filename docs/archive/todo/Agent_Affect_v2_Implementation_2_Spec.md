## 结论

你这个 MVP 已经把 **Agent Affect v2 的主干打通了**：配置、SQLite v20 表、`internal/agentaffect` 运行时、LLM-first evaluator、delta clamp、Prompt Block 注入、Debug API、插件 capability/hook 都已经具备。下一阶段不建议继续扩“情绪理论”，而是先把它变成一个 **可启用、可配置、可观察、可调参、可回放、可在前端操作** 的功能。

我建议下一阶段目标定为：

```text
Agent Affect v2 Usable Configuration & Console
= 后端配置中心化
+ 前端 Agent Affect 控制台
+ 心情状态可视化
+ 评估/提交/手动 delta/重置/历史回放
+ LLM evaluator 可调试
+ prompt block 可预览
+ 插件写入可审计
```

---

## 1. 当前 MVP 的实际状态

现在后端核心已经比较完整。`agentaffect.Service` 已经有 `GetCurrentMood`、`EvaluateMoodImpact`、`SubmitMoodImpact`、`ApplyMoodDelta`、`BuildPromptAffectBlock` 五个关键入口。

Turn Pipeline 也已经接上：`emotionPrepareStage` 在记忆检索后调用 `SubmitMoodImpact`，把用户消息、memory prompt block、turn id、episode ref 提交给 Agent Affect；成功后再生成 `agent_affect_prompt_block` 放入 diagnostics。 随后 `messageStage` 会把 `memory_prompt_block` 和 `agent_affect_prompt_block` 拼成 `extraSystem`，传给 `sendTurn`。

Prompt Block 也已经有了最小版本：包含 label、confidence、updated_at、完整 mood vector、cause summary、attachment externalization，并提醒模型不要直接暴露数值。

配置层也已经有 root-level `agent_affect`，字段包括 evaluator、context、dimensions、externalization、plugin_api、limits、features、prompt。 默认配置里 Agent Affect 仍然关闭，但 evaluator、context、attachment 外显、plugin API、delta limit 和 Prompt 选项都有默认值。

Debug API 目前也已经暴露四个路由：

```text
GET  /api/agent-affect/current
POST /api/agent-affect/evaluate
POST /api/agent-affect/submit
POST /api/agent-affect/delta
```

这些路由已经注册在 HTTP server 里。 MVP 使用文档也说明了当前默认关闭、如何启用，以及四个 Debug API 的 curl 示例。

所以，下一阶段不是“从 0 开始做 Agent Affect”，而是把它从 **开发者 Debug API** 提升成 **可持续使用的 Agent 内部状态控制台**。

---

## 2. 当前最需要补的缺口

### 2.1 配置中心还没有真正接管 root `agent_affect`

这是下一步最重要的问题。

现在 `Config` 里确实有 root-level `AgentAffect config.AgentAffectConfig`。 但 `configcenter.EffectiveConfig` 目前只返回 `Memory`、`Providers`、`RuntimeSettings`、`MemoryCore`、`SidecarGeneratedConfig`、`Issues`，没有 root `AgentAffect` 字段。

更关键的是，`ApplyRuntimeSettings` 目前只支持这些 namespace：

```text
system.server
chat
memory
memory.retrieval
memory.extraction
memory.sidecar
memory.provider_bindings
memory.natural_memory
memory.retention
memory.forgetting_privacy
memory.agent_affect
```

它还没有支持 root-level `agent_affect`。

这意味着：**现在前端就算想配置 Agent Affect，也没有干净的配置中心入口。** 旧的 `memory.agent_affect` 是 MemoryCore placeholder，不应该再拿来管理 v2 runtime。

### 2.2 前端 Admin 没有 Agent Affect 页面

Admin 现在有 Providers、Agents、Personas、Chat Settings、Memory Core、Pipeline、Retrieval、Sidecar、Privacy、Retention、Diagnostics 等 tab。 `TabID` 里也没有 Agent Affect。

所以下一步应该新增：

```text
Admin → Agent Affect
```

这个页面应该同时承担：

```text
配置
当前状态查看
手动评估
手动 delta
历史回放
Prompt 预览
插件写入审计
```

### 2.3 LLM evaluator 还只是最小可用

现在 evaluator prompt 已经比较清楚：它要求模型只估计事件如何改变 Agent simulated mood，不生成用户回复、不生成 conversation guidance，只输出严格 JSON。

但目前输出 schema 还比较薄，只支持：

```json
{
  "delta": {},
  "label": "",
  "cause_summary": "",
  "visible_cause_summary": "",
  "confidence": 0.5
}
```

对应 parser 也只解析 `delta/proposed_delta`、label、cause summary 和 confidence。

下一步应该加：

```text
schema_version
context_assessment
cause_stack
attachment_expression
risk_flags
no_change_reason
```

不需要让它变复杂，但要让前端能解释“为什么变成这样”。

### 2.4 context 配置还没有真正生效

配置里已经有：

```text
raw_window
raw_keep_last_requests
raw_keep_last_tokens
include_previous_evaluations
summary_enabled
store_raw_inputs
store_prompt_snapshot
```

这些字段在配置结构里存在。 但当前 evaluator 实际上把 `MemoryPromptBlock` 放进了 `<recent_affect_context>`，并把 previous evaluations 单独传入。

也就是说，下一阶段要实现真正的：

```text
Affect Context Window Builder
```

它应该按配置组装：

```text
当前输入
最近 N 次 affect evaluations
可选原始 input_text
可选 summary
可选 memory prompt block
token cap
```

### 2.5 历史、审计、重置、Prompt Preview API 还缺

现有 API 是 Debug MVP。前端可用版本还需要：

```text
GET  /api/agent-affect/config
PUT  /api/agent-affect/config

GET  /api/agent-affect/history
GET  /api/agent-affect/events
GET  /api/agent-affect/plugin-writes

GET  /api/agent-affect/profile
PUT  /api/agent-affect/profile

POST /api/agent-affect/reset
POST /api/agent-affect/prompt-preview
POST /api/agent-affect/test-evaluator
```

尤其是 `history/events/plugin-writes`，否则前端只能看到当前状态，看不到“为什么变成这样”。

---

## 3. 建议的后端完善方案

### Phase 1：配置中心接管 Agent Affect

新增 root-level Agent Affect 配置 API：

```text
GET /api/agent-affect/config
PUT /api/agent-affect/config
```

后端对应：

```go
ConfigService.GetAgentAffectConfig(ctx)
ConfigService.UpdateAgentAffectConfig(ctx, cfg)
```

runtime setting 建议用：

```text
namespace = "agent_affect"
key       = "config"
```

同时修改：

```go
ApplyRuntimeSettings
```

增加：

```go
case "agent_affect":
    return overlayJSONSetting(&cfg.AgentAffect, setting)
```

`EffectiveConfig` 也要加：

```go
AgentAffect config.AgentAffectConfig `json:"agent_affect"`
```

这样前端能从 `/api/config/effective` 看到实际生效的 root Agent Affect，而不是误用 `memory.agent_affect`。

配置保存后要刷新运行时：

```text
Update ConfigService runtime setting
→ rebuild effective config
→ update infra.Config.AgentAffect
→ ChatService.UpdateAgentAffect(...)
```

当前 `Engine` 已经有 `UpdateAgentAffect(runtime AgentAffectRuntime)` 方法。 所以只需要在 `ChatService` 加一层：

```go
func (s *ChatService) UpdateAgentAffect() {
    if s.engine != nil && s.agentAffect != nil {
        s.engine.UpdateAgentAffect(s.agentAffect.Runtime())
    }
}
```

---

### Phase 2：Agent Affect 查询与审计 API

补以下 API：

```text
GET /api/agent-affect/history
```

参数：

```text
persona_id
session_id
limit
kind = evaluations | events | both
```

返回：

```json
{
  "evaluations": [],
  "events": []
}
```

SQL 对应：

```text
agent_affect_evaluations
agent_affect_events
```

再补：

```text
GET /api/agent-affect/plugin-writes
```

用于看插件写入历史。现在已经有 `agent_affect_plugin_writes` 表，store 里也已经能插入 plugin write 记录。

这一步能让前端做出“心情时间线”。

---

### Phase 3：Profile 与 Baseline 可配置

当前 `EnsureProfile` 如果没有 profile，会插入硬编码 baseline：

```text
arousal 0.2
energy 0.5
warmth 0.6
concern 0.3
curiosity 0.3
playfulness 0.2
attachment 0
frustration 0
uncertainty 0.1
```

这在 MVP 可以，但可配置版应该允许前端编辑：

```text
baseline mood vector
profile_name
dimension_config_json
externalization_config_json
llm_config_json
context_policy_json
clamp_policy_json
```

新增 API：

```text
GET /api/agent-affect/profile?persona_id=default
PUT /api/agent-affect/profile
```

UI 上建议叫：

```text
人格心情基线
```

这个功能很重要，因为不同 Persona 的 affect baseline 不应该一样。比如：

```text
安静陪伴型：warmth 高，playfulness 低
活泼朋友型：playfulness 高，arousal 稍高
研究助手型：curiosity 高，dominance 稍高
```

---

### Phase 4：LLM evaluator 输出升级

建议把 LLM 输出 schema 升级为：

```json
{
  "schema_version": "agent_affect.v2.evaluation.v1",
  "context_assessment": {
    "trigger_interpretation": "user_message",
    "relationship_relevance": 0.2,
    "self_life_relevance": 0.1,
    "novelty": 0.4,
    "uncertainty": 0.2
  },
  "delta": {
    "valence": 0,
    "arousal": 0,
    "dominance": 0,
    "energy": 0,
    "warmth": 0,
    "concern": 0,
    "curiosity": 0,
    "playfulness": 0,
    "attachment": 0,
    "frustration": 0,
    "uncertainty": 0
  },
  "label": "steady",
  "cause_summary": "内部原因摘要",
  "visible_cause_summary": "安全可见原因摘要",
  "cause_stack": [
    {
      "kind": "user_message",
      "summary": "用户正在共同推进 Agent Affect 配置设计。",
      "weight": 0.55
    }
  ],
  "attachment_expression": {
    "allowed": true,
    "style": "gentle_explicit",
    "reason": "关系连续性增强。"
  },
  "risk_flags": [],
  "confidence": 0.7
}
```

DTO 里已经有 `CauseContributor` 和 `MoodSnapshot.CauseStack`，但当前 parser 还没吃 `cause_stack`。 这部分可以直接补上。

---

### Phase 5：真正实现 context modes

当前配置已经有 `mode`，但建议落实成四种：

```text
none
raw_window
summary_window
mixed
```

MVP 下一阶段先做：

```text
none
raw_window
mixed
```

`summary_window` 可以先放 UI 开关但标注“未实现 / 实验中”。

实现方式：

```go
BuildAffectContextWindow(req, cfg) AffectContextWindow
```

输入：

```text
current input
recent evaluations
memory prompt block
config limits
```

输出：

```json
{
  "mode": "raw_window",
  "items": [
    {
      "kind": "evaluation",
      "input_mode": "raw",
      "input_text": "...",
      "cause_summary": "...",
      "clamped_delta": {}
    }
  ],
  "token_estimate": 1234,
  "truncated": false
}
```

还要修正 `store_raw_inputs`：

```text
store_raw_inputs=false 时，不写 input_text，只写 input_summary 或空值。
store_prompt_snapshot=false 时，不写 prompt_snapshot。
```

当前 `InsertEvaluation` 会直接写 `eval.Input.Text`。 下一阶段应该在 service 层根据配置先 sanitize record。

---

### Phase 6：Reset / Time Passage / Decay

为了前端可调试，建议加：

```text
POST /api/agent-affect/reset
POST /api/agent-affect/time-passage
```

`reset` 支持：

```json
{
  "persona_id": "default",
  "session_id": "xxx",
  "mode": "baseline | zero | custom",
  "reason": "manual reset from admin"
}
```

`time-passage` 支持：

```json
{
  "persona_id": "default",
  "session_id": "xxx",
  "elapsed_minutes": 60,
  "commit": true
}
```

先不做复杂心理学 decay，可以做简单回归 baseline：

```text
new = baseline + (current - baseline) * exp(-elapsed / half_life)
```

这会让 Agent Affect 真正有“时间性”。

---

## 4. 前端设计方案

### 4.1 新增 Admin Tab：Agent Affect

在 `web/src/admin/lib/adminData.ts` 中新增：

```ts
export type TabID = ... | 'agent-affect';

tabs.push({ id: 'agent-affect', label: 'Agent Affect' });
```

在 `AdminApp.tsx` 里新增 lazy tab：

```ts
const AgentAffectTab = lazy(() => import('./tabs/AgentAffectTab'));
```

并在 switch 里渲染。

当前 Admin tab 架构已经非常适合加这一页：AdminApp 统一管理 tab，具体页面 lazy import。

---

### 4.2 Agent Affect 页面结构

建议页面分六块。

#### A. 状态总览

显示：

```text
enabled / disabled
storage_enabled
evaluator mode
provider / model
last updated
current label
confidence
cause summary
```

显示 mood vector：

```text
valence
arousal
dominance
energy
warmth
concern
curiosity
playfulness
attachment
frustration
uncertainty
```

UI 上建议每个维度用：

```text
滑条 + 数字 + 简短解释
```

例如：

```text
attachment：亲近牵引感
frustration：内部受阻感，默认不外显
```

#### B. 配置面板

分组：

```text
基础开关
Evaluator
Context Window
Prompt Block
Externalization
Limits
Plugin API
```

其中 `limits.per_request_delta` 和 `limits.absolute` 应该用表格编辑。

#### C. 当前 Prompt Block 预览

按钮：

```text
刷新 Prompt Block
```

显示当前实际会注入 LLM 的：

```text
[Agent Affect Runtime State]
...
```

这个对调试非常关键，因为最终回复质量取决于 Prompt Block 表达是否自然。

#### D. 手动 Evaluate / Submit

输入区：

```text
trigger_type
custom_type
input mode
text / summary
commit mode
```

按钮：

```text
Preview impact
Commit impact
```

结果显示：

```text
proposed_delta
clamped_delta
predicted mood
clamp notes
cause summary
raw response json 可折叠
```

#### E. 手动 Delta / Reset

提供：

```text
Apply delta
Reset to baseline
Apply time passage
```

这能帮助你调参与复现问题。

#### F. 历史时间线

展示：

```text
evaluations
events
plugin writes
```

建议一行显示：

```text
time
trigger_type
label before → after
top deltas
cause summary
status
source plugin
```

点击展开：

```text
before_state_json
response_json
clamp_notes
prompt_hash
input summary
```

---

### 4.3 聊天页可选加一个小状态卡

不建议一上来在聊天页塞完整配置。可以只加一个可折叠小卡：

```text
当前 Agent 状态：quiet_attached_curiosity
warmth 0.64 · curiosity 0.52 · attachment 0.28
```

放在现有 `MemoryStatusPanel` 附近或 Header 中。Chat 页面现在已经有 MemoryStatusPanel 和 PipelinePanel。  Agent Affect 可以先只进 Admin，后续再加 Chat 可视化。

---

## 5. 推荐实施顺序

### 第一步：配置中心和 API

优先做：

```text
GET /api/agent-affect/config
PUT /api/agent-affect/config
```

同时：

```text
EffectiveConfig.AgentAffect
ApplyRuntimeSettings namespace="agent_affect"
ConfigService.UpdateAgentAffectConfig
ChatService.UpdateAgentAffect
```

验收：

```text
修改 agent_affect.enabled 后，不重启服务，新 turn 生效。
```

### 第二步：历史查询 API

做：

```text
GET /api/agent-affect/history
GET /api/agent-affect/plugin-writes
GET /api/agent-affect/prompt-preview
POST /api/agent-affect/reset
```

验收：

```text
前端可以显示当前 mood + 最近 30 条 evaluations/events。
```

### 第三步：前端 Agent Affect Tab

新增：

```text
web/src/admin/hooks/useAgentAffectAdmin.ts
web/src/admin/protocol/agentAffectApi.ts
web/src/admin/tabs/AgentAffectTab.tsx
```

然后接入：

```text
AdminApp.tsx
adminData.ts
styles.css
```

验收：

```text
npm --prefix web run typecheck
npm --prefix web run build
```

### 第四步：Evaluator 输出升级

做：

```text
schema_version
cause_stack
context_assessment
attachment_expression
risk_flags
```

Parser 和 store 都要接上。

验收：

```text
fake LLM 返回 cause_stack，current mood API 能看到 cause_stack，Prompt Block 可以显示 safe cause summary。
```

### 第五步：Context Window Builder

实现：

```text
none
raw_window
mixed
```

并真正遵守：

```text
raw_keep_last_requests
raw_keep_last_tokens
store_raw_inputs
store_prompt_snapshot
include_previous_evaluations
```

验收：

```text
store_raw_inputs=false 时，agent_affect_evaluations.input_text 为空。
raw_keep_last_requests=3 时，prompt snapshot/context window 最多包含 3 条历史输入。
```

### 第六步：前端调参体验

加：

```text
配置 diff
保存前 validate
测试 evaluator
clamp notes 高亮
delta 可视化条形图
历史回放
```

---

## 6. 我建议这轮不要做的内容

这一轮先不要做：

```text
Agent Life Timeline
Daily News Digest
Autonomous Workspace
屏幕/进程观察
复杂长期心理模拟
多模态情绪识别
MemoryCore agent_affect 迁移
```

原因是当前最重要的是把 Agent Affect 本体变成可用的“控制面板”。等它能稳定观测、配置、调参，再让 Life Timeline / News / Autonomy 通过 plugin API 写入心情会更稳。

---

