# EmoAgent External Python + uv Toolchain v0.5 — Codex 修改实施 Spec

> **Document status**: Incremental Implementation Specification  
> **Version**: 0.5  
> **Date**: 2026-06-24  
> **Target repositories**:
> - `LongYiSang/EmoAgent`
> - `LongYiSang/EmoAgent-MemoryCore`
>
> **EmoAgent baseline**: `396236a995742ee0e33f4ca5539f5d202029fdf3`  
> **MemoryCore baseline used during design**: `45ca2b7ab4218252f3cb633c66026c3e4c18d364`  
> **Execution style**: 在 Managed Local Runtime v0.4 上增量修改；不得回滚 Host Resource Broker、ChangeSet、ProcessGuard、插件信任、调用审批、Result Provenance 和运行时生命周期补丁。

---

## 0. 最终产品决策

以下决策已确定，不作为实施中的开放问题：

1. EmoAgent **完全不内置、不下载、不解压 Python Runtime**。
2. 用户自行安装 **CPython 3.12.x**，并在 EmoAgent 中选择 `python.exe` 或其所在目录。
3. v0.5 只正式支持：
   ```text
   implementation = CPython
   major = 3
   minor = 12
   patch = 任意受支持补丁版本
   architecture = 与 EmoAgent 进程架构一致
   ```
4. 用户自行安装 `uv`，并在 EmoAgent 中选择 `uv.exe`。
5. `uv` 是 Sidecar 和 Managed Python Plugin 的唯一环境创建、锁校验和依赖同步工具。
6. Sidecar 与插件共享同一个**基础 Python 解释器**，但不共享 `site-packages`：
   ```text
   data/python-envs/memory-sidecar/
   data/python-envs/plugins/<plugin-id>/<version>/
   ```
7. Sidecar 和每个插件版本都必须拥有独立 uv 环境。
8. 依赖通过网络安装，默认使用 PyPI；用户必须保证网络可用。
9. Sidecar 和市场/普通 Managed Python Plugin 必须提交 `pyproject.toml + uv.lock`。
10. 安装和修复阶段由 uv 负责；运行阶段直接启动环境中的 Python：
    ```text
    <env>\Scripts\python.exe -I -P -u ...
    ```
    正常插件调用和 Sidecar 启动不得隐式执行 `uv sync`。
11. uv 不得自动下载、选择或切换到另一套 Python。
12. 没有配置 Python/uv 时：
    - EmoAgent 核心、聊天、SQLite/FTS Memory fallback、Go 工具继续可用；
    - Managed Sidecar 和 Managed Python Plugin 不可用；
    - 不再显示“私有 Python 文件缺失”；
    - 只有用户启用了依赖 Python 的能力时才升级为 error。
13. Sidecar 不可用时继续遵循现有 `fail_open` 语义，降级到 SQLite/FTS。
14. 已启用的 Managed Python Plugin 缺少有效 Toolchain 或环境时 fail-closed，不得退回 PATH Python。
15. `process_dev` 可以显式使用开发解释器，但不得成为 managed runtime 的静默 fallback。
16. 当前 v0.4 的插件风险模型保持不变：第三方 Python 插件是用户主动信任的本地代码，不是恶意代码沙箱。

---

## 1. 当前实现基线与要替换的内容

### 1.1 必须保留

```text
internal/processguard
Managed Bash
RuntimeSupervisor 的 single-start / generation 防覆盖
idle timeout / crash backoff / quarantine
Managed Python 环境变量 allowlist
Plugin Trust / Publisher / Digest
Facade Host API Capability
Tool Work + Ask 默认策略
active hook policy
Result Provenance / data_only
Host Resource Broker / ResourceGrant / ChangeSet
startup self-test / strict self-test
```

### 1.2 当前需要替换

当前 EmoAgent 中存在：

```text
plugins.runtime.private_python_executable
plugins.runtime.private_python_artifact_path
plugins.runtime.private_python_artifact_sha256
ProvisionPrivatePythonRuntime
Private Python artifact self-test
data/plugins/runtime/python/python.exe 默认探测
emo_dependencies.lock.json + python_module_zip 自定义依赖格式
```

当前 Sidecar 中存在：

