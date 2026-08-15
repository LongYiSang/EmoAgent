# EmoAgent 前端迁移 Spec：React 19 + TypeScript + Vite + Tailwind CSS v4

> **Document status**: Migration Implementation Spec Draft  
> **Target stack**: React 19 + TypeScript + Vite + Tailwind CSS v4 (`tailwindcss` + `@tailwindcss/vite`)  
> **UI policy**: 不使用 shadcn/ui、Radix、Headless UI、MUI、Ant Design 或其他组件库；组件全部在项目内实现。  
> **Target project path**: 建议落地为 `docs/frontend/react_vite_migration_spec.md`  
> **Date**: 2026-06-08

---

## 0. 一句话目标

把 EmoAgent 当前的纯 HTML/CSS/JS 临时前端，迁移为一个 **Vite 多页面 React 应用**：

```text
web/index.html      → Chat WebUI React entry
web/admin.html      → Admin React entry
web/src/**          → React 19 + TypeScript source
web/src/styles.css  → Tailwind CSS v4 + EmoAgent design tokens
pnpm build          → internal/web/static/**
Go embed.FS         → 继续打包 static 产物进单二进制
```

迁移核心原则：

```text
Frontend implementation changes.
Backend API / WebSocket protocol must not change.
Go single-binary embed deployment must remain intact.
```

中文表达：

```text
只替换前端实现方式，不改变 EmoAgent 后端协议、部署模型和现有功能语义。
```

---

## 1. 背景与现状

### 1.1 项目约束

EmoAgent 当前是本地部署的个人情感陪伴 Agent。WebUI 是交互层的一部分，后端仍然由 Go 主服务拥有会话、Emotion、Work、Memory、Admin API 和 WebSocket。当前 README 中的项目定位强调：

- 用户始终在和 Emotion 根代理对话；
- Work 执行代理在幕后工作；
- 记忆服务于关系连续性，不是普通日志；
- WebUI / Admin API / Session / Persona / Work / 工具审批 / 长期记忆抽取队列 / MemoryCore 已接入；
- 默认开发运行命令仍然是 `go run ./cmd/emoagent -config ./config.yaml`、`go build ...`、`go test ./...`；
- WebUI 和 Admin 静态资源通过 `embed.FS` 打包在 Go 二进制内。

因此前端迁移不能引入独立 Node 服务作为生产依赖，也不能要求用户用 Next.js/SSR 服务来运行 EmoAgent。

### 1.2 当前旧前端入口

当前前端主要由以下文件组成：

```text
internal/web/static/index.html   # Chat WebUI，纯 HTML/CSS/JS
internal/web/static/admin.html   # Admin 页面，纯 HTML/CSS/JS
internal/web/static/shared.css   # 共享设计 tokens、rail、button、form、badge 等基础样式
internal/web/embed.go            # //go:embed static/*
internal/app/server.go           # 静态资源、API、/ws 注册
```

当前 Chat WebUI 已经包含大量状态和功能，不是简单消息框：

```text
- 左侧 rail / chat-admin 导航
- persona chip
- session drawer / session list / delete session / switch session
- conversation header / connection status
- memory status panel
- memory scan button
- message thread
- assistant streaming bubble
- user optimistic message + failed retry
- work progress card
- tool activity card
- reasoning activity card
- memory pipeline snapshot panel
- approval required / approval resolved card
- mobile drawer behavior
```

当前 Admin 页面也不是简单表单，至少覆盖 Provider、Agent Config、Persona、Memory、Sidecar、Config 等运行时管理能力。迁移时应把 Admin 作为同一前端工程下的第二个入口，而不是暂时遗弃。

### 1.3 当前后端静态资源和路由方式

当前 Go 服务使用：

```go
staticSub, err := fs.Sub(web.StaticFS, "static")
registerRoutes(mux, api, chatHandler, http.FileServer(http.FS(staticSub)))
```

并通过：

```go
//go:embed static/*
var StaticFS embed.FS
```

嵌入 `internal/web/static/**`。

这意味着最小迁移路径应保持：

```text
internal/web/static/index.html
internal/web/static/admin.html
internal/web/static/assets/**
```

继续由 Go `http.FileServer` 服务。

---

## 2. 技术选型决策

### 2.1 选用 React 19

React 19 已是稳定版本，适合把当前大量手写 DOM 状态迁移为组件状态、reducer 和可测试的 hooks。对 EmoAgent 特别有用的点：

