# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

EmoAgent is a local-deployed personal emotional companion agent written in Go. It uses a dual-core architecture: an **Emotion agent** (owns all user-facing conversation, personality, and memory) and a **Work agent** (executes tasks in isolated context, never talks to user directly). Context isolation between the two cores is a hard design constraint.

## Status

Architecture docs are complete (`docs/architecture/`). The minimal Work runtime (P6) is fully wired except for real-LLM smoke testing and a dedicated architecture doc.

**Completed (P6):**
- `internal/work/` — TaskBrief validation, system prompt builder, TaskReport parsing, JSONL journal, Runtime tool-loop, `delegate_to_work` Emotion-scope tool
- `internal/tool/builtin/read_file.go` — path-safe, UTF-8, 1 MiB-capped, scope=work
- `internal/config/` — `WorkConfig` struct with defaults
- `internal/app/app.go` — DB-first Work profile resolution, runtime registration (graceful degradation if profile missing)
- `internal/context/assembler.go` — Delegation Guideline appended to Emotion system prompt

**Hard design constraints that apply at all times:**
- Work context must never pollute Emotion context — Work starts with an empty message history
- `permission_scope` hard-locked to `read-only` in the minimal phase (double gate: JSON schema enum + Go validation)
- DecisionRequest/Response, streaming progress, and `workspace-write` are explicitly deferred

## Build & Run (Expected)

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

- **Go** — single binary, cross-platform deployment
- **SQLite** via `modernc.org/sqlite` — pure Go, no CGO
- **LLM** — HTTP + SSE streaming, Anthropic API format (OpenAI compatible)
- **Frontend** — lightweight HTML/JS embedded via `embed.FS`
- **Communication** — WebSocket (chat streaming) + REST (config/management)
- **Config** — three-tier: YAML file (startup) → persona files (hot-load) → WebUI runtime (SQLite-persisted)

## Architecture (5 Layers)

```
L4  Transport    — WebUI (WebSocket), REST API
L3  Emotion      — Root agent: personality, memory, delegation decisions
L2a Personality  — Relationship state, user preferences, persistent memory
L2b Delegation   — TaskBrief generation, DecisionRequest/Response, TaskReport
L1  Work Runtime — Multi-turn tool loops, verification, audit artifacts
L0  Infra        — LLM client, SQLite, VectorStore interface, config, logger
```

## Key Domain Terms

- **Emotion** — personality-maintaining root agent that owns user conversation
- **Work** — task-executing subagent (never user-facing)
- **TaskBrief** — Emotion→Work task contract (goal, constraints, permissions)
- **TaskReport** — Work→Emotion result (status, summary, evidence)
- **DecisionRequest/Response** — escalation protocol when Work needs Emotion's judgment
- **Artifact** — audit trail of Work execution (JSONL)

## Design Constraints

- User always talks to Emotion; Work is invisible to the user
- Work context must never pollute Emotion context (strict isolation)
- Only Emotion can approve writes to persistent memory
- Work cannot self-elevate permissions; must escalate via DecisionRequest
- DecisionResponse uses append-only deltas (never resend full context)
- Context compression triggers at ~40k tokens, preserving last 6 turns

## Configuration

- `.env` for API keys (see `.env.example`)
- `config.yaml` for server/LLM/storage settings
- `personas/` directory for personality files (hot-loadable YAML)
- Runtime settings via WebUI, persisted to SQLite

## Documentation

All design docs are in Chinese. Key files:
- `docs/architecture/架构.md` — architecture whitepaper (philosophy, dual-core model, protocols)
- `docs/architecture/设计方案.md` — framework design (implementation guidance, state machines)