```text
默认命令 uv run python -m memorycore_sidecar.server
uv 和 Python 依赖 PATH 自动解析
Sidecar 与插件没有统一 Toolchain Authority
MemoryCore sidecar 尚需提交可重复的 uv.lock
```

### 1.3 迁移原则

```text
保留 managed_python_process 这个 RuntimeKind；
改变其解释器来源和环境准备实现；
不重新设计插件协议、JSON-RPC、Facade、Tool Registry 或 HookBus。
```

---

## 2. 目标架构

```text
Admin UI / Config Center
        ↓
PythonToolchainManager
  ├─ PythonProbe
  ├─ UVProbe
  ├─ ToolchainFingerprint
  ├─ UVEnvironmentManager
  └─ Diagnostics
        ↓
  ┌─────┴───────────────────────┐
  │                             │
SidecarEnvironment        PluginEnvironment
memory-sidecar            <id>/<version>
  │                             │
env\Scripts\python.exe     env\Scripts\python.exe
  │                             │
Sidecar Supervisor         RuntimeSupervisor
```

### 2.1 权威关系

```text
PythonToolchainManager
  = Python/uv 路径、版本、环境状态的唯一解析入口

SidecarService / PluginRuntimeSupervisor
  = Toolchain 的消费者，不再自行搜索 Python 或 uv

uv
  = 创建环境、校验 lock、同步依赖

Environment Python
  = 实际运行 Sidecar / Plugin
```

---

## 3. 配置模型

### 3.1 新增顶层配置

建议新增：

```go
type PythonToolchainConfig struct {
    Enabled               bool   `yaml:"enabled" json:"enabled"`
    PythonExecutable      string `yaml:"python_executable" json:"python_executable"`
    UVExecutable          string `yaml:"uv_executable" json:"uv_executable"`
    RequiredPython        string `yaml:"required_python" json:"required_python"`
    MinimumUVVersion      string `yaml:"minimum_uv_version" json:"minimum_uv_version"`
    EnvironmentRoot       string `yaml:"environment_root" json:"environment_root"`
    CacheDir              string `yaml:"cache_dir" json:"cache_dir"`
    DefaultIndex          string `yaml:"default_index" json:"default_index"`
    SyncTimeoutSeconds    int    `yaml:"sync_timeout_seconds" json:"sync_timeout_seconds"`
    UseSystemCertificates bool   `yaml:"use_system_certificates" json:"use_system_certificates"`
}
```

顶层：

```go
type Config struct {
    ...
    PythonToolchain PythonToolchainConfig `yaml:"python_toolchain" json:"python_toolchain"`
}
```

默认配置：

```yaml
python_toolchain:
  enabled: false
  python_executable: ""
  uv_executable: ""
  required_python: "3.12"
  minimum_uv_version: "0.11.0"
  environment_root: "data/python-envs"
  cache_dir: "data/uv-cache"
  default_index: "https://pypi.org/simple"
  sync_timeout_seconds: 600
  use_system_certificates: true
```

### 3.2 路径规则

- 配置和 Runtime Settings 最终保存绝对的 `python.exe` 与 `uv.exe`。
- UI 可以允许用户选择目录：
  ```text
  C:\Python312
  ```
  但保存前必须解析为：
  ```text
  C:\Python312\python.exe
  ```
- 路径必须是普通文件，不能只依赖 PATH。
- Managed Runtime 不允许把 `python`、`py`、`uv` 等裸命令作为最终有效路径。
- `environment_root` 和 `cache_dir` 可以是相对 Project Root 的应用数据路径。

### 3.3 Runtime Settings

在 Config Center 中增加：

```text
namespace = python_toolchain
key       = config
```

优先级沿用现有规则：

```text
DB runtime setting > seed config > defaults
```

首版修改后返回：

```json
{
  "restart_required": true
}
```

不要求在本阶段实现 Sidecar 和全部插件的安全热切换。

### 3.4 旧字段迁移

保留一版兼容解析，但不保留私有 Runtime 功能：

| 旧字段 | 迁移行为 |
|---|---|
| `plugins.runtime.private_python_executable` | 新配置为空时映射到 `python_toolchain.python_executable`，记录 deprecated warning |
| `plugins.runtime.private_python_artifact_path` | 明确报迁移错误：不再支持私有 Artifact |
| `plugins.runtime.private_python_artifact_sha256` | 同上 |
| `plugins.runtime.python_executable` | 仅用于 `process_dev` / legacy runtime |
| `python_process` | 保持 legacy 标签 |
| `managed_python_process` | 改用统一 Toolchain + uv 环境 |

