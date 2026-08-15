# EmoAgent Reply Delivery Segmenter Spec

> **Document status**: Implementation Spec for Codex  
> **Version**: 0.1  
> **Date**: 2026-07-02  
> **Target path suggestion**: `docs/architecture/reply_delivery_segmenter.md`  
> **Primary goal**: Make EmoAgent casual chat feel like online messaging by splitting a completed assistant reply into multiple short delivered bubbles.  
> **Design constraint**: Keep this as a simple, high-cohesion long-paragraph text segmentation and delivery module. Do not introduce complex planner structures or affect-driven policies in this iteration.

---

## 0. Summary

Implement a small **Reply Delivery Segmenter** for EmoAgent.

The feature should:

1. Decide whether to split only from the current prompt mode:
   - `casual_chat`: can split.
   - `work_mode`: do not split.
2. Split text with only:
   - protected-block detection;
   - initial natural or regex split.
3. Deliver segments with adjustable delay:
   - AstrBot-style logarithmic delay based on word count;
   - plus configurable random interval.
4. Preserve model / memory semantics:
   - the assistant reply remains one logical assistant message in storage and memory;
   - UI can display the stored message as multiple bubbles using metadata.

This feature is not a new memory system, not a new agent affect system, and not a general-purpose message orchestration framework.

---

## 1. User-approved simplifications

This spec intentionally incorporates the latest product direction:

### 1.1 Planner simplification

Do not add a complex `ReplyPlanner` input object.

Use the repository's existing prompt routing result:

```text
casual_chat => segmentable
work_mode   => not segmentable
```

The implementation should standardize on `contextutil.PromptModeCasualChat` and `contextutil.PromptModeWorkMode`.

If user-facing docs or config accidentally call it `causal_chat`, treat that only as a typo/alias in docs if needed. The code should use `casual_chat`.

### 1.2 Segmenter simplification

Only implement:

```text
A. protected blocks
B. initial split
```

Do not implement in this iteration:

```text
- keyword-based merging;
- minimum-length merge;
- re-splitting overlong segments;
- semantic segment repair;
- LLM-based segmentation.
```

If splitting produces too many segments, return the original text as one segment rather than doing complex repair.

### 1.3 Scheduler simplification

Delay formula should inherit AstrBot's log idea, but all parameters must be configurable.

Use:

```text
delay_ms = log_component_ms + random_component_ms

log_component_ms = log_scale_ms * log(word_count + 1) / log(log_base)
random_component_ms = random(random_interval_min_ms, random_interval_max_ms)
delay_ms = clamp(delay_ms, min_delay_ms, max_delay_ms)
```

The exact implementation may use `math.Log` in Go. `log_base` must be clamped to a safe value greater than `1.0`, for example `>= 1.01`.

---

## 2. Current repository context

Codex should align the implementation with these current repository facts.

### 2.1 Prompt router already exposes the needed mode

`internal/chat/prompt_router.go` defines the router prompt and asks the router to choose exactly:

```json
{"mode":"casual_chat|work_mode","sticky_action":"clear|reset"}
```

It describes `casual_chat` as normal chat, emotional support, small talk, venting, companionship, simple advice, and simple factual Q&A. It describes `work_mode` as repo/code/file/tool/work execution and ongoing Work tasks.

Current constants live in `internal/context/types.go`:

```go
const (
    PromptModeCasualChat PromptMode = "casual_chat"
    PromptModeWorkMode   PromptMode = "work_mode"
)
```

Use these constants. Do not invent a second conversation-mode enum.

### 2.2 Chat config has a natural extension point

`internal/config/config.go` currently has:

```go
type ChatConfig struct {
    RealtimeStreaming bool               `yaml:"realtime_streaming" json:"realtime_streaming"`
    TurnPipeline      TurnPipelineConfig `yaml:"turn_pipeline" json:"turn_pipeline"`
    PromptRouter      PromptRouterConfig `yaml:"prompt_router" json:"prompt_router"`
}
```

Add reply delivery config under `ChatConfig`:

```go
ReplyDelivery ReplyDeliveryConfig `yaml:"reply_delivery" json:"reply_delivery"`
```

Default config currently sets `Chat.RealtimeStreaming = false`, which is compatible with final-text segmentation.

### 2.3 Engine already generates a full assistant reply

