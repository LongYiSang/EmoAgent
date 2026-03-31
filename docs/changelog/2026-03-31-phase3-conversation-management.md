# Phase 3: 对话管理与聊天页恢复

**日期：** 2026-03-31  
**状态：** Completed

## 变更内容

### Persona 主键语义统一

- `personas` 表完成主键迁移，数据库主键从 `name` 改为 `key`
- `name` 现在只作为展示字段保留，不再承担稳定标识职责
- App 同步 persona 到数据库时，统一使用 persona key 持久化
- 这次迁移为后续 session 恢复、默认 Persona 匹配和会话列表过滤统一了标识语义

### Session 查询与删除能力

- 扩展 `internal/storage/db.go`
- 新增：
  - `SessionSummary`
  - `ListSessions()`
  - `GetLatestSession()`
  - `DeleteSession()`
  - `GetAllMessages()`
- “最新 session”与 session 列表都显式排除了空 session
- `DeleteSession` 会同时删除 session 与其消息
- user message 写入后立即更新 `sessions.updated_at`，使“最近对话”语义更准确

### App 与 REST API

- 扩展 `internal/app/app.go`
- 新增：
  - `ListSessions()`
  - `GetLatestSession()`
  - `GetSessionDetail()`
  - `DeleteSession()`
- 新增 `ErrSessionNotFound`
- 扩展 `internal/web/api.go`
- 新增：
  - `GET /api/sessions`
  - `GET /api/sessions/latest`
  - `GET /api/sessions/{id}`
  - `DELETE /api/sessions/{id}`
- 删除不存在的 session 现在返回 `404`

### WebSocket 会话恢复协议

- 扩展 `internal/chat/engine.go`
- 新增：
  - `ResumeSession()`
  - `GetHistory()`
- 扩展 `internal/chat/handler.go`
- WebSocket 连接建立后现在先发送 `session_ready`
- 当 `session_id` 合法且 persona key 匹配时，恢复已有 session
- 恢复成功且有历史消息时发送 `history`，不再发送 greeting
- 只有新 session 或恢复到空历史时才发送 greeting

### 聊天页会话管理

- 重写 `internal/web/static/index.html` 的聊天页状态流转
- 页面初始化顺序现在是：
  - 优先读取 URL 中的 `session_id`
  - 否则读取 admin 设置的默认 Persona
  - 再读取该 Persona 的最近非空 session
- 聊天页新增 session 侧边栏
- 支持：
  - 查看当前 Persona 的历史非空 session
  - 点击切换并恢复历史对话
  - `+ New Chat` 创建新会话
  - 删除 session
- 当前 session 与 persona 会同步回 URL query 参数

### 聊天页滚动与输入区修复

- 修复长对话时消息区把布局撑开的问题
- 消息区现在拥有独立滚动区域
- 输入框固定在底部，不会因消息过长而被挤出可视范围
- 修复点主要集中在聊天页 grid 布局的高度约束与滚动容器定义

## 数据与行为上的影响

- 默认 Persona 仍然只由 admin 设置，聊天页不会写回 `personas.default`
- `sessions.persona`、默认 Persona、会话恢复校验现在都以 persona key 为准
- 空 session 不会参与“自动恢复最近会话”
- 一个 Persona 如果存在本地历史对话，进入聊天页时优先展示历史消息而不是 greeting

## 测试与验证

- 新增和更新了 storage、app、web、chat 相关测试
- 覆盖：
  - persona key 主键迁移后的 CRUD 语义
  - 非空 session 列表与最新 session 查询
  - session 删除与 `404` 行为
  - engine 的 session 恢复与历史读取
  - WebSocket 的 `session_ready / history / greeting` 分流
- `go test ./...` 通过
- 手工验证已覆盖：
  - Persona 主键迁移成功
  - 聊天页会话恢复与历史列表
  - 长对话场景下消息区滚动与输入框可达性

## 涉及文件

- 修改：`docs/todo/phase3-conversation-management.md`
- 修改：`internal/apperrors/errors.go`
- 修改：`internal/app/app.go`
- 修改：`internal/app/app_test.go`
- 修改：`internal/chat/engine.go`
- 修改：`internal/chat/engine_test.go`
- 修改：`internal/chat/handler.go`
- 修改：`internal/chat/handler_test.go`
- 修改：`internal/storage/db.go`
- 修改：`internal/storage/db_test.go`
- 修改：`internal/storage/schema.go`
- 修改：`internal/storage/session_test.go`
- 修改：`internal/web/api.go`
- 修改：`internal/web/api_test.go`
- 修改：`internal/web/static/index.html`