```text
- useOptimistic：适合用户消息 optimistic append、审批按钮 pending 态。
- useTransition / Actions：适合 Admin 保存、Provider 测试、Memory scan 等异步 mutation 的 pending/error 管理。
- ref as prop：可减少输入框、滚动容器、面板组件转发 ref 的样板代码。
```

本项目不需要 React Server Components，也不需要 SSR。React 只作为浏览器端 SPA/MPA UI 层使用。

### 2.2 选用 Vite

Vite 适合 EmoAgent 的原因：

```text
- 开发时提供快速 dev server 和 HMR。
- 构建后输出纯静态资产，可被 Go embed.FS 打包。
- 支持 React + TypeScript 模板。
- 支持 dev proxy，把 /api 和 /ws 转发到 Go 后端。
- 支持多页面入口，保留 / 和 /admin.html 的现有访问方式。
```

### 2.3 选用 Tailwind CSS v4

Tailwind CSS v4 适合当前迁移：

```text
- v4 使用 CSS-first 配置，EmoAgent 现有 CSS variables 可迁移到 @theme。
- 官方 Vite plugin 能减少 PostCSS 样板配置。
- 现有 shared.css 的设计 tokens 可以保留语义：surface / ink / peach / lavender / mint / rose / radius / shadow / font。
- 不使用 UI 组件库时，Tailwind 能帮助快速实现一致布局、spacing、状态样式和响应式行为。
```

### 2.4 明确不选

本次不引入：

```text
- Next.js / Remix / SSR 服务
- shadcn/ui
- Radix UI
- Headless UI
- MUI / Ant Design / Chakra 等组件库
- Zustand / Jotai / Redux 等额外状态库，除非后续复杂度证明必要
- Vercel AI SDK 作为核心协议层
```

理由：EmoAgent 已有 Go WebSocket 协议、Admin API 和 embed.FS 静态部署。前端当前阶段优先完成协议不变的组件化重构。

---

## 3. 目标目录结构

建议新增 `web/` 作为前端源代码目录，构建产物输出到既有 Go embed 目录。

```text
web/
  package.json
  pnpm-lock.yaml
  tsconfig.json
  tsconfig.node.json
  vite.config.ts
  index.html
  admin.html

  src/
    chat/
      main.tsx
      ChatApp.tsx
      components/
        ChatShell.tsx
        Rail.tsx
        SessionDrawer.tsx
        ConversationHeader.tsx
        MemoryStatusPanel.tsx
        MessageThread.tsx
        MessageBubble.tsx
        Composer.tsx
        WorkProgressCard.tsx
        ToolActivityCard.tsx
        ReasoningActivityCard.tsx
        ApprovalCard.tsx
        MemoryPipelinePanel.tsx
      hooks/
        useAutoResizeTextarea.ts
        useChatSession.ts
        useChatSocket.ts
        useStickToBottom.ts
      state/
        chatReducer.ts
        chatTypes.ts
      protocol/
        wsTypes.ts
        wsClient.ts
        sessionApi.ts
        memoryApi.ts
        approvalTypes.ts

    admin/
      main.tsx
      AdminApp.tsx
      components/
        AdminShell.tsx
        AdminTabs.tsx
        ProviderCenter.tsx
        AgentConfigCenter.tsx
        PersonaCenter.tsx
        ChatSettingsPanel.tsx
        ConfigCenter.tsx
        MemoryConfigPanel.tsx
        SidecarPanel.tsx
      hooks/
        useAdminApi.ts
      protocol/
        adminApi.ts
        adminTypes.ts

    shared/
      components/
        Button.tsx
        Icon.tsx
        Field.tsx
        Badge.tsx
        StatusChip.tsx
        ConfirmDialog.tsx
        JsonBlock.tsx
      hooks/
        useAsyncAction.ts
      lib/
        api.ts
        classNames.ts
        date.ts
        safeText.ts
      styles/
        tokens.css
        app.css

    styles.css
```

构建输出：

```text
internal/web/static/
  index.html
  admin.html
  assets/*.js
  assets/*.css
  assets/*.{svg,png,...}
```

### 3.1 为什么保留 `index.html` + `admin.html` 双入口

当前旧前端使用 `/` 作为 Chat，`/admin.html` 作为 Admin。Go 静态服务当前是普通 `http.FileServer`，没有 React history fallback。为了不改后端，第一阶段采用 Vite multi-page build：

