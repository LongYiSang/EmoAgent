# Phase 3: 主循环与交互闭环

**日期：** 2026-03-30
**状态：** Completed

## 变更内容

### 对话主循环

- 在 `internal/chat/engine.go` 新增 Chat Engine
- 支持创建会话、注入 persona system prompt、加载最近消息历史、调用 LLM 流式回复
- 用户消息和助手消息都会持久化到 SQLite，并在回复完成后更新 session 时间

### WebSocket 交互

- 在 `internal/chat/handler.go` 新增 WebSocket Handler
- 建立连接后发送 greeting
- 支持 `message / ping` 输入消息
- 支持 `stream_start / stream_delta / stream_end / error / pong` 输出消息
- 单连接内按顺序写回流式内容，形成最小可用聊天协议

### 最小 WebUI

- 新增 `internal/web/embed.go`
- 新增 `internal/web/static/index.html`
- 使用 `embed.FS` 提供静态页面
- 聊天页支持消息气泡、流式展示、发送中禁用、连接状态提示、断线自动重连

### App 接入

- 改造 `internal/app/app.go`
- `Run()` 不再是占位阻塞，而是启动 HTTP 服务并挂载 `/ws` 与静态页面
- 当 LLM client 未初始化时启动直接失败，避免进入无效聊天页
- 新增 `GetDefaultPersonaName()` 供聊天层读取默认 persona

### Storage 扩展

- 扩展 `internal/storage/db.go`
- 新增 `sessions/messages` 的 CRUD 封装：
  - `CreateSession`
  - `GetSession`
  - `AddMessage`
  - `GetRecentMessages`
  - `UpdateSessionTimestamp`
- 消息时间使用高精度 UTC 时间戳，避免秒级排序不稳定
- 限制 message role 为 `user` 或 `assistant`

### 配置与 Persona 修复

- 补充 `config.yaml` 中已有配置项的占位说明和注释
- 修复 persona 加载索引逻辑：
  - `personas.default: "default"` 现在按文件名 `default.yaml` 匹配
  - `Persona.Name` 继续保留为角色展示名，如 `Emo`
- 解决进入页面后出现 `default persona not found` 并持续重连的问题

## 测试与验证

### 自动化测试

- 新增 `internal/storage/session_test.go`
- 新增 `internal/chat/engine_test.go`
- 新增 `internal/chat/handler_test.go`
- 新增 `internal/app/app_test.go`
- `go test ./...` 通过
- `go build -o ./bin/emoagent ./cmd/emoagent` 通过

### 手工验证

- 启动服务后可正常进入聊天页面
- WebSocket 可建立连接并收到 greeting
- 对话可正常发送并接收流式回复
- `default persona not found` 问题已修复

## 涉及文件

- 修改：`config.yaml`
- 修改：`internal/app/app.go`
- 修改：`internal/config/persona.go`
- 修改：`internal/config/persona_test.go`
- 修改：`internal/storage/db.go`
- 新增：`internal/app/app_test.go`
- 新增：`internal/chat/engine.go`
- 新增：`internal/chat/engine_test.go`
- 新增：`internal/chat/handler.go`
- 新增：`internal/chat/handler_test.go`
- 新增：`internal/storage/session_test.go`
- 新增：`internal/web/embed.go`
- 新增：`internal/web/static/index.html`
