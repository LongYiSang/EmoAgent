# Spec: Destructive Tool Approval v1

## 0. 背景与现状

当前 Work runtime 已经具备三档权限：`read-only`、`workspace-write`、`approved-destructive`；其中 `approved-destructive` 的定义就是“允许与已批准路径一致的破坏性操作，未获批准或不再匹配批准内容时必须停下并重新决策”。

现有运行主循环已经在执行真实工具前统一调用 `Dispatcher.ClassifyCall`，并且当命中 `permission_escalation_required` 或 `tool_approval_required` 时，会暂停、保存 pending tool call，不执行同轮 sibling 工具。

当前权限判定入口已经集中到 `Dispatcher.ClassifyCall`，它会做 registry lookup、JSON Schema 校验、`Spec.DestructiveClassifier` 输入级破坏性判定、permission scope 检查和 active approval context 检查；现有文档也明确写着 `write_file` / `edit_file` 暂未新增输入级 classifier。

代码层现状与文档一致：`Spec` 已有 `DestructiveClassifier func(input json.RawMessage) (bool, string)`；`Dispatcher.effectivePermission` 会在工具原权限是 `workspace-write` 且 classifier 判定 destructive 时升级到 `approved-destructive`。

但当前 `ApprovalContext` 只有：

```go
type ApprovalContext struct {
    RequestID        string
    AllowDestructive bool
}
```

也就是说，审批只表达“有一个允许 destructive 的请求”，没有绑定具体 tool name、输入 hash 或 path digest。

`write_file` 当前会创建新文件，也会覆盖已有内容，并支持 `create_dirs`；但 spec 里没有 classifier。 `edit_file` 当前支持 `replace_all`，且根据 `replace_all` 决定替换一次还是替换全部；也没有 classifier。

Memory forget 路径已经具备更成熟的“预览—确认—执行—审计”范式：`PreviewForget` 会生成 `PreviewHash` 并记录 preview audit；`ExecuteForget` 会重新 resolve preview，并校验 preview hash 是否变化，再要求 confirmed exact targets。

因此本 spec 的目标不是重做 Work runtime，而是补齐三处缺口：

```text
1. write_file / edit_file 输入级 destructive classifier
2. tool approval 从请求级升级为调用级绑定
3. destructive approval 与 manual forget 统一为“预览—确认—执行—审计”语言风格
```

---

## 1. 目标

### 1.1 功能目标

实现一个完整闭环：

```text
Work tool call
  ↓
ClassifyCall 输入级风险判定
  ↓
workspace-write 下 destructive → permission_escalation_required
approved-destructive 下 destructive 但无匹配审批 → tool_approval_required
  ↓
生成 approval request，绑定 tool_name + normalized_input_hash + path_digest + request_id
  ↓
用户确认 / 拒绝
  ↓
resume 前重新校验 pending tool call 与 approval binding
  ↓
匹配才执行；不匹配 fail-closed，不执行
  ↓
写入审批与执行审计
```

### 1.2 非目标

本阶段不做：

```text
- 文件 diff UI。
- 多文件批量审批。
- LLM 生成审批请求。
- 把普通 human_confirmation 和 runtime-only tool_approval 合并成同一种内部类型。
- 改造 MemoryCore 的底层 forget 语义。
- 引入法律合规或备份删除策略。
```

---

## 2. 设计原则

### 2.1 Dispatcher 仍是唯一权限入口

不要新增 `WouldNeedApproval` 之类的旁路预判。所有真实执行前都必须通过：

```go
Dispatcher.ClassifyCall
Dispatcher.Execute
```

### 2.2 输入语义决定 destructive，而不是工具名决定 destructive

`write_file` 和 `edit_file` 不应整体升级为 destructive。下面这些才需要提权：

```text
write_file:
- 覆盖已存在文件。
- 写入敏感文件名或敏感目录。
- create_dirs=true 且会创建/触碰关键目录。
- 目标路径疑似仓库、凭据、密钥、CI、部署或系统配置关键路径。

edit_file:
- replace_all=true。
- 大范围替换。
- 替换目标在敏感文件名或关键目录。
- old_string 过长，接近整文件替换。
- 替换次数过多。
- 修改后文件大小剧烈变化。
```

### 2.3 审批必须绑定“这一次调用”

批准不能只表示“本任务允许破坏性操作”，必须绑定：

```text
request_id
tool_name
normalized_input_hash
path_digest
```

