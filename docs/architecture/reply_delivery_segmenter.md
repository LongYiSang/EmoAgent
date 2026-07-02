# Reply Delivery Segmenter

## Summary

Reply Delivery Segmenter makes casual chat replies display like short messaging bubbles while preserving one logical assistant reply in storage and memory.

It is a delivery/display layer:

- `casual_chat` can be segmented.
- `work_mode` is never segmented in v0.1.
- `chat.realtime_streaming=true` disables segmentation by default.
- The stored assistant message content remains the full reply.
- Display segments are stored in `messages.metadata.reply_delivery`.

## Backend Flow

`internal/replydelivery` owns the pure logic:

- mode gate via prompt mode and realtime streaming;
- protected ranges for code blocks, markdown tables, and URLs;
- natural or regex initial split;
- segment cap fallback to the original full text;
- AstrBot-style logarithmic delay with optional random interval.

`internal/chat.Engine` records the prompt mode selected by the prompt router and writes optional `reply_delivery` metadata when `chat.reply_delivery.enabled=true`.

WebSocket delivery uses a dedicated `assistant_segment` event:

```text
stream_start
assistant_segment
assistant_segment
stream_end
```

Fallback replies keep the existing protocol:

```text
stream_start
stream_delta
stream_end
```

## Metadata

Stored assistant messages may contain:

```json
{
  "reply_delivery": {
    "schema_version": "reply_delivery.v0.1",
    "mode": "casual_chat",
    "strategy": "natural",
    "segments": ["第一句。", "第二句。"],
    "segment_count": 2,
    "suppressed": false
  }
}
```

Suppressed plans omit `segments` and include `suppress_reason`.

## Config Example

```yaml
chat:
  realtime_streaming: false
  reply_delivery:
    enabled: true
    apply_prompt_modes: ["casual_chat"]
    disable_when_realtime_streaming: true
    segment:
      split_mode: natural
      split_words: ["。", "？", "！", "!", "?", "~", "～", "…", "\n"]
      regex: ".*?[。？！!?~～…]+|.+$"
      cleanup_regex: ""
      long_text_threshold: 500
      max_segments: 8
      protect_code_blocks: true
      protect_markdown_tables: true
      protect_urls: true
    timing:
      enabled: true
      log_base: 2.6
      log_scale_ms: 1000
      random_interval_min_ms: 250
      random_interval_max_ms: 900
      min_delay_ms: 300
      max_delay_ms: 5000
```

Defaults keep `reply_delivery.enabled=false`.
