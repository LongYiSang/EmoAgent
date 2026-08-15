package conversation

import (
	"context"
	"sync"
)

type RunRef struct {
	OriginKey string
	SessionID string
	Kind      string
}

type StopSelector struct {
	OriginKey string
	SessionID string
}

type RunRegistry struct {
	mu     sync.Mutex
	nextID int
	runs   map[int]registeredRun
}

type registeredRun struct {
	ref    RunRef
	cancel context.CancelFunc
}

func NewRunRegistry() *RunRegistry {
	return &RunRegistry{runs: map[int]registeredRun{}}
}

func (r *RunRegistry) Register(ref RunRef, cancel context.CancelFunc) func() {
	if r == nil || cancel == nil {
		return func() {}
	}
	r.mu.Lock()
	r.nextID++
	id := r.nextID
	r.runs[id] = registeredRun{ref: ref, cancel: cancel}
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		delete(r.runs, id)
		r.mu.Unlock()
	}
}

func (r *RunRegistry) TryRegister(ref RunRef, cancel context.CancelFunc) (func(), bool) {
	if r == nil || cancel == nil {
		return func() {}, true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, run := range r.runs {
		if sameRun(ref, run.ref) {
			return func() {}, false
		}
	}
	r.nextID++
	id := r.nextID
	r.runs[id] = registeredRun{ref: ref, cancel: cancel}
	return func() {
		r.mu.Lock()
		delete(r.runs, id)
		r.mu.Unlock()
	}, true
}

// HasActiveRuns reports whether any reply is currently in flight. Proactive
// messaging consults this before interrupting: talking over a reply the user is
// waiting for is the worst thing that feature can do.
func (r *RunRegistry) HasActiveRuns() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.runs) > 0
}

func (r *RunRegistry) Stop(selector StopSelector) int {
	if r == nil {
		return 0
	}
	var cancels []context.CancelFunc
	r.mu.Lock()
	for id, run := range r.runs {
		if !selector.matches(run.ref) {
			continue
		}
		cancels = append(cancels, run.cancel)
		delete(r.runs, id)
	}
	r.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	return len(cancels)
}

func (s StopSelector) matches(ref RunRef) bool {
	if s.OriginKey != "" && s.OriginKey != ref.OriginKey {
		return false
	}
	if s.SessionID != "" && s.SessionID != ref.SessionID {
		return false
	}
	return s.OriginKey != "" || s.SessionID != ""
}

func sameRun(a, b RunRef) bool {
	return a.OriginKey == b.OriginKey && a.SessionID == b.SessionID && a.Kind == b.Kind
}
