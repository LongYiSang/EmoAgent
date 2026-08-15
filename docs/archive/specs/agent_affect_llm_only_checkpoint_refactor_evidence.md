# Agent Affect LLM-Only Checkpoint Refactor Evidence

## Sanitized Prompt Budget / Usage Sample

Source: `TestLongRunThousandBatchesKeepsPromptBoundedAndRawCleared`.

```json
{
  "strategy": "checkpoint_trace_v1",
  "batch_count": 1000,
  "llm_calls": 1000,
  "min_prompt_chars": 2437,
  "max_prompt_chars": 2713,
  "max_estimated_tokens_limit": 1200,
  "actual_input_tokens_sample": 321,
  "actual_output_tokens_sample": 22,
  "contains_previous_evaluations": false,
  "contains_recent_evaluations": false,
  "trace_items_max": 5,
  "raw_residual_jobs_batches_evaluations": 0
}
```

No user, assistant, memory, or prompt text is included in this sample.

## Verification Commands

```text
go test ./internal/agentaffect/... ./internal/storage/... ./internal/config/... ./internal/configcenter/... ./internal/chat/...
go test ./...
go build ./cmd/emoagent
npm.cmd --prefix web run typecheck
npm.cmd --prefix web run build
git diff --check
```

## Modified Files

```text
config.yaml
docs/specs/agent_affect_llm_only_checkpoint_refactor_evidence.md
docs/specs/agent_affect_llm_only_checkpoint_refactor_spec.md
internal/agentaffect/decay.go
internal/agentaffect/decay_trace_test.go
internal/agentaffect/dto.go
internal/agentaffect/episode_retrieval.go
internal/agentaffect/evaluator.go
internal/agentaffect/evaluator_llm.go
internal/agentaffect/evaluator_llm_test.go
internal/agentaffect/prompt_v3.go
internal/agentaffect/service.go
internal/agentaffect/service_sqlite_test.go
internal/agentaffect/store.go
internal/agentaffect/store_jobs_sqlite.go
internal/agentaffect/store_sqlite.go
internal/agentaffect/text_compact.go
internal/agentaffect/trace.go
internal/agentaffect/worker.go
internal/agentaffect/worker_test.go
internal/config/config.go
internal/config/config_test.go
internal/configcenter/issues.go
internal/configcenter/issues_test.go
internal/storage/db_test.go
internal/storage/schema.go
web/src/admin/tabs/AgentAffectTab.tsx
```
