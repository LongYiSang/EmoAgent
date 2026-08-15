# EmoAgent 插件编写说明书

本文面向插件作者，描述当前仓库里的 Plugin Runtime v0.2 实际接口。代码依据主要在 `internal/plugin`、`internal/app/plugin_service.go`、`internal/tool` 和 `sdk/python`。

> **这是参考手册**（字段、capability、hook 的全集）。要动手写一个插件，先读 [`docs/dev/recipes/add-plugin.md`](dev/recipes/add-plugin.md) —— 那里有最小可用骨架，以及五个不会报错、只会让你以为哪里没装好的失败模式。

## 当前状态

插件系统分为三层：

- 管理面：安装、启用、禁用、重启、查看日志和审计，位于 `/plugins.html` 和 `/api/plugins`。
- 运行时：由 `plugins.enabled` 控制 hook/tool 是否执行。默认配置是 `false`。
- 插件自身启用态：安装后还需要启用插件，并写入 `user_grant_json` 授权。

因此，“能安装和管理插件”不等于“插件会执行”。执行 process 插件至少需要：

1. `plugins.enabled: true`。
2. `chat.turn_pipeline.enabled: true`，且 turn pipeline rollout 或 allow list 命中。
3. 插件已安装并启用。
4. `plugins.runtime.process_enabled: true`。
5. `python_toolchain.enabled: true`，并配置用户自行安装的 CPython 3.12 `python.exe` 与 `uv.exe` 绝对路径。

当前面向第三方插件的稳定路径是 Python stdio JSON-RPC 插件。`container` 只做 mount plan 校验，不执行容器。`builtin` 是仓库内 Go 插件使用的内部路径。

## 宿主插件配置

`config.yaml` 中的 `plugins` 块控制宿主管理面和运行时：

```yaml
plugins:
  enabled: false
  directories:
    - data/plugins
  builtin_enabled:
    - com.emoagent.plugins.turn-audit
    - com.emoagent.plugins.memory-context-debug
    - com.emoagent.plugins.outbound-guard
  rollout_percent: 0
  default_timeout_ms: 80
  max_timeout_ms: 1000
  fail_closed_hooks:
    - before_tool_call
    - before_memory_commit
  audit:
    enabled: true
    include_payload: false
  store:
    root_dir: data/plugins
    allow_dev_dirs: true
  runtime:
    process_enabled: true
    startup_timeout_ms: 5000
    shutdown_timeout_ms: 3000
    idle_timeout_seconds: 600
    crash_backoff_initial_seconds: 5
    crash_backoff_max_seconds: 300
    max_stderr_bytes: 262144
  installer:
    github_enabled: true
    require_signature: true
    trusted_publishers_path: config/plugin_publishers.yaml
    allow_unsigned_dev: true
  provider_gateway:
    enabled: true
    default_provider_id: ""
    default_model: ""
  admin:
    enabled: true
```

`python_toolchain` 是 managed Python runtime 的统一解释器/uv 配置：

```yaml
python_toolchain:
  enabled: true
  python_executable: C:\Python312\python.exe
  uv_executable: C:\Users\you\.local\bin\uv.exe
  required_python: "3.12"
  minimum_uv_version: "0.11.0"
  environment_root: data/python-envs
  cache_dir: data/uv-cache
  default_index: https://pypi.org/simple
  sync_timeout_seconds: 600
  use_system_certificates: true
```

插件作者通常只需要知道这些影响：

| 配置 | 影响 |
| --- | --- |
| `enabled` | 总运行时开关。为 `false` 时插件管理 API/UI 仍可用，但 hook/tool 不会运行。 |
| `default_timeout_ms` / `max_timeout_ms` | hook 默认超时和 manifest `hooks[].timeout_ms` 上限。 |
| `store.root_dir` | 安装包、state/cache/run/workspace 目录根路径。 |
| `python_toolchain.python_executable` / `python_toolchain.uv_executable` | `managed_python_process` 使用的 CPython 3.12 与 uv 绝对路径。宿主只验证和调用，不下载、不切换 Python。 |
| `python_toolchain.environment_root` / `python_toolchain.cache_dir` | Sidecar 和 managed 插件的独立 uv 环境与 uv cache 根目录。 |
| `runtime.python_executable` | 仅用于 legacy/dev `python_process` / `process` 的显式 Python 路径，不作为 managed fallback。 |
| `runtime.max_stderr_bytes` | 插件 stderr tail 保留上限。 |
| `installer.allow_unsigned_dev` | 开发本地目录/zip 可无签名安装。 |
| `installer.require_signature` | 要求签名时，非 dev unsigned 包会安装失败。 |
| `provider_gateway.*` | `provider.generate` 的全局默认 provider/model 和网关开关。 |
| `admin.enabled` | 插件管理 API/UI 开关。 |

旧配置键 `runtime.container_enabled`、`runtime.sandbox_endpoint` 和 `runtime.prefer_rootless` 仍可被解析，但会被丢弃，不会进入运行时配置、Admin API、UI、runtime status 或 supervisor 决策。未来如果实现容器运行时，需要新的可验证 backend 配置和健康检查，不能让这些旧键自动变成权限开关。

`plugins.rollout_percent` 和 `plugins.fail_closed_hooks` 当前有配置字段和校验，但本次审阅未看到它们接入 HookBus 运行行为。插件作者应以 manifest 中每个 hook 的 `failure_policy` 作为当前实际失败策略来源。

`plugins.runtime.private_python_executable` 仅作为旧配置迁移来源；新配置为空时会映射到 `python_toolchain.python_executable`。`plugins.runtime.private_python_artifact_path` / `private_python_artifact_sha256` 不再支持。`plugins.runtime.python_executable` 只用于 legacy/dev 进程运行时，不作为 managed 默认值。

## 从零跑通示例插件

以仓库自带 `sdk/python/examples/echo_plugin` 为例，最短路径是：

