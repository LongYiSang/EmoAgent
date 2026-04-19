package tool

import (
	"encoding/json"
	"testing"
)

func TestMinimalSchemaValidator_ValidateAllowsEmptyObject(t *testing.T) {
	v := MinimalSchemaValidator{}
	schema := json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)

	if err := v.Validate(schema, json.RawMessage(`{}`)); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestMinimalSchemaValidator_ValidateRejectsAdditionalProperties(t *testing.T) {
	v := MinimalSchemaValidator{}
	schema := json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)

	if err := v.Validate(schema, json.RawMessage(`{"timezone":"UTC"}`)); err == nil {
		t.Fatal("Validate should reject unexpected properties")
	}
}

func TestMinimalSchemaValidator_ValidateRequiredAndTypes(t *testing.T) {
	v := MinimalSchemaValidator{}
	schema := json.RawMessage(`{
		"type":"object",
		"properties":{
			"name":{"type":"string"},
			"count":{"type":"integer"},
			"enabled":{"type":"boolean"}
		},
		"required":["name","count","enabled"],
		"additionalProperties":false
	}`)

	if err := v.Validate(schema, json.RawMessage(`{"name":"ok","count":2,"enabled":true}`)); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if err := v.Validate(schema, json.RawMessage(`{"name":"ok","count":"2","enabled":true}`)); err == nil {
		t.Fatal("Validate should reject invalid integer type")
	}
	if err := v.Validate(schema, json.RawMessage(`{"name":"ok","count":2}`)); err == nil {
		t.Fatal("Validate should reject missing required fields")
	}
}

func TestMinimalSchemaValidator_ValidateArrayItems(t *testing.T) {
	v := MinimalSchemaValidator{}
	schema := json.RawMessage(`{
		"type":"object",
		"properties":{
			"constraints":{"type":"array","items":{"type":"string"}}
		},
		"additionalProperties":false
	}`)

	if err := v.Validate(schema, json.RawMessage(`{"constraints":["a","b"]}`)); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if err := v.Validate(schema, json.RawMessage(`{"constraints":["a",1]}`)); err == nil {
		t.Fatal("Validate should reject invalid array item type")
	}
}

func TestMinimalSchemaValidator_ValidateEnum(t *testing.T) {
	v := MinimalSchemaValidator{}
	schema := json.RawMessage(`{
		"type":"object",
		"properties":{
			"permission_scope":{"type":"string","enum":["read-only"]}
		},
		"required":["permission_scope"],
		"additionalProperties":false
	}`)

	if err := v.Validate(schema, json.RawMessage(`{"permission_scope":"read-only"}`)); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if err := v.Validate(schema, json.RawMessage(`{"permission_scope":"workspace-write"}`)); err == nil {
		t.Fatal("Validate should reject enum mismatch")
	}
}

func TestMinimalSchemaValidator_ValidateNestedObject(t *testing.T) {
	v := MinimalSchemaValidator{}
	schema := json.RawMessage(`{
		"type":"object",
		"properties":{
			"metadata":{
				"type":"object",
				"properties":{
					"summary":{"type":"string"},
					"labels":{"type":"array","items":{"type":"string"}}
				},
				"additionalProperties":false
			}
		},
		"additionalProperties":false
	}`)

	if err := v.Validate(schema, json.RawMessage(`{"metadata":{"summary":"ok","labels":["short"]}}`)); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if err := v.Validate(schema, json.RawMessage(`{"metadata":{"unknown":"x"}}`)); err == nil {
		t.Fatal("Validate should reject unexpected nested properties")
	}
}
