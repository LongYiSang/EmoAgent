## 总体结论

可以改，而且现有代码里已经有一部分“桥”可以复用：**MemoryCore 本身已经支持由外部注入 ProviderRegistry 和 overrides**，只是 EmoAgent 现在打开 MemoryCore 时没有把自己的 Provider 配置传进去。最优雅的方案不是让 MemoryCore 反向依赖 EmoAgent，也不是继续让三个配置文件互相复制，而是在 **EmoAgent 侧建立统一配置中心 / EffectiveConfig 生成器**：

```text
EmoAgent Provider Center / 前端配置中心
        ↓
ConfigService 生成 Effective Runtime Config
        ├─ EmoAgent runtime config
        ├─ MemoryCore LoadEffectiveOptions / ProviderRegistry / Overrides
        └─ Sidecar generated TOML + process supervisor
```

这样可以做到：

```text
用户只在 EmoAgent 前端配置 Provider / 模型 / APIKey 环境变量名
        ↓
Emotion / Work / MemoryCore extraction / prefilter / query_analysis / curation
都通过 provider_id + model 绑定同一个 Provider Center
        ↓
Sidecar 自动按统一配置生成 TOML、启动、健康检查、关闭
```

这也符合你已有 MemoryGraph v2 的边界：SQLite 是权威事实库，TriviumDB / Sidecar 只做检索镜像和候选增强，不应该成为配置或事实权威。 当前 Sidecar README 也明确写了 SQLite 仍是 authoritative memory store，TriviumDB 可随时从 SQLite 清空重建，Sidecar 输出只是 candidate / routing hint / ranking signal。

---

# 1. 当前配置加载链路与问题定位

## 1.1 EmoAgent 主配置现状

EmoAgent 的主配置 `Config` 现在已经包含 server、chat、context、work、LLMProviders、AgentConfigs、Memory、DB、Log、Personas、WebSearch、WebFetch、Bash、Plugins 等大块配置。

其中主项目 Provider 中心的 Provider 结构是：

```go
type LLMProvider struct {
    ID
    Name
    PresetID
    Protocol
    BaseURL
    APIKeyEnv
    ModelDiscovery
    Enabled
}
```

也就是它保存的是供应商 ID、协议、BaseURL、APIKey 环境变量名、模型发现方式和启用状态。

实际 `config.yaml` 中也已经配置了 `llm_providers`，例如 Moonshot / Anthropic，并且 Emotion / Work 的模型绑定都通过 `provider_id` 指向这些 Provider。

但是 Memory 侧目前又在 EmoAgent 主配置里多放了一层：

```yaml
memory:
  extraction:
    provider:
      kind: openai-compatible
      id: memory_extractor
      base_url: https://api.deepseek.com
      api_key_env: MEMORYCORE_LLM_API_KEY
      model: deepseek-v4-flash
```

这意味着 **EmoAgent 自己已经有 Provider Center，但 memory extraction 仍然重复写了一份 provider / model / api_key_env**。当前配置文件里确实存在这段独立的 memory extraction provider。

EmoAgent 默认配置里也把 MemoryCore 配置路径固定成 `./config/memorycore.yaml`，并且默认 memory extraction provider 的 API key env 是 `MEMORYCORE_LLM_API_KEY`。

## 1.2 MemoryCore 配置现状

MemoryCore 自己也有完整配置系统。它的 `Config` 包含：

```text
schema_version
enabled
core
providers
pipelines
write_policy
retrieval
sidecar
mirror
semantic_ops
retention
forgetting_privacy
agent_affect
observability
eval
```

也就是说 MemoryCore 已经是一个完整独立运行时，而不是简单的库配置。

MemoryCore 的 ProviderConfig 又有自己的：

```go
ID
Provider
Protocol
BaseURL
APIKeyEnv
Enabled
TimeoutMS
Retry
Extra
Config
```

这和 EmoAgent 的 Provider Center 高度重叠。

当前 `config/memorycore.yaml` 里又配置了一份独立的 LLM Provider：

