package context

import (
	stdcontext "context"
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/storage"
)

// Every string below is real content pulled from sessions.metadata on
// 2026-08-13. "用户刚切了西瓜在吃" was written 2026-07-06 and was still being
// read as current 36 days later.
func TestFindStaleTimeFactsMatchesRealPollution(t *testing.T) {
	summary := RunningSummary{
		UserFacts: []string{
			"用户昵称叫鸡翅",
			"用户刚切了西瓜在吃，白籽，超级甜",
			"用户今天傍晚去骑车了，天气很热，小路上虫子多，骑车后需要洗澡",
			"用户刚才不小心点错了消息，现在时间是晚上十点多，不是凌晨",
			"用户喜欢看CS比赛",
		},
	}

	stale := FindStaleTimeFacts(summary)

	want := []string{
		"用户刚切了西瓜在吃，白籽，超级甜",
		"用户今天傍晚去骑车了，天气很热，小路上虫子多，骑车后需要洗澡",
		"用户刚才不小心点错了消息，现在时间是晚上十点多，不是凌晨",
	}
	if !reflect.DeepEqual(stale, want) {
		t.Fatalf("FindStaleTimeFacts() = %#v, want %#v", stale, want)
	}
}

func TestFindStaleTimeFactsLeavesDurableFacts(t *testing.T) {
	summary := RunningSummary{
		UserFacts: []string{
			"用户喜欢吃西瓜",
			"用户喜欢绿龙战队，认为他们很强",
			"用户目前没有工作，每天都是周末状态",
			"用户在 2026-07-06 晚上切了西瓜",
		},
	}

	if stale := FindStaleTimeFacts(summary); len(stale) != 0 {
		t.Fatalf("FindStaleTimeFacts() = %#v, want nothing flagged", stale)
	}
}

// "周末" contains no relative anchor and "刚好" is not a time word at all;
// flagging either would delete durable facts.
func TestFindStaleTimeFactsAvoidsFalsePositives(t *testing.T) {
	summary := RunningSummary{
		UserFacts: []string{
			"用户每天都是周末状态",
			"用户觉得这个尺寸刚好合适",
			"用户的作息现今已调整",
		},
	}

	if stale := FindStaleTimeFacts(summary); len(stale) != 0 {
		t.Fatalf("FindStaleTimeFacts() = %#v, want nothing flagged", stale)
	}
}

func TestPruneStaleTimeFactsRemovesOnlyFlagged(t *testing.T) {
	summary := RunningSummary{
		SessionGoal: "keep me",
		UserFacts: []string{
			"用户昵称叫鸡翅",
			"用户刚切了西瓜在吃，白籽，超级甜",
			"用户喜欢看CS比赛",
		},
		OpenLoops: []string{"用户未回答西瓜是什么品种"},
	}

	pruned, removed := PruneStaleTimeFacts(summary)

	wantFacts := []string{"用户昵称叫鸡翅", "用户喜欢看CS比赛"}
	if !reflect.DeepEqual(pruned.UserFacts, wantFacts) {
		t.Fatalf("pruned.UserFacts = %#v, want %#v", pruned.UserFacts, wantFacts)
	}
	if len(removed) != 1 || removed[0] != "用户刚切了西瓜在吃，白籽，超级甜" {
		t.Fatalf("removed = %#v, want the watermelon fact", removed)
	}
	if pruned.SessionGoal != "keep me" {
		t.Fatalf("pruned.SessionGoal = %q, want it untouched", pruned.SessionGoal)
	}
	// open_loops are questions, not asserted facts; they do not misdate anything.
	if len(pruned.OpenLoops) != 1 {
		t.Fatalf("pruned.OpenLoops = %#v, want them untouched", pruned.OpenLoops)
	}
}

func TestPruneStaleTimeFactsDoesNotMutateInput(t *testing.T) {
	summary := RunningSummary{UserFacts: []string{"用户刚切了西瓜在吃", "用户喜欢看CS比赛"}}

	PruneStaleTimeFacts(summary)

	if len(summary.UserFacts) != 2 {
		t.Fatalf("input was mutated: %#v", summary.UserFacts)
	}
}

