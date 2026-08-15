# EmoAgent 长期记忆提示词改造提案

## 当前实现与问题

在 EmoAgent 主仓库中，检索到的长期记忆通过 `Bridge.RetrievePromptBlock` 调用 `MemoryCore` 的 `Retrieve` 得到 `MemoryContext`，再通过 `FormatMemoryContext` 将其格式化为注入系统提示词的文本。集成文档提供的 `FormatMemoryContext` 实现为（节选）：

对每个 `MemoryContext.Block` 打印类型名并遍历其中的 `MemoryContextItem`，仅输出 `item.Summary`、`item.UsageGuidance` 以及非 `current` 的 `HistoricalStatus`【[1]】。当 `MemoryContext` 为空时返回空字符串。

这种实现简洁但存在明显不足：

- **缺乏分层结构**  
  目前直接使用底层 `BlockType` 作为标题，缺少对事实/因果/历史等类别的语义分组，无法区分“核心身份与边界”和“当前相关记忆”等不同用途。

- **丢失 rich contract 信息**  
  `MemoryContextItem` 还包含 `DoNotOverstate`、`ValidFrom/ValidTo`、`SourceRefs` 等字段【[2]】，这些信息没有体现。例如，
    - `DoNotOverstate` 用于提醒模型不应过度夸大某条记忆，现有格式化结果无法看出哪些事实需要谨慎使用；
    - `ValidFrom/ValidTo` 和 `HistoricalStatus` 可以帮助判断记忆是当前事实还是历史事件，但现有实现只在 `HistoricalStatus` 非 `current` 时附加标签；
    - `DoNotMention` 列表（`MemoryContext.DoNotMention`）指明了因疲劳、去重或预算原因不应主动提及的节点，但目前 `FormatMemoryContext` 完全忽略。

- **没有全局使用约束**  
  文档中强调“相关、必要、短、带使用约束”，但现有实现没有统一的使用指导，只在每条 item 后输出 `UsageGuidance`；也没有合并重复指导并在提示开头告知模型如何使用记忆。

- **未提供源信息与历史边界**  
  大部分事实来源于某些 episode，用户可能希望知道记忆的时间范围或来源，但现有格式未能体现。虽然不应向模型暴露内部 ID，但可以显示“（来源于几天前的对话）”等提示。

这些不足导致检索到的记忆无法体现 MemoryCore 为每条记忆附加的丰富契约，也不利于控制模型对记忆的使用。

## 改造目标

1. **设计面向 LLM 的提示格式**，在原有基础上增加统一的“长期记忆使用约束”和语义分区，使提示词更自然、更易用。格式示例：
    ```
    [长期记忆上下文：使用约束]
    - 不要主动说明“我记得”，除非用户询问来源。
    - 历史事实不能当当前事实说。
    - 低置信度记忆只可柔和使用。
    - 如有特殊 usage_guidance，则按括号提示执行。
    
    [核心身份与边界]
    - 用户喜欢咖啡 (用于偏好)
    - 用户不喜欢别人打断她的发言 (不夸大)
    
    [当前相关记忆]
    - 上次早会因为讨论太久而令人疲劳 (避免过度抱怨)
    - 朋友Alex昨天建议换个新的咖啡店
    
    [因果/历史上下文]
    - 三个月前调休导致早会延期 [historical]
    - 两年前用户还不喜欢咖啡 [superseded]
    
    [不要主动提及]
    - 会议中有人对用户表现出负面评价
    ```

2. **保留所有 rich contract 信息：**
- 对有 `DoNotOverstate=true` 的条目，在提示中提醒模型“不要夸大”；
- 合并 `UsageGuidance` 为全局指导，如果条目自带指导则在条目后括号中补充；
- 对 `HistoricalStatus` 为 `historical` 或 `superseded` 的条目，在标题或末尾增加标签，例如 `[historical]`；
- 根据 `ValidFrom/ValidTo`、`SourceRefs`，可在括号中提示时间范围（如“去年”）以加强时序感，但不暴露内部 ID；
- 将 `DoNotMention` 列表作为独立区域列出其摘要，并提示“不要主动提及”。

3. **支持过滤排除的 episode**：保留现有 `excludedEpisodeIDs` 参数，用于过滤掉带有相同 `SourceRefs.EpisodeID` 的条目，防止重复注入最近对话内容。

4. **确保提示词安全**：不得输出任何 `NodeID`、`NodeType`、`GraphActivation`、`QueryAnalysis`、`RetrievalConfidence` 等调试字段【[2]】，只呈现对话内容、长期事实和必要的历史标签。

## 实现建议

### 新增 `FormatMemoryContextForPrompt`