```yaml
providers:
  llm:
    - id: llm_main
      provider: deepseek
      protocol: openai_compatible
      base_url: https://api.deepseek.com
      api_key_env: MEMORYCORE_LLM_API_KEY
```

并且 prefilter、extraction、extraction_repair、query_analysis 都指向 `provider_id: llm_main` 和 `model: deepseek-v4-flash`。

这就是你现在遇到的第二个痛点：**MemoryCore 的 LLM Provider 和 EmoAgent 的 Provider Center 是两套来源**。

## 1.3 EmoAgent 打开 MemoryCore 的方式

当前 EmoAgent 启动时会：

1. 读取 `.env`；
2. 读取 `config.yaml`；
3. 打开 EmoAgent 自己的 DB；
4. bootstrap Provider / AgentConfig 到 DB；
5. 如果 `memory.enabled=true`，加载 manual rules，然后调用：

```go
memoryhost.OpenFromConfig(ctx, cfg.Memory.ConfigPath, logger)
```

也就是通过 `cfg.Memory.ConfigPath` 打开 MemoryCore 的独立配置。

而 `memoryhost.OpenFromConfig` 当前只把 `ConfigPath` 传给 MemoryCore：

```go
memconfig.LoadEffective(memconfig.LoadEffectiveOptions{
    ConfigPath: path,
})
```

没有传入 EmoAgent Provider Center。

这点很关键：**MemoryCore 已经有接收外部 ProviderRegistry 的能力，但 EmoAgent 还没有用。**

MemoryCore 配置包里已经定义了：

```go
ProviderRegistry
ProviderMapping
LoadEffectiveOptions{
    ConfigPath
    Overrides
    ProviderRegistry
    Runtime
}
```

并且 `LoadEffective` 会加载配置后执行 `cfg.ApplyProviderRegistry(opts.ProviderRegistry)` 和 `cfg.ApplyOverrides(opts.Overrides)`。

所以第一阶段改造不需要大拆 MemoryCore，只需要让 EmoAgent 在打开 MemoryCore 时把自己的 Provider Registry 注入进去。

## 1.4 Sidecar 现状

当前 MemoryCore 配置里有 Sidecar 配置：

```yaml
sidecar:
  enabled: true
  url: http://127.0.0.1:8765
  adapter: trivium
```

同时 mirror 也有自己的 enabled / sync_limit / rebuild_on_start 等配置。

但这只是告诉 Go 侧“去哪个 URL 找 Sidecar”。MemoryCore 的 `NewMirrorAdapter` 逻辑是：

```go
if !c.Sidecar.Enabled {
    return nil
}
if adapter == "trivium" {
    validate sidecar.url
    return NewSidecarMirrorAdapter(c.Sidecar.URL)
}
```

它只创建 HTTP client adapter，不负责启动 Python 进程。

Python Sidecar 自己的 CLI 入口也很明确：

```bash
python -m memorycore_sidecar.server --adapter trivium --config config.toml --host 127.0.0.1 --port 8765
```

`server.py` 的 main 只解析 `--adapter / --config / --host / --port`，然后 `load_config`、`create_adapter`、`create_server`。

Sidecar 的配置也是单独 TOML，包含 trivium、embedding、embedding_cache、rerank、query_analysis。

而 Python `config.py` 默认又内置 DashScope embedding / query_analysis 等默认值，并通过 `MEMORYCORE_*` 环境变量覆盖。

所以第三个痛点成立：**Sidecar 现在是外部手动进程 + 独立 TOML + 独立 embedding/rerank/query_analysis provider 环境变量。**

---

# 2. 改造目标架构

建议命名为：

```text
Unified Runtime Configuration Center
with Provider Registry Injection and Managed Memory Sidecar
```

中文：

```text
统一运行时配置中心 + Provider 注册表注入 + 受管 Memory Sidecar
```

目标架构：

