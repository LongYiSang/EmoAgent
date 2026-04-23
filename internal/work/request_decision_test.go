package work

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRequestDecisionDescriptionAndSchemaMatchBatchAContract(t *testing.T) {
	spec := NewRequestDecisionTool()

	if !strings.Contains(spec.Description, "auto / emotion_judgment / human_confirmation") {
		t.Fatalf("description = %q, want allowed LLM categories", spec.Description)
	}
	if !strings.Contains(spec.Description, "never use tool_approval") {
		t.Fatalf("description = %q, want tool_approval runtime-only guidance", spec.Description)
	}
	if !strings.Contains(spec.Description, "never try to request destructive permission via request_decision") {
		t.Fatalf("description = %q, want destructive permission escalation guidance", spec.Description)
	}
	if !strings.Contains(spec.Description, "use emotion_judgment only when Emotion should decide using relationship, tone, preference, or emotional context") {
		t.Fatalf("description = %q, want emotion_judgment ownership guidance", spec.Description)
	}

	var schema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(spec.Parameters, &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}

	if _, ok := schema.Properties["risk_level"]; ok {
		t.Fatalf("schema properties = %#v, should not expose risk_level", schema.Properties)
	}

	wantEnum := []string{"auto", "emotion_judgment", "human_confirmation"}
	gotEnum := schema.Properties["category"].Enum
	if len(gotEnum) != len(wantEnum) {
		t.Fatalf("category enum = %#v, want %#v", gotEnum, wantEnum)
	}
	for i := range wantEnum {
		if gotEnum[i] != wantEnum[i] {
			t.Fatalf("category enum = %#v, want %#v", gotEnum, wantEnum)
		}
	}

	for _, name := range schema.Required {
		if name == "risk_level" {
			t.Fatalf("required = %#v, should not require risk_level", schema.Required)
		}
	}
}
