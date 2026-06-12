package plugin

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const ManifestSchemaV02 = "emoagent.plugin.v0.2"

type AccessTier string

const (
	AccessTierRuntimeSafe AccessTier = "runtime_safe"
	AccessTierUserContext AccessTier = "user_context"
	AccessTierWorkspace   AccessTier = "workspace"
	AccessTierTrusted     AccessTier = "trusted"
)

type ManifestV2 struct {
	SchemaVersion   string              `json:"schema_version" yaml:"schema_version"`
	ID              string              `json:"id" yaml:"id"`
	Name            string              `json:"name" yaml:"name"`
	Version         string              `json:"version" yaml:"version"`
	EmoAgentVersion string              `json:"emoagent_version" yaml:"emoagent_version"`
	Runtime         ManifestV2Runtime   `json:"runtime" yaml:"runtime"`
	Access          ManifestV2Access    `json:"access" yaml:"access"`
	Hooks           []HookSpec          `json:"hooks" yaml:"hooks"`
	Provider        ManifestV2Provider  `json:"provider,omitempty" yaml:"provider"`
	Container       ManifestV2Container `json:"container,omitempty" yaml:"container"`
}

type ManifestV2Runtime struct {
	Kind  RuntimeKind `json:"kind" yaml:"kind"`
	Entry string      `json:"entry" yaml:"entry"`
}

type ManifestV2Access struct {
	Tier         AccessTier   `json:"tier" yaml:"tier"`
	Capabilities []Capability `json:"capabilities" yaml:"capabilities"`
}

type ManifestV2Provider struct {
	DefaultProviderID  string   `json:"default_provider_id" yaml:"default_provider_id"`
	DefaultModel       string   `json:"default_model" yaml:"default_model"`
	AllowedProviderIDs []string `json:"allowed_provider_ids,omitempty" yaml:"allowed_provider_ids"`
	AllowedModels      []string `json:"allowed_models,omitempty" yaml:"allowed_models"`
}

type ManifestV2Container struct {
	Workspace ManifestV2WorkspaceMount `json:"workspace,omitempty" yaml:"workspace"`
	Mounts    []ManifestV2Mount        `json:"mounts,omitempty" yaml:"mounts"`
}

type ManifestV2WorkspaceMount struct {
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Mode    string `json:"mode" yaml:"mode"`
}

type ManifestV2Mount struct {
	HostPath      string `json:"host_path" yaml:"host_path"`
	ContainerPath string `json:"container_path" yaml:"container_path"`
	Mode          string `json:"mode" yaml:"mode"`
}

func DecodeManifestV2YAML(data []byte, options ManifestValidationOptions) (ManifestV2, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var manifest ManifestV2
	if err := decoder.Decode(&manifest); err != nil {
		return ManifestV2{}, fmt.Errorf("decode manifest v0.2: %w", err)
	}
	if err := manifest.Validate(options); err != nil {
		return ManifestV2{}, err
	}
	return manifest, nil
}

func (m ManifestV2) Validate(options ManifestValidationOptions) error {
	if strings.TrimSpace(m.SchemaVersion) != ManifestSchemaV02 {
		return fmt.Errorf("schema_version must be %q", ManifestSchemaV02)
	}
	legacy := m.CompatManifest()
	if err := legacy.Validate(options); err != nil {
		return err
	}
	switch m.Runtime.Kind {
	case RuntimeBuiltin, RuntimeProcess, RuntimePythonProcess, RuntimeContainer:
	default:
		return fmt.Errorf("runtime.kind %q is unsupported", m.Runtime.Kind)
	}
	if m.Runtime.Kind == RuntimePythonProcess {
		if err := validateRelativeEntry(m.Runtime.Entry); err != nil {
			return fmt.Errorf("runtime.entry: %w", err)
		}
	}
	if !KnownAccessTier(m.Access.Tier) {
		return fmt.Errorf("access.tier %q is unsupported", m.Access.Tier)
	}
	if m.Runtime.Kind == RuntimeContainer {
		mode := strings.TrimSpace(m.Container.Workspace.Mode)
		if m.Container.Workspace.Enabled {
			switch mode {
			case "", "ro", "rw":
			default:
				return fmt.Errorf("container.workspace.mode must be ro or rw")
			}
		}
	}
	return nil
}

func (m ManifestV2) CompatManifest() Manifest {
	return Manifest{
		ID:              m.ID,
		Name:            m.Name,
		Version:         m.Version,
		Runtime:         m.Runtime.Kind,
		EmoAgentVersion: m.EmoAgentVersion,
		Capabilities:    append([]Capability(nil), m.Access.Capabilities...),
		Hooks:           append([]HookSpec(nil), m.Hooks...),
	}
}

func KnownAccessTier(tier AccessTier) bool {
	switch tier {
	case AccessTierRuntimeSafe, AccessTierUserContext, AccessTierWorkspace, AccessTierTrusted:
		return true
	default:
		return false
	}
}

func validateRelativeEntry(entry string) error {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return fmt.Errorf("is required")
	}
	if filepath.IsAbs(entry) || strings.HasPrefix(entry, "/") || strings.HasPrefix(entry, `\`) {
		return fmt.Errorf("must be relative")
	}
	cleaned := filepath.ToSlash(filepath.Clean(entry))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") {
		return fmt.Errorf("must not contain ..")
	}
	if cleaned != filepath.ToSlash(entry) {
		return fmt.Errorf("must be clean")
	}
	return nil
}
