package context

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/longyisang/emoagent/internal/config"
)

// StaleFactCleanup reports what one session's running_summary lost.
type StaleFactCleanup struct {
	SessionID string
	Removed   []string
}

type staleFactStore interface {
	sessionStateReader
	sessionStateWriter
}

// CleanStaleTimeFacts strips self-misdating facts from the given sessions.
//
// With apply=false nothing is written, so the report can be reviewed before
// anything is destroyed. Sessions with nothing to remove are left out of the
// report entirely.
func CleanStaleTimeFacts(ctx context.Context, db staleFactStore, sessionIDs []string, cfg config.ContextConfig, apply bool) ([]StaleFactCleanup, error) {
	if db == nil {
		return nil, fmt.Errorf("session state store is required")
	}
	var results []StaleFactCleanup
	for _, sessionID := range sessionIDs {
		state, err := LoadSessionState(ctx, db, sessionID, cfg)
		if err != nil {
			return nil, fmt.Errorf("load session %s: %w", sessionID, err)
		}
		if state == nil {
			continue
		}
		pruned, removed := PruneStaleTimeFacts(state.RunningSummary)
		if len(removed) == 0 {
			continue
		}
		if apply {
			next := *state
			next.RunningSummary = pruned
			if err := UpdateSessionContextState(ctx, db, sessionID, next); err != nil {
				return nil, fmt.Errorf("update session %s: %w", sessionID, err)
			}
		}
		results = append(results, StaleFactCleanup{SessionID: sessionID, Removed: removed})
	}
	return results, nil
}

// deicticTimeWords are anchored to the moment of writing, so a fact carrying
// one lies about when it happened as soon as it is replayed. A summary item is
// read back verbatim for as long as the session lives, which is how
// "用户刚切了西瓜在吃" still claimed to be happening 36 days later.
//
// The list is deliberately narrow: a false positive silently deletes a real
// memory, which is worse than leaving one stale item behind for the summariser
// to rewrite on its next pass.
var deicticTimeWords = []string{
	"刚才", "刚刚", "刚",
	"今天", "今早", "今晚", "今日",
	"昨天", "昨晚", "昨日",
	"明天", "明早", "明晚",
	"现在", "此刻", "眼下",
	"等下", "待会", "过会",
	"前几天", "这几天",
}

// timeOfDayWords name a clock position rather than a moment relative to now.
// They only misdate when the fact describes a single event: "用户凌晨两点多还没睡"
// is undated, while "用户经常凌晨不睡" is a durable habit worth keeping.
var timeOfDayWords = []string{"凌晨", "半夜", "清晨"}

// habitualMarkers turn a fact into a description of a pattern, which no clock
// position can misdate.
var habitualMarkers = []string{"经常", "总是", "老是", "习惯", "每天", "每晚", "每次", "通常", "一般"}

// exemptPhrases contain a trigger word as a substring without being about time.
// They are scrubbed before matching because RE2 has no lookaround.
var exemptPhrases = []string{"刚好", "刚巧"}

// absoluteDatePattern marks a fact that already carries its own date, which is
// exactly the form the summariser is now asked to write.
var absoluteDatePattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2}|\d{4}年\d{1,2}月`)

// FindStaleTimeFacts returns the user_facts entries whose wording will misdate
// itself once replayed. Only user_facts are considered: open_loops are open
// questions and do_not_forget items are standing instructions, so neither
// asserts when something happened.
func FindStaleTimeFacts(summary RunningSummary) []string {
	var stale []string
	for _, fact := range summary.UserFacts {
		if isStaleTimeFact(fact) {
			stale = append(stale, fact)
		}
	}
	return stale
}

// PruneStaleTimeFacts drops the stale facts and reports what it removed.
func PruneStaleTimeFacts(summary RunningSummary) (RunningSummary, []string) {
	pruned := summary
	kept := make([]string, 0, len(summary.UserFacts))
	var removed []string
	for _, fact := range summary.UserFacts {
		if isStaleTimeFact(fact) {
			removed = append(removed, fact)
			continue
		}
		kept = append(kept, fact)
	}
	pruned.UserFacts = kept
	return pruned, removed
}

func isStaleTimeFact(fact string) bool {
	if absoluteDatePattern.MatchString(fact) {
		return false
	}
	scrubbed := fact
	for _, exempt := range exemptPhrases {
		scrubbed = strings.ReplaceAll(scrubbed, exempt, "")
	}
	for _, word := range deicticTimeWords {
		if strings.Contains(scrubbed, word) {
			return true
		}
	}
	if containsAny(scrubbed, habitualMarkers) {
		return false
	}
	return containsAny(scrubbed, timeOfDayWords)
}

func containsAny(text string, words []string) bool {
	for _, word := range words {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}
