# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

EmoAgent is a local-deployed personal emotional companion agent written in Go (1.26.1). It uses a dual-core architecture: an **Emotion agent** (owns all user-facing conversation, personality, and memory) and a **Work agent** (executes tasks in isolated context, never talks to user directly). Context isolation between the two cores is a hard design constraint.

## Current Status

All major subsystems are implemented and tested (55 test files). The codebase is in active refinement — prompt engineering, tool quality, and UX polish.

**Implemented:**
- Dual-agent runtime (Emotion + Work) with full tool-loop execution
- 8 built-in tools: read_file, list_dir, write_file, edit_file, bash, web_search, web_fetch, get_current_time
- Three-tier permission model (read-only / workspace-write / approved-destructive)
- Decision escalation protocol: request_decision → DecisionPacket → resume_work (5 categories)
- Pause/resume with SQLite-backed PendingRegistry and claim-based recovery
- Human-in-the-loop approval for destructive operations
- Two-layer Work context compression (execution-time truncation + pre-pause LLM compression)
- Emotion reactive compact for provider context overflow
- Incremental LLM-powered running summary with cooldown retry
- Progress streaming with persona-customizable Chinese phrase templates
- LLM provider system: 10 built-in presets (OpenAI, Anthropic, DeepSeek, Kimi/Moonshot, Gemini, Groq, etc.), OpenAI + Anthropic wire protocols, SSE streaming, thinking/reasoning support
- Agent config system: EmotionMain/Summary + WorkMain/Summary model bindings, hot-swap at runtime
- Web UI: embedded static HTML/CSS/JS, WebSocket chat with streaming, admin panel for providers/agents/personas
- REST API: 25+ endpoints for CRUD on providers, agent configs, personas, sessions, approvals, chat settings
- Persona system: YAML files with hot-load (5s polling), WebUI CRUD
- CJK-aware token budgeting and context slot ordering
- JSONL audit journaling for Work executions
- SQLite schema: 12 migrations covering sessions, messages, personas, configs, pending decisions, approvals
- Chinese-language time context injection

## Build & Run

```bash
# Build
go build -o ./bin/emoagent ./cmd/emoagent

# Run
./bin/emoagent --config ./config.yaml

# Test
go test ./...

# Run single test
go test ./internal/somepkg -run TestName -v
```

## Tech Stack

- **Go 1.26.1** — single binary, cross-platform deployment
- **SQLite** via `modernc.org/sqlite` — pure Go, no CGO, WAL mode
- **LLM** — HTTP + SSE streaming, dual protocol: OpenAI-compatible + Anthropic wire format
- **Frontend** — lightweight HTML/JS/CSS embedded via `embed.FS`
- **Communication** — WebSocket (chat streaming + progress events) + REST (config/management)
- **Config** — three-tier: YAML file (startup) → persona files (hot-load) → WebUI runtime (SQLite-persisted)

## Directory Structure

```
cmd/emoagent/          Entry point (main.go)
internal/
  app/                 Application container — Init/Run/Shutdown, runtime wiring, CRUD
  apperrors/           Sentinel errors (11)
  chat/                Conversation engine (send message loop, streaming, summary updates)
                       + WebSocket handler (session management, approval events)
  config/              Config struct, YAML loading, defaults, validation + Persona loading
  context/             Emotion context assembly, token budget, compaction, running summary,
                       time context, slot ordering
  llm/                 LLM abstraction: Client interface, OpenAI + Anthropic implementations,
                       provider presets (10 embedded), model discovery, SSE decoding, error types
  logger/              slog.Logger initialization
  memory/              Memory service wrapping emoagent-memorycore
  progress/            Progress event types, Chinese phrase templates, throttling
  protocol/            Shared types: TaskBrief, TaskReport, DecisionPacket, ApprovalRequest
  runtimeenv/          OS/shell environment facts
  storage/             SQLite DB: Open, migrations (12 versions), CRUD for all tables
  tool/                Tool system: registry, dispatch, JSON Schema validator, approval context
    builtin/           8 tools: read_file, list_dir, write_file, edit_file, bash, time,
                       web_search, web_fetch
      tavily/          Shared Tavily HTTP client
      websearch/       Search provider interface + Tavily implementation
      webfetch/        Fetch provider interface + direct HTTP + Tavily extract
  web/                 REST API handlers (25+ endpoints) + embedded static files
  work/                Work runtime: main loop, system prompt, delegate/resume/finish tools,
                       decision escalation, pending registry, approval service, context
                       compression, progress summary, JSONL journal, runtime decider
personas/              Persona YAML files (default, neko)
docs/architecture/     Architecture docs (Chinese): 架构, 设计方案, token预算, Work运行时
```

