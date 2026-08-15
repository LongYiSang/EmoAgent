# EmoAgent 多模态 Media Input 改造 Spec

> Status: Implementation Spec Draft  
> Target repo: `LongYiSang/EmoAgent`  
> Primary goal: 将 EmoAgent 改造成可以接受图片等多模态输入，同时保持历史上下文、Memory pipeline、供应商适配和模型刷新逻辑的边界清晰、低成本、可测试。  
> Initial modality: image. Future-compatible: audio / video / file.

---

## 1. Outcome

EmoAgent 应支持用户在聊天消息中上传图片，并在当前轮模型调用中按供应商要求发送图片；历史消息重放、Memory 抽取、摘要、日志和检索管线中不重发图片字节，而是渲染为安全占位符，例如 `[used image]`。

最终效果：

```text
Current user turn with image:
  send text + actual image payload/provider ref to model, if model/policy allows.

Historical message containing image:
  send original text + [used image] placeholder only.

Memory extraction input:
  send original text + [used image] placeholder only; never send image bytes, base64, caption, OCR, or media ids.

Model refresh:
  fetch available model IDs from provider, then enrich capabilities from provider metadata, built-in presets, manual overrides, and optional probes.
```

---

## 2. Confirmed product decisions

1. **History does not replay image bytes.**  
   A media part is sent as actual image data only when it belongs to the current user turn, or when the current user turn explicitly reactivates a prior image reference. Ordinary history rendering must collapse images to `[used image]`.

2. **Memory pipeline does not process image content in MVP.**  
   Image bytes, generated captions, OCR, visual tags, thumbnails, and provider file IDs must not enter Memory Extraction / Consolidation / Retrieval. User text attached to an image remains eligible for Memory pipeline.

3. **Model multimodal capability is explicit and layered.**  
   Provider model-list endpoints are not uniformly rich enough to discover all input modalities, image transports, formats, and size limits. `刷新模型` should fetch model IDs, then merge capabilities from local presets, provider metadata where available, manual overrides, and optional probe results.

---

## 3. Non-goals for this pass

- Do not implement image captioning, OCR, image embeddings, visual memory extraction, or multimodal emotion recognition.
- Do not send images embedded in historical chat turns by default.
- Do not store base64 in SQLite, logs, Memory events, prompt debug dumps, or analytics.
- Do not change EmoAgent-MemoryCore semantics; Memory continues to receive text-only episodes.
- Do not require every provider to support vision. Unsupported/unknown behavior is controlled by policy.

---

## 4. Current repo observations to account for

Current `internal/llm/types.go` already has structured `ContentBlocks`, but the current block model is text/tool-oriented:

```go
type ContentBlock struct {
    Type string // text, tool_use, tool_result
    Text string
    ID string
    Name string
    Input map[string]any
    Content any
    IsError bool
}
```

`Message` also keeps `Content string` for backwards compatibility. The implementation comment says provider adapters must explicitly map internal blocks to wire formats. Keep that design: extend internal parts, then map in adapters.

Current `ModelInfo` only carries `ID`; provider discovery only lists models and cannot persist modality/transport metadata. Add a capability enrichment layer rather than replacing model discovery.

---

## 5. Core architecture

```text
UI / API upload
  ↓
MediaStore: validate + hash + local asset metadata
  ↓
Chat message parts: text + image references
  ↓
Episode renderer: text + [used image]
  ↓
Memory pipeline: text-only

Chat request build
  ↓
History renderer: historical images → [used image]
  ↓
Current-turn renderer: eligible current images stay as MediaPart
  ↓
MediaSendPlanner: capability + policy + size/transport planning
  ↓
Provider adapter: OpenAI / Kimi / Anthropic wire mapping
  ↓
Model call
```

The most important separation is:

```text
ContentPart / MediaPart = EmoAgent internal message representation.
Provider payload = generated at send time only.
Memory episode text = sanitized rendering only.
```

---

## 6. Data model

### 6.1 `media_assets`

