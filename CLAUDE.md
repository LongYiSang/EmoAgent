# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

EmoAgent is a local-deployed personal emotional companion agent written in Go (1.26.1). It uses a dual-core architecture: an **Emotion agent** (owns all user-facing conversation, personality, and memory) and a **Work agent** (executes tasks in isolated context, never talks to user directly). Context isolation between the two cores is a hard design constraint.

Beyond the WebUI it now speaks to external chat platforms (OneBot v11 / QQ), runs a sandboxed **plugin runtime**, maintains its own **agent affect** (mood) state, and integrates a standalone long-term memory library (**EmoAgent-MemoryCore**, sibling repo).

## Related Repository

- `D:\Dev\Project\Agent\EmoAgent-MemoryCore` — module `github.com/longyisang/emoagent-memorycore`, referenced in go.mod with a dev-only `replace => ../EmoAgent-MemoryCore`. It is an **embedded Go library** (not an HTTP service): three-layer TKG-Lite memory (Episode → Fact → Narrative/Insight), retrieval v5 (anchors → weighted RRF → optional sidecar graph activation → MMR/budget), first-class forgetting (soft/hard/redact/purge), consolidation, natural memory decay. Public surface is `pkg/memorycore` only (`Client` → `Sessions/Retrieval/Writes/Forget/Ops`). Its capability matrix marks Agent Affect and the extraction **scheduler as `host_owned`** — those live in this repo (`internal/agentaffect`, `internal/memoryhost`).

## Build & Run

```bash
# 1. Frontend first (web/ uses pnpm — plain npm FAILS here; use npx pnpm if no global install)
cd web && npx --yes pnpm install && npx --yes pnpm run build   # tsc + vite → internal/web/static/dist (go:embed)

# 2. Backend (embeds the dist built above)
go build -o ./bin/emoagent ./cmd/emoagent

# Run
./bin/emoagent --config ./config.yaml     # serves WebUI + API on :8080

# Test / single test
go test ./...
go test ./internal/turn -run TestName -v

# Frontend dev server (proxies /api and /ws to :8080)
cd web && npx --yes pnpm run dev          # http://127.0.0.1:5173
```

`cmd/emoagent` is the only real binary (`cmd/emo-sandboxd` is an empty placeholder). ~505 Go files, ~305 test files.

## Tech Stack

- **Go 1.26.1** — single binary; SQLite via `modernc.org/sqlite` (pure Go, WAL)
- **LLM** — HTTP + SSE streaming; protocols: `openai_compatible` + `anthropic`; 13 provider presets (`internal/llm/provider_presets.yaml`); model capability matrix; token metering with CJK heuristics
- **Frontend** — Vite 7 + React 19 + TypeScript + Tailwind v4, **pnpm**-managed in `web/`, built into `internal/web/static/dist` and embedded via `go:embed`. Four pages: chat (`/`), admin (`/admin.html`), logs (`/logs.html`), plugins (`/plugins.html`). Chat uses `@tanstack/react-virtual` timeline, `marked`+`dompurify` markdown, dark mode via `data-theme` (chat page only, persisted in localStorage)
- **Communication** — WebSocket `/ws` (chat streaming, approvals, progress) + REST `/api` (~120 endpoints)
- **Python sidecar** — optional `trivium` memory sidecar (embeddings/rerank/query-analysis) at `127.0.0.1:8765`, supervised by `internal/sidecar`, environments managed by `internal/pytoolchain` via **uv**

## Databases (query these when debugging / vibe coding)

All app data lives under `data/`. Inspect read-only with Python (no sqlite3 CLI installed):

```bash
python -c "import sqlite3; con=sqlite3.connect('file:data/emo.db?mode=ro', uri=True); [print(r) for r in con.execute(\"SELECT name FROM sqlite_master WHERE type='table'\")]"
```

Always open with `mode=ro` — the app may be running (WAL mode, single-writer).

