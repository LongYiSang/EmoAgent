package configcenter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/storage"
)

func ApplyRuntimeSettings(seed *config.Config, settings []storage.RuntimeSetting) (config.Config, []ConfigIssue) {
	var cfg config.Config
	if seed == nil {
		cfg = *config.DefaultConfig()
	} else {
		cfg = *seed
	}
	issues := make([]ConfigIssue, 0)
	for _, setting := range settings {
		if err := applyRuntimeSetting(&cfg, setting); err != nil {
			issues = append(issues, ConfigIssue{
				Path:     runtimeSettingPath(setting),
				Severity: "error",
				Message:  err.Error(),
			})
		}
	}
	return cfg, issues
}

func applyRuntimeSetting(cfg *config.Config, setting storage.RuntimeSetting) error {
	switch strings.TrimSpace(setting.Namespace) {
	case "system.server":
		return overlayJSONSetting(&cfg.Server, setting)
	case "chat":
		return overlayJSONSetting(&cfg.Chat, setting)
	case "memory":
		return overlayJSONSetting(&cfg.Memory, setting)
	case "memory.retrieval":
		return overlayJSONSetting(&cfg.Memory.Retrieval, setting)
	case "memory.extraction":
		return overlayJSONSetting(&cfg.Memory.Extraction, setting)
	case "memory.sidecar":
		return overlayJSONSetting(&cfg.Memory.Sidecar, setting)
	case "memory.provider_bindings":
		return overlayJSONSetting(&cfg.Memory.ProviderBindings, setting)
	case "memory.natural_memory":
		return overlayJSONSetting(&cfg.Memory.NaturalMemory, setting)
	case "memory.retention":
		return overlayJSONSetting(&cfg.Memory.Retention, setting)
	case "memory.forgetting_privacy":
		return overlayJSONSetting(&cfg.Memory.ForgettingPrivacy, setting)
	case "memory.agent_affect":
		return overlayJSONSetting(&cfg.Memory.AgentAffect, setting)
	case "agent_affect":
		return overlayJSONSetting(&cfg.AgentAffect, setting)
	default:
		return nil
	}
}

func overlayJSONSetting(target any, setting storage.RuntimeSetting) error {
	var value any
	decoder := json.NewDecoder(strings.NewReader(setting.ValueJSON))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("runtime setting value_json must be valid JSON: %w", err)
	}

	patch, ok := value.(map[string]any)
	if !ok || !wholeObjectRuntimeKey(setting.Key) {
		patch = map[string]any{setting.Key: value}
	}

	current, err := json.Marshal(target)
	if err != nil {
		return err
	}
	var base map[string]any
	if err := json.Unmarshal(current, &base); err != nil {
		return err
	}
	if base == nil {
		base = map[string]any{}
	}
	mergeJSONMap(base, patch)
	merged, err := json.Marshal(base)
	if err != nil {
		return err
	}
	decoder = json.NewDecoder(bytes.NewReader(merged))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("runtime setting does not match target schema: %w", err)
	}
	return nil
}

func wholeObjectRuntimeKey(key string) bool {
	switch strings.TrimSpace(key) {
	case "", "config", "value":
		return true
	default:
		return false
	}
}

func mergeJSONMap(dst map[string]any, patch map[string]any) {
	for key, value := range patch {
		if nestedPatch, ok := value.(map[string]any); ok {
			if nestedDst, ok := dst[key].(map[string]any); ok {
				mergeJSONMap(nestedDst, nestedPatch)
				continue
			}
		}
		dst[key] = value
	}
}

func runtimeSettingPath(setting storage.RuntimeSetting) string {
	namespace := strings.TrimSpace(setting.Namespace)
	key := strings.TrimSpace(setting.Key)
	if namespace == "" {
		return key
	}
	if key == "" || wholeObjectRuntimeKey(key) {
		return namespace
	}
	return namespace + "." + key
}
