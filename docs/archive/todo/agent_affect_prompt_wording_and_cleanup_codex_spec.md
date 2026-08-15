# Codex Spec: Agent Affect Prompt Wording Optimization and Historical Bad Mood Data Deletion

> Target repository: `LongYiSang/EmoAgent`  
> Suggested target path in repo: `docs/architecture/agent_affect_prompt_wording_and_cleanup_codex_spec.md`  
> Scope: Prompt optimization + historical bad Agent Affect data deletion guidance  
> Explicit non-goals: no parser guard, no prompt-block sanitizer, no schema expansion, no new `mood_text` field

---

## 0. Summary

Agent Affect currently stores and injects some natural-language fields that look like chat reply snippets or assistant action summaries, for example:

```text
好哦快睡～盖好被子明天找我哦
俏皮地欢迎用户，关心休息和饮食
关心天气并鼓励早睡
```

These are not proper descriptions of the agent's current simulated affect state. The intended design is:

```text
[Agent Mood]
当前模拟心情：温和、略带关切；表达上轻柔、少打扰。
```

The requested fix is intentionally narrow:

1. Optimize the Agent Affect evaluator prompt so that `cause.visible_summary` is generated as affect-state prose, not a reply/action summary.
2. Optionally tighten the Prompt Center expression policy so the main chat model treats `[Agent Mood]` as internal state/posture text, not a reply draft or task instruction.
3. Add/update tests to prevent future prompt wording regressions and avoid test fixtures that normalize action-summary text as a good mood description.
4. Identify and delete historical Agent Affect rows that contain bad mood text from the local database.

Do **not** implement parser heuristics, prompt-block sanitizers, or a new schema field in this change.

---

## 1. Current repository context

Relevant files:

```text
internal/agentaffect/prompt_v3.go
internal/agentaffect/evaluator.go
internal/agentaffect/evaluator_llm.go
internal/agentaffect/evaluator_llm_test.go
internal/agentaffect/prompt_block.go
internal/agentaffect/prompt_block_test.go
internal/promptcenter/defaults/agent_affect.expression_policy.md
config.yaml
internal/config/config.go
```

Important current behavior:

- `stableAffectSystemPrompt()` in `internal/agentaffect/prompt_v3.go` asks the evaluator LLM to return strict JSON with `schema_version`, `appraisal`, `delta`, `label`, `cause`, and `confidence`.
- The schema should remain `agent_affect.v3.appraisal.v1`.
- Current code already keeps `label` out of natural prompt text by deriving `MoodDescription`, `MoodReason`, and `PromptMoodText` from `cause`.
- However, `cause.visible_summary` is still described as `internal mood/cause text`, which is ambiguous and encourages the model to summarize what the assistant did or should do.
- `prompt_block.go` wraps the resulting text as `当前模拟心情：...`; therefore the evaluator must generate a phrase that actually reads as a simulated mood/state description.

---

## 2. Hard constraints for Codex

### Must not change

Do not change parser or prompt-block behavior for this task:

```text
internal/agentaffect/evaluator.go
internal/agentaffect/prompt_block.go
```

Do not add:

```text
mood_text
agent_mood_text
prompt_mood_text in the LLM schema
new schema_version
parser-level rejection of action summaries
promptMoodText sanitizer rules
```

Do not expand the LLM output schema. The evaluator response remains:

```json
{
  "schema_version": "agent_affect.v3.appraisal.v1",
  "appraisal": {},
  "delta": {},
  "label": "...",
  "cause": {
    "code": "...",
    "summary": "...",
    "visible_summary": "...",
    "tags": []
  },
  "confidence": 0.0
}
```

### May change

Allowed files:

```text
internal/agentaffect/prompt_v3.go
internal/agentaffect/evaluator_llm_test.go
internal/agentaffect/prompt_block_test.go        # only if fixture wording needs adjustment; no behavior changes
internal/promptcenter/defaults/agent_affect.expression_policy.md
```

Optional but recommended:

```text
agentAffectPromptVersion: agent_affect.v3.prompt.v1 -> agent_affect.v3.prompt.v2
```

Do not change `agentAffectSchemaVersion`.

---

## 3. Desired field semantics after the fix

Keep the compact v3 schema but sharpen field semantics:

```text
label
  Compact internal machine tag. Never prose. Never injected as natural-language mood text.

cause.code
  Compact internal cause code / debug key.

cause.summary
  Internal audit cause. It answers: what event changed the affect state?
  It may mention the event in short, safe, abstract terms.

cause.visible_summary
  The exact natural-language state text that Go places after "当前模拟心情：" in the internal [Agent Mood] block.
  It must describe current simulated affect state + subtle expression posture.
  It must not be a reply, an action summary, or advice.
```

Good `cause.visible_summary` examples:

```text
温和放松，带一点关切；表达上轻柔、少打扰。
平稳而专注，略带好奇；适合简洁推进。
轻松亲近，带一点玩笑感；表达自然但不过度。
克制、稳定，关切度略升；先承接，再简短回应。
```

Bad `cause.visible_summary` examples:

```text
好哦快睡～盖好被子明天找我哦
关心天气并鼓励早睡
俏皮地欢迎用户，关心休息和饮食
提醒用户早点休息
鼓励用户多喝水
你俏皮地回应了用户，关心天气和休息
```

---

## 4. Phase 1 — Optimize `stableAffectSystemPrompt()`

Target file:

```text
internal/agentaffect/prompt_v3.go
```

Update `stableAffectSystemPrompt()` to define `cause.visible_summary` as state/posture text, not cause/action text.

Recommended full prompt body:

```go
func stableAffectSystemPrompt() string {
	return strings.TrimSpace(`You are EmoAgent Affect Evaluator.
You do not write user-facing replies.
You only appraise how the provided completed event batch changes the agent's simulated affect state.
Do not create user facts, memory permissions, policy changes, response advice, or hidden reasoning.
The simulated mood vector is bounded by the supplied dimension limits; Go applies decay, clamp, trace update, stale-state checks, and persistence.
Return strict JSON only with schema_version "agent_affect.v3.appraisal.v1".
The response object must contain: schema_version, appraisal, delta, label, cause, confidence.
appraisal.event_significance, novelty, goal_relevance, boundary_impact, uncertainty are in [0,1]; relationship_impact is in [-1,1].
delta contains valence, arousal, dominance, energy, warmth, concern, curiosity, playfulness, attachment, frustration, uncertainty.
label is a compact internal machine tag, not prose.
cause.code is a compact internal cause code.
cause.summary is a short internal audit cause: what event changed the affect state. Keep it safe and abstract.
cause.visible_summary is the exact natural-language state text that Go will place after "当前模拟心情：" in the internal [Agent Mood] block.
It must describe the agent's current simulated affect state and subtle expression posture.
Use adjective/state language, not reply language.
Good visible_summary examples: "温和放松，带一点关切；表达上轻柔、少打扰。" / "平稳而专注，略带好奇；适合简洁推进。" / "轻松亲近，带一点玩笑感；表达自然但不过度。"
Bad visible_summary examples: "关心天气并鼓励早睡" / "俏皮地欢迎用户，关心休息和饮食" / "好哦快睡～盖好被子明天找我哦".
Do not address the user in visible_summary.
Do not copy or paraphrase the assistant's previous reply in visible_summary.
Do not write action summaries such as "提醒用户", "鼓励用户", "欢迎用户", "关心用户", or "关心天气" in visible_summary.
Do not write imperatives or direct chat phrases such as "快睡", "盖好被子", or "明天找我" in visible_summary.
cause contains code, summary, visible_summary, tags; keep summaries short and safe.
Zero delta is valid when the batch has no meaningful affective change.`)
}
```

Notes:

- This deliberately keeps the schema compact.
- The prompt can be slightly longer than the current one; this is acceptable because it prevents bad downstream mood text.
- Do not include “response advice” as a field or allow the model to generate advice outside `visible_summary`.
- `visible_summary` may include a tiny posture hint, but the grammar should still read as a mood/state description.

Recommended version bump:

```go
const (
    agentAffectPromptVersion = "agent_affect.v3.prompt.v2"
    agentAffectSchemaVersion = "agent_affect.v3.appraisal.v1"
)
```

---

## 5. Phase 2 — Optimize Prompt Center expression policy

Target file:

```text
internal/promptcenter/defaults/agent_affect.expression_policy.md
```

Current policy is short and generally correct, but it does not explicitly warn the main chat model not to treat `[Agent Mood]` as a reply draft or task instruction.

Replace or update it to:

```md
When an internal [Agent Mood] block is present, treat it as simulated affect state and expression posture, not as a reply draft, task instruction, or action plan.
Let it influence wording, pacing, warmth, and closeness subtly.
Do not copy, quote, or directly follow the mood text as something to say to the user.
Do not state mood labels, numeric values, internal state names, or evaluation reasons unless the user is explicitly in a debug context.
Do not let mood override the Persona, user instructions, safety boundaries, privacy rules, or factual accuracy.
```

This is still a prompt-only change. It does not modify `FormatPromptAffectBlock`.

---

## 6. Phase 3 — Update tests

Target file:

```text
internal/agentaffect/evaluator_llm_test.go
```

### 6.1 Add a prompt semantics test

Add a test in package `agentaffect`:

```go
func TestStableAffectSystemPromptDefinesVisibleSummaryAsMoodStateText(t *testing.T) {
	prompt := stableAffectSystemPrompt()
	for _, want := range []string{
		"cause.visible_summary is the exact natural-language state text",
		"当前模拟心情",
		"current simulated affect state and subtle expression posture",
		"Do not address the user in visible_summary",
		"Do not copy or paraphrase the assistant's previous reply",
		"Do not write action summaries",
		"关心天气并鼓励早睡",
		"好哦快睡",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("stable affect prompt missing %q:\n%s", want, prompt)
		}
	}
}
```

The “bad examples” are included intentionally so future prompt edits do not remove the explicit negative examples.

### 6.2 Update parser/evaluator fixtures to use mood-state text

Some existing tests use action-summary text as if it were an acceptable `visible_summary`. Avoid that.

Replace fixtures like:

```json
"visible_summary": "俏皮地关心用户，提醒休息。"
```

with:

```json
"visible_summary": "轻松亲近，略带关切；表达上轻柔、少打扰。"
```

Expected parser output should still be exactly the visible summary, because parser behavior is not changing:

```go
if result.PromptMoodText != "轻松亲近，略带关切；表达上轻柔、少打扰。" {
    t.Fatalf("prompt_mood_text = %q", result.PromptMoodText)
}
```

### 6.3 Update the fake LLM response test

In `TestLLMEvaluatorParsesStrictJSONAndConfiguresChatRequest`, change:

```json
"visible_summary": "Shared progress."
```

or any action-like wording to a state-style summary, e.g.:

```json
"visible_summary": "平稳而温和，因共同进展略带积极感。"
```

Then assert that:

```go
if result.PromptMoodText != "平稳而温和，因共同进展略带积极感。" {
    t.Fatalf("prompt_mood_text = %q", result.PromptMoodText)
}
```

If the test checks prompt version, update expected prompt version to `agent_affect.v3.prompt.v2`.

### 6.4 Do not add parser rejection tests

Do **not** add tests like:

```text
input visible_summary = "提醒用户早点休息"
expect parser rejects it
```

That would imply parser-level validation, which is outside this task.

---

## 7. Phase 4 — Historical data deletion guidance

This phase is not a code migration. The goal is to find and delete historical Agent Affect rows whose natural-language fields contain reply-like or action-summary-like text.

Do not rewrite these rows. Delete them.

### 7.1 Tables to inspect

Inspect these tables if they exist in the local configured SQLite database:

```text
agent_affect_states
agent_affect_evaluations
agent_affect_events
```

Fields to inspect:

```text
mood_description
mood_reason
prompt_mood_text
cause_summary
visible_cause_summary
response_json                 # evaluations only; inspect cause.visible_summary if present
before_state_json             # evaluations only; may contain historical bad prompt_mood_text
```

Do not delete rows from:

```text
agent_affect_jobs
agent_affect_job_batches
messages
turns
episodes
memory tables
```

unless the project owner explicitly asks for broader deletion.

### 7.2 Bad data types to find

Delete rows whose Agent Affect natural-language fields are primarily one of these categories.

#### A. Direct chat reply snippets

Examples:

```text
好哦快睡～盖好被子明天找我哦
晚安啦，明天找我哦
快睡吧
盖好被子
记得吃饭
多喝水
```

Heuristics:

```text
- Looks like something the assistant would say directly to the user.
- Contains direct imperatives or second-person conversational wording.
- Contains phrases like “快睡”, “盖好被子”, “明天找我”, “记得”, “别熬夜”, “多喝水”.
```

