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