```text
/           → internal/web/static/index.html
/admin.html → internal/web/static/admin.html
```

后续如果要做单页路由 `/admin`、`/memory`、`/settings`，再单独设计 Go fallback 或前端 hash router。

---

## 4. 构建配置

### 4.1 package.json

建议使用 pnpm：

```json
{
  "name": "emoagent-web",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite --host 127.0.0.1",
    "build": "tsc -b && vite build",
    "typecheck": "tsc -b",
    "preview": "vite preview --host 127.0.0.1"
  },
  "dependencies": {
    "@vitejs/plugin-react": "latest",
    "vite": "latest",
    "typescript": "latest",
    "react": "latest",
    "react-dom": "latest",
    "tailwindcss": "latest",
    "@tailwindcss/vite": "latest"
  },
  "devDependencies": {}
}
```

也可以把 `@vitejs/plugin-react`、`vite`、`typescript`、`tailwindcss`、`@tailwindcss/vite` 放入 `devDependencies`。Codex 实现时以项目实际 package manager 习惯为准。

### 4.2 vite.config.ts

```ts
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import { resolve } from 'node:path';

export default defineConfig({
  plugins: [react(), tailwindcss()],
  base: '/',
  server: {
    port: 5173,
    strictPort: false,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
      '/ws': {
        target: 'ws://127.0.0.1:8080',
        ws: true,
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: '../internal/web/static',
    emptyOutDir: true,
    sourcemap: false,
    rollupOptions: {
      input: {
        chat: resolve(__dirname, 'index.html'),
        admin: resolve(__dirname, 'admin.html'),
      },
    },
  },
});
```

### 4.3 Tailwind CSS v4 入口

`web/src/styles.css`：

```css
@import "tailwindcss";
@import "./shared/styles/tokens.css";
@import "./shared/styles/app.css";
```

`web/src/shared/styles/tokens.css`：

```css
@theme {
  --font-ui: "Manrope", "PingFang SC", "Hiragino Sans GB", sans-serif;
  --font-data: "Inter", "PingFang SC", sans-serif;
  --font-mono: "JetBrains Mono", ui-monospace, monospace;

  --color-surface: #fffcfd;
  --color-surface-2: #fff0f3;
  --color-surface-3: #ffe4ea;
  --color-ink: #3d2b3a;
  --color-ink-2: #5e4a5c;
  --color-muted: #a08da0;
  --color-peach: #ffb5c2;
  --color-peach-ink: #d4637a;
  --color-peach-soft: #ffe4ea;
  --color-coral: #ffa0b2;
  --color-lavender: #d4c2f0;
  --color-lavender-ink: #7e5fb8;
  --color-lavender-soft: #f0e8ff;
  --color-mint-ink: #4eaa7e;
  --color-mint-soft: #e0f7ec;
  --color-rose: #ffcad8;
  --color-rose-ink: #d4637a;
  --color-danger: #e85d6c;
  --color-danger-soft: #fff0f2;

  --radius-sm: 14px;
  --radius-md: 18px;
  --radius-lg: 22px;

  --shadow-emo-sm: 0 1px 2px rgb(42 34 48 / 0.04), 0 2px 8px rgb(42 34 48 / 0.03);
  --shadow-emo-md: 0 4px 16px rgb(42 34 48 / 0.06);
  --shadow-emo-lg: 0 12px 28px rgb(42 34 48 / 0.06), 0 28px 70px rgb(42 34 48 / 0.08);
}

:root {
  --hair: rgba(61, 43, 58, 0.06);
  --line: rgba(61, 43, 58, 0.09);
  --primary-grad: linear-gradient(135deg, #ffb5c2 0%, #d4c2f0 100%);
  --ease-out: cubic-bezier(0.2, 0.8, 0.2, 1);
  --ease-bounce: cubic-bezier(0.34, 1.56, 0.64, 1);
}
```

由于项目不使用组件库，应把常用样式封装为本地组件或本地 CSS utility：

```css
@utility bg-primary-grad {
  background-image: var(--primary-grad);
}

@utility border-hair {
  border-color: var(--hair);
}
```

---

## 5. 协议兼容要求

### 5.1 REST API 必须保持不变

前端迁移后继续使用当前后端已注册的 API：