迁移完成后删除文档中“应用携带私有 Python”的承诺。

---

## 4. PythonToolchainManager

建议新增：

```text
internal/pytoolchain/
  config.go
  manager.go
  probe_python.go
  probe_uv.go
  fingerprint.go
  environment.go
  environment_marker.go
  command_env.go
  diagnostics.go
  errors.go
```

### 4.1 Python Probe

执行：

```text
<python.exe> -I -P -c <host-owned-probe-script>
```

Probe 至少返回：

```json
{
  "implementation": "CPython",
  "version": "3.12.x",
  "major": 3,
  "minor": 12,
  "patch": 0,
  "architecture": "AMD64",
  "executable": "C:/.../python.exe",
  "prefix": "C:/...",
  "isolated": true,
  "safe_path": true
}
```

必须拒绝：

```text
非 CPython
非 3.12.x
架构与 EmoAgent 不匹配
Microsoft Store 空别名或无法真实运行的 launcher
相对路径
probe timeout
输出不是合法 JSON
```

不得要求用户基础 Python 预装项目依赖，也不得修改基础 Python。

### 4.2 uv Probe

执行：

```text
<uv.exe> --version
```

验证：

```text
合法 uv 版本
版本 >= minimum_uv_version
绝对路径
进程能正常退出
```

设计/CI 基线可使用 uv `0.11.24`，但产品只强制最低版本，避免用户因补丁版本不同被拒绝。

### 4.3 完整 Toolchain Probe

除版本检查外，创建临时环境验证 uv 确实能使用配置的 Python：

```text
uv venv <temp-env> --python <configured-python.exe>
```

运行 uv 时统一注入：

```text
UV_PYTHON=<configured-python.exe>
UV_PYTHON_DOWNLOADS=never
UV_PYTHON_PREFERENCE=only-system
UV_NO_MANAGED_PYTHON=1
UV_CACHE_DIR=<configured-cache-dir>
UV_NO_ENV_FILE=1
```

验证临时环境中的：

```text
<temp-env>\Scripts\python.exe
```

也是 CPython 3.12，随后清理临时环境。

### 4.4 Toolchain Fingerprint

```go
type ToolchainFingerprint struct {
    SchemaVersion  string
    PythonPath     string
    PythonVersion  string
    PythonArch     string
    PythonFileHash string
    UVPath         string
    UVVersion      string
}
```

用途：

```text
Python/uv 改变时让已有环境进入 needs_sync；
不以路径相同推断解释器内容未变。
```

Python 整个安装目录不需要全量 Hash；至少记录 `python.exe` 文件身份、版本、大小和 mtime，必要时计算 SHA-256。

---

## 5. uv 命令环境

### 5.1 固定行为

所有宿主发起的 uv 命令必须设置：

```text
UV_PYTHON=<configured python.exe>
UV_PYTHON_DOWNLOADS=never
UV_PYTHON_PREFERENCE=only-system
UV_NO_MANAGED_PYTHON=1
UV_CACHE_DIR=<configured cache>
UV_NO_ENV_FILE=1
UV_PROJECT_ENVIRONMENT=<target or temp env>
UV_DEFAULT_INDEX=<configured index>
```

`use_system_certificates=true` 时：

```text
UV_SYSTEM_CERTS=1
```

禁止：

```text
uv 自动安装 Python
从用户 PATH 选择不同 Python
修改用户基础 Python
uv pip install --system
继承 VIRTUAL_ENV / CONDA_PREFIX
```

### 5.2 网络与凭据

uv 同步进程可以继承必要的：

```text
HTTP_PROXY
HTTPS_PROXY
NO_PROXY
SSL_CERT_FILE
SSL_CERT_DIR
```

但不得继承 Provider API Key、Memory Secret 和普通插件运行时不需要的业务环境变量。

日志必须脱敏：

```text
URL userinfo
index token
Basic Auth
query token
环境变量值
```

---

## 6. UVEnvironmentManager

### 6.1 环境布局

```text
data/python-envs/
  memory-sidecar/
  plugins/
    <plugin-id>/
      <version>/

data/uv-cache/
```

