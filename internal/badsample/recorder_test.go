package badsample

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/storage"
)

type fakeStore struct {
	messages     []storage.BadSampleMessage
	messagesErr  error
	snapshots    []storage.BadSamplePromptSnapshot
	snapshotsErr error
	affect       storage.BadSampleAffect
	affectErr    error
	turns        []storage.BadSampleTurn
	turnsErr     error
	targetTurnID string
	targetErr    error

	inserted  []storage.BadSampleRecord
	insertErr error
	count     int
	countErr  error
}

func (f *fakeStore) RecentSessionMessages(_ context.Context, _ string, limit int) ([]storage.BadSampleMessage, error) {
	if f.messagesErr != nil {
		return nil, f.messagesErr
	}
	if len(f.messages) > limit {
		return f.messages[len(f.messages)-limit:], nil
	}
	return f.messages, nil
}

func (f *fakeStore) RecentPromptSnapshots(_ context.Context, _ string, _ int) ([]storage.BadSamplePromptSnapshot, error) {
	return f.snapshots, f.snapshotsErr
}

func (f *fakeStore) SessionAffectSnapshot(_ context.Context, _ string, _ int) (storage.BadSampleAffect, error) {
	return f.affect, f.affectErr
}

func (f *fakeStore) RecentTurns(_ context.Context, _ string, _ int) ([]storage.BadSampleTurn, error) {
	return f.turns, f.turnsErr
}

func (f *fakeStore) LatestCompletedTurnID(_ context.Context, _ string) (string, error) {
	return f.targetTurnID, f.targetErr
}

func (f *fakeStore) InsertBadSample(_ context.Context, record storage.BadSampleRecord) (storage.BadSampleRecord, error) {
	if f.insertErr != nil {
		return storage.BadSampleRecord{}, f.insertErr
	}
	if record.ID == "" {
		record.ID = "sample-1"
	}
	f.inserted = append(f.inserted, record)
	return record, nil
}

func (f *fakeStore) CountBadSamples(_ context.Context) (int, error) {
	return f.count, f.countErr
}

func newPopulatedStore() *fakeStore {
	return &fakeStore{
		messages: []storage.BadSampleMessage{
			{Role: "user", Content: "晚饭吃什么", CreatedAt: "t1", Metadata: `{"kind":"dialogue_user"}`},
			{Role: "assistant", Content: "香菜牛肉面", CreatedAt: "t2", Metadata: `{"memory_pipeline":{"prompt_block":"[记忆]"}}`},
		},
		snapshots: []storage.BadSamplePromptSnapshot{
			{TurnID: "turn-9", Purpose: "emotion_chat", Model: "kimi-k2.6", RenderedText: "prompt"},
		},
		affect:       storage.BadSampleAffect{State: &storage.BadSampleAffectState{Label: "calm"}},
		turns:        []storage.BadSampleTurn{{ID: "turn-9", Status: "done"}},
		targetTurnID: "turn-9",
		count:        7,
	}
}

func decodeContext(t *testing.T, raw string) FrozenContext {
	t.Helper()
	var frozen FrozenContext
	if err := json.Unmarshal([]byte(raw), &frozen); err != nil {
		t.Fatalf("context_json is not valid JSON: %v", err)
	}
	return frozen
}

