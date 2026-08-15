# EmoAgent Capability Runtime v0.3 → Managed Local Runtime v0.4 迁移说明

> **Document status**: Architecture Pivot / Migration Decision
> **Version**: 0.4
> **Date**: 2026-06-20
> **Target repository**: `LongYiSang/EmoAgent`
> **Supersedes**: `EmoAgent_CapabilityRuntime_Architecture_v0.3.md` 中 Phase 4 及之后的强沙箱、Docker 默认插件运行时设计
> **New parent architecture**: `EmoAgent_ManagedLocalRuntime_Architecture_v0.4.md`

---

## 0. 最终决策

**不要直接 `git reset --hard` 回到 GitHub 主分支，也不要丢弃已经完成的 Phase 0–3。**

推荐迁移方式：

```text
保存当前阻塞分支
→ 找到最后一个 Phase 3 全测试通过的提交
→ 从该提交创建新的 v0.4 分支或 Git Worktree
→ 执行 Reconciliation Phase
→ 保留 Resource Broker / ChangeSet / Result Provenance 等通用成果
→ 替换旧 Phase 4 以后强沙箱与容器假设
```

只有在以下情况同时成立时，才建议从当前 GitHub `main` 重新开始：

```text
1. Phase 0–3 没有独立提交或可识别边界；
2. Phase 0–3 与 sandboxd/Docker/AppContainer/Effect Graph 深度缠绕；
3. 回到 Phase 3 后 `go test ./...` 无法恢复；
4. Codex 无法用小范围适配把通用能力分离出来；
5. 迁移成本明显高于从 main 选择性重做 Phase 0–3。
```

即使需要从 `main` 重做，也应先保存当前分支和补丁，禁止直接销毁。

---

## 1. 为什么不应整体回滚

旧 v0.3 的前四部分并非都依赖强沙箱。

预计已经完成或部分完成的能力中，以下内容仍然有长期价值：

```text
Phase 0:
- 类型与 Feature Flag 骨架
- 新配置兼容
- Adapter 思路

Phase 1:
- Tool Result Envelope
- Producer / Runtime / Instruction Authority
- hash-first 审计
- ToolCard 来源展示

Phase 2:
- Host Resource Broker
- ResourceGrant
- 个人目录读取
- protected path
- resource id / canonical path

Phase 3:
- ChangeSet
- staging
- Diff / plan hash
- 覆盖、移动、删除、目录操作
- approval binding
- conflict detection / quarantine
```

这些能力服务于 EmoAgent 内置工具和宿主资源操作，不要求 Docker、WSL2、AppContainer 或恶意插件隔离。

需要替换的是：

```text
Phase 4:
- Bash 必须有 Linux/WSL2 强边界才能继续

Phase 5:
- 复杂 Effect / Runtime Physical Capability Intersection

Phase 6–7:
- sandboxd
- Docker 作为普通插件默认运行时
- 容器挂载和镜像控制面

Phase 8–9:
- 围绕强沙箱构建的 UI、文案、测试 Gate
```

---

## 2. 新威胁模型

### 2.1 v0.4 负责防御

```text
插件崩溃拖垮主进程
插件死循环、无限子进程、资源失控
插件依赖冲突
插件无意获取 Provider Key
插件通过 EmoAgent API 越权读取 MemoryCore 或其他宿主服务
Agent 未经允许自动调用高风险插件工具
包被篡改、版本漂移、发布者不明确
工具结果伪造审批或系统控制消息
Bash/插件日志泄露完整 Secret
```

### 2.2 v0.4 明确不防御

```text
用户主动安装并启用的恶意 Python 插件
恶意插件直接使用 os/socket/subprocess/ctypes 访问本机
恶意插件以当前用户身份读取或上传数据
高级本机 Shell 对用户文件和网络的访问
Windows 内核或 CPython 原生扩展漏洞
```

产品必须明确展示：

> 第三方 Python 插件是以当前用户身份运行的本地代码。启用插件等价于信任该插件及其依赖。EmoAgent 会管理进程、内部 API、调用审批和审计，但不会承诺隔离恶意插件对操作系统资源的访问。

---

## 3. 旧设计到新设计的映射

| v0.3 概念 | v0.4 处理 |
|---|---|
| OS-level Plugin Sandbox | 删除为 v1 必选项；未来可选后端 |
| Docker 默认市场插件 | 改为 Managed Python Process |
| AppContainer / WSL2 强边界 | 不进入 v1 |
| `sandboxd` | 删除/不实施 |
| Effect Graph | 仅保留内置工具需要的简单 Effect 或 Risk Hint |
| Runtime Physical Capability Intersection | 删除 |
| 插件自报 Scope/Permission | 仍然不可信，但改为宿主生成简单 Exposure + Invocation Policy |
| Host API Capability | 保留并加强 |
| ResourceGrant | 保留，用于内置 Host Resource Broker，不声称约束任意 Python |
| ChangeSet | 保留，用于内置宿主写入 |
| Result Provenance | 保留并轻量化 |
| Bash Sandbox | 改为 Host Managed Process + Job Object + 审批 |
| Python Security Shim | 改名 Audit Observer，不称为 Sandbox |
| Container Manifest | Legacy/Future 字段；不作为默认实现 Gate |

---

## 4. 推荐 Git 操作

### 4.1 首先保存当前状态

在当前阻塞分支执行：

