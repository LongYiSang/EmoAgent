# 2026-06-06 MemoryCore API Adapter Migration

- Added `internal/memoryhost.CoreClient` as the host-side MemoryCore port.
- Isolated MemoryCore capability grouping in `internal/memoryhost/core_client.go`.
- Routed Host, Bridge, async extraction, natural memory, and tests through the port.
- Removed the unused `internal/memory` compatibility wrapper.
- Preserved async extraction, prompt memory retrieval, degraded mirror sync, and manual forget confirmation behavior.
- Verified with the requested memoryhost tests, full Go tests, build, and static API surface searches.