```sql
CREATE TABLE IF NOT EXISTS media_assets (
    id                  TEXT PRIMARY KEY,
    sha256              TEXT NOT NULL,
    kind                TEXT NOT NULL CHECK (kind IN ('image','audio','video','file')),
    mime_type           TEXT NOT NULL,
    original_filename   TEXT,
    file_ext            TEXT,
    byte_size           INTEGER NOT NULL,
    width               INTEGER,
    height              INTEGER,
    duration_ms         INTEGER,

    storage_backend     TEXT NOT NULL DEFAULT 'local',
    storage_uri         TEXT NOT NULL,
    thumbnail_uri       TEXT,

    created_by_role     TEXT NOT NULL CHECK (created_by_role IN ('user','assistant','system','tool')),
    created_at          TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,

    visibility_status   TEXT NOT NULL DEFAULT 'visible'
        CHECK (visibility_status IN ('visible','hidden','forgotten','purged')),
    scan_status         TEXT NOT NULL DEFAULT 'pending'
        CHECK (scan_status IN ('pending','clean','rejected','failed')),
    retention_policy    TEXT NOT NULL DEFAULT 'chat_asset',
    expires_at          TEXT,
    reference_count     INTEGER NOT NULL DEFAULT 0,

    purged_at           TEXT,
    purge_reason        TEXT,

    UNIQUE(sha256, byte_size)
);
```

### 6.2 `message_parts`

```sql
CREATE TABLE IF NOT EXISTS message_parts (
    id              TEXT PRIMARY KEY,
    session_id      TEXT NOT NULL,
    message_id      TEXT NOT NULL,
    role            TEXT NOT NULL CHECK (role IN ('user','assistant','system','tool')),
    ordinal         INTEGER NOT NULL,

    part_type       TEXT NOT NULL CHECK (part_type IN ('text','image','audio','video','file','tool_use','tool_result')),
    text_content    TEXT,
    media_asset_id  TEXT,

    memory_render_policy TEXT NOT NULL DEFAULT 'placeholder_only'
        CHECK (memory_render_policy IN ('placeholder_only','text_only','never')),
    history_render_policy TEXT NOT NULL DEFAULT 'placeholder_only'
        CHECK (history_render_policy IN ('placeholder_only','resend_if_reactivated','never')),

    created_at      TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(media_asset_id) REFERENCES media_assets(id)
);

CREATE INDEX IF NOT EXISTS idx_message_parts_message
    ON message_parts(session_id, message_id, ordinal);
CREATE INDEX IF NOT EXISTS idx_message_parts_media
    ON message_parts(media_asset_id);
```

### 6.3 `provider_media_refs`

Optional in MVP, useful once Kimi `ms://`, Anthropic Files API, or OpenAI Files API reuse is implemented.

```sql
CREATE TABLE IF NOT EXISTS provider_media_refs (
    id              TEXT PRIMARY KEY,
    media_asset_id  TEXT NOT NULL,
    provider_id     TEXT NOT NULL,
    model_scope     TEXT,
    ref_type        TEXT NOT NULL, -- openai_file_id | anthropic_file_id | kimi_ms
    remote_ref      TEXT NOT NULL,
    created_at      TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at      TEXT,
    last_used_at    TEXT,
    delete_status   TEXT NOT NULL DEFAULT 'active'
        CHECK (delete_status IN ('active','delete_queued','deleted','delete_failed')),
    metadata_json   TEXT,
    FOREIGN KEY(media_asset_id) REFERENCES media_assets(id)
);
```

### 6.4 `message_media_deliveries`

Used to enforce “actual image only on current/first eligible send; history uses placeholder.”

```sql
CREATE TABLE IF NOT EXISTS message_media_deliveries (
    id              TEXT PRIMARY KEY,
    message_id      TEXT NOT NULL,
    part_id         TEXT NOT NULL,
    media_asset_id  TEXT NOT NULL,
    provider_id     TEXT NOT NULL,
    model_id        TEXT NOT NULL,
    turn_id         TEXT NOT NULL,
    delivery_scope  TEXT NOT NULL CHECK (delivery_scope IN ('current_turn','reactivated_reference','history_placeholder')),
    transport       TEXT NOT NULL CHECK (transport IN ('data_url','base64','remote_url','provider_file','placeholder')),
    status          TEXT NOT NULL CHECK (status IN ('prepared','sent','failed','omitted')),
    byte_size_sent  INTEGER,
    error_message   TEXT,
    created_at      TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_media_deliveries_lookup
    ON message_media_deliveries(media_asset_id, provider_id, model_id, created_at DESC);
```

### 6.5 `llm_model_capabilities`

