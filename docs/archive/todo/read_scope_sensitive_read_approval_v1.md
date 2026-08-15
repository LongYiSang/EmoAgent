# Spec: Read Scope & Sensitive Read Approval v1

## 0. 一句话定义

本设计把工具权限拆成两条正交轴：

```text
Operation Permission:
  read-only | workspace-write | approved-destructive

Read Scope:
  workspace | all
```

`Operation Permission` 决定 Work 能不能写、改、执行破坏性操作。这个三档保持不变，因为它已经贯穿 `TaskBrief.PermissionScope`、`ValidateAndComplete`、Dispatcher 和 Work runtime。当前 `ValidateAndComplete` 也只接受这三档。

`Read Scope` 只影响读取类工具：

```text
read_file
list_dir
未来可能的 search_files / stat_file
```

写入类工具：

```text
write_file
edit_file
```

仍然只能写 workspace 内路径。即使 `read_scope=all`，也不允许写 workspace 外部文件。

---

## 1. 设计目标

### 1.1 目标

实现以下能力：

```text
- 默认 read_scope=workspace，行为尽量保持现状。
- 显式 read_scope=all 时，read_file/list_dir 可以读取 workspace 外部路径。
- 读取敏感目录、敏感文件、凭据文件、密钥文件时，需要再次确认。
- 再次确认复用现有 tool_approval + approval binding + resume 链路。
- 不把“读外部路径”混入 workspace-write / approved-destructive。
- 不让 shell 成为绕过读取范围的通道。
```

### 1.2 非目标

本阶段不做：

```text
- 写 workspace 外文件。
- shell 的只读文件系统沙箱。
- 复杂 OS 权限代理。
- `~`、环境变量、glob、通配符展开。
- 多路径批量读取审批。
- 文件内容级 DLP 扫描。
- 对所有外部路径都要求确认。
```

---

## 2. 权限模型

### 2.1 Operation Permission 保持不变

继续使用：

```go
const (
    PermReadOnly            Permission = "read-only"
    PermWorkspaceWrite      Permission = "workspace-write"
    PermApprovedDestructive Permission = "approved-destructive"
)
```

当前 `Permission` 只表达操作强度，从只读到 workspace 写入，再到批准后的破坏性操作。

### 2.2 新增 ReadScope

在 `protocol.TaskBrief` 中新增：

```go
ReadScope string `json:"read_scope,omitempty"`
```

枚举：

```text
workspace
all
```

默认：

```text
workspace
```

含义：

| read_scope  | 含义                                                                   |
| ----------- | -------------------------------------------------------------------- |
| `workspace` | `read_file/list_dir` 只能读取 workspace 内路径；absolute path 和 escape 继续拒绝。 |
| `all`       | `read_file/list_dir` 可以读取 workspace 外路径；但敏感路径必须走 tool approval。      |

### 2.3 兼容性

现有调用如果不传 `read_scope`：

```text
permission_scope=read-only
read_scope=workspace
```

因此不会改变现有默认安全行为。

---

## 3. TaskBrief 与 delegate_to_work 改造

### 3.1 protocol.TaskBrief

文件：

```text
internal/protocol/types.go
```

改为：

```go
type TaskBrief struct {
    TaskID             string    `json:"task_id"`
    Goal               string    `json:"goal"`
    Background         string    `json:"background,omitempty"`
    Constraints        []string  `json:"constraints,omitempty"`
    AcceptanceCriteria []string  `json:"acceptance_criteria,omitempty"`
    PermissionScope    string    `json:"permission_scope"`
    ReadScope          string    `json:"read_scope,omitempty"`
    CreatedAt          time.Time `json:"created_at"`
}
```

### 3.2 ValidateAndComplete

文件：

```text
internal/work/brief.go
```

新增默认与校验：

```go
if strings.TrimSpace(brief.ReadScope) == "" {
    brief.ReadScope = "workspace"
}

switch brief.ReadScope {
case "workspace", "all":
    // accepted
default:
    return fmt.Errorf("unsupported read_scope %q (accepted: workspace, all)", brief.ReadScope)
}
```

### 3.3 delegate_to_work schema

文件：

```text
internal/work/delegate_tool.go
```

当前 schema 只允许 `goal/background/constraints/acceptance_criteria/permission_scope`。

新增：

