# EmoAgent Managed Local Runtime v0.4 — Codex 分阶段实施 Spec

> **Document status**: Implementation Specification
> **Version**: 0.4
> **Date**: 2026-06-20
> **Parent architecture**: `EmoAgent_ManagedLocalRuntime_Architecture_v0.4.md`
> **Migration guide**: `EmoAgent_CapabilityRuntime_v0.3_to_ManagedLocalRuntime_v0.4_Migration.md`
> **Target repository**: `LongYiSang/EmoAgent`
> **Execution style**: 先迁移审计，再渐进实现；不要求 Docker/WSL2/AppContainer。

---

## 0. 开始前要求

Codex 必须：

1. 阅读父架构和迁移说明。
2. 不假设本地分支等于 GitHub `main`。
3. 检查：
   ```text
   git status
   git log --oneline --decorate -n 40
   git diff --stat origin/main...HEAD
   git diff --name-status origin/main...HEAD
   ```
4. 保存当前阻塞分支，禁止直接 `reset --hard`。
5. 找到 Phase 3 最后绿色提交，或证明应从 `origin/main` 选择性重做。
6. 使用 SubAgent 完成：
   - Git/代码差异与保留项审计；
   - Runtime/Windows Job Object 设计；
   - Plugin Trust/Facade/Tool Policy 审计；
   - 测试和迁移审计。
7. 每阶段必须有完成报告和 Gate。

---

# Phase R0 — 架构转向与本地分支 Reconciliation

## 目标

决定从哪里继续，并把 Phase 0–3 的成果分为 KEEP / ADAPT / DROP / REVIEW。

## 必做

1. 创建 archive branch/tag/patch。
2. 输出：
   ```text
   docs/implementation/managed_local_runtime_v0.4_reconciliation.md
   ```
3. 报告：
   - 当前 HEAD；
   - origin/main；
   - Phase 0–4 相关提交；
   - 每个新增文件和迁移的用途；
   - 最后绿色提交；
   - 继续、选择性重放或重做的建议。
4. 按迁移文档分类：
   ```text
   KEEP / ADAPT / DROP / REVIEW
   ```
5. 创建 v0.4 分支或 Worktree。
6. 恢复绿色测试基线。

## 禁止

```text
删除 archive branch
直接丢弃未提交 WIP
在 Reconciliation 前继续实现 Phase 4
为了过测试删除 Phase 0–3 安全断言
```

## Gate

```text
go test ./...
npm --prefix web run typecheck
npm --prefix web run build
git diff --check
```

并且有明确的 base commit 和迁移报告。

---

# Phase R1 — 删除强沙箱发布 Gate，收敛 v0.4 契约

## 目标

让代码与新威胁模型一致，不再要求 Linux/WSL2/Docker/AppContainer 才能继续。

## 必做

1. 删除或 defer：
   - bubblewrap/WSL2 强边界作为跨阶段 Gate；
   - sandboxd；
   - Docker 默认插件；
   - AppContainer；
   - container-only DB/config；
   - runtime physical capability intersection。
2. 保留或适配：
   - Resource Broker；
   - ChangeSet；
   - Result Envelope；
   - allow/ask/deny；
   - Plugin Facade Capability。
3. 将文档和 UI 中：
   ```text
   sandboxed plugin / secure plugin / read-only plugin
   ```
   改为：
   ```text
   managed Python plugin / trusted local code
   ```
4. 定义轻量类型：
   ```text
   PluginTrustLevel
   ToolExposure
   InvocationPolicy
   ResultProvenance
   ```
5. 旧 config/DB 可加载。

## 测试

- 旧配置兼容；
- 不要求 Docker/WSL2；
- unknown fields 仍 fail-fast；
- Provider tool loop 无回归；
- Resource Broker/ChangeSet 原测试通过。

## Gate

默认开发和运行路径在纯 Windows + Go 环境可启动；插件可暂用现有 Python Runtime。

---

