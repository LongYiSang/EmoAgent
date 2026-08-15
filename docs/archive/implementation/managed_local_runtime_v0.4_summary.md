# Managed Local Runtime v0.4 Implementation Summary

Date: 2026-06-22

## Base

- Base commit: `e4cedb2 feat(chat): 增加会话上下文统计`
- Supersedes the archived v0.3 Capability Runtime Phase 4 branch.
- Detailed architecture and migration intent are in:
  - `docs/architecture/EmoAgent_ManagedLocalRuntime_Architecture_v0.4.md`
  - `docs/architecture/EmoAgent_CapabilityRuntime_v0.3_to_ManagedLocalRuntime_v0.4_Migration.md`
  - `docs/todo/EmoAgent_ManagedLocalRuntime_Codex_Implementation_Spec_v0.4.md`

## Implemented

- Host Resource Broker, ResourceGrant stores, configured host roots, protected paths, symlink/path containment, and built-in host resource tools.
- Previewable and recoverable ChangeSet operations for create, overwrite, move, delete, mkdir, rmdir, quarantine/restore, conflicts, plan hashes, and exact approval binding.
- Tool result provenance with producer/runtime/authority/input hash/output hash metadata and data-only provider reinjection.
- Windows ProcessGuard with Job Object process-tree lifecycle, timeout/cancel, output limits, environment filtering, and resource limits.
- Managed Bash as current-user host process automation, with approval policy, risk classification, ProcessGuard execution, provenance, and explicit non-sandbox wording.
- Managed Python plugin runtime using private Python resolution/provisioning, dependency environment provisioning, process supervision, crash/restart/idle lifecycle, and provider/tool/hook integration.
- Plugin Trust, signed package/digest checks, Host API Capability grants, Tool Work+Ask defaults, active hook policy, acknowledgement/consent drift checks, and plugin-admin risk display.
- Startup self-test and explicit `-self-test`, `-self-test-json`, `-self-test-strict` CLI diagnostics.
- Windows fresh-install smoke script that uses isolated temporary state, clears Python-related environment, restricts PATH, supports strict private-Python mode, and cleans generated state.
- Admin/API/UI updates for plugin runtime diagnostics, trust/policy summaries, resource grants/changesets, and ToolCard provenance display.
- Compatibility paths for legacy v0.2 plugin/config/database shapes and legacy runtime labelling.

## Verification

Executed during closeout:

```text
go test ./...
go test -race ./cmd/emoagent ./internal/work ./internal/resource ./internal/plugin ./internal/execution ./internal/processguard ./internal/tool
npm.cmd --prefix web run typecheck
npm.cmd --prefix web run build
git diff --check
```

Additional targeted smoke/contract checks covered:

- strict self-test fail-closed when private Python is unavailable;
- non-strict startup self-test does not block development startup;
- fresh-install smoke cleans generated state on success and failure;
- real `resume_work -> approval request -> approval binding -> host_apply_change -> ChangeSetManager -> external file write` E2E;
- private Python artifact smoke remains skipped unless `EMO_TEST_PRIVATE_PYTHON_ARTIFACT` and `EMO_TEST_PRIVATE_PYTHON_SHA256` are supplied.

## Security Invariants

- Emotion remains the only user-facing conversational actor.
- Work pause/resume and approval binding remain exact-input bound.
- Tool results, web/file/plugin outputs, and Memory content are data-only and do not grant authority.
- Plugins cannot assign their own trust level, host capability, or approval state.
- Missing managed private Python and unavailable strict smoke paths fail closed; no silent fallback to host Python for managed plugins.
- Python audit hooks, Job Objects, process separation, and Host API Capability are documented as risk controls, not malicious-plugin OS sandboxes.
- Bash is managed host process automation under current user authority, not a secure sandbox.
- Audit records use hashes, minimal summaries, and provenance rather than full sensitive payloads by default.

## Remaining Release Evidence

The implementation is complete at code level. Release status still needs external artifact/environment evidence:

- Windows installer/package carrying private Python;
- real Host-private Python artifact smoke;
- clean Windows user smoke without Python/Docker/WSL2.
