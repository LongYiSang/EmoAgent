package plugin

import (
	"strings"
	"testing"
)

func TestDecodeManifestV2YAMLValidPythonProcess(t *testing.T) {
	data := []byte(`
schema_version: emoagent.plugin.v0.2
id: com.example.echo
name: Echo Plugin
version: 0.1.0
emoagent_version: ">=0.2.0"
runtime:
  kind: python_process
  entry: main.py
access:
  tier: runtime_safe
  capabilities:
    - turn.read
    - provider.generate
hooks:
  - name: after_turn_end
    mode: observe
    failure_policy: fail_open
    priority: 100
    timeout_ms: 200
`)

	manifest, err := DecodeManifestV2YAML(data, ManifestValidationOptions{MaxTimeoutMS: 1000})
	if err != nil {
		t.Fatalf("DecodeManifestV2YAML: %v", err)
	}
	if manifest.Runtime.Kind != RuntimePythonProcess {
		t.Fatalf("runtime.kind = %q, want python_process", manifest.Runtime.Kind)
	}
	compat := manifest.CompatManifest()
	if compat.Runtime != RuntimePythonProcess || len(compat.Capabilities) != 2 {
		t.Fatalf("compat manifest = %#v", compat)
	}
}

func TestDecodeManifestV2YAMLValidManagedPythonProcess(t *testing.T) {
	data := []byte(`
schema_version: emoagent.plugin.v0.2
id: com.example.echo
name: Echo Plugin
version: 0.1.0
emoagent_version: ">=0.2.0"
runtime:
  kind: managed_python_process
  entry: main.py
access:
  tier: runtime_safe
  capabilities:
    - turn.read
hooks:
  - name: after_turn_end
    mode: observe
    failure_policy: fail_open
    priority: 100
    timeout_ms: 200
`)

	manifest, err := DecodeManifestV2YAML(data, ManifestValidationOptions{MaxTimeoutMS: 1000})
	if err != nil {
		t.Fatalf("DecodeManifestV2YAML: %v", err)
	}
	if manifest.Runtime.Kind != RuntimeManagedPythonProcess {
		t.Fatalf("runtime.kind = %q, want managed_python_process", manifest.Runtime.Kind)
	}
}

func TestDecodeManifestV2YAMLRejectsUnknownField(t *testing.T) {
	data := []byte(`
schema_version: emoagent.plugin.v0.2
id: com.example.echo
name: Echo Plugin
version: 0.1.0
emoagent_version: ">=0.2.0"
runtime:
  kind: python_process
  entry: main.py
access:
  tier: runtime_safe
  capabilities:
    - turn.read
raw_prompt_debug: true
`)

	_, err := DecodeManifestV2YAML(data, ManifestValidationOptions{MaxTimeoutMS: 1000})
	if err == nil || !strings.Contains(err.Error(), "field raw_prompt_debug not found") {
		t.Fatalf("DecodeManifestV2YAML error = %v, want unknown field", err)
	}
}

func TestDecodeManifestV2YAMLValidSettingsSchema(t *testing.T) {
	data := []byte(`
schema_version: emoagent.plugin.v0.2
id: com.example.settings
name: Settings Plugin
version: 0.1.0
emoagent_version: ">=0.2.0"
runtime:
  kind: managed_python_process
  entry: main.py
access:
  tier: runtime_safe
  capabilities:
    - plugin.kv
settings:
  key: settings
  schema:
    type: object
    required:
      - api_key
    properties:
      api_key:
        type: string
        title: API Key
        secret: true
      mode:
        type: string
        enum:
          - base
          - all
        enum_titles:
          base: Base
          all: All
        default: base
      retries:
        type: integer
        default: 1
      enabled:
        type: boolean
        default: true
hooks: []
`)

	manifest, err := DecodeManifestV2YAML(data, ManifestValidationOptions{MaxTimeoutMS: 1000})
	if err != nil {
		t.Fatalf("DecodeManifestV2YAML: %v", err)
	}
	if manifest.Settings == nil || manifest.Settings.Schema.Type != "object" {
		t.Fatalf("settings schema = %#v", manifest.Settings)
	}
	if !manifest.Settings.Schema.Properties["api_key"].Secret {
		t.Fatalf("api_key secret = false, want true")
	}
}

func TestDecodeManifestV2YAMLRejectsInvalidSettingsSchema(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "unsupported key",
			yaml: `
settings:
  key: profile
  schema:
    type: object
    properties:
      api_key:
        type: string
`,
			want: "settings.key",
		},
		{
			name: "non object root",
			yaml: `
settings:
  schema:
    type: array
    properties:
      api_key:
        type: string
`,
			want: "settings.schema.type",
		},
		{
			name: "nested object field",
			yaml: `
settings:
  schema:
    type: object
    properties:
      nested:
        type: object
`,
			want: "settings.schema.properties.nested.type",
		},
		{
			name: "required unknown property",
			yaml: `
settings:
  schema:
    type: object
    required:
      - missing
    properties:
      api_key:
        type: string
`,
			want: "settings.schema.required",
		},
		{
			name: "default outside enum",
			yaml: `
settings:
  schema:
    type: object
    properties:
      mode:
        type: string
        enum:
          - base
        default: all
`,
			want: "settings.schema.properties.mode.default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte(`
schema_version: emoagent.plugin.v0.2
id: com.example.settings
name: Settings Plugin
version: 0.1.0
emoagent_version: ">=0.2.0"
runtime:
  kind: managed_python_process
  entry: main.py
access:
  tier: runtime_safe
  capabilities:
    - plugin.kv
hooks: []
` + tt.yaml)

			_, err := DecodeManifestV2YAML(data, ManifestValidationOptions{MaxTimeoutMS: 1000})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("DecodeManifestV2YAML error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestDecodeManifestV2YAMLRejectsInvalidRuntimeEntryAndCapability(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "absolute entry",
			yaml: `
schema_version: emoagent.plugin.v0.2
id: com.example.echo
name: Echo Plugin
version: 0.1.0
emoagent_version: ">=0.2.0"
runtime:
  kind: python_process
  entry: /tmp/main.py
access:
  tier: runtime_safe
  capabilities:
    - turn.read
`,
			want: "runtime.entry",
		},
		{
			name: "unknown capability",
			yaml: `
schema_version: emoagent.plugin.v0.2
id: com.example.echo
name: Echo Plugin
version: 0.1.0
emoagent_version: ">=0.2.0"
runtime:
  kind: python_process
  entry: main.py
access:
  tier: runtime_safe
  capabilities:
    - memory.raw
`,
			want: "unknown capability",
		},
		{
			name: "unknown access tier",
			yaml: `
schema_version: emoagent.plugin.v0.2
id: com.example.echo
name: Echo Plugin
version: 0.1.0
emoagent_version: ">=0.2.0"
runtime:
  kind: python_process
  entry: main.py
access:
  tier: root
  capabilities:
    - turn.read
`,
			want: "access.tier",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeManifestV2YAML([]byte(tt.yaml), ManifestValidationOptions{MaxTimeoutMS: 1000})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("DecodeManifestV2YAML error = %v, want %q", err, tt.want)
			}
		})
	}
}