```powershell
git status
git log --oneline --decorate -n 30
git branch archive/capability-runtime-v0.3-phase4-blocked
git tag archive-capability-runtime-v0.3-phase4-blocked
git diff origin/main...HEAD > capability-runtime-v0.3-local.patch
```

如果有未提交内容：

```powershell
git add -A
git commit -m "wip(capability-runtime): archive phase4 blocked implementation"
```

不要用 `git reset --hard` 清除唯一副本。

### 4.2 找最后一个绿色 Phase 3 提交

寻找满足以下条件的提交：

```text
包含 Phase 0–3
不包含或基本不包含 Phase 4 driver
go test ./... 通过
前端 build/typecheck 通过（若 Phase 1 修改了 ToolCard）
```

记为：

```text
<PHASE3_GREEN_COMMIT>
```

### 4.3 推荐用 Worktree

```powershell
git worktree add ..\EmoAgent-v0.4 `
  -b refactor/managed-local-runtime-v0.4 `
  <PHASE3_GREEN_COMMIT>
```

优点：

```text
旧阻塞分支完整保留
新旧实现可以并排比较
不需要频繁 stash/reset
Codex 可在新目录中继续
```

### 4.4 如果 Phase 4 只有未提交改动

可以直接从当前提交创建 v0.4 Worktree；旧目录保留 WIP：

```powershell
git worktree add ..\EmoAgent-v0.4 `
  -b refactor/managed-local-runtime-v0.4 `
  HEAD
```

### 4.5 如果阶段提交完全纠缠

采用“选择性重放”：

```text
新分支基于 origin/main
→ 先移植 Result Provenance
→ 再移植 Resource Broker
→ 再移植 ChangeSet
→ 每步测试
→ 不移植 sandboxd / container / platform sandbox
```

优先 `cherry-pick` 清晰提交；提交不清晰时用 `git restore --source=<old-branch> -- <paths>` 选择目录，不复制整个分支。

---

## 5. Reconciliation 分类清单

Codex 必须为本地改动生成以下表。

### KEEP

```text
ResourceRef / ResourceGrant（宿主内置资源）
Host Resource Broker
protected path policy
read/search/stat/copy tools
ChangeSet / staging / diff / plan hash
external create/overwrite/move/delete/mkdir/rmdir
approval exact binding
quarantine / conflict detection
Result Envelope 的 producer/runtime/authority/hash
hash-first audit
Provider adapters 的兼容包装
```

### ADAPT

```text
Effect:
  从全能力图缩减为内置工具 Risk/Operation 描述

PolicyDecision:
  保留 allow/ask/deny，删除 runtime physical intersection

Result Envelope:
  缩减为轻量 Provenance，不做完整 taint graph

Plugin Effective Grant:
  改成 Host API Capabilities + Exposure + Invocation

Bash ExecutionBroker:
  改成 Managed Host Process，不承诺文件/网络隔离

Python Process Runner:
  改成 bundled runtime + per-plugin env + Job Object

Python Security Shim:
  改成 Audit Observer

read_scope=all:
  继续映射到 Host Resource Broker 的个人目录 Profile
```

### DROP / DEFER

```text
Linux bubblewrap 作为 Windows 发布 Gate
WSL2 Broker Projection
AppContainer
Docker 作为默认插件运行时
sandboxd
Docker image/mount plan authority
插件 OS 文件/网络/process.spawn 权限矩阵
“只读 Python 插件”安全承诺
复杂 taint propagation graph
Runtime physical capability hash
```

### REVIEW

```text
Phase 0 新增但没有实际消费者的抽象
只为 sandboxd 存在的数据库表
Container-only config
过度复杂的 Effect 类型
与现有 Permission 完全重复的字段
破坏旧 Plugin Runtime v0.2 的 schema 修改
```

---

## 6. Reconciliation Gate

新分支进入 v0.4 实施前必须达到：

```text
go test ./...
npm --prefix web run typecheck
npm --prefix web run build
git diff --check
```

并满足：

```text
1. 不存在 Docker/WSL2/AppContainer 才能启动的默认路径；
2. Bash 暂时可继续使用旧实现或被 Feature Flag 关闭；
3. Process Plugin 暂时可继续使用旧 Runtime；
4. Resource Broker/ChangeSet 测试通过；
5. Result Envelope 的 Provider 兼容测试通过；
6. 没有数据库迁移只服务于已删除的 sandboxd 且无法回滚。
```

---

## 7. 何时才从 GitHub main 重做

Codex 在 Reconciliation Report 中证明以下任一情况后，才允许建议从 `origin/main` 重做：

```text
A. 本地分支没有可测试的 Phase 3 边界；
B. 80% 以上新增代码直接依赖 sandboxd/Container/AppContainer；
C. Resource Broker/ChangeSet 只是空骨架，没有稳定测试；
D. Result Envelope 已破坏 Provider 工具循环且无法小范围修复；
E. DB migration 不可逆且只服务旧架构；
F. 选择性迁移预计比重新实现 Phase 1–3 更复杂。
```

此时也应：

```text
保留 archive branch/tag/patch
从 origin/main 开新分支
按新 Spec 的 Phase 1–3 重做
```

---

## 8. 决策摘要

```text
首选：从 Phase 3 绿色提交继续。
次选：从当前 HEAD 建新 Worktree，先删除/适配 Phase 4 WIP。
最后选择：从 origin/main 开始，选择性重放 Phase 1–3。
禁止：直接 hard reset 丢弃当前成果。
```