1. 在 `config.yaml` 中设置 `plugins.enabled: true`。
2. 确认 `chat.turn_pipeline.enabled: true`，并让当前 persona/session 命中 `chat.turn_pipeline.rollout_percent` 或 allow list。默认仓库配置里 rollout 是 100。
3. 在管理界面或配置文件中设置 Python Toolchain，并用 Probe 确认 CPython 3.12 与 uv 可用；如果使用 legacy/dev `python_process`，再显式配置 `plugins.runtime.python_executable`。
4. 重启 EmoAgent 宿主，让插件 host 和 Python runtime 配置生效。
5. 打开 `/plugins.html`，或调用 `/api/plugins/install/local` 安装 `D:\Dev\Project\Agent\EmoAgent\sdk\python\examples\echo_plugin`。
6. 启用 `com.example.echo`。UI 默认 grant `{}` 不会授予 facade capability；如果插件需要调用 Host API，请填下文启用 API 示例里的 `user_grant_json.capabilities`。
7. 发起一次聊天触发 `after_turn_end`，然后查看 `/api/plugins/com.example.echo/access-events?limit=25` 或插件页审计信息。
8. 调用插件工具时，工具名是 `plugin.com.example.echo.echo` 或 `plugin.com.example.echo.provider_ping`。

`provider_ping` 会调用宿主 ProviderGateway。示例 manifest 默认写了 `fake/fake-model`，这主要用于集成测试；真实宿主里如果没有 `fake` provider，请先只验证 hook/echo 工具，或把 manifest `provider.default_provider_id` / `default_model` 改成当前宿主已有 provider/model。

## 最小目录结构

一个 Python 插件目录至少包含：

```text
my_plugin/
  emo_plugin.yaml
  main.py
```

如果 `runtime.kind` 是 `managed_python_process`，还必须包含：

```text
my_plugin/
  pyproject.toml
  uv.lock
```

可参考 `sdk/python/examples/echo_plugin`。

当前 `sdk/python` 是仓库内源码 SDK，不是独立发布的 pip 包。仓库内插件由宿主启动时把 `sdk/python` 注入 `PYTHONPATH`；第三方独立仓库开发时，需要把 `emoagent_plugin` 放到可导入路径，或随插件包携带兼容副本。

`main.py` 最小示例：

```python
import sys

from emoagent_plugin import Plugin, hook, tool

plugin = Plugin()


@hook("after_turn_end")
async def after_turn_end(ctx):
    # 注意：ctx.log 当前是空转，不落盘。要看得到的诊断请写 stderr。
    print(f"turn ended: {ctx.turn.turn_id}", file=sys.stderr)
    await ctx.kv_set("last_turn_id", ctx.turn.turn_id)
    return {"Annotations": {"my_plugin": "observed"}}


@tool(
    "echo",
    description="Echo input through the plugin process",
    parameters={
        "type": "object",
        "properties": {"text": {"type": "string"}},
        "required": ["text"],
    },
    scope="both",
    permission="read-only",
)
async def echo(input_data, ctx):
    return {"ok": True, "text": input_data.get("text", "")}


if __name__ == "__main__":
    plugin.run_stdio()
```

stdout 只允许 JSON-RPC 协议输出。调试日志写 stderr，宿主会保留 bounded stderr tail。

## Manifest：`emo_plugin.yaml`

示例：

```yaml
schema_version: emoagent.plugin.v0.2
id: com.example.echo
name: Echo Plugin
version: 0.1.0
emoagent_version: ">=0.2.0"
runtime:
  kind: managed_python_process
  entry: main.py
access:
  tier: runtime_safe
  capabilities:
    - turn.read
    - tool.register
    - plugin.kv
    - provider.generate
hooks:
  - name: after_turn_end
    mode: observe
    failure_policy: fail_open
    priority: 100
    timeout_ms: 500
provider:
  default_provider_id: fake
  default_model: fake-model
```

Manifest 使用严格 YAML 解码，未知字段会导致安装失败。

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `schema_version` | 是 | 固定为 `emoagent.plugin.v0.2`。 |
| `id` | 是 | 插件 ID。必须匹配 `^[a-z0-9][a-z0-9.-]*[a-z0-9]$`，例如 `com.example.echo`。 |
| `name` | 是 | 展示名。 |
| `version` | 是 | 插件版本，格式为 semver，例如 `0.1.0` 或 `0.1.0-beta.1`。 |
| `emoagent_version` | 是 | 兼容范围，可写精确版本或 `^`、`~`、`>=`、`<=`、`>`、`<` 前缀范围，例如 `>=0.2.0`。 |
| `runtime.kind` | 是 | 支持值见下表。第三方插件当前推荐 `managed_python_process`。 |
| `runtime.entry` | Python 插件必填；`process` 当前启动时也需要 | 相对路径，必须干净，不能是绝对路径，不能包含 `..`。 |
| `access.tier` | 是 | 插件申请的访问等级。 |
| `access.capabilities` | 是 | 插件申请的能力列表。 |
| `hooks` | 否 | 插件要订阅的 hook 列表。 |
| `provider` | 否 | ProviderGateway 默认 provider/model 和 allowlist。 |
| `container` | 否 | 只用于 container mount plan 校验；当前不执行容器。 |
| `settings` | 否 | 声明式设置表单 schema；宿主原生渲染，保存到插件私有 KV。 |

`runtime.kind`：

| 值 | 当前用途 |
| --- | --- |
| `managed_python_process` | 当前第三方插件主路径。宿主用私有 Python runtime 加 `runtime.entry` 启动；缺失时 fail-closed，不回退系统 Python。 |
| `python_process` | legacy/dev 路径。必须显式配置 `plugins.runtime.python_executable`，按当前用户进程启动，不是 managed/private/sandbox。 |
| `process` | legacy/dev 路径。当前 supervisor 仍按 Python executable 加 `runtime.entry` 启动，非 Python runner 合约未稳定。不要把它当成通用进程 runtime。 |
| `container` | manifest 可校验，容器执行未实现。 |
| `builtin` | 仓库内 Go 插件使用。第三方包不要使用。 |

`access.tier`：

| 值 | 语义 |
| --- | --- |
| `runtime_safe` | 只接触运行时安全摘要、hash、计数、插件私有状态等。 |
| `user_context` | 申请用户上下文相关能力。 |
| `workspace` | 申请工作区相关能力。 |
| `trusted` | 最高 Manifest 访问层级，仅应由受信插件申请；它不是 Host 派生 Trust。 |

启用插件时的 `user_grant_json` 可以限制实际授权。若 grant 显式写入 `tier`，必须与 manifest tier 匹配；grant 不能用更高 tier 或信任字段升级插件权限。若 grant 显式列出 `capabilities`，插件只能使用 manifest 已声明 capability 的子集：

