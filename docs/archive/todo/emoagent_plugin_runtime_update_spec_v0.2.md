# EmoAgent Plugin Runtime v0.2 Implementation Spec

> Target path: `docs/specs/plugin_runtime_v0.2_update_spec.md`  
> Purpose: detailed implementation spec for Codex to update the current plugin system in one focused repository pass.  
> Primary repository: `LongYiSang/EmoAgent`  
> Related repository boundary: do not modify `EmoAgent-MemoryCore` for this update.

---

## 0. Outcome

Implement EmoAgent Plugin Runtime v0.2:

```text
Verified plugin packages
+ immutable plugin store
+ Python process runtime over stdio JSON-RPC
+ Host-managed lifecycle
+ Facade-gated access
+ ProviderGateway-only model use
+ persistent plugin audit/usage
+ admin API/UI management page
+ Python SDK/template fixture
```

The current builtin plugin system must keep working.

---

## 1. Repository baseline to preserve

Do not rewrite these foundations; extend them:

```text
internal/plugin/types.go        Manifest, RuntimeKind, Capability, HookName, HookSpec, HookResult
internal/plugin/hookbus.go      HookBus dispatch, timeout, patch authorization, audit
internal/plugin/host.go         PluginHost stage/outbound wrapping
internal/plugin/registrar.go    Registrar, HookRegistrar, ToolFacade
internal/plugin/facade.go       Memory/Work/Approval/AgentAffect facade stubs
internal/plugin/builtin.go      BuiltinRunner and builtin plugins
internal/app/plugin_service.go  app lifecycle entry for plugins
internal/config/config.go       PluginsConfig
internal/storage/schema.go      SQLite migrations
web/src/admin/AdminApp.tsx      tab-based admin app
web/src/admin/lib/adminData.ts  tab metadata
```

Current behavior that must remain true:

```text
- builtin plugins can be loaded from config.
- HookBus priority, timeout, fail-open/fail-closed, and patch conflict behavior remains compatible.
- plugin tools are namespaced and still go through tool.Dispatcher approval gates.
- safe views do not expose raw prompt/tool output/reasoning by default.
- plugins.enabled still requires turn_pipeline enablement.
```

---

## 2. Target file additions

Suggested new files:

```text
docs/architecture/plugin_runtime_v0.2.md
docs/specs/plugin_runtime_v0.2_update_spec.md

internal/plugin/manifest_v2.go
internal/plugin/manifest_v2_test.go
internal/plugin/installer.go
internal/plugin/installer_test.go
internal/plugin/signature.go
internal/plugin/signature_test.go
internal/plugin/store.go
internal/plugin/store_test.go
internal/plugin/manager.go
internal/plugin/manager_test.go
internal/plugin/runtime_supervisor.go
internal/plugin/runtime_supervisor_test.go
internal/plugin/process_runner.go
internal/plugin/process_runner_test.go
internal/plugin/jsonrpc.go
internal/plugin/jsonrpc_test.go
internal/plugin/process_adapter.go
internal/plugin/process_adapter_test.go
internal/plugin/facade_broker.go
internal/plugin/facade_broker_test.go
internal/plugin/provider_gateway.go
internal/plugin/provider_gateway_test.go
internal/plugin/storage_facade.go
internal/plugin/storage_facade_test.go
internal/plugin/container_mounts.go
internal/plugin/container_mounts_test.go
internal/plugin/admin_views.go

internal/app/plugin_admin_service.go
internal/web/plugin_api.go
internal/web/plugin_api_test.go

sdk/python/emoagent_plugin/__init__.py
sdk/python/emoagent_plugin/plugin.py
sdk/python/emoagent_plugin/rpc.py
sdk/python/emoagent_plugin/types.py
sdk/python/examples/echo_plugin/emo_plugin.yaml
sdk/python/examples/echo_plugin/main.py
sdk/python/examples/echo_plugin/README.md

web/src/admin/protocol/pluginApi.ts
web/src/admin/hooks/usePluginAdmin.ts
web/src/admin/tabs/PluginsTab.tsx
```

Allowed modifications:

