# 跨模块不变量

这些约束跨越模块边界，**编译器不会替你检查**。违反它们通常不会报错，只会让系统安静地行为错误。

动代码前扫一眼，看你要碰的地方有没有被约束住。

---

## 1. Emotion 与 Work 的上下文隔离

**约束：** Work 的执行上下文（工具调用、中间产物、失败重试）绝不能进入 Emotion 的上下文。用户永远只和 Emotion 对话，Work 是不可见的。

**为什么：** 这是双核架构的立身之本。Work 跑一个任务可能产生几十轮工具调用，把这些倒进 Emotion 的上下文会瞬间挤爆 token 预算，并且让人格被工程细节污染——用户会突然听到伴侣开始念工具日志。

**执行点：** `internal/protocol/types.go` —— Emotion 与 Work 之间**只有三个结构体能穿过**：`TaskBrief`（下派）、`TaskReport`（回报）、`DecisionPacket`（升级）。Work 侧的上下文由 `internal/work/context.go` 独立组装，与 `internal/context` 的 Emotion 组装器互不相通。

**违反后果：** Emotion 上下文被工程细节淹没，人格断裂，token 预算失控。这个边界一旦破了很难修回来——加一个"顺手把工具结果也传给 Emotion"的字段很容易，之后就再也收不住了。

---

## 2. MemoryCore 是库边界

**约束：** 禁止 import `emoagent-memorycore` 的 `internal/` 包；禁止从本仓库修改 `memory.db` 的 schema。

**为什么：** MemoryCore 是独立仓库（`../EmoAgent-MemoryCore`），以 Go 库形式嵌入而非 HTTP 服务。它的公开面只有 `pkg/memorycore`。绕过公开面直接碰内部实现或数据库结构，等于把两个仓库焊死，MemoryCore 之后任何重构都会崩掉这边。

**执行点：** `internal/memoryhost/` 是唯一的接触面，其所有文件只 import `github.com/longyisang/emoagent-memorycore/pkg/memorycore`（见 `internal/memoryhost/bridge.go`、`internal/memoryhost/core_client.go`）。数据库路径由 `config/memorycore.yaml` 指定，schema 迁移归 MemoryCore 自己管。

**违反后果：** 升级 MemoryCore 时编译崩溃或数据损坏；两个仓库的演进被互相锁死。

**注：** Agent Affect 与抽取调度器在能力矩阵里标记为 `host_owned`，它们**属于本仓库**（`internal/agentaffect`、`internal/memoryhost`），不要去 MemoryCore 里找。

---

## 3. SQLite 是唯一权威存储

**约束：** `data/emo.db` 是唯一的权威数据源。sidecar 与 trivium 的产出是**可重建的提示**，不是事实。它们不可用时系统必须降级运行，而不是报错停摆。

**为什么：** 向量索引、embedding 缓存这类东西本质上是加速结构，随时可以从权威数据重建。把它们当权威，意味着一次索引损坏就等于数据丢失；而让主链路依赖它们的可用性，意味着一个可选的 Python 进程能拖垮整个对话。

**执行点：** `internal/storage/schema.go` 持有全部权威表的迁移。降级由 `internal/sidecar/spec.go` 的 `FailOpen`（默认 `true`）与 `internal/sidecar/supervisor.go` 的 `StateDegraded` / `degradeOrError` 实现。

**违反后果：** sidecar 一挂，聊天就不能用了——这正是"想用一次要先伺候一堆依赖"的摩擦来源。

---

## 4. 权限单向递进

**约束：** 权限等级严格递进 `read-only` → `workspace-write` → `approved-destructive`。Work 不能自我提权，只能通过 `DecisionPacket` 向 Emotion 升级申请。

**为什么：** 能自我提权的 agent 等于没有权限系统。人类审批必须是链路上无法绕过的一环。

**执行点：** `internal/tool/spec.go` —— `permissionLevel` 把三级映射为 0/1/2，**未知权限返回 -1（永不授权）**；`PermissionSatisfied(granted, required)` 做比较。宿主资源的写入另走 changeset 的 preview / apply / quarantine 流程（`internal/resource/`）。

**违反后果：** Work 可以静默执行破坏性操作。注意那个 `default: return -1` —— 新增权限类型时如果忘了在这里登记，行为是"永不授权"而非"默认放行"，这是有意为之的失败方向。

---

## 5. 回复分段不得切开受保护内容

**约束：** 拟人化分段回复不能把代码块、Markdown 表格、URL 从中间切断；work 模式与流式输出下整体抑制分段。

**为什么：** 分段是为了让闲聊读起来像真人在打字。但把一个代码块劈成两条消息，接收端渲染就崩了——尤其在 QQ 这类不重组消息的平台上。

**执行点：** `internal/replydelivery/protect.go` 的 `protectedRanges` 按配置计算保护区间，覆盖 `ProtectCodeBlocks` / `ProtectMarkdownTables` / `ProtectURLs`。

**违反后果：** 代码块渲染破碎、链接被截断成两半而失效。

---

## 6. 工具路由三元组

**约束：** 一个工具要在闲聊（`casual_chat`）中对 Emotion 可见，必须**同时**满足：

```
permission    = read-only
scope         = emotion 或 both
routing_class = casual
```

三者缺一，即被**静默降级**为 work。

**为什么：** 闲聊路径上暴露的工具必须是无副作用、且与人格对话相容的。任何带写入能力或面向任务的工具都应该走 Work，而不是在闲聊里被随手调用。

**执行点：**

- `internal/tool/spec.go` 的 `NormalizeRoutingClass`：一切非字面 `"casual"` 的值一律归为 `RoutingClassWork` —— **不写就是 work**
- `internal/plugin/process_adapter.go` 的 `processToolRoutingClass`：依次检查 permission、scope、routing_class，任一不满足直接返回 `RoutingClassWork`

**违反后果：** **工具在正常聊天中永不触发，且没有任何报错。** 表现只是"它好像没理我"。对一个主模式就是闲聊的陪伴 Agent，这等于插件白写了。

这条对**内置工具和插件工具是同一套规则**，两边的配方都引用此处。插件侧还有第二个声明点：manifest 的 `tool_defaults.routing_class` 提供该插件所有工具的默认值。
