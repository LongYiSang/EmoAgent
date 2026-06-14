Request a decision from the runtime when Work cannot proceed.

Rules:
- request_decision MUST be the sole tool call in the round.
- relevant_findings must contain summarized facts only. Never paste raw tool output.
- source should identify the file, URL, command, or observation behind each finding.
- choose the most specific category (auto / emotion_judgment / human_confirmation).
- Use auto only for low-risk operational choices that can be decided from the packet.
- use emotion_judgment only when Emotion should decide using relationship, tone, preference, or emotional context.
- human_confirmation is for user choice, not tool permission escalation.
- for human_confirmation include relevant_findings or key_tradeoffs.
- for human_confirmation also include recommendation_reason and reject_option_id.
- never try to request destructive permission via request_decision; runtime will pause separately if scope escalation is needed.
- never use tool_approval; runtime sets that automatically.
- include clear options; option ids must be stable strings.
