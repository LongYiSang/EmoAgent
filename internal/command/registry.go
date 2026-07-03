package command

import (
	"fmt"
	"strings"
	"sync"
)

type Registry struct {
	mu      sync.RWMutex
	byID    map[string]CommandDescriptor
	rootIDs map[string]string
}

func NewRegistry() *Registry {
	return &Registry{
		byID:    make(map[string]CommandDescriptor),
		rootIDs: make(map[string]string),
	}
}

func (r *Registry) TryRegister(descriptor CommandDescriptor) error {
	if r == nil {
		return fmt.Errorf("command registry is nil")
	}
	descriptor.ID = strings.TrimSpace(descriptor.ID)
	descriptor.Name = normalizeRoot(descriptor.Name)
	if descriptor.ID == "" {
		return fmt.Errorf("command id is required")
	}
	if descriptor.Name == "" {
		return fmt.Errorf("command name is required")
	}
	roots := descriptorRoots(descriptor)
	if descriptor.ProviderKind == CommandProviderPlugin {
		for _, root := range roots {
			if IsReservedRoot(root) {
				return fmt.Errorf("command root %q is reserved", root)
			}
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.byID[descriptor.ID]; exists {
		return fmt.Errorf("command id %q already registered", descriptor.ID)
	}
	for _, root := range roots {
		if existingID, exists := r.rootIDs[root]; exists {
			return fmt.Errorf("command root %q already registered by %q", root, existingID)
		}
	}
	for _, root := range roots {
		r.rootIDs[root] = descriptor.ID
	}
	r.byID[descriptor.ID] = cloneDescriptor(descriptor)
	return nil
}

func (r *Registry) Lookup(root string) (CommandDescriptor, bool) {
	if r == nil {
		return CommandDescriptor{}, false
	}
	root = normalizeRoot(root)
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.rootIDs[root]
	if !ok {
		return CommandDescriptor{}, false
	}
	descriptor, ok := r.byID[id]
	if !ok {
		return CommandDescriptor{}, false
	}
	return cloneDescriptor(descriptor), true
}

func (r *Registry) Get(id string) (CommandDescriptor, bool) {
	if r == nil {
		return CommandDescriptor{}, false
	}
	id = strings.TrimSpace(id)
	r.mu.RLock()
	defer r.mu.RUnlock()
	descriptor, ok := r.byID[id]
	return cloneDescriptor(descriptor), ok
}

func (r *Registry) UnregisterPlugin(pluginID string) int {
	if r == nil {
		return 0
	}
	pluginID = strings.TrimSpace(pluginID)
	if pluginID == "" {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	removed := 0
	for id, descriptor := range r.byID {
		if descriptor.ProviderKind != CommandProviderPlugin || strings.TrimSpace(descriptor.PluginID) != pluginID {
			continue
		}
		for _, root := range descriptorRoots(descriptor) {
			if existingID, exists := r.rootIDs[root]; exists && existingID == id {
				delete(r.rootIDs, root)
			}
		}
		delete(r.byID, id)
		removed++
	}
	return removed
}

func (r *Registry) UpdateRoots(id string, name string, aliases []string) error {
	if r == nil {
		return fmt.Errorf("command registry is nil")
	}
	id = strings.TrimSpace(id)
	name = normalizeRoot(name)
	if id == "" {
		return fmt.Errorf("command id is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	descriptor, exists := r.byID[id]
	if !exists {
		return fmt.Errorf("command id %q is not registered", id)
	}
	if name == "" {
		name = descriptor.Name
	}
	next := cloneDescriptor(descriptor)
	next.Name = name
	next.Aliases = append([]string(nil), aliases...)
	roots := descriptorRoots(next)
	if next.ProviderKind == CommandProviderPlugin {
		for _, root := range roots {
			if IsReservedRoot(root) {
				return fmt.Errorf("command root %q is reserved", root)
			}
		}
	}
	for _, root := range roots {
		if existingID, exists := r.rootIDs[root]; exists && existingID != id {
			return fmt.Errorf("command root %q already registered by %q", root, existingID)
		}
	}
	for _, root := range descriptorRoots(descriptor) {
		if existingID, exists := r.rootIDs[root]; exists && existingID == id {
			delete(r.rootIDs, root)
		}
	}
	for _, root := range roots {
		r.rootIDs[root] = id
	}
	r.byID[id] = cloneDescriptor(next)
	return nil
}

func (r *Registry) Descriptors() []CommandDescriptor {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]CommandDescriptor, 0, len(r.byID))
	for _, descriptor := range r.byID {
		out = append(out, cloneDescriptor(descriptor))
	}
	return out
}

func descriptorRoots(descriptor CommandDescriptor) []string {
	seen := make(map[string]struct{}, 1+len(descriptor.Aliases))
	roots := make([]string, 0, 1+len(descriptor.Aliases))
	for _, root := range append([]string{descriptor.Name}, descriptor.Aliases...) {
		root = normalizeRoot(root)
		if root == "" {
			continue
		}
		if _, exists := seen[root]; exists {
			continue
		}
		seen[root] = struct{}{}
		roots = append(roots, root)
	}
	return roots
}

func cloneDescriptor(descriptor CommandDescriptor) CommandDescriptor {
	descriptor.Aliases = append([]string(nil), descriptor.Aliases...)
	descriptor.Capabilities = append([]string(nil), descriptor.Capabilities...)
	descriptor.Args = append([]CommandArgSpec(nil), descriptor.Args...)
	return descriptor
}