```text
┌──────────────────────────────┐
│ EmoAgent Web Config Center    │
│ Provider / Agent / Memory UI  │
└───────────────┬──────────────┘
                │
                ▼
┌──────────────────────────────┐
│ ConfigService / EffectiveConfig│
│ - YAML seed                    │
│ - DB runtime overrides         │
│ - Env validation               │
│ - Dependency graph validation  │
└───────┬───────────────┬──────┘
        │               │
        │               │
        ▼               ▼
┌──────────────┐  ┌────────────────────┐
│ EmoAgent      │  │ MemoryCore Runtime  │
│ Runtime       │  │ LoadEffective       │
│ Provider DB   │  │ + ProviderRegistry  │
└──────────────┘  └──────────┬─────────┘
                              │
                              ▼
                    ┌────────────────────┐
                    │ SidecarSupervisor   │
                    │ generated TOML      │
                    │ health / restart    │
                    └──────────┬─────────┘
                               ▼
                    Python Sidecar / TriviumDB
```

核心原则：

1. **EmoAgent Provider Center 是 Provider 信息的唯一编辑入口。**
2. **MemoryCore 不读取 EmoAgent DB，不反向依赖 EmoAgent。**
3. **EmoAgent 在启动 MemoryCore 时注入 ProviderRegistry / Overrides。**
4. **Sidecar 仍然是 loopback HTTP 服务，但由 EmoAgent 可选托管启动。**
5. **Sidecar TOML 由统一配置中心生成，不再让用户手写。**
6. **APIKey 不写入 YAML / TOML / DB 明文，只保存 `api_key_env` 或未来接 Secret Store。**
7. **Feature enable/disable 必须经过依赖图校验，而不是单独 true/false。**

---

# 3. 统一配置中心的数据边界

建议把配置分成三层。

## 3.1 Bootstrap Seed：`config.yaml`

继续保留 `config.yaml`，但定位变成：

```text
首次启动 seed + 无数据库时的 fallback
```

它仍然可以配置 server、db、初始 provider、初始 agent、默认 memory preset。

但是前端修改后的运行时配置不应该反写 `config.yaml`，而应该写入 DB runtime config。

原因：你现在已经有 Provider CRUD API，Provider 会被写入 EmoAgent DB；启动时也会把初始 config provider bootstrap 到 DB。

## 3.2 Runtime Config：EmoAgent DB

建议新增统一配置表，而不是继续把所有东西都塞进单个 `config_runtime` 字符串表。

最低成本方案：

```sql
CREATE TABLE runtime_settings (
    namespace TEXT NOT NULL,
    key TEXT NOT NULL,
    value_json TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT 'ui',
    updated_by TEXT,
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY(namespace, key)
);
```

建议命名空间：

```text
system.server
chat
providers
agent
memory.core
memory.pipelines
memory.retrieval
memory.sidecar
memory.mirror
memory.semantic_ops
memory.retention
memory.forgetting
memory.agent_affect
diagnostics
```

更严格的方案是为 memory config 建结构化表；但第一版用 `runtime_settings` 更容易快速落地，并能在 API 层做 schema validation。

## 3.3 Effective Config：只读生成结果

新增 `ConfigService`：

```go
type ConfigService struct {
    SeedConfig *config.Config
    DB *storage.DB
    Env func(string) string
}

func (s *ConfigService) BuildEffective(ctx context.Context) (EffectiveConfig, error)
func (s *ConfigService) Validate(e EffectiveConfig) []ConfigIssue
func (s *ConfigService) BuildMemoryCoreOptions(e EffectiveConfig) memconfig.LoadEffectiveOptions
func (s *ConfigService) BuildSidecarSpec(e EffectiveConfig) SidecarSpec
```

`EffectiveConfig` 是运行时唯一入口：

```text
config.yaml seed
+ DB runtime settings
+ Provider DB
+ env availability check
+ dependency validation
= EffectiveConfig
```

前端展示的也应该是 EffectiveConfig，而不是某个单独 YAML 文件。

---

# 4. Provider 共享设计

## 4.1 第一阶段：复用现有 LLM Provider Center

MemoryCore 里已经有 `ProviderRegistry` 和 `ProviderMapping`，而且 `LoadEffective` 会应用这个 registry。

所以第一阶段可以做一个 Adapter：

