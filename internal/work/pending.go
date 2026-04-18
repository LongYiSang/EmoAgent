package work

import (
	"sync"
	"time"
)

const defaultPendingTTL = 30 * time.Minute

type pendingKey struct {
	sessionID string
	taskID    string
}

type pendingEntry struct {
	paused    *PausedWork
	expiresAt time.Time
}

// PendingRegistry stores paused work tasks awaiting an Emotion decision.
type PendingRegistry struct {
	mu      sync.RWMutex
	ttl     time.Duration
	entries map[pendingKey]pendingEntry
}

// NewPendingRegistry constructs a TTL-based in-memory paused-task registry.
func NewPendingRegistry(ttl time.Duration) *PendingRegistry {
	if ttl <= 0 {
		ttl = defaultPendingTTL
	}
	return &PendingRegistry{
		ttl:     ttl,
		entries: make(map[pendingKey]pendingEntry),
	}
}

// Put adds or replaces a paused task for the given session/task pair.
func (r *PendingRegistry) Put(sessionID, taskID string, paused *PausedWork) {
	if r == nil || sessionID == "" || taskID == "" || paused == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[pendingKey{sessionID: sessionID, taskID: taskID}] = pendingEntry{
		paused:    paused,
		expiresAt: time.Now().UTC().Add(r.ttl),
	}
}

// Take retrieves and removes a paused task for the given session/task pair.
func (r *PendingRegistry) Take(sessionID, taskID string) *PausedWork {
	if r == nil || sessionID == "" || taskID == "" {
		return nil
	}
	key := pendingKey{sessionID: sessionID, taskID: taskID}

	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.entries[key]
	if !ok {
		return nil
	}
	delete(r.entries, key)
	if entry.expiresAt.Before(time.Now().UTC()) {
		return nil
	}
	return entry.paused
}

// List returns non-expired paused tasks for one session without removing them.
func (r *PendingRegistry) List(sessionID string) []*PausedWork {
	if r == nil || sessionID == "" {
		return nil
	}
	now := time.Now().UTC()
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*PausedWork, 0, len(r.entries))
	for key, entry := range r.entries {
		if key.sessionID != sessionID {
			continue
		}
		if entry.expiresAt.Before(now) {
			continue
		}
		out = append(out, entry.paused)
	}
	return out
}

// ExpireOnce removes expired entries and returns the number of removals.
func (r *PendingRegistry) ExpireOnce() int {
	if r == nil {
		return 0
	}
	now := time.Now().UTC()
	removed := 0

	r.mu.Lock()
	defer r.mu.Unlock()
	for key, entry := range r.entries {
		if !entry.expiresAt.Before(now) {
			continue
		}
		delete(r.entries, key)
		removed++
	}
	return removed
}