```json
{
  "tier": "runtime_safe",
  "capabilities": ["turn.read", "tool.register", "plugin.kv"]
}
```

当前 `user_grant_json` 主要在 process facade 调用路径强制执行。hook 注册、process 工具注册主要依据 manifest 声明和 host policy；不要把 grant 当成当前版本的全局沙箱。

插件页默认 grant 是 `{}`，含义是“不授予 facade capability”。只有显式写入 `capabilities` 数组，且该 capability 同时在 manifest 中声明时，facade 调用才会被允许。

插件页显示的 `trust_level` 是 Host 根据安装来源、签名/发布者、用户接受和宿主策略派生的代码运行信任分类。Manifest、`user_grant_json`、依赖 digest、私有 Python artifact 或插件返回值都不能自提升该字段；签名验证只说明供应链完整性/发布者匹配，不代表插件处在 OS 沙箱内。

插件页同时显示 Host 派生的 `host_api_policy`、`tool_policy` 和 `hook_policy`。`host_api_policy` 用于说明 Manifest 申请、用户 Grant 与宿主 allowlist 收敛后的 facade capability；`tool_policy` 显示第三方工具默认 `work + ask` 以及进程工具最终采用的 `host_invocation`，Host 会生成最终 Tool Spec，插件自报 `scope` / `permission` 只作为兼容 hint；`hook_policy` 显示 observe/active hook 分类和 active hook 是否被宿主策略允许。这些字段用于可解释性和重新确认，不是 OS 沙箱、文件/网络隔离或恶意插件隔离证明。

### 声明式设置表单

插件可以在 `emo_plugin.yaml` 中声明 `settings.schema`，插件详情页会用宿主原生表单渲染，保存后仍写入插件私有 KV 的 `settings` key。v1 的 `settings.key` 只能省略或写 `settings`：

```yaml
settings:
  key: settings
  schema:
    type: object
    required:
      - api_key
    properties:
      api_key:
        type: string
        title: API Key
        secret: true
      mode:
        type: string
        enum: [base, all]
        enum_titles:
          base: 实时
          all: 预报
        default: base
      enabled:
        type: boolean
        default: true
```

当前只支持根 `type: object`、一层 `properties`、`required`、`default`、`title`、`description`、`enum`、`enum_titles`、`secret`。字段类型只支持 `string`、`number`、`integer`、`boolean`；不支持数组、嵌套对象和条件逻辑。`secret: true` 只表示前端用 password input 显示，不表示加密、密钥托管或 secret vault。保存时宿主会按 schema 校验并丢弃 schema 外字段；没有 schema 的旧插件仍可通过 settings API 直接读写 JSON。

## Capabilities

Manifest 中可以声明以下能力：

| Capability | 用途 |
| --- | --- |
| `turn.read` | 读取 turn 安全视图，普通 observe hook 的基础能力。 |
| `turn.annotate` | 返回 `turn.annotate`、`llm.add_system_appendix`、`llm.add_tool_hint` patch。当前这些 patch 主要被授权和审计，未见核心阶段消费。 |
| `memory.read.safe` | 读取安全 memory view；调用 `memory.safe_context.current`；返回 memory query/block patch。 |
| `memory.candidate.submit` | 调用 `memory.candidate.submit` 或声明同名 hook。当前 process facade 返回 `queued` 占位结果。 |
| `memory.forget.request` | 调用 `memory.forget.request` 或声明同名 hook。当前 process facade 返回 `requested` 占位结果。 |
| `memory.forget.destructive` | Go 内置 facade 中用于 hard/source_redact/purge 级 forget 请求。 |
| `work.observe` | 观察 work decision。process facade 的 `work.decision.observe` 当前返回 `ok`。 |
| `work.dispatch.annotate` | 为 Work task brief 添加约束或验收提示；调用 `work.dispatch.annotate`。 |
| `approval.observe` | 观察 approval 生命周期。process facade 的 `approval.observe` 当前返回 `ok`。 |
| `outbound.decorate` | 订阅 outbound hook；返回 outbound payload/debug patch。 |
| `outbound.emit.safe_debug` | 向 outbound payload 的插件命名空间写安全调试信息。 |
| `tool.register` | 注册插件工具。 |
| `tool.observe` | 观察工具调用；返回 `tool.downgrade_permission` patch。 |
| `tool.require_approval` | 返回 `tool.require_approval` patch。 |
| `agent_affect.read` | Agent Affect 安全读取。process facade 的 `agent_affect.current` 当前是占位返回。 |
| `agent_affect.read.reason` | Agent Affect reason 读取能力，配置里可控制是否暴露原因。 |
| `agent_affect.evaluate` | Go 内置 facade 中用于情绪影响评估。 |
| `agent_affect.submit` | Go 内置 facade 中用于提交情绪影响。 |
| `agent_affect.write_delta` | Go 内置 facade 中用于写入 mood delta。 |
| `agent_affect.write_target` | trusted 插件可申请的目标写入能力。 |
| `agent_affect.configure` | Agent Affect 配置类能力。 |
| `agent_affect.observe` | 订阅 Agent Affect hook。 |
| `provider.generate` | 通过 ProviderGateway 调用宿主 LLM provider。 |
| `provider.embed` | 预留能力；当前未看到 process facade 实现。 |
| `plugin.kv` | 使用插件私有 KV。 |
| `plugin.files` | 读写插件私有 state 文件。 |
| `network.web` | 预留给 `web.search`/`web.fetch`；当前 facade 会返回未实现错误。 |
| `plugin.admin.read` | 插件管理读取能力；当前未看到 process facade 实现。 |

process 插件的 hook 注册当前以 manifest `hooks` 为准；返回 patch、facade 调用仍会按 capability 和 user grant 授权。建议 manifest 始终声明与 hook 和 patch 匹配的 capability，避免后续实现收紧时失效。

## Hooks

Hook 项字段：

| 字段 | 说明 |
| --- | --- |
| `name` | hook 名，必须是已知 hook。 |
| `mode` | `observe`、`transform` 或 `side_effect`。当前主要用于描述/排序，不自动赋予能力。 |
| `failure_policy` | `fail_open` 或 `fail_closed`。`fail_open` 下 hook 错误会被吞掉；`fail_closed` 会让 HookBus 返回错误，但是否阻断宿主流程取决于派发点是否传播该错误。 |
| `priority` | 数字越小越先执行；同优先级按 plugin id 排序。 |
| `timeout_ms` | 单次 hook 超时。不能小于 0，不能超过 `plugins.max_timeout_ms`，默认最大 1000ms。 |

