// Package task is the only cross-module channel: it queues heavy jobs,
// propagates cancellation and fans progress events out to SSE subscribers.
package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Status string

const (
	Queued    Status = "queued"
	Running   Status = "running"
	Done      Status = "done"
	Failed    Status = "failed"
	Cancelled Status = "cancelled"
)

type Task struct {
	ID       string          `json:"id"`
	Kind     string          `json:"kind"` // mediainfo|bdinfo|screenshot|torrent|fileop
	Title    string          `json:"title"`
	Status   Status          `json:"status"`
	Progress float64         `json:"progress"` // 0..1
	Message  string          `json:"message"`
	Error    string          `json:"error,omitempty"`
	Heavy    bool            `json:"heavy"`
	Created  time.Time       `json:"created"`
	Started  *time.Time      `json:"started,omitempty"`
	Finished *time.Time      `json:"finished,omitempty"`
	Result   json.RawMessage `json:"result,omitempty"`
	Log      []string        `json:"log,omitempty"`
	cancel   context.CancelFunc
}

// Event is what SSE subscribers receive.
type Event struct {
	Type string `json:"type"` // task
	Task *Task  `json:"task"`
}

// Reporter is handed to job functions to publish progress.
type Reporter interface {
	ID() string
	Progress(p float64, msg string)
	Logf(format string, a ...any)
}

type Job func(ctx context.Context, rep Reporter) (result any, err error)

type Manager struct {
	mu       sync.Mutex
	saveMu   sync.Mutex
	tasks    map[string]*Task
	order    []string
	heavySem chan struct{}
	subs     map[chan Event]struct{}
	keep     int
	persist  string
	seq      int64
}

func New(heavyConcurrency, historyKeep int, persistDir string) *Manager {
	m := &Manager{
		tasks:    map[string]*Task{},
		heavySem: make(chan struct{}, heavyConcurrency),
		subs:     map[chan Event]struct{}{},
		keep:     historyKeep,
	}
	if persistDir != "" {
		m.persist = filepath.Join(persistDir, "tasks.json")
		m.load()
		m.trimLocked()
	}
	return m
}

func (m *Manager) Submit(kind, title string, heavy bool, job Job) *Task {
	m.mu.Lock()
	m.seq++
	t := &Task{
		ID: fmt.Sprintf("%d-%d", time.Now().Unix(), m.seq), Kind: kind, Title: title,
		Status: Queued, Heavy: heavy, Created: time.Now(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	m.tasks[t.ID] = t
	m.order = append(m.order, t.ID)
	m.trimLocked()
	queued := *t
	queued.cancel = nil
	m.mu.Unlock()
	m.emit(t)

	go m.run(ctx, t, job)
	return &queued
}

func (m *Manager) run(ctx context.Context, t *Task, job Job) {
	if t.Heavy {
		select {
		case m.heavySem <- struct{}{}:
			defer func() { <-m.heavySem }()
		case <-ctx.Done():
			m.finish(t, nil, ctx.Err())
			return
		}
	}
	if err := ctx.Err(); err != nil {
		m.finish(t, nil, err)
		return
	}
	now := time.Now()
	m.update(t, func() { t.Status = Running; t.Started = &now })
	rep := &reporter{m: m, t: t}
	rep.Logf("开始 · %s", t.Title)
	res, err := func() (res any, err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("内部错误: %v", r)
			}
		}()
		return job(ctx, rep)
	}()
	m.finish(t, res, err)
}

func (m *Manager) finish(t *Task, res any, err error) {
	now := time.Now()
	rep := &reporter{m: m, t: t}
	switch {
	case err == nil:
		if t.Started != nil {
			rep.Logf("完成 · 用时 %s", now.Sub(*t.Started).Round(time.Millisecond))
		} else {
			rep.Logf("完成")
		}
	case errors.Is(err, context.Canceled):
		rep.Logf("已取消")
	default:
		rep.Logf("失败 · %v", err)
	}
	m.update(t, func() {
		t.Finished = &now
		switch {
		case err == nil:
			t.Status, t.Progress = Done, 1
			if res != nil {
				if b, e := json.Marshal(res); e == nil {
					t.Result = b
				}
			}
		case errors.Is(err, context.Canceled):
			t.Status, t.Error = Cancelled, "cancelled"
		default:
			t.Status, t.Error = Failed, err.Error()
		}
	})
	m.mu.Lock()
	m.trimLocked()
	m.mu.Unlock()
	m.save()
}

