# EmoAgent Platform Turn Pipeline Hardening Spec for Codex

> **Document status**: Codex implementation spec / phased patch plan
> **Target path in repo**: `docs/architecture/platform_turn_pipeline_hardening_spec.md`
> **Repo**: `LongYiSang/EmoAgent`
> **Baseline reviewed**: current `main` observed at `a9a8dda52d5ed7107aec224881738f2c15e8d13f` plus the original OneBot Turn Pipeline commit `77e61900d443c606e9cf8be61cb772a3c293e8c9`
> **Primary scope**: OneBot / SnowLuma platform inbound text, media parts, Turn Pipeline execution, receipts, idempotency, approvals, and platform-side concurrency.
> **Non-goal**: full MemoryGraph v2 implementation, full Agent Affect implementation, or general multi-platform rewrite.

---

## 0. One-sentence goal

Make the OneBot / SnowLuma platform entry a first-class EmoAgent entry point by hardening the current Platform Gateway → platform-bound Agent runtime → Turn Pipeline path, without bypassing memory, agent affect, plugin hooks, approval workflow, receipt/idempotency, or privacy boundaries.

---

## 1. Current repository baseline

The current platform path is already substantially improved. Codex should treat the following as existing behavior, not as TODOs.

### 1.1 Latest platform-related commit sequence

Current `main` has these relevant commits in order:

```text
a9a8dda  feat(logs): 增加运行日志中心
3e2ae36  feat(platform): 支持 OneBot 私聊图片入站
f156e51  fix: 收敛会话切换展示与 OneBot 日志
08cae96  feat(platform): 支持 OneBot 分段发送和插件 casual 路由
77e6190  feat(platform): 让 OneBot 文本接入 Turn Pipeline
```

The original `77e6190` commit introduced the critical architectural shift: ordinary platform text no longer calls the active WebUI engine directly; it enters a shared `TurnRunner` using the platform-bound Agent runtime.

### 1.2 `ChatService.SendPlatformTurn`

Current `SendPlatformTurn`:

```text
- requires chat.turn_pipeline.enabled == true;
- requires chat.turn_pipeline.memory_stages == true;
- builds a new engine from the platform-bound ActiveAgentRuntime;
- disables reply delivery timing for platform turns;
- builds a platform inbound Turn envelope;
- executes the shared TurnRunner;
- returns final text, reply-delivery segments, and result type.
```

Important current files:

```text
internal/app/chat_service.go
  SendPlatformTurn
  EnsureTurnStores
  turnRunnerForEngine
  platformTurnSink
  platformTurnEnvelope
```

`newEngine` already wires Memory, AgentAffect, media store/resolver, prompt snapshots, AgentID, PersonaKey, tool registry, dispatcher, approvals, and runtime LLM clients. Do not regress this wiring.

### 1.3 Shared Turn stores

`ChatService.EnsureTurnStores()` centralizes Turn journal and idempotency stores and injects the journal into the plugin host when available. WebUI and platform turns should keep sharing this storage layer.

Current design intent:

```text
Different entry points may use different Agent runtimes,
but they should share Turn journal/idempotency infrastructure.
```

### 1.4 `PlatformGateway.HandleInbound`

Current gateway flow:

```text
1. Validate inbound message has text or parts.
2. Resolve platform origin.
3. Begin platform receipt if ExternalMessageID exists.
4. Return duplicate if receipt store says duplicate.
5. Resolve platform default Agent ID and persona key.
6. Ensure current conversation binding.
7. Reject commands mixed with media parts.
8. Handle platform commands before building Agent runtime.
9. Build platform-bound Agent runtime.
10. If runtime persona differs from binding persona, re-bind session.
11. Run platform text through ChatService.SendPlatformTurn.
12. Emit reply-delivery segments or final text as platform messages.
13. Complete receipt with Turn result type.
```

Important current files:

```text
internal/app/platform_gateway.go
internal/app/platform_gateway_test.go
internal/app/onebot_adapter_integration_test.go
```

### 1.5 OneBot mapper and media state

Current OneBot mapper:

```text
- accepts private messages when private routing is enabled;
- still ignores group messages before PlatformGateway;
- constructs composite ExternalMessageID;
- fills SourceType / AdapterInstanceID / PlatformID / ChannelType / ExternalConversationID / ExternalActorID;
- renders text and unsupported segment placeholders;
- later adapter layer can resolve inbound images into llm.ContentBlock parts.
```

Important current files:

```text
internal/platform/onebotv11/mapper.go
internal/platform/onebotv11/adapter.go
internal/platform/onebotv11/inbound_media.go
```

### 1.6 Current tested behavior to preserve

Existing tests already cover important happy paths:

```text
- OneBot private commands route through PlatformGateway.
- OneBot private ordinary text routes through PlatformGateway.
- Platform text uses bound Agent runtime, not active WebUI LLM.
- Platform text requires Turn Pipeline + memory_stages.
- Platform text persists user/assistant messages.
- Platform text writes Turn row with source=platform.
- Memory commit stage exists for platform turns.
- Platform reply-delivery segmentation sends multiple outbound messages.
- Platform image parts pass through to multimodal LLM request and persist message parts.
- Command with image parts is rejected.
- Duplicate external message ID is deduplicated.
```

