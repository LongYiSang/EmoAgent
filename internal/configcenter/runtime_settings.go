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
		return applyOverlayJSONSetting(&cfg.Server, setting)
	case "chat":
		next, err := overlayJSONSetting(cfg.Chat, setting)
		if err != nil {
			return err
		}
		cfg.Chat = config.NormalizeChatConfig(next)
		if err := cfg.Chat.PromptRouter.Validate(); err != nil {
			return fmt.Errorf("prompt_router.%w", err)
		}
		return nil
	case "memory":
		return applyOverlayJSONSetting(&cfg.Memory, setting)
	case "memory.retrieval":
		return applyOverlayJSONSetting(&cfg.Memory.Retrieval, setting)
	case "memory.extraction":
		return applyOverlayJSONSetting(&cfg.Memory.Extraction, setting)
	case "memory.sidecar":
		return applyOverlayJSONSetting(&cfg.Memory.Sidecar, setting)
	case "memory.provider_bindings":
		return applyOverlayJSONSetting(&cfg.Memory.ProviderBindings, setting)
	case "memory.natural_memory":
		return applyOverlayJSONSetting(&cfg.Memory.NaturalMemory, setting)
	case "memory.retention":
		return applyOverlayJSONSetting(&cfg.Memory.Retention, setting)
	case "memory.forgetting_privacy":
		return applyOverlayJSONSetting(&cfg.Memory.ForgettingPrivacy, setting)
	case "memory.agent_affect":
		return applyOverlayJSONSetting(&cfg.Memory.AgentAffect, setting)
	case "agent_affect":
		return applyOverlayJSONSetting(&cfg.AgentAffect, setting)
	case "python_toolchain":
		return applyOverlayJSONSetting(&cfg.PythonToolchain, setting)
	case "websearch":
		return applyOverlayJSONSetting(&cfg.WebSearch, setting)
	case "platforms":
		return applyOverlayJSONSetting(&cfg.Platforms, setting)
	default:
		return nil
	}
}

func applyOverlayJSONSetting[T any](target *T, setting storage.RuntimeSetting) error {
	next, err := overlayJSONSetting(*target, setting)
	if err != nil {
		return err
	}
	*target = next
	return nil
}

func overlayJSONSetting[T any](currentValue T, setting storage.RuntimeSetting) (T, error) {
	var zero T
	var value any
	decoder := json.NewDecoder(strings.NewReader(setting.ValueJSON))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return zero, fmt.Errorf("runtime setting value_json must be valid JSON: %w", err)
	}

	patch, ok := value.(map[string]any)
	if !ok || !wholeObjectRuntimeKey(setting.Key) {
		patch = map[string]any{setting.Key: value}
	}

	current, err := json.Marshal(currentValue)
	if err != nil {
		return zero, err
	}
	var base map[string]any
	if err := json.Unmarshal(current, &base); err != nil {
		return zero, err
	}
	if base == nil {
		base = map[string]any{}
	}
	mergeJSONMap(base, patch)
	merged, err := json.Marshal(base)
	if err != nil {
		return zero, err
	}
	decoder = json.NewDecoder(bytes.NewReader(merged))
	decoder.UseNumber()
	var next T
	if err := decoder.Decode(&next); err != nil {
		return zero, fmt.Errorf("runtime setting does not match target schema: %w", err)
	}
	return next, nil
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
