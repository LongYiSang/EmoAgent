# 配方：写一个插件

## 何时用

给 EmoAgent 加新能力时。**这是后续加功能的主路径** —— 除非要改的是核心链路本身，否则优先写插件而不是内置工具。

本文覆盖"从零到工具真的在闲聊里被触发"。字段全集、capability 清单、hook 列表见 [`docs/plugin_development_guide.md`](../../plugin_development_guide.md)（参考手册）。

## 锚点文件

| 锚点 | 作用 |
|---|---|
| `sdk/python/emoagent_plugin/plugin.py` | `@tool` / `@hook` 装饰器与 `Context`；`routing_class` 是 `tool()` 的关键字参数 |
| `sdk/python/examples/echo_plugin` | 仓库内最小示例 |
| `internal/plugin/process_adapter.go` | `processToolRoutingClass` —— 决定工具是否对闲聊可见的降级逻辑 |
| `internal/tool/spec.go` | `NormalizeRoutingClass` —— 非 `casual` 一律归 work |
| `internal/plugin/facade_broker.go` | facade 分发与 capability 映射 |
| `docs/dev/invariants.md` | 第 6 条：工具路由三元组 |
| https://github.com/LongYiSang/emoagent-amap-weather | 完整可用的真实插件源码 |

> 不要把已安装插件的路径（`data/plugins/packages/<id>/<version>/...`）当锚点：那是版本化的安装快照，升级即失效、卸载即消失，且 `data/` 已 gitignore，干净克隆上不存在。要指真实例子就指源码仓库。

## 最小可用骨架

### `emo_plugin.yaml`

```yaml
schema_version: emoagent.plugin.v0.2
id: com.example.my-plugin
name: My Plugin
version: 0.1.0
emoagent_version: ">=0.2.0"

runtime:
  kind: managed_python_process
  entry: main.py

access:
  tier: runtime_safe
  capabilities:
    - tool.register
    - plugin.kv        # 读插件自身设置也要它，见失败模式 5

tool_defaults:
  routing_class: casual   # 不写即为 work，工具在闲聊中永不触发

hooks: []
```

### `main.py`

```python
import sys

from emoagent_plugin import Plugin, tool

plugin = Plugin()


@tool(
    "my_tool",                      # 名字是位置参数
    description="什么时候该调用这个工具——写清触发场景，LLM 靠它决定调不调",
    parameters={
        "type": "object",
        "properties": {"query": {"type": "string"}},
        "additionalProperties": False,
    },
    scope="emotion",                # emotion 或 both，否则强制 work
    permission="read-only",         # 非 read-only 强制 work
    invocation_policy="auto",       # ask 则每次调用都要人工审批
)
async def my_tool(input_data, ctx):     # 注意顺序：input_data 在前，ctx 在后
    try:
        query = input_data.get("query") or ""
        return {"ok": True, "result": query}
    except Exception as exc:            # 见失败模式 2：异常逃逸会打进 backoff
        print(f"my_tool failed: {exc}", file=sys.stderr)
        return {"ok": False, "error": str(exc)}


if __name__ == "__main__":
    plugin.run_stdio()
```

读插件设置（设置页填的内容）：

```python
resp = await ctx.kv_get("settings")     # 返回 {"found": bool, "value": {...}}
if not isinstance(resp, dict) or not resp.get("found"):
    raise RuntimeError("plugin is not configured")
settings = resp["value"]
```

## 步骤

1. 建目录，写 `emo_plugin.yaml` 与 `main.py`（照上面骨架）
2. 插件页安装本地目录，或 `POST /api/plugins/install/local` 传 `{"path": "..."}`
3. **启用时手动勾选 grant** —— 默认是空的，见失败模式 5
4. 有设置项的话，在插件设置页填好
5. 在 WebUI **正常聊天**里说一句该触发工具的话，确认它真的被调用

## 五个隐形失败模式

这五个都**不会报错**，只会让你以为哪里没装好。按现象查：

### 1. 工具在聊天里永不触发，且无任何报错

**原因：** 路由三元组不满足，被静默降级为 work。要在闲聊中对 Emotion 可见，必须同时满足：

```
permission    = read-only
scope         = emotion 或 both
routing_class = casual
```

`NormalizeRoutingClass` 把一切非字面 `"casual"` 的值归为 work —— **不写就是 work**。详见 [`docs/dev/invariants.md`](../invariants.md) 第 6 条。

**两处声明点**，任选其一：

- manifest 的 `tool_defaults.routing_class`（作用于该插件所有工具，推荐）
- `@tool(..., routing_class="casual")`（逐个工具）

**处置：** 三个字段一起检查，缺一不可。

### 2. 状态显示 backoff，但 `restart_count: 0`、stderr 为空

**原因：** 异常逃出工具处理函数，宿主把这次 `invoke_tool` 记为运行时故障并降级**整个插件**。现象极具误导性——看起来像进程崩了，其实进程好好的。

**处置：** 工具处理函数必须有兜底 `except`，任何异常都转成结构化结果返回，不要往外抛。facade 调用失败（比如 grant 少了某个 capability）抛的就是普通 `RuntimeError`，最容易从这里逃出去。

### 3. `ctx.log` 写的日志哪儿都找不到

**原因：** `log.emit` facade 是空转 —— 校验完参数直接返回 `{"ok": true}`，不落盘、不可检索（见 `internal/plugin/facade_broker.go`）。

**处置：** **stderr 是唯一诊断通道。** `/api/plugins/{id}/logs` 显示的就是 stderr tail；stdout 被 JSON-RPC 独占，绝不能往里写普通日志。

```python
print(f"debug: {value}", file=sys.stderr)
```

### 4. 改了源码，重启却没生效

**原因：** 安装是**快照复制**到 `data/plugins/packages/<id>/<version>/`。运行的是那份副本，不是你的源码目录。

**处置：** 改完必须重装。同版本重装行为不确定，稳妥做法是**升 version 再装**。

### 5. 插件报"未配置"，但设置明明填了也保存了

**原因：** 启用时 UI 的默认 grant 是 `{}`，不授予任何 facade capability。而插件设置本身是经 `ctx.kv_get("settings")` 读的 —— 少了 `plugin.kv`，读取就失败，表现成"没配置"。

**处置：** 启用时手填 grant，确认 `plugin.kv` 在内。被拒的 facade 调用可在 `/api/plugins/{id}/access-events?limit=25` 查到。

## 验证

装好启用后：

```bash
grep -n "routing_class" <你的插件目录>/emo_plugin.yaml
```

必须有输出且值为 `casual`，否则工具不会出现在闲聊里。

然后在 WebUI **正常聊天**（不是 work 模式）里触发一次该工具，并确认：

- 工具真的被调用（回复里体现出了工具结果）
- `/api/plugins/{id}/access-events?limit=25` 里没有拒绝记录
- `/api/plugins/{id}/status` 不是 backoff
