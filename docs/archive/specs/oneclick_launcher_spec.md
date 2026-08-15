# EmoAgent 一键启动器（emo up / down / status）设计规格

- 日期：2026-07-26
- 状态：设计已确认，待实施
- 形态：B——手动触发的一键启动（机器非常开机，不做开机自启）

## 1. 背景

EmoAgent 的日常使用链路需要手动完成：启动 SnowLuma（`launcher.bat`）→ 打开 `http://127.0.0.1:5099` 控制台输密码 → 在进程注入页人工探测并加载 Agent 用的 QQ 进程 → 用 GoLand 重新构建前后端并启动 EmoAgent → 在 WebUI 确认 adapter 连接。摩擦过高导致作者本人使用频率下降，项目数据与反馈随之枯竭。

本规格定义一组脚本，把上述链路压缩为：**双击一个图标 → （唯一人工动作）在 QQ 登录窗选择 Agent 账号 → 收到"已上线"通知 → 直接在 QQ 聊天**。

## 2. 已验证的链路事实

以下事实均已实际核验，设计建立在其上：

1. **启动顺序无关**：SnowLuma 以 wsClient 身份主动连接 EmoAgent（`ws://127.0.0.1:8080/api/platforms/onebot/v11/qq-main/ws`，`reconnectIntervalMs: 5000`）；EmoAgent 侧为 `ws_reverse` 被动监听。任一侧后启动或中途重启均自动重连。
2. **SnowLuma 是普通 Node 应用**：`launcher.bat` 内容仅为 `node ./index.mjs`。可用隐藏窗口方式静默启动，控制台网页仅是管理界面，日常运行不需要。
3. **SnowLuma 控制台操作全部有 HTTP API**（从 server bundle 提取）：
   `/api/login`、`/api/logout`、`/api/auth/state`、`/api/processes`、`/api/processes/:pid/load`、`/api/processes/:pid/probe-login`、`/api/processes/:pid/unload`、`/api/qq-list`、`/api/status`、`/api/connections` 等。
4. **注入可先于登录**：控制台文案"加载 SnowLuma 后会监听登录状态，登录后自动接入 OneBot 流程"。即：对进程注入后再登录账号，OneBot 流程自动衔接。
5. **EmoAgent 无需 IDE 启动**：`bin/emoagent.exe` 已存在；`.env` 由程序自身加载（godotenv，见 `internal/app/bootstrap.go`），sidecar 与插件进程由程序自我监管。以项目根为 cwd 运行 exe 即可。
6. **EmoAgent 有平台状态接口**：`GET /api/platforms/status`（`internal/app/server.go:147`），可确认 adapter 连接状态。
7. `data/` 已在 `.gitignore` 中，适合存放凭据、状态与日志。

## 3. 目标与非目标

### 目标

- `emo up`：幂等地把三个环节（SnowLuma → Bot QQ 注入登录 → EmoAgent）补齐到就绪，完成后 Windows 通知。
- `emo down`：一键全停（EmoAgent、Bot QQ 注入与进程、SnowLuma），个人 QQ 永不触碰。
- `emo status`：只读检查全链路状态，输出各环节 ✓/✗ 与修复提示。
- 桌面快捷方式（隐藏窗口）作为主要入口；脚本也可在终端直接运行。

### 非目标（明确不做）

- 不使用 SnowLuma 的 `hookAutoLoad`（可能注入所有 QQ 进程，包括用户个人 QQ，方向错误）。
- 不做开机自启 / Windows 服务化（用户选择形态 B；未来升级只需把 `emo up` 挂入计划任务）。
- 不做任何 UI 自动化（模拟点击 QQ 登录窗等）。QQ 账号选择保留为人工动作，用户已确认接受。
- 不修改 SnowLuma 的 `0.0.0.0` 绑定（用户计划未来将整套部署到局域网内低功耗笔记本，需要保留外部可达性）。
- 不实现 QQ 账号自动登录逻辑；若用户日后在 QQ 端为 Bot 账号勾选"自动登录"，流程自动受益，无需改脚本。
- 不支持群聊、多 Bot 账号、远程主机编排（见 §10）。

## 4. 组件与文件布局

