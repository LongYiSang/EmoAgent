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
