package storage

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/longyisang/emoagent/internal/promptcenter"
)

func TestOpenAndMigrate_CreatesPromptCenterSchema(t *testing.T) {
	db := testDB(t)

	for _, table := range []string{"prompt_overrides", "prompt_render_snapshots"} {
		var name string
		if err := db.SqlDB().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name); err != nil {
			t.Fatalf("table %q not found: %v", table, err)
		}
	}
	assertTableColumns(t, db, "prompt_overrides", []string{
		"id", "component_id", "scope_type", "scope_id", "mode", "override_text",
		"enabled", "default_hash_at_edit", "note", "created_at", "updated_at",
	})
	assertTableColumns(t, db, "prompt_render_snapshots", []string{
		"id", "request_id", "turn_id", "session_id", "agent_id", "persona_key",
		"purpose", "model", "final_hash", "components_json", "rendered_text",
		"truncated", "created_at",
	})
	for _, index := range []string{
		"idx_prompt_overrides_component",
		"idx_prompt_overrides_agent",
		"idx_prompt_render_snapshots_session_time",
		"idx_prompt_render_snapshots_agent_time",
		"idx_prompt_render_snapshots_purpose_time",
	} {
		var name string
		if err := db.SqlDB().QueryRow("SELECT name FROM sqlite_master WHERE type='index' AND name=?", index).Scan(&name); err != nil {
			t.Fatalf("index %q not found: %v", index, err)
		}
	}
}

func TestPromptOverrideCRUD(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	componentID := promptcenter.ComponentEmotionOperatingContract

	if err := db.UpsertPromptOverride(ctx, promptcenter.UpsertOverrideRequest{
		ComponentID:            componentID,
		ScopeType:              promptcenter.ScopeGlobal,
		Mode:                   promptcenter.OverrideModeCustom,
		OverrideText:           "global text",
		DefaultHashAtEdit:      "hash-1",
		TrustDefaultHashAtEdit: true,
		Note:                   "first",
	}); err != nil {
		t.Fatalf("UpsertPromptOverride global: %v", err)
	}
	got, err := db.GetOverride(ctx, componentID, promptcenter.ScopeGlobal, "")
	if err != nil {
		t.Fatalf("GetOverride global: %v", err)
	}
	if got == nil || got.OverrideText != "global text" || got.Mode != promptcenter.OverrideModeCustom || !got.Enabled || got.DefaultHashAtEdit != "hash-1" || got.Note != "first" {
		t.Fatalf("global override = %#v", got)
	}

	if err := db.UpsertPromptOverride(ctx, promptcenter.UpsertOverrideRequest{
		ComponentID:            componentID,
		ScopeType:              promptcenter.ScopeGlobal,
		Mode:                   promptcenter.OverrideModeCustom,
		OverrideText:           "updated global",
		DefaultHashAtEdit:      "hash-2",
		TrustDefaultHashAtEdit: true,
	}); err != nil {
		t.Fatalf("UpsertPromptOverride update global: %v", err)
	}
	got, err = db.GetOverride(ctx, componentID, promptcenter.ScopeGlobal, "")
	if err != nil {
		t.Fatalf("GetOverride updated global: %v", err)
	}
	if got == nil || got.OverrideText != "updated global" || got.DefaultHashAtEdit != "hash-2" {
		t.Fatalf("updated global override = %#v", got)
	}

	if err := db.UpsertPromptOverride(ctx, promptcenter.UpsertOverrideRequest{
		ComponentID: componentID,
		ScopeType:   promptcenter.ScopeAgent,
		ScopeID:     "agent-a",
		Mode:        promptcenter.OverrideModeUseDefault,
	}); err != nil {
		t.Fatalf("UpsertPromptOverride agent use_default: %v", err)
	}
	records, err := db.ListOverrides(ctx)
	if err != nil {
		t.Fatalf("ListOverrides: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2: %#v", len(records), records)
	}

	if err := db.DeleteOverride(ctx, componentID, promptcenter.ScopeAgent, "agent-a"); err != nil {
		t.Fatalf("DeleteOverride agent: %v", err)
	}
	got, err = db.GetOverride(ctx, componentID, promptcenter.ScopeAgent, "agent-a")
	if err != nil {
		t.Fatalf("GetOverride deleted agent: %v", err)
	}
	if got != nil {
		t.Fatalf("deleted agent override = %#v, want nil", got)
	}
}

func TestPromptRenderSnapshotCRUD(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	snapshot := promptcenter.RenderSnapshot{
		ID:           "snap-1",
		RequestID:    "req-1",
		TurnID:       "turn-1",
		SessionID:    "session-1",
		AgentID:      "agent-a",
		PersonaKey:   "default",
		Purpose:      "emotion_chat",
		Model:        "model-a",
		RenderedText: "system text",
		Components: []promptcenter.RenderComponent{
			{
				ComponentID:   promptcenter.ComponentEmotionOperatingContract,
				Source:        promptcenter.SourceGlobalOverride,
				ScopeType:     promptcenter.ScopeGlobal,
				DefaultHash:   "default",
				EffectiveHash: "effective",
			},
		},
	}
	built, err := promptcenter.BuildRenderSnapshot(snapshot)
	if err != nil {
		t.Fatalf("BuildRenderSnapshot: %v", err)
	}
	if err := db.SaveRenderSnapshot(ctx, built); err != nil {
		t.Fatalf("SaveRenderSnapshot: %v", err)
	}

	items, err := db.ListRenderSnapshots(ctx, promptcenter.SnapshotFilter{AgentID: "agent-a", Purpose: "emotion_chat", Limit: 10})
	if err != nil {
		t.Fatalf("ListRenderSnapshots: %v", err)
	}
	if len(items) != 1 || items[0].ID != "snap-1" || items[0].FinalHash == "" {
		t.Fatalf("items = %#v", items)
	}
	if none, err := db.ListRenderSnapshots(ctx, promptcenter.SnapshotFilter{AgentID: "agent-b", Limit: 10}); err != nil || len(none) != 0 {
		t.Fatalf("filtered snapshots = %#v, err=%v", none, err)
	}

	got, err := db.GetRenderSnapshot(ctx, "snap-1")
	if err != nil {
		t.Fatalf("GetRenderSnapshot: %v", err)
	}
	if got == nil || got.RenderedText != "system text" || len(got.Components) != 1 {
		t.Fatalf("snapshot = %#v", got)
	}
	var rawComponents []promptcenter.RenderComponent
	if err := json.Unmarshal([]byte(got.ComponentsJSON), &rawComponents); err != nil {
		t.Fatalf("components_json: %v", err)
	}
	if len(rawComponents) != 1 || rawComponents[0].ComponentID != promptcenter.ComponentEmotionOperatingContract {
		t.Fatalf("components_json decoded = %#v", rawComponents)
	}
}
