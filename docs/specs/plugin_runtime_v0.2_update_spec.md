# Plugin Runtime v0.2 Update Spec

## Scope

Implement a verified, host-managed process plugin runtime while preserving builtin plugin behavior and HookBus semantics.

In scope:

- manifest v0.2 validation
- immutable package store
- Ed25519 release signature verification
- zip-slip and symlink rejection
- Python stdio JSON-RPC runtime
- process hook/tool adapters
- FacadeBroker capability and grant checks
- ProviderGateway host-side model access
- plugin audit and provider usage persistence
- admin API/UI plugin management
- minimal Python SDK and example plugin
- container mount-plan validation only

Out of scope:

- plugin-managed TCP/gRPC ports
- direct MemoryCore, TriviumDB, SQLite, or raw provider access
- raw screen/process capture
- arbitrary plugin dashboard JavaScript
- Docker/container execution
- changing canonical assistant final text behavior

## Config Defaults

`plugins.enabled` remains `false`.

Runtime defaults:

```yaml
plugins:
  store:
    root_dir: data/plugins
    allow_dev_dirs: true
  runtime:
    process_enabled: true
    python_executable: python3
    startup_timeout_ms: 5000
    shutdown_timeout_ms: 3000
    idle_timeout_seconds: 600
    crash_backoff_initial_seconds: 5
    crash_backoff_max_seconds: 300
    max_stderr_bytes: 262144
    container_enabled: false
  installer:
    github_enabled: true
    require_signature: true
    trusted_publishers_path: config/plugin_publishers.yaml
    allow_unsigned_dev: true
  provider_gateway:
    enabled: true
  admin:
    enabled: true
```

Unknown nested plugin config keys must be rejected by strict YAML loading.

## Manifest v0.2

Required shape:

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

Validation requirements:

- reject unknown YAML fields
- preserve existing plugin id regex
- require semver plugin version
- require supported semver range for `emoagent_version`
- allow `builtin`, `process`, `python_process`, `container`
- require clean relative `runtime.entry` for `python_process`
- reject absolute entry paths and `..`
- require known `access.tier`
- require known capabilities
- reuse existing hook/mode/failure policy validation

## Storage

SQLite must persist:

- installations, digest, signature status, source, store path
- enabled state and user grant JSON
- runtime status records
- access events
- provider usage
- plugin KV

Schema repair remains additive.

## Installer

Installer behavior:

- compute package digest from zip bytes
- compute manifest digest from raw `emo_plugin.yaml`
- verify Ed25519 signature descriptors with deterministic canonical payloads
- support statuses `verified`, `unsigned_dev`, `missing_signature`, `bad_signature`, `unknown_publisher`, `digest_mismatch`
- reject missing signatures when required unless local unsigned dev installs are explicitly allowed
- reject zip absolute paths, `..`, symlinks, and missing manifest
- copy validated package contents to immutable `packages/<plugin_id>/<version>`
- reject duplicate immutable package versions

## Runtime

`RuntimeSupervisor` must support:

- `EnsureReady`
- `InvokeHook`
- `InvokeTool`
- `Stop`
- `StopAll`
- `Status`

Process protocol methods:

- Host to plugin: `initialize`, `invoke_hook`, `invoke_tool`, `shutdown`, `health`
- Plugin to Host: `facade.call`, `log.emit`, `metric.emit`

Stdout is JSON-RPC only. Stderr is bounded logs. Timeouts and crashes mark runtime status as failed.

Host-side JSON-RPC handlers are bound to the manifest plugin ID by the supervisor. Any `plugin_id` supplied by a plugin is checked against that bound identity and cannot select another plugin's grant/capabilities.

The Python runner injects a `sitecustomize` audit shim that blocks direct socket listener binds and Python stdlib opens for plugin-store-external SQLite, MemoryCore, and Trivium paths. Provider key environment variables are stripped both by generic sensitive-name matching and by exact configured provider `api_key_env` names.

## Facade

`FacadeBroker.Call` must:

- check plugin registration
- check enabled state
- check manifest capability
- check user grant tier/capability list
- decode strict request params where implemented
- record allowed and denied access events

Implemented methods:

- `plugin.info`
- `plugin.kv.get`
- `plugin.kv.set`
- `plugin.files.read_text`
- `plugin.files.write_text`
- `memory.safe_context.current`
- `memory.candidate.submit`
- `memory.forget.request`
- `work.decision.observe`
- `work.dispatch.annotate`
- `approval.observe`
- `agent_affect.current`
- `provider.generate`
- `log.emit`
- `metric.emit`

## ProviderGateway

Provider/model resolution order:

1. request provider/model
2. manifest provider defaults
3. plugin gateway defaults
4. active work-summary model when no plugin/config default exists

All model calls are host-side through `llm.Client`. Plugin processes never receive raw provider API keys.

Usage records are written for success and error paths.

Optional manifest provider allowlists:

```yaml
provider:
  allowed_provider_ids: [moonshot]
  allowed_models: [kimi-k2.6]
```

When present, plugin-requested provider/model overrides outside those lists are rejected before a client is resolved.

## Admin API

Routes:

```text
GET    /api/plugins
GET    /api/plugins/{id}
POST   /api/plugins/install/local
POST   /api/plugins/install/local-zip
POST   /api/plugins/install/github-release
POST   /api/plugins/{id}/enable
POST   /api/plugins/{id}/disable
POST   /api/plugins/{id}/restart
GET    /api/plugins/{id}/status
DELETE /api/plugins/{id}
GET    /api/plugins/{id}/logs
GET    /api/plugins/{id}/access-events
GET    /api/plugins/{id}/provider-usage
```

`/local` and `/local-zip` both accept a local filesystem `path` JSON body for this development-targeted update.

## Admin UI

The `plugins` tab must show:

- installed plugin list
- enabled/runtime status
- signature status
- package and manifest digest
- source type/ref
- access tier and runtime kind
- manifest capabilities and hooks
- local install path action
- grant JSON editor
- enable/disable/restart/delete
- stderr logs
- access audit
- provider usage
- provider usage summary
- state/cache/run/workspace directories
- privacy warning

No arbitrary dashboard JavaScript is loaded.

## Python SDK

The SDK is dependency-free and provides:

- JSON-RPC stdio loop
- hook decorator
- tool decorator
- `Context.facade_call`
- `Context.provider_generate`
- `Context.kv_get`
- `Context.kv_set`
- `Context.log`

The example plugin registers `after_turn_end`, `echo`, and `provider_ping`.

## Container Mount Planner

Container execution is not implemented. The planner emits only:

```text
/plugin ro
/data rw
/cache rw
/run rw
/workspace ro|rw only when declared
```

It rejects arbitrary plugin-declared host mounts, absolute host paths, `..`, project root, MemoryCore path, and provider config path.

## Required Verification

Run:

```bash
go test ./internal/plugin ./internal/config ./internal/storage ./internal/app ./internal/web
npm --prefix web run build
```

If no web test script exists, report that `package.json` has no test script and use build/typecheck as available web verification.

Required evidence:

- manifest validation rejects unknown/invalid fields
- digest/signature verification and mismatch rejection
- zip-slip rejection
- immutable store duplicate rejection
- Python process hook invocation and HookResult
- timeout/crash failure state
- process tool namespacing and dispatcher approval gates
- facade capability denial
- ProviderGateway fake provider call plus usage audit
- plugin access event persistence
- admin API list/enable/disable/status
- container mount-plan fixed mounts and rejection cases