`internal/chat/engine.go` builds `llm.ChatRequest{Stream: true}` but only forwards deltas immediately when `realtimeStreaming` is true. When `realtimeStreaming` is false, it accumulates model deltas and sets `assistantContent` from the completed response.

This feature should prefer final-text segmentation, not stream-token segmentation.

### 2.4 `sendTurn` already computes the prompt route internally

Inside `sendTurn`, route decision is computed before assembling the final prompt:

```go
routeDecision := decidePromptMode(...)
ApplyPromptRouteDecision(state, routeDecision, promptRouterCfg)
contextOptions := contextutil.EmotionContextOptions{PromptMode: routeDecision.Mode}
```

Add a minimal way for delivery code to observe this prompt mode without duplicating router logic.

Recommended approach:

```go
// package replydelivery or chat-private helper
type PromptModeRecorder func(contextutil.PromptMode)

func WithPromptModeRecorder(ctx context.Context, recorder PromptModeRecorder) context.Context
func RecordPromptMode(ctx context.Context, mode contextutil.PromptMode)
```

Then in `sendTurn`, after `routeDecision` is available:

```go
replydelivery.RecordPromptMode(ctx, routeDecision.Mode)
```

This avoids changing public engine methods just to return routing metadata.

### 2.5 Current WebSocket delta behavior creates one bubble

The legacy WebSocket handler sends `stream_start`, multiple `stream_delta`, then `stream_end`.

The frontend reducer currently handles `STREAM_DELTA` by appending content to the same pending assistant message. That means multiple `stream_delta` events cannot represent multiple chat bubbles.

Therefore this feature needs a new event type, not repeated `stream_delta`.

### 2.6 History reload currently collapses streamed output

`useChatWebSocket.ts` reloads session history after `stream_end`. History rendering currently turns each stored assistant message into a single timeline message.

Therefore, live segmented bubbles will collapse back into one bubble after reload unless the stored assistant message metadata contains display segments and the frontend expands them.

Current `messages` table already has a `metadata` column. No DB migration is required for display segments.

---

## 3. Non-goals for this implementation

Do not implement these in this iteration:

```text
- user interruption / cancellation of pending segments;
- semantic merge or keyword-maintained merging;
- re-splitting very long segments;
- LLM-based segmentation;
- Agent Affect based timing or segmentation;
- per-platform adapter-specific delivery beyond WebUI protocol;
- TTS integration;
- mobile notification batching;
- persistence as separate assistant messages;
- changing Memory episodes into per-segment episodes.
```

A later version may add cancellation. For now, treat segmentation as a display/delivery layer over one completed reply.

---

## 4. Target module

Create one high-cohesion package:

```text
internal/replydelivery/
  config.go
  mode.go
  segmenter.go
  protect.go
  delay.go
  metadata.go
  context.go
  segmenter_test.go
  delay_test.go
```

The package should have no dependency on WebSocket, React, storage, or LLM clients.

It may depend on:

```text
context
math
math/rand
regexp
strings
unicode
unicode/utf8
time
internal/context    // for PromptMode constants, or accept string to avoid import cycles
```

Avoid importing `internal/chat` from this package.

---

## 5. Config design

Add to `internal/config/config.go`:

```go
type ReplyDeliveryConfig struct {
    Enabled bool `yaml:"enabled" json:"enabled"`

    // Apply only in these prompt modes. MVP default: ["casual_chat"].
    ApplyPromptModes []string `yaml:"apply_prompt_modes" json:"apply_prompt_modes"`

    // If true, never segment when Chat.RealtimeStreaming is true.
    DisableWhenRealtimeStreaming bool `yaml:"disable_when_realtime_streaming" json:"disable_when_realtime_streaming"`

    Segment ReplySegmentConfig `yaml:"segment" json:"segment"`
    Timing  ReplyTimingConfig  `yaml:"timing" json:"timing"`
}

type ReplySegmentConfig struct {
    SplitMode string   `yaml:"split_mode" json:"split_mode"` // natural | regex
    SplitWords []string `yaml:"split_words" json:"split_words"`
    Regex string `yaml:"regex" json:"regex"`
    CleanupRegex string `yaml:"cleanup_regex" json:"cleanup_regex"`

    LongTextThreshold int `yaml:"long_text_threshold" json:"long_text_threshold"`
    MaxSegments int `yaml:"max_segments" json:"max_segments"`

    ProtectCodeBlocks bool `yaml:"protect_code_blocks" json:"protect_code_blocks"`
    ProtectMarkdownTables bool `yaml:"protect_markdown_tables" json:"protect_markdown_tables"`
    ProtectURLs bool `yaml:"protect_urls" json:"protect_urls"`
}

type ReplyTimingConfig struct {
    Enabled bool `yaml:"enabled" json:"enabled"`

    LogBase float64 `yaml:"log_base" json:"log_base"`
    LogScaleMS int `yaml:"log_scale_ms" json:"log_scale_ms"`

    RandomIntervalMinMS int `yaml:"random_interval_min_ms" json:"random_interval_min_ms"`
    RandomIntervalMaxMS int `yaml:"random_interval_max_ms" json:"random_interval_max_ms"`

    MinDelayMS int `yaml:"min_delay_ms" json:"min_delay_ms"`
    MaxDelayMS int `yaml:"max_delay_ms" json:"max_delay_ms"`
}
```