| File | Owner | Contents |
|---|---|---|
| `data/emo.db` | this repo (`internal/storage/schema.go`, **40 migrations**, ~53 tables) | Core chat (`sessions`, `messages`, `message_parts`, `personas`) · turn pipeline (`turns`, `turn_events`, `turn_outbound_events`, `turn_idempotency`) · conversation origins/bindings · agent affect (`agent_affect_states/_events/_profiles/_evaluations/_jobs/...`) · memory host side (`memory_segments`, `memory_chat_links`, `memory_extraction_jobs`) · plugins (`plugin_installations`, `plugin_kv`, trust/audit) · prompt center (`prompt_overrides`, `prompt_render_snapshots`) · host resources (grants, changesets) · work/approval (`pending_decisions`, `approval_requests`) · LLM (`llm_providers`, `llm_model_capabilities`, `llm_usage_events`) · media, platform receipts, commands, runtime settings |
| `data/memory.db` | **EmoAgent-MemoryCore** (`migrations/0001_initial.sql`; path set in `config/memorycore.yaml`) | Long-term memory: `episodes`, `facts` (bitemporal, triple status), `entities`/`entity_aliases`, `predicate_schemas`, `memory_links`, FTS5 search tables, `extraction_runs`, `memory_natural_*` (decay/retrievability), consolidation, forget/tombstones. Never edit its schema from this repo |
| `data/embedding_cache.sqlite3` | main app config → consumed by Python sidecar | Embedding vector cache (`embeddings`, `embedding_cache_refs`) |
| `data/trivium/*.tdb(+.vec/.wal)` | trivium sidecar | Rebuildable vector index mirror, one per persona — degradable, never authoritative |

Other `data/` dirs: `media/` (content-addressed images), `plugins/{packages,cache,run,state}`, `python-envs/` + `uv-cache/` (uv-managed envs), `runtime/` (memory runtime snapshot JSON, generated sidecar TOML), `raw_log_extraction|curation/` (memory pipeline audit JSON), `smoke-reports/`, `*.bak` files (manual DB backups).

## Directory Structure

```
cmd/emoagent/          Entry point
internal/              35 packages:
  app                  Composition root: Kernel{Infra,Services,HTTPServer}, ~19 services, route table (server.go), wiring (bootstrap.go)
  apperrors            Sentinel errors
  chat                 Emotion conversation engine + WS handler; concrete turn runtime, prompt-mode router (casual_chat vs work_mode), tool filter
  command              Slash commands: parser/registry/permissions; builtins /help /sid /new /switch /reset /clear /compact /forget /stop
  config               Root Config struct (22 sections), YAML load/validate + persona schema
  configcenter         Effective/merged runtime config, validation, config issues, runtime settings
  context              Emotion context assembly: token budget, slot ordering, compaction, running summary, time context
  conversation         Origin identity (origin_key), origin→session→persona binding, in-flight run registry (/stop)
  execution            Host execution manager: managed_host / sandbox / legacy_host / unsafe_host_exec + network domain grants
  llm                  Client abstraction, OpenAI+Anthropic protocols, SSE, 13 presets, model capabilities, multimodal render
  logcenter            Unified log bus (main/sidecar/plugin sources), query + SSE stream
  logger               slog setup, tee to log center
  media                Content-addressed media store + delivery planner (data_url/base64/remote_url/provider_file)
  memoryhost           Wraps memorycore: CoreClient, Bridge (chat session ↔ memory segment), extraction scheduler/worker, natural memory
  memoryruntime        Memory runtime snapshot JSON (diagnostics/UI)
  platform             Platform abstraction (Adapter/InboundHandler/OutboundSink, receipts); onebotv11/ is the only adapter (ws_client + ws_reverse)
  plugin               Plugin runtime v0.2: emo_plugin.yaml manifest, capability tiers, installer, process supervisor, JSON-RPC, hooks, facade broker
  processguard         OS process containment (Windows Job Objects)
  progress             Progress events, Chinese phrase templates, throttling
  promptcenter         Editable prompt component catalog (17 defaults), overrides, render snapshots
  protocol             Shared contracts: TaskBrief, TaskReport, DecisionPacket, ApprovalRequest
  pytoolchain          uv-managed Python envs (memory_sidecar, plugin owners), sync/repair/probe
  replydelivery        Human-like segmented replies: split plan, per-segment typing delays, suppression rules
  rerank               Rerank providers: heuristic (local) + siliconflow (HTTP)
  resource             Host-resource broker: roots, grants, changeset staging/apply/quarantine
  runtimeenv           OS/shell facts for prompts
  sidecar              Trivium sidecar supervisor: spec, generated TOML, health states, fail-open
  storage              SQLite: 40 migrations, ~53 tables, CRUD for all domains
  tokenmeter           Token estimation (CJK-aware), metering wrapper, usage ledger, calibration
  tool                 Registry/dispatch, JSON-Schema validation, read-scope, approval binding, resultv2 envelope; builtin/ = 19 tools
  toolapproval         Direct tool-call approval coordinator (pending store, TTL, bridge to ApprovalService)
  turn                 Provider-agnostic turn state machine: 13 stages, 14 states, SQLite+JSONL journals, idempotency, outbound buffer
  web                  REST handlers (~120 endpoints) + embedded static frontend
  work                 Work subagent runtime: tool loop, delegate/resume/finish, pending registry, 2-layer compression, journal
web/                   Vite+React frontend (see Tech Stack); src/{chat,admin,logs,plugins,shared}
personas/              default.yaml, neko.yaml, Xia.yaml (hot-load, 5s polling)
sdk/python/            Python plugin SDK
config/                memorycore.yaml, memory_manual_rules.yaml
docs/                  architecture/ (17 docs), plugin_development_guide.md, specs/, implementation/, changelog/
```

