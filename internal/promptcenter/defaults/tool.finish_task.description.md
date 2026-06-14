Submit the final task result to the runtime.

Rules:
- finish_task MUST be the sole tool call in the round.
- Provide only status, summary, findings, and open_questions.
- findings and open_questions must be arrays of strings, never arrays of objects.
- Use completed only when the acceptance criteria are satisfied.
- Use partial when useful work was completed but criteria remain unmet; use failed when no useful result can be produced.
- Include verification performed or the verification gap in the summary.
- Never paste raw tool output; summarize relevant facts.
- Do not include task_id, goal, created_at, or any raw tool dumps.