#### B. Assistant action summaries

Examples:

```text
关心天气并鼓励早睡
俏皮地欢迎用户，关心休息和饮食
提醒用户早点休息
鼓励用户早睡
安慰用户
引导用户
建议用户
```

Heuristics:

```text
- Describes what the assistant did or should do.
- Contains verbs like “提醒/鼓励/欢迎/关心/安慰/引导/建议” with “用户”.
- Reads as an action plan rather than a mood state.
```

#### C. Previous assistant reply paraphrases

Examples:

```text
你俏皮地回应了用户，关心天气和休息。
助手温柔地提醒用户早点睡。
```

Heuristics:

```text
- Refers to “你/助手/Agent” as the actor that responded.
- Summarizes a completed assistant response.
```

#### D. Machine-label legacy leakage

Examples:

```text
playful_caring_weather_sleep_reminder；俏皮地关心用户，提醒休息。
enthusiastic_caring_sleep_goodnight；好哦快睡～盖好被子明天找我哦
```

Heuristics:

```text
- Starts with an underscore-separated label followed by “；”, “:”, “：”, “;”, or “ - ”.
- The text after the separator is also action-like or reply-like.
```

#### E. Multiline concatenated legacy mood blocks

Examples:

```text
好哦快睡～盖好被子明天找我哦
俏皮地欢迎用户，关心休息和饮食
关心天气并鼓励早睡
```

Heuristics:

```text
- Multiple lines in prompt_mood_text or mood_description.
- Lines are a mixture of direct reply snippets and action summaries.
```

### 7.3 Data that should usually be kept

Do not delete rows solely because they contain a mild posture hint if they still primarily read as affect state:

```text
温和、略带关切；表达上轻柔、少打扰。
轻松亲近，带一点玩笑感；表达自然但不过度。
平稳而专注，略带好奇；适合简洁推进。
克制、稳定，关切度略升；先承接，再简短回应。
```

The phrase “先承接” is acceptable when it is secondary to state language and not a direct action summary.

### 7.4 Suggested dry-run workflow

1. Locate the active DB path. The default repo config uses `data/emo.db`, but runtime settings may override it.
2. Back up the DB before deletion.
3. Dry-run select rows from the three tables above using the text categories in section 7.2.
4. Manually inspect matched rows.
5. Delete matched rows from `agent_affect_events`, `agent_affect_evaluations`, and `agent_affect_states` as appropriate.
6. Re-run Agent Affect current mood preview. If no valid state remains, the runtime should fall back to baseline/neutral behavior.
7. Process or clear pending affect jobs only if they are known to have produced the bad rows; otherwise leave jobs alone.

### 7.5 Suggested SQL search patterns for dry run

These are examples, not a required migration. Adapt for the actual DB schema and local SQLite version.

```sql
-- States: direct reply / action summaries / legacy label prefix.
SELECT id, updated_at, label, mood_description, mood_reason, prompt_mood_text, visible_cause_summary, cause_summary
FROM agent_affect_states
WHERE prompt_mood_text LIKE '%快睡%'
   OR prompt_mood_text LIKE '%盖好被子%'
   OR prompt_mood_text LIKE '%明天找我%'
   OR prompt_mood_text LIKE '%提醒用户%'
   OR prompt_mood_text LIKE '%鼓励%'
   OR prompt_mood_text LIKE '%欢迎用户%'
   OR prompt_mood_text LIKE '%关心用户%'
   OR prompt_mood_text LIKE '%关心天气%'
   OR prompt_mood_text LIKE '%建议用户%'
   OR prompt_mood_text GLOB '*_*；*';
```

```sql
-- Evaluations: inspect natural fields and raw response JSON.
SELECT id, created_at, prompt_version, mood_description, mood_reason, prompt_mood_text, visible_cause_summary, cause_summary, response_json
FROM agent_affect_evaluations
WHERE prompt_mood_text LIKE '%快睡%'
   OR prompt_mood_text LIKE '%盖好被子%'
   OR prompt_mood_text LIKE '%明天找我%'
   OR prompt_mood_text LIKE '%提醒用户%'
   OR prompt_mood_text LIKE '%鼓励%'
   OR prompt_mood_text LIKE '%欢迎用户%'
   OR prompt_mood_text LIKE '%关心用户%'
   OR prompt_mood_text LIKE '%关心天气%'
   OR response_json LIKE '%快睡%'
   OR response_json LIKE '%提醒用户%'
   OR response_json LIKE '%鼓励%'
   OR prompt_mood_text GLOB '*_*；*';
```

