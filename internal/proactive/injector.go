package proactive

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/storage"
)

// AmbientStore supplies the candidates the injector renders.
type AmbientStore interface {
	ListProactiveCandidates(ctx context.Context, filter storage.ProactiveCandidateFilter) ([]storage.ProactiveCandidateRecord, error)
}

// Injector turns candidates the gate declined into context for the next turn the
// user starts.
//
// This is what makes the feature worth having even when the gate never lets it
// speak: the agent did not interrupt, but when the user comes back it knows what
// they were doing. It is also the whole value proposition if the user decides to
// keep proactive messaging switched off.
type Injector struct {
	store    AmbientStore
	maxChars int
	maxItems int
	now      func() time.Time
}

func NewInjector(store AmbientStore) *Injector {
	return &Injector{store: store, maxChars: 1200, maxItems: 8, now: time.Now}
}

// SetBudget overrides the rendering limits. The block competes with memory and
// history for the context window, so it stays small by default.
func (i *Injector) SetBudget(maxChars, maxItems int) {
	if i == nil {
		return
	}
	if maxChars > 0 {
		i.maxChars = maxChars
	}
	if maxItems > 0 {
		i.maxItems = maxItems
	}
}

// SetClock overrides the injector's clock. Test hook.
func (i *Injector) SetClock(now func() time.Time) {
	if i != nil && now != nil {
		i.now = now
	}
}

// Block renders the ambient activity summary for a persona, or "" when there is
// nothing recent worth mentioning.
func (i *Injector) Block(ctx context.Context, cfg config.ProactiveConfig, personaKey string) string {
	if i == nil || i.store == nil {
		return ""
	}
	cfg = config.NormalizeProactiveConfig(cfg)
	since := i.now().Add(-time.Duration(cfg.Candidates.TTLHours) * time.Hour)

	records, err := i.store.ListProactiveCandidates(ctx, storage.ProactiveCandidateFilter{
		PersonaKey: personaKey,
		Statuses: []string{
			storage.ProactiveCandidateStatusPending,
			storage.ProactiveCandidateStatusSkipped,
		},
		Since: since,
		Limit: i.maxItems,
	})
	if err != nil || len(records) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("[最近的动静]\n")
	b.WriteString("这段时间用户在做的事（你只是知道，不必主动提起，除非自然聊到）：\n")
	body := b.Len()

	for _, record := range records {
		summary := strings.TrimSpace(record.Summary)
		if summary == "" {
			continue
		}
		prefix := formatObservedRange(record)
		remaining := i.maxChars - b.Len() - len(prefix) - 3 // "- " and "\n"
		if remaining <= 0 {
			break
		}
		// Truncate an over-long summary rather than dropping it: recent activity
		// the agent cannot see at all is worse than activity it sees in part.
		summary = truncateRunes(summary, remaining)
		if summary == "" {
			break
		}
		fmt.Fprintf(&b, "- %s%s\n", prefix, summary)
	}
	if b.Len() == body {
		return ""
	}
	return strings.TrimRight(b.String(), "\n")
}

// truncateRunes cuts text to at most maxBytes without splitting a rune, which
// matters because these summaries are usually Chinese.
func truncateRunes(text string, maxBytes int) string {
	if len(text) <= maxBytes {
		return text
	}
	if maxBytes <= 3 {
		return ""
	}
	limit := maxBytes - 3 // room for the ellipsis
	cut := 0
	for index := range text {
		if index > limit {
			break
		}
		cut = index
	}
	if cut == 0 {
		return ""
	}
	return text[:cut] + "..."
}

func formatObservedRange(record storage.ProactiveCandidateRecord) string {
	from := parseTimeOrZero(record.ObservedFrom)
	to := parseTimeOrZero(record.ObservedTo)
	if from.IsZero() || to.IsZero() {
		return ""
	}
	return fmt.Sprintf("%s–%s ", from.Format("15:04"), to.Format("15:04"))
}