```text
Provider:
  GET/POST /api/llm-providers
  GET/PUT/DELETE /api/llm-providers/{id}
  POST /api/llm-providers/{id}/refresh-models
  GET /api/llm-providers/{id}/models
  GET /api/llm-providers/{id}/env-status
  POST /api/providers/{id}/test

Config:
  GET /api/config/effective
  POST /api/config/validate
  GET /api/config/issues

Memory / Sidecar:
  GET/PUT /api/memory/config
  GET/PUT /api/memory/features
  POST /api/memory/extractions
  GET /api/memory/extractions
  GET /api/memory/segments
  POST /api/memory/natural-runs
  GET /api/memory/natural-runs/latest
  GET /api/sidecar/status
  POST /api/sidecar/start|stop|restart
  GET /api/sidecar/generated-config
  GET /api/sidecar/logs

Agent / Persona / Session:
  GET/POST /api/agent-configs
  GET /api/agent-configs/active
  GET/PUT/DELETE /api/agent-configs/{id}
  POST /api/agent-configs/{id}/activate
  GET/PUT /api/settings/chat
  GET/POST /api/personas
  GET/PUT/DELETE /api/personas/{name}
  GET/PUT /api/personas/{name}/progress-phrases
  GET /api/progress-phrases/defaults
  GET /api/sessions
  GET /api/sessions/latest
  GET /api/sessions/{id}
  GET /api/sessions/{id}/approvals
  DELETE /api/sessions/{id}
```

### 5.2 WebSocket URL 兼容

保持：

```text
/ws?persona={personaKey}&session_id={sessionID}&skip_greeting=1
```

客户端行为：

```text
- 新建会话：不带 session_id。
- 恢复会话：带 session_id。
- 用户已经输入消息后重连：带 skip_greeting=1，避免重复 greeting。
- persona 为空时，先通过 /api/agent-configs/active 或 /api/personas 获取默认 persona。
```

### 5.3 WS client event union

前端必须将后端 `WSMessage` 转换为 TypeScript union，不要在组件里直接散落字符串判断。

```ts
export type ServerEvent =
  | { type: 'session_ready'; session_id: string; persona?: string; is_new?: boolean }
  | { type: 'greeting'; content: string }
  | { type: 'stream_start' }
  | { type: 'stream_delta'; content: string }
  | { type: 'stream_end' }
  | { type: 'reasoning_start'; reasoning: ReasoningActivity }
  | { type: 'reasoning_delta'; reasoning: ReasoningActivity }
  | { type: 'reasoning_end'; reasoning: ReasoningActivity }
  | { type: 'tool_call_start'; tool: ToolActivity }
  | { type: 'tool_call_end'; tool: ToolActivity }
  | { type: 'approval_required'; approval: ApprovalRequest }
  | { type: 'approval_updated'; approval: ApprovalRequest }
  | { type: 'work_progress'; content: string }
  | { type: 'work_progress_end' }
  | { type: 'error'; content: string; error_kind?: string }
  | { type: 'pong' };

export type ClientEvent =
  | { type: 'message'; content: string; request_id?: string }
  | { type: 'approval_action'; request_id: string; action: 'approve' | 'reject' | string; option_id?: string }
  | { type: 'ping' };
```

兼容注意：旧前端处理过大小写混合字段，例如 `SessionID/session_id`、`CreatedAt/created_at/createdAt`。新前端应在 `protocol/normalize.ts` 统一归一化，而不是在组件里重复写兼容逻辑。

---

## 6. Chat 前端状态模型

### 6.1 ChatState

```ts
export interface ChatState {
  connection: {
    status: 'idle' | 'loading' | 'ready' | 'connecting' | 'connected' | 'disconnected' | 'error';
    message: string;
    reconnectDelayMs: number;
  };

  personaKey: string;
  sessionId: string;
  sessions: SessionSummary[];

  messages: TimelineItem[];
  pendingAssistantId?: string;
  pendingLocalMessages: Record<string, LocalMessageStatus>;

  approvals: ApprovalRequest[];
  pendingApprovalActions: Record<string, boolean>;
  dismissedApprovalIds: Record<string, boolean>;

  workProgress?: WorkProgressItem;
  toolActivities: Record<string, ToolActivity>;
  reasoningActivities: Record<string, ReasoningActivity>;

  memory: {
    statusVisible: boolean;
    segments: MemorySegment[];
    jobs: MemoryExtractionJob[];
    pipelineSnapshots: Record<string, MemoryPipelineSnapshot>;
  };

  ui: {
    drawerOpen: boolean;
    drawerCollapsed: boolean;
    memoryPipelinePanelOpen: boolean;
    activeMemoryPipelineKey?: string;
  };
}
```

