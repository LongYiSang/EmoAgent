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

// Upsert adds or replaces a tool registration. It is intended for host-owned
// runtime reconfiguration paths, not plugin registration.
func (r *Registry) Upsert(spec Spec, handler Handler) error {
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

	r.specs[spec.Name] = spec
	r.funcs[spec.Name] = handler
	return nil
}

// Unregister removes a tool registration if present.
func (r *Registry) Unregister(name string) {
	if r == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.specs, name)
	delete(r.funcs, name)
}

// UnregisterPlugin removes all tools owned by the given plugin.
func (r *Registry) UnregisterPlugin(pluginID string) {
	if r == nil {
		return
	}
	pluginID = strings.TrimSpace(pluginID)
	if pluginID == "" {
		return
	}
	legacyPrefix := "plugin." + pluginID + "."
	safePrefix := "plugin_" + safeToolNameSegment(pluginID) + "_"

	r.mu.Lock()
	defer r.mu.Unlock()

	for name, spec := range r.specs {
		if (spec.Source.Kind == ToolSourcePlugin && spec.Source.ProducerID == pluginID) ||
			strings.HasPrefix(name, legacyPrefix) ||
			strings.HasPrefix(name, safePrefix) {
			delete(r.specs, name)
			delete(r.funcs, name)
		}
	}
}

func safeToolNameSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "tool"
	}
	var b strings.Builder
	b.Grow(len(value))
	lastUnderscore := false
	for _, r := range value {
		allowed := (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '_' ||
			r == '-'
		if allowed {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "tool"
	}
	return out
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
