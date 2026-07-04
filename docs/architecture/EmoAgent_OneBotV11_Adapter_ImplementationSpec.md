# EmoAgent OneBot v11 Adapter Implementation Spec

> Status: Implementation Spec for Codex
> Date: 2026-07-04
> Target path: `docs/architecture/onebot_v11_adapter_implementation_spec.md`
> Scope: Build a reusable OneBot v11 protocol adapter layer for EmoAgent, with Generic / NapCat / SnowLuma profiles, private-message MVP, no media, no group intake, and simple forward + reverse WebSocket support.

---

## 0. Executive summary

The current EmoAgent repository already has the right pre-NapCat foundation:

```text
internal/platform        = EmoAgent platform bus DTOs and Adapter interface
app.PlatformGateway     = platform-neutral ingress → command/session/chat gateway
conversation.Binding    = origin_key/persona_key → current session
CommandService          = slash command router and executor
platform_message_receipts = inbound idempotency store
```

This spec adds a high-cohesion protocol-family package:

```text
internal/platform/onebotv11
```

`onebotv11.Adapter` will implement `platform.Adapter` and translate between OneBot v11 JSON events/actions and EmoAgent `platform.InboundMessage` / `platform.OutboundEvent`.

NapCat and SnowLuma should **not** be implemented as separate full adapters. They are OneBot v11 implementations and should be represented as **profiles**:

```text
generic | napcat | snowluma
```

The OneBot adapter should be built fully enough that future OneBot-compatible platforms require only configuration and small profile quirks.

---

## 1. User decisions incorporated

### Decision 1: Stable Origin across OneBot implementations

Use:

```text
SourceType = onebot
PlatformID = qq
AdapterInstanceID = stable logical instance id, e.g. qq-main
ImplementationProfile = napcat | snowluma | generic
```

Do **not** put `napcat` or `snowluma` into `SourceType`. This keeps `origin_key` stable when switching from NapCat to SnowLuma:

```text
onebot:qq-main:private:<user_id>
onebot:qq-main:group:<group_id>        // future, group disabled in MVP
```

### Decision 2: Support both forward WS and simple reverse WS

Implement both:

```text
ws_client       EmoAgent connects to OneBot forward WebSocket `/` endpoint.
ws_reverse      OneBot connects to EmoAgent reverse WebSocket endpoint.
```

Reverse WS should be a simple locally deployed form. Default assumption: EmoAgent and OneBot implementation are on the same machine. Do not build complex security policy in this phase.

MVP reverse WS:

```text
GET /api/platforms/onebot/v11/{adapter_id}/ws
Accept X-Client-Role: Universal
Accept X-Self-ID for diagnostics / fallback instance id
Optional Authorization Bearer token if configured
No multi-tenant security model
No remote exposure hardening beyond optional token
```

### Decision 3: Do not accept group messages yet

First version:

```text
private messages: accepted
group messages: ignored
notice/request/meta events: ignored
```

Do not implement group activation yet. Do not route group `/command` yet. Keep the OneBot model extensible for future `group_shared` and `group_user_unique`, but default config must not process groups.

### Decision 4: No media yet

First version is text-only.

Inbound media segments are rendered as safe placeholders and kept only in raw metadata. They should not enter `Parts`, because `PlatformGateway` currently rejects `InboundMessage.Parts`.

Outbound media is unsupported. Only text replies are sent.

### Decision 5: OneBot as the adapter, NapCat / SnowLuma as profiles

Implement:

```text
onebotv11.Adapter
onebotv11.ProfileGeneric
onebotv11.ProfileNapCat
onebotv11.ProfileSnowLuma
```

Do not duplicate adapter logic for NapCat and SnowLuma.

---

## 2. Current repository baseline

### 2.1 Existing platform DTOs

Current `internal/platform/types.go` has:

```go
type InboundMessage struct {
    ID                     string
    ExternalMessageID      string
    SourceType             string
    AdapterInstanceID      string
    PlatformID             string
    ChannelType            string
    ExternalConversationID string
    ExternalActorID        string
    PersonaKey             string
    Text                   string
    Parts                  []llm.ContentBlock
    Actor                  Actor
    Timestamp              time.Time
    RawEventHash           string
    Raw                    map[string]any
}

type OutboundEvent struct {
    Type                     string
    Origin                   conversation.Origin
    SessionID                string
    PersonaKey               string
    Content                  string
    Status                   string
    ErrorKind                string
    Payload                  map[string]any
    ReplyToExternalMessageID string
}
```

Current `HandleResult` has `Handled`, `Duplicate`, `SessionID`.

### 2.2 Existing origin builder

`platform.BuildOriginKey` already supports:

```text
private
 group_shared
 group_user_unique
```

For private messages it produces:

```text
<source>:<instance>:private:<actor_id>
```

For future group shared messages:

```text
<source>:<instance>:group:<group_id>
```

For future per-user-in-group messages:

```text
<source>:<instance>:group_user:<group_id>:<actor_id>
```

### 2.3 Existing PlatformGateway

`app.PlatformGateway.HandleInbound` currently performs:

```text
1. reject unsupported Parts
2. reject empty text
3. build Origin from InboundMessage
4. BeginInbound receipt dedupe
5. select default persona
6. EnsureCurrent session binding
7. CommandService.TryHandle
8. Chat Engine SendMessage
9. emit platform.OutboundEvent
10. complete/fail receipt
```

This should remain the central gateway. OneBot must not bypass it.

### 2.4 Existing config skeleton

Current config has:

```go
type PlatformsConfig struct {
    Enabled  bool
    Common   PlatformCommonConfig
    Adapters map[string]PlatformAdapterConfig
}

type PlatformAdapterConfig struct {
    Enabled    bool
    Kind       string
    InstanceID string
    PlatformID string
    ConfigJSON map[string]any
}
```

The OneBot adapter should live inside this existing skeleton.

### 2.5 Existing service wiring

`Kernel.Services` already contains `Platforms *PlatformService`, and `NewPlatformService(...)` builds a `PlatformGateway` and `platform.Manager`.

This spec extends `PlatformService` so configured OneBot adapters can be registered, started, stopped, and expose reverse WS HTTP routes.

---

## 3. Target architecture

### 3.1 Layering

```text
NapCat / SnowLuma / other OneBot v11 implementation
        ↓
internal/platform/onebotv11
  protocol types
  message rendering
  event mapper
  action client
  ws client transport
  reverse ws server transport
  profiles
        ↓
internal/platform
  platform-neutral DTOs and Adapter interface
        ↓
app.PlatformGateway
  receipt dedupe
  origin binding
  commands
  chat engine
        ↓
OneBot sink
  OutboundEvent → send_private_msg / send_group_msg action
```

### 3.2 Design principle

`internal/platform/onebotv11` should be high-cohesion and own all OneBot-specific logic:

```text
JSON event/action structs
message segment parse/render
CQ string fallback
external message id composition
OneBot role mapping
OneBot event filtering
OneBot action request/response/echo correlation
forward WS lifecycle
reverse WS endpoint and connection lifecycle
profile quirks
```

Do not leak OneBot-specific fields into `app.PlatformGateway` unless the field is platform-general.

---

## 4. Package layout

Create:

```text
internal/platform/onebotv11/
  action.go          // ActionRequest, ActionResponse, ActionClient helpers
  adapter.go         // implements platform.Adapter
  config.go          // OneBotConfig parsed from PlatformAdapterConfig.ConfigJSON
  event.go           // Event, Sender, Anonymous, EventKind helpers
  message.go         // Message, Segment, array/string/CQ rendering
  mapper.go          // OneBot Event → platform.InboundMessage
  sink.go            // platform.OutboundEvent → OneBot action call
  profile.go         // generic/napcat/snowluma profile definitions
  transport.go       // Transport interface
  ws_client.go       // forward WS universal client
  ws_reverse.go      // simple reverse WS server transport
  echo.go            // pending echo map / timeouts
  errors.go          // typed errors, retcode handling
  diagnostics.go     // status snapshots
  testdata/
```

