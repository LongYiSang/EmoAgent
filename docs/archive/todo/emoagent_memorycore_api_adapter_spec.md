# EmoAgent × MemoryCore c362192 API 接入 Spec

## 0. 背景与结论

MemoryCore commit `c362192bd7842a6a6df99633fd48e35cf99f1a63` 将 public API 从“直接暴露 internal appcore 大 Service + alias DTO”收口为 `*memorycore.Client` 与 capability 分组。EmoAgent 当前 `internal/memoryhost` 仍有若干旧调用形态，接入方向应是：在 EmoAgent 内部建立一个极薄的 MemoryCore 防腐层/Port，所有代码只调用这个 Port；Port 的唯一实现使用 `client.Sessions()`、`client.Retrieval()`、`client.Writes()`、`client.Forget()`、`client.Ops()`。

## 1. 目标

1. EmoAgent 只依赖 `github.com/longyisang/emoagent-memorycore/pkg/memorycore`，不 import MemoryCore `internal/...` 或 `pkg/memorycore/extractionruntime`。
2. `memoryhost.Host` 不再保存 `memorycore.Service`，改为保存内部接口 `CoreClient` 或直接保存 `*memorycore.Client`。
3. Bridge、ExtractionWorker、NaturalMemoryRunner 等调用方不直接知道 MemoryCore capability 分组细节，只通过 `Host.core` 或 `Host.client` 的封装方法调用。
4. 聊天热路径保持：append episode → retrieve prompt memory block → LLM reply；抽取继续异步 job 化，不同步阻塞聊天。
5. Sidecar/Trivium 仍只作为可降级检索增强，不能成为事实写入入口。
6. 用户遗忘继续走 preview → execute → verify/confirmation 语义，不允许只在 EmoAgent 文本层屏蔽。

## 2. 非目标

- 不改 MemoryCore public API。
- 不在 EmoAgent 中复刻 MemoryCore schema、SQLite store、sidecar HTTP 协议或 extraction runtime。
- 不把 Work 执行日志直接写为长期 facts。
- 不实现新的 Agent Affect 算法；只保持现有 prompt / affect 边界。

## 3. 新边界设计

### 3.1 包边界

建议新增或改造：

```text
internal/memoryhost/
  host.go              # 生命周期、配置打开、Host 结构
  core_client.go       # EmoAgent 内部 MemoryCore Port + memorycore.Client adapter
  bridge.go            # chat session/segment/prompt integration
  extraction_worker.go # async extraction jobs
  natural_memory.go    # natural memory scheduler
```

`internal/memoryhost/core_client.go` 定义 EmoAgent 所需的最小 Port：

```go
type CoreClient interface {
    Close() error

    StartSession(context.Context, memorycore.StartSessionRequest) (*memorycore.Session, error)
    EndSession(context.Context, memorycore.EndSessionRequest) (*memorycore.Session, error)
    AppendEpisode(context.Context, memorycore.AppendEpisodeRequest) (*memorycore.Episode, error)

    Retrieve(context.Context, memorycore.RetrievalRequest) (*memorycore.MemoryContext, error)

    RunExtraction(context.Context, memorycore.RunExtractionRequest) (*memorycore.ExtractionRunResult, error)
    RunExtractionBatch(context.Context, memorycore.ExtractionBatchRequest) (*memorycore.ExtractionBatchResult, error)
    ConsolidateCandidate(context.Context, memorycore.ConsolidateCandidateRequest) (*memorycore.ConsolidationResult, error)

    PreviewForget(context.Context, memorycore.ForgetPreviewRequest) (*memorycore.ForgetPreviewResult, error)
    ExecuteForget(context.Context, memorycore.ForgetExecuteRequest) (*memorycore.ForgetExecuteResult, error)
    VerifyForget(context.Context, memorycore.ForgetVerifyRequest) (*memorycore.ForgetVerifyResult, error)
    GetPendingManualForgetOperation(context.Context, memorycore.GetPendingManualForgetOperationRequest) (*memorycore.PendingManualForgetOperation, error)
    CancelPendingManualForgetOperation(context.Context, memorycore.CancelPendingManualForgetOperationRequest) (*memorycore.PendingManualForgetOperation, error)

    RunMirrorSync(context.Context, memorycore.RunMirrorSyncRequest) (*memorycore.RunMirrorSyncResult, error)
    RebuildMirror(context.Context, memorycore.RebuildMirrorRequest) (*memorycore.RebuildMirrorResult, error)
    RebuildSearchDocuments(context.Context, memorycore.RebuildSearchDocumentsRequest) (*memorycore.RebuildSearchDocumentsResult, error)
    RunRetentionJobs(context.Context, memorycore.RunRetentionJobsRequest) (*memorycore.RunRetentionJobsResult, error)
    RunNaturalMemoryCycle(context.Context, memorycore.RunNaturalMemoryCycleRequest) (*memorycore.RunNaturalMemoryCycleResult, error)
    RunNaturalMemoryTick(context.Context, memorycore.RunNaturalMemoryTickRequest) (*memorycore.RunNaturalMemoryCycleResult, error)
}
```

