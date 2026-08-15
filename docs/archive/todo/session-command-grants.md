# Session 级同类命令授权计划

**状态：** TODO  
**日期：** 2026-04-25  
**背景：** 当前先实现一次性 `tool_approval` 与 `task_id/call_id/input hash` 绑定。本计划记录后续可选增强：用户可选择“在当前 Session 内允许某种命令”。

## 目标

在不削弱一次性审批安全性的前提下，支持用户对当前 Session 内的同类破坏性命令授予短期权限。例如用户批准一次删除 `docs/*.qq` 后，可以选择在本 Session 内允许后续同类删除命令直接执行。

## 设计原则

- 一次性审批和 Session 级授权分离，不复用同一语义。
- Session grant 只在当前 Session 内有效，必须支持过期、撤销和可选使用次数限制。
- `read-only` 不允许被 Session grant 覆盖。
- 只能匹配 classifier 能结构化识别的命令；无法解析、复杂 shell、变量展开、管道、危险通配符等情况继续要求一次性审批。
- 当前被冻结的 call 仍通过一次性 approval binding 恢复执行；创建 grant 只是影响后续同类 call。

## 数据模型草案

新增 `approval_grants`：

```sql
CREATE TABLE approval_grants (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    status TEXT NOT NULL,
    tool_name TEXT NOT NULL,
    command_class TEXT NOT NULL,
    target_scope_json TEXT NOT NULL,
    created_from_approval_id TEXT NOT NULL DEFAULT '',
    max_uses INTEGER NOT NULL DEFAULT 0,
    used_count INTEGER NOT NULL DEFAULT 0,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
```

一次性 `approval_requests` 仍应先补齐绑定字段：

- `tool_call_id`
- `tool_name`
- `tool_input_hash`

## 运行时规则

- `workspace-write` + destructive classifier 命中：
  - 有 matching session grant：执行。
  - 无 matching session grant：暂停为 `permission_escalation_required`。
- `approved-destructive` + destructive classifier 命中：
  - 有 matching session grant：执行。
  - 无 matching session grant：进入 `tool_approval` 审批卡片。
- `tool_approval` 卡片未来可增加选项：
  - 允许本次
  - 本 Session 内允许同类命令
  - 拒绝

## 匹配策略

`bash` destructive classifier 需要产出或复用结构化 intent：

```go
type CommandIntent struct {
    Destructive bool
    Kind        string   // delete_file, overwrite_file, move_file
    Targets     []string // normalized workspace-relative paths or safe patterns
    Reason      string
}
```

Session grant 匹配时至少检查：

- `session_id`
- `tool_name`
- `command_class`
- 目标路径或安全 pattern 是否落在 grant scope 内
- grant 未过期、未撤销、未超过 max uses

## 代码落点

- `internal/work/approval.go`：approval request 绑定字段与 grant 创建/查询/消费。
- `internal/work/pending.go`：创建 `tool_approval` 时从 `PendingToolCall` 写入一次性绑定。
- `internal/work/resume_tool.go`：消费 approval 前校验 call binding；用户选择 Session grant 时创建 grant。
- `internal/tool/dispatch.go`：`ClassifyCall` 或其调用链在 destructive 判定后查询 active grant。
- `internal/tool/builtin/bash.go`：输出结构化 destructive intent，用于 grant 匹配。
- `internal/protocol/types.go`：表达 approval option 或 grant 选择。
- 前端审批卡片：展示“本 Session 内允许同类命令”。

## 推荐实施顺序

1. 先实现一次性 approval 绑定到 `task_id/call_id/input hash`。
2. 再抽出结构化 destructive intent。
3. 新增 `approval_grants` 和匹配器。
4. 最后扩展前端审批卡片选项。

## 测试重点

- grant 只在同一 Session 内生效。
- grant 不覆盖 `read-only`。
- 同类命令匹配成功时不弹审批。
- 不同路径、不同命令类型、不同工具名不匹配。
- 复杂或无法解析的 shell 命令不匹配 grant。
- grant 过期、撤销、超过使用次数后重新触发审批。
