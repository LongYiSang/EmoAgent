package tool

import (
	"fmt"
	"strings"
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

// TryRegister adds a tool and returns an error on invalid or duplicate
// registrations. Plugin code must use this path so a bad plugin cannot panic
// the host or overwrite built-in tools.
func (r *Registry) TryRegister(spec Spec, handler Handler) error {
	if r == nil {
		return fmt.Errorf("tool registry is nil")
	}
	spec.Name = strings.TrimSpace(spec.Name)
	if spec.Name == "" {
		return fmt.Errorf("tool name is required")
	}
	if handler == nil {
		return fmt.Errorf("tool %q handler is required", spec.Name)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.specs[spec.Name]; exists {
		return fmt.Errorf("tool %q already registered", spec.Name)
	}
	r.specs[spec.Name] = spec
	r.funcs[spec.Name] = handler
	return nil
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
