# Multimodal Media Input

- Added a parts-first image input path: `POST /api/media` uploads PNG/JPEG into local media storage, and chat WS messages can send text plus image parts.
- Added an internal `MediaPart` pipeline. Current-turn eligible images are opened by the media planner and passed to provider adapters; historical, summary, and Memory renders collapse media to `[used image]`.
- Added model-level capability persistence in `llm_model_capabilities`. Model refresh still updates model IDs, then enriches capabilities from provider metadata, built-in presets, and manual overrides.
- Provider wire formats remain adapter-owned: OpenAI-compatible/Kimi use `image_url` data URLs, Anthropic uses base64 image source blocks.
- MemoryCore remains text-only. EmoAgent writes Memory episodes and retrieval queries as text plus `[used image]`, with no media bytes, paths, provider refs, or media IDs.
- Added `message_media_deliveries` audit writes for current-turn sends and historical placeholder omissions.
- Added migration v26 trigger guards to reject image base64/data-URL leakage in chat, message parts, media storage URI fields, Memory extraction job JSON, and prompt/debug text surfaces.
- Admin provider model lists now surface compact modality/capability badges from persisted model capability records.

Verification run during implementation:

- `go test ./internal/llm`
- `go test ./internal/media`
- `go test ./internal/storage`
- `go test ./internal/chat`
- `go test ./internal/app`
- `go test ./internal/web`
- `go test ./...`
- `npm --prefix web run typecheck`
- `npm --prefix web run build`
