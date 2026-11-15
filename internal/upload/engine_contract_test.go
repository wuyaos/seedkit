package upload

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type sequencedUploader struct {
	Credential         string
	mu                 sync.Mutex
	active, max, calls int
	delays             map[string]time.Duration
}

func (*sequencedUploader) Name() string { return "sequenced" }
func (u *sequencedUploader) Upload(ctx context.Context, file string) (string, error) {
	u.mu.Lock()
	u.active++
	u.calls++
	if u.active > u.max {
		u.max = u.active
	}
	u.mu.Unlock()
	if d := u.delays[filepath.Base(file)]; d > 0 {
		time.Sleep(d)
	}
	u.mu.Lock()
	u.active--
	u.mu.Unlock()
	return "https://cdn.test/" + u.Credential + "/" + filepath.Base(file), nil
}
func TestEngineCompletionOrderAndSerializedCallbacks(t *testing.T) {
	dir := t.TempDir()
	files := []string{}
	for _, n := range []string{"slow", "fast", "middle"} {
		p := filepath.Join(dir, n)
		if e := os.WriteFile(p, []byte(n), 0600); e != nil {
			t.Fatal(e)
		}
		files = append(files, p)
	}
	u := &sequencedUploader{Credential: "a", delays: map[string]time.Duration{"slow": 40 * time.Millisecond, "fast": 0, "middle": 10 * time.Millisecond}}
	order := []int{}
	var mu sync.Mutex
	active := false
	r := NewEngine("").UploadAll(context.Background(), u, files, Options{Concurrency: 3}, func(x Result) {
		mu.Lock()
		defer mu.Unlock()
		if active {
			t.Error("callbacks overlapped")
		}
		active = true
		order = append(order, x.Index)
		active = false
	})
	if u.max < 2 {
		t.Fatalf("uploads not concurrent: %d", u.max)
	}
	if len(order) != 3 || order[0] != 1 || r[0].Index != 0 || r[1].Index != 1 || r[2].Index != 2 {
		t.Fatalf("order=%v results=%+v", order, r)
	}
}
func TestEngineCredentialIsolationAndCacheWriteFailure(t *testing.T) {
	f := filepath.Join(t.TempDir(), "same")
	if e := os.WriteFile(f, []byte("same"), 0600); e != nil {
		t.Fatal(e)
	}
	cache := filepath.Join(t.TempDir(), "cache.json")
	u1 := &sequencedUploader{Credential: "one"}
	if r := NewEngine(cache).UploadAll(context.Background(), u1, []string{f}, Options{}, nil); r[0].Status != "success" {
		t.Fatalf("first upload: %+v", r[0])
	}
	u2 := &sequencedUploader{Credential: "two"}
	r := NewEngine(cache).UploadAll(context.Background(), u2, []string{f}, Options{}, nil)
	if u2.calls != 1 || r[0].URL == "https://cdn.test/one/same" {
		t.Fatalf("credentials shared cache: calls=%d result=%+v", u2.calls, r[0])
	}
	r = NewEngine(t.TempDir()).UploadAll(context.Background(), &sequencedUploader{Credential: "x"}, []string{f}, Options{}, nil)
	if r[0].Status != "success" || r[0].Err == "" {
		t.Fatalf("cache write failure not surfaced: %+v", r[0])
	}
}