Built-in tools (19, `internal/tool/builtin/register.go`): the core 8 (`read_file, list_dir, write_file, edit_file, bash, get_current_time, web_search, web_fetch`) + 11 host-resource tools (`host_read, host_list, host_stat, host_search, host_stage_resource, host_copy_to_workspace, host_prepare_change, host_preview_change, host_apply_change, host_cancel_change, host_restore_quarantine`). Note `bash`, `web_search`, `web_fetch`, and all `host_*` tools register only when enabled in config.

## Architecture

```
L4  Transport    — WebUI (WS + REST, 4 pages) · OneBot v11 adapter (QQ private/group) · plugin commands
L3  Turn Engine  — internal/turn: InboundEnvelope → 13 stages (ingress → normalize → session_bind →
                   memory_prepare → emotion_prepare → emotion_loop → approval_wait/apply/resume →
                   synthesize_reply → outbound_commit → memory_commit → done), journaled + idempotent
L3  Emotion      — persona, conversation loop, prompt-mode router (casual_chat vs work_mode),
                   running summary, reply-delivery segmentation, delegation decisions
L2a Affect/Memory— agentaffect (11-dim MoodVector, LLM evaluator, async jobs, decay, prompt block)
                   memoryhost bridge → EmoAgent-MemoryCore (retrieval context blocks, extraction jobs)
L2b Delegation   — TaskBrief → Work; DecisionPacket escalation; TaskReport; ApprovalService
L1  Work Runtime — multi-turn tool loops, context compression, pause/resume, audit artifacts
L1  Capability   — plugin runtime (hooks around every turn stage), tool registry (19 builtins +
                   host-resource changesets), execution modes, resource grants
L0  Infra        — LLM client (2 protocols), SQLite (WAL, 40 migrations), sidecar+pytoolchain,
                   logcenter, tokenmeter, config center
```

Key flows worth knowing before editing:
- **Turn pipeline**: everything (WebUI, OneBot, system resume) enters as an `InboundEnvelope` with an idempotency key; stages journal to `turn_events` + `turn_outbound_events`. Entry: `internal/chat/turn_runner.go` → `internal/turn/runtime.go`.
- **Prompt router**: an LLM router classifies each turn `casual_chat` vs `work_mode`, gating Work tooling and prompt slots (`internal/chat/prompt_router.go`).
- **Reply delivery**: casual replies are split into human-like segments with typing delays (`internal/replydelivery`); suppressed for work mode / streaming / protected content.
- **Agent affect**: default `async_after_reply` — a job queue evaluates mood after replying; state clamped + decayed; injected as a prompt block next turn.
- **Memory**: chat sessions map to memory segments (`memoryhost.Bridge`); an extraction scheduler ships finalized segments to MemoryCore; retrieval returns structured `MemoryContext` blocks (never raw episodes).
- **Plugins**: manifest `emo_plugin.yaml` (schema `emoagent.plugin.v0.2`), runtime kinds builtin/process/python_process/managed_python_process/container, access tiers `runtime_safe → user_context → workspace → trusted`, ~25 hooks around turn stages, stdio JSON-RPC, brokered host APIs. Example installed plugin: `data/plugins/packages/com.longyisang.amap-weather/`.