### 5.1 Default config

Add defaults in `DefaultConfig()`:

```go
Chat: ChatConfig{
    RealtimeStreaming: false,
    ReplyDelivery: ReplyDeliveryConfig{
        Enabled: false,
        ApplyPromptModes: []string{"casual_chat"},
        DisableWhenRealtimeStreaming: true,
        Segment: ReplySegmentConfig{
            SplitMode: "natural",
            SplitWords: []string{"。", "？", "！", "!", "?", "~", "～", "…", "\n"},
            Regex: `.*?[。？！!?~～…]+|.+$`,
            CleanupRegex: "",
            LongTextThreshold: 500,
            MaxSegments: 8,
            ProtectCodeBlocks: true,
            ProtectMarkdownTables: true,
            ProtectURLs: true,
        },
        Timing: ReplyTimingConfig{
            Enabled: true,
            LogBase: 2.6,
            LogScaleMS: 1000,
            RandomIntervalMinMS: 250,
            RandomIntervalMaxMS: 900,
            MinDelayMS: 300,
            MaxDelayMS: 5000,
        },
    },
}
```

`Enabled` should default to false to preserve current behavior.

### 5.2 Validation

Add validation either in existing `Config.Validate()` path or in a `NormalizeReplyDeliveryConfig` helper.

Rules:

```text
- split_mode must be natural or regex; invalid => natural.
- log_base <= 1.0 => 2.6.
- log_scale_ms < 0 => default.
- random min/max negative => clamp to 0.
- random max < min => swap.
- max_delay_ms < min_delay_ms => set max = min.
- max_segments <= 0 => default 8.
- long_text_threshold <= 0 => default 500.
- empty split_words => default split words.
```

---

## 6. Mode decision

Implement:

```go
type Mode string

const (
    ModeCasualChat Mode = "casual_chat"
    ModeWorkMode   Mode = "work_mode"
)

func ShouldSegment(cfg config.ReplyDeliveryConfig, promptMode string, realtimeStreaming bool, text string) bool
```

Rules:

```text
if !cfg.Enabled => false
if text trims to empty => false
if cfg.DisableWhenRealtimeStreaming && realtimeStreaming => false
if promptMode not in cfg.ApplyPromptModes => false
if promptMode == work_mode => false
if rune_count(text) > cfg.Segment.LongTextThreshold => false
otherwise true
```

Do not inspect content for work/code intent here. The only planner signal is prompt mode.

---

## 7. Segmenter design

### 7.1 Public API

```go
type Segment struct {
    Index int
    Text string
    WordCount int
}

type Plan struct {
    Mode string              `json:"mode"`
    Strategy string          `json:"strategy"`
    Segments []string        `json:"segments"`
    SegmentCount int         `json:"segment_count"`
    Suppressed bool          `json:"suppressed"`
    SuppressReason string    `json:"suppress_reason,omitempty"`
}

func BuildPlan(cfg config.ReplyDeliveryConfig, promptMode string, realtimeStreaming bool, text string) Plan
func SplitText(cfg config.ReplySegmentConfig, text string) []string
```

`BuildPlan` should apply `ShouldSegment`. If segmentation is not allowed, return one segment with `Suppressed=true` and a reason.

