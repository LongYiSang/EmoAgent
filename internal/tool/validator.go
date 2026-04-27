package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
)

// MinimalSchemaValidator validates the small JSON Schema subset used by current tools.
type MinimalSchemaValidator struct{}

// Validate checks input against a restricted object-schema subset:
// type, enum, properties, required, additionalProperties, and primitive or
// array property types.
func (MinimalSchemaValidator) Validate(schema json.RawMessage, input json.RawMessage) error {
	var schemaValue any
	if err := decodeJSON(schema, &schemaValue, false); err != nil {
		return fmt.Errorf("invalid schema: %w", err)
	}

	var inputValue any
	if len(bytes.TrimSpace(input)) == 0 {
		input = json.RawMessage(`{}`)
	}
	if err := decodeJSON(input, &inputValue, true); err != nil {
		return fmt.Errorf("invalid input: %w", err)
	}

	return validateValue(schemaValue, inputValue, "$")
}

func decodeJSON(raw json.RawMessage, dst any, useNumber bool) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if useNumber {
		dec.UseNumber()
	}
	return dec.Decode(dst)
}

func validateValue(schema any, value any, path string) error {
	schemaMap, ok := schema.(map[string]any)
	if !ok {
		return fmt.Errorf("%s: schema must be object", path)
	}

	schemaType, _ := schemaMap["type"].(string)
	switch schemaType {
	case "":
		// Continue so enum-only schemas still validate.
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: expected object", path)
		}
		if err := validateObject(schemaMap, obj, path); err != nil {
			return err
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s: expected string", path)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s: expected boolean", path)
		}
	case "number":
		if _, ok := value.(json.Number); !ok {
			return fmt.Errorf("%s: expected number", path)
		}
	case "integer":
		n, ok := value.(json.Number)
		if !ok {
			return fmt.Errorf("%s: expected integer", path)
		}
		f, err := n.Float64()
		if err != nil || math.Trunc(f) != f {
			return fmt.Errorf("%s: expected integer", path)
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s: expected array", path)
		}
		if err := validateArray(schemaMap, items, path); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%s: unsupported schema type %q", path, schemaType)
	}

	if err := validateEnum(schemaMap, value, path); err != nil {
		return err
	}

	return nil
}

func validateEnum(schema map[string]any, value any, path string) error {
	rawEnum, ok := schema["enum"]
	if !ok {
		return nil
	}

	enumValues, ok := rawEnum.([]any)
	if !ok {
		return fmt.Errorf("%s: enum must be an array", path)
	}
	for _, allowed := range enumValues {
		if reflect.DeepEqual(allowed, value) {
			return nil
		}
	}
	return fmt.Errorf("%s: value %#v not in enum", path, value)
}

func validateArray(schema map[string]any, items []any, path string) error {
	if rawMinItems, ok := schema["minItems"]; ok {
		minItems, ok := rawMinItems.(float64)
		if !ok || math.Trunc(minItems) != minItems || minItems < 0 {
			return fmt.Errorf("%s: minItems must be a non-negative integer", path)
		}
		if len(items) < int(minItems) {
			return fmt.Errorf("%s: expected at least %d items", path, int(minItems))
		}
	}

	itemSchema, ok := schema["items"]
	if !ok {
		return nil
	}

	for i, item := range items {
		if err := validateValue(itemSchema, item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
			return err
		}
	}
	return nil
}

func validateObject(schema map[string]any, obj map[string]any, path string) error {
	properties := map[string]any{}
	if rawProps, ok := schema["properties"].(map[string]any); ok {
		properties = rawProps
	}

	if required, ok := schema["required"].([]any); ok {
		for _, item := range required {
			name, ok := item.(string)
			if !ok {
				return fmt.Errorf("%s: required field names must be strings", path)
			}
			if _, exists := obj[name]; !exists {
				return fmt.Errorf("%s.%s: required property missing", path, name)
			}
		}
	}

	allowAdditional := true
	if raw, ok := schema["additionalProperties"].(bool); ok {
		allowAdditional = raw
	}

	for key, value := range obj {
		propSchema, exists := properties[key]
		if !exists {
			if !allowAdditional {
				return fmt.Errorf("%s.%s: unexpected property", path, key)
			}
			continue
		}
		if err := validateValue(propSchema, value, path+"."+key); err != nil {
			return err
		}
	}

	return nil
}
