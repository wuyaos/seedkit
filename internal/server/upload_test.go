package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wuyaos/seedkit/internal/config"
	"github.com/wuyaos/seedkit/internal/fsroot"
	"github.com/wuyaos/seedkit/internal/results"
	"github.com/wuyaos/seedkit/internal/task"
	"github.com/wuyaos/seedkit/internal/upload"
)

func uploadServer(t *testing.T, endpoint string) *Server {
	t.Helper()
	dir := t.TempDir()
	out, err := fsroot.OpenSingle(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { out.Close() })
	cfg := config.Default(dir)
	cfg.Upload.Picgo.Endpoint = endpoint
	cfg.Upload.Picgo.APIKey = "test-only"
	return &Server{cfg: cfg, dataDir: dir, output: out, tasks: task.New(1, 20, dir), results: results.Open(dir, 10), uploadEngine: upload.NewEngine(filepath.Join(dir, "upload-cache.json")), log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}
func waitUpload(t *testing.T, s *Server, id string) *task.Task {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		v, ok := s.tasks.Get(id)
		if ok && (v.Status == task.Done || v.Status == task.Failed || v.Status == task.Cancelled) {
			// Terminal status is visible just before the manager's final atomic save.
			b, _ := os.ReadFile(filepath.Join(s.dataDir, "tasks.json"))
			var persisted []task.Task
			_ = json.Unmarshal(b, &persisted)
			for _, item := range persisted {
				if item.ID == id && item.Status == v.Status {
					return v
				}
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("upload did not finish")
	return nil
}
func postUpload(t *testing.T, s *Server, body string, reupload bool) *task.Task {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	if reupload {
		s.handleReupload(w, req)
	} else {
		s.handleUpload(w, req)
	}
	if w.Code != 202 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var v task.Task
	if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil {
		t.Fatal(e)
	}
	return waitUpload(t, s, v.ID)
}
func TestUploadPartialFailureRetryAndOrderedMerge(t *testing.T) {
	var mu sync.Mutex
	counts := map[string]int{}
	fail := true
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		f, h, err := r.FormFile("source")
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		f.Close()
		mu.Lock()
		counts[h.Filename]++
		bad := h.Filename == "bad.png" && fail
		mu.Unlock()
		if bad {
			w.WriteHeader(401)
			return
		}
		if h.Filename == "slow.png" {
			time.Sleep(30 * time.Millisecond)
		}
		fmt.Fprintf(w, `{"status_code":200,"image":{"url":"https://cdn.test/%s","thumb":{"url":"https://cdn.test/t/%s"}}}`, h.Filename, h.Filename)
	}))
	defer host.Close()
	s := uploadServer(t, host.URL)
	names := []string{"slow.png", "bad.png", "fast.png"}
	shots := []map[string]any{}
	for _, name := range names {
		p := filepath.Join(s.dataDir, name)
		if err := os.WriteFile(p, []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
		shots = append(shots, map[string]any{"path": p, "web": "/api/output/" + name, "url": "", "custom": "keep"})
	}
	rec, err := s.results.Add("screenshot", "data/movie", "movie", "old", map[string]any{"shots": shots, "metadata": "keep"})
	if err != nil {
		t.Fatal(err)
	}
	v := postUpload(t, s, fmt.Sprintf(`{"ID":%q}`, rec.ID), true)
	if v.Status != task.Failed || len(v.Result) == 0 {
		t.Fatalf("partial failure lost result: %+v", v)
	}
	check := func() {
		r, _ := s.results.Get(rec.ID)
		var data struct {
			Shots    []map[string]any `json:"shots"`
			Uploads  []upload.Result  `json:"uploads"`
			Metadata string           `json:"metadata"`
		}
		if e := json.Unmarshal(r.Data, &data); e != nil {
			t.Fatal(e)
		}
		if data.Metadata != "keep" || len(data.Shots) != 3 || len(data.Uploads) != 3 {
			t.Fatalf("metadata/order lost: %s", r.Data)
		}
		for i, sh := range data.Shots {
			if sh["custom"] != "keep" || data.Uploads[i].Index != i {
				t.Fatal("index/unknown field lost")
			}
			if i != 1 && sh["url"] != "https://cdn.test/"+names[i] {
				t.Fatalf("completion-order mismatch: %v", sh)
			}
		}
	}
	check()
	mu.Lock()
	fail = false
	mu.Unlock()
	v = postUpload(t, s, fmt.Sprintf(`{"ID":%q}`, rec.ID), true)
	if v.Status != task.Done {
		t.Fatalf("retry=%+v", v)
	}
	check()
	mu.Lock()
	if counts["slow.png"] != 1 || counts["fast.png"] != 1 || counts["bad.png"] != 2 {
		t.Errorf("retry repeated successful rows: %v", counts)
	}
	mu.Unlock()
	v = postUpload(t, s, fmt.Sprintf(`{"ID":%q,"All":true}`, rec.ID), true)
	if v.Status != task.Done {
		t.Fatal(v.Error)
	}
	mu.Lock()
	if counts["bad.png"] != 2 {
		t.Error("cached all reuploaded")
	}
	mu.Unlock()
	v = postUpload(t, s, fmt.Sprintf(`{"ID":%q,"All":true,"Force":true}`, rec.ID), true)
	if v.Status != task.Done {
		t.Fatal(v.Error)
	}
	mu.Lock()
	if counts["slow.png"] != 2 || counts["bad.png"] != 3 || counts["fast.png"] != 2 {
		t.Errorf("force failed: %v", counts)
	}
	mu.Unlock()
}
func TestUploadRejectsInvalidInputWithoutShortening(t *testing.T) {
	s := uploadServer(t, "http://127.0.0.1:1")
	if err := os.WriteFile(filepath.Join(s.dataDir, "ok.png"), []byte("ok"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"Files":[]}`, `{"Files":["ok.png","../secret"]}`, `{"Files":["missing"]}`, `{"Files":["."]}`, `{"Files":["ok.png"],"Host":"mteam"}`} {
		w := httptest.NewRecorder()
		s.handleUpload(w, httptest.NewRequest("POST", "/", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("accepted %s: %d", body, w.Code)
		}
	}
	if len(s.tasks.List()) != 0 {
		t.Fatal("invalid input queued partial batch")
	}
}
func TestUploadCancelRetainsSuccessfulRows(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseMultipartForm(1 << 20)
		f, h, e := r.FormFile("source")
		if e != nil {
			w.WriteHeader(400)
			return
		}
		f.Close()
		if h.Filename == "block.png" {
			once.Do(func() { close(started) })
			select {
			case <-r.Context().Done():
				return
			case <-release:
			}
		}
		fmt.Fprint(w, `{"status_code":200,"image":{"url":"https://cdn.test/ok"}}`)
	}))
	defer host.Close()
	defer close(release)
	s := uploadServer(t, host.URL)
	s.cfg.Upload.Concurrency = 1
	for _, name := range []string{"ok.png", "block.png"} {
		os.WriteFile(filepath.Join(s.dataDir, name), []byte(name), 0600)
	}
	w := httptest.NewRecorder()
	s.handleUpload(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"Files":["ok.png","block.png"]}`)))
	if w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	var queued task.Task
	json.Unmarshal(w.Body.Bytes(), &queued)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("not started")
	}
	s.tasks.Cancel(queued.ID)
	v := waitUpload(t, s, queued.ID)
	var rows []upload.Result
	json.Unmarshal(v.Result, &rows)
	if v.Status != task.Cancelled || len(rows) != 2 || rows[0].URL == "" || rows[1].Status != "cancelled" {
		t.Fatalf("cancel lost result: %+v rows=%+v", v, rows)
	}
	// Confirm cancellation still reports an error while preserving rows.
	if uploadOutcome(context.Background(), rows) == nil {
		t.Fatal("cancelled row reported success")
	}
}
func TestSecretMaskDoesNotRevealShortCredential(t *testing.T) {
	for _, v := range []string{"a", "ab", "abcd"} {
		if got := maskSecret(v); got != "••••" {
			t.Fatalf("short key exposed %q", got)
		}
	}
}