推荐额外记录：

```text
destructive_reason
input_preview
path_preview
created_at
expires_at
```

### 2.4 恢复前必须重新校验

用户批准后，恢复执行前必须重新计算当前 `PendingToolCall` 的 binding。如果任一字段不一致：

```text
- 不执行工具 handler。
- 不复用旧 approval。
- 给 Work 注入安全失败结果，或重新生成新的 approval request。
- 写入审计。
```

### 2.5 UX 统一为“预览—确认—执行—审计”

manual forget 当前已经有“找到候选，尚未执行删除；确认删除/取消”的 UX。 destructive approval 应采用同一语言骨架：

```text
预览：我准备执行 X，尚未执行。
确认：确认执行 / 取消。
执行：已执行 / 未执行。
审计：内部记录 request_id、hash、digest、actor、时间、结果。
```

---

## 3. 数据结构设计

### 3.1 扩展 `tool.ApprovalContext`

文件：

```text
internal/tool/approval_context.go
```

建议改为：

```go
type ApprovalContext struct {
    RequestID           string
    AllowDestructive    bool
    ToolName            string
    NormalizedInputHash string
    PathDigest          string
}
```

兼容策略：

```text
- 允许旧测试继续构造 RequestID + AllowDestructive。
- 但对 destructive call 的真正执行路径，必须要求 ToolName / NormalizedInputHash / PathDigest 全部匹配。
- 测试中如使用旧 ApprovalContext，应更新为绑定版。
```

### 3.2 新增 approval binding DTO

建议放在：

```text
internal/tool/approval_binding.go
```

```go
type ApprovalBinding struct {
    RequestID           string `json:"request_id,omitempty"`
    ToolName            string `json:"tool_name"`
    NormalizedInputHash string `json:"normalized_input_hash"`
    PathDigest          string `json:"path_digest,omitempty"`
    InputPreview        string `json:"input_preview,omitempty"`
}
```

核心函数：

```go
func BuildApprovalBinding(call Call, requestID string) (ApprovalBinding, error)
func NormalizedInputHash(input json.RawMessage) (string, error)
func PathDigestForCall(call Call) string
func ValidateApprovalBinding(ctx context.Context, call Call) error
```

`NormalizedInputHash` 规则：

```text
- 对 input 做 JSON 解析。
- 使用确定性 canonical JSON 重新编码。
- sha256(canonical_json)。
- 输出格式：sha256:<hex>。
- hash 必须覆盖完整输入，包括 write_file.content / edit_file.old_string / new_string / replace_all。
```

`PathDigestForCall` 规则：

```text
- 只对有 path 字段的工具生成。
- path 先 trim + filepath.Clean + filepath.ToSlash。
- 不保存原始 path 到 digest 字段。
- sha256(normalized_path)。
- 输出格式：sha256:<hex>。
```

### 3.3 扩展 protocol

文件：

```text
internal/protocol/types.go
```

新增：

```go
type ToolApprovalBinding struct {
    ToolName            string `json:"tool_name"`
    NormalizedInputHash string `json:"normalized_input_hash"`
    PathDigest          string `json:"path_digest,omitempty"`
    InputPreview        string `json:"input_preview,omitempty"`
}
```

扩展：

```go
type DecisionPacket struct {
    ...
    ToolApprovalBinding *ToolApprovalBinding `json:"tool_approval_binding,omitempty"`
}

type ApprovalRequest struct {
    ...
    ToolApprovalBinding *ToolApprovalBinding `json:"tool_approval_binding,omitempty"`
}
```

---

## 4. 存储迁移设计

当前 `approval_requests` 只保存 request、task、category、question、options、status、actor、time 等字段，没有调用级 binding 字段。

新增 migration，版本号按当前仓库最后版本递增，例如：

```text
internal/storage/schema.go
Migration 13 或当前最新版本 + 1
```

SQL：

```sql
ALTER TABLE approval_requests ADD COLUMN tool_name TEXT NOT NULL DEFAULT '';
ALTER TABLE approval_requests ADD COLUMN normalized_input_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE approval_requests ADD COLUMN path_digest TEXT NOT NULL DEFAULT '';
ALTER TABLE approval_requests ADD COLUMN input_preview TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_approval_requests_binding
    ON approval_requests(session_id, task_id, tool_name, normalized_input_hash, path_digest);
```

如 SQLite 迁移系统不适合多次 `ALTER TABLE`，按现有 migration 风格拆为同一个 SQL 字符串即可。