# Phase 1 — 轻量 Result Provenance 与 Hash-first Audit

## 目标

保留已完成的结果来源能力，但去掉过重 Taint Graph。

## 必做

1. Result 最小 Provenance：
   ```text
   ProducerKind
   ProducerID
   RuntimeKind
   InstructionAuthority
   InputHash
   OutputHash
   GeneratedAt
   ```
2. 固定：
   ```text
   plugin/web/file/memory -> data_only
   approval/policy -> host_control
   ```
3. 插件返回的 authority/trust 字段无效。
4. OpenAI/Anthropic Provider Rendering 保持兼容。
5. Work Journal 默认只存 hash + safe preview。
6. ToolCard 展示 producer/runtime/authority；legacy fallback。

## 测试

- Provider golden tests；
- plugin 不能伪造 host_control；
- journal 不含正文/secret fixture；
- legacy result 可用；
- snip/compact 无回归。

## Gate

所有 Tool Result 都有最小来源；工具循环和前端通过。

---

# Phase 2 — 验证并稳定 Host Resource Broker

## 目标

复用旧 Phase 2，不重新把它包装成插件 OS 权限。

## 必做

1. 验证：
   ```text
   list/search/stat/read/copy
   personal roots
   protected paths
   ResourceGrant
   symlink/path containment
   ```
2. `read_scope=all` 映射到配置个人目录。
3. 文档声明：
   ```text
   Broker 约束内置工具，不约束任意 Python 标准库。
   ```
4. 删除为插件沙箱专门存在的 mount/projection 代码。
5. Result 标记 `host_file/data_only`。

## 测试

沿用旧 Phase 2 测试，增加：

- 未配置 root 不可访问；
- MemoryCore/DB protected；
- plugin runtime 不把 Broker 误称为强边界。

## Gate

内置文件工具的读取能力和远程助手体验可用。

---

# Phase 3 — 验证并稳定 ChangeSet

## 目标

复用旧 Phase 3 的创建、覆盖、移动、删除和外部目录操作。

## 必做

1. 验证：
   ```text
   staging
   text diff / binary summary
   plan hash
   baseline conflict
   create/overwrite/move/delete/mkdir/rmdir
   quarantine
   rollback metadata
   approval binding
   ```
2. 所有外部写入的内置工具走 ChangeSet。
3. 文档明确：
   ```text
   高级 Bash/恶意插件仍可绕过；ChangeSet 是内置工具边界。
   ```
4. 不新增容器 mount 逻辑。

## Gate

旧 Phase 3 测试全部通过，失败/冲突不伪装成功。

---

# Phase 4 — Windows Managed Process Foundation

## 目标

用轻量进程管理替换强沙箱 Gate。

## 必做

1. 新包建议：
   ```text
   internal/processguard
   ```
2. 接口：
   ```go
   type ManagedProcess interface {
       Start(...)
       Wait(...)
       TerminateTree(...)
       Stats(...)
   }
   ```
3. Windows：
   - Job Object；
   - `KILL_ON_JOB_CLOSE`；
   - 禁止 breakaway；
   - active process limit；
   - process/job memory limit；
   - 可配置 CPU/time limit；
   - timeout/cancel 杀进程树。
4. Unix：
   - process group；
   - kill group；
   - rlimit best effort。
5. 环境变量：
   - allowlist；
   - 自动过滤 API_KEY/SECRET/TOKEN/PASSWORD；
   - 不无条件继承用户环境。
6. 句柄和 stdio：
   - 仅继承必要 pipe；
   - bounded stdout/stderr；
   - protocol stdout 与日志 stderr。
7. 明确类型：
   ```text
   isolation_level=process_managed
   not sandboxed
   ```

## 测试

Windows integration：

- 子进程被纳入 Job；
- 关闭 Job 杀进程树；
- timeout/cancel；
- process/memory limit；
- sensitive env 不继承；
- 不影响主进程；
- race test。

## Gate

