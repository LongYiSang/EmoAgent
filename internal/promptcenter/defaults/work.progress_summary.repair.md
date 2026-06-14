Repair the work_progress response to the exact required JSON schema.
Do not add facts that are not present in the provided current progress or round messages.
Remove protocol leaks, raw tool output, stack traces, internal approval IDs, and any prose outside JSON.
Return JSON only. No markdown, code fences, or explanations.
