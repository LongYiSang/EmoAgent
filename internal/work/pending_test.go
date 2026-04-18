package work

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/protocol"
)

func samplePaused(taskID string) *PausedWork {
	return &PausedWork{
		TaskID: taskID,
		Brief: protocol.TaskBrief{
			TaskID: taskID,
			Goal:   "sample",
		},
	}
}

func TestPendingRegistry_PutTake(t *testing.T) {
	reg := NewPendingRegistry(5 * time.Minute)
	reg.Put("s1", "t1", samplePaused("t1"))

	got := reg.Take("s1", "t1")
	if got == nil || got.TaskID != "t1" {
		t.Fatalf("Take returned %#v, want task t1", got)
	}
}

func TestPendingRegistry_TakeMissing(t *testing.T) {
	reg := NewPendingRegistry(5 * time.Minute)
	if got := reg.Take("s1", "missing"); got != nil {
		t.Fatalf("Take on missing key returned %#v, want nil", got)
	}
}

func TestPendingRegistry_DoubleTake(t *testing.T) {
	reg := NewPendingRegistry(5 * time.Minute)
	reg.Put("s1", "t1", samplePaused("t1"))
	if reg.Take("s1", "t1") == nil {
		t.Fatal("first Take should return entry")
	}
	if reg.Take("s1", "t1") != nil {
		t.Fatal("second Take should return nil")
	}
}

func TestPendingRegistry_ListDoesNotRemove(t *testing.T) {
	reg := NewPendingRegistry(5 * time.Minute)
	reg.Put("s1", "t1", samplePaused("t1"))
	reg.Put("s1", "t2", samplePaused("t2"))
	reg.Put("s2", "t3", samplePaused("t3"))

	got := reg.List("s1")
	if len(got) != 2 {
		t.Fatalf("List returned %d entries, want 2", len(got))
	}
	if reg.Take("s1", "t1") == nil || reg.Take("s1", "t2") == nil {
		t.Fatal("List should not remove entries")
	}
}

func TestPendingRegistry_ExpireOnce(t *testing.T) {
	reg := NewPendingRegistry(25 * time.Millisecond)
	reg.Put("s1", "t1", samplePaused("t1"))
	time.Sleep(35 * time.Millisecond)

	if removed := reg.ExpireOnce(); removed != 1 {
		t.Fatalf("ExpireOnce removed %d entries, want 1", removed)
	}
	if got := reg.Take("s1", "t1"); got != nil {
		t.Fatalf("expired entry should be unavailable, got %#v", got)
	}
}

func TestPendingRegistry_ConcurrentAccess(t *testing.T) {
	reg := NewPendingRegistry(1 * time.Minute)
	const goroutines = 8
	const perG = 50
	var wg sync.WaitGroup

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			session := fmt.Sprintf("s-%d", g)
			for i := 0; i < perG; i++ {
				task := fmt.Sprintf("t-%d-%d", g, i)
				reg.Put(session, task, samplePaused(task))
				_ = reg.List(session)
				_ = reg.Take(session, task)
			}
		}(g)
	}
	wg.Wait()
}