---

## 5. Classifier 设计

### 5.1 文件组织

建议新增：

```text
internal/tool/builtin/destructive_file_classifier.go
internal/tool/builtin/destructive_file_classifier_test.go
```

并在：

```text
internal/tool/builtin/write_file.go
internal/tool/builtin/edit_file.go
```

给 spec 加上：

```go
DestructiveClassifier: classifyWriteFileDestructive(projectRoot)
DestructiveClassifier: classifyEditFileDestructive(projectRoot)
```

由于 classifier 类型只接收 `json.RawMessage`，可以用 closure 捕获 `projectRoot`。

### 5.2 write_file classifier

输入：

```go
struct {
    Path       string `json:"path"`
    Content    string `json:"content"`
    CreateDirs bool   `json:"create_dirs"`
}
```

判定 destructive 的条件：

```text
A. 覆盖已有文件
- safeJoin(projectRoot, path) 成功。
- os.Stat(fullPath) 成功且不是目录。
- reason: write_file would overwrite existing file.

B. 敏感文件名或敏感目录
- path basename 或任一 segment 命中 sensitive list。
- reason: write_file targets sensitive path.

C. create_dirs=true 且目标 parent 会创建关键目录
- parent 不存在，且 parent path 包含 critical dir segment。
- reason: write_file would create critical directory.

D. path 指向目录
- os.Stat(fullPath) 是目录时，handler 会失败；classifier 可以不升级，也可以返回 destructive=false。
```

建议 sensitive path list：

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
.kube
.aws
.ssh
.git
```

建议 critical dir list：

```text
.git
.github/workflows
.ssh
.aws
.kube
node_modules
vendor
dist
build
target
bin
```

注意：

```text
- 普通新建 docs/foo.md 不应 destructive。
- 普通新建 internal/foo.go 不应 destructive。
- 普通 create_dirs=true 到 docs/newdir/foo.md 不应 destructive。
```

### 5.3 edit_file classifier

输入：

```go
struct {
    Path       string `json:"path"`
    OldString string `json:"old_string"`
    NewString string `json:"new_string"`
    ReplaceAll bool   `json:"replace_all"`
}
```

判定 destructive 的条件：

```text
A. replace_all=true
- 一律 destructive。
- reason: edit_file replace_all may modify multiple locations.

B. 敏感文件名或敏感目录
- 同 write_file。
- reason: edit_file targets sensitive path.

C. 大范围替换
- 读取目标文件成功，UTF-8，有 old_string 命中。
- old_string 长度 / 文件长度 >= 0.25。
- 或 abs(len(newContent)-len(oldContent)) >= 8192。
- 或 replace count >= 5。
- reason: edit_file changes large portion of file.