```go
func BuildMemoryCoreProviderRegistry(providers []config.LLMProvider) memconfig.ProviderRegistry {
    return memconfig.ProviderRegistry{
        LLM: []memconfig.ProviderMapping{
            {
                ID:        provider.ID,
                Provider:  mapProtocolToMemoryCoreProvider(provider.Protocol),
                Protocol:  provider.Protocol,
                BaseURL:   provider.BaseURL,
                APIKeyEnv: provider.APIKeyEnv,
                Enabled:   provider.Enabled,
                TimeoutMS: 30000,
            },
        },
    }
}
```

映射建议：

```text
EmoAgent protocol=openai_compatible
→ MemoryCore provider=openai-compatible
→ MemoryCore protocol=openai_compatible

EmoAgent protocol=anthropic
→ 第一阶段不允许绑定给 MemoryCore extraction/prefilter/query_analysis
→ UI 显示“MemoryCore 当前仅支持 openai-compatible provider”
```

原因：MemoryCore extraction 当前会把 openai-compatible provider 映射为 ExtractionProviderOpenAICompatible；非 openai-compatible 虽然可能保留 provider string，但实际 extraction provider 支持范围需要保守处理。

然后把 MemoryCore YAML 里的：

```yaml
providers:
  llm:
    - id: llm_main ...
```

逐步改成：

```yaml
providers:
  llm: []
```

或者保留为 fallback，但只在 EmoAgent 没有注入 ProviderRegistry 时使用。

## 4.2 第二阶段：Provider Center 扩展为 AI Provider Center

Sidecar 还需要 embedding / rerank / query_analysis provider。当前 Sidecar TOML 默认用 DashScope embedding，key env 是 `DASHSCOPE_API_KEY`。

因此 Provider Center 需要从“LLM Provider”升级成“AI Provider”：

```go
type AIProvider struct {
    ID string
    Name string
    ProviderKind string // llm | embedding | rerank | multimodal_rerank
    Protocol string     // openai_compatible | anthropic | dashscope_rerank | ...
    BaseURL string
    APIKeyEnv string
    ModelDiscovery string
    Enabled bool
    Capabilities []string // chat, json_object, embedding, rerank, reasoning, vision
}
```

也可以保留现有 `llm_providers` 表，新增：

```sql
provider_kind TEXT NOT NULL DEFAULT 'llm'
capabilities_json TEXT NOT NULL DEFAULT '[]'
default_timeout_ms INTEGER
```

因为当前 EmoAgent DB 已有 `llm_providers` 表，字段包括 id、name、protocol、base_url、api_key_env、model_discovery、enabled、models_cache_json 等。

## 4.3 统一模型绑定

所有使用模型的地方都改成：

```yaml
provider_binding:
  provider_id: moonshot
  model: kimi-k2.6
  params:
    temperature: 0
    max_output_tokens: 4096
    response_format: json_object
    thinking: 
        type: disabled
    等内容...
```

适用对象：

```text
agent.emotion.main
agent.emotion.summary
agent.work.main
agent.work.summary
memory.pipelines.prefilter
memory.pipelines.extraction
memory.pipelines.extraction_repair
memory.pipelines.query_analysis
memory.semantic_ops.curation.llm
sidecar.embedding
sidecar.rerank
sidecar.query_analysis
```

这样你以后换 DeepSeek / Moonshot / Qwen / OpenRouter，只需要在 Provider Center 配置一次，然后在各 pipeline 里选择 provider_id + model。

---

# 5. Sidecar 自动启动设计

## 5.1 新增 SidecarSupervisor

在 EmoAgent 侧新增：

```text
internal/sidecar/
  supervisor.go
  spec.go
  health.go
  config_render.go
  process.go
```

核心结构：

```go
type SidecarSpec struct {
    Enabled bool
    Managed bool
    Adapter string       // fake | trivium
    Host string          // 127.0.0.1
    Port int             // 8765 or auto
    URL string           // http://127.0.0.1:8765
    WorkingDir string    // ../EmoAgent-MemoryCore/sidecar or configured path
    Command []string     // uv run python -m memorycore_sidecar.server ...
    ConfigPath string    // ./data/runtime/sidecar.generated.toml
    StartupTimeout time.Duration
    ShutdownTimeout time.Duration
    RestartPolicy string // never | on_failure
    LogPath string
}
```

