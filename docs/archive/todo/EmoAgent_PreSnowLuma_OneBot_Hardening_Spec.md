# EmoAgent SnowLuma 接入前补强实施 Spec

> Target repo: `LongYiSang/EmoAgent`  
> Suggested path: `docs/architecture/pre_snowluma_onebot_hardening_spec.md`  
> Baseline: OneBot v11 adapter 已在 `9ee7d0a902921b2e5f2c04c653b6f63bc0e787e4` 完成。  
> Scope: SnowLuma 真实联调前的小范围补强。  
> Non-goal: 不实现群聊接收、不实现媒体下载/发送、不实现 OneBot v12、不重写 OneBot adapter、不改变 `PlatformGateway` 作为唯一入口的架构。

---

## 0. 当前状态

当前架构已经成型：

```text
OneBot event
  → internal/platform/onebotv11.Adapter
  → platform.InboundMessage
  → app.PlatformGateway
  → CommandService / Conversation Binding / Chat Engine
  → platform.OutboundEvent
  → onebotv11.Sink
  → OneBot action
```

已有能力：

- `InboundMessage.OriginScope / AcceptedReason`
- `HandleResult.Ignored`
- `PlatformService.Configure / InstallHTTPRoutes / Start / Stop`
- `ws_client` 与 `ws_reverse`
- `generic / napcat / snowluma` profile
- 默认只接收 private message，忽略 group message
- text-only message render，非文本段 placeholder
- `send_private_msg / send_group_msg` action builder
- echo correlation / request timeout
- OneBot 私聊命令集成测试与 WS transport 测试

本轮目标不是大改，而是做真实 SnowLuma 联调前的可观测性、兼容性和文档补强。

---

## 1. 不变量

1. OneBot 入站不能绕过 `app.PlatformGateway.HandleInbound`。
2. `source_type` 必须保持 `onebot`，不要使用 `snowluma`。
3. `instance_id` 必须是稳定逻辑 ID，推荐 `qq-main`。
4. MVP 不接收群聊，`group` message 必须 ignored，不得创建 session、receipt 或调用 LLM。
5. MVP 不处理媒体，非文本段继续 placeholder，不填充 `InboundMessage.Parts`。
6. 反向 WS 只支持 `Universal`。
7. 默认本机部署，仅保留可选 Bearer token，不做复杂公网安全策略。

---

## 2. Phase A：OneBot action 参数类型兼容

### 问题

当前 `sendPrivateMsgRequest(userID string, ...)` 和 `sendGroupMsgRequest(groupID string, ...)` 可能把 `user_id/group_id` 作为 string 放入 params。SnowLuma 可接受数字字符串，但其它 OneBot 实现可能更严格。

### 目标

新增 best-effort numeric conversion：

```go
func onebotIDParam(id string) any {
    id = strings.TrimSpace(id)
    if n, err := strconv.ParseInt(id, 10, 64); err == nil && n > 0 {
        return n
    }
    return id
}
```

用于：

```go
sendPrivateMsgRequest
sendGroupMsgRequest
```

### 验收

- `send_private_msg` 对 `"10001"` 输出 JSON number `10001`。
- 非数字 ID 不 panic，保留 string。
- 现有 fake transport 测试适配新的 param 类型。
- `go test ./internal/platform/onebotv11 ./internal/app` 通过。

---

## 3. Phase B：SnowLuma-shaped fixture 测试

### 目标

补充更贴近 SnowLuma 的事件/响应 fixture，避免只测手写最小 JSON。

### 新增测试文件

```text
internal/platform/onebotv11/snowluma_profile_test.go
```

### 测试项

1. `TestSnowLumaPrivateMessageArray`
   - 输入：`post_type=message`, `message_type=private`, `sub_type=friend`, `message=[{type:"text",data:{text:"hello"}}]`, `raw_message="hello"`, sender 信息。
   - 期望：accepted，`Text=hello`，`SourceType=onebot`，`PlatformID=qq`，`OriginScope=private`。

2. `TestSnowLumaMessageSentIgnored`
   - 输入：`post_type=message_sent`。
   - 期望：ignored，不进入 Gateway。

3. `TestSnowLumaGroupMessageIgnored`
   - 输入：`post_type=message`, `message_type=group`。
   - 期望：ignored，不进入 Gateway。

4. `TestSnowLumaImagePlaceholder`
   - 输入：image segment。
   - 期望：`Text` 包含 `[图片]`，`Parts` 为空。

5. `TestSnowLumaSendPrivateActionShape`
   - 输入：private `OutboundEvent`。
   - 期望：`action=send_private_msg`，`params.user_id` 为 number，`message` 为 array text segment。

### 验收

- 新测试通过。
- 不引入真实 SnowLuma 依赖。
- 不调用网络。

---

## 4. Phase C：平台状态诊断 API

### 问题

真实联调时，如果 SnowLuma 连不上，当前主要靠日志排查。已有 `TransportStatus`，应暴露为只读诊断 API。

### 目标

新增：

```text
GET /api/platforms/status
```

返回示例：

