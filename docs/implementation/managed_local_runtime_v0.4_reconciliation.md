# Managed Local Runtime v0.4 Reconciliation Report

> Phase: R0
> Date: 2026-06-21
> Worktree: `D:\Dev\Project\Agent\EmoAgent-v0.4`
> Branch: `codex/managed-local-runtime-v04`

## 1. Base and Archive

| Item | Value |
|---|---|
| `origin/main` | `e4cedb2f3e71ec6031c83dfbaed9f8f8d2dabf62` |
| v0.3 archive commit | `2555e61428a1c1ea754fdf828c2099a9c7ae2ba2` |
| archive branch | `archive/capability-runtime-v0.3-phase4-blocked` |
| archive tag | `archive-capability-runtime-v0.3-phase4-blocked` |
| archive patch | `docs/implementation/archive/capability-runtime-v0.3-phase4-blocked-2555e61.patch` |
| v0.4 continuation branch | `codex/managed-local-runtime-v04` |
| v0.4 continuation base | `2555e61428a1c1ea754fdf828c2099a9c7ae2ba2` |

R0 archived the uncommitted v0.3 Capability Runtime work as:

```text
2555e61 chore(capability-runtime): 归档 v0.3 phase4 阻塞实现
```

The archive commit contains all local Phase 0-4 WIP in one commit. `origin/main..2555e61` has no intermediate commits, so there is no Git-checkoutable Phase 3 green commit. Before archival, the WIP tree passed:

```text
go test ./...
ok ... all packages
```

This means the migration source is a tested archive WIP, not a clean Phase 3 boundary.

R24 evidence closeout tracked the archive patch and v0.4 reference documents in the continuation branch:

```text
git show 2555e61 | git patch-id --stable
0a8dd0bc1336c61f54e05e238406cdd9a1a96c5d 2555e61428a1c1ea754fdf828c2099a9c7ae2ba2

Get-Content -Raw -Encoding UTF8 docs\implementation\archive\capability-runtime-v0.3-phase4-blocked-2555e61.patch | git patch-id --stable
0a8dd0bc1336c61f54e05e238406cdd9a1a96c5d 2555e61428a1c1ea754fdf828c2099a9c7ae2ba2
```

The copied v0.4 migration guide, parent architecture, and implementation spec initially matched the ignored local source files by SHA256, then their trailing whitespace was normalized so the branch-level `git diff --check` gate remains useful. The archive patch content was not rewritten; it is tracked as archive evidence.

## 2. Phase Boundary Finding

The repository does not contain independent Phase 0, Phase 1, Phase 2, Phase 3, or Phase 4 commits. Evidence:

- Before archival, `HEAD` was `origin/main` and all Capability Runtime work was uncommitted.
- The archive commit `2555e61` is the only local commit above `origin/main`.
- `git show --name-status 2555e61` contains Result Envelope, Resource Broker, ChangeSet, plugin policy, execution/sandbox, DB/config, backend API, and ToolCard changes together.

Decision:

```text
Continue from archive commit 2555e61 in a new v0.4 worktree.
Do not reset or delete the archive branch/tag/patch.
Do not claim a separate Phase 3 green commit exists.
In R1, remove or adapt old strong-sandbox Phase 4 residue before continuing.
```

Reasoning:

- Phase 0-3 useful work is real and covered by tests.
- Strong-sandbox residue is concentrated in `internal/execution`, `internal/sandboxapi`, config defaults, Result runtime labels, and a small UI/protocol field set.
- Rebuilding from `origin/main` would require redoing more than the localized R1 cleanup.

## 3. SubAgent Findings

