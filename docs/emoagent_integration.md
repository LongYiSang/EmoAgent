# EmoAgent 长期记忆提示词集成

EmoAgent 通过 `Bridge.RetrievePromptBlock` 从 MemoryCore 检索 `MemoryContext`，并使用 `FormatMemoryContextForPrompt` 将长期记忆格式化为可注入 LLM system prompt 的文本。

新格式按语义区块组织记忆，并在开头合并使用约束：

```text
[长期记忆上下文：使用约束]
- 不要主动说明“我记得”，除非用户询问来源。
- 历史事实不能当当前事实说。
- 低置信度记忆只可柔和使用。
- 用于偏好回应

[核心身份与边界]
- 用户喜欢咖啡。 (用于偏好回应)

[当前相关记忆]
- 用户最近因为早会过长而疲劳。 (不要夸大)

[因果/历史上下文]
- 用户以前住在上海。 [historical]

[不要主动提及]
- 会议中有人对用户表现出负面评价。 (近期已多次使用，避免主动提及)
```

调用方式：

```go
promptBlock := memoryhost.FormatMemoryContextForPrompt(memoryContext, excludedEpisodeIDs...)
```

`excludedEpisodeIDs` 用于过滤最近对话已经提供过的 episode，避免把当前轮用户输入重复注入长期记忆。函数只输出 `Summary`、必要的使用提示、历史标签和面向 prompt 的 `DoNotMention` 原因，不输出 `NodeID`、`GraphActivation`、`QueryAnalysis`、`RetrievalConfidence` 等内部字段。

当前 `MemorySuppression` 不携带独立摘要；格式化器只会在同一 `MemoryContext.Blocks` 中能反查到对应摘要时输出“不要主动提及”条目，找不到摘要时会跳过该 suppression，避免泄露内部 ID。

旧的 `FormatMemoryContext` 保留用于兼容历史调用；新的 prompt 注入路径应使用 `FormatMemoryContextForPrompt`。

## 统一配置入口

EmoAgent 是运行时配置中心：`config.yaml` 作为首次启动 seed，Provider Center 和 runtime settings 作为运行时来源，`/api/config/effective`、`/api/config/validate`、`/api/config/issues` 用于查看生效配置、依赖问题和 auto-fix 建议。运行时保存入口是 `/api/memory/config`、`/api/memory/features` 和 `runtime_settings`；无效配置会返回 issues，不写成 active runtime setting。

MemoryCore 继续保持 standalone：它只暴露 `LoadEffectiveOptions.ProviderRegistry` / `Overrides`，不读取 EmoAgent DB，也不 import EmoAgent 包。EmoAgent 打开 MemoryCore 时会把 DB 中的 Provider Center 转成 MemoryCore `ProviderRegistry`，并把 `memory.provider_bindings.*` / runtime settings 合成 MemoryCore overrides；过渡期 `config/memorycore.yaml` 仍可作为 standalone fallback，但普通 EmoAgent 用户不应在其中重复维护 LLM provider。旧 `memory.extraction.provider` 已废弃为兼容字段，新配置应迁移到 `memory.provider_bindings.extraction`，并通过对应 binding 的可选 `max_tokens` / `thinking` 覆盖 pipeline 参数。

API key 不写入 YAML/TOML/DB 明文；Provider Center 和 generated sidecar TOML 只保存 `api_key_env`，前端只显示环境变量 present/missing 状态。Managed sidecar 由 EmoAgent 生成 `data/runtime/sidecar.generated.toml` 并只监听 loopback；`sidecar/config.toml` 仅保留为 standalone Python sidecar 示例。

## 异步记忆抽取

EmoAgent 的 chat session 是产品会话；`memory_segments` 是一个 chat session 内的记忆切片；每个 `memory_segments.memory_session_id` 对应一个底层 MemoryCore session。消息写入时只追加 MemoryCore episode 并更新 segment 的 `last_activity_at`，不会在聊天热路径里调用抽取 LLM。

抽取现在由 EmoAgent 侧的 `memory_extraction_jobs` 队列驱动：

- `Bridge.FinalizeSegment` 仍会 `EndSession` 并写入 `memory_segments.finalized_at`，但只入队 `trigger=session_end`，不再同步等待 `RunExtraction`。
- 手动固定记忆和“扫描记忆”按钮/API 会入队高优先级 `manual_pin` 或 `manual_scan` job，并立即返回 job id。
- idle scheduler 会按 `idle_after_seconds` 扫描 active/finalized segment；从未抽取、失败或新活动晚于 `last_extracted_until_at` 的 segment 会入队 `idle_detect`。
- 后台 worker claim pending job 后才同步调用 MemoryCore `RunExtraction`，并把结果写回 `memory_extraction_jobs` 与 `memory_segments.last_extracted_at / last_extracted_until_at / extraction_status`。
- `RunExtraction` 返回 `skipped_by_fingerprint` 时 job 记为 `skipped`；失败会记录脱敏错误并按指数退避重试，超过 `max_attempts` 后标记 `failed`。
- apply 成功后默认调用 MemoryCore `RunMirrorSync`。mirror/sidecar 失败只写入 job 的 degraded mirror 结果，不影响 SQLite 权威抽取成功，除非显式配置 `mirror_sync.fail_extraction_on_sync_error=true`。

可观察入口：

- `POST /api/memory/extractions`：按 `session_id`、`segment_id` 或全局 scope 立即提交抽取 job；支持 `force` 和 `mode`。
- `GET /api/memory/extractions`：查看 job 状态。
- `GET /api/memory/segments?session_id=...`：查看当前 chat session 下的 segment 抽取状态。

Web UI 的“扫描记忆”按钮使用 `POST /api/memory/extractions` 对当前 session 入队，并展示最近 segment/job 状态。触发抽取不再要求断开并重连 WebSocket；断线重连只负责恢复 chat session 和必要的 segment rollover。
