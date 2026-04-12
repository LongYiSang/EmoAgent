package builtin

import "github.com/longyisang/emoagent/internal/tool"

// RegisterAll registers all built-in tools with the given registry.
// Called once during App initialization.
func RegisterAll(registry *tool.Registry) {
	registry.Register(GetCurrentTimeSpec, GetCurrentTimeHandler)
}
