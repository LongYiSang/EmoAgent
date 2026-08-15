# EmoAgent App Decomposition Spec

> Document status: Implementation Spec Draft  
> Scope: Split `internal/app.App` before adding Agent Affect.  
> Non-goal: Do not implement emotional simulation, change MemoryCore behavior, change HTTP routes, or rewrite storage/chat/work packages.

## 0. Decision

Split `App` into a thin lifecycle facade plus bounded service owners. Keep `App` as the application composition root, but remove business ownership from it. The refactor should be behavior-preserving and should first stay inside package `internal/app` to avoid import-cycle and API churn. Later PRs may move services into subpackages after seams are stable.

Recommended target shape:

```text
cmd/emoagent
  -> app.New().Init(ctx, configPath).Run(ctx)

internal/app/
  app.go                 thin facade: Init / Run / Shutdown + interface delegation only
  kernel.go              Kernel, Infra, Services, Background, Close ordering
  bootstrap.go           ordered startup pipeline
  server.go              HTTP server, route registration, static assets
  admin_facade.go        implements web.AdminApp by delegating to services
  chat_facade.go         implements chat.AppInterface by delegating to PersonaService
  persona_service.go     persona load/watch/sync CRUD
  agent_runtime_service.go active agent config, ModelRuntime, LLM client factory
  llm_provider_service.go provider CRUD/model discovery/env status
  config_service.go      runtime overrides, configcenter wrapper
  sidecar_service.go     sidecar spec/start/stop/restart/logs/status
  memory_service.go      MemoryCore host, manual rules, extraction policy, natural memory runner
  tool_service.go        built-in tool registry, dispatcher, runtimeenv facts
  plugin_service.go      PluginHost and BuiltinRunner lifecycle
  work_service.go        approval service, pending registry, delegate/resume/finish tools, cleanup job
  chat_service.go        chat.Engine construction/update and chat handler factory
```

## 1. Why this is needed

Current `App` is doing four jobs at once:

1. Resource container: Config, DB, logger, MemoryCore, sidecar, tool registry, plugin host, chat engine, approval service, active runtime.
2. Bootstrap pipeline: load config, open DB, apply runtime overrides, load/sync personas, bootstrap agent config, build LLM runtime, start persona watcher, start sidecar, open MemoryCore.
3. Runtime composition: tool registry, plugin hook, work runtime, approval registry, chat engine, memory bridge, background memory extraction.
4. Delivery layer: HTTP route registration, static assets, server lifecycle, shutdown ordering.

This is acceptable for MVP, but it makes Agent Affect dangerous to add because the natural path would be `App.AgentAffect`, more boot code in `Init`, more wiring in `Run`, and more cleanup in `Shutdown`. The desired architecture is to let Agent Affect later attach to the Emotion runtime / chat pipeline as a service contract, not as another field on `App`.

## 2. Architectural principles

### 2.1 App is a lifecycle facade, not a service locator

`App` should expose the existing external behavior required by `cmd`, `web.APIHandler`, and `chat.Handler`, but internally delegate to services. It should own only:

```go
type App struct {
    mu     sync.RWMutex
    kernel *Kernel
    cancel context.CancelFunc
}
```

`App` may remain the composition root. It should not directly know how to create MemoryCore, build work tools, start sidecar, sync personas, or update active LLM runtime.

### 2.2 Split by bounded context, not by file size

The split should follow ownership boundaries:

| Context | Owner service | Responsibilities |
|---|---|---|
| Config/runtime settings | `ConfigService` | configcenter wrapper, runtime overrides, memory config updates |
| Infra | `Infra` | config pointer, DB, logger, runtime environment facts |
| Persona | `PersonaService` | load, watch, clone, save, DB sync |
| LLM provider | `LLMProviderService` | CRUD, validation, model discovery, env status |
| Agent runtime | `AgentRuntimeService` | active config, model runtime, LLM client factory, engine update callback |
| Sidecar | `SidecarService` | BuildSidecarSpec, start/stop/restart/status/logs |
| Memory | `MemoryService` | manual rules, MemoryCore host, extraction policy, natural memory runner, bridge config |
| Tools | `ToolService` | registry, dispatcher, builtin registration, runtimeenv facts |
| Plugins | `PluginService` | plugin host, builtin runner, tool hook, shutdown |
| Work | `WorkService` | approval service, pending registry, cleanup loop, delegate/resume/finish/request tools |
| Chat | `ChatService` | chat.Engine construction, runtime updates, handler construction |
| HTTP | `Server` | routes, mux, static assets, ListenAndServe/Shutdown |

### 2.3 Service dependencies flow inward