在 `internal/memoryhost/` 新增一个格式化函数，例如 `FormatMemoryContextForPrompt(mc *memorycore.MemoryContext, excludedEpisodeIDs ...string) string`，其步骤：

1. **空值处理**：当 `mc` 为 `nil` 或所有 `Blocks` 为空时返回空字符串。

2. **过滤条目**：
- 遍历 `mc.Blocks`，跳过 `len(block.Items)==0` 的块；
- 如果某条 `item.SourceRefs` 中的 `EpisodeID` 在 `excludedEpisodeIDs` 列表中，则忽略该条目。

3. **收集使用约束**：
- 初始化一组默认约束：
    - 不要主动说明“我记得”，除非用户询问来源；
    - 历史事实不能当成当前事实描述；
    - 低置信度（例如 `item.Confidence` 低于某阈值）或 `item.DoNotOverstate` 为 `true` 的记忆只可柔和使用；
- 遍历所有条目，将 `item.UsageGuidance` 去重后加入约束列表。
- 在后续条目中如果需要使用特殊指导，则在条目后括号注明；全局约束无需在每条后重复。

4. **划分语义分区**：根据 `block.BlockType` 建议的映射将条目分配到提示区：

| 区域名称         | 包含的 BlockType                                                                 | 说明                                                         |
| ---------------- | -------------------------------------------------------------------------------- | ------------------------------------------------------------ |
| 核心身份与边界   | `facts`、`relationship_arc_memory`                                               | 稳定的用户偏好、身份特征、关系边界；历史性的条目应带 `[historical]` 或 `[superseded]` 标签。 |
| 当前相关记忆     | `relevant_causal_memory`、`supportive_memory`、`experience_context`              | 与查询强相关或提供支持背景的事实。                           |
| 因果/历史上下文  | `historical_transition_memory`、`provenance_memory`、`premise_check_memory`      | 用于解释事件因果链、历史变迁或前提检查。                     |
| 不要主动提及     | 来自 `MemoryContext.DoNotMention`                                                | 不应主动向用户提及的记忆节点摘要。                           |

如果未来 MemoryCore 增加新的 block 类型，可通过配置追加映射。

5. **构建字符串**：
- 首先输出 `[长期记忆上下文：使用约束]`，按条目逐行列出合并后的约束；
- 按上述顺序遍历每个分区，若分区中有条目，则输出 `[区域名称]` 标题，随后每行输出 `- <item.Summary>`：
    - 若 `item.UsageGuidance` 非空且与全局约束不重复，则在条目后括号输出；
    - 若 `item.DoNotOverstate` 为真，则括号加入“不要夸大”；
    - 若 `item.HistoricalStatus != current`，在条目末尾附加 `[historical]` 或 `[superseded]` 标签；
    - 若需要提示时间范围，可根据 `item.ValidFrom/ValidTo` 或 `SourceRefs.OccurredAt` 格式化为“去年”“两个月前”等人类可读表达，但不得透露具体 Episode ID；此为可选增强。
- 最后输出 `[不要主动提及]` 区域。对每个 `MemorySuppression`，输出其 `Summary`，若 `Reason` 非空则括号说明（如“因疲劳请勿提及”）。
- 返回结果：使用 `strings.Builder` 组装并 `TrimSpace`；如果没有任何可输出内容，则返回空字符串。

### 调整 `Bridge.RetrievePromptBlock`

- 在 `Bridge.RetrievePromptBlock` 中，将调用 `FormatMemoryContext` 替换为 `FormatMemoryContextForPrompt`。保留 `excludedEpisodeIDs` 参数传递。
- 在测试中更新断言，验证返回的 prompt 中包含“长期记忆上下文：使用约束”等分区标题，并正确过滤 `excludedEpisodeIDs`。同时确保不包含任何 `NodeID`、`QueryAnalysis`、`RetrievalConfidence` 等字段。

## 兼容性考虑

- `FormatMemoryContextForPrompt` 应与现有 `FormatMemoryContext` 并行存在，并在文档中注明推荐使用新接口；旧接口保留以兼容历史。可通过特征开关选择格式化函数。
- 字符串输出仅供 LLM 系统 prompt 使用，不对外公开，因此无需实现本地化 i18n；如果将来需要支持其他语言，可通过配置替换区块标题和默认约束文本。
- 建议增加单元测试覆盖不同 `BlockType`、历史状态、`DoNotOverstate`、`DoNotMention`、`UsageGuidance` 组合场景。

________________________________________
[1] emoagent_integration.md
EmoAgent-MemoryCore/emoagent_integration.md
[2] dto_retrieval.go
EmoAgent-MemoryCore/internal/app/memorycore/dto_retrieval.go