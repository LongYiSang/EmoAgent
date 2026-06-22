package resultv2

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPhase0ToolResultEnvelopeContractCompile(t *testing.T) {
	env := ToolResultEnvelope{
		SchemaVersion: SchemaVersion,
		CallID:        "call-1",
		Status:        StatusOK,
		Content: []ContentItem{{
			Type: "json",
			Data: json.RawMessage(`{"ok":true}`),
		}},
		StructuredContent: json.RawMessage(`{"ok":true}`),
		Provenance: Provenance{
			ProducerKind:   ProducerBuiltin,
			ProducerID:     "builtin",
			ToolName:       "read_file",
			InvocationID:   "inv-1",
			InputHash:      "sha256:input",
			RuntimeKind:    RuntimeHost,
			GeneratedAt:    time.Unix(1, 0).UTC(),
			GrantIDs:       []string{"grant-1"},
			SandboxProfile: "workspace",
		},
		Labels: ContentLabels{
			Executor:             ExecutorHostBuiltin,
			Origin:               OriginWorkspaceFile,
			Integrity:            IntegrityHashVerified,
			InstructionAuthority: InstructionDataOnly,
			Sensitivity:          SensitivityPrivate,
			Freshness:            FreshnessLive,
		},
	}
	if env.Labels.InstructionAuthority != InstructionDataOnly || env.Provenance.InputHash == "" {
		t.Fatalf("result envelope contract mismatch: %#v", env)
	}
}