实现：

```go
type memoryCoreClientAdapter struct { client *memorycore.Client }
func (a memoryCoreClientAdapter) StartSession(ctx context.Context, r memorycore.StartSessionRequest) (*memorycore.Session, error) {
    return a.client.Sessions().StartSession(ctx, r)
}
func (a memoryCoreClientAdapter) Retrieve(ctx context.Context, r memorycore.RetrievalRequest) (*memorycore.MemoryContext, error) {
    return a.client.Retrieval().Retrieve(ctx, r)
}
func (a memoryCoreClientAdapter) RunExtraction(ctx context.Context, r memorycore.RunExtractionRequest) (*memorycore.ExtractionRunResult, error) {
    return a.client.Writes().RunExtraction(ctx, r)
}
func (a memoryCoreClientAdapter) PreviewForget(ctx context.Context, r memorycore.ForgetPreviewRequest) (*memorycore.ForgetPreviewResult, error) {
    return a.client.Forget().PreviewForget(ctx, r)
}
func (a memoryCoreClientAdapter) RunMirrorSync(ctx context.Context, r memorycore.RunMirrorSyncRequest) (*memorycore.RunMirrorSyncResult, error) {
    return a.client.Ops().RunMirrorSync(ctx, r)
}
```

其余方法按同样映射补齐。

### 3.2 Host 结构

```go
type Host struct {
    Core            CoreClient
    Source          string
    DBPath          string
    retrievalPolicy memorycore.RetrievalPolicy
    extractionPolicy ExtractionHostPolicy
    logger          *slog.Logger
}
```

兼容迁移期可暂时提供：

```go
func (h *Host) configured() bool { return h != nil && h.Core != nil }
```

不要再出现：

```go
Service memorycore.Service
memorycore.ExtractionLLM
```

### 3.3 Open 流程

`open(ctx, opts, policy, logger, source)` 中：

```go
client, err := memorycore.Open(ctx, opts)
if err != nil { ... }
host := &Host{Core: memoryCoreClientAdapter{client: client}, ...}
```

如需保留测试可注入 fake core：

```go
func NewHostForTest(core CoreClient, policy memorycore.RetrievalPolicy) *Host
```

## 4. 调用映射表