Windows 本地可执行真实集成测试；不依赖 Docker/WSL2。

---

# Phase 5 — Application-managed Private Python

## 目标

普通用户无需安装 Python，插件不依赖系统 Python。

## 必做

1. 定义 Runtime Layout：
   ```text
   runtime/python
   runtime/bootstrap
   plugins/envs
   plugins/state
   plugins/cache
   plugins/run
   ```
2. `PythonRuntimeResolver`：
   - production 使用 bundled Python；
   - developer 可显式使用 external Python；
   - 不读取 PATH 作为默认。
3. 启动：
   ```text
   -I -P -u host bootstrap
   ```
4. Bootstrap 控制：
   - sys.path；
   - plugin id/version；
   - entry；
   - stdout JSON-RPC；
   - stderr logs。
5. self-test 和 diagnostics。
6. 安装包构建脚本准备 private Python。
7. 不把 CPython 二进制或字体等第三方资源提交到不合适的位置；按项目分发策略处理。

## 测试

- 系统未安装 Python 也可用 fixture runtime；
- user site-packages 不进入；
- PATH 中恶意 python 不被选中；
- bootstrap handshake；
- runtime missing/corrupt diagnostics。

## Gate

Plugin Runtime 可完全使用应用管理 Python。

---

# Phase 6 — Per-plugin Dependency Environments

## 目标

优先 Python 兼容，并隔离插件依赖冲突。

## 必做

1. 每版本环境：
   ```text
   plugins/envs/<plugin_id>/<version>
   ```
2. 第一版使用 private Python 创建 venv。
3. 安装输入：
   ```text
   requirements.lock
   wheelhouse optional
   ```
4. 普通模式：
   - binary wheel 优先/默认；
   - 固定版本；
   - lock digest；
   - 安装日志 hash；
   - shared download cache；
   - env 不共享 site-packages。
5. Developer Mode：
   - 可允许未锁定/source build；
   - UI 显示风险。
6. update/rollback：
   - 新环境；
   - 切 enabled pointer；
   - 失败不破坏旧版本。
7. cleanup 和磁盘配额。

## 测试

- 两插件依赖冲突；
- update/rollback；
- lock drift；
- failed install cleanup；
- wheel cache；
- uninstall 清理；
- offline wheelhouse fixture。

## Gate

普通插件安装不要求用户手动 pip/venv。

---

# Phase 7 — Managed Python Runtime Supervisor

## 目标

把现有 Process Runtime 接到 private Python、per-plugin env 和 ProcessGuard。

## 必做

1. Runtime Kind：
   ```text
   managed_python_process
   process_dev
   builtin
   ```
2. 兼容：
   ```text
   python_process -> managed path
   process -> developer path
   ```
3. 保留 stdio JSON-RPC。
4. 生命周期：
   ```text
   cold/starting/ready/idle/stopping/stopped/failed/backoff/quarantined
   ```
5. 实现：
   - lazy start；
   - idle stop；
   - concurrency limit；
   - startup/shutdown timeout；
   - crash backoff；
   - crash loop quarantine；
   - bounded logs；
   - health。
6. Python Audit Shim 改名 Observer，失败不宣称安全。
7. Runtime record 增加 Python/env/process managed 信息。

## 测试

- SDK example；
- crash/restart/backoff；
- idle stop；
- child process cleanup；
- malformed stdout；
- observer bypass/unavailable 只记录状态；
- PluginService Close。

## Gate

现有 Python 插件在新 Managed Runtime 中运行。

---

# Phase 8 — Plugin Trust、Host API Capability 与调用策略

## 目标

替代复杂插件 OS 权限图，保留真实可执行控制。

## 必做

1. `PluginTrustLevel`。
2. 签名/发布者/版本/digest 信任。
3. `FacadeBroker`：
   ```text
   manifest requested ∩ user grant ∩ host policy
   ```
4. Tool：
   ```text
   exposure = hidden/work/emotion
   invocation = auto/ask/deny
   ```
