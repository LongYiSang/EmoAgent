package promptcenter

import (
	"context"
	"encoding/json"
	"testing"
)

func TestDefaultCatalogLoadsMVPComponents(t *testing.T) {
	catalog, err := DefaultCatalog()
	if err != nil {
		t.Fatalf("DefaultCatalog: %v", err)
	}

	wantIDs := []string{
		ComponentEmotionOperatingContract,
		ComponentEmotionInternalContextDataPolicy,
		ComponentRunningSummarySystem,
		ComponentRunningSummaryRepair,
		ComponentWorkRuntimeDeciderSystem,
		ComponentWorkProgressSummarySystem,
		ComponentWorkProgressSummaryRepair,
		ComponentToolDelegateDescription,
		ComponentToolResumeDescription,
		ComponentToolFinishTaskDescription,
		ComponentToolRequestDecisionDescription,
	}
	for _, id := range wantIDs {
		component, ok := catalog.Get(id)
		if !ok {
			t.Fatalf("component %s missing", id)
		}
		if component.DefaultText == "" {
			t.Fatalf("component %s default text is empty", id)
		}
		if component.DefaultHash == "" {
			t.Fatalf("component %s default hash is empty", id)
		}
		if !component.SupportsScope(ScopeGlobal) || !component.SupportsScope(ScopeAgent) {
			t.Fatalf("component %s does not support global+agent scopes", id)
		}
	}

	items := catalog.List()
	for i := 1; i < len(items); i++ {
		if items[i-1].Order > items[i].Order {
			t.Fatalf("catalog not ordered: %s order %d before %s order %d", items[i-1].ID, items[i-1].Order, items[i].ID, items[i].Order)
		}
	}
}

func TestHashTextIsStableSHA256(t *testing.T) {
	if got := HashText("hello"); got != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("HashText mismatch: %s", got)
	}
	if HashText("hello") == HashText("hello\n") {
		t.Fatalf("HashText should distinguish trailing newline")
	}
}