```json
"read_scope":{"type":"string","enum":["workspace","all"]}
```

不设为 required。

### 3.4 delegate_to_work description

增加说明：

```text
Read scope guidance:
- read_scope defaults to workspace.
- use read_scope=all only when the user explicitly asks to inspect local files outside the workspace, or the task cannot be completed without external local files.
- read_scope=all affects read_file/list_dir only; write_file/edit_file still cannot modify files outside the workspace.
- sensitive paths still require explicit approval.
```

---

## 4. Work system prompt 改造

文件：

```text
internal/work/context.go
```

当前 Work prompt 会告诉模型 file tool paths 必须 workspace-relative。 这需要改成根据 read scope 分支。

### 4.1 Execution Environment 增加

```text
- Read scope: workspace
```

或：

```text
- Read scope: all local files
```

### 4.2 Tool Selection Policy 改造

当 `read_scope=workspace`：

```text
- read_file/list_dir paths must be workspace-relative.
- absolute paths and paths escaping the workspace are not allowed.
```

当 `read_scope=all`：

```text
- read_file/list_dir may use absolute local paths or paths relative to the workspace.
- Use the narrowest possible path.
- Do not inspect credential, secret, browser profile, keychain, SSH, cloud credential, or system-sensitive directories unless the task explicitly requires it.
- Sensitive local reads will pause for explicit approval.
- write_file/edit_file remain workspace-only.
```

### 4.3 Permission section 保持操作语义

不要把 `read_scope=all` 写进 `permission_scope`。例如：

```text
You are limited to read-only operations.
Read scope: all local files.
You may inspect local files outside the workspace using read_file/list_dir, but you must not modify files.
Sensitive reads require explicit approval.
```

---

## 5. ReadScope Context

新增文件：

```text
internal/tool/read_scope.go
```

```go
package tool

import "context"

type ReadScope string

const (
    ReadScopeWorkspace ReadScope = "workspace"
    ReadScopeAll       ReadScope = "all"
)

type readScopeKey struct{}

func WithReadScope(ctx context.Context, scope ReadScope) context.Context {
    if ctx == nil {
        ctx = context.Background()
    }
    if scope == "" {
        scope = ReadScopeWorkspace
    }
    return context.WithValue(ctx, readScopeKey{}, scope)
}

func ReadScopeFromContext(ctx context.Context) ReadScope {
    if ctx == nil {
        return ReadScopeWorkspace
    }
    scope, ok := ctx.Value(readScopeKey{}).(ReadScope)
    if !ok || scope == "" {
        return ReadScopeWorkspace
    }
    return scope
}
```

### 5.1 Runtime 注入

文件：

```text
internal/work/runtime.go
```

在 `runLoop` 开始处，基于 `brief.ReadScope` 包装执行上下文：

```go
readScope := tool.ReadScope(strings.TrimSpace(brief.ReadScope))
if readScope == "" {
    readScope = tool.ReadScopeWorkspace
}
ctx = tool.WithReadScope(ctx, readScope)
```

这样：

```text
classifyToolCalls(...)
Dispatcher.Execute(...)
read_file/list_dir handler
ApprovalClassifier
```

都能从 context 获取当前 read scope。

### 5.2 Resume 保持一致

因为 `PausedWork.Brief` 会被持久化进 `ResumeBlob`，恢复时继续用同一个 `brief.ReadScope`。`ResumeBlob` 当前已经保存完整 `Brief`。

---

## 6. 路径解析设计

新增文件：

```text
internal/tool/builtin/read_path_policy.go
```

### 6.1 ResolveReadPath

建议结构：

```go
type resolvedReadPath struct {
    InputPath         string
    FullPath          string
    DisplayPath       string
    WorkspaceRelative string
    InWorkspace       bool
    External          bool
    Sensitive         bool
    SensitiveReason   string
}
```

函数：

```go
func resolveReadPath(ctx context.Context, projectRoot string, rawPath string) (resolvedReadPath, error)
```

规则：

#### read_scope=workspace

```text
- rawPath 不能为空。
- rawPath 不能是 absolute path。
- filepath.Clean(rawPath) 不能是 "." 以外的 escape。
- fullPath = filepath.Join(projectRoot, cleaned)
- 对 fullPath 做 filepath.EvalSymlinks；如果目标存在且 symlink 后逃出 workspace，则拒绝。
- display path 保持 workspace-relative。
```