### 6.2 TimelineItem

```ts
export type TimelineItem =
  | { kind: 'message'; id: string; role: 'user' | 'assistant' | 'error'; content: string; createdAt: string; status?: 'pending' | 'sent' | 'failed' }
  | { kind: 'approval'; id: string; approval: ApprovalRequest; createdAt: string }
  | { kind: 'work_progress'; id: string; content: string; createdAt: string }
  | { kind: 'tool'; id: string; tool: ToolActivity; createdAt: string }
  | { kind: 'reasoning'; id: string; reasoning: ReasoningActivity; createdAt: string }
  | { kind: 'memory_pipeline'; id: string; snapshotKey: string; createdAt: string };
```

旧前端靠 DOM dataset 和插入顺序维护 timeline。React 版应把 timeline 变成 state 数据，然后组件只负责渲染。

### 6.3 reducer 动作

```ts
export type ChatAction =
  | { type: 'BOOTSTRAP_START' }
  | { type: 'BOOTSTRAP_READY'; personaKey: string; sessionId: string; sessions: SessionSummary[] }
  | { type: 'SOCKET_CONNECTING' }
  | { type: 'SOCKET_CONNECTED' }
  | { type: 'SOCKET_DISCONNECTED'; reconnectDelayMs: number }
  | { type: 'SESSION_READY'; sessionId: string; personaKey: string; isNew?: boolean }
  | { type: 'APPEND_LOCAL_USER_MESSAGE'; localId: string; content: string; createdAt: string }
  | { type: 'MARK_LOCAL_MESSAGE'; localId: string; status: 'pending' | 'sent' | 'failed' }
  | { type: 'STREAM_START' }
  | { type: 'STREAM_DELTA'; delta: string }
  | { type: 'STREAM_END' }
  | { type: 'UPSERT_TOOL'; tool: ToolActivity; collapse?: boolean }
  | { type: 'UPSERT_REASONING'; reasoning: ReasoningActivity; append?: boolean; collapse?: boolean }
  | { type: 'UPSERT_APPROVAL'; approval: ApprovalRequest }
  | { type: 'WORK_PROGRESS'; content: string }
  | { type: 'WORK_PROGRESS_END' }
  | { type: 'MEMORY_STATUS_LOADED'; segments: MemorySegment[]; jobs: MemoryExtractionJob[] }
  | { type: 'ERROR'; content: string };
```

---

## 7. 组件迁移映射

| 旧 DOM / 函数 | 新组件 / hook | 迁移说明 |
|---|---|---|
| `.app-shell`, `.rail` | `ChatShell`, `Rail` | 保留 Chat / Admin 入口。Admin 链接继续用 `/admin.html`。 |
| `.drawer`, `renderSessionList` | `SessionDrawer` | session active、delete、refresh、mobile drawer 都在组件内实现。 |
| `.conv-top` | `ConversationHeader` | persona、status、memory buttons。 |
| `renderMemoryStatus` | `MemoryStatusPanel` | segments/jobs 最多展示前 4 条；状态 badge 保持。 |
| `appendMessage`, `setMessageContent` | `MessageThread`, `MessageBubble` | pendingAssistant 从 DOM 变量迁移到 reducer。 |
| `appendLocalUserMessage`, retry | `Composer` + reducer | 发送失败显示 Retry。 |
| `showWorkProgress` | `WorkProgressCard` | `work_progress` 与 `work_progress_end` 兼容。 |
| `upsertToolActivity` | `ToolActivityCard` | 支持 expanded/collapsed、preview、duration、hash、truncated。 |
| `upsertReasoningActivity` | `ReasoningActivityCard` | 支持 delta append、done collapse、memory pipeline button。 |
| `renderApprovals` | `ApprovalCard` | 支持 pending、approved、rejected、dismiss、option action pending。 |
| `renderMemoryPipelinePanel` | `MemoryPipelinePanel` | 支持 prompt_block、query_analysis、stages。 |
| `connect`, `ensureConnected` | `useChatSocket` | WebSocket 生命周期、重连、skip_greeting 逻辑。 |
| `requestJSON` | `shared/lib/api.ts` | fetch wrapper，统一错误处理和 credentials。 |

---

## 8. Admin 迁移范围

Admin 迁移建议先做到功能对等，不在第一轮重新设计信息架构。