| SubAgent | Key output |
|---|---|
| Git/code audit | No independent Phase 3 commit exists; recommend continuing from `2555e61` and adapting/removing concentrated Phase 4 residue. |
| Windows Runtime | ADAPT command limits, output caps, timeout, and manager shape; DROP `ModeSandbox`, WSL2/bubblewrap/seatbelt defaults, and Python "Security Shim" blocking claims. |
| Plugin Policy | KEEP FacadeBroker, ResultGateway, and approval binding direction; downgrade plugin self-reported Scope/Permission/Trust to hints; add host policy and non-empty grants later. |
| Test/Migration | KEEP resource and ChangeSet DB migrations; REVIEW `capability_runtime.enabled`; DROP/REVIEW sandbox/container defaults; add migration tests for new approval binding columns later. |

## 4. Added File Inventory

| File | Purpose | R0 classification |
|---|---|---|
| `internal/app/resource_admin_service.go` | App methods for grant and ChangeSet admin APIs. | KEEP |
| `internal/app/resource_service.go` | Service wrapper for ResourceGrant and ChangeSet managers. | KEEP |
| `internal/execution/bubblewrap_integration_test.go` | Old Linux bubblewrap boundary integration test. | DROP / DEFER |
| `internal/execution/contracts_test.go` | Compile contract for v0.3 sandbox command API. | ADAPT |
| `internal/execution/manager.go` | v0.3 sandbox/unsafe execution manager. | ADAPT / DROP |
| `internal/execution/manager_test.go` | Tests for no silent fallback and sandbox dispatch. | ADAPT |
| `internal/execution/network.go` | v0.3 sandbox network grant model. | REVIEW |
| `internal/execution/network_test.go` | Tests for v0.3 network grant model. | REVIEW |
| `internal/execution/types.go` | Command request/result, limits, sandbox profile. | ADAPT |
| `internal/plugin/effective/contracts_test.go` | Compile contract for EffectivePluginGrant. | REVIEW |
| `internal/plugin/effective/types.go` | v0.3 effective grant/effect/runtime profile type. | ADAPT / REVIEW |
| `internal/plugin/transport/contracts_test.go` | Compile contract for process/container transport. | ADAPT / REVIEW |
| `internal/plugin/transport/types.go` | v0.3 transport launch/runtime contract. | ADAPT / REVIEW |
| `internal/resource/broker.go` | Host Resource Broker core implementation. | KEEP |
| `internal/resource/broker_test.go` | Broker path/root/protected path tests. | KEEP |
| `internal/resource/changeset.go` | ChangeSet staging, diff, apply, conflict, quarantine. | KEEP |
| `internal/resource/changeset_store_sqlite.go` | SQLite persistence for ChangeSet records. | KEEP |
| `internal/resource/changeset_test.go` | ChangeSet behavior tests. | KEEP |
| `internal/resource/contracts_test.go` | Compile contracts for resource types. | KEEP |
| `internal/resource/grant_store.go` | ResourceGrant store interface. | KEEP |
| `internal/resource/grant_store_sqlite.go` | SQLite ResourceGrant persistence. | KEEP |
| `internal/resource/grant_store_sqlite_test.go` | ResourceGrant DB tests. | KEEP |
| `internal/resource/roots.go` | Host resource root/profile resolution. | KEEP |
| `internal/resource/roots_test.go` | Root resolution tests. | KEEP |
| `internal/resource/types.go` | ResourceRef, Grant, PolicyDecision, ChangeSet DTOs. | KEEP / ADAPT |
| `internal/sandboxapi/contracts_test.go` | v0.3 sandbox plan compile contract. | DROP |
| `internal/sandboxapi/types.go` | v0.3 sandbox/container plan API. | DROP |
| `internal/tool/builtin/host_resource_adapter.go` | Builtin tool adapter for Resource Broker. | KEEP |
| `internal/tool/builtin/host_resource_adapter_test.go` | Adapter tests. | KEEP |
| `internal/tool/builtin/host_resource_changeset_tools.go` | Builtin ChangeSet tools. | KEEP |
| `internal/tool/builtin/host_resource_changeset_tools_test.go` | ChangeSet tool tests. | KEEP |
| `internal/tool/builtin/host_resource_tools.go` | Builtin read/list/search/copy host resource tools. | KEEP |
| `internal/tool/builtin/source_metadata.go` | Default source/provenance labels for builtins. | ADAPT |
| `internal/tool/result_gateway.go` | Host wrapping/rendering for tool result envelopes. | ADAPT |
| `internal/tool/result_gateway_test.go` | Result gateway tests. | ADAPT |
| `internal/tool/resultv2/contracts_test.go` | Compile contract for v0.3 result envelope. | ADAPT |
| `internal/tool/resultv2/types.go` | v0.3 result envelope/provenance labels. | ADAPT |
| `internal/web/resource_changesets.go` | HTTP handlers for ChangeSet admin APIs. | KEEP |
| `internal/web/resource_changesets_test.go` | ChangeSet HTTP tests. | KEEP |
| `internal/web/resource_grants.go` | HTTP handlers for ResourceGrant admin APIs. | KEEP |
| `internal/web/resource_grants_test.go` | ResourceGrant HTTP tests. | KEEP |