Update or add:

```text
internal/platform/types.go
internal/app/platform_service.go
internal/app/server.go
internal/config/config.go
internal/storage/platform_receipts.go        // only if ignored receipt status is added
internal/storage/schema.go                   // only if schema must change
```

---

## 5. Minimal platform DTO adjustments

### 5.1 Add `OriginScope` to `platform.InboundMessage`

Current `PlatformGateway` calls:

```go
platform.OriginFromInbound(in, "")
```

Change to:

```go
platform.OriginFromInbound(in, in.OriginScope)
```

Add:

```go
type InboundMessage struct {
    ...
    OriginScope    OriginScope
    AcceptedReason string // private | future: command_prefix | mention | reply_to_bot | always
}
```

For OneBot MVP:

```text
private message → OriginScopePrivate, AcceptedReason=private
group message   → ignored before PlatformGateway
```

### 5.2 Optional: add `Ignored` to `platform.HandleResult`

Add:

```go
type HandleResult struct {
    Handled   bool
    Duplicate bool
    Ignored   bool
    SessionID string
}
```

MVP OneBot can ignore group messages inside the adapter without calling `PlatformGateway`; this field is still useful for future observability.

### 5.3 Keep `Parts` unsupported for now

Do not make OneBot mapper populate `InboundMessage.Parts` in MVP. `PlatformGateway` currently rejects `Parts`; the mapper should render all inbound content to safe text.

---

## 6. Configuration

### 6.1 User-facing config example

Add docs and tests for this shape:

```yaml
platforms:
  enabled: true
  common:
    default_persona: default
    command_prefixes: ["/"]

  adapters:
    qq-main:
      enabled: true
      kind: onebot_v11
      instance_id: qq-main
      platform_id: qq
      config:
        implementation: napcat        # generic | napcat | snowluma
        source_type: onebot

        transport:
          mode: ws_reverse             # ws_client | ws_reverse

          # ws_client mode only:
          url: ws://127.0.0.1:3001/

          # ws_reverse mode only:
          reverse_path: /api/platforms/onebot/v11/qq-main/ws

          access_token: ""             # optional, dev/local may be empty
          access_token_env: ""         # optional; env wins if set
          request_timeout_ms: 15000
          connect_timeout_ms: 5000
          reconnect_interval_ms: 3000

        routing:
          private_enabled: true
          group_enabled: false          # important MVP default
          ignore_self_messages: true
          private_scope: private
          group_scope: group_shared     # future only; group disabled now

        message:
          input_format: array            # array | string | auto
          output_format: array           # array | string
          unsupported_segment_policy: placeholder
          max_text_chars: 8000

        outbound:
          auto_escape: false
          coalesce_command_events: true
          split_long_messages: true
          max_message_chars: 1800
```

### 6.2 Config struct

Inside `internal/platform/onebotv11/config.go` define a strongly typed config and decode `PlatformAdapterConfig.ConfigJSON` into it.

```go
type Config struct {
    Implementation string
    SourceType     string
    Transport      TransportConfig
    Routing        RoutingConfig
    Message        MessageConfig
    Outbound       OutboundConfig
}
```

Apply defaults:

```text
implementation = generic
source_type = onebot
platform_id = PlatformAdapterConfig.PlatformID or qq
adapter_instance_id = PlatformAdapterConfig.InstanceID or config key
transport.mode = ws_reverse if reverse_path set else ws_client
routing.private_enabled = true
routing.group_enabled = false
routing.ignore_self_messages = true
message.input_format = array
message.output_format = array
outbound.coalesce_command_events = true
```

Validate:

```text
kind must be onebot_v11
mode must be ws_client or ws_reverse
ws_client requires url
ws_reverse requires reverse_path or uses default path
request_timeout_ms > 0
max_message_chars > 0
implementation in generic | napcat | snowluma
```