```sql
CREATE TABLE IF NOT EXISTS llm_model_capabilities (
    id                  TEXT PRIMARY KEY,
    provider_id          TEXT NOT NULL,
    model_id             TEXT NOT NULL,

    input_modalities_json  TEXT NOT NULL DEFAULT '["text"]',
    output_modalities_json TEXT NOT NULL DEFAULT '["text"]',
    image_transports_json  TEXT NOT NULL DEFAULT '[]',
    image_formats_json     TEXT NOT NULL DEFAULT '[]',

    max_images_per_request INTEGER,
    max_image_bytes        INTEGER,
    max_request_bytes      INTEGER,
    max_long_edge_pixels   INTEGER,

    supports_vision_tools      INTEGER NOT NULL DEFAULT 0,
    supports_vision_streaming  INTEGER NOT NULL DEFAULT 0,
    supports_vision_json_mode  INTEGER NOT NULL DEFAULT 0,

    param_policy_json      TEXT,
    capability_source      TEXT NOT NULL DEFAULT 'unknown'
        CHECK (capability_source IN ('unknown','provider_metadata','provider_docs_preset','manual_override','probe_passed','probe_failed','merged')),
    confidence             REAL NOT NULL DEFAULT 0.0 CHECK (confidence >= 0 AND confidence <= 1),
    last_refreshed_at      TEXT,
    last_verified_at       TEXT,
    raw_provider_json      TEXT,

    created_at             TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at             TEXT,
    UNIQUE(provider_id, model_id)
);
```

---

## 7. Go DTOs and interfaces

### 7.1 Content parts

```go
type ContentPartType string

const (
    PartText       ContentPartType = "text"
    PartImage      ContentPartType = "image"
    PartAudio      ContentPartType = "audio"
    PartVideo      ContentPartType = "video"
    PartFile       ContentPartType = "file"
    PartToolUse    ContentPartType = "tool_use"
    PartToolResult ContentPartType = "tool_result"
)

type ContentPart struct {
    Type  ContentPartType `json:"type"`
    Text  string          `json:"text,omitempty"`
    Media *MediaPart      `json:"media,omitempty"`

    ID      string         `json:"id,omitempty"`
    Name    string         `json:"name,omitempty"`
    Input   map[string]any `json:"input,omitempty"`
    Content any            `json:"content,omitempty"`
    IsError bool           `json:"is_error,omitempty"`
}

type MediaPart struct {
    MediaAssetID string `json:"media_asset_id"`
    Kind         string `json:"kind"`
    MimeType     string `json:"mime_type"`
    Detail       string `json:"detail,omitempty"` // auto | low | high
    AltText      string `json:"alt_text,omitempty"`
}
```

Keep the old `Message.Content string` field for compatibility, but all new APIs should be parts-first.

### 7.2 Render modes

```go
type RenderMode string

const (
    RenderForCurrentLLMTurn RenderMode = "current_llm_turn"
    RenderForHistory        RenderMode = "history"
    RenderForMemory         RenderMode = "memory"
    RenderForSummary        RenderMode = "summary"
)

type RenderPolicy struct {
    CurrentTurnID       string
    ReactivatedMediaIDs map[string]bool
    Placeholder         string // default: "[used image]"
}
```

Rules:

```text
RenderForCurrentLLMTurn:
  - current turn image → keep as MediaPart.
  - explicitly reactivated prior image → keep as MediaPart.
  - ordinary historical image → [used image].

RenderForHistory:
  - all images → [used image].

RenderForMemory:
  - all images → [used image].
  - do not include media_asset_id, base64, caption, OCR, thumbnail, provider ref.

RenderForSummary:
  - all images → [used image].
```

### 7.3 Media store

```go
type MediaStore interface {
    Put(ctx context.Context, r io.Reader, meta UploadMediaMeta) (*MediaAsset, error)
    Get(ctx context.Context, mediaAssetID string) (*MediaAsset, error)
    Open(ctx context.Context, mediaAssetID string) (io.ReadCloser, *MediaAsset, error)
    MarkPurged(ctx context.Context, mediaAssetID string, reason string) error
}
```

MVP validation:

```text
- MIME sniffing, not extension trust.
- image/png and image/jpeg initially.
- Max bytes configurable.
- Max pixels configurable.
- No base64 persisted.
- EXIF removal optional but recommended before provider send.
```

### 7.4 Capability resolver

```go
type ModelCapabilityResolver interface {
    Resolve(ctx context.Context, providerID, modelID string) (*ModelCapabilities, error)
    RefreshProvider(ctx context.Context, providerID string, opts RefreshOptions) ([]ModelCapabilities, error)
}

type ModelCapabilities struct {
    ProviderID string
    ModelID    string

    InputModalities  []string
    OutputModalities []string
    ImageTransports  []string
    ImageFormats     []string

    MaxImagesPerRequest int
    MaxImageBytes       int64
    MaxRequestBytes     int64
    MaxLongEdgePixels   int64

    SupportsVisionTools     bool
    SupportsVisionStreaming bool
    SupportsVisionJSONMode  bool

    CapabilitySource string
    Confidence       float64
}
```