当前会实际派发的 hook：

| Hook | 触发点 | Context 重点字段 |
| --- | --- | --- |
| `before_ingress_normalize` | turn normalize stage 前 | `turn` |
| `after_ingress_normalize` | turn normalize stage 后 | `turn` |
| `before_memory_prepare` | memory prepare stage 前 | `turn`, `memory` |
| `after_memory_prepare` | memory prepare stage 后 | `turn`, `memory` |
| `before_memory_retrieve` | emotion prepare stage 前 | `turn`, `memory` |
| `after_memory_retrieve` | emotion prepare stage 后 | `turn`, `memory` |
| `before_memory_commit` | memory commit stage 前 | `turn`, `memory` |
| `after_memory_commit` | memory commit stage 后 | `turn`, `memory` |
| `before_outbound` | outbound event 写出前 | `outbound` |
| `after_outbound` | outbound event 写出后 | `outbound` |
| `before_tool_call` | tool dispatcher 分类前 | `tool` |
| `after_tool_call` | tool dispatcher 执行后 | `tool` |
| `work.dispatch.annotate` | Work task brief 生成时 | `work` |
| `on_turn_error` | turn stage 失败时 | `turn`, `config.error` |
| `after_turn_end` | turn 结束后 | `turn` |
| `agent_affect_get_state` | Agent Affect 当前状态读取前 | `config.agent_affect` 可能为空 |
| `before_agent_affect_evaluate` | Agent Affect evaluate/submit 前 | `config.agent_affect` |
| `after_agent_affect_evaluate` | Agent Affect evaluate/submit 后 | `config.agent_affect` |
| `before_agent_affect_commit` | Agent Affect commit 前 | `config.agent_affect` |
| `after_agent_affect_commit` | Agent Affect commit 后 | `config.agent_affect` |

已知但当前未找到派发点的 hook：

- `memory.candidate.submit`
- `memory.forget.request`
- `on_decision_packet`
- `on_approval_requested`
- `on_approval_resolved`
- `on_approval_consumed`

这些名字能通过 manifest 校验，但插件作者不应依赖它们在当前版本被调用。

`fail_closed` 当前适合放在会传播错误的策略类 hook 上，例如 turn stage 的 `before_*`/`after_*` stage hook、`before_outbound`、`before_tool_call` 和 `work.dispatch.annotate`。`after_tool_call` 失败当前只记录 warn，不改变工具结果；`after_outbound`、`after_turn_end` 和 `on_turn_error` 当前会丢弃 HookBus 返回错误，不应用作阻断策略。

## Hook Context

Python SDK 中 `ctx` 提供：

- `ctx.envelope`
- `ctx.turn`
- `ctx.memory`
- `ctx.tool`
- `ctx.work`
- `ctx.outbound`
- `ctx.config`

`AttrView` 同时兼容 snake_case、camelCase、PascalCase 和 Go initialism 字段名，因此可写 `ctx.turn.turn_id` 读取 Go 字段 `TurnID`。

主要安全视图字段：

| View | 字段 |
| --- | --- |
| `turn` | `turn_id`, `state`, `kind`, `session_id`, `persona_key`, `request_id`, `user_content_bytes`, `user_content_hash`, `started_at` |
| `memory` | `prepared`, `retrieved`, `record_metadata`, `blocks`, `diagnostics` |
| `memory.blocks[]` | `block_type`, `summary`, `usage_guidance`, `confidence`, `node_ref` |
| `tool` | `call_id`, `name`, `agent_scope`, `required_permission`, `action`, `input_bytes`, `input_hash`, `result_status`, `result_bytes`, `result_hash` |
| `work` | `task_id`, `goal_summary`, `permission_scope`, `read_scope`, `constraint_count`, `acceptance_count`, `decision_category`, `decision_risk_level`, `approval_request_id`, `approval_status` |
| `outbound` | `type`, `turn_id`, `seq`, `content_bytes`, `content_hash`, `has_tool`, `has_reasoning`, `has_approval`, `safe` |

安全视图只提供摘要、hash、大小、计数、状态和安全 memory block，不暴露 raw prompt、raw tool output、reasoning content、SQLite、MemoryCore 或 TriviumDB 内部状态。

## Hook 返回值和 Patch

Hook 可以返回空 dict，也可以返回 `HookResult` 风格 dict：

```python
return {
    "Annotations": {"key": "value"},
    "Patches": [
        {
            "Type": "tool.require_approval",
            "Operation": "secure",
            "Value": {
                "kind": "sensitive_read",
                "reason": "plugin requested approval"
            },
            "ReasonCode": "policy"
        }
    ],
    "Events": [
        {"Type": "my_plugin.event", "Payload": {"ok": True}}
    ]
}
```

字段建议使用 Go 字段名或 lowerCamel，例如 `ReasonCode` / `reasonCode`。不要使用 `reason_code`，当前 Go 端没有 JSON tag，snake_case 不能保证被解析到对应字段。

Patch 字段：

| 字段 | 说明 |
| --- | --- |
| `Type` | patch 类型。 |
| `Operation` | `append`、`replace`、`secure`。空值按 `append` 处理。 |
| `Path` | patch 目标路径。当前只有冲突合并会使用 `replace` path。 |
| `Value` | patch 值。 |
| `ReasonCode` | 原因码，用于审计/诊断。 |

当前实际生效的 patch：

| Patch type | 需要 capability | 当前效果 |
| --- | --- | --- |
| `tool.require_approval` | `tool.require_approval` | 在 `before_tool_call` 中要求本次工具调用进入审批。`Value.kind` 可为 `destructive_write` 或 `sensitive_read`，`Value.reason` 是审批原因。 |
| `tool.downgrade_permission` | `tool.observe` | 在 `before_tool_call` 中把本次调用最大权限降到更保守级别，例如 `read-only`。 |
| `work.add_constraint_hint` | `work.dispatch.annotate` | 在 `work.dispatch.annotate` 中追加 Work 约束提示。`Value` 是字符串。 |
| `work.add_acceptance_hint` | `work.dispatch.annotate` | 在 `work.dispatch.annotate` 中追加 Work 验收提示。`Value` 是字符串。 |
| `outbound.add_payload` | `outbound.emit.safe_debug` | 在 `before_outbound` 中写入 outbound `Payload.plugins[plugin_id]`。`Value` 是对象。 |
| `outbound.emit.safe_debug` | `outbound.emit.safe_debug` | 同上，写入插件命名空间的安全调试 payload。 |