启动顺序：

```text
1. EmoAgent ConfigService 生成 EffectiveConfig。
2. 判断 memory.enabled / memory.retrieval.use_mirror / sidecar.enabled。
3. 如果 sidecar.managed=true：
   3.1 生成 sidecar.generated.toml。
   3.2 准备 env，只传 APIKey 环境变量名，不写明文 key。
   3.3 exec 启动 uv/python module。
   3.4 轮询 GET /health。
   3.5 健康后把 sidecar.url 注入 MemoryCore overrides。
4. 打开 MemoryCore。
5. 启动 memory extraction worker / mirror sync worker。
6. EmoAgent 关闭时：先停 worker / MemoryCore，再停 Sidecar 子进程。
```

当前 Python Sidecar 已有 `/health`，Go sidecar client 也已经支持 health check。

## 5.2 managed / external 两种模式

```yaml
memory:
  sidecar:
    enabled: true
    managed: true
    adapter: trivium
    host: 127.0.0.1
    port: 8765
    working_dir: ../EmoAgent-MemoryCore/sidecar
    command:
      - uv
      - run
      - python
      - -m
      - memorycore_sidecar.server
    startup_timeout_ms: 15000
    shutdown_timeout_ms: 5000
```

当 `managed=false`：

```yaml
sidecar:
  enabled: true
  managed: false
  url: http://127.0.0.1:8765
```

EmoAgent 不启动进程，只做 health check；失败时按 `fail_open` 和 MemoryCore sidecar circuit breaker 降级。

当前 Sidecar 设计本来就是 optional enhancement：超时、预算、degraded、provider error、malformed response 都会回退到 SQLite authority retrieval。

## 5.3 Sidecar TOML 生成

不再让用户维护 `sidecar/config.toml`。统一配置中心生成：

```toml
[trivium]
dir = "./data/trivium"
dtype = "f32"
sync_mode = "normal"

[embedding]
provider = "openai-compatible"
base_url = "https://dashscope.aliyuncs.com/compatible-mode/v1"
api_key_env = "DASHSCOPE_API_KEY"
model = "text-embedding-v4"
dimensions = 1024
timeout_seconds = 30
encoding_format = "float"

[embedding_cache]
mode = "read_write"
db_path = "./data/embedding_cache.sqlite3"
store_raw_text = false

[rerank]
provider = "none"

[query_analysis]
provider = "openai-compatible"
base_url = "..."
api_key_env = "..."
model = "..."
temperature = 0
response_format = "json_object"
```

注意：Sidecar Python 现在仍然只接受 TOML/env，不接受 Go 直接注入 config 对象；所以第一版“受管 Sidecar”最稳的方式就是 **生成 TOML 文件**，而不是改 Python 协议。

## 5.4 安全要求

Sidecar README 已明确要求不要把 plaintext API key 写入 config，而是通过 `api_key_env` 指定环境变量。

统一配置中心也应该延续这个规则：

```text
MVP：
  只保存 api_key_env，不保存 API key 明文。

后续增强：
  可支持本地 .env.local 写入或 OS keychain；
  前端显示“已检测到 / 未检测到环境变量”，不回显 key。
```

---

# 6. 功能开关与依赖关系设计

当前配置里已经有大量 enable 开关：retrieval、use_fts、use_mirror、extraction、async、idle、manual、mirror_sync、semantic_dedup、forget、curation、sidecar、mirror、retention、agent_affect 等。MemoryCore 里也有 write triggers、semantic ops、retention、forgetting cleanup、agent affect safety 等配置。

建议把开关从“独立 bool”升级为：

```go
type FeatureFlag struct {
    Key string
    Enabled bool
    EffectiveEnabled bool
    DisabledReasons []string
    Requires []string
    Blocks []string
    Severity string // error | warning | info
}
```

## 6.1 关键依赖矩阵

