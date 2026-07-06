package plugin

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/longyisang/emoagent/internal/tool"
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
	SchemaVersion   string                 `json:"schema_version" yaml:"schema_version"`
	ID              string                 `json:"id" yaml:"id"`
	Name            string                 `json:"name" yaml:"name"`
	Version         string                 `json:"version" yaml:"version"`
	EmoAgentVersion string                 `json:"emoagent_version" yaml:"emoagent_version"`
	Runtime         ManifestV2Runtime      `json:"runtime" yaml:"runtime"`
	Access          ManifestV2Access       `json:"access" yaml:"access"`
	Hooks           []HookSpec             `json:"hooks" yaml:"hooks"`
	Provider        ManifestV2Provider     `json:"provider,omitempty" yaml:"provider"`
	Container       ManifestV2Container    `json:"container,omitempty" yaml:"container"`
	Settings        *ManifestV2Settings    `json:"settings,omitempty" yaml:"settings"`
	Commands        []ManifestV2Command    `json:"commands,omitempty" yaml:"commands"`
	ToolDefaults    ManifestV2ToolDefaults `json:"tool_defaults,omitempty" yaml:"tool_defaults"`
}

type ManifestV2ToolDefaults struct {
	RoutingClass tool.RoutingClass `json:"routing_class,omitempty" yaml:"routing_class"`
}

type ManifestV2Command struct {
	Name       string   `json:"name" yaml:"name"`
	RootName   string   `json:"root_name,omitempty" yaml:"root_name"`
	Aliases    []string `json:"aliases,omitempty" yaml:"aliases"`
	Summary    string   `json:"summary" yaml:"summary"`
	Usage      string   `json:"usage,omitempty" yaml:"usage"`
	Permission string   `json:"permission,omitempty" yaml:"permission"`
	Handler    string   `json:"handler" yaml:"handler"`
	OutputMode string   `json:"output_mode,omitempty" yaml:"output_mode"`
	TimeoutMS  int      `json:"timeout_ms,omitempty" yaml:"timeout_ms"`
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

type ManifestV2Settings struct {
	Key    string               `json:"key,omitempty" yaml:"key"`
	Schema PluginSettingsSchema `json:"schema,omitempty" yaml:"schema"`
}

type PluginSettingsSchema struct {
	Type       string                               `json:"type" yaml:"type"`
	Required   []string                             `json:"required,omitempty" yaml:"required"`
	Properties map[string]PluginSettingsFieldSchema `json:"properties,omitempty" yaml:"properties"`
}

type PluginSettingsFieldSchema struct {
	Type        string            `json:"type" yaml:"type"`
	Title       string            `json:"title,omitempty" yaml:"title"`
	Description string            `json:"description,omitempty" yaml:"description"`
	Enum        []string          `json:"enum,omitempty" yaml:"enum"`
	EnumTitles  map[string]string `json:"enum_titles,omitempty" yaml:"enum_titles"`
	Secret      bool              `json:"secret,omitempty" yaml:"secret"`
	Default     any               `json:"default,omitempty" yaml:"default"`
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
	case RuntimeBuiltin, RuntimeProcess, RuntimePythonProcess, RuntimeManagedPythonProcess, RuntimeContainer:
	default:
		return fmt.Errorf("runtime.kind %q is unsupported", m.Runtime.Kind)
	}
	if m.Runtime.Kind == RuntimePythonProcess || m.Runtime.Kind == RuntimeManagedPythonProcess {
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
	if m.Settings != nil {
		if err := m.Settings.Validate(); err != nil {
			return err
		}
	}
	if m.ToolDefaults.RoutingClass != "" && !tool.KnownRoutingClass(m.ToolDefaults.RoutingClass) {
		return fmt.Errorf("tool_defaults.routing_class must be work or casual")
	}
	if err := m.validateCommands(options); err != nil {
		return err
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

var manifestCommandNamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`)

func (m ManifestV2) validateCommands(options ManifestValidationOptions) error {
	if len(m.Commands) == 0 {
		return nil
	}
	if !manifestHasCapability(m.Access.Capabilities, CapabilityCommandRegister) {
		return fmt.Errorf("commands require %s capability", CapabilityCommandRegister)
	}
	for i, command := range m.Commands {
		if !manifestCommandNamePattern.MatchString(strings.TrimSpace(command.Name)) {
			return fmt.Errorf("commands[%d].name is invalid", i)
		}
		if strings.TrimSpace(command.Handler) == "" {
			return fmt.Errorf("commands[%d].handler is required", i)
		}
		maxTimeout := 1000
		if options.MaxTimeoutMS > 0 {
			maxTimeout = options.MaxTimeoutMS
		}
		if command.TimeoutMS < 0 || command.TimeoutMS > maxTimeout {
			return fmt.Errorf("commands[%d].timeout_ms must be between 0 and %d", i, maxTimeout)
		}
		rootName := strings.TrimSpace(command.RootName)
		if rootName != "" {
			if !manifestCommandNamePattern.MatchString(rootName) {
				return fmt.Errorf("commands[%d].root_name is invalid", i)
			}
			if manifestReservedCommandRoot(rootName) {
				return fmt.Errorf("commands[%d].root_name %q is reserved", i, rootName)
			}
		}
		for j, alias := range command.Aliases {
			alias = strings.TrimSpace(alias)
			if !manifestCommandNamePattern.MatchString(alias) {
				return fmt.Errorf("commands[%d].aliases[%d] is invalid", i, j)
			}
			if manifestReservedCommandRoot(alias) {
				return fmt.Errorf("commands[%d].aliases[%d] %q is reserved", i, j, alias)
			}
		}
		outputMode := strings.TrimSpace(command.OutputMode)
		if outputMode == "" {
			outputMode = "direct"
		}
		switch outputMode {
		case "direct":
		case "llm_synthesize":
			if !manifestHasCapability(m.Access.Capabilities, CapabilityProviderGenerate) {
				return fmt.Errorf("commands[%d].output_mode llm_synthesize requires %s capability", i, CapabilityProviderGenerate)
			}
		default:
			return fmt.Errorf("commands[%d].output_mode %q is unsupported", i, outputMode)
		}
	}
	return nil
}

func manifestHasCapability(capabilities []Capability, target Capability) bool {
	for _, capability := range capabilities {
		if capability == target {
			return true
		}
	}
	return false
}

func manifestReservedCommandRoot(name string) bool {
	switch strings.ToLower(strings.TrimPrefix(strings.TrimSpace(name), "/")) {
	case "help", "sid", "new", "switch", "reset", "clear", "compact", "forget", "stop", "set", "unset", "plugin", "plugins", "provider", "model", "memory", "config", "admin":
		return true
	default:
		return false
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