D. empty old_string
- 当前 handler 没有拒绝 empty old_string；这会导致 `strings.Count(content, "")` 出现大量匹配。
- 本次应在 handler 中新增硬校验：`old_string` 不能为空。
- classifier 可将 empty old_string 判为 destructive，但 handler 必须最终拒绝执行。
```

### 5.4 handler 安全补丁

`edit_file` 应新增：

```go
if in.OldString == "" {
    return nil, fmt.Errorf("edit_file: old_string must not be empty")
}
```

这是独立安全修复，应纳入本任务验收。

---

## 6. Approval binding 执行链路

### 6.1 生成 approval request

当前 `buildToolApprovalPacket` 只生成 question/options/recommendation。

改造：

```go
func buildToolApprovalPacket(brief protocol.TaskBrief, call tool.Call) protocol.DecisionPacket {
    binding, _ := tool.BuildApprovalBinding(call, "")
    return protocol.DecisionPacket{
        ...
        ToolApprovalBinding: &protocol.ToolApprovalBinding{
            ToolName:            binding.ToolName,
            NormalizedInputHash: binding.NormalizedInputHash,
            PathDigest:          binding.PathDigest,
            InputPreview:        binding.InputPreview,
        },
    }
}
```

`CreateRequestFromDecision` 生成 UUID 后：

```go
req.ID = uuid.NewString()
binding.RequestID = req.ID
```

并持久化：

```text
tool_name
normalized_input_hash
path_digest
input_preview
```

### 6.2 恢复时绑定 approval context

当前 approval-gated resume 在批准后只放入：

```go
tool.WithApproval(resumeCtx, tool.ApprovalContext{
    RequestID:        approval.Request.ID,
    AllowDestructive: true,
})
```



应改为：

```go
tool.WithApproval(resumeCtx, tool.ApprovalContext{
    RequestID:           approval.Request.ID,
    AllowDestructive:    true,
    ToolName:            approval.Request.ToolApprovalBinding.ToolName,
    NormalizedInputHash: approval.Request.ToolApprovalBinding.NormalizedInputHash,
    PathDigest:          approval.Request.ToolApprovalBinding.PathDigest,
})
```

### 6.3 Dispatcher 执行前校验

在 `ClassifyCall` 中，现有逻辑是：

```go
if requiredPermission == PermApprovedDestructive {
    approval, ok := ApprovalFromContext(ctx)
    if !ok || !approval.AllowDestructive {
        tool_approval_required
    }
}
```



应改为：

```go
if requiredPermission == PermApprovedDestructive {
    approval, ok := ApprovalFromContext(ctx)
    if !ok || !approval.AllowDestructive {
        return tool_approval_required
    }

    binding, err := BuildApprovalBinding(call, approval.RequestID)
    if err != nil {
        return tool_approval_required / error
    }

    if approval.RequestID == "" ||
       approval.ToolName != binding.ToolName ||
       approval.NormalizedInputHash != binding.NormalizedInputHash ||
       approval.PathDigest != binding.PathDigest {
        classification.Action = CallActionToolApprovalRequired
        classification.Reason = "approval binding mismatch: approved request does not match current tool call"
        return classification
    }
}
```

### 6.4 fail-closed 行为

当 binding mismatch：

```text
- handler 不得执行。
- result.NeedsApproval = true 或 error result。
- Work 不应把 mismatch 当成普通成功。
- journal 写入 approval_binding_mismatch。
```

推荐新增 journal 事件：

```text
tool_approval_binding_mismatch
```

字段：

```json
{
  "task_id": "...",
  "approval_request_id": "...",
  "call_id": "...",
  "tool_name": "...",
  "expected_normalized_input_hash": "...",
  "actual_normalized_input_hash": "...",
  "expected_path_digest": "...",
  "actual_path_digest": "..."
}
```

---

## 7. UX 文案规范

### 7.1 统一结构

所有 destructive approval 和 manual forget 都用四段语言：

```text
预览：我准备执行/删除 X，尚未执行。
影响：这会影响哪些对象。
确认：确认执行/确认删除，或取消。
结果：已执行/未执行，并说明数量或目标。
```

### 7.2 Tool approval question 模板

替换 `toolApprovalQuestion` 的输出风格。

#### write_file 覆盖已有文件

```text
我准备执行一个受限文件操作，尚未执行。

操作：写入文件
目标：<path>
风险：这会覆盖一个已存在的文件。
影响：文件内容将被替换为本次工具输入中的新内容。

确认执行请点击“允许执行”；取消请点击“拒绝”。
```

#### edit_file replace_all

```text
我准备执行一个受限文件编辑，尚未执行。

操作：编辑文件
目标：<path>
风险：replace_all=true，可能同时修改多个位置。
影响：所有匹配的 old_string 都会被替换。

确认执行请点击“允许执行”；取消请点击“拒绝”。
```

#### bash destructive

保持现有 command preview，但改成统一风格：

```text
我准备执行一个受限命令，尚未执行。

操作：执行 bash 命令
命令：<command>
风险：命令可能删除、覆盖、移动或重置文件。

确认执行请点击“允许执行”；取消请点击“拒绝”。
```

### 7.3 Manual forget 文案调整

当前 manual forget preview 是：

```text
我找到了以下可删除候选，尚未执行删除：
...
确认删除请回复“确认删除”；取消请回复“取消”。
```



建议升级为：

```text
我准备执行一次长期记忆删除，尚未执行。

候选：
- <safe_summary>
- <safe_summary>

影响：确认后只会删除上面列出的 exact-node 目标。
确认删除请回复“确认删除”；取消请回复“取消”。
```

执行结果从：

```text
已删除 N 条确认的长期记忆。
```

升级为：

```text
已执行长期记忆删除：N 条。
```

保留“不展示 raw node id / preview hash / internal request id”的原则。

---

## 8. 审计设计

### 8.1 tool approval 审计

继续使用现有：

```text
approval_requests
pending_decisions
work journal JSONL
```

Work journal 当前已有 `tool_approval_intercepted` 事件。

新增字段：

```text
tool_approval_intercepted:
- approval_request_id, if known after PendingRegistry.Put
- tool_name
- call_id
- normalized_input_hash
- path_digest
- destructive_reason