| 功能                                       | 前置依赖                                                                  | 不满足时策略                 |
| ---------------------------------------- | --------------------------------------------------------------------- | ---------------------- |
| `memory.enabled`                         | `memory.core.db_path` 有效                                              | error                  |
| `memory.retrieval.enabled`               | `memory.enabled`                                                      | 自动关闭                   |
| `memory.retrieval.inject_prompt`         | `memory.retrieval.enabled`                                            | 自动关闭                   |
| `memory.retrieval.use_fts`               | `memory.enabled` + `memorycore.core.enable_fts`                       | warning + 关闭           |
| `memory.retrieval.use_mirror`            | `memory.enabled` + `memory.mirror.enabled` + `memory.sidecar.enabled` | warning + fallback FTS |
| `memory.mirror.enabled`                  | `memory.sidecar.enabled` 或 fake adapter                               | warning                |
| `memory.mirror.rebuild_on_start`         | `memory.mirror.enabled`                                               | 自动关闭                   |
| `memory.extraction.enabled`              | `memory.enabled` + extraction provider 可用                             | error                  |
| `memory.extraction.async.worker_enabled` | `memory.extraction.async.enabled`                                     | 自动关闭                   |
| `memory.extraction.idle.enabled`         | `memory.extraction.enabled` + async worker                            | 自动关闭                   |
| `manual_pin`                             | `memory.enabled` + manual rules loaded                                | warning                |
| `manual_forget`                          | `memory.enabled` + Forget Manager enabled                             | warning                |
| `semantic_dedup.enabled`                 | `memory.extraction.enabled` + mirror/sidecar 推荐                       | warning                |
| `semantic_dedup.enforce`                 | `semantic_dedup.enabled` + `shadow=false`                             | error                  |
| `query_analysis.mode=sidecar`            | `memory.sidecar.enabled`                                              | fallback `rule_only`   |
| `sidecar.managed`                        | `sidecar.enabled` + command/working_dir 有效                            | error                  |
| `rerank.enabled`                         | `sidecar.enabled` + safe summaries only                               | warning                |
| `forget.cleanup.delete_trivium_nodes`    | `memory.mirror.enabled`                                               | warning                |
| `forget.cleanup.clean_agent_affect_refs` | `agent_affect.storage_enabled`                                        | warning                |
| `retention.mirror_compaction`            | `memory.mirror.enabled`                                               | 自动关闭                   |
| `agent_affect.enabled`                   | `agent_affect.storage_enabled` + neutral fallback                     | error                  |
| `agent_affect.retrieval.weight_cap`      | `<= 0.03`                                                             | error                  |

MemoryCore 现在已经硬性校验 Agent Affect 不能绕过 user fact / sensitivity / forget，且 retrieval weight cap 不能超过 0.03。

---

# 7. 推荐的新配置结构

最终用户面对的配置建议长这样：

```yaml
memory:
  enabled: true

  core:
    db_path: ./data/memory.db
    persona_id: default
    auto_migrate: true
    enable_fts: true
    timezone: Asia/Shanghai

  provider_bindings:
    prefilter:
      enabled: true
      provider_id: moonshot
      model: kimi-k2.6
      params:
        temperature: 0
        max_output_tokens: 4096
        response_format: json_object

    extraction:
      enabled: true
      provider_id: moonshot
      model: kimi-k2.6
      mode: apply
      params:
        temperature: 0
        max_output_tokens: 4096
        response_format: json_object

    extraction_repair:
      enabled: true
      provider_id: moonshot
      model: kimi-k2.6

    query_analysis:
      enabled: true
      mode: sidecar
      provider_id: moonshot
      model: kimi-k2.6
      fallback_mode: rule_only

    embedding:
      enabled: true
      provider_id: dashscope_embedding
      model: text-embedding-v4
      dimensions: 1024

    rerank:
      enabled: false
      provider_id: dashscope_rerank
      model: qwen3-vl-rerank

  features:
    retrieval:
      enabled: true
      inject_prompt: true
      use_fts: true
      use_mirror: true
      final_memory_count: 8
      context_budget_tokens: 4096

    extraction:
      async_enabled: true
      idle_enabled: true
      manual_enabled: true
      allow_inference: true
      allow_sensitive_extraction: false
      max_facts: 12
      max_links: 20

    semantic_dedup:
      enabled: true
      shadow: false
      enforce: true

    forgetting:
      preview_enabled: true
      execute_enabled: true

    retention:
      lazy_decay: true
      daily_ttl_expiry: true
      monthly_archive: false

  sidecar:
    enabled: true
    managed: true
    adapter: trivium
    host: 127.0.0.1
    port: 8765
    working_dir: ../EmoAgent-MemoryCore/sidecar
    config_path: ./data/runtime/sidecar.generated.toml
    startup_timeout_ms: 15000
    circuit_breaker:
      enabled: true
      failure_threshold: 3
```