环境状态：

```text
missing
needs_sync
syncing
ready
broken
```

### 6.2 Marker

每个环境根目录保存：

```text
.emoagent-python-env.json
```

内容至少包括：

```json
{
  "schema_version": "emoagent.python_env.v1",
  "owner_kind": "memory_sidecar|plugin",
  "owner_id": "...",
  "owner_version": "...",
  "toolchain_fingerprint": "...",
  "project_path_hash": "...",
  "pyproject_hash": "...",
  "uv_lock_hash": "...",
  "environment_python": "...",
  "sync_status": "ready",
  "synced_at": "..."
}
```

### 6.3 是否需要同步

满足任一条件进入 `needs_sync`：

```text
环境不存在
marker 不存在或损坏
Toolchain fingerprint 变化
pyproject.toml hash 变化
uv.lock hash 变化
环境 Python 无法 probe
上次 sync 未完成
用户点击 Repair
```

正常启动不运行 uv，只验证 marker 和环境 Python。

### 6.4 同步流程

```text
1. 对环境 key 做 singleflight / mutex。
2. 确认消费者进程已停止。
3. 在同一父目录创建临时环境。
4. 运行 uv lock --check，确认 lock 与项目一致。
5. 运行 uv sync --locked --no-dev。
6. 使用临时环境 Python 执行 owner self-test。
7. 写 marker。
8. current -> .previous。
9. temp -> current。
10. 删除 .previous。
```

命令形式：

```text
uv lock --check --project <project-dir>

uv sync
  --locked
  --no-dev
  --project <project-dir>
```

并通过 `UV_PROJECT_ENVIRONMENT=<temp-env>` 指定安装环境。

说明：

- 不使用 `--frozen` 替代 lock 一致性检查；
- `--locked` 必须在 `pyproject.toml` 与 `uv.lock` 不一致时失败；
- `uv sync` 使用 exact sync 默认行为；
- Marketplace 安装过程中禁止自动更新 `uv.lock`。

### 6.5 原子性与恢复

- 同步失败不得破坏已有 ready 环境。
- App 崩溃后清理 `.sync-*` 临时目录。
- `.previous` 存在且 current 损坏时允许恢复。
- Windows 文件占用导致 Rename 失败时，保持旧环境并返回 actionable error。
- 所有 uv 进程使用 ProcessGuard、超时、取消和 bounded logs。

---

## 7. MemoryCore Sidecar 迁移

本阶段同时修改 `EmoAgent-MemoryCore`。

### 7.1 Sidecar Python 项目

`sidecar/` 必须包含：

```text
pyproject.toml
uv.lock
memorycore_sidecar/
```

要求：

- `requires-python` 改为或保持兼容 `>=3.12,<3.13`；
- `triviumdb` 等依赖由 `uv.lock` 固定完整解析结果；
- 增加可安装的 PEP 517 build-system，使 `uv sync` 后：
  ```text
  env\Scripts\python.exe -I -P -m memorycore_sidecar.server
  ```
  可以在不依赖当前工作目录注入的情况下启动；
- `uv lock --check` 在仓库中通过；
- 测试依赖放入 dev group，生产 sync 使用 `--no-dev`。

### 7.2 Sidecar 环境

固定 owner：

```text
owner_kind = memory_sidecar
owner_id   = memorycore_sidecar
env        = data/python-envs/memory-sidecar
project    = resolved memory.sidecar.working_dir
```

### 7.3 启用流程

在用户开启 Managed Sidecar 时：

```text
Probe Toolchain
→ EnsureSidecarEnvironment
→ Sidecar self-test
→ 保存 enabled config
```

不要在每次应用启动时进行在线依赖同步。

### 7.4 启动流程

`BuildSidecarSpec` 注入：

```text
Command = [
  "<sidecar-env>/Scripts/python.exe",
  "-I", "-P", "-u",
  "-m", "memorycore_sidecar.server"
]
```

现有 `Spec.CommandArgs()` 继续附加 adapter/config/host/port。

不得继续把生产默认 Command 写成：

```text
uv run python ...
```

开发者文档可以保留手工 `uv run` 用法。

### 7.5 降级

```text
Sidecar disabled
  → 不要求 Toolchain

Sidecar enabled + Toolchain missing
  → fail_open=true: degraded to SQLite/FTS + action_required
  → fail_open=false: startup/config validation error

环境 broken
  → 不自动在线重装；提示 Repair
```