## 5. Modified File Groups

| Group | Files | Migration action |
|---|---|---|
| App integration | `internal/app/kernel.go`, `internal/app/server.go`, `internal/app/tool_service.go` | KEEP ResourceService registration and tool registry DB access. |
| Chat/protocol/UI provenance | `internal/chat/*`, `internal/protocol/types.go`, `web/src/chat/*` | ADAPT labels from sandbox wording to managed runtime/trust wording. |
| Config | `internal/config/config.go`, `internal/config/config_test.go` | KEEP host resources; ADAPT runtime fields; DROP/REVIEW sandbox/container defaults. |
| Plugin runtime | `internal/plugin/process_adapter.go`, `internal/plugin/process_adapter_test.go`, `internal/plugin/runtime_supervisor.go` | ADAPT to managed Python, private runtime, host-derived policy. |
| Storage | `internal/storage/schema.go`, `internal/storage/db_test.go` | KEEP resource/ChangeSet tables; add more repair tests later. |
| Tool/Work approval | `internal/tool/*`, `internal/work/*` | KEEP exact approval binding and hash-first journal; ADAPT result labels. |
| Builtin filesystem tools | `internal/tool/builtin/read_file.go`, `list_dir.go`, `write_file.go`, `edit_file.go`, `register.go` | KEEP Broker/ChangeSet path for host resource operations. |
| Bash | `internal/tool/builtin/bash.go`, `internal/tool/builtin/bash_test.go` | ADAPT to managed host process; DROP sandbox as default security claim. |

## 6. KEEP

- Host Resource Broker: roots, protected paths, resource IDs, canonical path hashes, read/list/search/stat/copy behavior.
- ResourceGrant store and grant events for brokered built-in host operations.
- ChangeSet: staging, preview, text diff, binary summary, plan hash, baseline conflict checks, create/overwrite/move/delete/mkdir/rmdir, quarantine.
- App/Web API entry points for resource grants and ChangeSets.
- Approval exact binding additions for ChangeSet ID, plan hash, resource ID, canonical path hash, baseline hash, baseline file ID, and delete mode.
- Hash-first Work journal direction for tool calls/results.
- ResultGateway concept that the Host wraps results and provider rendering uses compact `_emo_meta`.
- FacadeBroker concept as a real Host API boundary for plugin access to EmoAgent internals.
- Plugin runtime DB foundation for installations, enabled state, runtime records, access events, provider usage, and plugin KV.

## 7. ADAPT

- Result envelope:
  - Remove or stop surfacing `sandbox_profile` as a security promise.
  - Add/ensure v0.4 `OutputHash`.
  - Keep `plugin/web/file/memory -> data_only`.
  - Keep `approval/policy -> host_control`.
- Execution manager:
  - Rename/reshape from sandbox contract to managed process contract.
  - Keep timeout/output cap/unavailable result concepts.
  - Replace `ModeSandbox` default with `managed_host_process`.
- Bash:
  - Keep wrapper, command parsing, timeout cap, output cap, and destructive classifier.
  - Treat classifier only as approval/risk preview, not a sandbox boundary.
  - Move actual execution to later `processguard` with Windows Job Object.