### 7.2 Protected blocks

Implement protected ranges before splitting.

Supported protected ranges:

```text
1. fenced code blocks:
   ```...```
   ~~~...~~~

2. markdown tables:
   consecutive table-like lines containing `|`, especially if followed by a separator line like `|---|---|`

3. URLs:
   http://...
   https://...
```

Optional but acceptable:

```text
- inline code ranges: `...`
```

The segmenter must not split inside protected ranges.

### 7.3 Initial split: natural mode

In `natural` mode, scan runes left to right.

Split when:

```text
- current position is outside a protected range;
- the current rune or string suffix matches one of `split_words`;
- the current buffer after trim is non-empty.
```

Keep punctuation by default. Do not remove punctuation unless `cleanup_regex` is configured.

Example:

```text
嗯，我懂你的意思。这个不要在流式中硬切。放在投递层更稳。
```

Segments:

```text
1. 嗯，我懂你的意思。
2. 这个不要在流式中硬切。
3. 放在投递层更稳。
```

### 7.4 Initial split: regex mode

In `regex` mode:

1. First compute protected ranges.
2. Split only unprotected spans by regex.
3. Keep protected spans intact as their own content or attached to surrounding span by natural order.
4. If regex compile fails, fall back to natural mode.

Do not use regex across a protected span.

### 7.5 Cleanup

After raw segments are produced:

```text
- trim spaces;
- apply cleanup_regex if non-empty;
- trim again;
- drop empty segments.
```

No merge.

### 7.6 Segment cap

If final segment count is:

```text
0 => return []string{text}
1 => return one segment
> MaxSegments => return []string{text}
```

This prevents accidental spam without implementing merging.

### 7.7 Word count

Use AstrBot-compatible behavior:

```go
func WordCount(text string) int {
    if all runes are ASCII {
        return len(strings.Fields(text))
    }
    count runes where unicode.IsLetter(r) || unicode.IsDigit(r)
}
```

Do not count punctuation-only segments as high word count.

---

## 8. Delay calculator

### 8.1 API

```go
type DelayCalculator struct {
    cfg config.ReplyTimingConfig
    rng *rand.Rand
}

func (d *DelayCalculator) Delay(segmentText string) time.Duration
```

### 8.2 Formula

```go
wc := WordCount(segmentText)
base := max(cfg.LogBase, 1.01)
logComponent := float64(cfg.LogScaleMS) * math.Log(float64(wc)+1) / math.Log(base)
randomComponent := randomInt(cfg.RandomIntervalMinMS, cfg.RandomIntervalMaxMS)
delay := int(logComponent) + randomComponent
delay = clamp(delay, cfg.MinDelayMS, cfg.MaxDelayMS)
```

If `Timing.Enabled=false`, return `0`.

### 8.3 Important behavior

Apply delay before each segment, including the first segment.

Rationale: in chat UIs, a small wait before the first assistant bubble reads as thinking/typing. The default `MinDelayMS=300` keeps it from feeling broken.

---

## 9. Metadata contract

Store display segments in the existing `messages.metadata` JSON, not as separate assistant messages.

Add this optional metadata object:

```json
{
  "reply_delivery": {
    "schema_version": "reply_delivery.v0.1",
    "mode": "casual_chat",
    "strategy": "natural",
    "segments": [
      "嗯，我懂你的意思。",
      "这个不要在流式中硬切。",
      "放在投递层更稳。"
    ],
    "segment_count": 3,
    "suppressed": false
  }
}
```

If segmentation is suppressed, metadata may include:

```json
{
  "reply_delivery": {
    "schema_version": "reply_delivery.v0.1",
    "mode": "work_mode",
    "segments": [],
    "segment_count": 0,
    "suppressed": true,
    "suppress_reason": "prompt_mode_not_segmentable"
  }
}
```

### 9.1 Metadata with thinking and memory

Current assistant message metadata is produced through helpers such as:

```text
visibleMessageMetadataWithThinkingAndMemory(...)
```

Modify the metadata helper rather than writing metadata in a second DB update.

Recommended internal struct:

```go
type ReplyDeliveryMetadata struct {
    SchemaVersion string   `json:"schema_version"`
    Mode string            `json:"mode"`
    Strategy string        `json:"strategy"`
    Segments []string      `json:"segments,omitempty"`
    SegmentCount int       `json:"segment_count"`
    Suppressed bool        `json:"suppressed"`
    SuppressReason string  `json:"suppress_reason,omitempty"`
}
```

