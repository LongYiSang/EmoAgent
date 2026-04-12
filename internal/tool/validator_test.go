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