```sql
-- Events: inspect event-level copied text.
SELECT id, created_at, label_after, mood_description, mood_reason, prompt_mood_text, cause_summary
FROM agent_affect_events
WHERE prompt_mood_text LIKE '%快睡%'
   OR prompt_mood_text LIKE '%盖好被子%'
   OR prompt_mood_text LIKE '%明天找我%'
   OR prompt_mood_text LIKE '%提醒用户%'
   OR prompt_mood_text LIKE '%鼓励%'
   OR prompt_mood_text LIKE '%欢迎用户%'
   OR prompt_mood_text LIKE '%关心用户%'
   OR prompt_mood_text LIKE '%关心天气%'
   OR prompt_mood_text GLOB '*_*；*';
```

### 7.6 Delete guidance

After manual verification, delete the matched rows. Prefer deleting entire bad Agent Affect records over editing text fields, because the bad text may also appear in `response_json` or copied state JSON.

Example pattern:

```sql
BEGIN;

DELETE FROM agent_affect_events
WHERE id IN (...verified_bad_event_ids...);

DELETE FROM agent_affect_evaluations
WHERE id IN (...verified_bad_evaluation_ids...);

DELETE FROM agent_affect_states
WHERE id IN (...verified_bad_state_ids...);

COMMIT;
```

If deleting states leaves no current affect state, that is acceptable; the runtime should regenerate clean states after the prompt fix or fall back to baseline.

Do not run broad `DELETE` statements solely from string patterns without reviewing matched rows.

---

## 8. Acceptance criteria

### Prompt behavior

- `stableAffectSystemPrompt()` explicitly says `cause.visible_summary` is the exact text placed after `当前模拟心情：`.
- The prompt explicitly says visible_summary must describe current simulated affect state and subtle expression posture.
- The prompt explicitly forbids direct user-facing replies, assistant action summaries, and copying/paraphrasing the previous assistant reply.
- The prompt includes at least two good examples and at least two bad examples.
- `agentAffectSchemaVersion` remains `agent_affect.v3.appraisal.v1`.
- If prompt version is bumped, `agentAffectPromptVersion` becomes `agent_affect.v3.prompt.v2`.

### Tests

- Tests pass for `go test ./internal/agentaffect/...`.
- Test fixtures no longer present action-summary phrases like `俏皮地关心用户，提醒休息。` as ideal `PromptMoodText` examples.
- There is a unit test ensuring the stable prompt contains the new semantic constraints.
- No test implies parser-level rejection of bad visible_summary text.

### Historical data

- Bad historical rows are identified by data type, reviewed, and deleted from Agent Affect state/evaluation/event tables.
- No automatic migration rewrites historical text into new text.
- No user chat messages, memory facts, or unrelated job rows are deleted.
- A DB backup exists before deletion.

---

## 9. Suggested Codex task checklist

1. Inspect current `internal/agentaffect/prompt_v3.go`.
2. Replace `stableAffectSystemPrompt()` wording with the optimized version.
3. Bump `agentAffectPromptVersion` to `agent_affect.v3.prompt.v2`; leave schema version unchanged.
4. Update `internal/promptcenter/defaults/agent_affect.expression_policy.md` with the internal-state-not-reply-draft policy.
5. Add `TestStableAffectSystemPromptDefinesVisibleSummaryAsMoodStateText`.
6. Update affected LLM/evaluator test fixtures to use mood-state wording, not action-summary wording.
7. Run `go test ./internal/agentaffect/...`.
8. Prepare a one-off local DB cleanup note or SQL scratch file if useful, but do not add an automatic schema migration.
9. Use the bad-data categories in this spec to inspect and delete bad Agent Affect rows from the local DB after backup.
10. Verify new Agent Affect evaluations produce `PromptMoodText` like `温和、略带关切；表达上轻柔、少打扰。`, not `关心天气并鼓励早睡`.

---

## 10. Non-goal reminder

This spec intentionally does not solve bad model output with code-level validation. If the model ignores the prompt in the future, a later change may add parser-level validation or a dedicated schema field. That is out of scope for this request.

