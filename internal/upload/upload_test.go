package upload

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type rt func(*http.Request) (*http.Response, error)

func (f rt) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func responseClient(t *testing.T, fn func(*http.Request) (int, string)) {
	t.Helper()
	old := client
	client = &http.Client{Transport: rt(func(r *http.Request) (*http.Response, error) {
		s, b := fn(r)
		return &http.Response{StatusCode: s, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(b)), Request: r}, nil
	})}
	t.Cleanup(func() { client = old })
}
func img(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x.png")
	if e := os.WriteFile(p, []byte("image"), 0600); e != nil {
		t.Fatal(e)
	}
	return p
}
func fields(t *testing.T, r *http.Request) map[string]string {
	t.Helper()
	if e := r.ParseMultipartForm(1 << 20); e != nil {
		t.Fatal(e)
	}
	m := map[string]string{}
	for k, v := range r.MultipartForm.Value {
		m[k] = strings.Join(v, ",")
	}
	return m
}
func TestProviderFixtures(t *testing.T) {
	cases := []struct {
		name  string
		u     details
		body  string
		check func(*testing.T, *http.Request)
	}{
		{"imgbb", &Imgbb{APIKey: "k"}, `{"success":true,"data":{"url":"https://i.test/a","display_url":"https://i.test/t"}}`, func(t *testing.T, r *http.Request) {
			if fields(t, r)["key"] != "k" || r.URL.RawQuery != "" {
				t.Fatal("ImgBB key must be multipart")
			}
		}},
		{"freeimage", &Freeimage{APIKey: "k"}, `{"status_code":200,"image":{"url":"https://i.test/a"}}`, func(t *testing.T, r *http.Request) {
			v := fields(t, r)
			if v["key"] != "k" || v["action"] != "upload" || v["format"] != "json" {
				t.Fatalf("%v", v)
			}
		}},
		{"agsv", &AGSV{Token: "t"}, `{"data":{"links":{"url":"https://i.test/a"}}}`, func(t *testing.T, r *http.Request) {
			if r.URL.String() != "https://img.seedvault.cn/api/v1/upload" || r.Header.Get("Authorization") != "Bearer t" || r.Header.Get("Accept") != "application/json" {
				t.Fatal("AGSV endpoint/auth mismatch")
			}
			if _, _, e := r.FormFile("file"); e != nil {
				t.Fatal(e)
			}
		}},
		{"picgo", &Picgo{Endpoint: "https://test/upload", APIKey: "k"}, `{"status_code":200,"image":{"url":"https://i.test/a","thumb":{"url":"https://i.test/t"}},"url_viewer":"https://i.test/page"}`, func(t *testing.T, r *http.Request) {
			if r.Header.Get("X-API-Key") != "k" {
				t.Fatal("Picgo API key absent")
			}
		}},
		{"pixhost", &Pixhost{}, `{"show_url":"https://pixhost.to/show/1/a","th_url":"https://t12.pixhost.to/thumbs/1/a"}`, func(t *testing.T, r *http.Request) {
			v := fields(t, r)
			if v["content_type"] != "0" || v["max_th_size"] != "350" {
				t.Fatalf("%v", v)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			responseClient(t, func(r *http.Request) (int, string) { tc.check(t, r); return 200, tc.body })
			im, e := tc.u.UploadDetails(context.Background(), img(t))
			if e != nil {
				t.Fatal(e)
			}
			if !validURL(im.URL) {
				t.Fatalf("image=%+v", im)
			}
			if tc.name == "pixhost" && im.URL != "https://img12.pixhost.to/images/1/a" {
				t.Fatalf("pixhost url=%s", im.URL)
			}
		})
	}
}
func TestBusinessResponseValidation(t *testing.T) {
	for _, tc := range []struct {
		u    details
		body string
	}{{&Picgo{Endpoint: "https://t", APIKey: "k"}, `{"status_code":400,"image":{"url":"https://i.test/a"}}`}, {&Freeimage{APIKey: "k"}, `{"status_code":403,"image":{"url":"https://i.test/a"}}`}, {&Imgbb{APIKey: "k"}, `{"success":false,"data":{"url":"https://i.test/a"}}`}} {
		responseClient(t, func(*http.Request) (int, string) { return 200, tc.body })
		if _, e := tc.u.UploadDetails(context.Background(), img(t)); e == nil {
			t.Fatal("accepted invalid business result")
		}
	}
}
func TestRetryAndUncertain(t *testing.T) {
	for _, tc := range []struct{ code, expected int }{{401, 1}, {403, 1}, {400, 1}, {429, 2}, {503, 2}} {
		calls := 0
		responseClient(t, func(*http.Request) (int, string) {
			calls++
			if calls == 1 {
				return tc.code, `{}`
			}
			return 200, `{"status_code":200,"image":{"url":"https://i.test/a"}}`
		})
		r := NewEngine("").UploadAll(context.Background(), &Freeimage{APIKey: "k"}, []string{img(t)}, Options{}, nil)[0]
		if calls != tc.expected || r.Attempts != tc.expected {
			t.Fatalf("status=%d calls=%d result=%+v", tc.code, calls, r)
		}
	}
	old := client
	calls := 0
	client = &http.Client{Transport: rt(func(*http.Request) (*http.Response, error) { calls++; return nil, fmt.Errorf("timeout") })}
	r := NewEngine("").UploadAll(context.Background(), &Freeimage{APIKey: "k"}, []string{img(t)}, Options{}, nil)[0]
	client = old
	if calls != 1 || !r.Uncertain {
		t.Fatalf("calls=%d result=%+v", calls, r)
	}
}

type fake struct {
	mu sync.Mutex
	n  int
}

func (*fake) Name() string { return "fake" }
func (f *fake) Upload(context.Context, string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	return "https://i.test/a", nil
}
func TestEngineDedupPersistenceForceCancellation(t *testing.T) {
	p := img(t)
	p2 := filepath.Join(t.TempDir(), "copy")
	b, _ := os.ReadFile(p)
	_ = os.WriteFile(p2, b, 0600)
	cache := filepath.Join(t.TempDir(), "cache.json")
	u := &fake{}
	seen := map[int]bool{}
	r := NewEngine(cache).UploadAll(context.Background(), u, []string{p, p2}, Options{Concurrency: 2}, func(x Result) {
		if seen[x.Index] {
			t.Errorf("duplicate callback")
		}
		seen[x.Index] = true
	})
	if u.n != 1 || r[0].Status != "success" || r[1].Status != "cached" || len(seen) != 2 {
		t.Fatalf("calls=%d results=%+v", u.n, r)
	}
	u2 := &fake{}
	r = NewEngine(cache).UploadAll(context.Background(), u2, []string{p}, Options{}, nil)
	if u2.n != 0 || !r[0].Cached {
		t.Fatalf("restart: %+v", r)
	}
	u3 := &fake{}
	NewEngine(cache).UploadAll(context.Background(), u3, []string{p}, Options{Force: true}, nil)
	if u3.n != 1 {
		t.Fatal("Force did not upload")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r = NewEngine("").UploadAll(ctx, u3, []string{p}, Options{}, nil)
	if r[0].Status != "cancelled" {
		t.Fatalf("cancel=%+v", r)
	}
}
func TestPterclubFieldsCookieAndRefresh(t *testing.T) {
	old := client
	gets, posts := 0, 0
	client = &http.Client{Transport: rt(func(r *http.Request) (*http.Response, error) {
		status := 200
		body := ""
		if r.Method == "GET" {
			gets++
			if r.URL.Host != "s3.pterclub.net" || r.Header.Get("Cookie") != "c=x" {
				t.Fatalf("GET leaked/host mismatch %s", r.URL)
			}
			token := "old"
			if gets == 2 {
				token = "new"
			}
			body = `PF.obj.config.auth_token = "` + token + `";`
		}
		if r.Method == "POST" {
			posts++
			if r.URL.Host != "s3.pterclub.net" || r.Header.Get("Cookie") != "c=x" {
				t.Fatalf("POST leaked/host mismatch %s", r.URL)
			}
			v := fields(t, r)
			if v["type"] != "file" || v["action"] != "upload" || v["timestamp"] == "" || v["nsfw"] != "0" {
				t.Fatalf("form fields=%v", v)
			}
			if v["auth_token"] == "old" {
				body = `{"status_code":403}`
			} else {
				body = `{"status_code":200,"image":{"url":"https://i.test/a"}}`
			}
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	t.Cleanup(func() { client = old })
	if _, e := (&Pterclub{Cookie: "c=x"}).UploadDetails(context.Background(), img(t)); e != nil {
		t.Fatal(e)
	}
	if gets != 2 || posts != 2 {
		t.Fatalf("refresh GET=%d POST=%d", gets, posts)
	}
}