已定义但当前不应依赖其改变核心行为的 patch：

- `outbound.decorate_text`：当前明确忽略，不修改 assistant final text。
- `turn.annotate`
- `memory.add_query_hint`
- `memory.add_safe_context_block`
- `memory.suppress_context_block`
- `llm.add_system_appendix`
- `llm.add_tool_hint`

这些 patch 会经过授权、合并、审计，但当前未看到对应 turn/memory/LLM 阶段消费逻辑。

多个插件返回 `replace` 且 `Path` 相同会产生 `plugin_patch_conflict`，后来的冲突 patch 会被拒绝。

## Facade API

Python SDK 提供：

```python
await ctx.facade_call("method.name", {"param": "value"})
await ctx.provider_generate(...)
await ctx.kv_get("key")
await ctx.kv_set("key", {"value": 1})
await ctx.log("info", "message", {"field": "value"})   # 空转，不落盘；排障请写 stderr
```

Facade 调用会检查：

1. 插件是否已注册。
2. 插件是否启用。
3. manifest 是否声明所需 capability。
4. `user_grant_json` 是否允许该 tier/capability。
5. 参数是否符合当前实现的严格字段。

每次 allowed/denied 调用都会写 `plugin_access_events`，记录 method、capability、状态、请求摘要、输入/输出 hash 和耗时。

| Method | Capability | Params | Result / 状态 |
| --- | --- | --- | --- |
| `plugin.info` | 无 | `{}` | 当前插件 id/name/version/access/capabilities。 |
| `plugin.kv.get` | `plugin.kv` | `{"key": "name"}` | `{"found": true/false, "value": ...}`。 |
| `plugin.kv.set` | `plugin.kv` | `{"key": "name", "value": ...}` | `{"ok": true}`，value 必须是合法 JSON。 |
| `plugin.files.read_text` | `plugin.files` | `{"path": "notes/a.txt", "max_bytes": 4096}` | `{"content": "..."}`。路径限制在插件 state 目录内，默认/最大读取 256KiB。 |
| `plugin.files.write_text` | `plugin.files` | `{"path": "notes/a.txt", "content": "..."}` | `{"ok": true}`，内容最大 256KiB。 |
| `memory.safe_context.current` | `memory.read.safe` | `{"scope": "...", "limit": 10}` | 当前返回空 blocks/summary，占位实现。 |
| `memory.candidate.submit` | `memory.candidate.submit` | `{"candidate": {...}, "reason": "..."}` | 当前返回 `{"status": "queued"}`，占位实现。 |
| `memory.forget.request` | `memory.forget.request` | `{"query": "...", "reason": "..."}` | 当前返回 `{"status": "requested"}`，占位实现。 |
| `work.decision.observe` | `work.observe` | `{"decision_id": "...", "metadata": {...}}` | 当前返回 `{"ok": true}`。 |
| `work.dispatch.annotate` | `work.dispatch.annotate` | `{"task_id": "...", "annotation": {...}}` | 当前返回 `{"ok": true}`。 |
| `approval.observe` | `approval.observe` | `{"request_id": "...", "status": "..."}` | 当前返回 `{"ok": true}`。 |
| `agent_affect.current` | `agent_affect.read` | `{"persona_id": "...", "session_id": "...", "view": "plugin_safe"}` | 当前 process facade 返回 `{"ok": true}`，不是完整 mood DTO。 |
| `log.emit` | 无 | `{"level": "info", "message": "...", "fields": {...}}` | **空转：校验参数后直接返回 `{"ok": true}`，不落盘、不可检索。排障请写 stderr。** |
| `metric.emit` | 无 | `{"name": "...", "value": 1, "fields": {...}}` | 当前返回 `{"ok": true}`。 |
| `provider.generate` | `provider.generate` | 见下一节 | 调用宿主 provider 并记录 usage。 |
| `web.search` | `network.web` | `{"query": "...", "limit": 5}` | 预留但未实现，当前返回错误。 |
| `web.fetch` | `network.web` | `{"url": "https://..."}` | 预留但未实现，当前返回错误。 |

## ProviderGateway

插件不能读取 provider API key，也不能直接构造宿主 provider client。需要通过 `provider.generate`：

```python
resp = await ctx.provider_generate(
    purpose="summarize",
    provider_id="moonshot",  # 可省略
    model="kimi-k2.6",       # 可省略
    system="You are concise.",
    messages=[{"role": "user", "content": "ping"}],
    max_tokens=128,
    temperature=0.2,
)
```

请求字段：

| 字段 | 说明 |
| --- | --- |
| `purpose` | 用途标签，写入 provider usage。 |
| `provider_id` | 可选。若 manifest 配了 allowlist，必须在 `provider.allowed_provider_ids` 内。 |
| `model` | 可选。若 manifest 配了 allowlist，必须在 `provider.allowed_models` 内。 |
| `system` | system prompt。 |
| `messages` | `role`/`content` 消息数组；也支持宿主 `llm.Message` 的结构字段。 |
| `params` | provider-agnostic 参数，例如 `top_p`、`reasoning_effort`、`extra` 等。 |
| `max_tokens` | 最大输出 token。 |
| `temperature` | 可选温度。 |

provider/model 解析顺序：

1. 请求里的 `provider_id` / `model`。
2. manifest `provider.default_provider_id` / `provider.default_model`。
3. `plugins.provider_gateway.default_provider_id` / `default_model`。
4. 当前 active work-summary model fallback。

成功和失败都会写 `plugin_provider_usage`，包括 provider、model、purpose、token、状态、错误和耗时。

## 插件工具

用 `@tool` 注册工具：

```python
@tool(
    "echo",
    description="Echo input",
    parameters={
        "type": "object",
        "properties": {"text": {"type": "string"}},
        "required": ["text"],
    },
    scope="both",
    permission="read-only",
)
async def echo(input_data, ctx):
    return {"text": input_data["text"]}
```