---

## 8. Managed Python Plugin 迁移

### 8.1 插件包契约

Managed Python Plugin v0.5 包含：

```text
emoagent.plugin.yaml
pyproject.toml
uv.lock
plugin source
```

建议：

```toml
[project]
requires-python = ">=3.12,<3.13"

[tool.uv]
package = false
```

插件源码仍由现有 Host-owned Bootstrap 加载，不要求插件自身成为可安装包。

### 8.2 环境路径

```text
data/python-envs/plugins/<plugin-id>/<version>/
```

以下仍保留在现有 Plugin Store：

```text
package
state
cache
run
logs
trust records
```

### 8.3 安装与启用

```text
Install:
  验证 package/manifest digest
  验证 pyproject.toml 和 uv.lock
  计算 lock digest
  不自动执行插件

Enable:
  验证 Trust acknowledgement
  Probe Toolchain
  EnsurePluginEnvironment
  运行 bootstrap self-test
  注册 Hook/Tool
```

同步失败时不启用插件。

### 8.4 运行

RuntimeSupervisor 使用：

```text
<plugin-env>/Scripts/python.exe -I -P -u <host bootstrap>
```

不再调用：

```text
PrivatePythonExecutable
store/runtime/python
custom python_module_zip env
uv run
```

保持现有：

```text
ProcessGuard
environment allowlist
single-start
generation
idle stop
backoff
quarantine
stdio JSON-RPC
Facade Capability
```

### 8.5 Lock 与信任

将当前 Dependency Lock Summary 语义迁移为：

```text
uv_lock_digest
pyproject_digest
environment_fingerprint
```

以下变化要求重新同步：

```text
uv.lock
pyproject.toml
Python fingerprint
```

以下变化继续触发 Trust Review：

```text
依赖 lock digest 改变
发布者改变
签名改变
Capability 扩张
active hook 增加
Work -> Emotion exposure
ask -> auto
```

### 8.6 旧自定义依赖格式

当前生态尚未正式发布，因此 v0.5 不继续扩展：

```text
emo_dependencies.lock.json
python_module_zip
```

迁移行为：

```text
已安装旧插件：
  标记 dependency_format=legacy
  不自动转换
  管理页提示重新打包为 pyproject.toml + uv.lock

无依赖的开发插件：
  也应补齐最小 pyproject.toml + uv.lock
```

删除 `dependency_provisioner.go` 前，先迁移 SDK 示例、测试插件和文档。

---

## 9. Admin API 与 UI

### 9.1 页面

新增“Python 工具链”管理区域：

```text
Python 3.12
  路径
  版本
  实现
  架构
  Probe 状态

uv
  路径
  版本
  最低版本
  Probe 状态

环境
  Sidecar
  插件环境列表
  lock hash
  status
  last sync
  last error
```

### 9.2 操作

```text
自动检测候选
选择 Python 目录
选择 python.exe
选择 uv.exe
验证
保存
准备/修复 Sidecar 环境
准备/修复插件环境
清理废弃环境
查看脱敏同步日志
```

自动检测只能生成候选；用户确认后必须保存绝对路径。

### 9.3 API 建议

```text
GET  /admin/python-toolchain
POST /admin/python-toolchain/probe
PUT  /admin/python-toolchain
GET  /admin/python-toolchain/environments
POST /admin/python-toolchain/environments/{owner}/sync
POST /admin/python-toolchain/environments/{owner}/repair
DELETE /admin/python-toolchain/environments/{owner}
```

同步接口可先同步返回；若实现异步 Job，必须有状态查询和取消。

### 9.4 错误文案

不要再显示：

```text
data/plugins/runtime/python/python.exe 不存在
Private Python missing
```

改为：

```text
Python 工具链尚未配置。
EmoAgent 核心功能可用；Managed Memory Sidecar 和 Python 插件不可用。
请选择 CPython 3.12 的 python.exe 和 uv.exe。
```

---

## 10. Diagnostics 与 Self-test

### 10.1 新检查项

```text
python_toolchain_configured
python_312_probe
python_architecture
uv_probe
uv_python_binding
sidecar_environment
plugin_environments
```

### 10.2 Overall Status

