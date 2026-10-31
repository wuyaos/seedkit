// Package results keeps artefacts (MediaInfo text, BDInfo report, screenshot
// set, torrent) indexed by the file they were produced from, independent of
// the rolling task log. results.json survives task cleanup; files under the
// output dir are only removed when the user deletes a record with files=1.
package results

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Record struct {
	ID    string          `json:"id"`
	Kind  string          `json:"kind"` // mediainfo|bdinfo|screenshot|torrent
	Path  string          `json:"path"` // virtual path of the source
	Title string          `json:"title"`
	Time  time.Time       `json:"time"`
	Task  string          `json:"task,omitempty"`
	Data  json.RawMessage `json:"data"`
}

type Store struct {
	mu   sync.Mutex
	file string
	keep int
	recs map[string]*Record // id -> record
	seq  int64
}

var ErrNotFound = errors.New("result not found")

// Open loads results.json from dir. keep is the number of records retained
// per (path, kind); older ones are dropped on Add.
func Open(dir string, keep int) *Store {
	if keep <= 0 {
		keep = 10
	}
	s := &Store{file: filepath.Join(dir, "results.json"), keep: keep, recs: map[string]*Record{}}
	b, err := os.ReadFile(s.file)
	if err == nil {
		var list []*Record
		if json.Unmarshal(b, &list) == nil {
			for _, r := range list {
				s.recs[r.ID] = r
			}
		}
	}
	return s
}

func (s *Store) Add(kind, vpath, title, taskID string, data any) (*Record, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	r := &Record{ID: fmt.Sprintf("%d-%d", time.Now().UnixNano()/1e6, s.seq), Kind: kind, Path: vpath, Title: title, Time: time.Now(), Task: taskID, Data: b}
	s.recs[r.ID] = r
	s.trimLocked(vpath, kind)
	return r, s.saveLocked()
}

// Update rewrites the data of a record in place.
func (s *Store) Update(id string, fn func(data json.RawMessage) (any, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.recs[id]
	if !ok {
		return ErrNotFound
	}
	v, err := fn(r.Data)
	if err != nil {
		return err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	r.Data = b
	return s.saveLocked()
}

func (s *Store) Get(id string) (*Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.recs[id]
	if !ok {
		return nil, false
	}
	c := *r
	return &c, true
}

func (s *Store) Delete(id string) (*Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.recs[id]
	if !ok {
		return nil, false
	}
	delete(s.recs, id)
	_ = s.saveLocked()
	return r, true
}

// ForPath returns records of every kind for one source path, newest first.
func (s *Store) ForPath(vpath string) map[string][]*Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string][]*Record{}
	for _, r := range s.recs {
		if r.Path == vpath {
			c := *r
			out[r.Kind] = append(out[r.Kind], &c)
		}
	}
	for _, l := range out {
		sort.Slice(l, func(i, j int) bool { return l[i].Time.After(l[j].Time) })
	}
	return out
}

// Recent returns the newest n records across all paths.
func (s *Store) Recent(n int) []*Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := make([]*Record, 0, len(s.recs))
	for _, r := range s.recs {
		c := *r
		all = append(all, &c)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Time.After(all[j].Time) })
	if n > 0 && len(all) > n {
		all = all[:n]
	}
	return all
}

func (s *Store) trimLocked(vpath, kind string) {
	var same []*Record
	for _, r := range s.recs {
		if r.Path == vpath && r.Kind == kind {
			same = append(same, r)
		}
	}
	if len(same) <= s.keep {
		return
	}
	sort.Slice(same, func(i, j int) bool { return same[i].Time.After(same[j].Time) })
	for _, r := range same[s.keep:] {
		delete(s.recs, r.ID)
	}
}

func (s *Store) saveLocked() error {
	list := make([]*Record, 0, len(s.recs))
	for _, r := range s.recs {
		list = append(list, r)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Time.Before(list[j].Time) })
	b, err := json.MarshalIndent(list, "", " ")
	if err != nil {
		return err
	}
	tmp := s.file + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.file)
}
