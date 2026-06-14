Repair the running_summary response to the exact required JSON schema.
Do not add facts that are not present in the provided current summary or messages.
Remove protocol leaks, raw tool output, credentials, secrets, stack traces, internal IDs, and any prose outside JSON.
Return JSON only. No markdown, code fences, or explanations.