目标页面：

```text
Provider Center
  - provider list / create / update / delete
  - refresh models
  - env status
  - test provider

Agent Config Center
  - list / create / update / delete / activate
  - model slots
  - progress phrases

Persona Center
  - list / create / update / delete
  - persona fields
  - quirks / progress phrases

Chat Settings
  - GET/PUT /api/settings/chat

Config Center
  - effective config
  - validate config
  - config issues

Memory Center
  - memory config
  - memory features
  - natural run latest / trigger
  - extraction jobs / segments summary

Sidecar Center
  - status
  - start / stop / restart
  - generated config
  - logs
```

实现方式：

```text
- AdminApp 使用 tab state，不引入 router。
- 表单状态用 useState / useReducer / useAsyncAction。
- 所有 API 类型集中在 admin/protocol/adminTypes.ts。
- 不使用组件库，本地实现 Button、Field、Badge、JsonBlock、ConfirmDialog。
```

---

## 9. 视觉迁移原则

### 9.1 保留旧视觉语言

旧 `shared.css` 的核心视觉语言应保留：

```text
- 温暖、柔和、低对比背景
- peach / lavender / mint / rose 色系
- 大圆角：14 / 18 / 22
- pill 状态标签
- 轻阴影和柔和渐变
- Manrope / Inter / JetBrains Mono 字体层次
- rail + drawer + conversation 三段式布局
```

### 9.2 Tailwind 迁移策略

不要机械地把每条 CSS 都翻译成超长 className。推荐规则：

```text
- 布局、spacing、flex/grid、text、rounded、shadow 用 Tailwind utility。
- 复杂背景、渐变、动画、滚动条、输入 focus、主题 tokens 保留在 CSS。
- 重复视觉单元抽成项目内组件，而不是引入 UI 库。
- className 超过可读阈值时使用本地组件或 CSS class。
```

### 9.3 可访问性最低要求

```text
- 所有 button 必须是 <button type="button|submit">。
- 抽屉 toggle、memory status panel、tool/reasoning collapse 必须维护 aria-expanded。
- Memory pipeline panel 使用 aria-hidden，并支持 Escape 关闭。
- Approval action pending 时按钮 disabled，避免重复提交。
- 错误消息用 role="alert" 或 aria-live 区域展示。
- Composer 保持 Enter 发送、Shift+Enter 换行。
```

---

## 10. 迁移阶段

### Phase 0：冻结旧前端行为

目标：在改代码前建立行为基线。

动作：

```text
- 记录旧 index.html/admin.html 的功能清单。
- 保留旧文件作为 reference，可放入 docs/frontend/legacy/ 或 Git commit history。
- 从 handler.go / handler_test.go 提取 WS event contract。
- 从 server.go 提取 API endpoint inventory。
```

验收：

```text
- docs/frontend/react_vite_migration_spec.md 存在。
- 已列出 Chat 和 Admin parity checklist。
```

### Phase 1：引入 Vite + React + Tailwind v4 骨架

目标：构建链路跑通，但不要求功能完整。

动作：

```text
- 新增 web/package.json、vite.config.ts、tsconfig。
- 新增 web/index.html、web/admin.html。
- 新增 React entry：chat/main.tsx、admin/main.tsx。
- 新增 Tailwind v4 styles.css 和 tokens.css。
- 构建输出到 internal/web/static。
```

验收：

```text
pnpm --dir web install
pnpm --dir web typecheck
pnpm --dir web build
go test ./...
```

并确认：

```text
internal/web/static/index.html 存在
internal/web/static/admin.html 存在
internal/web/static/assets/** 存在
```

### Phase 2：Chat Shell 和静态视觉迁移

目标：React 版 Chat 页面视觉结构与旧页面一致。

动作：

```text
- 实现 Rail、SessionDrawer、ConversationHeader、MessageThread、Composer。
- 先用 mock data 渲染 session、messages、tool、reasoning、approval、memory status。
- 保留移动端 drawer 行为。
```

验收：

```text
pnpm --dir web typecheck
pnpm --dir web build
人工打开 Vite dev server，确认视觉结构和参考 HTML 一致。
```

### Phase 3：REST API 接入

目标：不连 WebSocket 时，页面也能加载 session/persona/memory status。

动作：