tool_approval_resume:
- approval_request_id
- selected_option_id
- binding_match
- executed

tool_approval_binding_mismatch:
- approval_request_id
- tool_name
- expected hashes
- actual hashes
```

### 8.2 forget 审计保持现有范式

MemoryCore 已在 preview 和 execute 阶段记录 semantic decision audit，并包含 `PreviewHash`、candidate hash、selected node ids、policy snapshot。

本任务不改 MemoryCore audit 数据结构，只统一 EmoAgent 层对用户展示的语言风格。

---

## 9. 测试计划

### 9.1 Tool classifier tests

文件：

```text
internal/tool/builtin/destructive_file_classifier_test.go
```

必测：

```text
write_file:
- new docs/foo.md, create_dirs=false → non-destructive
- overwrite existing docs/foo.md → destructive
- new .env → destructive
- new secrets.json → destructive
- create_dirs=true to docs/new/foo.md → non-destructive
- create_dirs=true to .github/workflows/test.yml → destructive

edit_file:
- normal single replacement in docs/foo.md → non-destructive
- replace_all=true → destructive
- sensitive path .env → destructive
- old_string covers >=25% file → destructive
- replacement count >=5 → destructive
- old_string="" → handler rejects
```

### 9.2 Dispatcher approval binding tests

文件：

```text
internal/tool/dispatch_test.go
```

必测：

```text
- approved-destructive + exact binding → execute
- approved-destructive + missing binding → tool_approval_required
- tool_name mismatch → tool_approval_required
- normalized_input_hash mismatch → tool_approval_required
- path_digest mismatch → tool_approval_required
- same JSON input with different key order → same normalized_input_hash
```

### 9.3 Approval persistence tests

文件：

```text
internal/work/approval_test.go
```

必测：

```text
- CreateRequestFromDecision persists tool_name / normalized_input_hash / path_digest / input_preview.
- GetRequest / ListSessionApprovals / consumeRequestForResume round-trip binding fields.
- rejected request can still be consumed as rejection without executing pending tool.
```

### 9.4 Runtime resume tests

文件：

```text
internal/work/runtime_test.go
```

必测：

```text
- Resume approved tool call with exact binding executes once.
- Resume rejected approval does not execute.
- Resume approved request with mutated PendingToolCall.Input does not execute.
- Resume approved request with mutated PendingToolCall.Name does not execute.
```

### 9.5 Chat / UX tests

文件：

```text
internal/chat/engine_test.go
internal/memoryhost/facade_extraction_test.go 或 bridge test
```

必测：

```text
- approval_required WS event includes binding metadata but no raw full content.
- tool approval question contains “尚未执行”.
- manual forget preview contains “尚未执行”.
- manual forget execute notice uses new unified wording.
```

### 9.6 Recommended command

```bash
go test ./internal/tool/... ./internal/work/... ./internal/chat/... ./internal/memoryhost/...
```

最终建议跑：

```bash
go test ./...
```

---

## 10. 实施顺序

### Step 1 — 先做 classifier

```text
1. 新增 destructive_file_classifier.go。
2. 给 write_file / edit_file spec 挂 classifier。
3. 修复 edit_file old_string=""。
4. 补 builtin tests。
```

原因：这一步最小、风险最低，能马上让 `ClassifyCall` 现有链路生效。

### Step 2 — 做 approval binding

```text
1. 新增 tool.ApprovalBinding / hash / digest。
2. 扩展 ApprovalContext。
3. 扩展 protocol DecisionPacket / ApprovalRequest。
4. 扩展 storage migration。
5. 扩展 ApprovalService scan / create / list / consume。
6. 在 buildToolApprovalPacket 写入 binding。
7. 在 resume_tool.go 批准恢复时带 binding context。
8. 在 Dispatcher.ClassifyCall 执行前校验 binding。
```

原因：这一步改动面较大，但仍集中在 tool / work / protocol / storage。

### Step 3 — 统一 UX 与审计

```text
1. 改 toolApprovalQuestion / operation / recommendation 文案。
2. 改 manual forget preview / executed notice 文案。
3. journal 增加 binding 字段和 mismatch 事件。
4. 更新 Work 运行时文档。
```

原因：UX 和审计应在核心安全逻辑稳定后再收口。

---

