package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

const PluginSettingsKey = "settings"

func (s *ManifestV2Settings) EffectiveKey() string {
	if s == nil || strings.TrimSpace(s.Key) == "" {
		return PluginSettingsKey
	}
	return strings.TrimSpace(s.Key)
}

func (s *ManifestV2Settings) HasSchema() bool {
	return s != nil && !s.Schema.IsZero()
}

func (s *ManifestV2Settings) Validate() error {
	if s == nil {
		return nil
	}
	if s.EffectiveKey() != PluginSettingsKey {
		return fmt.Errorf("settings.key must be %q", PluginSettingsKey)
	}
	if s.Schema.IsZero() {
		return fmt.Errorf("settings.schema is required")
	}
	if err := s.Schema.ValidateDefinition(); err != nil {
		return err
	}
	return nil
}

func (s PluginSettingsSchema) IsZero() bool {
	return strings.TrimSpace(s.Type) == "" && len(s.Required) == 0 && len(s.Properties) == 0
}

func (s PluginSettingsSchema) ValidateDefinition() error {
	if strings.TrimSpace(s.Type) != "object" {
		return fmt.Errorf("settings.schema.type must be object")
	}
	if len(s.Properties) == 0 {
		return fmt.Errorf("settings.schema.properties is required")
	}
	required := map[string]struct{}{}
	for _, name := range s.Required {
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("settings.schema.required contains empty property")
		}
		if _, ok := s.Properties[name]; !ok {
			return fmt.Errorf("settings.schema.required property %q is not declared", name)
		}
		required[name] = struct{}{}
	}
	for name, field := range s.Properties {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(name) != name {
			return fmt.Errorf("settings.schema.properties contains invalid property name %q", name)
		}
		path := "settings.schema.properties." + name
		if err := field.validateDefinition(path, name, requiredContains(required, name)); err != nil {
			return err
		}
	}
	return nil
}

func (s PluginSettingsSchema) CleanValue(raw json.RawMessage) (json.RawMessage, error) {
	if s.IsZero() {
		if len(raw) == 0 {
			return json.RawMessage("{}"), nil
		}
		return append(json.RawMessage(nil), raw...), nil
	}
	if err := s.ValidateDefinition(); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	var input map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&input); err != nil {
		return nil, fmt.Errorf("settings value must be valid JSON object: %w", err)
	}
	if input == nil {
		return nil, fmt.Errorf("settings value must be object")
	}
	required := map[string]struct{}{}
	for _, name := range s.Required {
		required[strings.TrimSpace(name)] = struct{}{}
	}
	output := map[string]any{}
	for name, field := range s.Properties {
		value, ok := input[name]
		if !ok || value == nil {
			if requiredContains(required, name) {
				return nil, fmt.Errorf("settings.%s is required", name)
			}
			continue
		}
		clean, err := field.cleanValue(name, value, requiredContains(required, name))
		if err != nil {
			return nil, err
		}
		output[name] = clean
	}
	cleaned, err := json.Marshal(output)
	if err != nil {
		return nil, err
	}
	return cleaned, nil
}

func (f PluginSettingsFieldSchema) validateDefinition(path, name string, required bool) error {
	fieldType := strings.TrimSpace(f.Type)
	switch fieldType {
	case "string", "number", "integer", "boolean":
	default:
		return fmt.Errorf("%s.type %q is unsupported", path, f.Type)
	}
	if len(f.Enum) > 0 && fieldType != "string" {
		return fmt.Errorf("%s.enum is only supported for string fields", path)
	}
	enum := map[string]struct{}{}
	for _, value := range f.Enum {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s.enum contains empty value", path)
		}
		if _, exists := enum[value]; exists {
			return fmt.Errorf("%s.enum contains duplicate value %q", path, value)
		}
		enum[value] = struct{}{}
	}
	if len(f.EnumTitles) > 0 && len(enum) == 0 {
		return fmt.Errorf("%s.enum_titles requires enum", path)
	}
	for value := range f.EnumTitles {
		if _, ok := enum[value]; !ok {
			return fmt.Errorf("%s.enum_titles contains unknown enum value %q", path, value)
		}
	}
	if f.Default != nil {
		if _, err := f.cleanValue(name, f.Default, required); err != nil {
			return fmt.Errorf("%s.default: %w", path, err)
		}
	}
	return nil
}

func (f PluginSettingsFieldSchema) cleanValue(name string, value any, required bool) (any, error) {
	switch strings.TrimSpace(f.Type) {
	case "string":
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("settings.%s must be string", name)
		}
		if required && strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf("settings.%s is required", name)
		}
		if len(f.Enum) > 0 && !stringIn(text, f.Enum) {
			return nil, fmt.Errorf("settings.%s must match enum", name)
		}
		return text, nil
	case "number":
		number, err := normalizeNumber(value, false)
		if err != nil {
			return nil, fmt.Errorf("settings.%s must be number", name)
		}
		return number, nil
	case "integer":
		number, err := normalizeNumber(value, true)
		if err != nil {
			return nil, fmt.Errorf("settings.%s must be integer", name)
		}
		return number, nil
	case "boolean":
		boolean, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf("settings.%s must be boolean", name)
		}
		return boolean, nil
	default:
		return nil, fmt.Errorf("settings.%s has unsupported schema type", name)
	}
}

func normalizeNumber(value any, integer bool) (any, error) {
	switch v := value.(type) {
	case json.Number:
		if integer {
			if i, err := v.Int64(); err == nil {
				return i, nil
			}
			f, err := v.Float64()
			if err != nil || !finiteWholeNumber(f) {
				return nil, fmt.Errorf("not integer")
			}
			return int64(f), nil
		}
		f, err := v.Float64()
		if err != nil {
			return nil, err
		}
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("not finite")
		}
		return v, nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("not finite")
		}
		if integer {
			if !finiteWholeNumber(v) {
				return nil, fmt.Errorf("not integer")
			}
			return int64(v), nil
		}
		return v, nil
	case float32:
		return normalizeNumber(float64(v), integer)
	case int:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	default:
		return nil, fmt.Errorf("not number")
	}
}

func finiteWholeNumber(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && math.Trunc(value) == value
}

func requiredContains(required map[string]struct{}, name string) bool {
	_, ok := required[name]
	return ok
}

func stringIn(value string, candidates []string) bool {
	for _, candidate := range candidates {
		if value == candidate {
			return true
		}
	}
	return false
}
