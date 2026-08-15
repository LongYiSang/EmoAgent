# EmoAgent Direct Tool Approval Lifecycle — Codex Implementation Spec

> **Document status**: Implementation Spec for Codex
> **Target repository**: `LongYiSang/EmoAgent`
> **Target branch baseline**: current `main`
> **Primary problem**: Emotion direct tool calls can be blocked by `tool.Dispatcher`, but the block is not converted into a persistent pending approval request, so the turn does not reliably enter `approval_wait` and cannot resume the approved tool call.
> **Primary goal**: Add a source-agnostic direct tool approval lifecycle for Emotion tools, including plugin tools, without moving plugin policy into chat UI or weakening the dispatcher approval gate.

---

## 0. Executive summary

Implement a **Direct Tool Approval Lifecycle** for Emotion tool calls.

The existing architecture already has the right lower-level pieces:

```text
plugin manifest / process tool spec
        ↓
tool.Spec + ApprovalClassifier
        ↓
tool.Dispatcher.ClassifyCall / ExecuteClassified
        ↓
ApprovalBinding + ApprovalContext exact-call check
```

The missing part is a host-side orchestration bridge for direct Emotion calls:

```text
Emotion tool call classification says: ToolApprovalRequired
        ↓
Host creates protocol.ApprovalRequest
        ↓
Host persists the exact pending tool call input
        ↓
Turn enters approval_wait
        ↓
User approves / rejects
        ↓
Host consumes approval and resumes exactly the same tool call
        ↓
Emotion continues with an internal tool outcome note
```

This must remain **source-agnostic**. A third-party plugin invocation is one reason a tool may require approval, but the lifecycle is not a plugin subsystem. It is a tool invocation approval subsystem.

Recommended shape:

```text
internal/toolapproval/
  packet.go          # source-agnostic ToolApproval DecisionPacket / wording builders
  context.go         # ApprovalContext from ApprovalRequest binding
  direct_store.go    # pending_direct_tool_calls persistence
  coordinator.go     # create pending direct approval + claim/resolve helpers

internal/chat/engine.go
  classify before executing Emotion tools
  on ToolApprovalRequired: create direct approval, emit events, return errApprovalPending
  on ContinueAfterApproval: direct-tool branch before Work resume branch

internal/work/approval.go
  expose a generic ConsumeResolvedRequest wrapper for resolved approvals

internal/storage/schema.go
  migration for pending_direct_tool_calls + repair helper
```

MVP deliberately does **not** restore the provider-native tool transcript after approval. Instead, after approval it executes the exact approved call with `tool.WithApproval(...)`, snips the result, injects an internal runtime note, and lets Emotion answer naturally. This keeps the implementation small and avoids storing full LLM message snapshots.

---

## 1. Current repository facts Codex must preserve

### 1.1 Plugin registration is already source-agnostic

`internal/plugin/process_adapter.go` converts process plugin tool specs into `tool.Spec`. It derives scope, permission, source metadata, and an `ApprovalClassifier` from invocation policy.

Important invariants:

- Empty invocation policy is normalized to `ask`.
- `InvocationAuto` does not attach an approval classifier.
- `InvocationDeny` skips tool registration.
- Plugin read-only Emotion tools can be exposed to Emotion.
- Write-capable plugin tools are kept in Work scope.

Do not move approval decisions into plugin runtime code. Plugin runtime only declares policy and tool metadata; Host owns lifecycle.

### 1.2 Dispatcher is already the enforcement gate

`internal/tool/dispatch.go` is the correct place to enforce:

- schema validation;
- permission checks;
- `ApprovalClassifier` checks;
- exact approval binding matching.

`Dispatcher.ClassifyCall` can return `CallActionToolApprovalRequired`. `Dispatcher.ExecuteClassified` currently converts that to a `tool.Result` with `NeedsApproval=true` and does not execute the handler.

This is correct. Do not make dispatcher create approval requests or depend on session DB.

### 1.3 Approval binding already exists and must be reused

`internal/tool/approval_binding.go` builds a binding from:

```text
tool name
normalized input hash
path digest
input preview
changeset fields when relevant
```

`internal/tool/approval_context.go` carries active approval data in context.

The resumed direct tool call must be executed only with a matching `ApprovalContext`. This prevents an approval for one input from being reused for another input.

### 1.4 Work already has a complete approval lifecycle

Work runtime does this correctly:

```text
Classify tool calls
        ↓
if ToolApprovalRequired: build DecisionPacket
        ↓
routeDecision / PausedWork
        ↓
PendingRegistry.Put
        ↓
ApprovalService.CreateRequestFromDecision
        ↓
turn waits
        ↓
resume_work consumes approval and calls tool.WithApproval
```

Do not stuff Emotion direct tool calls into `pending_decisions` or `PausedWork`. Emotion direct tool approval is not a Work task pause.

### 1.5 Emotion direct tool loop currently misses the lifecycle bridge

`internal/chat/engine.go` currently executes direct Emotion tools with `dispatcher.Execute(...)`. If the result has `NeedsApproval`, it only emits a `tool_call_end` status of `approval_required`, then still converts the result into a tool message and feeds it back to the LLM. It only interrupts the turn if a new pending `approval_requests` row appears.

The fix must change this flow to:

```text
classify first
if any call requires approval:
    create ApprovalRequest + pending_direct_tool_call
    emit approval_required
    return errApprovalPending
else:
    execute classified calls
```

---

## 2. Design principles

### 2.1 Source-agnostic approval lifecycle

Do not implement `PluginApprovalManager`.

Implement `ToolApprovalCoordinator` semantics.

A tool may require approval because it is:

- a third-party plugin invocation;
- a destructive write;
- a sensitive read;
- a future remote tool;
- a future account/resource action.

The approval lifecycle should not care where the tool came from. Plugin metadata is only one input to `tool.Spec` and `ApprovalClassifier`.

### 2.2 Dispatcher remains pure

Dispatcher must not import:

```text
internal/work
internal/chat
internal/storage
database/sql
protocol approval service
WebSocket / turn runtime
```

Dispatcher continues to answer:

```text
Can this call execute now under this context and max permission?
```

It does not answer:

```text
How do we show approval UI?
How do we persist pending approvals?
How do we resume an interrupted turn?
```

### 2.3 ApprovalRequest remains the UI and API contract

Reuse existing `protocol.ApprovalRequest`.

The WebSocket / Turn Pipeline already knows how to emit:

```text
approval_required
approval_updated
approval_wait
```

Do not invent a plugin-specific approval event.

### 2.4 Separate Work pending from Emotion direct pending

Use:

```text
pending_decisions             # Work paused task state
pending_direct_tool_calls     # Emotion direct tool call state
approval_requests             # shared user-facing approval ledger
```

This prevents Work concepts (`TaskBrief`, `PausedWork`, resume snapshots) from leaking into direct Emotion tool calls.

### 2.5 Resume only the exact approved call

The pending direct tool store must persist the original `tool.Call` input. The approval request stores a binding and preview, but not the raw input. To execute after approval, the Host needs the exact original input.

Execution after approval must still go through `dispatcher.Execute` with `tool.WithApproval(...)`, not directly through registry handler.

### 2.6 MVP uses internal outcome note, not provider-native transcript replay

After approval:

```text
Host executes exact approved tool call
Host snips result
Host injects internal runtime note into sendTurn(extraSystem)
Emotion responds naturally
```

Do not store full LLM messages snapshot in v1.

A provider-native transcript resume can be a later enhancement if internal-note continuation proves insufficient.

---

## 3. Recommended implementation phases

### Phase 0 — Add failing tests first

Purpose: make the current gap explicit.

Add tests that fail on current main:

1. `TestEngineSendMessage_DirectToolApprovalCreatesPendingAndStopsBeforeToolMessage`
2. `TestEngineContinueAfterApproval_ExecutesPendingDirectToolOnce`
3. `TestEngineContinueAfterApproval_RejectedDirectToolDoesNotExecute`
4. `TestTurnRuntimeDirectToolApprovalWaitReplaysApprovalRequired`

The first test should prove the bug:

```text
LLM requests an Emotion-visible read-only tool whose ApprovalClassifier requires approval.
Dispatcher must not execute the handler.
Engine must create an approval_requests row.
Engine must create a pending_direct_tool_calls row.
Engine must emit approval_required.
Engine must return errApprovalPending.
Engine must not append an approval_required tool result to the next LLM round.
```

Expected current-main behavior before implementation:

```text
handler not executed: pass
approval_requests row: fail
turn approval_wait: fail
LLM receives tool error result: likely fail depending test hook
```

### Phase 1 — Add `internal/toolapproval` helpers and direct pending store

Implement new package:

```text
internal/toolapproval/
  packet.go
  context.go
  direct_store.go
  coordinator.go
```

#### 1.1 `packet.go`

Implement a source-agnostic builder:

```go
package toolapproval

func BuildDecisionPacket(taskID, goalSummary string, classification tool.CallClassification) protocol.DecisionPacket
```

Requirements:

- `Category = protocol.CatToolApproval`.
- Options: `allow` and `deny`.
- `RejectOptionID = "deny"`.
- `RecommendedOption = "allow"` unless future policy says otherwise.
- Use `tool.BuildApprovalBinding(call, "", kind)`.
- Fill `protocol.ToolApprovalBinding` with all supported binding fields.
- For plugin invocation approval, wording should be equivalent to current Work wording.
- If `classification.ApprovalKind == ""`, default to `tool.ApprovalKindDestructiveWrite`.

Suggested helper names:

```go
func BuildToolApprovalPacket(taskID, goalSummary string, classification tool.CallClassification) protocol.DecisionPacket
func ToolApprovalQuestion(classification tool.CallClassification) string
func ToolApprovalWhyBlocked(classification tool.CallClassification) string
func ToolApprovalRecommendation(goalSummary string) string
```

Phase 1 may duplicate existing Work wording. Phase 5 should refactor Work to reuse this package.

#### 1.2 `context.go`

Implement:

```go
func ApprovalContextFromRequest(req *protocol.ApprovalRequest) (tool.ApprovalContext, error)
```

For direct tool approvals, require a non-nil `ToolApprovalBinding`. Return an error if binding is missing or incomplete.

Populate:

```text
RequestID
ApprovalKind
AllowToolCall = true
AllowDestructive = true only for destructive_write
ToolName
NormalizedInputHash
PathDigest
ChangeSetID
PlanHash
ResourceID
CanonicalPathHash
BaselineHash
BaselineFileID
DeleteMode
```

Do not relax matching semantics.

#### 1.3 storage migration

Add migration after current latest version in `internal/storage/schema.go`.

At the current repository baseline, latest migration is version `34`; add version `35`.

Suggested SQL:

```sql
CREATE TABLE IF NOT EXISTS pending_direct_tool_calls (
    approval_request_id   TEXT PRIMARY KEY,
    session_id            TEXT NOT NULL,
    turn_id               TEXT NOT NULL DEFAULT '',
    task_id               TEXT NOT NULL,
    call_id               TEXT NOT NULL,
    tool_name             TEXT NOT NULL,
    input_json            TEXT NOT NULL,
    max_permission        TEXT NOT NULL DEFAULT 'read-only',
    provider              TEXT NOT NULL DEFAULT '',

    approval_kind         TEXT NOT NULL DEFAULT '',
    normalized_input_hash TEXT NOT NULL DEFAULT '',
    path_digest           TEXT NOT NULL DEFAULT '',
    input_preview         TEXT NOT NULL DEFAULT '',

    status                TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','claimed','consumed','rejected','expired','failed')),
    claim_id              TEXT NOT NULL DEFAULT '',
    claimed_at            TEXT,
    consumed_at           TEXT,
    created_at            TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at            TEXT NOT NULL,
    error_message         TEXT NOT NULL DEFAULT '',

    FOREIGN KEY(approval_request_id) REFERENCES approval_requests(id) ON DELETE CASCADE,
    UNIQUE(session_id, task_id)
);

CREATE INDEX IF NOT EXISTS idx_pending_direct_tool_calls_session_status
    ON pending_direct_tool_calls(session_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_pending_direct_tool_calls_task
    ON pending_direct_tool_calls(session_id, task_id);
CREATE INDEX IF NOT EXISTS idx_pending_direct_tool_calls_expires
    ON pending_direct_tool_calls(expires_at)
    WHERE status IN ('pending','claimed');
CREATE INDEX IF NOT EXISTS idx_pending_direct_tool_calls_tool
    ON pending_direct_tool_calls(tool_name, created_at DESC);
```

Also add `ensurePendingDirectToolCallsSchema(db)` in schema repair and call it from `ApplySchemaRepairs`.

#### 1.4 `direct_store.go`

Implement store around `*sql.DB`:

```go
type DirectToolCallStore struct { ... }

type PendingDirectToolCall struct {
    ApprovalRequestID string
    SessionID string
    TurnID string
    TaskID string
    CallID string
    ToolName string
    Input json.RawMessage
    MaxPermission tool.Permission
    Provider string
    ApprovalKind string
    NormalizedInputHash string
    PathDigest string
    InputPreview string
    Status string
    ClaimID string
    CreatedAt time.Time
    ExpiresAt time.Time
}

func (s *DirectToolCallStore) Put(ctx context.Context, row PendingDirectToolCall) error
func (s *DirectToolCallStore) GetByApproval(ctx context.Context, sessionID, approvalRequestID string) (PendingDirectToolCall, bool, error)
func (s *DirectToolCallStore) Claim(ctx context.Context, sessionID, approvalRequestID string) (PendingDirectToolCall, string, bool, error)
func (s *DirectToolCallStore) MarkConsumed(ctx context.Context, sessionID, approvalRequestID, claimID string) error
func (s *DirectToolCallStore) MarkRejected(ctx context.Context, sessionID, approvalRequestID, claimID string) error
func (s *DirectToolCallStore) MarkFailed(ctx context.Context, sessionID, approvalRequestID, claimID string, err error) error
func (s *DirectToolCallStore) ExpireStale(ctx context.Context, now time.Time) (int, error) // optional
```

Claim must be atomic:

```sql
UPDATE pending_direct_tool_calls
SET status='claimed', claim_id=?, claimed_at=?
WHERE approval_request_id=? AND session_id=? AND status='pending'
```

If zero rows updated, fetch row and return `claimed=false` with current status so resume can be idempotent.

#### 1.5 `coordinator.go`

Implement:

```go
type ApprovalCreator interface {
    CreateRequestFromDecision(sessionID string, packet protocol.DecisionPacket, expiresAt time.Time) (*protocol.ApprovalRequest, error)
}

type ApprovalConsumer interface {
    ConsumeResolvedRequest(sessionID, taskID, requestID string) (*work.ApprovalConsumeResult, error)
}

type Coordinator struct {
    Store *DirectToolCallStore
    Approvals ApprovalCreator // and consumer for resume
    Logger *slog.Logger
    HardTTL time.Duration
}
```

If importing `work.ApprovalConsumeResult` into `toolapproval` feels too coupled, define a local interface returning:

```go
type ConsumedApproval struct {
    Request *protocol.ApprovalRequest
    PreviousStatus protocol.ApprovalStatus
}
```

and adapt in chat. Keep package imports acyclic.

Implement create:

```go
func (c *Coordinator) CreatePending(ctx context.Context, req CreatePendingRequest) (*protocol.ApprovalRequest, error)
```

`CreatePendingRequest` should include:

```go
SessionID string
TurnID string
Provider string
MaxPermission tool.Permission
Classification tool.CallClassification
GoalSummary string
ExpiresAt time.Time
```

Task ID format:

```text
emotion-tool:<turn_id>:<call_id>
```

If `turn_id` is empty, generate a UUID-based synthetic id. Prefer using current turn id from `sendTurn`.

Creation flow:

```text
1. Build taskID.
2. Build DecisionPacket from classification.
3. Create ApprovalRequest through ApprovalService.
4. Build binding again from classification/call or use packet.ToolApprovalBinding.
5. Insert pending_direct_tool_calls.
6. If insert fails, expire the approval request as compensation if possible.
```

Atomic creation is desirable but not required for MVP. Compensating expiry is acceptable.

### Phase 2 — Wire creation into Emotion direct tool loop

Modify `internal/chat/engine.go`.

#### 2.1 Add coordinator dependency

Add field to `Engine`:

```go
directToolApprovals *toolapproval.Coordinator
```

Preferred wiring:

- In `NewEngine`, if `cfg.DB != nil && cfg.Approvals != nil`, create a default coordinator using `cfg.DB.SqlDB()`.
- Optionally add `EngineConfig.DirectToolApprovals` for tests or custom wiring.

Suggested:

```go
type EngineConfig struct {
    ...
    DirectToolApprovals *toolapproval.Coordinator
}
```

`NewEngine`:

```go
directApprovals := cfg.DirectToolApprovals
if directApprovals == nil && cfg.DB != nil && cfg.Approvals != nil {
    directApprovals = toolapproval.NewCoordinator(cfg.DB.SqlDB(), cfg.Approvals, cfg.Logger, toolapproval.Config{})
}
```

#### 2.2 Classify before execution

Replace the direct loop logic around tool execution.

Current rough shape:

```go
for _, call := range calls {
    result := dispatcher.Execute(ctx, call, tool.PermReadOnly)
    ...
}
toolMsgs := tool.ResultsToMessages(provider, snippedResults)
messages = append(messages, toolMsgs...)
```

New rough shape:

```go
classifications := make([]tool.CallClassification, 0, len(calls))
for _, call := range calls {
    classifications = append(classifications, dispatcher.ClassifyCall(ctx, call, tool.PermReadOnly))
}

if blocked, ok := firstDirectApprovalRequired(classifications); ok {
    approval, err := directToolApprovals.CreatePending(ctx, toolapproval.CreatePendingRequest{...})
    if err != nil { return "", err }

    emitToolApprovalRequiredActivity(rawWriter, blocked)
    if rawWriter != nil && approvals != nil {
        approvalSnapshot, _ = emitApprovalDiff(rawWriter, approvalSnapshot, approvals.ListSessionApprovals(sessionID, nil))
        // Or emit approval directly if diff snapshot does not catch it.
        _ = approval
    }
    return "", errApprovalPending
}

results := dispatcher.ExecuteAllClassified(ctx, classifications, tool.PermReadOnly)
...
```

Important requirements:

- If any tool call in a model round needs approval, execute **none** of the calls in that round.
- Emit `tool_call_end` with `status="approval_required"` for the blocked call.
- Do not convert the approval-required result into provider tool messages.
- Do not continue the LLM loop after creating a direct approval.
- Return `errApprovalPending` so `turn_runtime.go` enters `approval_wait`.

#### 2.3 Helper for blocked classification

Add local helper in `chat` or new package:

```go
func firstClassificationByAction(classifications []tool.CallClassification, action tool.CallAction) (tool.CallClassification, bool)
```

There is already a similar helper in `internal/work/runtime.go`. Avoid importing Work. Either duplicate small helper in chat or move to a neutral package later.

#### 2.4 Tool activity emission

For blocked call, emit:

```json
{
  "type": "tool_call_end",
  "tool": {
    "id": call.ID,
    "name": call.Name,
    "status": "approval_required",
    "preview": "...safe approval reason or input preview..."
  }
}
```

Use `tool.BuildApprovalBinding` and `InputPreview` for a safe preview when possible. Do not include raw full input.

#### 2.5 Approval event emission

After creating the approval request, ensure the front end sees a real approval object.

Preferred:

```go
approvalSnapshot, interrupted = emitApprovalDiff(rawWriter, approvalSnapshot, approvals.ListSessionApprovals(sessionID, nil))
return "", errApprovalPending
```

If diff logic misses it because of snapshot edge cases, emit directly:

```go
rawWriter(WSMessage{Type: "approval_required", Approval: approval})
```

Avoid double emission if possible, but duplicate `approval_required` is less dangerous than no pending approval.

### Phase 3 — Resume direct approved/rejected tool call

#### 3.1 Expose generic approval consumption

Modify `internal/work/approval.go`.

Current private method:

```go
func (s *ApprovalService) consumeRequestForResume(...) (*ApprovalConsumeResult, error)
```

Add public wrapper:

```go
func (s *ApprovalService) ConsumeResolvedRequest(sessionID, taskID, requestID string) (*ApprovalConsumeResult, error) {
    return s.consumeRequestForResume(sessionID, taskID, requestID)
}
```

Keep existing `ConsumeApprovedRequestForResume` for compatibility.

