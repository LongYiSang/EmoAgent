# EmoAgent 文档导航

这个仓库的文档混着三种体裁，寿命完全不同：**设计意图**（为什么这么定，最抗老）、**施工规格**（打算怎么建，合并当天就失效）、**现状描述**（它现在是什么样，腐烂最快）。过去它们平铺在一起且无标注，导致读到一份旧施工规格会被当成现行设计。

本页按体裁标注可信度。**先看这张表再读任何文档。**

## 目录

| 位置 | 体裁 | 可信度 |
|---|---|---|
| **[`dev/`](dev/)** | 现行工作台 | **与代码同步，配方带验证命令。要改代码，从这里开始** |
| [`architecture/`](architecture/) | 设计意图 | 大方向可信，**细节可能滞后于代码** |
| [`archive/`](archive/) | 历史快照 | **不代表现在**，仅供追溯当初为什么这么选 |
| [`changelog/`](changelog/) | 变更历史 | 记录真实，但**停更于 2026-06**，此后的变更不在其中 |
| `superpowers/` | 设计对话记录 | 按日期命名的规格与计划，非现行。**本地目录，未纳入版本控制** |

另有两份独立文档留在本目录：

| 文件 | 内容 |
|---|---|
| [`plugin_development_guide.md`](plugin_development_guide.md) | 插件**参考手册**：manifest 字段、capability、hook 清单。动手写插件前请先读 [`dev/recipes/add-plugin.md`](dev/recipes/add-plugin.md) |
| [`emoagent_integration.md`](emoagent_integration.md) | 长期记忆注入 system prompt 的文本格式 |

## 要做什么，看哪里

| 你要做的事 | 去哪 |
|---|---|
| 改代码（加工具、加插件、加迁移、改前端） | [`dev/recipes/`](dev/recipes/) |
| 搞清楚某个约束能不能破 | [`dev/invariants.md`](dev/invariants.md) |
| 找某个包/表/端点在哪 | 仓库根的 `CLAUDE.md` |
| 理解双核架构为什么这么设计 | [`architecture/架构.md`](architecture/架构.md)、[`architecture/设计方案.md`](architecture/设计方案.md) |
| 查当前运行时架构 | [`architecture/EmoAgent_ManagedLocalRuntime_Architecture_v0.4.md`](architecture/EmoAgent_ManagedLocalRuntime_Architecture_v0.4.md) |
| 追溯某个已废弃方案当初的理由 | [`archive/`](archive/) |

## 上游仓库

长期记忆库 **EmoAgent-MemoryCore** 是独立仓库（`../EmoAgent-MemoryCore`），以 Go 库形式嵌入。它的文档在自己仓库里，本仓库不复制。
