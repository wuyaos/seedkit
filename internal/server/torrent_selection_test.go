package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/autobrr/go-torrent/metainfo"
	"github.com/wuyaos/seedkit/internal/auth"
	"github.com/wuyaos/seedkit/internal/fsroot"
	"github.com/wuyaos/seedkit/internal/task"
	"github.com/wuyaos/seedkit/internal/torrent"
)

func torrentServer(t *testing.T) *Server {
	t.Helper()
	s := uploadServer(t, "")
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Movie", "Sample"), 0755); err != nil {
		t.Fatal(err)
	}
	for p, body := range map[string]string{"Movie/main.mkv": "movie bytes", "Movie/sub.srt": "subtitle", "Movie/Sample/clip.mkv": "sample", "Movie/download.log": "log"} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(p)), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	files, errs := fsroot.New(map[string]string{"data": root})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	t.Cleanup(files.Close)
	s.files = files
	s.cfg.Paths.Roots = map[string]string{"data": root}
	s.cfg.Torrent.Exclude = []string{"*.log"}
	return s
}

func previewTorrent(t *testing.T, s *Server, body string, want int) *torrent.Preview {
	t.Helper()
	w := httptest.NewRecorder()
	s.handleTorrentPreview(w, httptest.NewRequest("POST", "/api/torrent/preview", strings.NewReader(body)))
	if w.Code != want {
		t.Fatalf("preview status=%d body=%s", w.Code, w.Body.String())
	}
	if want != 200 {
		return nil
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("preview cache header")
	}
	var p torrent.Preview
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return &p
}

func TestTorrentPreviewRulesAndValidation(t *testing.T) {
	s := torrentServer(t)
	p := previewTorrent(t, s, `{"Path":"data/Movie"}`, 200)
	count := 0
	for _, f := range p.Files {
		if f.Reason == "" {
			count++
		}
	}
	if count != 3 || !p.IsDir || p.Snapshot == "" {
		t.Fatalf("unexpected preview: %+v", p)
	}
	p2 := previewTorrent(t, s, `{"Path":"data/Movie","Exclude":[]}`, 200)
	count = 0
	for _, f := range p2.Files {
		if f.Reason == "" {
			count++
		}
	}
	if count != 4 || p.Snapshot == p2.Snapshot {
		t.Fatal("empty rules must override defaults and change snapshot")
	}
	previewTorrent(t, s, `{"Path":"data/Movie","Exclude":["["]}`, 400)
	previewTorrent(t, s, `{"Path":"unknown/Movie"}`, 404)
	selected := torrentReq{Path: "data/Movie", Selection: &torrent.Selection{Snapshot: p.Snapshot, Paths: []string{"main.mkv", "sub.srt"}}}
	abs, err := s.files.Resolve(selected.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.validateTorrentSelection(context.Background(), abs, selected); err != nil {
		t.Fatal(err)
	}
	for _, paths := range [][]string{nil, {"../outside"}, {"main.mkv", "main.mkv"}, {"download.log"}} {
		selected.Selection.Paths = paths
		if err = s.validateTorrentSelection(context.Background(), abs, selected); err == nil {
			t.Fatalf("accepted invalid selection %v", paths)
		}
	}
	selected.Selection.Paths = []string{"main.mkv"}
	if err = os.WriteFile(filepath.Join(abs, "new.txt"), []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	if err = s.validateTorrentSelection(context.Background(), abs, selected); err == nil {
		t.Fatal("new file must invalidate preview")
	}
}

func TestTorrentSelectedAPIProducesExactFiles(t *testing.T) {
	s := torrentServer(t)
	p := previewTorrent(t, s, `{"Path":"data/Movie"}`, 200)
	in := torrentReq{Path: "data/Movie", Selection: &torrent.Selection{Snapshot: p.Snapshot, Paths: []string{"sub.srt", "main.mkv"}}}
	body, _ := json.Marshal(in)
	w := httptest.NewRecorder()
	s.handleTorrent(w, httptest.NewRequest("POST", "/api/torrent", bytes.NewReader(body)))
	if w.Code != 202 {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
	var v task.Task
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	done := waitUpload(t, s, v.ID)
	if done.Status != task.Done {
		t.Fatalf("task failed: %+v", done)
	}
	records := s.results.ForPath("data/Movie")["torrent"]
	if len(records) != 1 {
		t.Fatalf("records: %+v", records)
	}
	var out struct {
		Torrent   torrent.Result    `json:"torrent"`
		Selection torrent.Selection `json:"selection"`
	}
	if err := json.Unmarshal(records[0].Data, &out); err != nil {
		t.Fatal(err)
	}
	mi, err := metainfo.LoadFromFile(out.Torrent.Path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range info.Files {
		names = append(names, strings.Join(f.Path, "/"))
	}
	if strings.Join(names, ",") != "main.mkv,sub.srt" || out.Selection.Snapshot != p.Snapshot {
		t.Fatalf("actual torrent differs: %v", names)
	}
}

func TestTorrentPreviewAuthentication(t *testing.T) {
	s := torrentServer(t)
	s.auth = auth.New("", "", time.Hour)
	if _, err := s.auth.SetCredentials("tester", "test-password"); err != nil {
		t.Fatal(err)
	}
	s.routes()
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/torrent/preview", strings.NewReader(`{"Path":"data/Movie"}`)))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized preview: %d", w.Code)
	}
}