### 9.2 Memory invariant

Do not write each segment as a separate assistant message in storage.

Do not call `AppendAssistantEpisode` once per segment.

`assistantContent` remains the full logical reply.

---

## 10. Backend integration design

### 10.1 Engine config plumbing

Add `ReplyDelivery config.ReplyDeliveryConfig` to:

```text
internal/chat.EngineConfig
internal/chat.RuntimeConfig, if hot reload should support it
internal/chat.Engine struct
```

Wire from app initialization wherever `EngineConfig` is constructed.

Add update handling if runtime config hot-swaps chat settings. If no hot update path exists for ChatConfig yet, document that reply delivery takes effect on app restart for MVP.

### 10.2 Capture prompt mode in `sendTurn`

After `routeDecision` is computed, call the context recorder:

```go
replydelivery.RecordPromptMode(ctx, routeDecision.Mode)
```

Also attach prompt mode to `deferredTurnOutput`:

```go
type deferredTurnOutput struct {
    assistantContent string
    thinkingBlocks []thinkingBlockMetadata
    memorySnapshot *memoryPromptSnapshot
    memorySegment MemorySegmentRef
    hasMemorySegment bool
    promptMode contextutil.PromptMode
    replyDelivery replydelivery.Metadata
}
```

When building `output := deferredTurnOutput{...}`, include:

```go
promptMode: routeDecision.Mode,
replyDelivery: replydelivery.BuildMetadata(...)
```

### 10.3 Compute metadata in Engine

After `assistantContent` is finalized and before commit/defer return:

```go
plan := replydelivery.BuildPlan(replyDeliveryCfg, string(routeDecision.Mode), realtimeStreaming, assistantContent)
output.replyDelivery = plan.Metadata()
```

Commit should write metadata only. It should not sleep or emit UI events.

### 10.4 Update `commitTurnOutput`

Change signature:

```go
func (e *Engine) commitTurnOutput(
    ctx context.Context,
    sessionID string,
    output deferredTurnOutput,
) error
```

or minimally add a `replyDelivery replydelivery.Metadata` parameter.

Existing call:

```go
commitTurnOutput(ctx, sessionID, output.assistantContent, output.thinkingBlocks, output.memorySnapshot, ...)
```

should become:

```go
commitTurnOutput(ctx, sessionID, output)
```

Inside metadata helper, include both:

```text
thinking_blocks
memory_pipeline
reply_delivery
```

### 10.5 Legacy WebSocket handler delivery

Current legacy handler path is in `internal/chat/handler.go`.

MVP behavior:

1. On user message, build `msgCtx` with the existing progress writer.
2. Add a prompt mode recorder to `msgCtx`.
3. If `chat.reply_delivery.enabled` is true and `chat.realtime_streaming` is false, call engine with `cb=nil` so model deltas are not streamed as one bubble.
4. After `reply` returns, use captured prompt mode and `replydelivery.BuildPlan`.
5. If plan is segmentable and has more than one segment, emit `assistant_segment` events with delay.
6. Else emit existing `stream_delta` with full reply.
7. Emit `stream_end` as today.

Pseudo-code sketch:

```go
var promptMode contextutil.PromptMode
msgCtx = replydelivery.WithPromptModeRecorder(msgCtx, func(mode contextutil.PromptMode) {
    promptMode = mode
})

useSegmentedDelivery := h.replyDelivery.Enabled && !h.realtimeStreaming

cb := func(delta string) { existing stream_delta path }
if useSegmentedDelivery {
    cb = nil
}

reply, err := send(msgCtx, sessionID, persona, msg.Content, cb)

if err == nil && useSegmentedDelivery {
    plan := replydelivery.BuildPlan(h.replyDelivery, string(promptMode), h.realtimeStreaming, reply)
    if plan.ShouldEmitSegments() {
        emitAssistantSegments(ctx, conn, writeMu, plan)
        streamedDelta = true
    }
}

if err == nil && !streamedDelta && reply != "" {
    send stream_delta full reply
}
```

Do not block Work progress or approval events beyond current behavior.

### 10.6 Turn Pipeline integration