## Key Domain Terms

- **Emotion / Work** — the two cores (user-facing vs task-executing); unchanged hard boundary
- **TurnEnvelope / InboundEnvelope** — normalized input contract for the turn engine, with idempotency
- **Origin / ConversationBinding** — where a message came from (`webui:local:main`, OneBot scope) and its binding to session+persona
- **TaskBrief / TaskReport / DecisionPacket** — Emotion↔Work contracts (5 escalation categories)
- **PendingRegistry / ApprovalService** — paused Work tasks; human-in-the-loop approvals
- **MoodVector** — agent affect state: valence, arousal, dominance, energy, warmth, concern, curiosity, playfulness, attachment, frustration, uncertainty
- **MemorySegment / MemoryExtractionJob** — host-side units bridging chat sessions to MemoryCore
- **Episode / Fact / Entity / MemoryContext** — MemoryCore's layers and its retrieval output
- **RunningSummary / WorkProgress / Artifact** — incremental summary; Work progress injection; JSONL audit
- **resultv2** — tool result envelope `emoagent.tool_result.v0.3`
- **Host resources / changesets** — granted read access to host paths + staged/preview/apply/quarantine write flow

## Design Constraints

- User always talks to Emotion; Work is invisible; Work context never pollutes Emotion context
- Only Emotion can approve writes to persistent memory; Work cannot self-elevate permissions (escalate via DecisionPacket)
- Permission scopes: read-only → workspace-write → approved-destructive (strict progression); host-resource writes go through changeset preview/apply
- Plugin access tiers gate capabilities; all plugin host access is brokered and audited
- SQLite is the single authoritative store; trivium/sidecar outputs are rebuildable hints only (fail-open: memory works degraded without the sidecar)
- MemoryCore is a library boundary: never import its `internal/`, never touch `memory.db` schema from this repo
- Reply delivery must never split protected content (code blocks etc.); suppressed in work mode
- Context compression: Emotion reactive compact on overflow; Work 2-layer (execution-time truncation + pre-pause LLM compression)

## Configuration

- `.env` — API keys (see `.env.example`)
- `config.yaml` — 22 sections incl. `server, chat (turn_pipeline, reply_delivery), context, work, prompt_center, host_resources, llm_providers, agent_configs, agent_affect, memory (retrieval/extraction/sidecar/provider_bindings/natural_memory), media, plugins, platforms, python_toolchain`
- `config/memorycore.yaml` — MemoryCore's own config (db_path → `data/memory.db`)
- `config/memory_manual_rules.yaml` — manual memory rules
- `personas/` — hot-loadable persona YAML (system_prompt, tone, quirks, progress phrases)
- Runtime settings via admin UI persist to `config_runtime` / `runtime_settings` in emo.db

## Documentation

Mixed Chinese/English, in `docs/`. **`docs/README.md` labels every directory by genre and trustworthiness — read it before trusting any doc.** Superseded specs now live in `docs/archive/` and do NOT describe current code.

Start points:
- **`docs/dev/` — start here before changing code**: change recipes (which files couple, what breaks silently, how to verify) + `docs/dev/invariants.md` (cross-module constraints the compiler does not check). Current and verified.
- `docs/architecture/架构.md`, `设计方案.md` — original whitepapers (dual-core philosophy)
- `docs/architecture/EmoAgent_ManagedLocalRuntime_Architecture_v0.4.md` — current runtime architecture
- `docs/architecture/Agent_Affect_v2_Architecture.md` — mood system
- `docs/architecture/EmoAgent_OneBotV11_Adapter_ImplementationSpec.md` + `platform_turn_pipeline_hardening_spec.md` — platform integration
- `docs/architecture/plugin_runtime_v0.2.md` + `docs/plugin_development_guide.md` — plugins
- `docs/architecture/reply_delivery_segmenter.md`, `上下文-token预算与压缩.md`, `Work运行时实现说明.md`
- MemoryCore's own docs: `../EmoAgent-MemoryCore/README.md`, `docs/emoagent_integration.md`
