# Prompt Center Phase 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development for review/check work. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Prompt Center a trustworthy observation and governance panel for real injected prompts without changing default chat behavior or Work runtime behavior.

**Architecture:** Keep MVP override semantics unchanged. Extend snapshot data at the promptcenter/storage boundary, enrich Emotion/Summary assemblers with audit components, expose full preview through the existing Prompt Center API, and enhance the existing React admin tab without introducing new runtime prompt control surfaces.

**Tech Stack:** Go backend (`internal/promptcenter`, `internal/context`, `internal/chat`, `internal/storage`, `internal/app`, `internal/web`), SQLite storage, React/Vite admin UI under `web/src/admin`.

---

### Task 1: Snapshot v2 Backend

**Files:**
- Modify: `internal/promptcenter/snapshot.go`
- Modify: `internal/promptcenter/render.go`
- Modify: `internal/promptcenter/component.go`
- Modify: `internal/promptcenter/store.go`
- Modify: `internal/promptcenter/memory_store.go`
- Modify: `internal/context/assembler.go`
- Modify: `internal/chat/engine.go`
- Modify: `internal/chat/turn_runtime.go`
- Modify: `internal/config/config.go`
- Modify: `internal/app/chat_service.go`
- Modify: `internal/storage/prompt_store.go`
- Test: `internal/promptcenter/promptcenter_test.go`
- Test: `internal/context/context_test.go`
- Test: `internal/chat/engine_test.go`
- Test: `internal/chat/turn_runtime_test.go`
- Test: `internal/storage/prompt_store_test.go`
- Test: `internal/config/config_test.go`

- [ ] Add RED tests for dynamic component metadata, default Emotion prompt compatibility, inbound request_id snapshot, truncation/final_hash, cleanup retention/max rows.
- [ ] Extend `RenderComponent` with name, section, kind, editable, dynamic, text length, truncated, metadata JSON.
- [ ] Add dynamic source constants and `DynamicComponent`.
- [ ] Add `PromptSnapshotConfig` defaults and inject it into `chat.Engine`.
- [ ] Append persona/runtime/pending/memory/agent_affect/extra_system components without changing system text order.
- [ ] Truncate or omit stored `rendered_text` according to config while hashing the full prompt.
- [ ] Add `CleanupRenderSnapshots` for SQLite and memory store.
- [ ] Run `go test ./internal/promptcenter ./internal/storage ./internal/context ./internal/chat ./internal/config`.

### Task 2: Running Summary Snapshots

**Files:**
- Modify: `internal/context/types.go`
- Modify: `internal/context/summary.go`
- Modify: `internal/chat/engine.go`
- Test: `internal/context/context_test.go`
- Test: `internal/chat/engine_test.go`

- [ ] Add RED tests for update and repair prompt audit.
- [ ] Add `SummaryPromptAudit` to summary reports.
- [ ] Record only summary system prompt snapshots with purposes `context.running_summary.update` and `context.running_summary.repair`.
- [ ] Run `go test ./internal/context ./internal/chat`.

### Task 3: Preview v2 Backend

**Files:**
- Modify: `internal/promptcenter/api_types.go`
- Modify: `internal/app/prompt_center_service.go`
- Modify: `internal/web/prompt_center.go`
- Test: `internal/app/prompt_center_admin_service_test.go`
- Test: `internal/web/prompt_center_api_test.go`

- [ ] Add RED tests for full Emotion preview and compatible component preview.
- [ ] Extend preview request/response with mode, session, user message, include flags, components, and warnings.
- [ ] Render full Emotion preview through the same assembler, without memory or affect side effects by default.
- [ ] Run `go test ./internal/app ./internal/web`.

### Task 4: UI Snapshot Detail And Full Preview

**Files:**
- Modify: `web/src/admin/protocol/promptCenterApi.ts`
- Modify: `web/src/admin/hooks/usePromptCenterAdmin.ts`
- Modify: `web/src/admin/tabs/PromptCenterTab.tsx`

- [ ] Extend TypeScript types for components, warnings, snapshot detail, and override save response.
- [ ] Add snapshot detail selection, rendered prompt viewer, component table, metadata display, truncated/hash-only notices, and copy buttons.
- [ ] Add full Emotion prompt preview controls.
- [ ] Run `npm --prefix web run typecheck` and `npm --prefix web run build`.

### Task 5: Override Governance And Lint

**Files:**
- Modify: `internal/promptcenter/api_types.go`
- Modify: `internal/promptcenter/validate.go`
- Modify: `internal/app/prompt_center_service.go`
- Modify: `internal/web/prompt_center.go`
- Modify: `web/src/admin/protocol/promptCenterApi.ts`
- Modify: `web/src/admin/hooks/usePromptCenterAdmin.ts`
- Modify: `web/src/admin/tabs/PromptCenterTab.tsx`
- Test: `internal/promptcenter/promptcenter_test.go`
- Test: `internal/app/prompt_center_admin_service_test.go`
- Test: `internal/web/prompt_center_api_test.go`

- [ ] Add RED tests for global/agent/effective stale flags and prompt lint warnings.
- [ ] Return warnings from override saves.
- [ ] Show separate stale warnings and save-time lint warnings in UI.
- [ ] Add protocol-sensitive confirm in UI only.
- [ ] Run targeted Go tests and frontend checks.

### Task 6: Final Verification And Spec Review

**Files:**
- Read: `docs/todo/EmoAgent_PromptCenter_Phase2_Spec.md`
- Read: changed files

- [ ] Run `go test ./internal/promptcenter ./internal/storage ./internal/context ./internal/chat ./internal/web ./internal/app`.
- [ ] Run `go test ./...`.
- [ ] Run `npm --prefix web run typecheck`.
- [ ] Run `npm --prefix web run build`.
- [ ] Dispatch a final SubAgent spec compliance review against the original spec.
- [ ] Fix any remaining spec gaps before closing the goal.