老的 `config/memorycore.yaml` 可以保留一段时间，但逐步降级为：

```text
MemoryCore standalone example / fallback overlay
```

而不是 EmoAgent 内部运行时的主要配置入口。

---

# 8. 前端配置中心设计

建议在现有 admin UI 上新增“配置中心”一级页面。

## 8.1 页面分区

```text
1. Provider Center
   - LLM Provider
   - Embedding Provider
   - Rerank Provider
   - 模型发现 / 测试连接 / 环境变量检测

2. Agent Config
   - Emotion main / summary
   - Work main / summary
   - Persona binding
   - Context overrides

3. Memory Core
   - DB path / persona / auto migrate / FTS
   - Write policy
   - Extraction limits
   - Manual pin / forget

4. Memory Pipelines
   - prefilter
   - extraction
   - extraction repair
   - query analysis
   - curation

5. Retrieval & Mirror
   - use_fts
   - use_mirror
   - final_memory_count
   - context budget
   - activation / MMR / fatigue

6. Sidecar
   - enabled / managed / external
   - adapter
   - host / port
   - working dir
   - generated TOML preview
   - health status
   - start / stop / restart

7. Privacy / Forgetting
   - default forget level
   - purge confirmation
   - cleanup flags
   - verification

8. Retention / Lifecycle
   - lazy decay
   - TTL expiry
   - compression
   - archive
   - mirror compaction

9. Diagnostics
   - effective config view
   - dependency graph
   - validation errors
   - sidecar logs
   - memory extraction queue
```

## 8.2 API 设计

已有 API 已经支持 LLM Provider CRUD、AgentConfig CRUD、Memory extraction queue/list 等。

建议新增：

```http
GET  /api/config/effective
POST /api/config/validate
GET  /api/config/issues

GET  /api/memory/config
PUT  /api/memory/config
GET  /api/memory/features
PUT  /api/memory/features

GET  /api/sidecar/status
POST /api/sidecar/start
POST /api/sidecar/stop
POST /api/sidecar/restart
GET  /api/sidecar/generated-config
GET  /api/sidecar/logs

POST /api/providers/{id}/test
GET  /api/providers/{id}/env-status
```

返回结构建议：

```json
{
  "effective": {...},
  "issues": [
    {
      "path": "memory.retrieval.use_mirror",
      "severity": "warning",
      "message": "use_mirror requires sidecar.enabled and mirror.enabled",
      "auto_fixed_to": false
    }
  ],
  "sidecar": {
    "enabled": true,
    "managed": true,
    "status": "healthy",
    "url": "http://127.0.0.1:8765"
  }
}
```

---

# 9. 分阶段实施方案

## Phase 0：现状保护与测试补齐

目标：先锁住当前行为，避免改配置时把记忆系统搞坏。

任务：

```text
1. 为 config.Load 增加测试：memory 配置、provider 配置、enable 校验。
2. 为 memoryhost.OpenFromConfig 增加测试：旧 memorycore.yaml 仍可打开。
3. 为 MemoryCore LoadEffective + ProviderRegistry 增加集成测试。
4. 为 Sidecar external URL 模式增加 health check 测试。
5. 记录当前 config.yaml + memorycore.yaml + sidecar TOML 的字段映射。
```

验收：