---

## 7. OneBot protocol model

### 7.1 Event struct

Define a practical OneBot v11 event struct:

```go
type Event struct {
    Time     int64  `json:"time"`
    SelfID   int64  `json:"self_id"`
    PostType string `json:"post_type"`

    MessageType string          `json:"message_type,omitempty"` // private | group
    SubType     string          `json:"sub_type,omitempty"`
    MessageID   json.RawMessage `json:"message_id,omitempty"`
    UserID      int64           `json:"user_id,omitempty"`
    GroupID     int64           `json:"group_id,omitempty"`

    Message    RawMessageValue `json:"message,omitempty"`
    RawMessage string          `json:"raw_message,omitempty"`
    Sender     Sender          `json:"sender,omitempty"`
    Anonymous  *Anonymous      `json:"anonymous,omitempty"`

    Raw map[string]any `json:"-"`
}
```

`RawMessageValue` should support both array message and string/CQ-code message.

### 7.2 Message segment

```go
type Segment struct {
    Type string            `json:"type"`
    Data map[string]string `json:"data"`
}

type Message []Segment
```

MVP segment support:

| Segment | Inbound behavior | Outbound behavior |
|---|---|---|
| `text` | append text | supported |
| `at` | private: placeholder or empty; group ignored | future only |
| `reply` | metadata/placeholder only | optional future |
| `image` | `[图片]` placeholder | unsupported |
| `record` | `[语音]` placeholder | unsupported |
| `video` | `[视频]` placeholder | unsupported |
| unknown | `[<type>]` placeholder | unsupported |

### 7.3 Action model

```go
type ActionRequest struct {
    Action string         `json:"action"`
    Params map[string]any `json:"params,omitempty"`
    Echo   string         `json:"echo,omitempty"`
}

type ActionResponse struct {
    Status  string          `json:"status"`
    Retcode int             `json:"retcode"`
    Data    json.RawMessage `json:"data"`
    Echo    any             `json:"echo,omitempty"`
    Wording string          `json:"wording,omitempty"`
}
```

Treat `status != "ok"` or non-zero/non-OneBot-success `retcode` as action failure. Keep retcode policy configurable by profile.

---

## 8. Event mapping

### 8.1 Accept only private messages in MVP

Adapter event handling:

```text
post_type != message       → ignore
message_type == private    → accept if private_enabled
message_type == group      → ignore because group_enabled=false by default
self message               → ignore if ignore_self_messages=true
empty rendered text        → ignore
unsupported non-text only   → ignore or placeholder text depending policy
```

### 8.2 Private event → platform.InboundMessage

Map:

```go
platform.InboundMessage{
    SourceType:             "onebot",
    AdapterInstanceID:      adapterInstanceID,
    PlatformID:             "qq",
    ChannelType:            "private",
    ExternalConversationID: userID,
    ExternalActorID:        userID,
    ExternalMessageID:      compositeExternalMessageID(event),
    PersonaKey:             configuredPersonaOrEmpty,
    Text:                   renderedText,
    Actor: platform.Actor{
        ID:          userID,
        DisplayName: sender.Nickname,
        Role:        platform.ActorRoleMember,
        IsBot:       false,
        Raw:         senderRaw,
    },
    Timestamp:     event time,
    RawEventHash:  stable hash of raw event JSON,
    Raw:           safe raw map,
    OriginScope:   platform.OriginScopePrivate,
    AcceptedReason: "private",
}
```

### 8.3 ExternalMessageID composition

Do not use `message_id` alone. Use a composite:

```text
<self_id>:<message_type>:<conversation_id>:<message_id>
```

Examples:

```text
123456:private:11223344:24680
123456:group:987654321:13579   // future, group ignored now
```

This matches the existing receipt unique key semantics:

```text
source_type + adapter_instance_id + external_message_id
```

### 8.4 Actor role mapping

OneBot private messages:

```text
member
```

OneBot group messages, future:

```text
sender.role owner → platform.ActorRoleOwner
sender.role admin → platform.ActorRoleAdmin
else              → platform.ActorRoleMember
```

Even though groups are ignored in MVP, implement role mapping unit tests now for future readiness.

---

## 9. Outbound mapping

### 9.1 OneBotSink

Create:

```go
type Sink struct {
    client ActionClient
    profile Profile
    cfg OutboundConfig
}
```

It implements:

```go
func (s *Sink) Emit(ctx context.Context, event platform.OutboundEvent) error
```

### 9.2 Event type rendering

| `platform.OutboundEvent.Type` | OneBot behavior |
|---|---|
| `message` | send text message |
| `command_result` | send `Content` as text |
| `context_switched` | coalesce with command result; if alone, send `Content` |
| `error` | send concise error text |
| other | ignore unless `Content` non-empty and profile allows |

### 9.3 Coalesce duplicate command events

`CommandService` may emit both `context_switched` and `command_result` for one command. WebUI needs both; QQ should not get duplicate messages.

MVP `OneBotSink` should implement local coalescing:

```text
If event A is context_switched and event B is command_result with same content/status/session within the same inbound handling, send only one visible text message.
```

Simpler implementation:

```text
Send context_switched immediately;
if the next command_result has identical content and occurs within short coalescing window, suppress it.
```

Alternative acceptable implementation:

```text
PlatformGateway exposes a platform event coalescer before passing events to sink.
```

The sink-based approach is less invasive.

### 9.4 Send private message

For private origin:

```go
send_private_msg(user_id, message, auto_escape)
```

`user_id` comes from `event.Origin.ExternalActorID` or `ExternalConversationID`.

### 9.5 Send group message, future-ready only

Implement action builder for group messages even though group inbound is disabled:

```go
send_group_msg(group_id, message, auto_escape)
```

If somehow a group `OutboundEvent` appears and `group_enabled=false`, return a clear error or ignore with log depending config.

### 9.6 Long message splitting

MVP support optional splitting:

```text
if len(content) > max_message_chars and split_long_messages=true:
    split by paragraph/newline first, then rune length
```

Do not split inside UTF-8 bytes.

---

## 10. Transport design

### 10.1 Interface

```go
type Transport interface {
    Start(ctx context.Context, handler EventHandler) error
    Stop(ctx context.Context) error
    Call(ctx context.Context, req ActionRequest) (ActionResponse, error)
    Status() TransportStatus
}

type EventHandler interface {
    HandleOneBotEvent(context.Context, Event) error
}
```

### 10.2 Forward WS client transport

`ws_client` mode:

```text
EmoAgent dials OneBot forward WebSocket URL, usually ws://127.0.0.1:3001/
Use Universal `/` endpoint where events and API responses share one connection.
Send Authorization: Bearer <token> if configured.
Read loop demuxes event vs action response.
Call() sends action request with echo and waits for response.
Reconnect on disconnect.
Fail pending calls on disconnect.
```

### 10.3 Reverse WS server transport

`ws_reverse` mode:

```text
OneBot implementation connects to EmoAgent.
EmoAgent exposes GET /api/platforms/onebot/v11/{adapter_id}/ws.
MVP supports Universal client role only.
Use X-Self-ID for self id diagnostics and fallback.
Use X-Client-Role to reject unsupported roles.
Optional Authorization Bearer validation if token configured.
The established connection supports both events and action calls.
```

Implementation shape:

```go
type ReverseServer struct {
    adapters map[string]*Adapter
}

func (s *ReverseServer) ServeHTTP(w http.ResponseWriter, r *http.Request)
```

`PlatformService` should expose route installation:

```go
func (s *PlatformService) InstallHTTPRoutes(mux *http.ServeMux)
```

`server.go` should call this before static handler registration.

### 10.4 Reverse WS constraints

MVP constraints are intentional:

```text
Only Universal role required.
No separate API/Event connection pair in first version.
No HTTP POST in first version.
No complex remote security policy.
No TLS termination inside adapter.
No multi-account authorization beyond adapter_id + optional token.
```

