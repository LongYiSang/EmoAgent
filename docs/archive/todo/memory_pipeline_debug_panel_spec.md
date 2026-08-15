# 记忆管线 Debug 面板短 Spec

日期：2026-06-01

## 背景

当前聊天页会展示模型 reasoning activity，例如“思考了 xx 秒”。调试长期记忆检索和注入时，需要在该入口附近打开一个“记忆管线”小窗，用于查看本轮回复实际注入的长期记忆文本，以及 MemoryCore 检索管线的轻量调参信息。

这个面板是本地 debug / 调参 / 排障工具，不是普通用户说明，也不是模型 reasoning 内容。

## 已确认边界

- 名称使用“记忆管线”或“记忆管线快照”，不再叫“思考抽屉”。
- 一份记忆管线快照绑定到一个 assistant turn。
- 第一版只在本轮回复完成后显示，不做流式临时面板。
- 快照持久化到最终 assistant message 的 `metadata.memory_pipeline`。
- 如果本轮没有最终 assistant message，例如 LLM error、用户中断或 approval pending，第一版不持久化快照。
- 只有 debug 配置开启时才生成和持久化快照，默认关闭。
- 允许在 debug 面板中暴露内部检索候选和过滤结果。
- 第一版不新增 WebSocket 实时事件；`stream_end` 后前端重新拉取当前 session detail 并重渲染历史，从最终 assistant message metadata 恢复按钮和小窗。

## 非目标

- 不做独立 retrieval attempt 表。
- 不做实时流式展示。
- 不从 EmoAgent 直接读取 MemoryCore 内部表或 `memory_access_events.score_breakdown_json` 拼装数据。
- 不要求完整事件级 trace，不记录所有内部 raw event。
- 不处理 README / 架构文档中旧 roadmap 与当前实现不一致的问题。

## 数据语义

### 记忆注入文本

`PromptBlock` 原文是“实际注入给 LLM 的长期记忆内容”的唯一权威表示。

前端不得根据 `MemoryContext.Blocks` 重新拼装注入区块，避免和实际 system prompt 不一致。

### 检索轨迹

检索轨迹只提供轻量调参视图：

- `query_analysis` 只展示：
  - `normalized`
  - `scores`
- 其余阶段只展示：
  - `content_summary`
  - `score`

各阶段建议拆分为：

- `anchor_recall`
- `rrf_fusion`
- `sqlite_authority_filter`
- `safe_rerank`
- `final_selection_mmr`

其中“记忆文案”统一使用 MemoryCore fact 的 `ContentSummary`。

如果某个阶段暂时拿不到 authority fact 的 `ContentSummary`，允许 `content_summary` 为空；不要为了补文案让 EmoAgent 读取 MemoryCore 内部表。MemoryCore 能在阶段内部解析到 fact / safe summary 时再填充。

各阶段 `score` 第一版只选一个最能解释该阶段排序或过滤意义的数据：

| 阶段 | `score` 语义 |
| --- | --- |
| `anchor_recall` | 同一条 fact 在 recall 阶段的最强 anchor raw score；如果该来源只有 rank 信号，则用该来源进入 RRF 前可计算的 rank contribution。 |
| `rrf_fusion` | `FusedAnchorScore`。 |
| `sqlite_authority_filter` | 通过 SQLite authority filter 后进入 rerank 前的 Go base score，即当前 `scoredFact.Score` 的 rerank 前值。 |
| `safe_rerank` | sidecar reranker 返回的 `RerankScore`；如果 reranker disabled / skipped / degraded，本阶段可以为空。 |
| `final_selection_mmr` | 最终被选入 prompt 的 selection score；MMR 循环选中的 item 使用当次 MMR objective score，raw-floor / protected item 使用 rerank 后 final score。 |

`query_analysis.scores` 的具体字段先不复用现有 `QueryAnalysisScores` 全量 Go struct。第一版优先考虑四个最能解释检索路由的分数：`rule_fit`、`anchor_readiness`、`semantic_need`、`expected_retrieval_confidence`。实现前再根据 MemoryCore trace DTO 落地情况确认是否保留这四个或扩展为全量 snake_case scores。

## 建议数据结构

Assistant message metadata 第一版结构：

