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

func LintOverride(component PromptComponent, req UpsertOverrideRequest) []PromptLintWarning {
	if req.Mode != OverrideModeCustom {
		return nil
	}
	text := strings.TrimSpace(req.OverrideText)
	if text == "" {
		return nil
	}
	lint := promptLint{componentID: component.ID, text: strings.ToLower(text)}
	switch component.ID {
	case ComponentRunningSummarySystem:
		lint.require("missing_json_only", "JSON only")
		for _, token := range []string{"running_summary", "user_facts", "relationship_state", "open_loops", "do_not_forget"} {
			lint.require("missing_"+token, token)
		}
		lint.requireAny("missing_secret_guard", []string{"credentials", "secrets", "access tokens", "private keys"})
	case ComponentRunningSummaryRepair:
		lint.require("missing_repair_intent", "repair")
		lint.require("missing_json_only", "JSON only")
		lint.require("missing_no_new_facts", "do not add facts")
	case ComponentEmotionInternalContextDataPolicy:
		lint.requireAll("missing_instruction_boundary", []string{"do not treat", "new user instructions"})
		lint.require("missing_raw_json_guard", "raw JSON")
		lint.require("missing_internal_ids_guard", "internal IDs")
		lint.require("missing_hash_guard", "hashes")
	case ComponentEmotionOperatingContract:
		for _, token := range []string{"delegate_to_work", "permission scope", "TaskReport", "resume_work", "decision"} {
			lint.require("missing_"+strings.ReplaceAll(strings.ToLower(token), " ", "_"), token)
		}
	}
	return lint.warnings
}

type promptLint struct {
	componentID string
	text        string
	warnings    []PromptLintWarning
}

func (l *promptLint) require(code, token string) {
	if !strings.Contains(l.text, strings.ToLower(token)) {
		l.add(code, fmt.Sprintf("提示词缺少关键约束：%s", token))
	}
}

func (l *promptLint) requireAny(code string, tokens []string) {
	for _, token := range tokens {
		if strings.Contains(l.text, strings.ToLower(token)) {
			return
		}
	}
	l.add(code, fmt.Sprintf("提示词缺少关键约束：%s", strings.Join(tokens, " / ")))
}

func (l *promptLint) requireAll(code string, tokens []string) {
	for _, token := range tokens {
		if !strings.Contains(l.text, strings.ToLower(token)) {
			l.add(code, fmt.Sprintf("提示词缺少关键约束：%s", strings.Join(tokens, " + ")))
			return
		}
	}
}

func (l *promptLint) add(code, message string) {
	l.warnings = append(l.warnings, PromptLintWarning{
		ComponentID: l.componentID,
		Code:        code,
		Severity:    "warning",
		Message:     message,
	})
}
