# EmoAgent Plugin Runtime v0.2

## Goals

Plugin Runtime v0.2 extends the existing builtin plugin host without replacing `PluginHost`, `HookBus`, or `BuiltinRunner`.

The runtime adds:

- verified local zip/directory and GitHub release installation
- immutable plugin package storage under `plugins.store.root_dir`
- Host-managed Python process plugins over stdio JSON-RPC 2.0
- facade-gated data access and persistent access audit
- ProviderGateway-only model access with usage records
- admin API/UI lifecycle controls
- minimal dependency-free Python SDK and example plugin

`plugins.enabled` remains the runtime gate for hook/tool execution and stays `false` in the default config.

## Components

`PluginService` remains the app lifecycle entrypoint. It creates the existing builtin host/runner, then wires the v0.2 manager pieces when plugin runtime support is needed:

- `PluginStore`: immutable package path plus per-plugin state/cache/run/workspace directories.
- `PluginInstaller`: validates manifests, digest/signature descriptors, zip paths, and copies packages into the immutable store.
- `RuntimeSupervisor`: starts Python process plugins, owns stdio JSON-RPC calls, status, stderr tail, stop/restart.
- `FacadeBroker`: enforces enabled state, manifest capability, user grant tier, request decoding, and access audit.
- `ProviderGateway`: resolves provider/model and calls `llm.Client` from host-side provider config only.
- `plugin.Manager`: small owner aggregate for store/supervisor/broker/gateway.

Builtin plugins still load through `BuiltinRunner` and use the existing registrar/facade path.

## Process Runtime

Python process plugins are launched as:

```text
<python_executable> <runtime.entry>
```

For `managed_python_process`, `<python_executable>` is the Python inside the plugin's uv environment. The Host obtains the base toolchain from explicit `python_toolchain.python_executable` and `python_toolchain.uv_executable` absolute paths, verifies CPython 3.12 and the minimum uv version, and then syncs each managed plugin environment from the package-local `pyproject.toml` and `uv.lock`. The Host does not download, install, unpack, or silently fall back to another Python interpreter.

The working directory is the immutable package directory. The plugin receives a filtered inherited host environment plus Host-injected runtime variables:

```text
EMO_PLUGIN_ID
EMO_PLUGIN_VERSION
EMO_PLUGIN_ROOT
EMO_PLUGIN_STATE_DIR
EMO_PLUGIN_CACHE_DIR
EMO_PLUGIN_RUN_DIR
PYTHONUNBUFFERED=1
```

For `managed_python_process`, the package must contain `pyproject.toml` and `uv.lock`. The Host runs `uv lock --check` and `uv sync --locked --no-dev` only during install/update/configuration/repair phases, never implicitly during normal hook/tool execution. Each plugin version gets an independent uv environment under `python_toolchain.environment_root`.

Managed Python processes do not inherit the host process `PYTHONPATH`; the process path is built from the audit shim and Host-configured SDK paths. Legacy `python_process` / `process` keeps its explicit `python_executable` behavior and is not a fallback for managed plugins.

Inherited environment variables whose names contain `API_KEY`, `SECRET`, `TOKEN`, or `PASSWORD` are stripped. Configured provider `api_key_env` names from static config and SQLite are also stripped, including non-standard names. Provider API keys are never passed intentionally to plugin processes. This filtering is a host process hygiene step, not a sandbox boundary.

Stdout is JSON-RPC only; stderr is retained as bounded logs.

Host RPC handlers are bound by `RuntimeSupervisor` to the manifest plugin ID. A plugin-supplied `plugin_id` is only a consistency check and cannot impersonate another enabled plugin.

The runner injects a Python `sitecustomize` audit shim. It provides a Python-level best-effort guardrail against plugin-code socket listener binds and Python stdlib file opens for plugin-store-external SQLite, MemoryCore, and Trivium paths while preserving Python runtime internals such as Windows `asyncio` self-pipes. It is not an OS file/network sandbox and does not make bypass impossible for malicious local code.

## Hook And Tool Flow

When a process plugin is enabled:

1. The installed manifest is decoded from SQLite.
2. The compatible manifest is registered in the existing `PluginRegistry`.
3. Hook adapters are registered on the existing `HookBus`.
4. The supervisor starts the process and calls `initialize`.
5. Tool specs returned by `initialize` are registered through the existing `ToolFacade`, preserving `plugin.<id>.<tool>` namespacing and `tool.Dispatcher` approval checks.

The supervisor checks SQLite enabled state before starting or invoking a process plugin, so a disabled plugin cannot be restarted implicitly by an already registered hook/tool adapter.

## Facade Boundary

Host API does not expose direct MemoryCore, TriviumDB, SQLite, raw provider client, screen/process capture, or network listener access to plugins. All supported host access goes through `facade.call`; the Python audit guard and process lifecycle guard are best-effort guardrails, not malicious-plugin data isolation.

Implemented facade methods include:

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

Every call records `plugin_access_events` with status, capability, input/output hashes, request summary, and duration.

## Provider Gateway

`ProviderGateway.Generate` resolves provider/model in this order:

1. request `provider_id` / `model`
2. plugin manifest provider defaults
3. `plugins.provider_gateway` defaults
4. active work-summary model only when no plugin/config default exists

Optional manifest allowlists `provider.allowed_provider_ids` and `provider.allowed_models` restrict plugin-requested overrides. `ProviderGateway` builds `llm.Client` from stored provider config inside the host and records `plugin_provider_usage` for success and error paths.

## Storage

The v0.2 migration adds:

- `plugin_installations`
- `plugin_enabled_state`
- `plugin_runtime_records`
- `plugin_access_events`
- `plugin_provider_usage`
- `plugin_kv`

Schema repair is additive for plugin runtime tables and indexes.

## Admin Surface

The admin API exposes install, list, detail, enable, disable, restart, delete, status, logs, access events, and provider usage routes under `/api/plugins`.

Multiple immutable versions of the same plugin may be installed. The default detail route reports the currently enabled version when present, while `?version=` selects a specific installed version. Update and rollback are represented by enabling a chosen immutable version; this does not weaken package signature or digest policy.

When a version is enabled, the Host validates the stored user grant against that installed manifest. A grant cannot add capabilities that the manifest did not declare, cannot use unknown capability names, and cannot set a different access tier to upgrade the plugin's effective authority. An empty grant is valid but grants no facade capabilities.

The admin UI adds a `插件` tab showing installed plugin versions, access tier, signature and digest state, source, runtime state, manifest hook/capability summary, state/cache/run/workspace directories, stderr tail, access events, provider usage, and a privacy warning.

The UI does not load arbitrary plugin dashboard JavaScript.

## Container Boundary

Container execution is not implemented in v0.2. The mount planner is validation-only and emits only fixed mounts:

```text
/plugin ro
/data rw
/cache rw
/run rw
/workspace ro|rw only when manifest declares workspace
```

Plugin-declared arbitrary host mounts, absolute host paths, `..`, project root, MemoryCore, and provider config paths are rejected.