func TestRecordFreezesFullScene(t *testing.T) {
	store := newPopulatedStore()
	result, err := NewRecorder(store).Record(context.Background(), RecordRequest{
		SessionID:  "s1",
		OriginKey:  "webui:local:main",
		PersonaKey: "default",
		Reason:     "  又忘了我讨厌香菜  ",
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if len(store.inserted) != 1 {
		t.Fatalf("inserted %d rows, want 1", len(store.inserted))
	}
	record := store.inserted[0]
	if record.Reason != "又忘了我讨厌香菜" {
		t.Fatalf("Reason = %q, want trimmed text", record.Reason)
	}
	if record.TargetTurnID != "turn-9" {
		t.Fatalf("TargetTurnID = %q", record.TargetTurnID)
	}
	if record.ContextSchemaVersion != ContextSchemaVersion {
		t.Fatalf("ContextSchemaVersion = %d", record.ContextSchemaVersion)
	}

	frozen := decodeContext(t, record.ContextJSON)
	if len(frozen.Messages) != 2 || len(frozen.PromptSnapshots) != 1 || len(frozen.Turns) != 1 {
		t.Fatalf("scene blocks wrong: %d messages, %d snapshots, %d turns",
			len(frozen.Messages), len(frozen.PromptSnapshots), len(frozen.Turns))
	}
	if frozen.Affect.State == nil || frozen.Affect.State.Label != "calm" {
		t.Fatalf("affect not frozen: %#v", frozen.Affect.State)
	}
	if frozen.Meta.Model != "kimi-k2.6" {
		t.Fatalf("Meta.Model = %q, want the newest snapshot's model", frozen.Meta.Model)
	}
	if !strings.Contains(frozen.Messages[1].Metadata, "memory_pipeline") {
		t.Fatal("assistant metadata lost; it carries the injected memory block")
	}
	if len(frozen.Errors) != 0 {
		t.Fatalf("unexpected block errors: %#v", frozen.Errors)
	}

	if result.TotalCount != 7 {
		t.Fatalf("TotalCount = %d, want 7", result.TotalCount)
	}
	if result.FrozenMessages != 2 || result.FrozenSnapshots != 1 {
		t.Fatalf("result counts = %d/%d, want the actual frozen counts 2/1",
			result.FrozenMessages, result.FrozenSnapshots)
	}
}

func TestRecordRejectsEmptyReason(t *testing.T) {
	for _, reason := range []string{"", "   "} {
		store := newPopulatedStore()
		_, err := NewRecorder(store).Record(context.Background(), RecordRequest{SessionID: "s1", Reason: reason})
		if !errors.Is(err, ErrEmptyReason) {
			t.Fatalf("reason %q: err = %v, want ErrEmptyReason", reason, err)
		}
		if len(store.inserted) != 0 {
			t.Fatal("a record was written despite the empty reason")
		}
	}
}

// A block that cannot be collected must degrade the sample, never lose it.
func TestRecordDegradesWhenBlocksFail(t *testing.T) {
	store := newPopulatedStore()
	store.snapshotsErr = errors.New("snapshot table pruned")
	store.affectErr = errors.New("affect not initialised")

	result, err := NewRecorder(store).Record(context.Background(), RecordRequest{
		SessionID: "s1", Reason: "记忆没召回",
	})
	if err != nil {
		t.Fatalf("Record must still succeed with partial scene, got: %v", err)
	}
	frozen := decodeContext(t, store.inserted[0].ContextJSON)
	if len(frozen.Messages) != 2 {
		t.Fatalf("healthy block was dropped: %d messages", len(frozen.Messages))
	}
	if len(frozen.PromptSnapshots) != 0 || frozen.Affect.State != nil {
		t.Fatal("failed blocks should be empty")
	}
	blocks := map[string]bool{}
	for _, e := range frozen.Errors {
		blocks[e.Block] = true
	}
	if !blocks["prompt_snapshots"] || !blocks["affect"] {
		t.Fatalf("failures not recorded in errors[]: %#v", frozen.Errors)
	}
	if result.FrozenSnapshots != 0 {
		t.Fatalf("FrozenSnapshots = %d, want 0 — the receipt must not overstate", result.FrozenSnapshots)
	}
}

func TestRecordWithoutCompletedTurnStillWrites(t *testing.T) {
	store := newPopulatedStore()
	store.targetTurnID = ""

	if _, err := NewRecorder(store).Record(context.Background(), RecordRequest{
		SessionID: "s1", Reason: "刚开会话就不对劲",
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if len(store.inserted) != 1 {
		t.Fatal("record was not written")
	}
	if store.inserted[0].TargetTurnID != "" {
		t.Fatalf("TargetTurnID = %q, want empty", store.inserted[0].TargetTurnID)
	}
}

func TestRecordFailsOnlyWhenRowWriteFails(t *testing.T) {
	store := newPopulatedStore()
	store.insertErr = errors.New("disk full")

	if _, err := NewRecorder(store).Record(context.Background(), RecordRequest{
		SessionID: "s1", Reason: "理由",
	}); err == nil {
		t.Fatal("Record must fail when the row itself cannot be written")
	}
}

// A failing count costs the receipt its number but must not lose the sample.
func TestRecordSurvivesCountFailure(t *testing.T) {
	store := newPopulatedStore()
	store.countErr = errors.New("count exploded")

	result, err := NewRecorder(store).Record(context.Background(), RecordRequest{
		SessionID: "s1", Reason: "理由",
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if len(store.inserted) != 1 {
		t.Fatal("sample was lost because counting failed")
	}
	if result.TotalCount != 0 {
		t.Fatalf("TotalCount = %d, want 0 when counting failed", result.TotalCount)
	}
}

func TestRecordTruncatesOversizedScene(t *testing.T) {
	store := newPopulatedStore()
	huge := strings.Repeat("x", MaxContextBytes)
	store.snapshots = []storage.BadSamplePromptSnapshot{
		{TurnID: "old", Model: "m1", RenderedText: huge},
		{TurnID: "new", Model: "m2", RenderedText: huge},
	}

	result, err := NewRecorder(store).Record(context.Background(), RecordRequest{
		SessionID: "s1", Reason: "太长了",
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	raw := store.inserted[0].ContextJSON
	if len(raw) > MaxContextBytes {
		t.Fatalf("context_json = %d bytes, exceeds cap %d", len(raw), MaxContextBytes)
	}
	frozen := decodeContext(t, raw)
	if !frozen.Truncated || !result.Truncated {
		t.Fatal("truncation happened but was not flagged")
	}
	if len(frozen.Messages) != 2 {
		t.Fatalf("messages should survive snapshot truncation, got %d", len(frozen.Messages))
	}
}