字段：

| 字段 | 说明 |
| --- | --- |
| `name` | 本地工具名。宿主会注册为 `plugin.<plugin_id>.<name>`。如果传入完整 `plugin.<plugin_id>.<name>` 也可；其他 `plugin.*` 前缀会被拒绝。 |
| `description` | 给 LLM 的工具说明。 |
| `parameters` | JSON Schema。工具调用前会校验输入。 |
| `scope` | `emotion`、`work` 或 `both`。插件自报**不能提升**权限，但会决定降级结果：非 `emotion`/`both` 时该工具被强制归为 work，闲聊中不可见。 |
| `permission` | `read-only`、`workspace-write` 或 `approved-destructive`。同上，不能提升权限；非 `read-only` 时被强制归为 work。 |
| `routing_class` | `casual` 或 `work`，**缺省为 `work`**。可由 manifest 的 `tool_defaults.routing_class` 提供该插件所有工具的默认值。 |
| `invocation_policy` | `ask`、`auto` 或 `deny`。默认 `ask`；Python SDK 会序列化为进程协议字段 `invocation`。只有安全、只读、无需用户逐次确认的工具才建议使用 `auto`。 |

> **闲聊可见性由三者共同决定：** `permission=read-only` + `scope=emotion|both` + `routing_class=casual`，**三者缺一即被静默降级为 work，工具在正常聊天中永不触发且无任何报错**。这是插件开发最常踩的坑，详见 `docs/dev/recipes/add-plugin.md` 与 `docs/dev/invariants.md` 第 6 条。

插件工具仍走宿主 `tool.Dispatcher`；插件自报 `scope`/`permission` 不能提升权限：

- 未知工具、重复工具名、schema 校验失败会拒绝。
- Host 派生策略不足会拒绝。
- `invocation_policy="ask"` 会要求本次第三方插件工具调用有有效审批；`auto` 不要求逐次审批；`deny` 不注册该工具。
- `approved-destructive` 需要有效审批。
- `before_tool_call` hook 可以进一步要求审批或降级权限，但不能提升权限。

工具 handler 返回值必须 JSON 可序列化。带两个参数时第二个参数是 `ctx`；只带一个参数时只传 `input_data`。

## 安装、启用和 API

开发时可通过插件页安装本地目录，也可以直接调用：

```http
POST /api/plugins/install/local
Content-Type: application/json

{"path": "D:\\Dev\\Project\\Agent\\EmoAgent\\sdk\\python\\examples\\echo_plugin"}
```

启用：

```http
POST /api/plugins/com.example.echo/enable
Content-Type: application/json

{
  "version": "0.1.0",
  "user_grant_json": "{\"tier\":\"runtime_safe\",\"capabilities\":[\"turn.read\",\"tool.register\",\"plugin.kv\",\"provider.generate\"]}"
}
```

若详情里的 `trust_review.required=true`，先读取目标版本详情，并把 Host 返回的完整 `trust_review.acknowledgement` 原样随启用请求带回：

```http
GET /api/plugins/com.example.echo?version=0.2.0
```

```http
POST /api/plugins/com.example.echo/enable
Content-Type: application/json

{
  "version": "0.2.0",
  "user_grant_json": "{\"tier\":\"runtime_safe\",\"capabilities\":[\"turn.read\",\"provider.generate\"]}",
  "trust_acknowledgement": {
    "plugin_id": "com.example.echo",
    "version": "0.2.0",
    "package_digest": "sha256:...",
    "manifest_digest": "sha256:...",
    "signature_status": "verified",
    "publisher_id": "example",
    "default_tool_exposure": "work",
    "default_invocation_policy": "ask",
    "ack_nonce": "...",
    "ack_issued_at": "2026-06-22T10:00:00Z",
    "user_action": "enable_plugin",
    "reasons": ["capability_added:provider.generate"]
  }
}
```

`ack_nonce` 是宿主为当前进程中这一次启用确认签发的一次性值；缺失、被使用过、服务重启后失效或不再匹配当前 review 的 acknowledgement 会被拒绝，需要重新读取详情获取新的 acknowledgement。

常用管理 API：

| Method | Path | 说明 |
| --- | --- | --- |
| `GET` | `/api/plugins` | 列表。 |
| `GET` | `/api/plugins/{id}` | 详情。 |
| `POST` | `/api/plugins/install/local` | 安装本地目录或 zip，body: `{"path": "..."}`。 |
| `POST` | `/api/plugins/install/local-zip` | 兼容路由，行为同 `/api/plugins/install/local`，body: `{"path": "..."}`。 |
| `POST` | `/api/plugins/install/github-release` | 安装 GitHub release asset，body: `{"owner":"...","repo":"...","tag":"...","asset":"..."}`。 |
| `POST` | `/api/plugins/{id}/enable` | 启用。 |
| `POST` | `/api/plugins/{id}/disable` | 禁用并停止 runtime。 |
| `POST` | `/api/plugins/{id}/restart` | 重启 runtime。 |
| `GET` | `/api/plugins/{id}/status` | runtime 状态。 |
| `GET` | `/api/plugins/{id}/logs` | stderr tail。 |
| `GET` | `/api/plugins/{id}/access-events?limit=25` | facade 访问审计。 |
| `GET` | `/api/plugins/{id}/provider-usage?limit=25` | provider 使用审计。 |
| `DELETE` | `/api/plugins/{id}` | 删除安装记录。 |

`/api/plugins/{id}/status` 的 `runtime_status` 对 Python process 会包含可用性诊断字段：`runtime_kind`、`python_executable_path`、`python_executable_source` 和 `python_executable_available`。这些字段只说明宿主会从哪里启动 Python，以及该路径在启动检查时是否存在；它们不是插件权限、信任等级或安全边界。

