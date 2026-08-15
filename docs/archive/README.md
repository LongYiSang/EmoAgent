# 归档：历史快照

**这里的所有文档都是历史快照，不代表当前代码。**

它们记录的是"当时打算怎么建"和"当时为什么这么选"。绝大多数在写下之后就被实现了，此后代码继续演进，而文档停在了原地。把它们当现行设计去改代码，是这个仓库过去最容易踩的坑。

判断当前行为，请按这个顺序：

1. `docs/dev/` —— 变更配方与跨模块不变量，与代码同步，带验证命令
2. 代码本身
3. `docs/architecture/` —— 设计意图，大方向可信，细节可能滞后

## 为什么保留

因为"当初为什么这么选"只存在于这里。代码能告诉你现在是什么样，`git log` 能告诉你什么时候变的，但只有这些文档记录了当时权衡掉的方案、踩过的坑和被否决的理由。删掉它们，那部分判断就再也找不回来了。

## 内部链接按归档时原样保留

这些文件的内容一字未改，因此其中指向 `docs/todo/`、`docs/specs/` 等旧路径的引用现在是失效的。**这是有意的** —— 修改它们就等于修改历史记录。看到失效链接，把路径里的 `docs/` 换成 `docs/archive/` 通常就能找到。

## 各目录来源

| 目录 | 原路径 | 性质 |
|---|---|---|
| `todo/` | `docs/todo/` | 施工规格。目录名是"待办"，但里面几乎全是**已完成**的东西——这个命名本身就是最大的误导源 |
| `specs/` | `docs/specs/` | 功能规格，均已实现 |
| `implementation/` | `docs/implementation/` | 时点报告（v0.4 迁移的对账与总结），另含一个 v0.3 分支的 `.patch` 存档 |
| `reference/` | `docs/reference/` | 早期的 Claude Code 源码分析笔记。**已确认严重过时**：其中仍在建议"Phase 4 时把 Engine 改造成 agent loop"，而工具循环早已实现 |
| `architecture/` | `docs/architecture/` | 被取代的架构文档，逐份理由见下 |

## `architecture/` 各份的归档理由

| 文件 | 理由 |
|---|---|
| `EmoAgent_CapabilityRuntime_Architecture_v0.3.md` | 已被 Managed Local Runtime v0.4 取代 |
| `EmoAgent_CapabilityRuntime_v0.3_to_ManagedLocalRuntime_v0.4_Migration.md` | 迁移本身已完成 |
| `emoagent_plugin_runtime_architecture_v0.2.md` | 顶部自述 `Document status: implementation architecture draft`，且 `Target path` 明写指向 `plugin_runtime_v0.2.md` —— 后者才是定稿，仍在 `docs/architecture/` |
| `plugin_interface_suite.md` | Plugin Suite Phase 1 运行时契约，已被插件运行时 v0.2 取代 |
| `prompt_center_phase2_implementation_plan.md` | 含 checkbox 的施工计划体裁，工作已完成 |