func TestDynamicComponentBuildsHashAndMetadata(t *testing.T) {
	text := "你好🙂"
	component := DynamicComponent(ComponentEmotionPersona, "persona", SourcePersona, text, map[string]any{
		"persona_key": "default",
	})

	if component.ComponentID != ComponentEmotionPersona || component.SectionName != "persona" || component.Source != SourcePersona {
		t.Fatalf("component identity = %#v", component)
	}
	if !component.Dynamic || component.Editable {
		t.Fatalf("component dynamic/editable = %#v", component)
	}
	if component.EffectiveHash != HashText(text) {
		t.Fatalf("EffectiveHash = %q, want hash of text", component.EffectiveHash)
	}
	if component.TextLength != len([]rune(text)) {
		t.Fatalf("TextLength = %d, want rune count", component.TextLength)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(component.MetadataJSON), &metadata); err != nil {
		t.Fatalf("metadata json: %v", err)
	}
	if metadata["persona_key"] != "default" {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func TestBuildRenderSnapshotTruncatesRenderedTextWithoutChangingFinalHash(t *testing.T) {
	full := "abcdef"
	snapshot, err := BuildRenderSnapshot(RenderSnapshot{
		ID:           "snap-1",
		Purpose:      "emotion_chat",
		RenderedText: full,
	}, SnapshotRenderOptions{
		StoreRenderedText:    true,
		MaxRenderedTextChars: 3,
	})
	if err != nil {
		t.Fatalf("BuildRenderSnapshot: %v", err)
	}
	if snapshot.FinalHash != HashText(full) {
		t.Fatalf("FinalHash = %q, want hash of full prompt", snapshot.FinalHash)
	}
	if snapshot.RenderedText != "abc" || !snapshot.Truncated {
		t.Fatalf("snapshot text/truncated = %q/%v", snapshot.RenderedText, snapshot.Truncated)
	}

	hashOnly, err := BuildRenderSnapshot(RenderSnapshot{
		ID:           "snap-2",
		Purpose:      "emotion_chat",
		RenderedText: full,
	}, SnapshotRenderOptions{
		StoreRenderedText: false,
	})
	if err != nil {
		t.Fatalf("BuildRenderSnapshot hash-only: %v", err)
	}
	if hashOnly.FinalHash != HashText(full) || hashOnly.RenderedText != "" || hashOnly.Truncated {
		t.Fatalf("hash-only snapshot = %#v", hashOnly)
	}
}

func TestResolverPrecedenceAndFallback(t *testing.T) {
	ctx := context.Background()
	catalog := mustTestCatalog(t)
	store := NewMemoryStore()
	resolver := NewResolver(catalog, store)

	componentID := ComponentEmotionOperatingContract
	resolved, err := resolver.Resolve(ctx, componentID, PromptScope{AgentID: "agent-a", PersonaKey: "default"})
	if err != nil {
		t.Fatalf("resolve default: %v", err)
	}
	defaultText := resolved.Text
	if resolved.Source != SourceEmbeddedDefault {
		t.Fatalf("default source = %s", resolved.Source)
	}

	upsertPromptOverride(t, store, UpsertOverrideRequest{
		ComponentID:  componentID,
		ScopeType:    ScopeGlobal,
		Mode:         OverrideModeCustom,
		OverrideText: "global text",
	})
	resolved, err = resolver.Resolve(ctx, componentID, PromptScope{AgentID: "agent-a"})
	if err != nil {
		t.Fatalf("resolve global: %v", err)
	}
	if resolved.Text != "global text" || resolved.Source != SourceGlobalOverride {
		t.Fatalf("global resolve = (%q, %s)", resolved.Text, resolved.Source)
	}

	upsertPromptOverride(t, store, UpsertOverrideRequest{
		ComponentID:  componentID,
		ScopeType:    ScopeAgent,
		ScopeID:      "agent-a",
		Mode:         OverrideModeCustom,
		OverrideText: "agent text",
	})
	resolved, err = resolver.Resolve(ctx, componentID, PromptScope{AgentID: "agent-a"})
	if err != nil {
		t.Fatalf("resolve agent custom: %v", err)
	}
	if resolved.Text != "agent text" || resolved.Source != SourceAgentOverride {
		t.Fatalf("agent resolve = (%q, %s)", resolved.Text, resolved.Source)
	}

	upsertPromptOverride(t, store, UpsertOverrideRequest{
		ComponentID: componentID,
		ScopeType:   ScopeAgent,
		ScopeID:     "agent-a",
		Mode:        OverrideModeUseDefault,
	})
	resolved, err = resolver.Resolve(ctx, componentID, PromptScope{AgentID: "agent-a"})
	if err != nil {
		t.Fatalf("resolve agent use_default: %v", err)
	}
	if resolved.Text != defaultText || resolved.Source != SourceAgentDefault {
		t.Fatalf("agent default resolve = (%q, %s), want default source", resolved.Text, resolved.Source)
	}

	if err := store.DeleteOverride(ctx, componentID, ScopeAgent, "agent-a"); err != nil {
		t.Fatalf("delete agent override: %v", err)
	}
	resolved, err = resolver.Resolve(ctx, componentID, PromptScope{AgentID: "agent-a"})
	if err != nil {
		t.Fatalf("resolve after agent delete: %v", err)
	}
	if resolved.Text != "global text" || resolved.Source != SourceGlobalOverride {
		t.Fatalf("after agent delete = (%q, %s)", resolved.Text, resolved.Source)
	}

	if err := store.DeleteOverride(ctx, componentID, ScopeGlobal, ""); err != nil {
		t.Fatalf("delete global override: %v", err)
	}
	resolved, err = resolver.Resolve(ctx, componentID, PromptScope{AgentID: "agent-a"})
	if err != nil {
		t.Fatalf("resolve after global delete: %v", err)
	}
	if resolved.Text != defaultText || resolved.Source != SourceEmbeddedDefault {
		t.Fatalf("after global delete = (%q, %s)", resolved.Text, resolved.Source)
	}
}

func TestResolverFallsBackToEmbeddedDefaultOnStoreError(t *testing.T) {
	ctx := context.Background()
	catalog := mustTestCatalog(t)
	resolver := NewResolver(catalog, errorStore{})

	resolved, err := resolver.Resolve(ctx, ComponentRunningSummarySystem, PromptScope{AgentID: "agent-a"})
	if err != nil {
		t.Fatalf("fallback resolve returned error: %v", err)
	}
	if resolved.Source != SourceEmbeddedDefault {
		t.Fatalf("fallback source = %s", resolved.Source)
	}
	if resolved.Text == "" {
		t.Fatalf("fallback text is empty")
	}
}

func TestResolverMarksStaleOverride(t *testing.T) {
	ctx := context.Background()
	catalog := mustTestCatalog(t)
	store := NewMemoryStore()
	componentID := ComponentRunningSummaryRepair
	upsertPromptOverride(t, store, UpsertOverrideRequest{
		ComponentID:            componentID,
		ScopeType:              ScopeGlobal,
		Mode:                   OverrideModeCustom,
		OverrideText:           "repair custom",
		DefaultHashAtEdit:      "old-default-hash",
		TrustDefaultHashAtEdit: true,
	})

	resolved, err := NewResolver(catalog, store).Resolve(ctx, componentID, PromptScope{})
	if err != nil {
		t.Fatalf("resolve stale override: %v", err)
	}
	if !resolved.StaleOverride {
		t.Fatalf("expected stale override")
	}
	if resolved.DefaultHashAtEdit != "old-default-hash" {
		t.Fatalf("DefaultHashAtEdit = %q", resolved.DefaultHashAtEdit)
	}
}

func TestResolverResolveTextNilSafe(t *testing.T) {
	var resolver *Resolver
	if got := resolver.ResolveText(context.Background(), ComponentEmotionOperatingContract, PromptScope{}); got != "" {
		t.Fatalf("nil resolver ResolveText = %q, want empty", got)
	}
	if got := NewResolver(nil, nil).ResolveText(context.Background(), ComponentEmotionOperatingContract, PromptScope{}); got != "" {
		t.Fatalf("nil catalog ResolveText = %q, want empty", got)
	}
}

func TestResolverFallbackEmitsWarning(t *testing.T) {
	ctx := context.Background()
	catalog := mustTestCatalog(t)
	component := catalog.MustGet(ComponentEmotionOperatingContract)
	var warnings []ResolveWarning
	resolver := NewResolverWithWarning(catalog, errorStore{}, func(w ResolveWarning) {
		warnings = append(warnings, w)
	})

	resolved, err := resolver.Resolve(ctx, component.ID, PromptScope{})
	if err != nil {
		t.Fatalf("Resolve fallback: %v", err)
	}
	if resolved.Text != component.DefaultText || resolved.Source != SourceEmbeddedDefault {
		t.Fatalf("resolved = %#v", resolved)
	}
	if len(warnings) != 1 || warnings[0].ComponentID != component.ID || warnings[0].Code != "store_error" {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestDetailFromComponentReportsScopeStale(t *testing.T) {
	catalog := mustTestCatalog(t)
	component := catalog.MustGet(ComponentRunningSummaryRepair)
	global := &OverrideRecord{DefaultHashAtEdit: "old-global"}
	agent := &OverrideRecord{DefaultHashAtEdit: component.DefaultHash}
	resolved := resolvedFromComponent(component, "custom", SourceGlobalOverride, ScopeGlobal, "", global)

	detail := DetailFromComponent(component, global, agent, resolved)
	if !detail.GlobalOverrideStale {
		t.Fatalf("expected global override stale")
	}
	if detail.AgentOverrideStale {
		t.Fatalf("did not expect agent override stale")
	}
	if !detail.EffectiveOverrideStale || !detail.StaleOverride {
		t.Fatalf("effective stale flags = effective:%v legacy:%v", detail.EffectiveOverrideStale, detail.StaleOverride)
	}
}

func TestValidateUpsertOverride(t *testing.T) {
	ctx := context.Background()
	catalog := mustTestCatalog(t)
	agentExists := func(_ context.Context, id string) (bool, error) {
		return id == "agent-a", nil
	}

	tests := []struct {
		name string
		req  UpsertOverrideRequest
	}{
		{
			name: "unknown component",
			req: UpsertOverrideRequest{
				ComponentID:  "missing",
				ScopeType:    ScopeGlobal,
				Mode:         OverrideModeCustom,
				OverrideText: "text",
			},
		},
		{
			name: "global requires empty scope",
			req: UpsertOverrideRequest{
				ComponentID:  ComponentEmotionOperatingContract,
				ScopeType:    ScopeGlobal,
				ScopeID:      "agent-a",
				Mode:         OverrideModeCustom,
				OverrideText: "text",
			},
		},
		{
			name: "agent requires existing id",
			req: UpsertOverrideRequest{
				ComponentID:  ComponentEmotionOperatingContract,
				ScopeType:    ScopeAgent,
				ScopeID:      "missing-agent",
				Mode:         OverrideModeCustom,
				OverrideText: "text",
			},
		},
		{
			name: "use_default only agent",
			req: UpsertOverrideRequest{
				ComponentID: ComponentEmotionOperatingContract,
				ScopeType:   ScopeGlobal,
				Mode:        OverrideModeUseDefault,
			},
		},
		{
			name: "custom requires non-empty text",
			req: UpsertOverrideRequest{
				ComponentID:  ComponentEmotionOperatingContract,
				ScopeType:    ScopeAgent,
				ScopeID:      "agent-a",
				Mode:         OverrideModeCustom,
				OverrideText: "  ",
			},
		},
		{
			name: "custom rejects NUL",
			req: UpsertOverrideRequest{
				ComponentID:  ComponentEmotionOperatingContract,
				ScopeType:    ScopeAgent,
				ScopeID:      "agent-a",
				Mode:         OverrideModeCustom,
				OverrideText: "bad\x00text",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateUpsertOverride(ctx, catalog, agentExists, tt.req); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}

	valid := UpsertOverrideRequest{
		ComponentID:  ComponentEmotionOperatingContract,
		ScopeType:    ScopeAgent,
		ScopeID:      "agent-a",
		Mode:         OverrideModeCustom,
		OverrideText: "agent custom",
	}
	if err := ValidateUpsertOverride(ctx, catalog, agentExists, valid); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
}

func TestLintOverrideWarnings(t *testing.T) {
	catalog := mustTestCatalog(t)
	component := catalog.MustGet(ComponentRunningSummarySystem)
	warnings := LintOverride(component, UpsertOverrideRequest{
		ComponentID:  component.ID,
		ScopeType:    ScopeGlobal,
		Mode:         OverrideModeCustom,
		OverrideText: "Summarize the conversation in prose.",
	})
	if len(warnings) == 0 {
		t.Fatalf("expected lint warnings")
	}
	if warnings[0].ComponentID != component.ID || warnings[0].Severity != "warning" {
		t.Fatalf("warnings = %#v", warnings)
	}

	warnings = LintOverride(component, UpsertOverrideRequest{
		ComponentID:  component.ID,
		ScopeType:    ScopeGlobal,
		Mode:         OverrideModeCustom,
		OverrideText: component.DefaultText,
	})
	if len(warnings) != 0 {
		t.Fatalf("default text warnings = %#v", warnings)
	}
}

func mustTestCatalog(t *testing.T) *Catalog {
	t.Helper()
	catalog, err := DefaultCatalog()
	if err != nil {
		t.Fatalf("DefaultCatalog: %v", err)
	}
	return catalog
}

func upsertPromptOverride(t *testing.T, store Store, req UpsertOverrideRequest) {
	t.Helper()
	if err := store.UpsertOverride(context.Background(), req); err != nil {
		t.Fatalf("UpsertOverride: %v", err)
	}
}

type errorStore struct{}

func (errorStore) GetOverride(context.Context, string, string, string) (*OverrideRecord, error) {
	return nil, assertErr{}
}
func (errorStore) ListOverrides(context.Context) ([]OverrideRecord, error)      { return nil, assertErr{} }
func (errorStore) UpsertOverride(context.Context, UpsertOverrideRequest) error  { return assertErr{} }
func (errorStore) DeleteOverride(context.Context, string, string, string) error { return assertErr{} }
func (errorStore) SaveRenderSnapshot(context.Context, RenderSnapshot) error     { return assertErr{} }
func (errorStore) ListRenderSnapshots(context.Context, SnapshotFilter) ([]RenderSnapshotSummary, error) {
	return nil, assertErr{}
}
func (errorStore) GetRenderSnapshot(context.Context, string) (*RenderSnapshot, error) {
	return nil, assertErr{}
}

type assertErr struct{}

func (assertErr) Error() string { return "assert error" }