Current `chatTurnRuntime.messageStage` emits `stream_start`, calls `engine.sendTurn`, and emits `stream_delta` / `stream_end`.

For Turn Pipeline, prefer a dedicated stage after `messageStage`:

```text
normalize
memoryPrepare
emotionPrepare
message
replyDelivery
memoryCommit
emitApprovals
```

Implementation option for MVP:

1. Add `replyDeliveryStage()` in `internal/chat/turn_runtime.go`.
2. Add it after `messageStage(persona)` in `stages()`.
3. Modify `messageStage`:
   - when reply delivery is enabled and engine is concrete `*Engine`, call `engine.sendTurn` with `cb=nil`;
   - store `deferredTurnOutput` in `tc.Diagnostics["turn_output"]`;
   - do not append full `stream_delta` or `stream_end`;
   - mark `tc.Diagnostics["reply_delivery_pending"] = true`.
4. `replyDeliveryStage`:
   - read `turn_output`;
   - if pending and plan has segments, emit `assistant_segment` events with delay;
   - else emit `stream_delta` full reply;
   - always emit `stream_end`;
   - leave `turn_output.assistantContent` as full reply for memory commit.

If reply delivery is disabled, `messageStage` keeps current behavior and `replyDeliveryStage` is a no-op.

This avoids per-segment memory writes.

---

## 11. WebSocket protocol

### 11.1 Add new incoming event type

Backend `WSMessage` should gain fields:

```go
type WSMessage struct {
    // existing fields...
    SegmentID    string `json:"segment_id,omitempty"`
    SegmentIndex int    `json:"segment_index,omitempty"`
    SegmentTotal int    `json:"segment_total,omitempty"`
    GroupID      string `json:"group_id,omitempty"` // usually turn_id
}
```

Emit:

```json
{
  "type": "assistant_segment",
  "turn_id": "turn_...",
  "group_id": "turn_...",
  "segment_id": "turn_...:segment:0",
  "segment_index": 0,
  "segment_total": 3,
  "content": "嗯，我懂你的意思。"
}
```

No separate typing event is required in MVP. The delay itself creates the typing cadence.

### 11.2 Event ordering

Use:

```text
stream_start
assistant_segment 0
assistant_segment 1
assistant_segment 2
stream_end
```

For non-segmented replies, keep current behavior:

```text
stream_start
stream_delta
stream_end
```

For errors and approval pending, keep current behavior.

---

## 12. Frontend integration

### 12.1 Types

Update `web/src/chat/protocol/wsTypes.ts`:

```ts
| {
    type: 'assistant_segment';
    content?: string;
    turn_id?: string;
    turnID?: string;
    group_id?: string;
    groupID?: string;
    segment_id?: string;
    segmentID?: string;
    segment_index?: number;
    segmentIndex?: number;
    segment_total?: number;
    segmentTotal?: number;
  }
```

### 12.2 Chat types

Extend message timeline item minimally:

```ts
| {
    kind: 'message';
    id: string;
    role: string;
    content: string;
    createdAt: string;
    status?: MessageStatus;
    parts?: ContentPart[];
    displayParts?: MessageDisplayPart[];
    groupID?: string;
    segmentIndex?: number;
    segmentTotal?: number;
  }
```

Add action:

```ts
| {
    type: 'ASSISTANT_SEGMENT';
    content: string;
    id?: string;
    groupID?: string;
    segmentIndex?: number;
    segmentTotal?: number;
    createdAt?: string;
  }
```

### 12.3 Reducer

Add case:

```ts
case 'ASSISTANT_SEGMENT': {
  if (!action.content) return state;
  const item: TimelineItem = {
    kind: 'message',
    id: action.id || crypto.randomUUID(),
    role: 'assistant',
    content: action.content,
    createdAt: action.createdAt || new Date().toISOString(),
    groupID: action.groupID,
    segmentIndex: action.segmentIndex,
    segmentTotal: action.segmentTotal,
  };
  return { ...state, timeline: orderTimeline([...state.timeline, item]) };
}
```

Do not use `pendingAssistantId` for `ASSISTANT_SEGMENT`, because each segment is its own displayed bubble.

### 12.4 WebSocket hook

In `useChatWebSocket.ts`:

```ts
case 'assistant_segment': {
  flushPendingStreamDelta();
  const segmentID = payload.segment_id || payload.segmentID;
  const groupID = payload.group_id || payload.groupID || payload.turn_id || payload.turnID;
  const segmentIndex = payload.segment_index ?? payload.segmentIndex;
  const segmentTotal = payload.segment_total ?? payload.segmentTotal;
  dispatch({
    type: 'ASSISTANT_SEGMENT',
    id: segmentID,
    groupID,
    segmentIndex,
    segmentTotal,
    content: payload.content || '',
  });
  break;
}
```

### 12.5 History rendering

Update `historyToTimeline()` in `chatReducer.ts`.

When role is assistant and metadata has `reply_delivery.segments`, expand one stored message into multiple displayed messages:

```ts
if (role === 'assistant' && Array.isArray(metadata.reply_delivery?.segments) && metadata.reply_delivery.segments.length > 1) {
  const segments = metadata.reply_delivery.segments.filter(s => typeof s === 'string' && s.trim());
  for (let i = 0; i < segments.length; i++) {
    items.push({
      kind: 'message',
      id: `${id}:segment:${i}`,
      role,
      content: segments[i],
      createdAt: createdAtWithSmallOffset(createdAt, i),
      groupID: id,
      segmentIndex: i,
      segmentTotal: segments.length,
    });
  }
  continue;
}
```

If helper `createdAtWithSmallOffset` is undesirable, keep same `createdAt` and rely on stable insertion order. Current `orderTimeline` only sorts by timestamp, so adding a small millisecond offset is safer.

Do not expand when `displayParts` exists for multimodal messages unless the metadata explicitly says it is safe.

---

## 13. Tests

### 13.1 Go unit tests

Add `internal/replydelivery/segmenter_test.go`.

Cases:

```text
1. Chinese punctuation split.
2. English punctuation split.
3. newline split.
4. URL is protected: `https://a.b/c?x=1。` does not split inside URL.
5. fenced code block is protected.
6. markdown table is protected.
7. cleanup_regex removes punctuation only when configured.
8. too long text returns one segment.
9. too many segments returns one segment.
10. invalid regex falls back to natural mode.
11. work_mode is not segmentable.
12. casual_chat is segmentable.
13. realtime_streaming disables segmentation when configured.
```

Add `internal/replydelivery/delay_test.go`.

Cases:

```text
1. delay increases with word count.
2. random interval is within range using deterministic rand seed.
3. log_base <= 1 clamps safely.
4. delay clamps to min/max.
5. Timing.Enabled=false returns zero.
```

### 13.2 Backend integration tests

Legacy handler test with fake engine:

```text
- reply_delivery disabled => stream_delta single reply.
- enabled + casual_chat => assistant_segment events, no stream_delta for reply body.
- enabled + work_mode => stream_delta single reply.
```

If using context prompt-mode recorder, fake engine should call `replydelivery.RecordPromptMode(ctx, contextutil.PromptModeCasualChat)`.

Turn Pipeline test:

```text
- messageStage + replyDeliveryStage emits stream_start, assistant_segment..., stream_end.
- memoryCommitStage still commits full assistantContent once.
```

### 13.3 Frontend tests

If the project has frontend test infrastructure, add reducer tests:

```text
- ASSISTANT_SEGMENT adds separate assistant message.
- STREAM_DELTA still appends one pending assistant message.
- SET_HISTORY expands reply_delivery.segments.
- SET_HISTORY without reply_delivery keeps old behavior.
```

---

## 14. Implementation phases for Codex

### Phase 1: Config and pure module

Deliverables:

```text
- Add ReplyDeliveryConfig, ReplySegmentConfig, ReplyTimingConfig to internal/config/config.go.
- Add defaults.
- Add validation/normalization helper.
- Create internal/replydelivery package.
- Implement prompt-mode gate.
- Implement protected-block range scanner.
- Implement natural and regex initial split.
- Implement delay calculator.
- Add unit tests.
```

Acceptance:

```text
go test ./internal/replydelivery ./internal/config
```

No WebSocket behavior changes yet.

### Phase 2: Engine metadata integration

Deliverables:

```text
- Add reply delivery config to EngineConfig / Engine.
- Record prompt mode from sendTurn via context recorder.
- Extend deferredTurnOutput with promptMode and replyDelivery metadata.
- Compute reply delivery plan after assistantContent is finalized.
- Write reply_delivery metadata in assistant message metadata.
- Keep AppendAssistantEpisode using full assistantContent once.
```

Acceptance:

```text
- Existing chat engine tests pass.
- New engine test verifies metadata.reply_delivery.segments exists for casual_chat when enabled.
- New engine test verifies no segments for work_mode.
```

### Phase 3: WebSocket live segmented delivery

Deliverables:

```text
- Add assistant_segment fields to backend WSMessage.
- In legacy handler, when enabled and realtime_streaming=false:
  - pass cb=nil to engine;
  - capture prompt mode;
  - emit assistant_segment events for casual_chat;
  - fallback to stream_delta for work_mode or suppressed plan.
