package app

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/promptcenter"
	"github.com/longyisang/emoagent/internal/storage"
)

func TestPromptCenterAdminServiceOverridePrecedence(t *testing.T) {
	app, db := newPromptCenterTestApp(t)
	ctx := context.Background()
	componentID := promptcenter.ComponentEmotionOperatingContract

	components, err := app.ListPromptComponents(ctx, "agent-a")
	if err != nil {
		t.Fatalf("ListPromptComponents default: %v", err)
	}
	detail := findPromptDetail(t, components.Components, componentID)
	if detail.EffectiveSource != promptcenter.SourceEmbeddedDefault {
		t.Fatalf("default effective source = %s", detail.EffectiveSource)
	}
	defaultText := detail.DefaultText

	resp, err := app.UpsertPromptOverride(ctx, promptcenter.UpsertOverrideRequest{
		ComponentID:  componentID,
		ScopeType:    promptcenter.ScopeGlobal,
		Mode:         promptcenter.OverrideModeCustom,
		OverrideText: "global text",
	})
	if err != nil {
		t.Fatalf("UpsertPromptOverride global: %v", err)
	}
	if !resp.OK || len(resp.Warnings) == 0 {
		t.Fatalf("expected lint warnings for minimal global override, resp=%#v", resp)
	}
	detail, err = app.GetPromptComponent(ctx, componentID, "agent-a")
	if err != nil {
		t.Fatalf("GetPromptComponent global: %v", err)
	}
	if detail.EffectiveText != "global text" || detail.EffectiveSource != promptcenter.SourceGlobalOverride || detail.GlobalOverride == nil {
		t.Fatalf("global detail = %#v", detail)
	}
	if detail.GlobalOverride.DefaultHashAtEdit == "" || detail.GlobalOverride.DefaultHashAtEdit != detail.DefaultHash {
		t.Fatalf("global default hash at edit = %#v default=%s", detail.GlobalOverride, detail.DefaultHash)
	}
	if detail.GlobalOverrideStale || detail.AgentOverrideStale || detail.EffectiveOverrideStale {
		t.Fatalf("fresh stale flags = %#v", detail)
	}

	if _, err := app.UpsertPromptOverride(ctx, promptcenter.UpsertOverrideRequest{
		ComponentID:  componentID,
		ScopeType:    promptcenter.ScopeAgent,
		ScopeID:      "agent-a",
		Mode:         promptcenter.OverrideModeCustom,
		OverrideText: "agent text",
	}); err != nil {
		t.Fatalf("UpsertPromptOverride agent custom: %v", err)
	}
	detail, err = app.GetPromptComponent(ctx, componentID, "agent-a")
	if err != nil {
		t.Fatalf("GetPromptComponent agent: %v", err)
	}
	if detail.EffectiveText != "agent text" || detail.EffectiveSource != promptcenter.SourceAgentOverride || detail.AgentOverride == nil {
		t.Fatalf("agent detail = %#v", detail)
	}

	if err := db.UpsertOverride(ctx, promptcenter.UpsertOverrideRequest{
		ComponentID:            componentID,
		ScopeType:              promptcenter.ScopeGlobal,
		Mode:                   promptcenter.OverrideModeCustom,
		OverrideText:           "stale global text",
		DefaultHashAtEdit:      "old-default-hash",
		TrustDefaultHashAtEdit: true,
	}); err != nil {
		t.Fatalf("UpsertOverride stale global: %v", err)
	}
	detail, err = app.GetPromptComponent(ctx, componentID, "agent-a")
	if err != nil {
		t.Fatalf("GetPromptComponent stale global with agent override: %v", err)
	}
	if !detail.GlobalOverrideStale || detail.AgentOverrideStale || detail.EffectiveOverrideStale {
		t.Fatalf("stale global hidden by agent override flags = %#v", detail)
	}
	if _, err := app.UpsertPromptOverride(ctx, promptcenter.UpsertOverrideRequest{
		ComponentID:  componentID,
		ScopeType:    promptcenter.ScopeGlobal,
		Mode:         promptcenter.OverrideModeCustom,
		OverrideText: "global text",
	}); err != nil {
		t.Fatalf("restore global override: %v", err)
	}

	if _, err := app.UpsertPromptOverride(ctx, promptcenter.UpsertOverrideRequest{
		ComponentID: componentID,
		ScopeType:   promptcenter.ScopeAgent,
		ScopeID:     "agent-a",
		Mode:        promptcenter.OverrideModeUseDefault,
	}); err != nil {
		t.Fatalf("UpsertPromptOverride agent use_default: %v", err)
	}
	detail, err = app.GetPromptComponent(ctx, componentID, "agent-a")
	if err != nil {
		t.Fatalf("GetPromptComponent use_default: %v", err)
	}
	if detail.EffectiveText != defaultText || detail.EffectiveSource != promptcenter.SourceAgentDefault {
		t.Fatalf("use_default detail = %#v", detail)
	}

	if err := app.DeletePromptOverride(ctx, promptcenter.DeleteOverrideRequest{
		ComponentID: componentID,
		ScopeType:   promptcenter.ScopeAgent,
		ScopeID:     "agent-a",
	}); err != nil {
		t.Fatalf("DeletePromptOverride agent: %v", err)
	}
	detail, err = app.GetPromptComponent(ctx, componentID, "agent-a")
	if err != nil {
		t.Fatalf("GetPromptComponent after agent delete: %v", err)
	}
	if detail.EffectiveText != "global text" || detail.EffectiveSource != promptcenter.SourceGlobalOverride {
		t.Fatalf("after agent delete = %#v", detail)
	}

	if err := app.DeletePromptOverride(ctx, promptcenter.DeleteOverrideRequest{
		ComponentID: componentID,
		ScopeType:   promptcenter.ScopeGlobal,
	}); err != nil {
		t.Fatalf("DeletePromptOverride global: %v", err)
	}
	detail, err = app.GetPromptComponent(ctx, componentID, "agent-a")
	if err != nil {
		t.Fatalf("GetPromptComponent after global delete: %v", err)
	}
	if detail.EffectiveText != defaultText || detail.EffectiveSource != promptcenter.SourceEmbeddedDefault {
		t.Fatalf("after global delete = %#v", detail)
	}

	preview, err := app.PreviewPrompt(ctx, promptcenter.PromptPreviewRequest{
		AgentID:      "agent-a",
		Purpose:      "emotion_chat",
		ComponentIDs: []string{componentID},
	})
	if err != nil {
		t.Fatalf("PreviewPrompt: %v", err)
	}
	if preview.RenderedText != defaultText || preview.FinalHash == "" || len(preview.Components) != 1 {
		t.Fatalf("preview = %#v", preview)
	}

	built, err := promptcenter.BuildRenderSnapshot(promptcenter.RenderSnapshot{
		ID:           "snap-app",
		AgentID:      "agent-a",
		PersonaKey:   "default",
		Purpose:      "emotion_chat",
		RenderedText: "system",
	})
	if err != nil {
		t.Fatalf("BuildRenderSnapshot: %v", err)
	}
	if err := db.SaveRenderSnapshot(ctx, built); err != nil {
		t.Fatalf("SaveRenderSnapshot: %v", err)
	}
	snapshots, err := app.ListPromptSnapshots(ctx, promptcenter.PromptSnapshotListRequest{AgentID: "agent-a", Limit: 5})
	if err != nil {
		t.Fatalf("ListPromptSnapshots: %v", err)
	}
	if len(snapshots.Snapshots) != 1 || snapshots.Snapshots[0].ID != "snap-app" {
		t.Fatalf("snapshots = %#v", snapshots)
	}
	snapshot, err := app.GetPromptSnapshot(ctx, "snap-app")
	if err != nil {
		t.Fatalf("GetPromptSnapshot: %v", err)
	}
	if snapshot.ID != "snap-app" || snapshot.RenderedText != "system" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestPromptCenterAdminServiceFullEmotionPreview(t *testing.T) {
	app, _ := newPromptCenterTestApp(t)
	setTestPersonas(app, map[string]*config.Persona{
		"default": {Name: "Default", SystemPrompt: "You are warm."},
	})

	preview, err := app.PreviewPrompt(context.Background(), promptcenter.PromptPreviewRequest{
		Mode:       "full",
		Purpose:    "emotion_chat_full",
		AgentID:    "agent-a",
		PersonaKey: "default",
	})
	if err != nil {
		t.Fatalf("PreviewPrompt full: %v", err)
	}
	for _, snippet := range []string{"<persona>", "You are warm.", "<operating_contract>", "<runtime_context>", "<internal_context_data_policy>"} {
		if !strings.Contains(preview.RenderedText, snippet) {
			t.Fatalf("full preview missing %q:\n%s", snippet, preview.RenderedText)
		}
	}
	for _, id := range []string{
		promptcenter.ComponentEmotionPersona,
		promptcenter.ComponentEmotionRuntimeContext,
		promptcenter.ComponentEmotionOperatingContract,
		promptcenter.ComponentEmotionInternalContextDataPolicy,
	} {
		if findPreviewComponent(preview.Components, id).ComponentID == "" {
			t.Fatalf("preview components = %#v, missing %s", preview.Components, id)
		}
	}
	if preview.FinalHash != promptcenter.HashText(preview.RenderedText) {
		t.Fatalf("FinalHash = %q, want rendered hash", preview.FinalHash)
	}
	for _, code := range []string{"no_session", "memory_preview_disabled", "agent_affect_preview_disabled"} {
		if !hasPreviewWarning(preview.Warnings, code) {
			t.Fatalf("warnings = %#v, missing %s", preview.Warnings, code)
		}
	}

	testConfig(app).AgentAffect.Enabled = true
	withAffect, err := app.PreviewPrompt(context.Background(), promptcenter.PromptPreviewRequest{
		Mode:               "full",
		Purpose:            "emotion_chat_full",
		AgentID:            "agent-a",
		PersonaKey:         "default",
		IncludeAgentAffect: true,
	})
	if err != nil {
		t.Fatalf("PreviewPrompt full with affect: %v", err)
	}
	if !strings.Contains(withAffect.RenderedText, "[Agent Mood]") {
		t.Fatalf("full preview missing agent affect block:\n%s", withAffect.RenderedText)
	}
	if findPreviewComponent(withAffect.Components, promptcenter.ComponentAgentAffectPromptBlock).ComponentID == "" {
		t.Fatalf("preview components missing agent affect: %#v", withAffect.Components)
	}
	if hasPreviewWarning(withAffect.Warnings, "agent_affect_preview_disabled") {
		t.Fatalf("with affect warnings = %#v", withAffect.Warnings)
	}

	withMemory, err := app.PreviewPrompt(context.Background(), promptcenter.PromptPreviewRequest{
		Mode:          "full",
		Purpose:       "emotion_chat_full",
		AgentID:       "agent-a",
		PersonaKey:    "default",
		SessionID:     "session-preview",
		UserMessage:   "hello",
		IncludeMemory: true,
	})
	if err != nil {
		t.Fatalf("PreviewPrompt full with memory: %v", err)
	}
	if !hasPreviewWarning(withMemory.Warnings, "memory_preview_unavailable") {
		t.Fatalf("with memory warnings = %#v", withMemory.Warnings)
	}
}

func TestPromptCenterAdminServiceValidationRejectsMissingAgent(t *testing.T) {
	app, _ := newPromptCenterTestApp(t)
	ctx := context.Background()
	_, err := app.UpsertPromptOverride(ctx, promptcenter.UpsertOverrideRequest{
		ComponentID:  promptcenter.ComponentEmotionOperatingContract,
		ScopeType:    promptcenter.ScopeAgent,
		ScopeID:      "missing-agent",
		Mode:         promptcenter.OverrideModeCustom,
		OverrideText: "text",
	})
	if err == nil {
		t.Fatalf("expected missing agent validation error")
	}

	checks := []struct {
		name string
		run  func() error
	}{
		{name: "list", run: func() error {
			_, err := app.ListPromptComponents(ctx, "missing-agent")
			return err
		}},
		{name: "get", run: func() error {
			_, err := app.GetPromptComponent(ctx, promptcenter.ComponentEmotionOperatingContract, "missing-agent")
			return err
		}},
		{name: "preview", run: func() error {
			_, err := app.PreviewPrompt(ctx, promptcenter.PromptPreviewRequest{AgentID: "missing-agent", ComponentID: promptcenter.ComponentEmotionOperatingContract})
			return err
		}},
		{name: "delete", run: func() error {
			return app.DeletePromptOverride(ctx, promptcenter.DeleteOverrideRequest{ComponentID: promptcenter.ComponentEmotionOperatingContract, ScopeType: promptcenter.ScopeAgent, ScopeID: "missing-agent"})
		}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			err := check.run()
			if err == nil || !strings.Contains(err.Error(), "agent_id does not exist") {
				t.Fatalf("err = %v, want missing agent validation", err)
			}
		})
	}
}