#### read_scope=all

```text
- absolute path：clean 后直接使用。
- relative path：先按 workspace-relative 解析；允许 ../ 逃出 workspace。
- 不做 shell expansion，不展开 ~，不展开环境变量。
- 对已存在路径做 EvalSymlinks。
- 判断最终 real path 是否在 workspace 内。
- display path：workspace 内显示相对路径，workspace 外显示 clean absolute path。
```

### 6.2 为什么要 EvalSymlinks

当前 `read_file` 的 workspace containment 只看字符串路径；如果 workspace 内存在 symlink 指向外部，可能绕过 workspace 边界。新 resolver 应把 symlink 后真实路径纳入边界判断。

### 6.3 写工具不改

`write_file/edit_file` 仍使用 workspace-only safe join。当前读取扩展不能影响写入范围。

---

## 7. Sensitive Read Approval

### 7.1 不复用 DestructiveClassifier

不要把敏感读升级成 `approved-destructive`。原因：

```text
- 读取敏感文件不是 destructive。
- Operation Permission 应继续表达写入/破坏能力。
- 敏感读需要的是确认，不是写权限提权。
```

建议给 `tool.Spec` 新增一个更通用的 approval classifier。

### 7.2 新增 ApprovalClassifier

文件：

```text
internal/tool/spec.go
```

新增：

```go
type ApprovalKind string

const (
    ApprovalKindDestructiveWrite ApprovalKind = "destructive_write"
    ApprovalKindSensitiveRead    ApprovalKind = "sensitive_read"
)

type ApprovalRequirement struct {
    Kind   ApprovalKind
    Reason string
}

type ApprovalClassifier func(ctx context.Context, input json.RawMessage) (ApprovalRequirement, bool)
```

扩展 `Spec`：

```go
type Spec struct {
    Name                  string
    Description           string
    Parameters            json.RawMessage
    Scope                 Scope
    Permission            Permission
    DestructiveClassifier DestructiveClassifier
    ApprovalClassifier    ApprovalClassifier
}
```

### 7.3 read_file/list_dir classifier

`read_file` 和 `list_dir` 使用 closure 捕获 `projectRoot`：

```go
ApprovalClassifier: classifySensitiveRead(projectRoot, "read_file")
ApprovalClassifier: classifySensitiveRead(projectRoot, "list_dir")
```

分类逻辑：

```text
1. resolveReadPath(ctx, projectRoot, input.path)
2. 如果路径因 read_scope 不允许而解析失败：不返回 approval requirement，让 handler 返回范围错误。
3. 如果 path sensitive：返回 ApprovalKindSensitiveRead。
4. 如果 list_dir recursive=true 且目标在 workspace 外：返回 ApprovalKindSensitiveRead，reason=external_recursive_list.
5. 否则不需要 approval。
```

### 7.4 敏感路径规则

初始规则建议保守但不过度阻塞：

#### 敏感目录 segment

```text
.ssh
.aws
.gcloud
.azure
.kube
.gnupg
.password-store
.keychain
.git
```

#### 敏感文件名 / 后缀

```text
.env
.env.*
*.pem
*.key
*.p12
*.pfx
id_rsa
id_ed25519
authorized_keys
known_hosts
credentials
credentials.*
secrets
secrets.*
secret
secret.*
token
token.*
.netrc
.npmrc
.pypirc
```

#### 系统特殊目录

POSIX：

```text
/proc
/sys
/dev
/etc/ssh
/etc/sudoers
```

Windows：

```text
C:\Windows
C:\Users\<user>\AppData\Roaming\Microsoft\Credentials
```

MVP 可以先不做复杂 Windows 凭据目录，至少覆盖通用 segment + basename 规则。

---

## 8. Dispatcher 改造

当前 Dispatcher 已经先做 schema 校验、再做 operation permission 检查、再做 destructive approval binding 检查。

改造方向：

```text
1. 保留 existing destructive path。
2. 在 permission satisfied 后，额外检查 Spec.ApprovalClassifier。
3. 如果返回 sensitive_read requirement，则进入 CallActionToolApprovalRequired。
4. approval binding 必须包含 approval_kind。
```

### 8.1 CallClassification 扩展

```go
type CallClassification struct {
    ...
    ApprovalKind   ApprovalKind
    ApprovalReason string
}
```