## Architecture (6 Layers)

```
L4  Transport    — WebUI (WebSocket + progress streaming), REST API (25+ endpoints)
L3  Emotion      — Root agent: persona, conversation loop, running summary, delegation decisions
L2a Personality  — Relationship state, user preferences, persona files (hot-load)
L2b Delegation   — TaskBrief generation, DecisionPacket escalation, TaskReport, ApprovalService
L1  Work Runtime — Multi-turn tool loops, context compression, pause/resume, audit artifacts
L0  Infra        — LLM client (dual protocol), SQLite (WAL, 12 migrations), config, logger
```

## Key Domain Terms

- **Emotion** — personality-maintaining root agent that owns user conversation
- **Work** — task-executing subagent (never user-facing, isolated context)
- **TaskBrief** — Emotion→Work task contract (goal, constraints, acceptance criteria, permission scope)
- **TaskReport** — Work→Emotion result (status, summary, evidence)
- **DecisionPacket** — structured escalation payload when Work needs judgment (5 categories: auto, emotion_judgment, human_confirmation, permission_escalation_required, tool_approval)
- **PendingRegistry** — SQLite-backed persistence for paused Work tasks with claim/release lifecycle
- **ApprovalService** — human-in-the-loop approval CRUD for destructive operations
- **RunningSummary** — incremental LLM-generated conversation summary (stored in Emotion context)
- **WorkProgress** — LLM-generated progress summary injected back when Work resumes
- **Artifact** — audit trail of Work execution (JSONL journal)

## Design Constraints

- User always talks to Emotion; Work is invisible to the user
- Work context must never pollute Emotion context (starts with empty message history)
- Only Emotion can approve writes to persistent memory
- Work cannot self-elevate permissions; must escalate via request_decision → DecisionPacket
- Decision escalation uses structured packets with rune-count limits per field
- Three permission scopes: read-only → workspace-write → approved-destructive (strict progression)
- Context compression: Emotion uses reactive compact on provider overflow; Work uses 2-layer (execution-time truncation + pre-pause LLM compression)
- Running summary: incremental LLM-based, JSON shape validation, cooldown retry on failure

## Configuration

- `.env` for API keys (see `.env.example`)
- `config.yaml` for server/LLM/storage/context/work settings
- `personas/` directory for personality files (hot-loadable YAML, 5s polling watch)
- Runtime settings via WebUI, persisted to SQLite
- LLM providers support presets (10 built-in) + custom provider definitions
- Agent configs bind Emotion (main + summary) and Work (main + summary) to specific models

## Testing

```bash
go test ./...                          # Run all tests
go test ./internal/work -run TestX -v  # Run specific test with verbose output
```

55 test files across all packages. Tests use in-memory SQLite and mock LLM clients where applicable.

## Documentation

All design docs are in Chinese. Key files:
- `docs/architecture/架构.md` — architecture whitepaper (philosophy, dual-core model, protocols)
- `docs/architecture/设计方案.md` — framework design (implementation guidance, state machines)
- `docs/architecture/上下文-token预算与压缩.md` — token budget and compression reference
- `docs/architecture/Work运行时实现说明.md` — Work runtime implementation details