This is a generic approval ledger method. It does not make direct tool approval depend on Work runtime semantics, even though `ApprovalService` still lives in `internal/work` for now.

#### 3.2 Direct branch before Work resume

Modify `Engine.ContinueAfterApproval`:

```go
func (e *Engine) ContinueAfterApproval(...) (string, error) {
    if note, handled, terminal, err := e.resumeDirectToolApproval(ctx, sessionID, approval); err != nil {
        return "", err
    } else if handled {
        return e.sendTurn(ctx, sessionID, persona, cb, turnOptions{
            persistUser:  false,
            extraSystem:  note,
            disableTools: terminal,
            inboundKind:  turn.InboundApprovalAction,
        })
    }

    if note, handled, terminal, err := e.resumeApprovalDirectly(ctx, sessionID, approval); err != nil {
        return "", err
    } else if handled {
        ... existing Work direct resume path ...
    }

    ... existing fallback continuation note ...
}
```

Direct must be before Work because direct approvals also use `approval.TaskID`; otherwise `resume_work` may incorrectly try to claim a non-Work task.

#### 3.3 Implement `resumeDirectToolApproval`

Suggested signature:

```go
func (e *Engine) resumeDirectToolApproval(ctx context.Context, sessionID string, approval *protocol.ApprovalRequest) (note string, handled bool, terminal bool, err error)
```

Flow:

```text
1. Return handled=false if no direct coordinator/store.
2. Look up pending_direct_tool_calls by approval.ID and sessionID.
3. If not found: handled=false.
4. Claim row atomically.
5. Consume resolved approval request using ApprovalService.
6. If rejected or selected option equals reject_option_id:
      MarkRejected.
      Return internal rejection note, handled=true, terminal=true or false.
7. If approved:
      Build ApprovalContext from ApprovalRequest.ToolApprovalBinding.
      Execute dispatcher.Execute(tool.WithApproval(ctx,...), stored call, stored max permission).
      If NeedsApproval again: MarkFailed and return error.
      MarkConsumed.
      Build internal outcome note from snipped result.
      Return handled=true.
```

Use `work.WithSessionID(ctx, sessionID)` before execution, matching current `sendTurn` setup.

Do not execute the registry handler directly.

#### 3.4 Direct outcome notes

Add helpers in `chat` or `toolapproval`:

```go
func buildDirectToolApprovalOutcomeNote(approval *protocol.ApprovalRequest, call tool.Call, result tool.Result, digest contextutil.ToolResultDigest) string
func buildDirectToolApprovalRejectedNote(approval *protocol.ApprovalRequest, call tool.Call) string
```

Suggested approved note format:

```text
## Internal Direct Tool Approval Outcome
A user approval decision was received for a pending direct Emotion tool call.
This note is internal runtime state, not user-facing content.

Approval request <id> for tool <tool_name> was approved.
The approved tool call has already been executed exactly once by the host.
Do not call the same tool again for this approval request.
Do not reveal approval_request_id, internal task_id, raw protocol objects, or approval plumbing.

Tool result, snipped for context:
<safe JSON/string result>

Use the tool result naturally in the next reply. If the tool result is an error, explain it briefly and safely.
```

Suggested rejected note format:

```text
## Internal Direct Tool Approval Outcome
The user rejected a pending direct Emotion tool call.
The tool was not executed.
Do not call the same tool again unless the user explicitly asks to retry.
Do not reveal approval_request_id, internal task_id, or approval plumbing.

Continue naturally by acknowledging that the action was cancelled or by offering a safe alternative.
```

For rejected approvals, set `disableTools=true` for the continuation turn to reduce immediate re-request loops. For approved approvals, `disableTools=false` is acceptable so the model can continue naturally.

#### 3.5 Idempotency and duplicate safety

The store claim prevents duplicate execution.

If the row is already `consumed`, `rejected`, `expired`, or `failed`, return an internal no-op note or handled result that does not execute again.

Codex should prefer fail-closed behavior:

```text
unknown store status → handled=true + no execution + safe note or error
approval binding missing → no execution + error
approval binding mismatch → dispatcher returns NeedsApproval → error
```

### Phase 4 — Tests and regression suite

Add tests in at least these packages:

```text
internal/toolapproval
internal/storage
internal/chat
```

#### 4.1 `internal/toolapproval` tests

- `TestBuildToolApprovalPacket_PluginInvocationBinding`
- `TestApprovalContextFromRequest_PreservesBinding`
- `TestDirectToolCallStore_PutClaimConsume`
- `TestDirectToolCallStore_ClaimIsSingleUse`

#### 4.2 storage tests

- Migration applies on fresh DB.
- `ensurePendingDirectToolCallsSchema` repairs missing table/columns.
- Foreign key to `approval_requests` works if used.

#### 4.3 chat engine tests

##### Test 1: creates pending and stops

```go
func TestEngineSendMessage_DirectToolApprovalCreatesPendingAndStopsBeforeToolMessage(t *testing.T)
```

Setup:

- Fake LLM returns a `tool_use` response.
- Registry contains a read-only Emotion tool with `ApprovalClassifier` returning `ApprovalKindPluginInvocation` or `SensitiveRead`.
- Handler increments counter if executed.
- Engine has `ApprovalService` and direct coordinator.

Assert:

```text
sendTurn returns errApprovalPending
handler counter == 0
approval_requests has one pending request
pending_direct_tool_calls has one pending row
approval ToolApprovalBinding has correct tool_name and normalized_input_hash
outbound includes tool_call_end status=approval_required
outbound includes approval_required with ApprovalRequest
no second LLM call was made with tool_result approval error
```

##### Test 2: approved direct tool executes once

```go
func TestEngineContinueAfterApproval_ExecutesPendingDirectToolOnce(t *testing.T)
```

Flow:

```text
create pending direct approval
approve request with option_id=allow
call ContinueAfterApproval
```

Assert:

```text
tool handler executed exactly once
pending_direct_tool_calls.status == consumed
approval_requests.status == consumed, if generic consume implemented
execution context has matching ApprovalContext
tool result appears in internal note to subsequent LLM request
no resume_work call was attempted for this direct approval
```

##### Test 3: rejected direct tool does not execute

```go
func TestEngineContinueAfterApproval_RejectedDirectToolDoesNotExecute(t *testing.T)
```

Assert:

```text
tool handler counter == 0
pending row status == rejected
continuation note says tool was not executed
```

##### Test 4: binding mismatch fails closed

Mutate the stored input after approval or build a mismatched context.

Assert:

```text
dispatcher returns NeedsApproval or error
tool handler not executed
pending row status == failed
```

##### Test 5: turn pipeline replay

```go
func TestTurnRuntimeDirectToolApprovalWaitReplaysApprovalRequired(t *testing.T)
```

Assert duplicate idempotency key:

```text
first execution enters approval_wait
second duplicate does not run engine again
second duplicate replays approval_required
```

#### 4.4 existing regression tests must still pass

Run:

```powershell
go test ./internal/plugin -run "TestProcessToolSpecRequiresInvocationApproval|TestPluginToolStillUsesDispatcherApprovalGate|TestSDKExamplePluginToolRequiresApproval" -count=1
go test ./internal/chat -run "TestEngineSendMessage_StopsTurnImmediatelyWhenToolApprovalIsRaised|TestTurnRuntimeDuplicateApprovalWaitReplaysApprovalRequired" -count=1
go test ./internal/tool -run "Approval|Dispatcher" -count=1
go test ./internal/work -run "Approval|ToolApproval|Resume" -count=1
```

Then broader:

```powershell
go test ./internal/plugin ./internal/tool ./internal/work ./internal/chat ./internal/storage -count=1
```

### Phase 5 — Cleanup and architecture consolidation

After MVP works, reduce duplication.

#### 5.1 Move shared Work approval wording/building

Current Work code has unexported helpers for tool approval packet and plugin wording. Replace them with `internal/toolapproval` helpers.

Target:

```text
Work runtime uses toolapproval.BuildToolApprovalPacket(...)
Emotion direct approval uses the same builder
```

This ensures plugin invocation approval cards look the same whether the tool is called by Work or Emotion.

#### 5.2 Consider package rename later

`ApprovalService` currently lives in `internal/work`, but it is effectively a shared approval ledger.

Do **not** move it in MVP.

Later optional migration:

```text
internal/work/approval.go → internal/approval/service.go
```

Only do this after direct lifecycle is stable because it touches many imports.

#### 5.3 Optional provider-native resume

If internal note continuation is not good enough, add a v2 resume mode:

```sql
assistant_tool_use_message_json TEXT
messages_snapshot_json TEXT
resume_mode TEXT CHECK(resume_mode IN ('internal_note','provider_tool_result'))
```

Do not implement in MVP.

---

## 4. Important edge cases

### 4.1 Multiple tool calls in one model round

If any call requires approval, execute none in that round.

Reason: executing some tools before pausing can make approval semantics confusing and can leak side effects before user consent.

### 4.2 Approval service unavailable

If `ApprovalService` or direct coordinator is nil and a tool requires approval, fail closed.

Return a real error rather than feeding approval-required result back to LLM.

### 4.3 Direct approval row missing on continuation

If `ContinueAfterApproval` cannot find a direct pending row, return `handled=false` so Work resume can try. This allows Work approvals to continue unchanged.

If row exists but is already consumed/rejected/failed, handle it as direct and do not fall through to Work.

### 4.4 Direct approval accidentally routed to Work

Prevent this by checking direct store before `resumeApprovalDirectly`.

### 4.5 Approval binding missing

For direct tool approvals, missing `ToolApprovalBinding` is a hard error. Do not execute.

### 4.6 Approval created but pending row insert fails

Compensate by expiring the approval request if possible. Return error and do not enter approval_wait with an orphan approval.

### 4.7 Weather plugin with `invocation_policy=auto`

Do not remove support for `InvocationAuto`. The weather plugin can remain auto for low-risk read-only use. This change is for future plugins/tools that legitimately require approval.

### 4.8 Frontend compatibility

No frontend protocol change should be required.

Existing events remain:

```text
tool_call_start
tool_call_end(status=approval_required)
approval_required
approval_updated
turn_status approval_wait
```

The crucial change is that `approval_required` must carry an actual `ApprovalRequest` and `/approvals` must list it.

---

## 5. Acceptance criteria

### 5.1 Architecture acceptance