```text
internal/plugin/types.go
internal/plugin/registry.go
internal/plugin/host.go
internal/plugin/registrar.go
internal/plugin/facade.go
internal/plugin/builtin.go
internal/app/plugin_service.go
internal/app/kernel.go
internal/app/server.go
internal/config/config.go
internal/storage/schema.go
web/src/admin/AdminApp.tsx
web/src/admin/lib/adminData.ts
web/src/admin/hooks/useAdminBootstrap.ts
web/src/admin/styles or shared styles if needed
config.yaml
```

Forbidden for this update:

```text
internal/memorycore direct changes
EmoAgent-MemoryCore repository changes
raw MemoryCore sidecar access from plugins
raw provider key exposure
arbitrary plugin dashboard JS loading
plugin-managed TCP/gRPC listener support
screen capture/process observation implementation
```

---

## 3. Config changes

Extend `PluginsConfig` while preserving existing fields and strict YAML unknown-field checks.

Suggested structs:

```go
type PluginsConfig struct {
    Enabled          bool
    Directories      []string
    BuiltinEnabled   []string
    RolloutPercent   int
    DefaultTimeoutMS int
    MaxTimeoutMS     int
    FailClosedHooks  []string
    Audit            PluginAuditConfig

    Store            PluginStoreConfig
    Runtime          PluginRuntimeConfig
    Installer        PluginInstallerConfig
    ProviderGateway  PluginProviderGatewayConfig
    Admin            PluginAdminConfig
}

type PluginStoreConfig struct {
    RootDir      string `yaml:"root_dir" json:"root_dir"`
    AllowDevDirs bool   `yaml:"allow_dev_dirs" json:"allow_dev_dirs"`
}

type PluginRuntimeConfig struct {
    ProcessEnabled           bool `yaml:"process_enabled" json:"process_enabled"`
    PythonExecutable         string `yaml:"python_executable" json:"python_executable"`
    StartupTimeoutMS         int `yaml:"startup_timeout_ms" json:"startup_timeout_ms"`
    ShutdownTimeoutMS        int `yaml:"shutdown_timeout_ms" json:"shutdown_timeout_ms"`
    IdleTimeoutSeconds       int `yaml:"idle_timeout_seconds" json:"idle_timeout_seconds"`
    CrashBackoffInitialSeconds int `yaml:"crash_backoff_initial_seconds" json:"crash_backoff_initial_seconds"`
    CrashBackoffMaxSeconds   int `yaml:"crash_backoff_max_seconds" json:"crash_backoff_max_seconds"`
    MaxStderrBytes           int `yaml:"max_stderr_bytes" json:"max_stderr_bytes"`
    ContainerEnabled         bool `yaml:"container_enabled" json:"container_enabled"`
}

type PluginInstallerConfig struct {
    GithubEnabled       bool `yaml:"github_enabled" json:"github_enabled"`
    RequireSignature    bool `yaml:"require_signature" json:"require_signature"`
    TrustedPublishersPath string `yaml:"trusted_publishers_path" json:"trusted_publishers_path"`
    AllowUnsignedDev    bool `yaml:"allow_unsigned_dev" json:"allow_unsigned_dev"`
}

type PluginProviderGatewayConfig struct {
    Enabled bool `yaml:"enabled" json:"enabled"`
    DefaultProviderID string `yaml:"default_provider_id" json:"default_provider_id"`
    DefaultModel string `yaml:"default_model" json:"default_model"`
}

type PluginAdminConfig struct {
    Enabled bool `yaml:"enabled" json:"enabled"`
}
```

Defaults:

```text
store.root_dir = data/plugins
store.allow_dev_dirs = true
runtime.process_enabled = true
runtime.python_executable = python3
runtime.startup_timeout_ms = 5000
runtime.shutdown_timeout_ms = 3000
runtime.idle_timeout_seconds = 600
runtime.crash_backoff_initial_seconds = 5
runtime.crash_backoff_max_seconds = 300
runtime.max_stderr_bytes = 262144
runtime.container_enabled = false
installer.github_enabled = true
installer.require_signature = true
installer.allow_unsigned_dev = true only when store.allow_dev_dirs=true
provider_gateway.enabled = true
admin.enabled = true
```

Update `config.yaml` with defaults but keep `plugins.enabled: false` unless intentionally changed.

---

## 4. Manifest v0.2