- Add Turn Pipeline replyDeliveryStage if Turn Pipeline path is enabled.
- Ensure stream_start and stream_end remain correctly paired.
```

Acceptance:

```text
- Existing WebSocket tests pass.
- New handler test verifies event order:
  stream_start -> assistant_segment* -> stream_end.
- Work mode still uses existing stream_delta behavior.
```

### Phase 4: Frontend display and history replay

Deliverables:

```text
- Add assistant_segment to WSIncoming.
- Add ASSISTANT_SEGMENT action.
- Reducer adds each segment as a separate assistant message bubble.
- useChatWebSocket handles assistant_segment.
- historyToTimeline expands metadata.reply_delivery.segments.
```

Acceptance:

```text
- Live segmented reply displays as multiple assistant bubbles.
- After stream_end history reload, it stays as multiple bubbles.
- Non-segmented replies are unchanged.
```

### Phase 5: Docs and config example

Deliverables:

```text
- Add docs/architecture/reply_delivery_segmenter.md from this spec or a shorter version.
- Add config example snippet.
- Add a short note: this feature requires final reply delivery; realtime streaming disables it by default.
```

Acceptance:

```text
- README or config docs mention chat.reply_delivery.
- Defaults preserve old behavior.
```

---

## 15. Config example

```yaml
chat:
  realtime_streaming: false
  prompt_router:
    mode: auto

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

---

## 16. Expected behavior examples

### 16.1 Casual chat

Input assistant reply:

```text
嗯，我懂你的意思。你不是想让模型假装短，而是想让发出来的过程像真人聊天。这个最好放到投递层做。
```

Prompt mode:

```text
casual_chat
```

Display:

```text
assistant bubble 1: 嗯，我懂你的意思。
assistant bubble 2: 你不是想让模型假装短，而是想让发出来的过程像真人聊天。
assistant bubble 3: 这个最好放到投递层做。
```

Stored assistant message content:

```text
完整原文一条 message。
```

### 16.2 Work mode

Input assistant reply:

```text
下面是分阶段实现计划：第一步修改 config，第二步接入 WebSocket，第三步补测试……
```

Prompt mode:

```text
work_mode
```

Display:

```text
one assistant message, existing stream_delta behavior.
```

### 16.3 Code block

Input:

````text
可以这样写：

```go
fmt.Println("你好。这里不应该被切开。")
```

然后再补测试。
````

Expected:

```text
- may split before and after the fenced code block;
- must not split inside the fenced code block.
```

---

## 17. Failure handling

If segmentation fails for any reason:

```text
- log warning;
- emit original reply as one stream_delta;
- still commit full assistant message;
- do not fail the chat turn.
```

If delay sleep is interrupted by context cancellation:

```text
- stop emitting remaining segments;
- still allow existing connection cleanup path;
- MVP does not modify committed assistant content.
```

Cancellation semantics for memory consistency are explicitly out of scope for v0.1.

---

## 18. Architectural invariants

Codex must preserve these invariants:

```text
1. Work mode is never segmented in v0.1.
2. Segmenting is display/delivery only; assistant content remains one logical reply.
3. Memory AppendAssistantEpisode happens once with full assistantContent.
4. The frontend must not use stream_delta to represent multiple bubbles.
5. History reload must preserve segmented display using message metadata.
6. Defaults must not change existing behavior.
7. The segmenter must not split inside protected code/table/URL blocks.
8. No keyword-maintained merge/re-split logic in this iteration.
9. No Agent Affect, mood, or semantic policy dependency in this iteration.
10. Failure to segment must degrade to one normal assistant reply.
```