```text
- 实现 shared/lib/api.ts。
- 实现 sessionApi、memoryApi。
- bootstrapChat：读取 URL persona/session_id，加载 session detail、default persona、session list、approvals、memory status。
- 实现 switchSession、startNewChat、deleteSession、queueMemoryExtraction。
```

验收：

```text
- / 显示默认 persona。
- session list 可刷新、切换、删除。
- memory status 可展开。
- memory scan 可提交 POST /api/memory/extractions。
- 不修改后端 API。
```

### Phase 4：WebSocket 实时聊天接入

目标：流式聊天、工具、推理、审批、Work progress 完全恢复。

动作：

```text
- 实现 wsTypes.ts / wsClient.ts / useChatSocket。
- 支持 session_ready、greeting、stream_start/delta/end。
- 支持 tool_call_start/end。
- 支持 reasoning_start/delta/end。
- 支持 approval_required/updated。
- 支持 approval_action。
- 支持 work_progress/end。
- 支持 ping/pong、reconnect exponential backoff、skip_greeting。
```

验收：

```text
pnpm --dir web typecheck
pnpm --dir web build
go test ./internal/chat -run TestHandler
go test ./...
```

手动 smoke：

```text
1. 启动 Go 服务。
2. 打开 /。
3. 发送消息，看到 token streaming。
4. 切换 session 后历史正确恢复。
5. 触发工具调用，看到 tool card。
6. 触发 Work progress，看到 progress card。
7. 触发 approval，能 approve/reject 并继续 streaming。
```

### Phase 5：Admin React 迁移

目标：Admin 页面功能对等。

动作：

```text
- 实现 AdminShell / tabs。
- 按旧 admin.html 功能分块迁移 Provider、Agent Config、Persona、Memory、Sidecar、Config、Chat Settings。
- 所有 mutation 使用 useAsyncAction，显示 pending/error/success。
```

验收：

```text
- /admin.html 可打开。
- Provider CRUD / refresh / test 可用。
- Agent Config list / edit / activate 可用。
- Persona list / edit 可用。
- Config effective / validate / issues 可用。
- Memory config/features 和 Sidecar status/actions 可用。
```

### Phase 6：清理与文档

目标：完成替换，不留下双实现混乱。

动作：

```text
- internal/web/static 成为 build output，不再手写维护。
- README 增加 frontend dev/build 命令。
- 可选新增 scripts/build-web.ps1 / scripts/build-web.sh。
- 记录迁移 checklist。
```

验收：

```text
pnpm --dir web build
go test ./...
go build -o ./bin/emoagent ./cmd/emoagent
```

---

## 11. 风险与应对

### 11.1 Go embed 未包含 nested assets

风险：Vite 输出 `assets/*.js`，但 `go:embed static/*` 未正确包含嵌套资产。

应对：

```text
- 先通过 go build + 浏览器加载验证。
- 如果 asset 404，只允许将 internal/web/embed.go 改为更稳妥的：
  //go:embed static
  var StaticFS embed.FS
- 不改 API 和 /ws。
```

### 11.2 组件库禁止导致基础组件重复

应对：

```text
- shared/components 中维护 Button、Field、Badge、StatusChip、ConfirmDialog、JsonBlock。
- 不引入外部 UI 组件库。
- 图标优先内联 SVG，或后续单独评估是否允许 icon-only 依赖。
```

### 11.3 旧 HTML 里大小写兼容散落

应对：

```text
- 在 protocol/normalize.ts 统一做 field fallback。
- 组件只消费规范化后的 camelCase 类型。
```

### 11.4 stream_end 后历史重载导致闪烁

旧前端在 `stream_end` 后会重新加载 session detail 并 render history。React 版可以保留该行为，但应避免明显闪烁：

```text
- stream 期间 optimistic 更新 UI。
- stream_end 后后台 reload history。
- 使用 message id / content hash 合并，而不是直接清空重绘。
```

MVP 可先保持旧行为，后续优化。

### 11.5 长对话性能

第一版可先用普通列表；后续如果消息数量和 reasoning/tool 组件明显变慢，再引入自研简单 windowing 或单独评估 `@tanstack/react-virtual`。当前 Spec 不把虚拟列表作为必需依赖，以遵守“无组件库 / 最小依赖”原则。

---

## 12. 验证命令

基础验证：

```bash
pnpm --dir web install
pnpm --dir web typecheck
pnpm --dir web build
go test ./...
go build -o ./bin/emoagent ./cmd/emoagent
```

开发验证：

