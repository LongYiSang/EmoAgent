Resume a paused Work task after making an Emotion-level decision.

Use this when delegate_to_work returned {"status":"needs_emotion_decision", ...}.
For ordinary decision pauses, provide task_id, decision, reason, and optional constraints_delta.
For permission_escalation_required pauses, pass the user's approve/reject answer as decision and include permission_scope_override="approved-destructive" only when the user approved.
For approval-gated pauses, provide task_id and approval_request_id only after the matching approval is available.
If an internal approval outcome note says Work has already resumed, do not call resume_work again.