Do not remove or weaken these tests.

---

## 2. Remaining gaps this spec addresses

### P0 gaps

```text
1. Same platform session can run multiple ordinary turns concurrently.
2. Turn outbound events other than text/segments are mostly dropped at platform boundary.
3. Approval-required turns are not user-actionable from OneBot/SnowLuma.
```

### P1 gaps

```text
4. Platform receipt dedupe treats any existing record as duplicate, including failed or stale processing records.
5. Platform Turn idempotency falls back to ephemeral UUID when ExternalMessageID and RequestID are empty.
6. Receipt audit does not clearly store final resolved Agent/persona/turn metadata.
```

### P2 gaps

```text
7. Default platform Agent selection falls back to first agent silently.
8. Group messages are still pre-gateway ignored; future group support needs a safe design, not a boolean flip.
9. Platform logs/observability can be improved using Turn/receipt correlation.
```

---

## 3. Architecture invariants Codex must preserve

### 3.1 Entry point boundaries

```text
OneBot Adapter:
  Transport + OneBot event mapping + media download + outbound OneBot action sink.
  It must not know about Memory, Agent runtime, TurnRunner internals, or approvals beyond platform events.

PlatformGateway:
  Origin/session/persona/agent resolution, receipt lifecycle, command short-circuit, run gating, platform event translation.
  It must not duplicate chat engine internals.

ChatService:
  Agent runtime → Engine construction, shared TurnStores, TurnRunner execution.
  It must not contain OneBot-specific logic.

TurnRuntime:
  Turn idempotency, journal, stages, memory prepare/commit, plugin hooks, approvals, outbound journaling.
  Platform code must not bypass it for ordinary text.
```

### 3.2 Memory and Agent Affect boundaries

The current architecture reserves Agent Affect as runtime state, not user memory. Codex must not introduce any platform patch that writes Agent affect into facts, user mood, relationship facts, narratives, or insights. Platform patches may pass through the existing `AgentAffect` runtime already wired into `chat.Engine`, but should not add new Agent Affect semantics.

Relevant invariant:

```text
Agent affect may bias expression and weak retrieval only;
it must not become user memory or bypass privacy/safety/forget filters.
```

### 3.3 Commands remain available when runtime config is broken

Platform commands must continue to run before building the platform Agent runtime. `/sid`, `/new`, `/reset`, `/clear`, `/stop`, and future `/approve` / `/reject` / `/approvals` should not fail merely because the default platform Agent model config is broken.

### 3.4 Turn Pipeline remains mandatory for platform ordinary text

Current platform ordinary text requires:

```text
chat.turn_pipeline.enabled = true
chat.turn_pipeline.memory_stages = true
```

This is intentional. Do not reintroduce a fallback to `engine.SendMessage` for ordinary platform text.

### 3.5 Busy is not failure

If a message cannot start because the same platform conversation already has a running turn, it should complete receipt as a non-LLM result such as `busy`, emit a user-visible busy message, and avoid marking the receipt as failed.

### 3.6 Deleted/hidden memory safety remains authority-side

Platform improvements must not introduce direct Trivium/Memory sidecar shortcuts. Any memory retrieval or memory-affect behavior remains governed by existing Memory/Turn/Engine paths.

---

## 4. Proposed phased implementation

Codex should implement this spec in phases. Each phase should compile, preserve existing tests, and add focused regression tests before moving to the next phase.

---

# Phase 0 — Baseline documentation and guard tests

## Goal

Add this spec to the repository and add regression tests that lock current behavior before changing control flow.

## Code/doc changes

1. Add this document at:

```text
docs/architecture/platform_turn_pipeline_hardening_spec.md
```

2. Ensure current tests still pass:

```bash
go test ./internal/app ./internal/chat ./internal/platform/onebotv11
```

3. Add or verify tests for current behavior if missing:

```text
TestPlatformGatewayUsesBoundAgentForText
TestPlatformGatewayNormalTextEmitsFinalMessage
TestPlatformGatewayNormalTextEmitsReplyDeliverySegments
TestPlatformGatewayAllowsImagePartsInput
TestPlatformGatewayRejectsCommandWithParts
TestOneBotAdapterRoutesPrivateTextThroughPlatformGateway
TestOneBotAdapterIgnoresGroupMessagesBeforeGateway
```

## Acceptance criteria

```text
- No behavior change except docs/tests.
- Existing platform Turn Pipeline happy paths remain green.
- Codex understands this spec as the patch plan for later phases.
```

---

# Phase 1 — P0 platform run gating for same origin/session

## Problem

WebSocket chat has an active run guard, but platform text only registers a cancellable `RunRef` and does not prevent two ordinary text turns from running concurrently in the same platform session.

This can corrupt conversational order:

```text
message A and message B arrive close together
→ both read similar history
→ both memory_prepare
→ both LLM calls run
→ both memory_commit
→ final persisted order and retrieved context become nondeterministic
```