```
scripts/launcher/                  # 进 git
  emo.ps1                          # 入口分发：emo.ps1 up|down|status [-Rebuild]
  emo-common.ps1                   # 共享函数：配置加载、SnowLuma API 客户端、通知、日志
  launcher.config.example.json     # 配置样例（含注释性字段说明）
  New-Shortcuts.ps1                # 生成桌面快捷方式（隐藏窗口调用 emo.ps1 up / down）

data/launcher/                     # 不进 git（data/ 已 ignore）
  launcher.config.json             # 实际配置（首次运行时从 example 复制并引导填写）
  snowluma.cred                    # SnowLuma 控制台密码（Windows DPAPI 加密，仅当前用户可解）
  state.json                       # 脚本启动的进程记录（QQ PID、EmoAgent PID、时间戳）
  logs/emo-YYYYMMDD.log            # 脚本运行日志
  logs/emoagent-console.log        # EmoAgent 进程 stdout/stderr 重定向
```

### 配置 schema（launcher.config.json）

```json
{
  "snowluma": {
    "dir": "D:\\Dev\\Deploy\\SnowLuma-v1.11.4-win-x64",
    "base_url": "http://127.0.0.1:5099",
    "ready_timeout_seconds": 30
  },
  "qq": {
    "exe_path": "C:\\Program Files\\Tencent\\QQNT\\QQ.exe",
    "bot_uin": "1765843429",
    "login_wait_seconds": 300
  },
  "emoagent": {
    "project_dir": "D:\\Dev\\Project\\Agent\\EmoAgent",
    "exe": "bin\\emoagent.exe",
    "config": "config.yaml",
    "base_url": "http://127.0.0.1:8080",
    "ready_timeout_seconds": 120
  }
}
```

所有地址与路径均可配置——未来迁移到局域网笔记本时，`base_url` 可指向远程主机（进程启动部分届时不适用，见 §10）。

## 5. emo up 编排

每一步先检查、已就绪则跳过（幂等）；轮询间隔 2s。

1. **配置与凭据**：加载 `data/launcher/launcher.config.json`（缺失则从 example 复制并提示填写后退出）；解密 SnowLuma 密码（缺失则提示"首次运行请在终端执行以录入密码"，交互录入并 DPAPI 落盘）。
2. **SnowLuma 服务**：`GET {snowluma.base_url}/api/auth/state` 不可达 → 以 `snowluma.dir` 为 cwd 隐藏启动 `node.exe index.mjs`，等待就绪（超时 `ready_timeout_seconds`）。
3. **登录 SnowLuma API**：`POST /api/login`，会话仅保存在本次运行内存中。
4. **Bot QQ 检查**：查询 `/api/qq-list` 与 `/api/processes`，若 `bot_uin` 已在线 → 跳到第 6 步。
5. **注入与登录**（唯一可能需要人工的环节）：
   a. 启动 `qq.exe_path`，记录新进程 PID（写入 `state.json`）。
   b. `POST /api/processes/{PID}/load` 定向注入——**只注入脚本自己启动的进程**，个人 QQ 无论是否在跑均不受影响。
   c. Windows 通知："请在 QQ 登录窗选择 Agent 账号"。
   d. 轮询等待 `bot_uin` 在该 PID 上线（上限 `login_wait_seconds`）。
   e. **错账号防护**：若该 PID 上线的 UIN ≠ `bot_uin`（用户误选个人账号），立即 `POST /api/processes/{PID}/unload` 并通知，不杀进程（其中已登录用户个人账号）。
6. **EmoAgent**：`GET {emoagent.base_url}/api/platforms/status` 不可达 → 以 `project_dir` 为 cwd 隐藏启动 `{exe} --config {config}`（stdout/stderr 重定向到日志），PID 写入 `state.json`。
   - 默认**不构建**，直接运行现有二进制。`-Rebuild` 参数才执行 `cd web && npx --yes pnpm run build` + `go build -o bin/emoagent.exe ./cmd/emoagent`（开发后刷新二进制用）。
7. **就绪确认**：轮询 `/api/platforms/status` 直到 adapter 处于已连接状态（超时 `ready_timeout_seconds`）→ Windows 通知"EmoAgent 已上线，直接在 QQ 发消息即可"。
8. **失败路径**：任何环节超时/出错 → 通知写明卡在哪一环 + 日志文件路径；脚本以非零码退出；已启动的环节保持运行（不回滚，便于人工接续排查后重跑 `emo up` 续走）。