- Plugin runtime:
  - Map `python_process` to `managed_python_process`.
  - Keep `process_dev` only as explicit unsafe developer mode.
  - Remove default `python3` fallback from normal user path.
- Plugin policy:
  - Treat plugin Scope/Permission/Trust as requested hints.
  - Host must generate Exposure/Invocation/Host API Capability.
  - Third-party default must become Work + Ask.
- Config:
  - Keep old fields loadable.
  - Change defaults away from Docker/WSL2/AppContainer/sandbox claims during R1.
- UI/protocol:
  - Replace sandbox wording with managed runtime and trust labels.

## 8. DROP / DEFER

- `internal/sandboxapi/*`.
- `BubblewrapSandbox` and its integration test as a release Gate.
- WSL2, bubblewrap, and seatbelt as default Bash requirements.
- `sandbox_endpoint` / `emo-sandboxd` as v0.4 default path.
- Docker/container default plugin runtime and mount authority.
- Runtime physical capability intersection.
- Claims that network deny/allow can constrain arbitrary Python or Bash OS network access.
- "Security Shim" language for Python audit hooks; it must become Audit Observer and cannot prove safety.

## 9. REVIEW

- `CapabilityRuntimeConfig.Enabled`: likely v0.3-era umbrella switch; rename or narrow in R1.
- `resource.Effect` and `PolicyDecision.RequiredEffects`: keep only if reduced to simple operation/risk hints.
- `internal/plugin/effective`: current TrustTier/effects/runtime profile overlap v0.4 Trust/Exposure/Invocation.
- Empty plugin user grants: current Facade behavior treats empty capabilities as all manifest capabilities; v0.4 needs explicit host policy/user grant semantics.
- HookBus policy: active hooks must default off; observe hooks need independent grant/policy checks.
- `plugin_runtime_records`: needs private Python/env/job-object fields later.
- DB tests: add assertions for new `approval_requests` ChangeSet binding columns and `idx_approval_requests_changeset_binding`.
- Container fields in v0.2 manifests: keep as legacy/future metadata, not authority.

## 10. Compatibility

- Existing `tool.Spec` and `tool.Result` remain usable through adapter wrapping.
- Provider tool loop is preserved by `_emo_meta` rendering and legacy result fallback.
- Plugin Runtime v0.2 schema remains loadable; v0.4 must reinterpret old Scope/Permission as hints.
- DB migrations are additive for ResourceGrant and ChangeSet. No committed `plugin_sandbox_instances`, `plugin_images`, `sandbox_events`, or container-only migration tables were found in the archive WIP.
- Old config fields should remain parseable, but R1 must change default semantics away from `sandbox`/`container`.

## 11. Security Invariants

- Emotion remains the only user-facing conversational actor.
- Work pause/resume and approval binding are preserved; external write apply must match exact ChangeSet/plan/resource/baseline fields.
- Host Resource Broker and ChangeSet govern built-in host file operations only; they do not claim to constrain arbitrary Python or advanced shell OS access.
- Plugin, web, file, and memory results remain data-only unless Host policy/approval code generates host-control events.
- Plugin self-reported Scope, Permission, or Trust cannot grant authority.
- Audit defaults stay hash-first/minimal; journal changes avoid raw tool input persistence.
- No Docker, WSL2, AppContainer, or system Python dependency is accepted as the v0.4 normal-user default.

## 12. Known Limitations

- There is no independent Phase 3 green commit in Git history.
- The v0.4 branch currently starts from the archive WIP `2555e61`, which still includes old Phase 4 strong-sandbox residue.
- R1 must remove/adapt old defaults before any further phase is claimed:
  - `bash.execution_mode=sandbox`.
  - Windows driver `wsl2`.
  - Linux driver `bubblewrap`.
  - `RuntimeHostSandbox` labels.
  - `process_dev`/`python3` normal-path fallback.
  - Python "Security Shim" blocking semantics.
  - plugin default `ScopeBoth`.
  - empty grant means all manifest capabilities.

