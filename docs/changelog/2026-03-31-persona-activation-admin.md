# Persona 默认切换与 Admin 页整合

**日期：** 2026-03-31  
**状态：** Completed

## 变更内容

### Persona 激活流程改为正式默认值切换

- 新增 `POST /api/personas/{name}/activate`
- 管理页不再通过聊天页 query 参数临时指定 Persona
- 选中 Persona 后，点击 `Activate` 会把它设为新的默认 Persona
- 默认 Persona 持久化到 `config_runtime["personas.default"]`
- 返回聊天页后，新会话会按新的默认 Persona 建立

### Admin 页入口收敛

- 移除重复的 `Chat Launcher`
- Persona 区工具栏改为：
  - `New Persona`
  - `Activate`
  - `Reload`
- `Activate` 只对“当前已选中且不是默认 Persona”的项可用
- Persona 列表中的 active badge 改为基于当前默认 Persona 显示

### 交互与状态同步

- `GET /api/personas` 返回的 `default` 字段现在直接驱动管理页 active 状态
- 管理页加载 Persona 列表后，会优先选中当前默认 Persona
- 激活 Persona 后，管理页会刷新列表并保持当前选中项
- 聊天页保留纯聊天职责，不再承担 Persona 切换入口

## 数据与行为上的影响

- Persona 的“当前生效项”现在有正式后端状态，而不是前端临时跳转状态
- 新的聊天会话会统一使用活动默认 Persona
- Admin 页和聊天页的职责边界更清晰：
  - Admin 页负责管理与切换
  - 聊天页负责连接与对话

## 测试与验证

- 新增 Persona 激活 API 测试
- 新增 `personas.default` runtime 持久化测试
- 新增路由分发测试，覆盖 `/api/personas/{name}/activate`
- `node --check` 通过：
  - `internal/web/static/admin.html`
  - `internal/web/static/index.html`
- `go test ./...` 通过

## 涉及文件

- 修改：`internal/app/app.go`
- 修改：`internal/app/app_test.go`
- 修改：`internal/web/api.go`
- 修改：`internal/web/api_test.go`
- 修改：`internal/web/static/admin.html`