Services may depend on `Infra`, `config`, `storage`, `memoryhost`, `tool`, `work`, `plugin`, etc. Services must not depend on `*App`. `App` delegates to services; services never call back into `App`.

Allowed dependency direction:

```text
App facade
  -> Kernel
     -> Infra
     -> Services
        -> storage/config/memoryhost/tool/work/plugin/chat/sidecar
```

Forbidden dependency direction:

```text
PersonaService -> App
MemoryService -> App
WorkService -> App
ChatService -> App
```

### 2.4 Keep current interfaces stable first

`web.AdminApp` already gives a useful seam. Keep `web.NewAPIHandler(a, logger)` working during the first refactor by making `App` implement the same methods through delegation. `chat.Handler` only requires `GetPersona` and `GetDefaultPersonaName`; keep those delegated to `PersonaService` / `AgentRuntimeService`.

### 2.5 Agent Affect extension point

This refactor should create an extension slot but not implement affect:

```go
type EmotionRuntimeServices struct {
    Memory MemoryContextProvider
    AgentAffect AgentAffectProvider // future, neutral fallback now
}
```

For now, do not add `AgentAffect` to `App`. If a stub is necessary, put it under `ChatService` or a future `EmotionService` as an interface dependency with neutral fallback. Memory boundaries remain: Agent affect may weakly modulate retrieval/expression later, but must not become user memory or bypass Memory authority/privacy gates.

## 3. Proposed runtime topology

```text
App
 ├─ Kernel
 │   ├─ Infra
 │   │   ├─ Config
 │   │   ├─ DB
 │   │   ├─ Logger
 │   │   └─ EnvironmentFacts
 │   ├─ Services
 │   │   ├─ Config
 │   │   ├─ Personas
 │   │   ├─ LLMProviders
 │   │   ├─ AgentRuntime
 │   │   ├─ Sidecar
 │   │   ├─ Memory
 │   │   ├─ Tools
 │   │   ├─ Plugins
 │   │   ├─ Work
 │   │   └─ Chat
 │   ├─ Background
 │   │   ├─ PersonaWatcher
 │   │   ├─ WorkCleanup
 │   │   ├─ MemoryExtractionWorker
 │   │   └─ NaturalMemoryRunner
 │   └─ HTTPServer
```

`Kernel.Close()` owns shutdown order:

```text
1. cancel background context
2. stop HTTP server if running
3. plugin runner shutdown
4. natural memory runner stop
5. MemoryCore close
6. sidecar stop
7. DB close
8. log stopped
```

## 4. Bootstrap pipeline

Move `Init` implementation into a `Bootstrapper`:

```go
type Bootstrapper struct {
    ConfigPath string
    ProjectRoot string
}

func (b Bootstrapper) Build(ctx context.Context) (*Kernel, context.CancelFunc, error)
```

Pipeline stages:

```text
1. load dotenv
2. load config
3. init logger
4. open DB
5. apply runtime overrides / runtime settings
6. load personas and sync to DB
7. bootstrap agent configs
8. build active agent runtime
9. start persona watcher
10. build runtime environment facts
11. build tool registry and builtin tools
12. build config/sidecar services
13. if memory enabled: start sidecar if managed/enabled, open MemoryCore, load manual rules, configure extraction policy
14. return Kernel
```

`App.Init` becomes:

```go
func (a *App) Init(ctx context.Context, configPath string) error {
    kernel, cancel, err := Bootstrapper{ConfigPath: configPath}.Build(ctx)
    if err != nil { return err }
    a.mu.Lock()
    a.kernel = kernel
    a.cancel = cancel
    a.mu.Unlock()
    kernel.Infra.Logger.Info("EmoAgent initialized")
    return nil
}
```

## 5. Run/server pipeline

Move `Run` into `Server` and `ChatService` construction.

`App.Run` should do only:

```go
func (a *App) Run(ctx context.Context) error {
    k := a.kernelSnapshot()
    srv, err := BuildServer(ctx, k)
    if err != nil { return err }
    k.HTTPServer = srv
    return srv.Run(ctx)
}
```

`BuildServer` owns:

```text
1. ensure tool registry/environment exists
2. build dispatcher
3. configure plugin hook
4. build work service/tools when active work runtime exists
5. build chat engine
6. start memory background workers
7. build chat handler
8. build web API handler
9. register routes
10. return http server wrapper
```

Route registration should move to `server.go`. No route path, HTTP method, handler name, static asset behavior, or websocket path may change.

## 6. Facades and API compatibility

Keep current public methods on `App` as delegators until all callers are migrated:

```go
func (a *App) ListPersonas() map[string]*config.Persona {
    return a.services().Personas.List()
}

func (a *App) ActivateAgentConfig(id string) error {
    return a.services().AgentRuntime.Activate(id)
}
```

