# Work 运行时实现说明

本文档说明 P6 最小化 Work 运行时的具体实现，记录与完整设计规格（`设计方案.md` §9）的差异，以及下一迭代的候选工作项。

---

## 1. 目的与范围

Work 是 EmoAgent 的任务执行子代理（subagent）。它由 Emotion 通过 `delegate_to_work` 工具异步触发，在**完全隔离的上下文**中执行读操作类任务，将结果以 `TaskReport` 结构体返回给 Emotion，对用户不可见。

P6 最小化实现的约束：

- `permission_scope` 硬锁为 `"read-only"`（双重门控：JSON Schema enum + Go 运行时校验）
- 内置工具仅 `read_file`
- 无 `DecisionRequest / DecisionResponse` 回路
- 无 Work 内部上下文压缩（依赖 `MaxInputTokens` 截断）
- 无流式进度推送

---

## 2. 与完整设计（设计方案.md §9）的差异

| 完整设计                         | 最小实现                                                 |
|----------------------------------|----------------------------------------------------------|
| 状态机含 `NeedDecision` 分支     | 未实现；Work 遇到障碍只能 partial 退出                    |
| permission_scope 支持三档        | 仅 `read-only`；`workspace-write` / `approved-destructive` 在 `ValidateAndComplete` 和 JSON Schema 双重拒绝 |
| Work 内部 compact（~40k token）  | 未实现；超出 `MaxInputTokens` 直接返回 partial             |
| DecisionRequest / DecisionResponse | 未实现（§12 future roadmap）                            |
| 流式 ProgressUpdate              | 未实现；`delegate_to_work` 调用同步阻塞                  |
| 丰富工具集                       | 仅 `read_file`（scope=work, read-only）                  |
| Emotion history 注入 Work        | **永远不注入**；Work 每次从空 `messages` 开始（硬约束）  |

---

## 3. 文件映射

### 3.1 `internal/work/` — Work 核心包

| 文件 | 职责 |
|------|------|
| [`brief.go`](../../internal/work/brief.go) | `ValidateAndComplete` — goal 长度校验、permission 双重门控、自动填充 TaskID / CreatedAt |
| [`context.go`](../../internal/work/context.go) | `BuildWorkSystem` — 组装 Work system prompt，无 Emotion persona 泄漏 |
| [`report.go`](../../internal/work/report.go) | `ParseOrFallback` — code fence 剥离、JSON 解析、失败时返回 partial 报告 |
| [`journal.go`](../../internal/work/journal.go) | `Journal` — nil-safe JSONL 审计日志，按日分目录 |
| [`runtime.go`](../../internal/work/runtime.go) | `Runtime.Run` — 空历史隔离的工具循环主体 |
| [`delegate_tool.go`](../../internal/work/delegate_tool.go) | `NewDelegateTool` — Emotion-scope `delegate_to_work` 工具工厂 |

### 3.2 `internal/tool/builtin/`

| 文件 | 职责 |
|------|------|
| [`read_file.go`](../../internal/tool/builtin/read_file.go) | `NewReadFileTool` — 路径安全的只读文件工具（scope=work） |

### 3.3 集成层

| 文件 | 变更说明 |
|------|----------|
| [`internal/config/config.go`](../../internal/config/config.go) | 新增 `WorkConfig` 结构体及 `ApplyDefaults()`；defaults: profile=`"default"`, MaxToolRounds=15, MaxInputTokens=100000, JournalDir=`"./logs/work"` |
| [`internal/app/app.go`](../../internal/app/app.go) | `resolveWorkProfile()` DB-first 解析 → 若 DB 无则从 `config.llm_profiles` 种入；`buildWorkClient()` 构造独立 LLM 客户端；`Run()` 中注册 `delegate_to_work`，Work 不可用时降级警告 |
| [`internal/context/assembler.go`](../../internal/context/assembler.go) | `buildEmotionSystemPrompt` 在 persona 基础提示末尾追加 `delegationGuideline`，指导 Emotion 何时调用 `delegate_to_work` |

---

## 4. Work 工具循环流程

