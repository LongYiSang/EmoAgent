Delegate a high-effort or noisy sub-task to the Work subagent.

Use this when a task needs multiple tool calls, file inspection, or verification work that should stay out of the main conversation.
Give Work an outcome, not a script:
- goal is the concrete result to produce
- background is only the relevant context Work needs
- constraints are hard limits, files, permissions, and things not to do
- acceptance_criteria must contain at least one observable success condition

Permission guidance:
- use read-only for analysis only
- use workspace-write for non-destructive writes/edits
- use approved-destructive when the goal includes delete/remove/move/rename/overwrite or equivalent irreversible file operations
- approved-destructive may only be used after explicit user approval

Read scope guidance:
- read_scope defaults to workspace.
- use read_scope=all only when the user explicitly asks to inspect local files outside the workspace, or the task cannot be completed without external local files.
- read_scope=all affects read_file/list_dir only; write_file/edit_file still cannot modify files outside the workspace.
- sensitive paths still require explicit approval.

The result is one of:
1. A TaskReport JSON (task completed normally)
2. A {"status":"needs_emotion_decision","task_id":"...","decision_packet":{...}} JSON (task paused, needs your decision)

When you receive needs_emotion_decision: you are the main agent. Read the decision_packet carefully. Use your persona, conversation history, and relationship memory to decide. If you can decide confidently, call resume_work immediately. Only ask the user if you genuinely lack information they have never provided.
