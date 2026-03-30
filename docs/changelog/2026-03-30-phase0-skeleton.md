# Phase 0: 基础骨架搭建

**日期：** 2026-03-30
**状态：** Completed

## 变更内容

### 新增 Go 项目结构

- Go module: `github.com/longyisang/emoagent`, Go 1.26.1
- 外部依赖：`modernc.org/sqlite`、`gopkg.in/yaml.v3`、`github.com/joho/godotenv`

### 新增包

| 包 | 路径 | 职责 |
|---|---|---|
| logger | `internal/logger/` | 基于 slog 的结构化日志 |
| config | `internal/config/` | YAML 配置加载 + Persona 文件加载与热更新 |
| storage | `internal/storage/` | SQLite 封装、DDL 迁移、运行时配置读写 |
| llm | `internal/llm/` | LLM Client 接口、SSE 解码器、OpenAI 与 Anthropic 实现 |
| protocol | `internal/protocol/` | 协议类型定义（TaskBrief、TaskReport、DecisionRequest、DecisionResponse） |
| app | `internal/app/` | App 生命周期（Init / Run / Shutdown） |

### 入口与模板

- `cmd/emoagent/main.go` — 程序入口，flag 解析 + 信号处理
- `config.yaml` — 默认配置模板
- `personas/default.yaml` — 默认人格 "Emo"

### 基础设施能力

- 三级配置优先级：运行时(SQLite) > YAML 文件 > 默认值
- SQLite 自动建表迁移（sessions、messages、personas、config_runtime、schema_version）
- Persona 文件轮询热更新（5s 间隔）
- LLM Client 支持 OpenAI 兼容格式（含 SSE 流式）和 Anthropic Messages API
- 优雅启停（context + signal）

### 测试

- logger、config、persona、storage、SSE 解码器均有单元测试
- `go test ./...` 全部通过

### 文档对齐

- 修改 `docs/architecture/设计方案.md`，与 `架构.md` 对齐（路线图、协议消息、权限模型、执行日志定位等）
- 新建 `CLAUDE.md`