| 状态 | Overall |
|---|---|
| Toolchain 未配置，且 Sidecar 关闭、无已启用 Managed Plugin | `ok`，检查项为 `info` |
| Toolchain Enabled 但路径缺失 | `action_required` |
| Sidecar Enabled 但 Toolchain 不可用 | `warning/error` 取决于 fail_open |
| 已启用 Managed Plugin 但 Toolchain 不可用 | `error` |
| Toolchain ready，环境 needs_sync | `action_required` |
| 环境 sync/broken | `error` |
| 全部 ready | `ok` |

`-self-test-strict` 只在当前已启用功能真正需要 Toolchain 时因缺失而失败。

### 10.3 日志

只记录：

```text
路径
版本
环境 owner
lock hash
状态
耗时
脱敏错误
```

不记录：

```text
完整 Index 凭据
代理密码
环境变量值
完整 pip/uv 认证 URL
```

---

# 分阶段实施

## Phase 0 — Baseline 与文档

### 目标

建立增量迁移边界，不破坏 v0.4。

### 必做

1. 保存当前分支并记录两个仓库 baseline。
2. 把本 Spec 放入 EmoAgent `docs/todo/` 或项目约定目录。
3. 运行并记录：
   ```text
   go test ./...
   npm --prefix web run typecheck
   npm --prefix web run build
   ```
4. 在 MemoryCore 记录：
   ```text
   go test ./...
   当前 sidecar Python tests
   ```
5. 列出所有 Private Python、自定义依赖格式和 Sidecar Command 的引用。

### Gate

只有迁移清单和 baseline 全部明确后才能改代码。

---

## Phase 1 — PythonToolchainConfig 与 Probe

### 目标

新增统一 Toolchain Authority，但暂不切换 Sidecar/Plugin。

### 必做

- 新增顶层 Config、strict YAML、defaults、validation；
- Config Center Runtime Setting；
- Python 3.12 Probe；
- uv Probe；
- 临时 venv 绑定验证；
- Fingerprint；
- 单元测试使用 fake command runner；
- Admin read/probe API。

### 测试

```text
3.12.x accepted
3.11 / 3.13 rejected
non-CPython rejected
architecture mismatch rejected
relative path rejected
uv below minimum rejected
uv missing rejected
uv automatic Python download disabled
```

### Gate

Toolchain probe 能在 Windows 真实 Python 3.12 + uv 上通过。

---

## Phase 2 — UVEnvironmentManager

### 目标

实现 lock 驱动、原子、可恢复的 uv 环境管理。

### 必做

- 环境 marker；
- needs_sync 判定；
- `uv lock --check`；
- `uv sync --locked --no-dev`；
- temp/current/previous swap；
- per-env singleflight；
- ProcessGuard；
- bounded/redacted logs；
- environment self-test；
- crash cleanup。

### 测试

```text
首次创建
无变化不重新 sync
lock 变化触发 sync
Python/uv fingerprint 变化触发 sync
网络失败保留旧环境
stale lock 拒绝
并发 sync 去重
取消清理临时目录
Windows Rename 失败不破坏 current
```

### Gate

Fake runner 和真实 uv integration 均通过。

---

## Phase 3 — MemoryCore Sidecar uv 化

### 目标

让 Sidecar 使用统一 Toolchain 和独立 uv 环境。

### MemoryCore 必做

- `requires-python = ">=3.12,<3.13"`；
- 可安装 build-system；
- 生成并提交 `uv.lock`；
- `uv lock --check`；
- `uv sync --locked --no-dev`；
- 环境 Python 的 `-I -P -m memorycore_sidecar.server` 启动测试；
- 更新 README。

### EmoAgent 必做

- Sidecar Environment Owner；
- BuildSidecarSpec 注入环境 Python；
- 移除生产默认 `uv run python`；
- enable 前 prepare；
- startup 只验证、不联网 sync；
- fail-open diagnostics。

### Gate

```text
真实 Python 3.12 + uv
→ 创建 Sidecar env
→ 启动 health
→ 真实/假 Adapter smoke
→ 关闭后无遗留进程
```

---

## Phase 4 — Managed Plugin uv 环境

### 目标

替换 Private Python 和 custom ZIP dependency provisioner。

### 必做

