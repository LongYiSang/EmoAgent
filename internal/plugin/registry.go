package plugin

import (
	"bytes"
	"fmt"
	"sort"
	"sync"

	"gopkg.in/yaml.v3"
)

type PluginRegistry struct {
	mu        sync.RWMutex
	manifests map[string]Manifest
}

func NewPluginRegistry() *PluginRegistry {
	return &PluginRegistry{manifests: map[string]Manifest{}}
}

func DecodeManifestYAML(data []byte, options ManifestValidationOptions) (Manifest, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	if err := manifest.Validate(options); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (r *PluginRegistry) Register(manifest Manifest, options ManifestValidationOptions) error {
	if r == nil {
		return fmt.Errorf("plugin registry is nil")
	}
	if err := manifest.Validate(options); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.manifests[manifest.ID]; exists {
		return fmt.Errorf("plugin %q is already registered", manifest.ID)
	}
	r.manifests[manifest.ID] = manifest
	return nil
}

func (r *PluginRegistry) Get(id string) (Manifest, bool) {
	if r == nil {
		return Manifest{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	manifest, ok := r.manifests[id]
	return manifest, ok
}

func (r *PluginRegistry) List() []Manifest {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	manifests := make([]Manifest, 0, len(r.manifests))
	for _, manifest := range r.manifests {
		manifests = append(manifests, manifest)
	}
	sort.SliceStable(manifests, func(i, j int) bool {
		return manifests[i].ID < manifests[j].ID
	})
	return manifests
}