func newPromptCenterTestApp(t *testing.T) (*App, *storage.DB) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.OpenWithOptions(filepath.Join(t.TempDir(), "test.db"), logger, storage.StorageOptions{Timezone: "Asia/Shanghai"})
	if err != nil {
		t.Fatalf("OpenWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	provider := config.LLMProvider{
		ID:             "fake",
		Name:           "Fake",
		Protocol:       "openai_compatible",
		BaseURL:        "https://example.invalid",
		APIKeyEnv:      "FAKE_API_KEY",
		ModelDiscovery: "manual",
		Enabled:        true,
	}
	if err := db.UpsertLLMProvider(provider); err != nil {
		t.Fatalf("UpsertLLMProvider: %v", err)
	}
	agent := config.AgentConfig{
		ID:         "agent-a",
		Name:       "Agent A",
		PersonaKey: "default",
		Emotion: config.AgentModelGroup{
			Main:    config.ModelBinding{ProviderID: "fake", Model: "main", Params: llm.RequestParams{}},
			Summary: config.ModelBinding{ProviderID: "fake", Model: "summary", Params: llm.RequestParams{}},
		},
		Work: config.AgentModelGroup{
			Main:    config.ModelBinding{ProviderID: "fake", Model: "work", Params: llm.RequestParams{}},
			Summary: config.ModelBinding{ProviderID: "fake", Model: "work-summary", Params: llm.RequestParams{}},
		},
		ContextOverrides: map[string]any{},
	}
	if err := db.UpsertAgentConfig(agent); err != nil {
		t.Fatalf("UpsertAgentConfig: %v", err)
	}
	return newTestApp(config.DefaultConfig(), db, logger), db
}

func findPromptDetail(t *testing.T, details []promptcenter.PromptComponentDetail, id string) promptcenter.PromptComponentDetail {
	t.Helper()
	for _, detail := range details {
		if detail.ID == id {
			return detail
		}
	}
	t.Fatalf("detail %s not found", id)
	return promptcenter.PromptComponentDetail{}
}

func findPreviewComponent(components []promptcenter.RenderComponent, id string) promptcenter.RenderComponent {
	for _, component := range components {
		if component.ComponentID == id {
			return component
		}
	}
	return promptcenter.RenderComponent{}
}

func hasPreviewWarning(warnings []promptcenter.PromptPreviewWarning, code string) bool {
	for _, warning := range warnings {
		if warning.Code == code {
			return true
		}
	}
	return false
}