## Desired behavior

For ordinary platform text, default policy should be:

```text
same origin + same session + platform_text already running:
  do not start a second LLM turn;
  emit a short busy message;
  complete receipt with result_type = "busy";
  return Handled=true.

different origin/session:
  may run independently.
```

Queue mode can be added later. Phase 1 should implement `busy` first because it is simple and safe.

## Suggested implementation

### 1. Add a platform run gate

Create a small run guard rather than overloading existing `RunRegistry` if `RunRegistry` does not already expose `TryStart` semantics.

Candidate file:

```text
internal/app/platform_turn_gate.go
```

Candidate API:

```go
type platformTurnGate struct {
    mu     sync.Mutex
    active map[string]platformTurnGateEntry
}

type platformTurnGateEntry struct {
    originKey string
    sessionID string
    kind      string
    cancel    context.CancelFunc
    startedAt time.Time
}

func (g *platformTurnGate) TryStart(key string, entry platformTurnGateEntry) (release func(), ok bool)
func (g *platformTurnGate) CancelByOriginSession(originKey, sessionID string) int
```

Key recommendation:

```text
platform_text:<origin_key>:<session_id>
```

Integrate into `PlatformGateway.sendText` before calling `ChatService.SendPlatformTurn`.

### 2. Keep `/stop` working

Current `/stop` uses `conversation.RunRegistry`. Do not remove that. The new gate should either:

```text
- register the same cancel function with RunRegistry, or
- be driven by the existing RunRegistry if it is extended with TryStart.
```

`/stop` should cancel the active platform turn and release the gate.

### 3. Busy response text

Add a small helper:

```go
func platformBusyMessage(cfg *config.Config) string
```

Default Chinese message:

```text
上一条消息还在处理中，请等我回复完，或发送 /stop 后再试。
```

Do not call LLM for the busy path.

### 4. Receipt semantics

If receipt has already begun and the message is rejected as busy:

```text
CompleteInbound(receipt.ID, sessionID, "busy")
```

Do not call `FailInbound`.

## Tests

Add tests in `internal/app/platform_gateway_test.go`:

```text
TestPlatformGatewayRejectsConcurrentTextForSameSessionAsBusy
  - first request blocks in fake LLM/server;
  - second request same origin/session enters while first running;
  - second emits exactly one busy message;
  - active LLM call count remains 1;
  - second receipt status=handled result_type=busy.

TestPlatformGatewayAllowsDifferentOriginsInParallel
  - two origins can run independently;
  - both call bound platform runtime.

TestPlatformStopCancelsPlatformRunAndReleasesGate
  - start blocking platform text;
  - run /stop;
  - gate releases;
  - next message can start.
```

## Acceptance criteria

```text
- No concurrent ordinary platform LLM turns for the same origin/session.
- Different origins are unaffected.
- /stop still cancels platform_text.
- Busy path is visible to the user and receipt is not failed.
```

---

# Phase 2 — P0 platform outbound event translator and approval UX

## Problem

Current `platformTurnSink` collects only:

```text
turn.EventStreamDelta
turn.EventAssistantSegment
```

It ignores other Turn events:

```text
turn_status
approval_required
approval_updated
tool_call_start / tool_call_end
work_progress / work_progress_end
error
```

For simple chat this is fine. For real EmoAgent functionality, especially tools/work approvals, OneBot/SnowLuma users need a visible and actionable approval path.

## Desired behavior

Add a platform-level translator that converts Turn outbound events into platform outbound events or internal result state.

Minimum required mapping:

| Turn event | Platform result |
|---|---|
| `stream_delta` | collect final text only, not necessarily immediate send |
| `assistant_segment` | collect segment as outbound message |
| `approval_required` | emit user-visible approval prompt |
| `approval_updated` | emit user-visible approval status update |
| `turn_status busy` | emit user-visible busy/status message; result_type=`busy` |
| `turn_status previous_failed` | emit retry/status message; result_type=`previous_failed` |
| `error` | emit platform error event; result_type=`error` |
| `tool_call_*` | record/log; optionally emit compact progress if config enabled |
| `work_progress*` | record/log; optionally emit compact progress if config enabled |

## Suggested implementation

### 1. Replace `platformTurnSink` with event collector + translator

Candidate types:

```go
type PlatformTurnResult struct {
    Text       string
    Segments   []string
    Events     []platform.OutboundEvent
    TurnID     string
    Status     string
    ErrorKind  string
    ResultType string
}

type platformTurnCollector struct {
    origin     conversation.Origin
    sessionID  string
    personaKey string
    replyTo    string
    text       strings.Builder
    segments   []string
    events     []platform.OutboundEvent
    status     string
    errorKind  string
    turnID     string
}
```

Keep text/segment behavior compatible:

```text
- If assistant segments exist, gateway emits segments.
- Else if final text exists, gateway emits final text.
```

But add translated events before or after final messages as appropriate.

### 2. Add `translateTurnOutboundToPlatform`

Candidate file:

```text
internal/app/platform_turn_translator.go
```

Candidate function:

```go
func translateTurnOutboundToPlatform(event turn.OutboundEvent, meta platformTurnMeta) []platform.OutboundEvent
```

Where `platformTurnMeta` includes origin, sessionID, personaKey, replyToExternalMessageID.

### 3. Approval prompt format

For `approval_required`, emit a normal platform message with concise content. Avoid dumping raw tool input. Use the existing `protocol.ApprovalRequest` safe fields.

Suggested message format:

```text
需要你的确认：<question 或 goal_summary>
风险等级：<risk_level>

可选操作：
/approve <request_id> <option_id>
/reject <request_id>
/approvals
```

If option IDs are long, list short labels plus IDs. Do not expose sensitive raw tool input unless existing approval object already provides safe preview fields.

### 4. Approval commands

Add platform commands:

```text
/approvals
/approve <request_id> [option_id]
/reject <request_id>
```

Implementation options:

#### Preferred path

Use Turn Pipeline approval stages for platform approval actions.

Add in `ChatService`:

```go
func (s *ChatService) SendPlatformApprovalTurn(
    ctx context.Context,
    runtime *ActiveAgentRuntime,
    sessionID string,
    persona *config.Persona,
    in platform.InboundMessage,
    approval turn.InboundApproval,
) (PlatformTurnResult, error)
```

Build envelope:

```go
turn.InboundEnvelope{
    Source:        turn.SourcePlatform,
    SourceEventID: stableApprovalSourceEventID(...),
    Kind:          turn.InboundApprovalAction,
    SessionID:     sessionID,
    PersonaKey:    runtime.PersonaKey,
    Approval: &turn.InboundApproval{
        RequestID: requestID,
        Action:    action,
        OptionID:  optionID,
    },
}
```

Then execute the same `TurnRunner`.

#### Acceptable fallback for Phase 2 if needed

If adding approval-specific Turn entry is too large, implement `/approvals` first and return a clear message that approval action from platform is not yet enabled. But the phase is not complete until `/approve` and `/reject` can continue the turn.

### 5. Command parser integration

Current `CommandService` handles built-ins. Codex can either:

```text
A. Add built-in commands to CommandService and call a PlatformApprovalHandler injected by PlatformGateway; or
B. Intercept /approve /reject /approvals in PlatformGateway before CommandService.TryHandle.
```

Preferred: use CommandService if it keeps audit/history consistent. Intercepting in PlatformGateway is acceptable if it is simpler and well-tested.

Important: approval commands must work before platform Agent runtime validation when they only list pending approvals. To continue after approval, they will need runtime/persona resolution.

## Tests

Add tests:

```text
TestPlatformTurnTranslatesApprovalRequiredToMessage
  - fake Turn/Engine generates approval_required;
  - platform sink receives actionable message containing request_id and /approve /reject guidance;
  - receipt result_type=approval_wait.

TestPlatformApproveCommandContinuesTurn
  - seed pending approval request;
  - send /approve request_id option_id through PlatformGateway;
  - Turn Pipeline approval action executes;
  - assistant continuation is emitted to platform;
  - approval status updated.

TestPlatformRejectCommandUpdatesApproval
  - same as approve but reject path.

TestPlatformTurnStatusBusyDoesNotEmitEmptyMessage
  - duplicate/running Turn status maps to a status/busy platform message.

TestPlatformToolProgressNotEmittedByDefault
  - tool_call events do not spam OneBot unless feature flag enabled.
```

## Acceptance criteria

```text
- Platform users can see why a turn is waiting for approval.
- Platform users can approve/reject pending approval requests from OneBot/SnowLuma.
- Non-text Turn events are not silently lost when user action is required.
- Existing simple text and segmented reply behavior remains unchanged.
```

---

# Phase 3 — P1 receipt retry, stale processing, and final metadata

## Problem

Current receipt logic treats any existing receipt for `source_type + adapter_instance_id + external_message_id` as duplicate, regardless of status.

That means:

```text
failed first attempt → platform retry is swallowed as duplicate
stale processing attempt → platform retry is swallowed as duplicate
handled attempt → duplicate behavior is correct
```

## Desired behavior

Receipt begin semantics should distinguish:

| Existing status | Desired behavior |
|---|---|
| `handled` | duplicate/noop |
| `processing`, fresh | duplicate/busy or running |
| `processing`, stale | allow retry/takeover |
| `failed` | allow retry by default |
| `ignored` | duplicate/noop |

## Suggested implementation

### 1. Extend receipt result

In `internal/platform` or app storage adapter, extend result shape:

```go
type ReceiptResult struct {
    ID             string
    Status         ReceiptStatus
    Duplicate      bool
    Retry          bool
    DuplicateKind  string // handled | running | ignored | previous_failed | stale_processing
    ExistingStatus string
}
```

If changing public interface is too invasive, add internal helper methods on storage side and adapt in `PlatformGateway`.

### 2. Storage behavior

In `internal/storage/platform_receipts.go`, change `BeginPlatformMessageReceipt` behavior:

```text
if no existing:
  insert processing

if existing.status == handled:
  return Duplicate=true, DuplicateKind=handled

if existing.status == processing and not stale:
  return Duplicate=true, DuplicateKind=running

if existing.status == processing and stale:
  update same receipt row to processing, clear error/result, increment attempt_count, return Retry=true

if existing.status == failed:
  update same receipt row to processing, clear error/result, increment attempt_count, return Retry=true

if existing.status == ignored:
  return Duplicate=true, DuplicateKind=ignored
```

### 3. Schema additions

If current `platform_message_receipts` lacks the following fields, add migration:

```sql
ALTER TABLE platform_message_receipts ADD COLUMN attempt_count INTEGER NOT NULL DEFAULT 1;
ALTER TABLE platform_message_receipts ADD COLUMN last_attempt_at TEXT;
ALTER TABLE platform_message_receipts ADD COLUMN turn_id TEXT NOT NULL DEFAULT '';
ALTER TABLE platform_message_receipts ADD COLUMN agent_id TEXT NOT NULL DEFAULT '';
ALTER TABLE platform_message_receipts ADD COLUMN resolved_persona_key TEXT NOT NULL DEFAULT '';
```

If SQLite migration system cannot safely add columns unconditionally, follow existing repo migration style and use helper-safe `ALTER TABLE` if available.

### 4. Final metadata

Update `CompleteInbound` to support final resolved metadata.

Preferred API:

```go
type PlatformReceiptCompletion struct {
    ReceiptID          string
    SessionID          string
    ResultType         string
    TurnID             string
    AgentID            string
    ResolvedPersonaKey string
}
```

If not feasible, add a separate method:

```go
CompletePlatformMessageReceiptWithMeta(ctx, completion)
```

Gateway should complete with:

```text
session_id = binding.SessionID
result_type = turnResult.ResultType
turn_id = turnResult.TurnID
agent_id = agentRuntime.ID
resolved_persona_key = personaKey
```

### 5. Stale threshold

Add default threshold in code:

```text
platform receipt processing stale after 5 minutes
```

Optional future config:

```yaml
platforms:
  common:
    receipts:
      retry_failed: true
      stale_processing_after_seconds: 300
```

Phase 3 can hard-code a private default if config plumbing is too large.

## Tests

Add storage tests:

```text
TestPlatformReceiptHandledDuplicateIsNoop
TestPlatformReceiptFailedCanRetry
TestPlatformReceiptStaleProcessingCanRetry
TestPlatformReceiptFreshProcessingReturnsRunningDuplicate
TestPlatformReceiptCompletionStoresTurnAgentPersonaMetadata
```

Add gateway tests:

```text
TestPlatformGatewayRetriesFailedReceiptAndCallsLLMAgain
TestPlatformGatewayDoesNotRetryFreshProcessingDuplicate
```

## Acceptance criteria

```text
- Failed platform messages can be retried.
- Stale processing receipts can be retried/taken over.
- Fresh processing duplicates do not start a second LLM turn.
- Handled duplicates remain no-op.
- Receipt rows record final turn_id, agent_id, and resolved persona.
```

---

# Phase 4 — P1 stable platform idempotency fallback

## Problem

`platformTurnEnvelope` currently sets `SourceEventID` from `ExternalMessageID`. If it is empty, it records `in.ID` in `RawMeta["inbound_id"]`, but `turn.BuildIdempotencyKey` does not use RawMeta. When both `SourceEventID` and `RequestID` are empty, Turn idempotency falls back to an ephemeral UUID key.

OneBot has composite `ExternalMessageID`, so private SnowLuma text is currently safe. But the platform abstraction is fragile for future adapters or synthetic inbound calls.

## Desired behavior

Platform inbound idempotency must be stable when any of the following exists:

```text
ExternalMessageID
InboundMessage.ID
RawEventHash
```

## Suggested implementation

Change `platformTurnEnvelope`:

```go
sourceEventID := strings.TrimSpace(in.ExternalMessageID)
if sourceEventID == "" {
    sourceEventID = strings.TrimSpace(in.ID)
}
if sourceEventID == "" {
    sourceEventID = strings.TrimSpace(in.RawEventHash)
}
```

Still keep raw meta:

```go
env.RawMeta["inbound_id"] = strings.TrimSpace(in.ID)
env.RawMeta["raw_event_hash"] = strings.TrimSpace(in.RawEventHash)
```

Do not include raw text in idempotency key unless there is no other stable ID and privacy review accepts it. For now, if all IDs are empty, allow existing ephemeral behavior.

## Tests

Add tests in `internal/app/chat_service_test.go` or platform gateway tests:

```text
TestPlatformTurnEnvelopeUsesExternalMessageIDForSourceEventID
TestPlatformTurnEnvelopeFallsBackToInboundID
TestPlatformTurnEnvelopeFallsBackToRawEventHash
TestPlatformTurnEnvelopeWithoutAnyIDRemainsEphemeral
```

Also test `IdempotencyKey` equality:

```text
two envelopes with same inbound ID produce same idempotency key
```

