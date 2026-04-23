# Approval 恢复链路文档对齐

**日期：** 2026-04-23  
**状态：** Completed

## 变更内容

- 对齐 `tool_approval` 的当前实现：审批请求由交互层直接展示为审批卡片
- 明确区分两类恢复路径：
  - 普通决策 / `permission_escalation_required`：仍由 Emotion 调用 `resume_work`
  - `tool_approval`：用户点击审批后，由系统执行层直接携带 `approval_request_id` 续跑 Work
- 保持架构原则不变：
  - Emotion 仍然是唯一对外会话拥有者
  - Emotion 仍负责恢复后的对外表达
  - 本次只是把“恢复调用由谁提交”写清楚，不是把用户会话所有权转给系统层

## 更新范围

- `README.md`
- `docs/architecture/设计方案.md`
- `docs/architecture/架构.md`
- `docs/architecture/Work运行时实现说明.md`
- `docs/todo/p6-work-work-emotion-work-emo-piped-valley.md`

## 影响说明

- 这是文档对齐，不包含新的运行时行为变更
- 目的是避免继续按旧文档理解为“所有审批恢复都必须先经过一轮 Emotion LLM”