| EmoAgent 意图 | 旧调用 | 新调用 |
|---|---|---|
| Start memory segment | `Service.StartSession` | `Core.StartSession` → `client.Sessions().StartSession` |
| Append user/assistant episode | `Service.AppendEpisode` | `Core.AppendEpisode` → `client.Sessions().AppendEpisode` |
| End segment | `Service.EndSession` | `Core.EndSession` → `client.Sessions().EndSession` |
| Prompt memory retrieve | `Service.Retrieve` | `Core.Retrieve` → `client.Retrieval().Retrieve` |
| Async extraction | `Service.RunExtraction` | `Core.RunExtraction` → `client.Writes().RunExtraction` |
| Manual fact write | `Service.ConsolidateCandidate` | `Core.ConsolidateCandidate` → `client.Writes().ConsolidateCandidate` |
| Forget preview/execute | `Service.PreviewForget/ExecuteForget` | `Core.PreviewForget/ExecuteForget` → `client.Forget()` |
| Mirror sync/rebuild | `Service.RunMirrorSync/RebuildMirror` | `Core.RunMirrorSync/RebuildMirror` → `client.Ops()` |
| Natural memory | `Service.RunNaturalMemory*` | `Core.RunNaturalMemory*` → `client.Ops()` |

## 5. Prompt 与数据策略

1. `FormatMemoryContextForPrompt` 继续作为唯一 prompt 格式化入口。
2. 只使用 `MemoryContext.Blocks` 与安全字段：`Summary`、`UsageGuidance`、`HistoricalStatus`、有限 source metadata。
3. 不输出 `NodeID`、`GraphActivation`、`QueryAnalysis`、`RetrievalConfidence` 等诊断字段。
4. `DoNotMention` 只能在同一 MemoryContext 内能反查安全摘要时输出，否则跳过。

## 6. 抽取策略

1. `AppendEpisode` 只写原始事件，不假设立刻可检索。
2. `FinalizeSegment`、manual pin、manual scan、idle scheduler 只入队。
3. Worker claim job 后调用 `Core.RunExtraction`。
4. apply 成功后按配置调用 `Core.RunMirrorSync`。
5. mirror/sidecar 失败默认 degraded，不影响 SQLite 权威写入成功；只有显式 fail-closed 配置才让 extraction job 失败。
6. 删除 `NewExtractionRunnerWithLLM(..., memorycore.ExtractionLLM)` 形参；provider 注入只通过 MemoryCore Options/Config/RunExtraction Provider override。

## 7. 遗忘策略

1. 用户“别再提”默认 `soft_forget`。
2. 用户“忘掉这个事实/偏好”默认 `hard_forget`。
3. 用户“这段原文不要保留”默认 `source_redact`。
4. 用户“彻底删除”使用 `purge`，必须二次确认。
5. Semantic query preview 可以生成候选，但 execute 必须使用 confirmed exact-node targets 与 PreviewHash。

## 8. 配置策略

1. `go.mod` 开发期可保留 local replace；生产要切 tag 或固定 pseudo-version/commit。
2. `config/memorycore.yaml` 保留 standalone fallback；EmoAgent runtime 以 Provider Center/runtime settings 为主。
3. API key 只保存 env var 名称。
4. Sidecar URL 必须 loopback；EmoAgent 不直接调用 sidecar HTTP endpoints。
5. `RetrievalPolicy` 不放在 `memorycore.Options`，每次 Retrieve 显式传。

## 9. 验证

必跑：

```bash
go test ./internal/memoryhost/...
go test ./...
go build ./cmd/emoagent
```

静态检查：

```bash
rg 'memorycore\.Service|memorycore\.ExtractionLLM|pkg/memorycore/extractionruntime|internal/app/memorycore|internal/store/sqlite|internal/mirror' .
rg 'host\.Service\.|\.Service\.(StartSession|EndSession|AppendEpisode|Retrieve|RunExtraction|RunMirrorSync|RunNaturalMemory)' internal/memoryhost
```

期望：除文档或刻意兼容测试外，源码无命中。

## 10. 完成标准

- EmoAgent 通过 MemoryCore c362192 或更新版本编译。
- `internal/memoryhost` 只有一个 adapter 文件知道 MemoryCore capability 分组。
- chat 热路径仍能 append episode、retrieve prompt block。
- extraction worker 通过 `Writes().RunExtraction` 工作。
- manual forget 通过 `Forget().Preview/Execute` 工作。
- sidecar 关闭时 SQLite/FTS 检索仍可用。