- 插件 package contract；
- pyproject/lock validation；
- Plugin Environment Owner；
- RuntimeSupervisor 使用 env Python；
- 保留全部 v0.4 lifecycle 补丁；
- SDK example 改为 uv project；
- trust summary 使用 uv lock digest；
- legacy dependency format 明确迁移错误；
- 更新 Plugin Author Guide。

### Gate

```text
两个插件依赖冲突互不影响
启动只使用各自 env Python
运行期不执行 uv
lock 变化重新同步
sync 失败旧版本仍可回滚
插件工具、Hook、Facade 和审批 E2E 通过
```

---

## Phase 5 — UI、诊断和配置迁移

### 目标

让用户能选择 Python/uv 并修复环境。

### 必做

- Admin UI；
- API；
- Config Center persistence；
- restart_required；
- environment list/sync/repair；
- 按功能需求分级 diagnostics；
- 删除 Private Python warning 文案；
- 旧字段迁移 warning/error。

### Gate

从未配置状态可以完整完成：

```text
选择 Python 3.12
→ 选择 uv
→ Probe
→ 保存
→ 准备 Sidecar
→ 安装并启用示例插件
```

---

## Phase 6 — 删除私有 Python 实现

### 目标

完成架构收口。

### 删除

```text
ProvisionPrivatePythonRuntime
PrivatePythonArtifactPath/SHA256 运行逻辑
store private runtime path
private runtime artifact tests
fresh-install private artifact smoke
custom python_module_zip provisioner
```

### 保留

```text
旧字段兼容解析一版
process_dev
managed_python_process RuntimeKind
ProcessGuard / RuntimeSupervisor / Trust / Policy
```

### Gate

代码和文档中不再存在“EmoAgent 安装包携带 Python”的产品承诺。

---

## Phase 7 — Windows E2E 与发布 Gate

### 必测场景

```text
无 Python/uv：
  核心启动、聊天、SQLite fallback 正常，无全局 warning

配置错误 Python：
  Probe 拒绝，不保存为 ready

配置 Python 3.12 + uv：
  Toolchain ready

Sidecar：
  在线 sync、启动、health、停止、fallback

Plugin：
  install、sync、enable、tool approval、invoke、idle stop、
  crash backoff、quarantine、disable、update、rollback

Lock：
  stale/missing/changed/network failure

环境：
  Python 路径变化、uv 版本变化、环境损坏、Repair

安全：
  uv 和插件进程不继承 Provider Secret；
  日志不泄露 index credentials
```

### 命令

```text
go test ./...
go test -race ./internal/plugin/... ./internal/processguard/... ./internal/pytoolchain/... ./internal/sidecar/...
npm --prefix web run typecheck
npm --prefix web run build
git diff --check

MemoryCore:
go test ./...
uv lock --check --project sidecar
uv sync --locked --no-dev --project sidecar
<env-python> -I -P -m pytest ...
```

### 发布 Gate

```text
Windows 11
CPython 3.12 x64
uv >= configured minimum
真实网络安装
全新 data 目录
不安装 Docker/WSL2
不内置 Python
```

---

## 11. 不变量

```text
1. 不回滚 v0.4 已完成的运行时和安全补丁。
2. Managed Runtime 不从 PATH 隐式选择 Python/uv。
3. uv 不下载 Python。
4. 基础 Python 不被安装依赖。
5. Sidecar 与插件环境彼此隔离。
6. Marketplace lock 不在用户机器上自动更新。
7. 正常运行期不做依赖同步。
8. 已启用插件环境不可用时 fail-closed。
9. Sidecar fail-open 时 SQLite 仍是 authority。
10. Tool Result、插件内容和 Sidecar 内容仍是 data_only。
11. 第三方插件仍被定义为用户信任的本地代码，不宣称恶意代码隔离。
12. 没有启用 Python 能力时，缺少 Toolchain 不是应用故障。
```

---

## 12. 官方参考

- uv 环境管理：`https://docs.astral.sh/uv/pip/environments/`
- uv Lock 与 Sync：`https://docs.astral.sh/uv/concepts/projects/sync/`
- uv Settings：`https://docs.astral.sh/uv/reference/settings/`
- uv Environment Variables：`https://docs.astral.sh/uv/reference/environment/`
- uv Windows 安装：`https://docs.astral.sh/uv/getting-started/installation/`
