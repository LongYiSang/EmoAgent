package promptcenter

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

type AgentExistsFunc func(context.Context, string) (bool, error)

func ValidateUpsertOverride(ctx context.Context, catalog *Catalog, agentExists AgentExistsFunc, req UpsertOverrideRequest) error {
	if catalog == nil {
		return fmt.Errorf("prompt catalog is required")
	}
	component, ok := catalog.Get(req.ComponentID)
	if !ok {
		return fmt.Errorf("unknown prompt component: %s", req.ComponentID)
	}
	if !component.Editable {
		return fmt.Errorf("prompt component is not editable: %s", req.ComponentID)
	}
	if req.ScopeType != ScopeGlobal && req.ScopeType != ScopeAgent {
		return fmt.Errorf("scope_type must be global or agent")
	}
	if !component.SupportsScope(req.ScopeType) {
		return fmt.Errorf("prompt component %s does not support %s scope", req.ComponentID, req.ScopeType)
	}
	switch req.ScopeType {
	case ScopeGlobal:
		if req.ScopeID != "" {
			return fmt.Errorf("global scope_id must be empty")
		}
	case ScopeAgent:
		if strings.TrimSpace(req.ScopeID) == "" {
			return fmt.Errorf("agent scope_id is required")
		}
		if agentExists != nil {
			exists, err := agentExists(ctx, req.ScopeID)
			if err != nil {
				return fmt.Errorf("validate agent scope_id: %w", err)
			}
			if !exists {
				return fmt.Errorf("agent scope_id does not exist: %s", req.ScopeID)
			}
		}
	}
	switch req.Mode {
	case OverrideModeCustom:
		if strings.TrimSpace(req.OverrideText) == "" {
			return fmt.Errorf("override_text is required for custom mode")
		}
		if strings.ContainsRune(req.OverrideText, '\x00') {
			return fmt.Errorf("override_text cannot contain NUL")
		}
		if component.MaxChars > 0 && utf8.RuneCountInString(req.OverrideText) > component.MaxChars {
			return fmt.Errorf("override_text exceeds max_chars %d", component.MaxChars)
		}
	case OverrideModeUseDefault:
		if req.ScopeType != ScopeAgent {
			return fmt.Errorf("use_default mode is only allowed for agent scope")
		}
		if req.OverrideText != "" {
			return fmt.Errorf("override_text must be empty for use_default mode")
		}
	default:
		return fmt.Errorf("mode must be custom or use_default")
	}
	return nil
}