- Plugin adapter still only maps plugin tool policy into `tool.Spec` / `ApprovalClassifier`.
- Dispatcher still does not import chat/work/storage DB or create approval requests.
- Work `PendingRegistry` remains only for Work paused tasks.
- Emotion direct tool approvals use a separate `pending_direct_tool_calls` table.
- `approval_requests` remains the shared UI/API ledger.

### 5.2 Runtime acceptance

For an Emotion-visible tool requiring approval:

```text
Before user approval:
  tool handler is not executed
  approval_requests has pending row
  pending_direct_tool_calls has pending row
  UI receives approval_required with ApprovalRequest
  Turn Pipeline returns approval_wait
  no approval_required tool_result is sent back to LLM

After user approval:
  exact stored tool call executes once
  execution runs through dispatcher with tool.WithApproval
  approval binding matches current call
  pending row becomes consumed
  Emotion receives internal result note and answers naturally

After user rejection:
  tool handler is not executed
  pending row becomes rejected
  Emotion continues with safe cancellation response
```

### 5.3 Security acceptance

- Approval for one tool input cannot be reused for another input.
- Mutated input after approval fails closed.
- Missing binding fails closed.
- Duplicate resume cannot execute the tool twice.
- Stale/expired approvals cannot execute.
- Tool result snipping respects existing context budget behavior.

### 5.4 Regression acceptance

Existing plugin, dispatcher, Work approval, and turn idempotency tests continue to pass.

At minimum run:

```powershell
go test ./internal/plugin ./internal/tool ./internal/work ./internal/chat ./internal/storage -count=1
```

If full suite is reasonable:

```powershell
go test ./... -count=1
```

---

## 6. Suggested code skeletons

### 6.1 `internal/toolapproval/packet.go`

```go
package toolapproval

import (
    "fmt"
    "strings"
    "time"

    "github.com/longyisang/emoagent/internal/protocol"
    "github.com/longyisang/emoagent/internal/tool"
)

func BuildToolApprovalPacket(taskID, goalSummary string, classification tool.CallClassification) protocol.DecisionPacket {
    call := classification.Call
    kind := classification.ApprovalKind
    if kind == "" {
        kind = tool.ApprovalKindDestructiveWrite
    }

    var packetBinding *protocol.ToolApprovalBinding
    if binding, err := tool.BuildApprovalBinding(call, "", kind); err == nil {
        packetBinding = &protocol.ToolApprovalBinding{
            ApprovalKind:        binding.ApprovalKind,
            ToolName:            binding.ToolName,
            NormalizedInputHash: binding.NormalizedInputHash,
            PathDigest:          binding.PathDigest,
            InputPreview:        binding.InputPreview,
            ChangeSetID:         binding.ChangeSetID,
            PlanHash:            binding.PlanHash,
            ResourceID:          binding.ResourceID,
            CanonicalPathHash:   binding.CanonicalPathHash,
            BaselineHash:        binding.BaselineHash,
            BaselineFileID:      binding.BaselineFileID,
            DeleteMode:          binding.DeleteMode,
        }
    }

    return protocol.DecisionPacket{
        TaskID:               strings.TrimSpace(taskID),
        Category:             protocol.CatToolApproval,
        GoalSummary:          firstNonEmpty(goalSummary, "Execute an approval-gated Emotion tool call"),
        Question:             ToolApprovalQuestion(classification),
        WhyBlocked:           ToolApprovalWhyBlocked(classification),
        Options:              []protocol.DecisionOption{{ID: "allow", Summary: "允许执行"}, {ID: "deny", Summary: "拒绝"}},
        RejectOptionID:       "deny",
        RecommendedOption:    "allow",
        RecommendationReason: "该工具尚未执行；批准后只会执行当前输入绑定的一次调用。",
        SuggestsUserInput:    false,
        ToolApprovalBinding:  packetBinding,
        CreatedAt:            time.Now().UTC(),
    }
}

func ToolApprovalQuestion(classification tool.CallClassification) string {
    call := classification.Call
    if classification.ApprovalKind == tool.ApprovalKindPluginInvocation {
        return fmt.Sprintf("即将调用第三方插件或外部工具 `%s`。该工具尚未执行；批准后才会执行本次输入对应的一次调用。", call.Name)
    }
    return fmt.Sprintf("我准备执行受限工具 `%s`，尚未执行。确认执行请点击允许；取消请点击拒绝。", call.Name)
}

func ToolApprovalWhyBlocked(classification tool.CallClassification) string {
    if classification.ApprovalReason != "" {
        return classification.ApprovalReason
    }
    return fmt.Sprintf("Tool %q requires explicit approval before execution.", classification.Call.Name)
}
```

Codex should refine wording by reusing current Work wording where possible.

### 6.2 `internal/toolapproval/context.go`