对于 destructive：

```go
ApprovalKind = ApprovalKindDestructiveWrite
```

对于 sensitive read：

```go
ApprovalKind = ApprovalKindSensitiveRead
```

### 8.2 ApprovalContext 扩展

当前 `ApprovalContext` 已有 request、destructive flag、tool name、input hash、path digest。

新增：

```go
ApprovalKind string
AllowToolCall bool
```

建议兼容保留：

```go
AllowDestructive bool
```

但新逻辑优先用：

```go
ApprovalKind
AllowToolCall
```

示例：

```go
type ApprovalContext struct {
    RequestID           string
    ApprovalKind        string
    AllowToolCall       bool

    // legacy / compatibility
    AllowDestructive    bool

    ToolName            string
    NormalizedInputHash string
    PathDigest          string
}
```

匹配逻辑：

```go
approval.RequestID != ""
approval.ToolName == binding.ToolName
approval.NormalizedInputHash == binding.NormalizedInputHash
approval.PathDigest == binding.PathDigest
approval.ApprovalKind == binding.ApprovalKind
approval.AllowToolCall == true
```

兼容 destructive：

```go
if binding.ApprovalKind == "destructive_write" && approval.AllowDestructive {
    allow when old fields match
}
```

---

## 9. ApprovalBinding 扩展

当前 `ApprovalBinding` 没有 kind。

改成：

```go
type ApprovalBinding struct {
    RequestID           string `json:"request_id,omitempty"`
    ApprovalKind        string `json:"approval_kind"`
    ToolName            string `json:"tool_name"`
    NormalizedInputHash string `json:"normalized_input_hash"`
    PathDigest          string `json:"path_digest,omitempty"`
    InputPreview        string `json:"input_preview,omitempty"`
}
```

`BuildApprovalBinding` 改成：

```go
func BuildApprovalBinding(call Call, requestID string, kind ApprovalKind) (ApprovalBinding, error)
```

对于现有 destructive 路径，传：

```go
ApprovalKindDestructiveWrite
```

对于敏感读取路径，传：

```go
ApprovalKindSensitiveRead
```

---

## 10. protocol 与 storage 扩展

### 10.1 protocol.ToolApprovalBinding

当前 protocol 已经有 `ToolApprovalBinding`，但没有 `approval_kind`。

新增：

```go
ApprovalKind string `json:"approval_kind"`
```

### 10.2 approval_requests migration

当前 migration 14 已经给 `approval_requests` 增加：

```text
tool_name
normalized_input_hash
path_digest
input_preview
```



新增 migration 15：

```sql
ALTER TABLE approval_requests ADD COLUMN approval_kind TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_approval_requests_kind_binding
    ON approval_requests(session_id, task_id, approval_kind, tool_name, normalized_input_hash, path_digest);
```

### 10.3 ApprovalService

当前 `ApprovalService.CreateRequestFromDecision` 已经会把 binding 写入 approval request 与数据库字段。

需要补：

```text
- create 写 approval_kind。
- scan 读 approval_kind。
- List / Get / Consume round-trip approval_kind。
```

---

## 11. Tool Approval UX

### 11.1 destructive_write 文案

沿用你上一轮统一后的“预览—确认—执行—审计”风格。

### 11.2 sensitive_read 文案

模板：

```text
我准备执行一次敏感读取，尚未执行。

操作：读取文件 / 列出目录
目标：<display_path>
原因：目标位于敏感路径或可能包含凭据、密钥、令牌、账号配置等信息。
影响：确认后，我会把该文件/目录内容作为本次任务证据读取；不会修改任何文件。

确认读取请点击“允许执行”；取消请点击“拒绝”。
```

### 11.3 外部但非敏感路径

不需要 per-call approval，但 tool result 应标明：

```json
{
  "path": "...",
  "path_scope": "external",
  "content": "...",
  "size": 123
}
```

这样 Work 在汇报时知道这是 workspace 外部证据。

---

## 12. read_file/list_dir 输出改造

### 12.1 read_file

当前返回：

```json
{
  "path": "...",
  "content": "...",
  "size": 123
}
```

扩展为：

```json
{
  "path": "...",
  "path_scope": "workspace|external",
  "content": "...",
  "size": 123
}
```

### 12.2 list_dir

