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