```go
package toolapproval

import (
    "fmt"

    "github.com/longyisang/emoagent/internal/protocol"
    "github.com/longyisang/emoagent/internal/tool"
)

func ApprovalContextFromRequest(req *protocol.ApprovalRequest) (tool.ApprovalContext, error) {
    if req == nil {
        return tool.ApprovalContext{}, fmt.Errorf("approval request is nil")
    }
    if req.ToolApprovalBinding == nil {
        return tool.ApprovalContext{}, fmt.Errorf("approval request %s has no tool binding", req.ID)
    }
    b := req.ToolApprovalBinding
    if b.ToolName == "" || b.NormalizedInputHash == "" {
        return tool.ApprovalContext{}, fmt.Errorf("approval request %s has incomplete tool binding", req.ID)
    }

    ctx := tool.ApprovalContext{
        RequestID:           req.ID,
        ApprovalKind:        b.ApprovalKind,
        AllowToolCall:       true,
        ToolName:            b.ToolName,
        NormalizedInputHash: b.NormalizedInputHash,
        PathDigest:          b.PathDigest,
        ChangeSetID:         b.ChangeSetID,
        PlanHash:            b.PlanHash,
        ResourceID:          b.ResourceID,
        CanonicalPathHash:   b.CanonicalPathHash,
        BaselineHash:        b.BaselineHash,
        BaselineFileID:      b.BaselineFileID,
        DeleteMode:          b.DeleteMode,
    }
    if ctx.ApprovalKind == "" {
        ctx.ApprovalKind = string(tool.ApprovalKindDestructiveWrite)
    }
    if ctx.ApprovalKind == string(tool.ApprovalKindDestructiveWrite) {
        ctx.AllowDestructive = true
    }
    return ctx, nil
}
```

### 6.3 `Engine.sendTurn` direct approval intercept pseudo-patch

```go
classifications := make([]tool.CallClassification, 0, len(calls))
for _, call := range calls {
    classifications = append(classifications, dispatcher.ClassifyCall(ctx, call, tool.PermReadOnly))
}

if blocked, ok := firstClassificationByAction(classifications, tool.CallActionToolApprovalRequired); ok {
    if directApprovals == nil {
        return "", fmt.Errorf("tool %q requires approval but direct tool approval coordinator is not configured", blocked.Call.Name)
    }
    approval, err := directApprovals.CreatePending(ctx, toolapproval.CreatePendingRequest{
        SessionID: sessionID,
        TurnID: memoryAnchor.turnID,
        Provider: provider,
        MaxPermission: tool.PermReadOnly,
        Classification: blocked,
        GoalSummary: "Emotion direct tool call requested by the model",
        ExpiresAt: time.Now().UTC().Add(30 * time.Minute),
    })
    if err != nil {
        return "", err
    }

    emitDirectApprovalRequiredToolActivity(rawWriter, blocked)

    if rawWriter != nil {
        if approvals != nil {
            approvalSnapshot, _ = emitApprovalDiff(rawWriter, approvalSnapshot, approvals.ListSessionApprovals(sessionID, nil))
        } else {
            rawWriter(WSMessage{Type: "approval_required", Approval: approval})
        }
    }
    return "", errApprovalPending
}

results := dispatcher.ExecuteAllClassified(ctx, classifications, tool.PermReadOnly)
```

### 6.4 `ContinueAfterApproval` pseudo-patch

```go
func (e *Engine) ContinueAfterApproval(ctx context.Context, sessionID string, persona *config.Persona, approval *protocol.ApprovalRequest, cb func(delta string)) (string, error) {
    if approval == nil {
        return "", errors.New("approval is required")
    }

    if note, handled, terminal, err := e.resumeDirectToolApproval(ctx, sessionID, approval); err != nil {
        return "", err
    } else if handled {
        return e.sendTurn(ctx, sessionID, persona, cb, turnOptions{
            persistUser: false,
            extraSystem: note,
            disableTools: terminal,
            inboundKind: turn.InboundApprovalAction,
        })
    }

    if note, handled, terminal, err := e.resumeApprovalDirectly(ctx, sessionID, approval); err != nil {
        return "", err
    } else if handled {
        return e.sendTurn(ctx, sessionID, persona, cb, turnOptions{
            persistUser: false,
            extraSystem: note,
            disableTools: terminal,
            inboundKind: turn.InboundApprovalAction,
        })
    }

    note := buildApprovalContinuationNote(approval)
    return e.sendTurn(ctx, sessionID, persona, cb, turnOptions{
        persistUser: false,
        extraSystem: note,
        inboundKind: turn.InboundApprovalAction,
    })
}
```

---

## 7. Do not implement in this pass

Do not implement these in MVP:

```text
per-plugin persistent allowlist / remember approval
provider-native tool transcript resume
full migration of ApprovalService out of internal/work
front-end redesign
new approval event protocol
background async approval worker
automatic trust decisions from plugin permission only
storing full LLM message snapshots for every pending approval
```

These can be future improvements after the direct lifecycle is stable.

---

## 8. Final checklist for Codex

Before declaring complete:

- [ ] A direct Emotion tool requiring approval creates a real `approval_requests` pending row.
- [ ] The exact `tool.Call` is stored in `pending_direct_tool_calls`.
- [ ] Turn status becomes `approval_wait`.
- [ ] UI can receive `approval_required` with an `ApprovalRequest`.
- [ ] No tool result representing `NeedsApproval` is fed back to the LLM.
- [ ] User approval executes exactly one matching call through dispatcher.
- [ ] User rejection executes no call.
- [ ] Duplicate resume cannot double-execute.
- [ ] Work approvals still resume through `resume_work`.
- [ ] Existing plugin ask/auto/deny behavior is unchanged.
- [ ] Existing tests plus new tests pass.