5. 第三方默认：
   ```text
   work + ask
   ```
6. 插件 Scope/Permission：
   - 不作为权威；
   - v0.2 兼容记录 hint；
   - Host 生成最终 Tool Spec。
7. Hook：
   - observe 可独立授权；
   - active 默认关闭；
   - fail-open/closed 保持。
8. 更新扩权重新确认。
9. UI 显示：
   - Trust；
   - Host API；
   - Tool Policy；
   - Hook；
   - 本地代码风险。

## 测试

- fake `ScopeBoth/read-only` 不改变最终策略；
- default Work+Ask；
- active hook denied；
- user grant narrowing；
- publisher/signature changes；
- re-consent；
- blocked digest；
- capability audit。

## Gate

插件权限 UI 不再制造 OS 隔离错觉；内部 API 和 Agent 调用仍可控。

---

# Phase 9 — Managed Bash

## 目标

保留“全能助手”能力，以 ProcessGuard、审批和审计管理宿主 Shell。

## 必做

1. `bash` 不再直接裸 `exec.CommandContext`，统一走 ProcessGuard。
2. 配置：
   ```text
   enabled
   managed_host
   always_ask / ask_destructive / auto / deny
   scope=work
   timeout/output/process/memory
   env allowlist
   ```
3. 危险分类器仅用于 ask/risk preview。
4. Approval 保持精确 Input Binding。
5. UI 首次启用和高风险调用提示：
   ```text
   当前用户权限
   本机文件/网络风险
   非安全沙箱
   ```
6. Result：
   ```text
   producer=builtin.bash
   runtime=managed_host_process
   authority=data_only
   ```
7. 可选后续增加结构化 `run_command`，但不阻塞本阶段。

## 测试

- timeout/cancel tree；
- env redaction；
- output cap；
- approval binding；
- classifier bypass 不被描述为安全；
- process limits；
- Windows command integration。

## Gate

Windows 默认不依赖 Docker/WSL2，Bash 仍可用且生命周期受控。

---

# Phase 10 — Packaging、诊断、UI 和发布

## 目标

形成点击安装即可使用的 Windows 产品路径。

## 必做

1. 安装包携带 private Python。
2. 首次启动 self-test。
3. Plugin Admin：
   - runtime；
   - trust；
   - env；
   - crash；
   - API capabilities；
   - tool policy；
   - hook policy；
   - risk warning。
4. ToolCard Provenance。
5. Diagnostics：
   - private Python；
   - venv；
   - Job Object；
   - plugin logs；
   - dependency install；
   - repair。
6. 文档：
   - Security Model；
   - Plugin Author Guide；
   - Dependency Packaging；
   - Developer Mode；
   - Bash Risk；
   - User Trust。
7. 旧 v0.2 plugin/config/DB migration。
8. 删除 Docker/WSL2 作为用户安装要求的文案。

## 最终验证

```text
go test ./...
go test -race selected packages
npm --prefix web run typecheck
npm --prefix web run build
Windows packaged smoke test
fresh Windows user test without Python/Docker/WSL2
plugin install/update/rollback
Bash process tree test
Resource Broker/ChangeSet E2E
```

---

## 每阶段报告模板

```markdown
## Phase N Completion

### Base and commits
### Implemented
### Reused from v0.3
### Removed/deferred from v0.3
### Changed files
### Tests and exact results
### Compatibility
### Security model impact
### Known limitations
### Gate status
### Next-step basis
```

---

## 最终完成状态

```text
- 无 Docker/WSL2/AppContainer 普通用户依赖；
- 第三方 Python 插件作为 trusted local code；
- 私有 Python + per-plugin env；
- Job Object 进程治理；
- Host API Capability；
- Tool Work+Ask 默认；
- Active Hook 默认关闭；
- Host Resource Broker/ChangeSet 保留；
- Managed Bash 可用；
- Result Provenance 轻量可追踪；
- UI/文档诚实披露插件与 Shell 风险。
```