## 13. R0 Gate Results

Gate status: PASS.

The first frontend verification attempt failed because this new worktree did not have `web/node_modules`, so `tsc` was unavailable:

```text
npm.cmd --prefix web run typecheck
> typecheck
> tsc -p tsconfig.json --noEmit && tsc -p tsconfig.node.json --noEmit
'tsc' is not recognized as an internal or external command,
operable program or batch file.

npm.cmd --prefix web run build
> build
> tsc -p tsconfig.json --noEmit && tsc -p tsconfig.node.json --noEmit && vite build
'tsc' is not recognized as an internal or external command,
operable program or batch file.
```

Dependency environment was restored using the tracked `web/pnpm-lock.yaml`:

```text
corepack pnpm --dir web install --frozen-lockfile
Lockfile is up to date, resolution step is skipped
Packages: +86
Done in 965ms using pnpm v11.5.2
```

Final verification:

```text
go test ./...
ok  	github.com/longyisang/emoagent/cmd/emoagent	0.248s
ok  	github.com/longyisang/emoagent/internal/agentaffect	0.843s
ok  	github.com/longyisang/emoagent/internal/app	8.131s
?   	github.com/longyisang/emoagent/internal/apperrors	[no test files]
ok  	github.com/longyisang/emoagent/internal/chat	17.366s
ok  	github.com/longyisang/emoagent/internal/config	1.303s
ok  	github.com/longyisang/emoagent/internal/configcenter	4.599s
ok  	github.com/longyisang/emoagent/internal/context	1.209s
ok  	github.com/longyisang/emoagent/internal/execution	1.024s
ok  	github.com/longyisang/emoagent/internal/llm	0.793s
ok  	github.com/longyisang/emoagent/internal/logger	0.525s
ok  	github.com/longyisang/emoagent/internal/media	2.113s
ok  	github.com/longyisang/emoagent/internal/memoryhost	9.043s
ok  	github.com/longyisang/emoagent/internal/memoryruntime	0.278s
ok  	github.com/longyisang/emoagent/internal/plugin	3.247s
ok  	github.com/longyisang/emoagent/internal/plugin/effective	0.246s
ok  	github.com/longyisang/emoagent/internal/plugin/transport	0.301s
ok  	github.com/longyisang/emoagent/internal/progress	0.291s
ok  	github.com/longyisang/emoagent/internal/promptcenter	1.078s
?   	github.com/longyisang/emoagent/internal/protocol	[no test files]
ok  	github.com/longyisang/emoagent/internal/rerank	1.163s
ok  	github.com/longyisang/emoagent/internal/resource	1.894s
ok  	github.com/longyisang/emoagent/internal/runtimeenv	1.275s
ok  	github.com/longyisang/emoagent/internal/sandboxapi	1.086s
ok  	github.com/longyisang/emoagent/internal/sidecar	1.179s
ok  	github.com/longyisang/emoagent/internal/storage	11.402s
ok  	github.com/longyisang/emoagent/internal/tool	0.373s
ok  	github.com/longyisang/emoagent/internal/tool/builtin	12.099s
ok  	github.com/longyisang/emoagent/internal/tool/builtin/tavily	5.577s
ok  	github.com/longyisang/emoagent/internal/tool/builtin/webfetch	2.786s
ok  	github.com/longyisang/emoagent/internal/tool/builtin/websearch	0.815s
ok  	github.com/longyisang/emoagent/internal/tool/resultv2	0.701s
ok  	github.com/longyisang/emoagent/internal/turn	0.834s
ok  	github.com/longyisang/emoagent/internal/web	0.801s
ok  	github.com/longyisang/emoagent/internal/work	8.025s
```

```text
npm.cmd --prefix web run typecheck
> typecheck
> tsc -p tsconfig.json --noEmit && tsc -p tsconfig.node.json --noEmit
```