Keep existing `Manifest` fields compatible. Add a richer manifest type or embed existing `Manifest`.

Required YAML fields:

```yaml
schema_version: emoagent.plugin.v0.2
id: com.example.echo
name: Echo Plugin
version: 0.1.0
emoagent_version: ">=0.2.0"
runtime:
  kind: python_process
  entry: main.py
access:
  tier: runtime_safe
  capabilities:
    - turn.read
hooks:
  - name: after_turn_end
    mode: observe
    failure_policy: fail_open
    priority: 100
    timeout_ms: 200
```

Validation:

```text
- reject unknown YAML fields
- plugin id regex stays compatible with existing validPluginID
- semver version required
- semver range supports existing syntax plus >= if already supported
- runtime.kind allowed: builtin, process, python_process, container
- python_process requires runtime.entry
- entry must be relative, clean, not absolute, no `..`
- access.tier must be known
- capabilities must be known or explicitly added to KnownCapability
- hook names/modes/failure policies still use existing KnownHook/KnownHookMode/KnownFailurePolicy
- hook timeout <= host MaxTimeoutMS
```

Add capabilities:

```go
CapabilityProviderGenerate = "provider.generate"
CapabilityProviderEmbed = "provider.embed"
CapabilityPluginKV = "plugin.kv"
CapabilityPluginFiles = "plugin.files"
CapabilityNetworkWeb = "network.web"
CapabilityPluginAdminRead = "plugin.admin.read" // optional internal use
```

Do not remove current capabilities.

---

## 5. Storage migrations

Add next migration in `internal/storage/schema.go`.

Tables:

```sql
CREATE TABLE IF NOT EXISTS plugin_installations (
    id TEXT PRIMARY KEY,
    plugin_id TEXT NOT NULL,
    version TEXT NOT NULL,
    name TEXT NOT NULL,
    manifest_json TEXT NOT NULL,
    source_type TEXT NOT NULL,
    source_ref TEXT NOT NULL DEFAULT '',
    package_digest TEXT NOT NULL DEFAULT '',
    manifest_digest TEXT NOT NULL DEFAULT '',
    signature_status TEXT NOT NULL DEFAULT '',
    publisher_id TEXT NOT NULL DEFAULT '',
    installed_at TEXT NOT NULL DEFAULT (datetime('now')),
    installed_by TEXT NOT NULL DEFAULT 'local',
    store_path TEXT NOT NULL,
    UNIQUE(plugin_id, version)
);

CREATE TABLE IF NOT EXISTS plugin_enabled_state (
    plugin_id TEXT PRIMARY KEY,
    version TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0,1)),
    user_grant_json TEXT NOT NULL DEFAULT '{}',
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS plugin_runtime_records (
    plugin_id TEXT PRIMARY KEY,
    version TEXT NOT NULL DEFAULT '',
    runtime_kind TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'stopped',
    pid INTEGER,
    last_started_at TEXT,
    last_stopped_at TEXT,
    last_error TEXT NOT NULL DEFAULT '',
    restart_count INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS plugin_access_events (
    id TEXT PRIMARY KEY,
    plugin_id TEXT NOT NULL,
    access_kind TEXT NOT NULL,
    capability TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    request_summary TEXT NOT NULL DEFAULT '',
    input_hash TEXT NOT NULL DEFAULT '',
    output_hash TEXT NOT NULL DEFAULT '',
    duration_ms INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_plugin_access_events_plugin_time
    ON plugin_access_events(plugin_id, created_at DESC);

CREATE TABLE IF NOT EXISTS plugin_provider_usage (
    id TEXT PRIMARY KEY,
    plugin_id TEXT NOT NULL,
    provider_id TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    purpose TEXT NOT NULL DEFAULT '',
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    estimated_tokens INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL,
    error_message TEXT NOT NULL DEFAULT '',
    duration_ms INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_plugin_provider_usage_plugin_time
    ON plugin_provider_usage(plugin_id, created_at DESC);

CREATE TABLE IF NOT EXISTS plugin_kv (
    plugin_id TEXT NOT NULL,
    key TEXT NOT NULL,
    value_json TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY(plugin_id, key)
);
```

