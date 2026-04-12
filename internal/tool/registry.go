package tool

import (
	"fmt"
	"sync"

	"github.com/longyisang/emoagent/internal/llm"
)

// Registry maps tool names to handlers and specs.
type Registry struct {
	mu    sync.RWMutex
	specs map[string]Spec
	funcs map[string]Handler
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		specs: make(map[string]Spec),
		funcs: make(map[string]Handler),
	}
}

// Register adds a tool to the registry. Panics on duplicate name to catch
// registration errors at startup.
func (r *Registry) Register(spec Spec, handler Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.specs[spec.Name]; exists {
		panic(fmt.Sprintf("tool %q already registered", spec.Name))
	}
	r.specs[spec.Name] = spec
	r.funcs[spec.Name] = handler
}

// Get returns the handler for a tool name.
func (r *Registry) Get(name string) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.funcs[name]
	return h, ok
}

// GetSpec returns the spec for a tool name.
func (r *Registry) GetSpec(name string) (Spec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.specs[name]
	return s, ok
}

// ForScope returns ToolDefs for all tools matching the given scope.
// ScopeEmotion returns tools with scope=emotion or scope=both.
// ScopeWork returns tools with scope=work or scope=both.
func (r *Registry) ForScope(scope Scope) []llm.ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var defs []llm.ToolDef
	for _, s := range r.specs {
		if s.Scope == scope || s.Scope == ScopeBoth {
			defs = append(defs, s.ToToolDef())
		}
	}
	return defs
}

// Specs returns all registered specs.
func (r *Registry) Specs() []Spec {
	r.mu.RLock()
	defer r.mu.RUnlock()

	specs := make([]Spec, 0, len(r.specs))
	for _, s := range r.specs {
		specs = append(specs, s)
	}
	return specs
}