```
Emotion 调用 delegate_to_work(brief)
    │
    ▼
ValidateAndComplete(brief)          ← goal 非空 ≤500字符，permission=read-only，自动填 TaskID
    │
    ▼
Open journal (logs/work/YYYY-MM-DD/<task_id>.jsonl)
    │
    ▼
journal.Write("task_start", ...)
    │
    ▼
Runtime.Run(ctx, brief, journal)
    │
    ├─ messages = []llm.Message{}   ← 空历史，硬隔离
    │
    ├─ loop (round=0; round < MaxToolRounds):
    │     │
    │     ├─ ctx.Err() 检查 → task_error + failedReport
    │     ├─ token 预算检查 → partialReport
    │     │
    │     ├─ LLM.ChatStream(system, messages, tools)
    │     │     ├─ err → task_error + failedReport
    │     │     └─ StopReason != "tool_use" → ParseOrFallback(resp.Content) → return
    │     │
    │     ├─ 追加 assistant 消息
    │     ├─ 提取 tool calls → journal("tool_call")
    │     ├─ Dispatcher.ExecuteAll → journal("tool_result", preview截500字符)
    │     └─ 追加 tool result 消息
    │
    └─ 超出 MaxToolRounds → partialReport
    │
    ▼
journal.Write("task_end", ...)
    │
    ▼
json.Marshal(report) → 返回给 Emotion
```

---

## 5. Journal 事件格式

每行一个 JSON 对象（JSONL），路径为 `<JournalDir>/YYYY-MM-DD/<task_id>.jsonl`。

### 字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `ts` | RFC3339 UTC | 事件写入时间 |
| `task_id` | string | 对应的 TaskBrief.TaskID |
| `kind` | string | 事件类型（见下表） |
| `round` | int | 当前工具循环轮次（`task_start` / `task_end` 为 0） |
| `payload` | object | 事件专属数据 |

### 事件类型

| `kind` | 触发点 | payload 关键字段 |
|--------|--------|-----------------|
| `task_start` | 进入 `Runtime.Run` 前 | TaskBrief 完整内容 |
| `tool_call` | LLM 返回 `tool_use` | `call_id`, `name`, `input` |
| `tool_result` | 工具执行完成 | `call_id`, `content_preview`（≤500字符）, `truncated`, `is_error` |
| `task_error` | ctx 取消 / LLM 错误 | `error`, `last_round` |
| `task_end` | `Runtime.Run` 返回后 | TaskReport 完整内容 |

> `tool_result` 的 `content_preview` 刻意截断（500字符），完整内容仅存在于 Work 的内存 messages 中。如需完整内容用于调试，可在 `runtime.go:truncateContent` 调整截断上限。

---

## 6. TaskReport 解析容错

`ParseOrFallback`（[`report.go`](../../internal/work/report.go)）处理三种 LLM 输出形式：

1. **裸 JSON** — 直接 `json.Unmarshal`
2. **code fence 包裹**（\`\`\`json … \`\`\` 或 \`\`\` … \`\`\`）— 剥离首尾 fence 后再解析
3. **解析失败** — 构造 `status=partial` 报告，将原始输出截断至 800 字符填入 `summary`

解析成功时，`task_id` 和 `goal` 强制覆盖为 brief 中的值（防止 LLM 幻觉篡改）。`status` 非合法值时降级为 `"partial"`。

---

## 7. 权限双重门控

```
┌─ JSON Schema (delegate_tool.go)
│   "permission_scope": {"type":"string","enum":["read-only"]}
│   → LLM 无法生成其他值（schema 验证拦截）
│
└─ Go 运行时 (brief.go ValidateAndComplete)
    if brief.PermissionScope != "read-only" → error
    → 即使绕过 schema 校验，handler 仍拒绝
```

---

## 8. read_file 安全限制

| 限制 | 实现 |
|------|------|
| 禁止绝对路径 | `filepath.IsAbs` |
| 禁止目录穿越 | `filepath.Clean` + `HasPrefix("..")` + `filepath.Rel` 二次校验 |
| 禁止目录 | `os.Stat + IsDir()` |
| 文件大小上限 | 1 MiB（`readFileMaxBytes = 1 << 20`） |
| 编码校验 | `utf8.Valid` — 拒绝二进制文件 |

---

## 9. 下一迭代候选项（参考设计方案.md §12）

- **DecisionRequest / DecisionResponse** — Work 遭遇边界情况时向 Emotion 上报并等待裁定
- **Work 内部上下文压缩** — 超过 soft token 阈值时对中间 messages 做 compact，当前仅靠 `MaxInputTokens` 截断
- **新增工具** — `list_dir`、`write_file`（需 `workspace-write` 权限）、`run_command`（需 `approved-destructive`）
- **permission_scope 放开** — 解除 minimal phase 硬锁，按 TaskBrief 实际值路由
- **流式进度** — ProgressUpdate 机制，让用户感知 Work 正在执行