```json
{
  "memory_pipeline": {
    "enabled": true,
    "prompt_block": "[长期记忆上下文：使用约束]\n...",
    "query_analysis": {
      "normalized": "用户问晚饭和偏好",
      "scores": {
        "rule_fit": 0.82,
        "anchor_readiness": 0.74,
        "semantic_need": 0.31,
        "expected_retrieval_confidence": 0.79
      }
    },
    "stages": {
      "anchor_recall": [
        { "content_summary": "用户喜欢手冲咖啡。", "score": 0.81 }
      ],
      "rrf_fusion": [
        { "content_summary": "用户喜欢手冲咖啡。", "score": 0.78 }
      ],
      "sqlite_authority_filter": [
        { "content_summary": "用户喜欢手冲咖啡。", "score": 0.78 }
      ],
      "safe_rerank": [
        { "content_summary": "用户喜欢手冲咖啡。", "score": 0.86 }
      ],
      "final_selection_mmr": [
        { "content_summary": "用户喜欢手冲咖啡。", "score": 0.86 }
      ]
    }
  }
}
```

MemoryCore 侧应提供稳定轻量 DTO，避免 EmoAgent 理解内部检索实现：

```go
type MemoryPipelineTrace struct {
	QueryAnalysis MemoryPipelineQueryAnalysis `json:"query_analysis,omitempty"`
	Stages        MemoryPipelineStages        `json:"stages,omitempty"`
}

type MemoryPipelineQueryAnalysis struct {
	Normalized string              `json:"normalized,omitempty"`
	Scores     MemoryPipelineQueryScores `json:"scores,omitempty"`
}

type MemoryPipelineQueryScores struct {
	RuleFit                     float64 `json:"rule_fit,omitempty"`
	AnchorReadiness             float64 `json:"anchor_readiness,omitempty"`
	SemanticNeed                float64 `json:"semantic_need,omitempty"`
	ExpectedRetrievalConfidence float64 `json:"expected_retrieval_confidence,omitempty"`
}

type MemoryPipelineTraceItem struct {
	ContentSummary string  `json:"content_summary,omitempty"`
	Score          float64 `json:"score"`
}
```

字段名可按现有 DTO 风格调整，但语义保持上述边界。

## 配置

主仓库建议新增配置：

```yaml
memory:
  retrieval:
    pipeline_debug: false
```

行为：

- `false`：保持当前行为，不请求轻量 trace，不保存 `metadata.memory_pipeline`。
- `true`：EmoAgent 请求 MemoryCore 生成轻量 trace，并把 `PromptBlock` 与 trace 持久化到 assistant message metadata。

MemoryCore 侧通过 `RetrievalRequest` 增加诊断级别，例如 `DiagnosticsLevel: "pipeline_summary"`。默认空值保持现状。不要放进 `RetrievalPolicy`，避免把 debug trace 开关混入检索行为策略。

## 实现切点

### MemoryCore

- 在 public DTO 的 `MemoryContext` 上增加轻量 trace 字段。
- 只在请求诊断级别为 `pipeline_summary` 时填充。
- 轻量 trace 由 MemoryCore 自己从检索过程构造，EmoAgent 不读内部表。
- `query_analysis` 只投影 `Normalized` 和 `Scores`。
- 各阶段只输出 `ContentSummary + Score`。

### EmoAgent 后端

- `config.MemoryRetrievalConfig` 增加 `PipelineDebug bool`。
- `MemoryBridge` 不再只返回 prompt string；建议返回包含 `PromptBlock` 和可选 trace 的快照结构。
- `Bridge.RetrievePromptBlock` 可以演进为新方法，旧方法保留兼容测试。
- `engine.SendMessage` 在 debug 开启且本轮成功生成 assistant message 时，把快照写入 assistant message metadata。
- 第一版不新增 WebSocket 实时事件。

### EmoAgent 前端

- 在 reasoning activity 卡片附近增加“记忆管线”按钮。
- 只有 assistant message metadata 存在 `memory_pipeline` 时显示或启用按钮。
- 点击后打开右侧小窗。
- 小窗第一部分展示 `prompt_block` 原文。
- 小窗第二部分展示轻量 trace，各阶段只渲染 `content_summary` 和 `score`。
- `stream_end` 后调用 `loadSessionDetail(currentSessionId)` 并 `renderHistory(d.messages || [])`，让刚持久化到 assistant message metadata 的 `memory_pipeline` 立即出现在当前页面。
- 历史消息通过 `/api/sessions/{id}` 返回的 message metadata 恢复同一面板。

## 验收标准

- 默认配置下，聊天行为和 message metadata 与现状兼容。
- 开启 `memory.retrieval.pipeline_debug` 后，成功的 assistant reply metadata 包含 `memory_pipeline.prompt_block`。
- `prompt_block` 与实际追加到 LLM system prompt 的长期记忆文本一致。
- `query_analysis` 只包含 `normalized` 和 `scores`。
- 各阶段 trace item 只包含 `content_summary` 和 `score`。
- `stream_end` 后不刷新浏览器也能看到本轮 assistant message 对应的“记忆管线”入口。
- 前端历史回看时可以打开同一份记忆管线快照。
- EmoAgent 没有读取 MemoryCore 内部表来拼装 trace。
