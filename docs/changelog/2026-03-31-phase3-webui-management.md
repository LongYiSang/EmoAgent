# Phase 3: WebUI 管理页与 LLM Profile 管理

**日期：** 2026-03-31  
**状态：** Completed

## 变更内容

### 管理页与管理 API

- 新增 `internal/web/static/admin.html`，提供独立管理页
- 新增 `internal/web/api.go`，提供 LLM Profile 与 Persona 的 REST API
- 聊天页 `internal/web/static/index.html` 新增管理页入口
- 管理页当前覆盖两类资源：
  - LLM Profile 列表、详情、创建、更新、删除、激活切换
  - Persona 列表、详情、创建、更新、删除

### LLM 配置从单实例改为多 Profile

- 保留 `config.yaml.llm` 作为首次启动时的 bootstrap seed
- 新增数据库表 `llm_profiles`，用于持久化多个 LLM 配置
- 使用 `config_runtime["llm.active_profile"]` 持久化当前活动配置
- 当前实现里，LLM Profile 的稳定标识使用 `name`
- API 层的 `{id}` 也映射到 `profile.Name`
- 每个 Profile 可配置：
  - `provider`
  - `base_url`
  - `api_key_env`
  - `model`
  - `summary_model`
  - `max_tokens`
  - `temperature`

### LLM 客户端与运行时切换

- `internal/llm/client.go` 支持优先从 `api_key_env` 指定的环境变量读取 API Key
- 若未显式配置 `api_key_env`，仍回退到按 provider 的默认环境变量
- `internal/chat/engine.go` 新增运行时热更新能力
- 激活新的 LLM Profile 后，新消息会立即使用新配置
- 更新当前活动 Profile 时，会同步热更新聊天引擎

### 启动行为调整

- `internal/app/app.go` 不再要求启动时必须存在可用 LLM client
- 即使当前 LLM 配置缺失或失效，HTTP 服务与管理页仍可启动
- 聊天链路在没有可用活动 LLM 时返回明确错误，而不是让整个服务无法进入

### Persona 管理补齐

- `internal/config/persona.go` 新增 Persona 文件保存与删除能力
- `internal/storage/db.go` 补齐 Persona 读取与删除能力
- 管理页支持 Persona 的创建、编辑、删除
- 继续保留默认 Persona 保护逻辑，避免误删活动默认人格

### Persona 管理页修复

- 修复“选择默认 Persona 报 `Persona not found`”问题
- 根因是前端此前错误地把展示名当作路由 key 使用
- 现在 Persona 前后端统一区分：
  - `key`：内部标识、路由参数、文件名
  - `name`：展示名
- 修复“新建 Persona 保存时报 `key is required`”问题
- 管理页表单已拆分为独立的 `Key` 和 `Display Name`
- 后端 `POST /api/personas` 也增加了兼容兜底：缺少 `key` 时回退使用 `name`

## 数据与行为上的影响

- 现在可以在管理页维护多个 LLM 提供商配置，并在其中切换活动项
- 当前聊天链路始终只使用一个活动 LLM Profile
- 新建会话与后续新消息会跟随当前活动 LLM 配置
- 当前仍然没有“聊天页按会话切换 Persona”的能力
- 聊天页仍然使用全局默认 Persona 建立新会话

## 测试与验证

- 补充和更新了 storage、config、llm、chat、app、web 相关测试
- `go test ./...` 通过
- `go build -o ./bin/emoagent ./cmd/emoagent` 通过
- 管理页相关手工路径已覆盖：
  - LLM Profile 创建、编辑、激活、删除
  - Persona 创建、编辑、删除
  - 默认 Persona 选择与新建 Persona 保存

## 涉及文件

- 修改：`config.yaml`
- 修改：`.env.example`
- 修改：`internal/app/app.go`
- 修改：`internal/app/app_test.go`
- 修改：`internal/chat/engine.go`
- 修改：`internal/chat/engine_test.go`
- 修改：`internal/config/config.go`
- 修改：`internal/config/config_test.go`
- 修改：`internal/config/persona.go`
- 修改：`internal/config/persona_test.go`
- 修改：`internal/llm/client.go`
- 修改：`internal/storage/db.go`
- 修改：`internal/storage/db_test.go`
- 修改：`internal/storage/schema.go`
- 修改：`internal/web/static/index.html`
- 新增：`internal/web/api.go`
- 新增：`internal/web/api_test.go`
- 新增：`internal/web/static/admin.html`