`GET /api/plugins` 和 `GET /api/plugins/{id}` 的摘要会返回 `trust_level`、`trust_review`、`trust_acceptance`、`host_api_policy`、`tool_policy` 与 `hook_policy`。若 `trust_review.required=true`，启用请求必须带回 Host 生成的 `trust_acknowledgement`；服务端会重新计算目标版本、digest、publisher/signature、capability、active hook 与 tool policy 后再接受，不信任客户端自造的权限或 trust 字段。Host 还会为该 acknowledgement 绑定一次性 `ack_nonce`、`ack_issued_at` 和 `user_action=enable_plugin`；这些字段只证明启用请求回传了当前进程刚签发的 review，不证明真实用户身份、浏览器会话、不可抵赖或长期授权。`trust_acceptance` 是当前启用状态的最小审计 snapshot，只包含 `trust_level`、`accepted_at`、`acknowledgement_hash`、`reasons`、`default_tool_exposure` 和 `default_invocation_policy`；append-only history 会额外保存 Host 计算的 `user_grant_hash` 与 `host_policy_fingerprint`。这些 history 字段不保存原始 grant 或完整 Host policy，不授予 capability，不批准工具，不启用 hook，不是签名验证、权限提升、OS 沙箱、文件/网络隔离、Provider Key/MemoryCore 隔离、真实用户身份或不可抵赖证明；旧行为空表示该接受事件发生在记录这些指纹之前。

`managed_python_process` 还会报告 `dependency_env_dir`。该目录对应插件 store 下的 `dependencies/<plugin_id>/<version>`，只表示宿主为该插件版本准备并加入 `PYTHONPATH` 的依赖导入目录；它不是沙箱、权限来源或信任提升。

`process_guard_kind`、`process_guard_attached` 和 `process_guard_error` 是进程生命周期诊断，只说明宿主是否用 Job Object 等机制管理启动、停止、超时和进程树清理；它们不是权限、信任等级、OS 沙箱或数据隔离证明。

本地目录和本地 zip 在 `plugins.installer.allow_unsigned_dev: true` 时可无签名安装，signature status 为 `unsigned_dev`。目录或 zip 根目录必须直接包含 `emo_plugin.yaml`。zip 内路径会被校验，不能包含逃逸路径，不能包含 symlink。release/生产分发建议提供 `emo_plugin.signature.yaml` 并配置 trusted publisher。

GitHub release 安装请求示例：

```http
POST /api/plugins/install/github-release
Content-Type: application/json

{"owner":"example","repo":"emoagent-plugin-echo","tag":"v0.1.0","asset":"echo-plugin.zip"}
```

签名描述文件字段：

```yaml
plugin_id: com.example.echo
version: 0.1.0
package_digest: sha256:...
manifest_digest: sha256:...
publisher_id: example
key_id: main
signature_base64: ...
```

宿主会校验 manifest digest、可选 package digest、publisher/key 是否可信，以及 Ed25519 签名。

## 插件私有存储和运行环境

Python process 启动方式：

```text
managed_python_process: <uv environment python> <runtime.entry>
python_process/process: <plugins.runtime.python_executable> <runtime.entry>
```

`runtime.entry` 总是相对安装包目录解析。当前 `managed_python_process`、`process` 和 `python_process` 都由 Python process supervisor 启动，因此都需要一个可执行的 Python 入口文件。managed 插件的环境 Python 来自 `python_toolchain` 配置下的独立 uv env；普通运行阶段不会隐式执行 uv。

工作目录是安装后的 immutable package 目录。宿主注入环境变量：

```text
EMO_PLUGIN_ID
EMO_PLUGIN_VERSION
EMO_PLUGIN_ROOT
EMO_PLUGIN_STATE_DIR
EMO_PLUGIN_CACHE_DIR
EMO_PLUGIN_RUN_DIR
PYTHONUNBUFFERED=1
```

`managed_python_process` 不继承宿主进程的 `PYTHONPATH`。宿主只额外加入仓库 SDK 路径和 audit shim。

managed 插件包必须携带 `pyproject.toml` 与 `uv.lock`。安装阶段只校验这两个文件存在；环境同步只发生在安装、更新、配置或 Repair/Sync 阶段，由宿主执行：

```text
uv lock --check --project <plugin package>
uv sync --locked --no-dev --project <plugin package>
```

用户机器不会自动更新插件 `uv.lock`。`emo_dependencies.lock.json` 的本地 zip 依赖格式不再是 managed runtime 路径。

开发环境下，宿主还会把仓库的 `sdk/python` 加进 `PYTHONPATH`，所以示例插件可以直接 `from emoagent_plugin import ...`。

敏感环境变量会被移除：名称包含 `API_KEY`、`SECRET`、`TOKEN`、`PASSWORD` 的变量会被剔除；宿主 provider 配置里的 `api_key_env` 也会按精确名称剔除。

文件访问建议：

- 小型结构化状态：用 `ctx.kv_get` / `ctx.kv_set`。
- 文本状态文件：用 `plugin.files.read_text` / `plugin.files.write_text`，只允许相对路径，限制在插件 state 目录内。
- 临时运行文件：使用 `EMO_PLUGIN_RUN_DIR`。
- 缓存：使用 `EMO_PLUGIN_CACHE_DIR`。

Python runner 会注入 `sitecustomize` audit shim，阻止插件代码绑定 socket listener，并阻止直接打开插件 store 外部的 SQLite、MemoryCore、Trivium 路径。该 shim 是 Python-level best-effort guardrail，不是 OS 文件/网络沙箱，也不是恶意插件隔离证明。

## Go 内置插件

仓库内 Go 插件走 `BuiltinPlugin`：

```go
type BuiltinPlugin interface {
    Manifest() Manifest
    Register(context.Context, *Registrar) error
    Shutdown(context.Context) error
}
```

内置插件 manifest 使用兼容的 v0.1 `Manifest` 结构，注册时会通过 `Registrar` 检查 hook 声明和 capability。默认内置插件在 `internal/plugin/builtin.go`：

- `com.emoagent.plugins.turn-audit`
- `com.emoagent.plugins.memory-context-debug`
- `com.emoagent.plugins.outbound-guard`

第三方插件包不要使用 `builtin` runtime；这是宿主源码内扩展点。

## 当前限制和注意事项

