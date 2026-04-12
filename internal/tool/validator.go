package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
)

// MinimalSchemaValidator validates the small JSON Schema subset used by current tools.
type MinimalSchemaValidator struct{}

// Validate checks input against a restricted object-schema subset:
// type, properties, required, additionalProperties, and primitive property types.
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
		return nil
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: expected object", path)
		}
		return validateObject(schemaMap, obj, path)
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
	default:
		return fmt.Errorf("%s: unsupported schema type %q", path, schemaType)
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