func (m *Manager) update(t *Task, fn func()) {
	m.mu.Lock()
	fn()
	m.mu.Unlock()
	m.emit(t)
}

func (m *Manager) Cancel(id string) bool {
	m.mu.Lock()
	t, ok := m.tasks[id]
	m.mu.Unlock()
	if !ok || t.cancel == nil {
		return false
	}
	t.cancel()
	return true
}

func (m *Manager) Get(id string) (*Task, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok {
		return nil, false
	}
	c := *t
	return &c, true
}

// List returns tasks newest first.
func (m *Manager) List() []*Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Task, 0, len(m.order))
	for i := len(m.order) - 1; i >= 0; i-- {
		c := *m.tasks[m.order[i]]
		out = append(out, &c)
	}
	return out
}

func (m *Manager) Delete(id string) {
	m.mu.Lock()
	if t, ok := m.tasks[id]; ok && (t.Status == Done || t.Status == Failed || t.Status == Cancelled) {
		delete(m.tasks, id)
		for i, v := range m.order {
			if v == id {
				m.order = append(m.order[:i], m.order[i+1:]...)
				break
			}
		}
	}
	m.mu.Unlock()
	m.save()
}

func (m *Manager) Subscribe() (ch chan Event, unsub func()) {
	ch = make(chan Event, 64)
	m.mu.Lock()
	m.subs[ch] = struct{}{}
	m.mu.Unlock()
	return ch, func() {
		m.mu.Lock()
		delete(m.subs, ch)
		m.mu.Unlock()
	}
}

func (m *Manager) emit(t *Task) {
	m.mu.Lock()
	c := *t
	c.cancel = nil
	for ch := range m.subs {
		select {
		case ch <- Event{Type: "task", Task: &c}:
		default: // slow subscriber: drop, it can refetch via List
		}
	}
	m.mu.Unlock()
}

func (m *Manager) trimLocked() {
	finished := 0
	for _, id := range m.order {
		s := m.tasks[id].Status
		if s == Done || s == Failed || s == Cancelled {
			finished++
		}
	}
	kept := m.order[:0]
	for _, id := range m.order {
		s := m.tasks[id].Status
		if finished > m.keep && (s == Done || s == Failed || s == Cancelled) {
			delete(m.tasks, id)
			finished--
		} else {
			kept = append(kept, id)
		}
	}
	m.order = kept
}

type reporter struct {
	m *Manager
	t *Task
}

func (r *reporter) ID() string { return r.t.ID }

func (r *reporter) Progress(p float64, msg string) {
	r.m.update(r.t, func() {
		if p >= 0 {
			r.t.Progress = p
		}
		if msg != "" {
			r.t.Message = msg
		}
	})
}

func (r *reporter) Logf(format string, a ...any) {
	line := time.Now().Format("15:04:05 ") + fmt.Sprintf(format, a...)
	r.m.update(r.t, func() {
		r.t.Log = append(r.t.Log, line)
		if len(r.t.Log) > 500 {
			r.t.Log = r.t.Log[len(r.t.Log)-500:]
		}
	})
}

// persistence: only finished tasks survive restarts.
func (m *Manager) save() {
	if m.persist == "" {
		return
	}
	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	m.mu.Lock()
	list := make([]*Task, 0, len(m.order))
	for _, id := range m.order {
		t := m.tasks[id]
		if t.Status == Done || t.Status == Failed || t.Status == Cancelled {
			c := *t
			c.cancel = nil
			list = append(list, &c)
		}
	}
	m.mu.Unlock()
	b, err := json.Marshal(list)
	if err != nil {
		return
	}
	_ = os.WriteFile(m.persist+".tmp", b, 0o644)
	_ = os.Rename(m.persist+".tmp", m.persist)
}

func (m *Manager) load() {
	b, err := os.ReadFile(m.persist)
	if err != nil {
		return
	}
	var list []*Task
	if json.Unmarshal(b, &list) != nil {
		return
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Created.Before(list[j].Created) })
	for _, t := range list {
		m.tasks[t.ID] = t
		m.order = append(m.order, t.ID)
	}
}