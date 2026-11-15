package upload

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPixhostDomainAndRealThumbnail(t *testing.T) {
	responseClient(t, func(r *http.Request) (int, string) {
		if r.URL.Host != "api.pixhost.cc" {
			t.Errorf("domain ignored: %s", r.URL.Host)
		}
		return 200, `{"show_url":"https://pixhost.cc/show/1/a","th_url":"https://t1.pixhost.cc/thumbs/1/a"}`
	})
	im, err := (&Pixhost{Domain: "cc"}).UploadDetails(context.Background(), img(t))
	if err != nil || im.URL != "https://img1.pixhost.cc/images/1/a" {
		t.Fatalf("%+v %v", im, err)
	}
}
func TestThumbnailObjectsAndNoInventedImgBBThumb(t *testing.T) {
	for _, tc := range []struct {
		u     details
		body  string
		thumb string
	}{
		{&Freeimage{APIKey: "k"}, `{"status_code":200,"image":{"url":"https://cdn.test/a","thumb":{"url":"https://cdn.test/t"}}}`, "https://cdn.test/t"},
		{&Imgbb{APIKey: "k"}, `{"success":true,"data":{"url":"https://cdn.test/a","display_url":"https://cdn.test/display"}}`, ""},
	} {
		responseClient(t, func(*http.Request) (int, string) { return 200, tc.body })
		im, e := tc.u.UploadDetails(context.Background(), img(t))
		if e != nil || im.ThumbURL != tc.thumb {
			t.Fatalf("im=%+v error=%v", im, e)
		}
	}
}
func TestCredentialsNeverFollowRedirect(t *testing.T) {
	for _, u := range []Uploader{&Picgo{Endpoint: "https://www.picgo.net/upload", APIKey: "secret"}, &AGSV{Token: "secret"}, &Pterclub{Cookie: "secret"}} {
		calls := 0
		old := client
		client = &http.Client{Transport: rt(func(r *http.Request) (*http.Response, error) {
			calls++
			if r.URL.Host == "other.test" {
				t.Error("credential request followed redirect")
			}
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://other.test/collect"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		})}
		_, e := u.Upload(context.Background(), img(t))
		client = old
		if e == nil || calls != 1 {
			t.Fatalf("%s accepted/followed redirect calls=%d err=%v", u.Name(), calls, e)
		}
	}
}
func TestCacheNullAndInvalidLinksAreSafe(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cache.json")
	if e := os.WriteFile(p, []byte("null"), 0600); e != nil {
		t.Fatal(e)
	}
	u := &fake{}
	file := img(t)
	engine := NewEngine(p)
	r := engine.UploadAll(context.Background(), u, []string{file}, Options{}, nil)
	if r[0].Status != "success" {
		t.Fatalf("%+v", r)
	}
	b, _ := os.ReadFile(p)
	var cache map[string]Image
	if e := json.Unmarshal(b, &cache); e != nil {
		t.Fatal(e)
	}
	for k := range cache {
		cache[k] = Image{URL: "javascript:alert(1)"}
	}
	b, _ = json.Marshal(cache)
	os.WriteFile(p, b, 0600)
	u2 := &fake{}
	r = NewEngine(p).UploadAll(context.Background(), u2, []string{file}, Options{}, nil)
	if r[0].Cached || u2.n != 1 {
		t.Fatal("invalid cache link reused")
	}
}
func TestRetryAfterBounds(t *testing.T) {
	for _, s := range []string{"100000000000", "31", time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)} {
		if got := after(s); got > 30*time.Second || got < 0 {
			t.Fatalf("unbounded delay %q %v", s, got)
		}
	}
}
