# Agent Affect v2 MVP Usage

Agent Affect v2 is disabled by default. Enable it in `config.yaml`:

```yaml
agent_affect:
  enabled: true
  storage_enabled: true
  evaluator:
    mode: llm # or disabled for baseline/no-change testing
```

Debug API examples:

```bash
curl "http://127.0.0.1:8080/api/agent-affect/current?persona_id=default&session_id=session-1&view=plugin_safe"

curl -X POST "http://127.0.0.1:8080/api/agent-affect/evaluate" \
  -H "Content-Type: application/json" \
  -d '{"persona_id":"default","session_id":"session-1","trigger":{"trigger_type":"debug"},"input":{"mode":"summary","summary":"preview only"}}'

curl -X POST "http://127.0.0.1:8080/api/agent-affect/submit" \
  -H "Content-Type: application/json" \
  -d '{"persona_id":"default","session_id":"session-1","trigger":{"trigger_type":"debug"},"input":{"mode":"summary","summary":"commit this"},"commit_mode":"commit_if_allowed"}'

curl -X POST "http://127.0.0.1:8080/api/agent-affect/delta" \
  -H "Content-Type: application/json" \
  -d '{"persona_id":"default","session_id":"session-1","trigger":{"trigger_type":"debug"},"delta":{"valence":0.2,"attachment":0.2}}'
```

The runtime stores only `agent_affect_*` rows in the main EmoAgent SQLite database. It does not write MemoryCore facts, narratives, insights, user mood, or relationship mood.