```text
go test ./...
现有 config/memorycore.yaml 不需要改仍可运行。
Sidecar 手动启动模式不受影响。
```

## Phase 1：ProviderRegistry 注入，先消除 MemoryCore LLM 重复配置

目标：MemoryCore LLM pipelines 能直接使用 EmoAgent Provider Center。

任务：

```text
1. 新增 internal/memoryhost/provider_registry.go。
2. 从 EmoAgent DB 读取 llm_providers，转换为 memconfig.ProviderRegistry。
3. 修改 memoryhost.OpenFromConfig 签名或新增 OpenEffectiveFromConfig。
4. App.Init 在 bootstrap provider 后构建 ProviderRegistry。
5. 调用 memconfig.LoadEffective 时传入 ProviderRegistry。
6. 前端 Memory pipeline 选择 provider_id + model。
7. MemoryCore YAML 中 providers.llm 变成 fallback，不再是主入口。
```

验收：

```text
memorycore.yaml 删除 providers.llm 后，MemoryCore 仍能使用 EmoAgent Provider Center 中的 provider。
extraction / prefilter / query_analysis 可绑定同一个 provider_id。
禁用 provider 时，MemoryCore pipeline 校验失败并给出清晰错误。
```

## Phase 2：统一 EffectiveConfig / Runtime Settings

目标：建立统一配置中心后端。

任务：

```text
1. 新增 runtime_settings 表或结构化 memory settings 表。
2. 新增 ConfigService。
3. 实现 config.yaml seed + DB runtime + env check 的合并。
4. 输出 EffectiveConfig。
5. 实现 dependency graph validation。
6. 新增 /api/config/effective 和 /api/config/validate。
```

验收：

```text
前端能看到最终生效配置。
所有 enable 开关都能显示 disabled reason。
无效配置不能保存为 active。
```

## Phase 3：SidecarSupervisor 自动启动

目标：启动 EmoAgent 时自动配套启动 Sidecar。

任务：

```text
1. 新增 internal/sidecar Supervisor。
2. 支持 managed / external 两种模式。
3. 生成 sidecar.generated.toml。
4. 启动 uv/python sidecar 进程。
5. 健康检查 /health。
6. 将 sidecar.url 通过 MemoryCore overrides 注入。
7. Shutdown 时关闭 child process。
8. 添加 sidecar status API。
```

验收：

```text
用户只启动 EmoAgent，Sidecar 自动启动。
use_mirror=true 时 Sidecar healthy 后 MemoryCore 才启用 mirror。
Sidecar 启动失败时，根据 fail_open 降级到 FTS，不阻断聊天。
Shutdown 后无残留 sidecar 子进程。
```

## Phase 4：前端配置中心

目标：让用户不再编辑多个 YAML/TOML 文件。

任务：

```text
1. Provider 页面增加能力类型：chat / embedding / rerank。
2. Memory Core 页面配置 core / write policy / retrieval。
3. Pipelines 页面配置 prefilter / extraction / query_analysis / curation。
4. Sidecar 页面显示 managed 状态、health、日志、generated TOML。
5. 开关 UI 显示依赖关系和 disabled reason。
6. 保存前调用 /api/config/validate。
```

验收：

```text
可以一站式配置 EmoAgent、MemoryCore、Sidecar。
Provider / model 只配置一次，可被 Emotion、Work、MemoryCore pipeline 复用。
错误配置在 UI 上明确显示原因。
```

## Phase 5：清理旧入口与文档化

目标：减少配置入口混乱。

任务：

```text
1. config/memorycore.yaml 标记为 standalone fallback。
2. sidecar/config.toml 标记为 local standalone example。
3. EmoAgent 默认使用 generated effective config。
4. 删除或废弃 memory.extraction.provider 旧字段。
5. 更新 README / docs。
6. 增加迁移脚本，把旧 memory extraction provider 迁移成 provider binding。
```

验收：

```text
普通用户只需要：
- config.yaml 初始 seed
- Web 配置中心
- .env / 环境变量

不再需要手动编辑：
- config/memorycore.yaml
- sidecar/config.toml
- 单独 Sidecar 启动命令
```

---