```json
{
  "enabled": true,
  "adapters": [
    {
      "id": "qq-main",
      "kind": "onebot_v11",
      "enabled": true,
      "implementation": "snowluma",
      "source_type": "onebot",
      "platform_id": "qq",
      "instance_id": "qq-main",
      "transport": {
        "mode": "ws_reverse",
        "state": "connected",
        "url": "",
        "self_id": "123456",
        "connected": true
      },
      "routing": {
        "private_enabled": true,
        "group_enabled": false,
        "ignore_self_messages": true
      },
      "auth": {
        "access_token_configured": true
      }
    }
  ]
}
```

### 实现建议

新增：

```go
func (s *PlatformService) Status() PlatformStatus
```

`platform.Manager` 可补：

```go
func (m *Manager) List() []RegisteredAdapter
```

或由 `PlatformService` 自己维护 adapter id/config/status。

OneBot adapter 增加：

```go
func (a *Adapter) Status() onebotv11.Status
```

Web API：

```text
internal/web/api.go        HandleGetPlatformStatus
internal/app/server.go     mux.HandleFunc("GET /api/platforms/status", api.HandleGetPlatformStatus)
```

### 安全要求

- 不返回 access token 明文。
- 只返回 `access_token_configured: true/false`。

### 验收

- platforms disabled 时返回 `enabled=false, adapters=[]`。
- ws_reverse 未连接时 `state=waiting, connected=false`。
- ws_reverse 连接后 `self_id` 可见。
- 不返回 token 明文。
- 增加 API 或 service 层单测。

---

## 5. Phase D：配置诊断与日志补强

### 配置诊断

保持现有硬校验，并补充更友好的错误/警告：

- `implementation` 必须是 `generic | napcat | snowluma`。
- `transport.mode` 必须是 `ws_client | ws_reverse`。
- `ws_client` 需要 `url`。
- `ws_reverse` 需要 `reverse_path` 或使用默认路径。
- `routing.group_enabled` 默认 false。
- `message.input_format/output_format` 合法。
- `implementation=snowluma` 时，空 `input_format/output_format` 默认 array。
- `ws_reverse` 下配置 `url` 可忽略但记录 warning。
- `ws_client` 下配置 `reverse_path` 可忽略但记录 warning。
- `platforms.enabled=true` 但 adapters 为空，作为 config issue 或 warning，不阻止启动。

### 日志增强

新增结构化日志，但不要输出 token 和完整消息文本：

```text
onebot adapter configured id=qq-main implementation=snowluma mode=ws_reverse
onebot reverse route installed path=/api/platforms/onebot/v11/qq-main/ws
onebot reverse ws connected id=qq-main self_id=123456
onebot reverse ws disconnected id=qq-main self_id=123456
onebot inbound accepted id=qq-main message_type=private external_message_id=...
onebot inbound ignored id=qq-main reason=group_disabled
onebot action failed action=send_private_msg retcode=... wording=...
onebot action timeout action=send_private_msg echo=...
```

### 验收

- token 不出现在日志。
- 日志包含 adapter id、mode、self_id。
- group ignored 有 reason。
- action failed / timeout 可定位。
- `go test ./...` 通过。

---

## 6. Phase E：SnowLuma 反向 WS 文档

新增：

```text
docs/architecture/onebot_v11_snowluma_reverse_ws_guide.md
```

内容应包含：

- EmoAgent 配置
- SnowLuma `config/onebot_<uin>.json` 配置
- token 对齐
- URL / port / adapter id
- 启动顺序
- `/api/platforms/status` 检查
- `/sid`、普通私聊、群聊 ignored 测试
- 常见问题

### 验收

- 文档不包含真实 token。
- 使用 `dev-token` 或环境变量示例。
- 明确 MVP 限制：私聊文本、群聊忽略、无媒体。

---

## 7. 总体验收标准

完成后应满足：

1. `go test ./...` 通过。
2. `send_private_msg / send_group_msg` 数字 ID 优先作为 number。
3. SnowLuma-shaped private event 能通过 mapper 并进入 PlatformGateway。
4. SnowLuma-shaped `message_sent` 与 group message 被 ignored。
5. `/api/platforms/status` 可显示 OneBot adapter 状态。
6. 反向 WS 连接时 status 能显示 `self_id` 和 `connected=true`。
7. 日志能定位连接、忽略、发送失败，但不泄露 token 和完整消息文本。
8. 仓库中存在 SnowLuma reverse WS 接入文档。
9. 不新增群聊处理。
10. 不新增媒体处理。
11. 不改变 `source_type=onebot` 的稳定 Origin 策略。

---

## 8. Codex 修改边界

允许修改：

```text
internal/platform/
internal/platform/onebotv11/
internal/app/platform_service.go
internal/app/server.go
internal/web/api.go
internal/config/
docs/architecture/
```

谨慎修改：

```text
internal/chat/
internal/conversation/
internal/storage/
```

禁止本轮修改：

```text
MemoryCore 接入逻辑
长期记忆 extraction/consolidation/retrieval
Agent Affect
插件运行时
Work runtime
前端大改
```

---

## 9. 暂停条件

Codex 遇到以下情况应暂停并报告：

1. 必须绕过 `PlatformGateway` 才能实现功能。
2. SnowLuma 真实行为与 OneBot v11 标准明显不兼容，需要扩大 profile。
3. 需要接收群聊才能完成验收。
4. 需要下载或发送媒体才能完成验收。
5. 需要暴露 token 或存储敏感消息全文才能做诊断。
6. `go test ./...` 出现与本改动无关的大面积失败。