## Acceptance criteria

```text
- Platform adapter without ExternalMessageID can still get stable Turn idempotency if it provides ID or RawEventHash.
- Existing OneBot composite ExternalMessageID behavior unchanged.
```

---

# Phase 5 — P1 platform observability and Turn/receipt correlation

## Goal

Make platform turns diagnosable from logs and DB without digging across unrelated tables.

## Desired behavior

A platform message should be traceable through:

```text
platform_message_receipts.external_message_id
→ receipt.turn_id
→ turns.id
→ turn_events / turn_outbound_events
→ platform outbound action logs / log center
```

## Suggested implementation

### 1. Extend `PlatformTurnResult`

Ensure result contains:

```go
TurnID     string
Status     string
ErrorKind  string
ResultType string
```

Fill from `turn.TurnResult`.

### 2. Structured logs

In `PlatformGateway`, log at Info/Debug:

```text
platform inbound accepted: source, adapter, origin_key, external_message_id, session_id, agent_id, persona_key
platform turn started: receipt_id, turn_id, session_id, agent_id, persona_key
platform turn completed: receipt_id, turn_id, result_type, status, error_kind, outbound_count
platform turn busy: receipt_id, session_id, origin_key
platform turn approval_wait: receipt_id, turn_id, approval_count if available
```

Do not log raw user text beyond existing policy.

### 3. Tests

Testing logs may be optional. Prefer testing DB metadata and result fields.

## Acceptance criteria

```text
- Receipt row contains turn_id, agent_id, resolved persona.
- Turn row can be found from receipt metadata.
- No raw sensitive platform message text is introduced into logs beyond existing receipt text behavior.
```

---

# Phase 6 — P2 platform Agent selection and config guardrails

## Problem

`platformAgentID()` currently falls back to the first runtime/listed Agent if `platforms.common.default_agent_id` is empty. This is convenient in tests but risky in production: SnowLuma/OneBot is an external persistent channel and should not silently bind to the wrong Agent.

## Desired behavior

```text
production/platforms.enabled=true:
  require explicit platforms.common.default_agent_id

tests/dev fallback:
  may keep first-agent fallback only under test config or explicit allow_fallback flag
```

## Suggested implementation

### Option A: warning first

Phase 6 can start with a warning:

```text
if platforms.enabled && default_agent_id empty:
  logger.Warn("platform default agent is not configured; falling back to first agent")
```

### Option B: config validation

Add config validation:

```text
platforms.common.default_agent_id required when platforms.enabled=true and adapters exist
```

But this may break existing local setups. If implemented, provide a migration note and update sample config.

### Optional future config

```yaml
platforms:
  common:
    default_agent_id: "Chat"
    allow_default_agent_fallback: false
```

## Tests

```text
TestPlatformGatewayWarnsWhenDefaultAgentFallsBack
TestPlatformGatewayRequiresDefaultAgentWhenStrictConfigEnabled
TestPlatformGatewayDefaultAgentFallbackAllowedInTestMode
```

## Acceptance criteria

```text
- Production users are not silently routed to an arbitrary first Agent.
- Existing tests can still configure fallback intentionally.
```

---

# Phase 7 — P2 controlled group chat design guardrails

## Problem

OneBot group messages are currently ignored before the gateway. This is safe. Future group support must not simply remove that `return ignored` line.

## Desired behavior

Keep group messages ignored by default. Add a design and guarded implementation later.

Safe group support should require:

```text
group_enabled = true
AND accepted trigger condition:
  - bot mention, or
  - configured command prefix, or
  - group allowlist, or
  - explicit platform route rule
```

Group memory must be conservative:

```text
- Do not write every group message into personal long-term memory.
- Distinguish group topic memory from user/private relationship memory.
- Preserve actor role and actor ID.
- Commands use actor permission.
- Default ordinary group text should not enter MemoryGraph without explicit policy.
```

## Suggested implementation for this spec

Phase 7 should not implement full group memory. It may add docs/tests/config placeholders only.

If implementing group acceptance:

```text
1. Add Routing.GroupEnabled tests but keep default false.
2. Accept only @bot or command prefix.
3. For ordinary group text, route with origin_scope=group_user or group depending config.
4. Disable memory stages for group ordinary text unless explicit policy exists. If memory_stages is required globally, then do not accept ordinary group text yet.
5. Commands in group may be accepted separately from ordinary chat.
```

## Tests

```text
TestOneBotAdapterIgnoresGroupMessagesBeforeGatewayByDefault
TestOneBotGroupCommandCanBeAcceptedOnlyWhenGroupCommandsEnabled
TestOneBotGroupOrdinaryTextRequiresMentionAndAllowlist
TestGroupMessagesDoNotWritePrivateMemoryByDefault
```

## Acceptance criteria

```text
- Default group behavior remains ignored.
- Any group enablement is explicit, tested, and memory-safe.
```

---

# Phase 8 — Documentation, operations, and eval hooks

## Goal

Make the platform entry operationally understandable and future-proof.

## Documentation updates

Add or update:

```text
docs/architecture/platform_turn_pipeline_hardening_spec.md
docs/architecture/EmoAgent_OneBotV11_Adapter_ImplementationSpec.md, if present
README or config docs section for SnowLuma/OneBot platform setup
```

Include:

```text
- default_agent_id requirement;
- ordinary text Turn Pipeline requirement;
- /stop behavior;
- /approve /reject /approvals behavior;
- busy behavior;
- retry behavior;
- group messages ignored by default;
- media input limitations and image config;
- troubleshooting receipt/turn correlation.
```

## Eval hooks

Add tests or fixtures that align with memory eval principles:

```text
- behavior is tested at platform/receipt/turn level, not only final text;
- hidden/failed/stale/duplicate outcomes are explicitly asserted;
- Agent Affect remains a pass-through runtime dependency, not platform logic.
```

## Acceptance criteria

```text
- A user configuring SnowLuma can understand required Agent binding and Turn Pipeline settings.
- A developer debugging OneBot can trace external message ID → receipt → turn ID.
- Future Codex sessions can continue from this spec without rediscovering platform architecture.
```

---

## 5. Optional config shape for future phases

Do not implement all of this at once unless needed. This is the target shape for clarity.

```yaml
platforms:
  enabled: true
  common:
    default_agent_id: "Chat"
    default_persona: ""
    command_prefixes: ["/"]

    turn_concurrency:
      mode: busy              # busy | queue | parallel; default busy for private platform text
      busy_message: "上一条消息还在处理中，请等我回复完，或发送 /stop 后再试。"
      stale_after_seconds: 300

    receipts:
      retry_failed: true
      stale_processing_after_seconds: 300
      complete_busy_as_handled: true

    approvals:
      enabled: true
      list_command: "/approvals"
      approve_command: "/approve"
      reject_command: "/reject"

    events:
      emit_tool_progress: false
      emit_work_progress: false
      max_status_messages_per_turn: 3

    group:
      enabled: false
      accept_mentions: true
      accept_commands: true
      allowlist: []
      memory_policy: commands_only # commands_only | no_memory | explicit
```

If Codex adds config, it must also update:

```text
internal/config/config.go
internal/config/config_test.go
config.yaml
相关 UI/runtime settings if platform config is surfaced there
```

---

## 6. Suggested file map

Codex will likely need these files.

### Platform gateway and app wiring

```text
internal/app/platform_gateway.go
internal/app/chat_service.go
internal/app/command_service.go
internal/app/platform_gateway_test.go
internal/app/onebot_adapter_integration_test.go
```

### Turn runtime

```text
internal/chat/turn_runner.go
internal/chat/turn_runtime.go
internal/chat/handler.go
internal/turn/contract.go
internal/turn/idempotency.go
internal/turn/stream.go
```

### Platform and OneBot

```text
internal/platform/types.go
internal/platform/onebotv11/adapter.go
internal/platform/onebotv11/mapper.go
internal/platform/onebotv11/inbound_media.go
internal/platform/onebotv11/message.go
```

### Storage

```text
internal/storage/platform_receipts.go
internal/storage/schema.go
```

### Config and docs

```text
internal/config/config.go
internal/config/config_test.go
config.yaml
docs/architecture/
```

---

## 7. Detailed behavior matrix

| Scenario | Expected result |
|---|---|
| Private ordinary text, no active run | Run platform-bound Agent through Turn Pipeline. |
| Private ordinary text, active same-session platform turn | Do not call LLM; emit busy; receipt result_type=`busy`. |
| Private ordinary text, different origin | Can run independently. |
| Private `/sid` with broken Agent config | Command succeeds before Agent runtime build. |
| Private image ordinary input | Parts enter Turn Pipeline; model receives image when capability allows. |
| Private image + command | Reject with user-visible error; no command invocation. |
| Turn waits for approval | User sees actionable approval message; receipt result_type=`approval_wait`. |
| `/approve` valid request | Apply approval via Turn Pipeline; continue assistant response. |
| `/reject` valid request | Reject approval; user sees status/update. |
| Duplicate handled external ID | No second LLM call; no duplicate user-visible reply unless configured. |
| Duplicate failed external ID | Retry allowed; LLM may run again. |
| Duplicate fresh processing external ID | Do not run second LLM; return running/busy. |
| Duplicate stale processing external ID | Retry/takeover allowed. |
| ExternalMessageID empty but Inbound ID set | Stable Turn idempotency. |
| ExternalMessageID and Inbound ID empty but RawEventHash set | Stable Turn idempotency. |
| OneBot group ordinary message default | Ignored before gateway. |

---

## 8. Test naming checklist

Add tests incrementally. Suggested names:

```text
TestPlatformGatewayRejectsConcurrentTextForSameSessionAsBusy
TestPlatformGatewayAllowsDifferentOriginsInParallel
TestPlatformStopCancelsPlatformRunAndReleasesGate
TestPlatformTurnTranslatesApprovalRequiredToMessage
TestPlatformApproveCommandContinuesTurn
TestPlatformRejectCommandUpdatesApproval
TestPlatformTurnStatusBusyDoesNotEmitEmptyMessage
TestPlatformToolProgressNotEmittedByDefault
TestPlatformReceiptHandledDuplicateIsNoop
TestPlatformReceiptFailedCanRetry
TestPlatformReceiptStaleProcessingCanRetry
TestPlatformReceiptFreshProcessingReturnsRunningDuplicate
TestPlatformReceiptCompletionStoresTurnAgentPersonaMetadata
TestPlatformGatewayRetriesFailedReceiptAndCallsLLMAgain
TestPlatformTurnEnvelopeUsesExternalMessageIDForSourceEventID
TestPlatformTurnEnvelopeFallsBackToInboundID
TestPlatformTurnEnvelopeFallsBackToRawEventHash
TestPlatformTurnEnvelopeStableIdempotencyWithoutExternalMessageID
TestOneBotAdapterIgnoresGroupMessagesBeforeGatewayByDefault
```

---

## 9. Implementation notes and pitfalls

### 9.1 Do not double-send replies

If reply delivery is enabled, assistant segments are emitted as `assistant_segment`. If streaming is active, deltas may be emitted as `stream_delta`. Ensure platform sink does not send both duplicated segment and full final text.

Current behavior:

```text
- collector accumulates text from deltas or segments;
- gateway sends segments if any;
- otherwise sends final text.
```

Preserve this rule.

### 9.2 Do not mark busy as failed

Busy is a normal control-flow outcome. It should be visible and auditable, but not a failure.

Suggested receipt result types:

```text
message
command_result
approval_wait
no_output
busy
previous_failed
error
```

Only true exceptions should call `FailInbound`.

### 9.3 Be careful with receipt retry and Turn idempotency interaction

If receipt retry is allowed for failed attempts but Turn idempotency says the previous Turn failed, the platform turn may replay `previous_failed` rather than rerun, depending on idempotency key and store behavior.

For a true retry, Codex may need one of these strategies:

```text
A. keep same Turn idempotency and surface previous_failed; user must send new message to retry;
B. allow receipt retry to use a retry suffix in RequestID/IdempotencyKey;
C. clear or supersede idempotency entry when retrying failed receipts.
```

Preferred for Phase 3:

```text
- handled duplicates remain same idempotency;
- failed receipt retry should intentionally choose whether to rerun or return previous_failed;
- document the decision and test it.
```

Recommended decision:

```text
Failed receipt retry should rerun only when the previous Turn did not produce user-visible output.
If Turn status is previous_failed and no assistant output exists, append a stable retry attempt suffix to idempotency key.
If previous output exists, do not rerun automatically; send previous_failed/retry guidance.
```

This may require querying Turn journal/outbound events.

### 9.4 Approval prompts must be safe

Never dump raw tool input into QQ. Use existing `ApprovalRequest` safe fields:

```text
goal_summary
question
risk_level
options
recommended option if safe
```

### 9.5 Group memory must not be accidentally enabled

Do not let a group enablement patch accidentally make every group message part of private long-term relationship memory. Keep group default ignored until a separate memory policy exists.

### 9.6 Keep tests independent of real LLMs

Use fake LLMs or httptest OpenAI-compatible streaming servers like existing tests. Avoid network dependency.

---

## 10. Definition of done for the whole spec

The spec is complete when:

```text
- Same-session platform ordinary text cannot run concurrently by default.
- Platform approval_wait is visible and actionable from OneBot/SnowLuma.
- Failed/stale receipts can retry according to explicit tested semantics.
- Receipt rows can be correlated with Turn rows and resolved Agent/persona.
- Platform idempotency has stable fallback for non-OneBot adapters.
- Default group behavior remains safe.
- Existing OneBot text, command, segmented reply, and image input tests remain green.
- No patch violates Memory/Agent Affect privacy boundaries.
```

Recommended final validation:

```bash
go test ./internal/app ./internal/chat ./internal/platform/onebotv11 ./internal/storage ./internal/config
```

If time allows:

```bash
go test ./...
```

---

## 11. Minimal Codex prompt to use with this spec

```text
You are working in LongYiSang/EmoAgent.
Read docs/architecture/platform_turn_pipeline_hardening_spec.md.
Implement the next unchecked phase only.
Do not skip phases.
Preserve existing OneBot/Platform Turn Pipeline behavior.
Add focused regression tests for the phase.
Run the package tests listed in the phase.
Do not implement full group chat or full Agent Affect logic unless the phase explicitly says so.
```

---

## 12. Recommended phase order summary

```text
Phase 0: Add spec + lock current tests.
Phase 1: Add same-session platform run gate and busy result.
Phase 2: Add outbound translator and platform approval commands.
Phase 3: Add receipt retry/stale handling and metadata.
Phase 4: Add stable platform idempotency fallback.
Phase 5: Add turn/receipt observability and logs.
Phase 6: Add default Agent config guardrails.
Phase 7: Keep group default ignored; document/test controlled future group design.
Phase 8: Update docs and operational troubleshooting.
```
