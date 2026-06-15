# Web Search Pipeline

## Overview

`web_search` keeps the existing tool name and the legacy `{query, max_results}` call shape. The pipeline provider upgrades the internal path from single Tavily snippets to:

```text
web_search -> Tavily Search -> Tavily Extract Reader -> Rerank -> result assembly
```

The default pipeline uses Tavily `/search` for candidate discovery, Tavily `/extract` for top URL reading, and SiliconFlow rerank with `BAAI/bge-reranker-v2-m3`. Rerank falls back to the local heuristic provider when SiliconFlow is unavailable or fails.

## Compatibility

The old Tavily-only path remains available:

```yaml
websearch:
  enabled: true
  provider: tavily
  pipeline:
    enabled: false
```

`provider: tavily` always uses the legacy Tavily provider. `provider: pipeline` only enters the Search -> Reader -> Rerank path when `pipeline.enabled: true`; setting it to `false` rolls back to the Tavily provider.

`web_fetch` remains Work-only and keeps its existing scope. It is not opened to Emotion by this change.

## Tool Parameters

The public `web_search` schema accepts the legacy fields plus optional filters:

```json
{
  "query": "string",
  "max_results": 5,
  "profile": "auto|fast|deep|news|official_docs",
  "include_domains": ["example.com"],
  "exclude_domains": ["example.org"],
  "time_range": "day|week|month|year",
  "start_date": "YYYY-MM-DD",
  "end_date": "YYYY-MM-DD",
  "exact_match": false
}
```

These options are translated into Tavily search options where supported. Unknown or unsupported provider behavior should degrade by ignoring the option, not by failing the tool call.

## Result Contract

Legacy result fields are preserved:

```json
{
  "title": "Result title",
  "url": "https://example.com",
  "snippet": "Search snippet",
  "score": 0.82
}
```

Pipeline results can add:

```json
{
  "evidence": [{"text": "reader chunk", "source": "tavily_extract", "truncated": false}],
  "reasons": ["rerank: siliconflow"],
  "rerank_score": 0.91,
  "trust_score": 0.72,
  "final_score": 0.84,
  "needs_fetch": false,
  "fetch_hint": "",
  "warnings": []
}
```

`needs_fetch` is emitted as a boolean even when false. It becomes true when the reader is disabled, the reader fails, a result has no extracted evidence, or evidence was truncated. Work should use `web_fetch` only for the top 1-2 URLs when `needs_fetch` is true or when exact wording, tables, or code blocks are required.

The response-level `usage` object records stage counts such as `search_queries`, `extract_urls`, and `rerank_documents`.

## Configuration

Main pipeline example:

```yaml
websearch:
  enabled: true
  provider: pipeline
  api_key_env: TAVILY_API_KEY
  base_url: "https://api.tavily.com"
  max_results: 5
  timeout_sec: 30
  include_answer: false
  pipeline:
    enabled: true
    search:
      default_profile: auto
      default_depth: advanced
      fast_depth: basic
      max_subqueries: 1
      candidate_cap: 10
      per_query_max_results: 5
    reader:
      enabled: true
      top_n: 4
      extract_depth: basic
      format: markdown
      timeout_sec: 20
      max_chars_per_doc: 12000
      max_chunk_chars: 2000
    rerank:
      enabled: true
      provider: siliconflow
      base_url: "https://api.siliconflow.cn"
      path: "/v1/rerank"
      api_key_env: SILICONFLOW_API_KEY
      model: "BAAI/bge-reranker-v2-m3"
      fallback: heuristic
      timeout_sec: 10
      input_top_n: 8
      top_k: 5
      max_doc_chars: 4000
```

`config.yaml` is a seed/config file. Runtime overrides in `runtime_settings` or the config center can still determine the live effective config.

SiliconFlow Pro model IDs can be configured by replacing `model` when the provider confirms availability. The checked-in example uses `BAAI/bge-reranker-v2-m3`.

## Degradation

- Missing Tavily key: `web_search` is not registered.
- Search failure: the tool returns the provider error, matching the legacy search failure behavior.
- Reader disabled, `top_n: 0`, reader failure, or per-URL extract failure: keep snippet-only results, add warnings where appropriate, and set `needs_fetch` guidance.
- Rerank disabled: keep search/reader order and fill final/trust scores when possible.
- SiliconFlow key missing or remote rerank failure: use `fallback: heuristic` when configured; otherwise keep non-reranked results.

Rerank and Extract failures must not fail the whole `web_search` call.

## Privacy And Boundaries

Production logs must not include API keys, cleartext user query, raw extracted page text, result URLs, or raw provider error bodies. Pipeline completion logs record only provider, result count, warning count, stage usage counts, duration, and an error boolean.

Search results and Tavily Extract text are tool evidence. They are not automatically written into long-term memory, MemoryGraph, or MemoryCore. This pipeline does not use the MemoryCore sidecar; sidecar-backed rerank can be a future provider.

The implementation does not add browser automation, proxy pools, CAPTCHA handling, local reranker deployment, or crawler behavior.

## Rollback

Full rollback to legacy Tavily snippets:

```yaml
websearch:
  enabled: true
  provider: tavily
  pipeline:
    enabled: false
```

Disable only reader/rerank while keeping pipeline search planning:

```yaml
websearch:
  provider: pipeline
  pipeline:
    enabled: true
    reader:
      enabled: false
    rerank:
      enabled: false
      provider: disabled
      fallback: disabled
```

Disable only remote rerank and keep the local heuristic:

```yaml
websearch:
  provider: pipeline
  pipeline:
    enabled: true
    rerank:
      enabled: true
      provider: heuristic
```

## Smoke Test

Before live smoke, set test keys:

```powershell
$env:TAVILY_API_KEY = "<test tavily key>"
$env:SILICONFLOW_API_KEY = "<test siliconflow key>"
```

Start the app and ask a current, source-dependent question. Confirm:

- `web_search` is registered with provider `pipeline`.
- `websearch pipeline completed` appears in logs.
- `extract_urls > 0` when reader is enabled.
- `rerank_documents > 0` when rerank is enabled.
- Returned results contain `title/url/snippet/score`, plus `evidence`, `reasons`, `needs_fetch`, and `usage`.

Live smoke sends the query and selected candidate text to Tavily and SiliconFlow, consumes provider quota, and depends on the model choosing a structured `tool_use`.