func openStaleFactTestDB(t *testing.T) *storage.DB {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := storage.Open(filepath.Join(t.TempDir(), "stale.db"), logger)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedPollutedSession(t *testing.T, db *storage.DB, sessionID string) config.ContextConfig {
	t.Helper()
	ctx := stdcontext.Background()
	if err := db.CreateSession(ctx, sessionID, "Xia"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	cfg := config.DefaultConfig().Context
	state := defaultContextState(cfg)
	state.RunningSummary = RunningSummary{
		UserFacts: []string{"用户昵称叫鸡翅", "用户刚切了西瓜在吃，白籽，超级甜"},
	}
	if err := UpdateSessionContextState(ctx, db, sessionID, state); err != nil {
		t.Fatalf("UpdateSessionContextState: %v", err)
	}
	return cfg
}

func loadFacts(t *testing.T, db *storage.DB, sessionID string, cfg config.ContextConfig) []string {
	t.Helper()
	state, err := LoadSessionState(stdcontext.Background(), db, sessionID, cfg)
	if err != nil {
		t.Fatalf("LoadSessionState: %v", err)
	}
	return state.RunningSummary.UserFacts
}

// A dry run must be able to show the damage without touching the database, so
// the report can be read before anything is destroyed.
func TestCleanStaleTimeFactsDryRunReportsWithoutWriting(t *testing.T) {
	db := openStaleFactTestDB(t)
	cfg := seedPollutedSession(t, db, "session-1")

	results, err := CleanStaleTimeFacts(stdcontext.Background(), db, []string{"session-1"}, cfg, false)
	if err != nil {
		t.Fatalf("CleanStaleTimeFacts: %v", err)
	}

	if len(results) != 1 || len(results[0].Removed) != 1 {
		t.Fatalf("results = %#v, want one session with one stale fact", results)
	}
	if results[0].Removed[0] != "用户刚切了西瓜在吃，白籽，超级甜" {
		t.Fatalf("Removed = %#v, want the watermelon fact", results[0].Removed)
	}
	if got := loadFacts(t, db, "session-1", cfg); len(got) != 2 {
		t.Fatalf("dry run wrote to the database: %#v", got)
	}
}

func TestCleanStaleTimeFactsApplyPersists(t *testing.T) {
	db := openStaleFactTestDB(t)
	cfg := seedPollutedSession(t, db, "session-1")

	if _, err := CleanStaleTimeFacts(stdcontext.Background(), db, []string{"session-1"}, cfg, true); err != nil {
		t.Fatalf("CleanStaleTimeFacts: %v", err)
	}

	got := loadFacts(t, db, "session-1", cfg)
	if len(got) != 1 || got[0] != "用户昵称叫鸡翅" {
		t.Fatalf("user_facts = %#v, want only the durable fact left", got)
	}
}

func TestCleanStaleTimeFactsSkipsCleanSessions(t *testing.T) {
	db := openStaleFactTestDB(t)
	ctx := stdcontext.Background()
	cfg := config.DefaultConfig().Context
	if err := db.CreateSession(ctx, "session-clean", "Xia"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	state := defaultContextState(cfg)
	state.RunningSummary = RunningSummary{UserFacts: []string{"用户喜欢看CS比赛"}}
	if err := UpdateSessionContextState(ctx, db, "session-clean", state); err != nil {
		t.Fatalf("UpdateSessionContextState: %v", err)
	}

	results, err := CleanStaleTimeFacts(ctx, db, []string{"session-clean"}, cfg, true)
	if err != nil {
		t.Fatalf("CleanStaleTimeFacts: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %#v, want clean sessions omitted from the report", results)
	}
}

// Time-of-day words are not deictic: "凌晨" names a clock position, so it only
// misdates when it describes a single event. A habitual fact using the same word
// is durable and deleting it would lose a real memory. All four strings below
// are real content from sessions.metadata.
func TestFindStaleTimeFactsKeepsHabitualTimeOfDay(t *testing.T) {
	summary := RunningSummary{
		UserFacts: []string{
			"用户经常凌晨不睡，需要霞催促睡觉",
			"用户总是熬夜到凌晨",
			"用户每天晚上都要看视频",
		},
	}

	if stale := FindStaleTimeFacts(summary); len(stale) != 0 {
		t.Fatalf("FindStaleTimeFacts() = %#v, want habitual facts kept", stale)
	}
}

func TestFindStaleTimeFactsFlagsOneOffTimeOfDay(t *testing.T) {
	summary := RunningSummary{
		UserFacts: []string{"用户凌晨两点多还没睡，在发图片"},
	}

	if stale := FindStaleTimeFacts(summary); len(stale) != 1 {
		t.Fatalf("FindStaleTimeFacts() = %#v, want the one-off event flagged", stale)
	}
}

// A habitual marker must not rescue a fact that is genuinely pinned to a past
// moment by a deictic word.
func TestFindStaleTimeFactsFlagsDeicticDespiteHabitualMarker(t *testing.T) {
	summary := RunningSummary{
		UserFacts: []string{"用户这几天因暴雨一直躺着刷视频，作息颠倒，凌晨三点还不睡"},
	}

	if stale := FindStaleTimeFacts(summary); len(stale) != 1 {
		t.Fatalf("FindStaleTimeFacts() = %#v, want the deictic fact flagged", stale)
	}
}