Implement DB methods for list/get/upsert/toggle/runtime/audit/usage/kv. Keep `ApplySchemaRepairs` additive if needed.

---

## 6. Installer and signature

### 6.1 Installer interfaces

```go
type PluginInstaller struct { ... }
func (i *PluginInstaller) InstallFromZip(ctx context.Context, path string) (InstallResult, error)
func (i *PluginInstaller) InstallFromDirectory(ctx context.Context, path string) (InstallResult, error)
func (i *PluginInstaller) InstallFromGitHubRelease(ctx context.Context, owner, repo, tag, asset string) (InstallResult, error)
```

### 6.2 Digest

```text
- compute sha256 over zip bytes for package_digest
- reject mismatch if expected digest exists
- compute sha256 over raw emo_plugin.yaml for manifest_digest
```

### 6.3 Signature

Implement Ed25519 verification using stdlib.

Trusted publisher file:

```yaml
publishers:
  - id: example
    display_name: Example Publisher
    public_keys:
      - id: main
        algorithm: ed25519
        public_key_base64: ...
```

Signature payload is canonical release descriptor without the `signature` field. Keep canonicalization deterministic: marshal a normalized struct, not arbitrary map ordering.

Statuses:

```text
verified
unsigned_dev
missing_signature
bad_signature
unknown_publisher
digest_mismatch
```

If `installer.require_signature=true`, only `verified` and explicitly allowed `unsigned_dev` local installs pass.

### 6.4 Zip extraction security

Reject:

```text
- absolute paths
- paths containing `..`
- symlinks
- files larger than configurable max if added
- package without emo_plugin.yaml
```

Extract into temp dir, validate, then atomically move to immutable store.

---

## 7. RuntimeSupervisor and ProcessRunner

### 7.1 Interfaces

```go
type RuntimeSupervisor struct { ... }
func (s *RuntimeSupervisor) EnsureReady(ctx context.Context, pluginID string) (*PluginRuntime, error)
func (s *RuntimeSupervisor) InvokeHook(ctx context.Context, pluginID string, hook HookName, hc HookContext) (HookResult, error)
func (s *RuntimeSupervisor) InvokeTool(ctx context.Context, pluginID string, tool string, input json.RawMessage) (json.RawMessage, error)
func (s *RuntimeSupervisor) Stop(ctx context.Context, pluginID string) error
func (s *RuntimeSupervisor) StopAll(ctx context.Context) error
func (s *RuntimeSupervisor) Status(pluginID string) RuntimeStatus
```

### 7.2 Process launch

Command:

```text
<python_executable> <entry>
```

Working directory:

```text
immutable package directory
```

Environment:

```text
EMO_PLUGIN_ID
EMO_PLUGIN_VERSION
EMO_PLUGIN_ROOT
EMO_PLUGIN_STATE_DIR
EMO_PLUGIN_CACHE_DIR
EMO_PLUGIN_RUN_DIR
PYTHONUNBUFFERED=1
```

Do not pass provider API key env vars intentionally. If inherited process env is unavoidable, add a sanitization option and tests that Provider key names from configured providers are removed from plugin environment.

### 7.3 JSON-RPC framing

Use newline-delimited JSON-RPC 2.0 on stdout/stdin:

```json
{"jsonrpc":"2.0","id":"1","method":"initialize","params":{...}}
```

Rules:

```text
- stdout is JSON-RPC only
- stderr is logs
- each request has a deadline from context
- process protocol error marks runtime failed
```

### 7.4 Required plugin methods

Host calls:

```text
initialize
invoke_hook
invoke_tool
shutdown
health
```

Plugin may call Host:

```text
facade.call
log.emit
metric.emit
```

Implement `facade.call` dispatch in Go side with capability checks.

### 7.5 Adapter registration

When enabling a process plugin:

1. Decode manifest.
2. Register manifest in existing `PluginRegistry`.
3. For each declared HookSpec, register a `RegisteredHook` whose handler calls `RuntimeSupervisor.InvokeHook`.
4. If plugin declares tools in initialize response, register namespaced tool handlers that call `RuntimeSupervisor.InvokeTool`.

---

## 8. FacadeBroker

Implement a single dispatcher:

```go
type FacadeBroker struct { ... }
func (b *FacadeBroker) Call(ctx context.Context, pluginID string, method string, params json.RawMessage) (json.RawMessage, error)
```

Methods:

```text
plugin.info
plugin.kv.get
plugin.kv.set
plugin.files.read_text
plugin.files.write_text
memory.safe_context.current
memory.candidate.submit
memory.forget.request
work.decision.observe
work.dispatch.annotate
approval.observe
agent_affect.current
provider.generate
log.emit
metric.emit
```

v0.2 may omit web.search/web.fetch implementation if no stable app service exists, but reserve method names and return capability/error clearly.

Every method must:

```text
- check plugin enabled
- check manifest capability
- check user grant tier
- check request shape
- write plugin_access_events
- avoid returning raw hidden/forgotten/purged data
```

---

## 9. ProviderGateway

### 9.1 Interface

```go
type ProviderGateway struct { ... }
func (g *ProviderGateway) Generate(ctx context.Context, pluginID string, req PluginGenerateRequest) (PluginGenerateResponse, error)
```

Request:

```go
type PluginGenerateRequest struct {
    Purpose string
    ProviderID string
    Model string
    System string
    Messages []llm.Message
    Params llm.RequestParams
    MaxTokens int
    Temperature *float64
}
```

Response:

```go
type PluginGenerateResponse struct {
    Content string
    Model string
    Usage llm.Usage
    StopReason string
}
```

### 9.2 Resolution

Provider/model resolution order:

```text
1. request provider/model if allowed and enabled
2. plugin manifest provider defaults
3. plugins.provider_gateway defaults
4. active agent summary model as fallback only if safe
```

Build `llm.Client` from stored provider config. Do not expose API keys to plugin.

### 9.3 Usage audit

Always write `plugin_provider_usage`, even on error, with token usage when provider returns it and rough estimate otherwise.

No hard quota in v0.2.

---

## 10. PluginService integration

Extend `PluginService` to own:

```go
type PluginService struct {
    infra *Infra
    tools *ToolService
    agentAffect *AgentAffectService
    providers *LLMProviderService or infra access
    host *plugin.PluginHost
    runner *plugin.BuiltinRunner
    manager *plugin.Manager
    supervisor *plugin.RuntimeSupervisor
    facadeBroker *plugin.FacadeBroker
    providerGateway *plugin.ProviderGateway
}
```

`Configure` should:

```text
- create PluginHost as today
- load builtin plugins as today
- create Manager/Supervisor/Broker/Gateway
- load enabled process plugins from SQLite/file store
- register hook adapters for process plugins
- set ToolHook as today
```

`Close` should:

```text
- stop process supervisors first
- shutdown builtin runner
- clear host
```

---

## 11. Admin HTTP API

Add routes in `internal/app/server.go`:

```text
GET    /api/plugins
GET    /api/plugins/{id}
POST   /api/plugins/install/local-zip
POST   /api/plugins/install/github-release
POST   /api/plugins/{id}/enable
POST   /api/plugins/{id}/disable
POST   /api/plugins/{id}/restart
DELETE /api/plugins/{id}
GET    /api/plugins/{id}/logs
GET    /api/plugins/{id}/access-events
GET    /api/plugins/{id}/provider-usage
```

If multipart upload is too much for v0.2, implement local path install first and make UI call local-path API in dev mode. Keep API names stable for future drag/drop.

Views:

```go
type PluginSummaryView struct {
    ID string
    Name string
    Version string
    Enabled bool
    RuntimeKind string
    RuntimeStatus string
    AccessTier string
    Capabilities []string
    SignatureStatus string
    PublisherID string
    SourceType string
    LastError string
    ProviderUsageToday PluginProviderUsageSummary
}
```

---

## 12. Admin UI

Add `plugins` tab.

Modify:

```text
web/src/admin/lib/adminData.ts
web/src/admin/AdminApp.tsx
web/src/admin/hooks/useAdminBootstrap.ts
```

Add:

```text
web/src/admin/protocol/pluginApi.ts
web/src/admin/hooks/usePluginAdmin.ts
web/src/admin/tabs/PluginsTab.tsx
```

UI sections:

```text
- Installed plugin list
- Selected plugin details
- Source/digest/signature card
- Runtime status card with start/restart/disable buttons
- Access tier and capabilities card
- Privacy disclaimer card
- Mount/directories card
- Recent access events
- Provider usage
- Log tail
- Install form: local path + GitHub release fields
```

Privacy disclaimer exact meaning:

```text
EmoAgent 会按插件声明的层级限制并记录访问，但不承诺插件不会带来隐私风险。启用高层级插件表示你允许该插件通过 EmoAgent 接口访问对应类别的数据。
```

---

## 13. Python SDK and example plugin

SDK should be minimal and dependency-free.

Example:

```python
from emoagent_plugin import Plugin, hook

plugin = Plugin()

@hook("after_turn_end")
async def after_turn_end(ctx):
    await ctx.log("info", "turn ended", {"turn_id": ctx.turn.turn_id})
    return {"annotations": {"echo_plugin": "observed"}}

if __name__ == "__main__":
    plugin.run_stdio()
```

SDK responsibilities:

```text
- JSON-RPC stdio loop
- decorator registration for hooks/tools
- Context object with facade.call helper
- provider.generate helper
- kv get/set helper
- log helper writes facade/log or stderr safely
```

Example plugin must be usable in tests.

---

## 14. Container mount planner

Implement validation-only mount planner:

```go
type MountPlan struct { Mounts []Mount }
type Mount struct { HostPath string; ContainerPath string; Mode string }
func BuildContainerMountPlan(plugin PluginRuntimeRecord, store PluginStore) (MountPlan, error)
```

Rules:

```text
/plugin ro -> immutable package dir
/data rw -> state/<plugin_id>
/cache rw -> cache/<plugin_id>
/run rw -> run/<plugin_id>
/workspace none/ro/rw -> workspaces/<plugin_id> only when declared
```

Tests reject arbitrary host paths, project root, MemoryCore path, provider config path, absolute plugin-declared paths, and `..`.

Do not require Docker in tests.

---

## 15. Tests and verification

Run at least:

```bash
go test ./internal/plugin ./internal/config ./internal/storage ./internal/app ./internal/web
npm --prefix web test -- --run
npm --prefix web run build
```

If repo does not have web tests, run existing available build/lint scripts and report exact commands used.

Required Go tests:

```text
- ManifestV2 rejects unknown fields and invalid runtime/access/capability.
- Installer verifies digest/signature and rejects mismatch.
- Installer rejects zip-slip paths.
- Store writes immutable version layout.
- Supervisor starts a fixture Python plugin, invokes hook, shuts down.
- Supervisor handles timeout and crash fail-open/fail-closed.
- FacadeBroker denies missing capabilities.
- ProviderGateway uses configured provider without exposing API key to plugin env.
- Tool registration from process plugin is namespaced and still requires approval for destructive tool.
- Admin API lists plugins and toggles enable state.
- Container mount planner emits only fixed mounts.
```

---

## 16. Acceptance criteria

Completion evidence:

```text
1. `docs/architecture/plugin_runtime_v0.2.md` and this spec exist.
2. Existing builtin plugin tests still pass.
3. A sample Python process plugin can be installed/enabled from local fixture.
4. HookBus can dispatch `after_turn_end` or `after_memory_retrieve` to the Python plugin and receive HookResult.
5. Process plugin can register a namespaced tool through Host bridge.
6. Plugin ProviderGateway call succeeds against a fake llm client and records usage.
7. Plugin page displays access tier, signature/digest status, runtime state, events, usage, and warning.
8. No plugin code receives raw Provider API keys in tests.
9. No plugin has direct MemoryCore/TriviumDB access path.
10. All relevant Go and web checks pass.
```

---

## 17. Implementation notes and risk controls

- Keep v0.2 JSON-RPC simple. Do not add gRPC.
- Keep process plugins disabled unless `plugins.enabled && plugins.runtime.process_enabled`.
- Make installer deterministic and testable with local zip/httptest before GitHub network path.
- Keep UI useful even when plugins are disabled; show disabled state and config hint.
- Keep access tiers additive. A lower tier plugin must not call higher tier facade even if it guesses method names.
- Avoid large refactors in chat/work/memory. Use PluginService as integration point.
- Do not change canonical assistant content behavior.