- 默认 `plugins.enabled: false`。只启用单个插件不会让 runtime 自动执行。
- `user_grant_json` 当前主要限制 facade 调用，不会全面收窄 hook/tool 注册。
- `plugins.rollout_percent`、`plugins.fail_closed_hooks` 当前未看到运行时接入，实际 hook 失败策略看 manifest `failure_policy`。
- process 插件 stdout 必须只输出 JSON-RPC；普通日志写 stderr。
- `web.search` / `web.fetch` 是保留 facade，当前调用会报未实现。
- `container` runtime 当前不执行容器，只验证固定 mount plan。
- `outbound.decorate_text` 当前被忽略，插件不能修改最终 assistant 文本。
- `turn.annotate`、memory query/block patch、LLM appendix/tool hint patch 当前没有稳定消费路径。
- `memory.safe_context.current`、`memory.candidate.submit`、`memory.forget.request`、`agent_affect.current` 等 process facade 当前多为占位实现，不要把它们当成完整数据/写入通道。
- Host API 不提供 SQLite、MemoryCore、TriviumDB、原始 provider client 或 provider API key 直连能力；环境过滤和 Python audit observer 只降低误用风险，不是 OS 沙箱，不能承诺阻止恶意本地代码以当前用户权限直接访问文件或网络。
- 插件工具名不能覆盖宿主内置工具；重复注册会失败。
- `fail_closed` 只在派发点传播错误时阻断宿主流程。除非插件确实是安全策略类插件，且 hook 位于会传播错误的调用点，否则优先使用 `fail_open`。

## 常见问题排障

| 现象 | 优先检查 |
| --- | --- |
| 插件能安装但不执行 | `plugins.enabled` 是否为 `true`；当前 session/persona 是否命中 `chat.turn_pipeline` rollout 或 allow list；插件是否已启用。 |
| 启用后状态是 stopped 或 start failed | `/api/plugins/{id}/status` 和 `/api/plugins/{id}/logs`；确认 Python Toolchain Probe 成功，插件 uv environment 是 `ready`。 |
| Windows 上启动失败，提示找不到 Python | managed 插件检查 `python_toolchain.python_executable`、`python_toolchain.uv_executable` 和该插件环境状态；legacy/dev 插件才检查 `plugins.runtime.python_executable`。 |
| 依赖模块 import 失败 | 检查插件 `pyproject.toml` / `uv.lock` 是否包含依赖，执行 Repair/Sync 后确认环境状态为 `ready`。 |
| stdout 协议错误 | Python 插件不要向 stdout 打普通日志（stdout 被 JSON-RPC 独占）。普通日志一律写 stderr —— `/api/plugins/{id}/logs` 显示的就是 stderr tail。**不要用 `ctx.log(...)` 排障，它当前不落盘**，见 Facade API 表。 |
| 工具在聊天里永不触发，且无任何报错 | 路由三元组：`permission=read-only` + `scope=emotion` 或 `both` + `routing_class=casual`，缺一即被静默降级为 work。见 `docs/dev/recipes/add-plugin.md`。 |
| 状态是 backoff，但 `restart_count: 0`、stderr 为空 | 异常逃出工具处理函数会被记为运行时故障并降级整个插件（进程其实没崩）。工具处理函数必须有兜底 except。 |
| `import emoagent_plugin` 失败 | 仓库内运行时会注入 `sdk/python`；独立插件开发时需要把 SDK 源码放入 `PYTHONPATH` 或随包携带。 |
| 安装失败，提示 manifest decode/unknown field | `emo_plugin.yaml` 使用严格字段；删除未知字段并检查 `schema_version`、`runtime.entry`、capability、hook 名。 |
| facade 调用被拒绝 | 看 `/api/plugins/{id}/access-events?limit=25`；确认 manifest 声明了 capability，且 `user_grant_json` 没有收窄掉该 capability。 |
| `provider.generate` 失败 | 看 `/api/plugins/{id}/provider-usage?limit=25`；确认 provider/model 解析后存在且启用。示例的 `fake/fake-model` 只适合集成测试。 |
| GitHub release 安装失败 | body 必须是 JSON 对象 `owner`、`repo`、`tag`、`asset`；生产分发还要检查签名描述和 trusted publisher。 |

## 本地开发检查清单

发布或交给宿主安装前，至少检查：

1. `emo_plugin.yaml` 无未知字段，`id`、`version`、`emoagent_version` 合法。
2. `runtime.entry` 是干净相对路径，并且入口文件存在。
3. manifest 中声明了 hook、tool、facade 调用和 patch 所需 capability。
4. hook timeout 不超过宿主 `plugins.max_timeout_ms`。
5. Python 代码不会向 stdout 打印普通日志。
6. 工具参数 JSON Schema 能覆盖必填字段。
7. provider 调用有合理 `purpose`、`max_tokens`，并配置默认 provider/model 或要求调用方传入。
8. 在目标系统上确认 managed 私有 Python 可执行；legacy/dev 插件再确认 `plugins.runtime.python_executable` 是显式可执行路径。
9. 独立插件仓库确认 `emoagent_plugin` 可以被导入，或随插件包携带兼容 SDK。
10. `user_grant_json` 的 tier 与 manifest tier 匹配；如果显式列出 `capabilities`，必须是 manifest 声明的子集并覆盖插件实际 facade 调用。
11. 使用插件页或 `/api/plugins/{id}/logs` 检查 stderr；使用 access-events/provider-usage 检查 facade 和 provider 调用是否被拒绝。

仓库内示例的 legacy/dev 最小 smoke test：

```powershell
go test ./internal/plugin -run TestSDKExamplePluginInstallEnableHookToolProviderAudit -v
```

该测试会安装并启用 `sdk/python/examples/echo_plugin`，验证 hook、插件工具、ProviderGateway fake client 和审计写入。它使用显式 `plugins.runtime.python_executable`，不证明 Host-private Python artifact 或 store-private runtime。

Host-private Python runtime smoke 需要显式提供真实 artifact：

```powershell
$env:EMO_TEST_PRIVATE_PYTHON_ARTIFACT="D:\path\python-runtime.zip"
$env:EMO_TEST_PRIVATE_PYTHON_SHA256="sha256:<64 hex>"
go test ./internal/plugin -run TestRuntimeSupervisorManagedPythonStorePrivateRuntimeAuditGuardSmoke -v
```

该测试会校验 artifact、安装到 `plugins.store.root_dir/runtime/python`，再以 `managed_python_process` 启动插件；测试中故意配置无效 legacy `python_executable`，因此不能静默回退系统 Python。它还验证 `sitecustomize` audit guard 对原始 socket listener 和直接数据库访问生效。该 smoke 只证明宿主选择的 private Python 可启动并加载 guardrail，不提升插件 signature、trust、tier、capability，也不等同于容器或 OS sandbox。