当前返回：

```json
{
  "path": "...",
  "entries": [],
  "truncated": false
}
```

扩展为：

```json
{
  "path": "...",
  "path_scope": "workspace|external",
  "entries": [],
  "truncated": false
}
```

---

## 13. 安全边界

### 13.1 禁止绕过

```text
- read_scope=all 不允许 write_file/edit_file 写外部路径。
- read_scope=all 不允许 shell 在 read-only 下启用。
- sensitive_read approval 不得复用 destructive_write approval。
- approval binding 不匹配不得执行。
- approval 过期、拒绝、已 consumed 不得执行。
```

### 13.2 读取结果不进长期记忆

这和当前长期记忆边界一致：Work 的工具输出、文件内容、搜索页面等执行噪音不能自动进入长期记忆；Work 只能提交候选，且要经 Emotion/Memory policy 审批。

因此外部文件读取结果：

```text
- 可以进入 Work evidence / TaskReport summary。
- 不自动进入 MemoryCore facts。
- 不进入 long-term memory extraction，除非用户明确要求或 Emotion 审批。
```

---

## 14. 测试计划

### 14.1 Unit tests: read path resolver

文件：

```text
internal/tool/builtin/read_path_policy_test.go
```

必测：

```text
workspace scope:
- read_file path="README.md" -> workspace
- read_file path="/tmp/foo" -> reject
- read_file path="../foo" -> reject
- workspace symlink -> external target -> reject

all scope:
- read_file path="/tmp/foo" -> external allowed
- read_file path="../outside.txt" -> external allowed
- workspace file still path_scope=workspace
- no ~ expansion
```

### 14.2 Unit tests: sensitive classifier

```text
- .env -> sensitive_read approval
- .ssh/config -> sensitive_read approval
- id_rsa -> sensitive_read approval
- credentials.json -> sensitive_read approval
- ordinary external /tmp/readme.txt -> no approval
- list_dir recursive external -> sensitive_read approval
- list_dir non-recursive external ordinary dir -> no approval
```

### 14.3 Dispatcher tests

```text
- read_file sensitive path + no approval -> tool_approval_required
- read_file sensitive path + matching sensitive_read approval -> execute
- read_file sensitive path + destructive_write approval -> reject / tool_approval_required
- read_file sensitive path + mismatched input hash -> reject
- destructive_write approval does not satisfy sensitive_read
- sensitive_read approval does not satisfy destructive_write
```

### 14.4 Runtime tests

```text
- TaskBrief read_scope defaults to workspace.
- delegate_to_work accepts optional read_scope.
- Runtime injects read_scope into tool execution context.
- Paused/resumed sensitive_read call preserves read_scope through ResumeBlob.
```

### 14.5 Handler tests

```text
read_file:
- workspace scope rejects absolute.
- all scope reads absolute temp file.
- all scope reports path_scope=external.
- files > 1 MiB still rejected.
- non-UTF8 still rejected.

list_dir:
- workspace scope rejects escape.
- all scope lists external temp dir.
- max_entries still enforced.
```

### 14.6 Suggested commands

```bash
go test ./internal/tool/...
go test ./internal/work/...
go test ./internal/chat/...
go test ./...
```

---

## 15. 推荐实施顺序

### Step 1 — 只做 read_scope 字段与 prompt

```text
- TaskBrief 加 read_scope。
- ValidateAndComplete 默认 workspace。
- delegate_to_work schema 加 read_scope。
- Work system prompt 根据 read_scope 生成路径说明。
```

这一步不改 handler 行为也能先稳定 schema。

### Step 2 — 做 read path resolver

```text
- 新增 ReadScope context。
- Runtime 注入 read scope。
- read_file/list_dir 改用 resolveReadPath。
- read_scope=workspace 保持现状。
- read_scope=all 支持 absolute 和 escape。
```

### Step 3 — 做 sensitive read approval

```text
- Spec 加 ApprovalClassifier。
- Binding 加 approval_kind。
- Dispatcher 支持 sensitive_read approval。
- protocol/storage/ApprovalService 增加 approval_kind。
- read_file/list_dir 挂 sensitive classifier。
```

### Step 4 — 测试与文档收口

```text
- 补 resolver / classifier / dispatcher / runtime tests。
- 更新 Work 运行时文档。
- 更新 README 权限说明。
```