Internally, prefer service methods. External packages should continue to see the same `web.AdminApp` and `chat.AppInterface` behavior.

## 7. Implementation phases

### Phase 1: Mechanical split, same package

Allowed to create new files under `internal/app`, but keep package `app`.

Move without semantic changes:

- route registration and HTTP server lifecycle -> `server.go`
- bootstrap helper functions -> `bootstrap.go`
- `Kernel`, `Infra`, `Services`, `Close` -> `kernel.go`
- runtime-related types/builders -> `agent_runtime_service.go`
- sidecar methods -> `sidecar_service.go`
- memory/natural memory methods -> `memory_service.go`

Keep existing `App` method signatures.

### Phase 2: Service delegation

Introduce services and change `App` methods to delegate. Keep tests passing after each service extraction.

Priority order:

1. `PersonaService` and `LLMProviderService`
2. `AgentRuntimeService`
3. `ConfigService` and `SidecarService`
4. `MemoryService`
5. `ToolService`, `PluginService`, `WorkService`
6. `ChatService`

### Phase 3: Remove field sprawl

Once delegation is complete, replace direct fields with `kernel`. Public direct fields on `App` should either be removed or temporarily preserved as deprecated aliases only if needed for compile compatibility.

Target invariant:

```text
No new subsystem may add a top-level field to App.
```

### Phase 4: Affect-ready seam

Add only interfaces/config placeholders, not simulation:

```go
type AgentAffectProvider interface {
    Snapshot(ctx context.Context, req AgentAffectSnapshotRequest) (AgentAffectSnapshot, error)
}
```

Default implementation returns neutral fallback. Wire it into future Emotion/Chat service composition, not into `App`.

## 8. Acceptance criteria

- `App` no longer directly contains separate fields for Config, DB, Memory, NaturalMemory, ManualMemoryRules, LLM, Logger, Personas, ActiveAgentRuntime, Sidecar, chat engine, tool registry, approval service, plugin host, plugin runner, environment.
- `App.Init`, `App.Run`, and `App.Shutdown` are short lifecycle methods that delegate to bootstrap/kernel/server.
- Existing HTTP routes and websocket path remain unchanged.
- Existing `web.AdminApp` and `chat.AppInterface` usage continues to compile.
- MemoryCore, sidecar, natural memory, work delegation, approval listing, plugin hook, agent activation, and chat settings retain current behavior.
- No Agent Affect simulation is implemented.
- The refactor has tests passing with `go test ./...` and all modified Go files are `gofmt` formatted.

## 9. Codex /goal text

/goal Refactor EmoAgent `internal/app` so `App` becomes a thin lifecycle facade instead of a composition god object. Implement a behavior-preserving split inside package `app`: introduce `Kernel/Infra/Services` ownership, move bootstrap logic out of `App.Init`, move HTTP server/routes out of `App.Run`, move shutdown ordering into `Kernel.Close`, and delegate existing `web.AdminApp` / `chat.AppInterface` methods to focused services where practical. Keep the current external API and runtime behavior unchanged.
Verification: run `gofmt` on modified Go files; run `go test ./...`; provide `git diff --stat`; show evidence that `internal/app/app.go` mainly contains `App`, `New`, `Init`, `Run`, `Shutdown`, and delegation methods, while route/server/bootstrap/service code has moved into focused files.
Constraints: do not implement Agent Affect; do not change HTTP routes, websocket path, JSON contracts, config schema, DB schema, MemoryCore behavior, sidecar config generation, chat behavior, work approval behavior, or plugin behavior. Keep `web.NewAPIHandler(a, ...)` and `chat.NewHandler(..., a, ...)` compiling via the same interface contracts.
Boundaries: allowed writes under `internal/app/**` and optional docs/tests directly related to this refactor. Avoid edits outside `internal/app` unless strictly required for compilation; forbidden paths: `internal/storage/**`, `internal/chat/**`, `internal/web/**`, `internal/memoryhost/**`, `internal/work/**`, `internal/tool/**`, `internal/plugin/**`, `web/static/**`, MemoryCore repo.
Iteration policy: make one focused structural extraction at a time; after each extraction run `gofmt` and targeted `go test ./internal/app ./internal/web ./internal/chat`; log progress in the final response with changed files and behavior-preservation notes; rerun `go test ./...` before stopping.
Stop when: code compiles, tests pass, routes and interfaces are unchanged, `App` is no longer the owner of subsystem construction details, and diff evidence shows bootstrap/server/shutdown/service responsibilities split into focused files.
Pause if: a required change would alter public API/route/config/DB behavior, create an import cycle that cannot be solved within `internal/app`, require modifying MemoryCore, or require a design decision about where future Agent Affect should be injected.