## 6. emo down

顺序：EmoAgent → Bot QQ → SnowLuma。默认三件全停（已确认）。

1. **EmoAgent**：优先尝试优雅停止（实现时确认是否存在 shutdown 入口或可用 CTRL 事件；SQLite 为 WAL 模式，强杀可接受但仅作兜底）。目标进程 = `state.json` 记录的 PID 且进程名匹配 `emoagent`；无记录则按可执行路径匹配查找。
2. **Bot QQ**：通过 `/api/processes` 找到注入且 UIN = `bot_uin` 的进程 → `unload` → 结束该 PID（结束前校验进程名为 QQ.exe，防 PID 复用）。SnowLuma 不可用时退回 `state.json` 的 PID（同样校验进程名）；两者都无法确认归属时**跳过并通知**，绝不猜杀。
3. **SnowLuma**：结束 `state.json` 记录的 node 进程；无记录则按命令行（`index.mjs` + 部署目录）匹配查找。
4. 个人 QQ 在任何分支下都不在目标集合内。

## 7. emo status（只读，无副作用）

| 检查项 | 手段 |
|---|---|
| SnowLuma 服务 | `GET /api/auth/state` 可达性 |
| Bot QQ 注入并在线 | 登录后查 `/api/qq-list` 含 `bot_uin` |
| EmoAgent 进程健康 | `GET /api/platforms/status` 返回 200 |
| Adapter WS 已连接 | 上述返回体中的连接状态字段 |

每项输出 ✓/✗；✗ 时附一行修复提示（通常是"运行 emo up"）。

## 8. 凭据与通知

- SnowLuma 密码：PowerShell `SecureString` + DPAPI（`Export-Clixml`）存 `data/launcher/snowluma.cred`，仅当前 Windows 用户可解密，不进 git。已确认接受存本机。
- Windows 通知：优先使用系统自带 Toast（WinRT API，经 PowerShell 调用，零第三方依赖）；不可用时降级为控制台输出。
- EmoAgent `/api` 当前按无鉴权本地接口对待（与 WebUI 一致）；若实现时发现有鉴权，凭据处理方式与 SnowLuma 相同。

## 9. 实现期核验点

以下细节不影响设计成立，留在实现第一步实测确认：

1. `/api/login` 请求体格式与会话载体（cookie vs token）——打开控制台抓一次网络请求即可。
2. `/api/qq-list`、`/api/processes` 返回结构（UIN 与 PID 的对应字段名）。
3. EmoAgent 是否存在优雅停止入口；无则确认 CTRL 事件或接受 WAL 下的强停兜底。
4. `GET /api/platforms/status` 返回体中"已连接"的具体字段。
5. QQ.exe 实际安装路径（写入用户配置）。

## 10. 验收标准

1. **冷启动**：开机后什么都没跑，双击"启动 EmoAgent"→ 全程唯一人工动作是在 QQ 窗口选择 Agent 账号 → 收到"已上线"通知 → QQ 发消息得到回复。
2. **全就绪重入**：三环节都在跑时执行 `emo up`，秒级完成，不重复启动任何进程，直接通知已上线。
3. **半就绪补齐**：任一环节单独挂掉后执行 `emo up`，只补缺失环节。
4. **误选账号**：在脚本启动的 QQ 窗口登录个人账号 → 注入被卸载并收到提示，个人账号进程不被杀。
5. **emo down**：三环节全停、个人 QQ 不受影响；随后 `emo up` 可正常拉起。
6. **可排查**：以上所有过程在 `data/launcher/logs/` 有日志。

## 11. 未来扩展（不在本次范围）

- **迁移低功耗笔记本常驻**：配置中的 `base_url` 已支持指向远程；但"启动进程"部分需在目标机本地执行（届时演化为目标机上的计划任务/服务 + 本机仅剩 status 探测），另立规格。
- **升级形态 A（开机自启）**：把 `emo up` 注册为登录计划任务即可，无需改脚本。
- **`emo doctor`**：自动修复类动作（重注入、重启单环节），在 up/down/status 稳定使用后按需追加。
