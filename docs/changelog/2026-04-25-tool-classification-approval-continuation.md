# 工具分类与审批恢复链路对齐

**日期：** 2026-04-25  
**状态：** Completed

## 变更内容

- 工具权限判定收敛到 `Dispatcher.ClassifyCall`
  - 一次完成工具查询、schema 校验、输入级破坏性判定、权限比较和 active approval context 检查
  - 删除旧的 `WouldNeedApproval` / `WouldNeedPermissionEscalation` 双预判链路
- `tool.Spec` 增加 `DestructiveClassifier`
  - 当前 bash 破坏性规则由 `builtin/bash.go` 自己声明
  - dispatcher 不再硬编码 `spec.Name == "bash"` 的破坏性判断
- Work runtime 每轮先整体分类再行动
  - `permission_escalation_required` 和 `tool_approval` 都由分类结果驱动暂停
  - `finish_task` / `request_decision` 与其他工具混用时，本轮不执行任何真实工具
- 修复审批恢复后的重复委派
  - 用户点击 `tool_approval` 后，系统执行层会直接用 `approval_request_id` 恢复 Work
  - 如果内部恢复已经返回终态 `TaskReport`，Emotion 后续叙述轮禁用工具，只负责向用户表达结果

## 更新范围

- `README.md`
- `docs/architecture/设计方案.md`
- `docs/architecture/架构.md`
- `docs/architecture/Work运行时实现说明.md`

## 影响说明

- 用户交互模型不变：普通人工决策仍由 Emotion 自然语言发起，具体工具审批仍走前端审批卡片
- 内部权限判定链路更短，审批暂停来源更明确
- 终态审批恢复结果不会再因为 Emotion 叙述轮带工具而被重复委派
