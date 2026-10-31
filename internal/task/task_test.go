package task

import (
	"context"
	"testing"
)

func TestTrimKeepsActiveAndNewestHistory(t *testing.T) {
	m := New(1, 2, "")
	for i, s := range []Status{Running, Done, Failed, Queued, Cancelled, Done} {
		id := string(rune('a' + i))
		m.tasks[id] = &Task{ID: id, Status: s}
		m.order = append(m.order, id)
	}
	m.trimLocked()
	want := []string{"a", "d", "e", "f"}
	if len(m.order) != len(want) {
		t.Fatalf("order=%v, want=%v", m.order, want)
	}
	for i, id := range want {
		if m.order[i] != id || m.tasks[id] == nil {
			t.Fatalf("order=%v, want=%v", m.order, want)
		}
	}
	if len(m.tasks) != len(want) {
		t.Fatalf("discarded tasks still retained: %d", len(m.tasks))
	}
}

func TestFinishTrimsWithoutNewSubmission(t *testing.T) {
	m := New(1, 1, "")
	old := &Task{ID: "old", Status: Done}
	current := &Task{ID: "new", Status: Running}
	m.tasks[old.ID], m.tasks[current.ID] = old, current
	m.order = []string{old.ID, current.ID}
	m.finish(current, nil, nil)
	if _, ok := m.Get(old.ID); ok {
		t.Fatal("old history retained after completion")
	}
	if got := m.List(); len(got) != 1 || got[0].ID != current.ID || got[0].Status != Done {
		t.Fatalf("unexpected history: %+v", got)
	}
}

func TestCancelledJobDoesNotStart(t *testing.T) {
	for _, heavy := range []bool{false, true} {
		m := New(1, 1, "")
		job := &Task{ID: "cancelled", Status: Queued, Heavy: heavy}
		m.tasks[job.ID] = job
		m.order = []string{job.ID}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		called := false
		m.run(ctx, job, func(context.Context, Reporter) (any, error) {
			called = true
			return nil, nil
		})
		if called || job.Status != Cancelled || job.Started != nil {
			t.Fatalf("heavy=%v: called=%v status=%s started=%v", heavy, called, job.Status, job.Started)
		}
	}
}

func TestSubmitReturnsQueuedSnapshot(t *testing.T) {
	m := New(1, 1, "")
	finished := make(chan struct{})
	queued := m.Submit("test", "snapshot", false, func(context.Context, Reporter) (any, error) {
		close(finished)
		return nil, nil
	})
	<-finished
	if queued.Status != Queued || queued.Started != nil || queued.cancel != nil {
		t.Fatalf("submit response is not a detached snapshot: %+v", queued)
	}
}