```bash
# terminal 1
go run ./cmd/emoagent -config ./config.yaml

# terminal 2
pnpm --dir web dev
```

访问：

```text
http://127.0.0.1:5173/
http://127.0.0.1:5173/admin.html
```

生产静态验证：

```bash
go run ./cmd/emoagent -config ./config.yaml
```

访问：

```text
http://127.0.0.1:8080/
http://127.0.0.1:8080/admin.html
```

必要时检查：

```bash
curl -I http://127.0.0.1:8080/
curl -I http://127.0.0.1:8080/admin.html
```

---

## 13. 完成定义

迁移完成必须满足：

```text
- React Chat 页面可替代旧 index.html。
- React Admin 页面可替代旧 admin.html。
- /api 和 /ws 协议未变。
- Go embed.FS 仍能打包前端。
- go test ./... 通过。
- pnpm --dir web build 通过。
- 发送消息、streaming、session resume、memory scan、approval action、tool/reasoning/work progress、Admin provider/config/persona/memory/sidecar 基本操作通过 smoke test。
- 没有引入 shadcn/ui 或其他外部组件库。
```

---

## 14. 给 Codex 用的 `/goal` 文案

```text
/goal Migrate EmoAgent's existing pure HTML WebUI/Admin frontend to a Vite multi-page React 19 + TypeScript + Tailwind CSS v4 app, while preserving the existing Go backend REST APIs, WebSocket protocol, / and /admin.html routes, and embed.FS single-binary deployment. Implement project-local UI components only; do not use shadcn/ui or any external component library.
Verification: Run `pnpm --dir web install` if needed, then `pnpm --dir web typecheck`, `pnpm --dir web build`, `go test ./...`, and `go build -o ./bin/emoagent ./cmd/emoagent`. Evidence must include generated `internal/web/static/index.html`, `internal/web/static/admin.html`, Vite asset files under `internal/web/static/assets/`, and a short smoke-test note covering Chat load, session resume, streaming response, memory scan, approval action, tool/reasoning/work progress rendering, and Admin page load.
Constraints: Do not change `/api/*` endpoints, `/ws` URL/query semantics, WS event names or payload fields, chat/session/persona/memory approval behavior, Go config semantics, MemoryCore behavior, database schema, or Turn Pipeline behavior. Do not add shadcn/ui, Radix, Headless UI, MUI, Ant Design, Chakra, Redux, Zustand, Jotai, React Router, Vercel AI SDK, or any component/state/routing library unless explicitly approved. Production must remain static assets served by Go `embed.FS`; no production Node/SSR server.
Boundaries: Allowed writes: `web/**`, `internal/web/static/**` as generated build output, docs under `docs/frontend/**`, root/package manager metadata only if needed for frontend scripts, and `internal/web/embed.go` only if Vite nested assets are not embedded correctly. Forbidden paths without explicit approval: `internal/chat/**`, `internal/app/server.go`, `internal/memory/**`, `internal/turn/**`, `internal/protocol/**`, `internal/storage/**`, `config/**`, database migrations, MemoryCore integration, and backend API handlers. Use current `internal/web/static/index.html`, `internal/web/static/admin.html`, `internal/web/static/shared.css`, `internal/chat/handler.go`, and `internal/app/server.go` as behavior references; if a separate reference HTML file exists, use it only as visual guidance and do not copy unsafe inline scripts.
Iteration policy: Make one focused migration step at a time. After each step, rerun the smallest relevant check (`pnpm --dir web typecheck`, `pnpm --dir web build`, targeted Go tests, then full `go test ./...` before completion). Keep a concise progress log in the final response with files changed, commands run, and remaining parity gaps. Prefer typed protocol adapters and reducers over direct DOM manipulation. Preserve feature parity before visual refinements.
Stop when: The React/Vite build fully replaces the old hand-written static frontend, all verification commands pass, Go embed serves both Chat and Admin pages from `internal/web/static`, and the smoke-test evidence proves existing chat streaming, session, memory, approval, tool/reasoning/work progress, and Admin flows still work without backend protocol changes.
Pause if: The provided reference HTML conflicts with existing backend protocol, Vite output cannot be embedded without changing server routing beyond `internal/web/embed.go`, a required parity behavior depends on an undocumented API change, package installation fails due to network/registry policy, a verification command reveals backend regressions unrelated to frontend changes, or completing parity would require adding a forbidden dependency or modifying forbidden backend paths.
```