```text
npm.cmd --prefix web run build
> build
> tsc -p tsconfig.json --noEmit && tsc -p tsconfig.node.json --noEmit && vite build
vite v7.3.5 building client environment for production...
transforming...
104 modules transformed.
rendering chunks...
computing gzip size...
../internal/web/static/dist/index.html                               0.48 kB | gzip:  0.29 kB
../internal/web/static/dist/admin.html                               0.57 kB | gzip:  0.31 kB
../internal/web/static/dist/plugins.html                             0.72 kB | gzip:  0.36 kB
../internal/web/static/dist/assets/styles-BKImBP8K.css              28.43 kB | gzip:  6.86 kB
../internal/web/static/dist/assets/useAdminStatus-CMiy8R-t.js        0.25 kB | gzip:  0.23 kB
../internal/web/static/dist/assets/Field-BOWC4YLj.js                 0.35 kB | gzip:  0.25 kB
../internal/web/static/dist/assets/RetentionTab-Cj3H3aUO.js          0.36 kB | gzip:  0.28 kB
../internal/web/static/dist/assets/PrivacyForgetTab-BZeW-t0n.js      0.44 kB | gzip:  0.31 kB
../internal/web/static/dist/assets/JsonSavePanel-QLf8vDzL.js         0.83 kB | gzip:  0.46 kB
../internal/web/static/dist/assets/ListPane-CwtMF-AG.js              0.99 kB | gzip:  0.52 kB
../internal/web/static/dist/assets/ChatSettingsTab-C0IV2v9K.js       1.01 kB | gzip:  0.51 kB
../internal/web/static/dist/assets/DiagnosticsTab-akIk-K6M.js        1.79 kB | gzip:  0.73 kB
../internal/web/static/dist/assets/RetrievalTab-cuXbFLcV.js          1.92 kB | gzip:  0.77 kB
../internal/web/static/dist/assets/PipelinesTab-cOQm6R38.js          2.10 kB | gzip:  0.89 kB
../internal/web/static/dist/assets/SidecarTab-CeqO8akk.js            2.56 kB | gzip:  0.94 kB
../internal/web/static/dist/assets/PersonasTab-QinCCKJY.js           3.33 kB | gzip:  1.30 kB
../internal/web/static/dist/assets/MemoryCoreTab-CbKZVHYh.js         5.41 kB | gzip:  1.54 kB
../internal/web/static/dist/assets/ProvidersTab-B0JYLirX.js          5.91 kB | gzip:  1.95 kB
../internal/web/static/dist/assets/AgentsTab-DWapXus7.js             8.81 kB | gzip:  2.70 kB
../internal/web/static/dist/assets/plugins-Cq6wN1m5.js              10.43 kB | gzip:  3.15 kB
../internal/web/static/dist/assets/WebSearchPipelineTab-DXH9pHEW.js 12.02 kB | gzip:  2.71 kB
../internal/web/static/dist/assets/PromptCenterTab-CbJRMhzh.js      12.09 kB | gzip:  3.20 kB
../internal/web/static/dist/assets/AgentAffectTab-CVGKDBae.js       14.79 kB | gzip:  3.63 kB
../internal/web/static/dist/assets/admin-B6JSVoKW.js                38.36 kB | gzip: 11.71 kB
../internal/web/static/dist/assets/index-ptThaG5c.js                68.12 kB | gzip: 21.19 kB
../internal/web/static/dist/assets/styles-DrsEoBht.js              196.43 kB | gzip: 61.94 kB
built in 738ms
```

```text
git diff --check
<no output>
```

## 14. Next Step Basis

Proceed to Phase R1 on `codex/managed-local-runtime-v04` from `2555e61`.

R1 scope should be limited to contract convergence:

- Remove/defer strong sandbox release Gate.
- Rename or adapt execution contracts to managed host process.
- Change config defaults to v0.4 managed local runtime.
- Keep Resource Broker, ChangeSet, approval binding, FacadeBroker, and ResultGateway working.
- Preserve old config/DB/v0.2 plugin compatibility while downgrading old plugin declarations to hints.
