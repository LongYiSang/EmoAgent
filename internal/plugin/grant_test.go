package plugin

import (
	"strings"
	"testing"
)

func TestValidateUserGrantForManifestRejectsUnknownFieldsAndTrailingJSON(t *testing.T) {
	manifest := facadeTestManifest([]Capability{CapabilityTurnRead})
	tests := []struct {
		name  string
		grant string
		want  string
	}{
		{
			name:  "unknown trust field",
			grant: `{"tier":"runtime_safe","capabilities":["turn.read"],"trust":"official"}`,
			want:  "unknown field",
		},
		{
			name:  "trailing json",
			grant: `{"tier":"runtime_safe"} {"capabilities":["turn.read"]}`,
			want:  "trailing JSON",
		},
		{
			name:  "unsupported tier",
			grant: `{"tier":"root","capabilities":["turn.read"]}`,
			want:  "unsupported",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidateUserGrantForManifest(tt.grant, manifest)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateUserGrantForManifest error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateUserGrantForManifestAllowsCapabilityOnlyGrant(t *testing.T) {
	manifest := facadeTestManifest([]Capability{CapabilityTurnRead})

	grant, err := ValidateUserGrantForManifest(`{"capabilities":["turn.read"]}`, manifest)
	if err != nil {
		t.Fatalf("ValidateUserGrantForManifest: %v", err)
	}
	if len(grant.Capabilities) != 1 || grant.Capabilities[0] != CapabilityTurnRead || grant.Tier != "" {
		t.Fatalf("grant = %#v, want capability-only grant", grant)
	}
}