Capability source precedence:

```text
manual_override
  > probe_passed / probe_failed
  > provider_metadata
  > provider_docs_preset
  > unknown
```

### 7.5 Media send planner

```go
type MediaSendPlanner interface {
    Prepare(ctx context.Context, req MediaPrepareRequest) (*MediaPrepareResult, error)
}

type MediaPrepareRequest struct {
    ProviderID string
    ModelID    string
    Messages   []Message
    CurrentTurnID string
    ReactivatedMediaIDs map[string]bool
    Policy MediaPolicy
}

type MediaPolicy struct {
    Enabled bool
    UnknownModelPolicy string      // reject | optimistic_send | strip_media | force_send
    UnsupportedModelPolicy string  // reject | strip_media | force_send
    PreferredTransports []string   // provider_file | data_url | base64 | remote_url
    MaxRequestBytes int64
}
```

`MediaSendPlanner` must produce provider-neutral prepared parts and record `message_media_deliveries` for actual sends/omissions.

---

## 8. Provider mapping

### 8.1 OpenAI Chat Completions compatible

Internal:

```text
text + image MediaPart
```

Wire:

```json
{
  "role": "user",
  "content": [
    {"type": "text", "text": "帮我看看这张图"},
    {"type": "image_url", "image_url": {"url": "data:image/jpeg;base64,...", "detail": "auto"}}
  ]
}
```

### 8.2 OpenAI Responses API future path

Wire:

```json
{
  "role": "user",
  "content": [
    {"type": "input_text", "text": "帮我看看这张图"},
    {"type": "input_image", "image_url": "data:image/jpeg;base64,..."}
  ]
}
```

Keep this behind protocol-specific adapter code; do not mix it into Chat Completions mapping.

### 8.3 Kimi / Moonshot

MVP can map to OpenAI-compatible `image_url` with data URL. Future optimization can upload once and use `ms://<file-id>`.

Provider rule:

```text
Do not send ordinary remote image URLs to Kimi unless provider docs change.
Use data URL or ms:// provider file reference.
```

### 8.4 Anthropic / Claude

Wire:

```json
{
  "role": "user",
  "content": [
    {
      "type": "image",
      "source": {
        "type": "base64",
        "media_type": "image/png",
        "data": "..."
      }
    },
    {"type": "text", "text": "请描述图片内容"}
  ]
}
```

Future transports: URL source and Files API source.

---

## 9. Memory exclusion contract

Add a single renderer used by Episode Writer and Memory Extraction request assembly:

```go
func RenderForMemory(messages []Message) []MemoryEpisodeInput
```

Requirements:

```text
- All image/audio/video/file parts become placeholders.
- Placeholder must not include local path, provider ref, base64, sha256, file name, or media_asset_id.
- User text adjacent to images remains unchanged and may be extracted.
- No caption/OCR/vision-derived text is generated in MVP.
- Existing Memory Extraction invariants remain unchanged: episodes are evidence; facts must be grounded in text source_episode_ids.
```

Example:

```text
User parts:
  text: "记住这张图里的字不算，我只是问你这是什么花"
  image: med_abc

Memory episode content:
  "记住这张图里的字不算，我只是问你这是什么花\n[used image]"
```

The image itself must not produce a fact. The text may still produce a fact only if ordinary Memory policy allows it.

---

## 10. Model refresh integration

Current `刷新模型` should become:

```text
Refresh provider model list
  ↓
Normalize model IDs
  ↓
Read provider metadata if available
  ↓
Apply built-in capability presets by provider/model pattern
  ↓
Apply manual overrides from DB/config
  ↓
Optional lightweight probe for unknown/claimed vision models
  ↓
Persist llm_model_capabilities
  ↓
UI/API expose model modality badges
```

Preset examples:

```yaml
capability_presets:
  - provider_id: moonshot
    model_pattern: "kimi-k2.6*"
    input_modalities: ["text", "image", "video"]
    image_transports: ["data_url", "kimi_ms"]
    image_formats: ["image/png", "image/jpeg"]
    max_request_bytes: 104857600
    capability_source: provider_docs_preset

  - provider_id: anthropic
    model_pattern: "claude-*"
    input_modalities: ["text", "image"]
    image_transports: ["base64", "remote_url", "anthropic_file_id"]
    capability_source: provider_docs_preset

  - provider_id: openai
    model_pattern: "gpt-4.1*|gpt-4o*|gpt-5*"
    input_modalities: ["text", "image"]
    image_transports: ["data_url", "remote_url", "openai_file_id"]
    capability_source: provider_docs_preset
```

Unknown model behavior:

```text
Default production: reject current-turn media with clear model capability error.
Developer mode: optimistic_send for unknown models, record probe/result.
Force mode: force_send_media=true bypasses resolver, only for debugging.
```

---

## 11. API changes

### 11.1 Upload media

```http
POST /api/media
Content-Type: multipart/form-data

file=<image>
```

Response:

```json
{
  "media_asset_id": "med_01J...",
  "kind": "image",
  "mime_type": "image/png",
  "byte_size": 123456,
  "width": 1024,
  "height": 768
}
```

### 11.2 Send chat message

```json
{
  "session_id": "sess_01J...",
  "parts": [
    {"type": "text", "text": "帮我看看这张图"},
    {"type": "image", "media": {"media_asset_id": "med_01J...", "kind": "image", "mime_type": "image/png"}}
  ]
}
```

Compatibility:

```text
Old {content:"..."} requests map to parts=[{type:"text"}].
```

---

## 12. Tests and verification

### 12.1 Unit tests

```text
- RenderForHistory replaces all image parts with [used image].
- RenderForMemory replaces all image parts with [used image] and strips asset IDs.
- RenderForCurrentLLMTurn keeps current turn image but collapses historical images.
- Reactivated previous image can be sent only when current turn references it.
- MediaStore never stores base64 in SQLite.
- MediaSendPlanner rejects unsupported models by default.
- Unknown model policy optimistic_send can attempt send and record result.
- Capability merge precedence: manual > probe > metadata > preset > unknown.
- OpenAI/Kimi/Anthropic adapters map prepared image parts to correct provider payload.
```

### 12.2 Integration tests

```text
- Send text + image to vision-capable mocked OpenAI-compatible provider.
- Verify request payload contains image only for current turn.
- Continue conversation; verify previous image appears as [used image], no base64.
- Trigger memory extraction; verify input contains [used image] and no media ID/base64/path.
- Refresh models; verify capabilities are persisted and UI/API can read image modality.
```

### 12.3 Suggested commands

```bash
go test ./...
go test ./internal/llm/... ./internal/media/... ./internal/chat/...
```

Adjust package paths after inspecting the repo.

---

## 13. Implementation phases

### Phase 1: Message parts + media storage

- Add `internal/media` with MediaStore.
- Add DB migrations for `media_assets`, `message_parts`, `message_media_deliveries`.
- Add upload API.
- Make chat send API accept `parts` while keeping `content` compatibility.

### Phase 2: Rendering contract

- Add renderers for current LLM turn, history, memory, summary.
- Wire Episode Writer / Memory Extraction to use `RenderForMemory`.
- Add tests proving no image bytes enter Memory pipeline.

### Phase 3: Provider mapping

- Extend LLM internal message structs to support image parts.
- Add `MediaSendPlanner`.
- Implement OpenAI-compatible data URL mapping.
- Implement Anthropic image block mapping.
- Add Kimi data URL mapping; keep `ms://` provider file as optional follow-up.

### Phase 4: Capability registry + model refresh

- Extend `ModelInfo` and provider refresh path with capability enrichment.
- Add presets + DB table.
- Add optional probe mode.
- Add UI/API model modality badge.

### Phase 5: Hardening

- Add size estimation and downscale hooks.
- Add provider file ref cache.
- Add purge cleanup for media assets and provider refs.
- Add reactivated-reference resolver for “刚才那张图”.

---

## 14. Acceptance criteria

A build is complete when:

```text
1. A user can upload an image and send a chat message containing text + image.
2. A compatible vision model receives the image in the current turn provider payload.
3. The next model call renders that same historical image as [used image], not base64/provider ref.
4. Memory extraction input contains only text and [used image], never image bytes/IDs/paths/captions/OCR.
5. Unsupported models do not receive images unless explicit optimistic/force policy is enabled.
6. Refresh models persists per-model capability records with source/confidence.
7. Tests prove no base64 appears in history render, memory render, SQLite text columns, or logs used by Memory pipeline.
```