If a user configures OneBot reverse WS as separate API/Event connections, the adapter should log a clear unsupported-role error and close with a readable message.

### 10.5 Echo store

Implement:

```go
type EchoStore struct {
    pending map[string]chan ActionResponse
}
```

Requirements:

```text
echo must be unique per request
Call() must respect ctx timeout
responses with unknown echo are logged and ignored
pending calls are failed when connection closes
```

---

## 11. Adapter lifecycle

### 11.1 `onebotv11.Adapter`

```go
type Adapter struct {
    id        string
    cfg       Config
    profile   Profile
    transport Transport
    handler   platform.InboundHandler
    sink      *Sink
}
```

Implements:

```go
func (a *Adapter) Start(ctx context.Context, handler platform.InboundHandler) error
func (a *Adapter) Stop(ctx context.Context) error
```

When a OneBot event arrives:

```text
1. Apply event filters.
2. Render message text.
3. Map to platform.InboundMessage.
4. Call handler.HandleInbound(ctx, in, sink).
5. Sink maps PlatformGateway outbound events to OneBot actions.
```

### 11.2 PlatformService integration

Extend `PlatformService`:

```go
type PlatformService struct {
    gateway *PlatformGateway
    manager *platform.Manager
    onebotReverse *onebotv11.ReverseServer
    started []platform.Adapter
}

func (s *PlatformService) Configure(ctx context.Context, cfg config.PlatformsConfig) error
func (s *PlatformService) InstallHTTPRoutes(mux *http.ServeMux)
func (s *PlatformService) Start(ctx context.Context) error
func (s *PlatformService) Stop(ctx context.Context) error
```

`Configure`:

```text
if platforms.enabled=false: no-op
for each adapters[key]:
  if enabled && kind == onebot_v11:
    build onebotv11.Adapter
    manager.Register(key, adapter)
    if adapter uses ws_reverse: register with reverse server
```

`Start`:

```text
start ws_client adapters immediately
for ws_reverse adapters, Start marks adapter ready; actual connection arrives via HTTP route
```

`Stop`:

```text
stop all started adapters
close transports
```

### 11.3 Server integration

Update `BuildServer`:

```go
mux := http.NewServeMux()
if kernel.Services.Platforms != nil {
    if err := kernel.Services.Platforms.Configure(ctx, cfg.Platforms); err != nil { return nil, err }
    kernel.Services.Platforms.InstallHTTPRoutes(mux)
}
registerRoutes(...)
if kernel.Services.Platforms != nil {
    if err := kernel.Services.Platforms.Start(ctx); err != nil { return nil, err }
}
```

`Server` should hold `platforms *PlatformService` and stop it on shutdown.

---

## 12. Profiles

### 12.1 Profile interface

```go
type Profile struct {
    Name string
    DefaultInputFormat string
    DefaultOutputFormat string
    SupportsMarkdown bool
    SupportsJSONSegment bool
    RetcodeSuccess func(ActionResponse) bool
    NormalizeEvent func(*Event)
    NormalizeSegment func(Segment) Segment
}
```

MVP can keep this simpler; just avoid hardcoding NapCat-specific rules everywhere.

### 12.2 Generic profile

```text
strict OneBot v11
array message preferred
text output as array message segment
standard retcode/status handling
```

### 12.3 NapCat profile

MVP defaults:

```text
input_format=array
output_format=array
ignore_self_messages=true
supports json/markdown only as raw/placeholder for now
```

Do not use NapCat-specific APIs in MVP.

### 12.4 SnowLuma profile

MVP defaults:

```text
input_format=array
output_format=array
ignore_self_messages=true
supports json/markdown only as raw/placeholder for now
```

Do not depend on SnowLuma non-commercial source code. Profile is only compatibility metadata and behavior toggles.

---

## 13. Security and local deployment assumptions

This phase intentionally keeps security simple.

Rules:

```text
Default examples assume localhost deployment.
If access_token is configured, validate Authorization: Bearer token.
If access_token_env is configured and env var exists, use it.
Do not log token values.
Do not expose raw event JSON at info log level.
Do not process self messages.
Do not process group messages.
Do not download media.
```

Out of scope:

```text
TLS termination
IP allowlist
multi-tenant auth
admin UI for tokens
secret rotation
message signature verification
HTTP POST HMAC verification
```

---

## 14. Tests and acceptance criteria

### 14.1 Unit tests

Add tests for:

```text
onebotv11 config defaults and validation
profile selection: generic/napcat/snowluma
message array rendering: text + unsupported placeholders
CQ string fallback rendering
private event mapping
self message ignored
group message ignored
external_message_id composition
role mapping owner/admin/member
outbound private action builder
long text split
retcode error handling
echo timeout
```

### 14.2 Integration tests with fake PlatformGateway

Use a fake `platform.InboundHandler` and fake `ActionClient`:

```text
private message event → InboundMessage.SourceType=onebot
private message event → OriginScopePrivate
private message event → sink sends send_private_msg
/ sid command result emits one text message
/new command coalesces duplicate context_switched + command_result into one visible outgoing message
group event ignored and does not call handler
```

### 14.3 Forward WS fake server tests

Fake OneBot WS server:

```text
EmoAgent connects to ws://127.0.0.1:<port>/
server pushes private event
adapter calls PlatformGateway fake
sink sends send_private_msg action
server returns action response with matching echo
Call() completes
```

Also test:

```text
retcode failure
unknown echo ignored
disconnect fails pending call
reconnect attempts do not panic
```

### 14.4 Reverse WS fake client tests

Fake OneBot reverse client connects to EmoAgent route:

```text
GET /api/platforms/onebot/v11/qq-main/ws
X-Self-ID: 123456
X-Client-Role: Universal
```

Then:

```text
client sends private event
adapter handles event
adapter sends action request on same connection
client returns response
```

Unsupported role test:

```text
X-Client-Role: Event → close/reject with clear error in MVP
```

### 14.5 End-to-end-ish app test

Using `PlatformGateway` and fake Chat Engine if available:

```text
private OneBot `/sid` returns source_type=onebot, platform_id=qq, channel_type=private, session_id
private OneBot `/new` changes binding and sends one visible message
private normal message routes to Chat Engine and sends reply
receipt duplicate external message id does not call Chat Engine twice
```

### 14.6 Acceptance criteria

Feature is accepted when:

```text
1. `go test ./...` passes.
2. OneBot adapter can map private OneBot message events to PlatformGateway.
3. OneBot adapter ignores all group messages by default.
4. Forward WS universal client works against a fake OneBot WS server.
5. Reverse WS universal server works against a fake OneBot client.
6. `/sid`, `/new`, `/reset`, `/clear`, `/stop`, normal private messages work through the adapter in tests.
7. No media is downloaded or sent.
8. NapCat/SnowLuma are represented as profiles, not separate full adapters.
9. `source_type=onebot` is used for stable OriginKey.
10. Duplicate command events are coalesced for OneBot visible output.
```

---

## 15. Phased implementation plan

### Phase 1: Platform DTO and service lifecycle polish

Tasks:

```text
- Add InboundMessage.OriginScope and AcceptedReason.
- Make PlatformGateway use in.OriginScope.
- Optionally add HandleResult.Ignored.
- Extend PlatformService with Configure / InstallHTTPRoutes / Start / Stop.
- Wire PlatformService into BuildServer lifecycle.
```

Tests:

```text
- PlatformGateway still passes current tests.
- OriginScopePrivate and group_shared/group_user_unique origin tests.
- PlatformService no-op when platforms.enabled=false.
```

### Phase 2: OneBot v11 types, config, profiles, mapper

Tasks:

```text
- Add onebotv11 package skeleton.
- Implement Config decode/default/validate.
- Implement ProfileGeneric/ProfileNapCat/ProfileSnowLuma.
- Implement Event, Sender, Segment, ActionRequest/Response.
- Implement message rendering to safe text.
- Implement private event mapper.
- Implement group ignore behavior.
- Implement external message id composition.
```

Tests:

```text
- config/profile tests.
- event mapping tests.
- message rendering tests.
- group ignored tests.
```

### Phase 3: Action client and OneBot sink

Tasks:

```text
- Implement ActionClient abstraction.
- Implement send_private_msg / send_group_msg builders.
- Implement OneBotSink.Emit.
- Implement command event coalescing.
- Implement long text splitting.
```

Tests:

```text
- outbound event → action request tests.
- context_switched + command_result coalescing test.
- retcode failure test.
```

### Phase 4: Forward WS universal client transport

Tasks:

```text
- Implement ws_client transport.
- Implement echo pending map.
- Implement read loop demux event vs response.
- Implement reconnect and pending fail on disconnect.
- Implement optional Authorization bearer.
```

Tests:

```text
- fake OneBot WS server.
- event → action response flow.
- timeout/disconnect behavior.
```

### Phase 5: Simple reverse WS server transport

Tasks:

```text
- Implement ReverseServer HTTP handler.
- Register route in PlatformService.InstallHTTPRoutes.
- Accept only Universal role in MVP.
- Implement same event/action demux over inbound connection.
- Validate optional Authorization bearer if configured.
```

Tests:

```text
- fake reverse OneBot client.
- Universal role accepted.
- unsupported role rejected.
- action call returns via same connection.
```

### Phase 6: End-to-end configuration and docs

Tasks:

```text
- Add config examples for NapCat and SnowLuma profiles.
- Add docs explaining ws_client vs ws_reverse.
- Add docs warning that group intake is disabled in MVP.
- Add docs that media is placeholder-only.
```

Tests:

```text
- config validate tests for onebot_v11 adapters.
- `go test ./...`.
```

---

## 16. Non-goals for this implementation

Do not implement in this phase:

```text
Group message processing
Group activation by @ / reply / command
Media download/upload
Audio/video/image LLM processing
HTTP POST transport
HTTP API transport
OneBot v12
NapCat/SnowLuma extension APIs
Friend/group request handling
Notice/meta event handling beyond safe ignore
QQ group admin actions
Plugin-based platform ingress
Complex production security model
```

---

## 17. Suggested files to create or modify

Likely create:

```text
internal/platform/onebotv11/action.go
internal/platform/onebotv11/adapter.go
internal/platform/onebotv11/config.go
internal/platform/onebotv11/echo.go
internal/platform/onebotv11/event.go
internal/platform/onebotv11/message.go
internal/platform/onebotv11/mapper.go
internal/platform/onebotv11/profile.go
internal/platform/onebotv11/sink.go
internal/platform/onebotv11/transport.go
internal/platform/onebotv11/ws_client.go
internal/platform/onebotv11/ws_reverse.go
internal/platform/onebotv11/*_test.go
```

Likely modify:

```text
internal/platform/types.go
internal/app/platform_gateway.go
internal/app/platform_service.go
internal/app/server.go
internal/config/config.go
internal/config/config_test.go
```

Only modify storage if ignored receipts are implemented:

```text
internal/storage/platform_receipts.go
internal/storage/schema.go
```

---

## 18. Implementation notes for Codex

- Preserve the existing `PlatformGateway` responsibilities. Do not let OneBot call `Chat.Engine()` directly.
- Do not introduce a NapCat-specific full adapter.
- Do not process group messages in MVP.
- Do not populate `InboundMessage.Parts` from OneBot segments in MVP.
- Keep all OneBot JSON and transport details inside `internal/platform/onebotv11`.
- Keep source_type stable as `onebot`.
- Prefer tests with fake WS servers/clients over real NapCat/SnowLuma dependencies.
- Keep errors explicit and user-readable in logs, especially unsupported reverse WS role.
- Run `go test ./...` before final response.
