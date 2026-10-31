package server

import (
	"crypto/sha256"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wuyaos/seedkit/internal/auth"
	"github.com/wuyaos/seedkit/web"
)

func TestStaticCacheAndIconReferences(t *testing.T) {
	s := &Server{auth: auth.New("", "", time.Hour)}
	s.routes()
	icons, err := web.Static.ReadFile("static/vendor/lucide.svg")
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(icons)
	version := fmt.Sprintf("%x", hash[:6])
	for _, file := range []string{"index.html", "app.js"} {
		b, err := web.Static.ReadFile("static/" + file)
		if err != nil {
			t.Fatal(err)
		}
		refs := strings.Split(string(b), "vendor/lucide.svg")
		if len(refs) < 2 {
			t.Fatalf("%s has no icon references", file)
		}
		for _, suffix := range refs[1:] {
			if !strings.HasPrefix(suffix, "?v="+version+"#") {
				t.Fatalf("%s: stale icon reference; run make icons", file)
			}
		}
	}
	for _, tc := range []struct {
		url       string
		immutable bool
	}{
		{"/vendor/lucide.svg?v=" + version, true},
		{"/vendor/lucide.svg", false},
		{"/vendor/lucide.svg?v=stale", false},
		{"/app.js", false},
		{"/app.css", false},
		{"/", false},
	} {
		t.Run(tc.url, func(t *testing.T) {
			r := httptest.NewRecorder()
			s.Handler().ServeHTTP(r, httptest.NewRequest("GET", tc.url, nil))
			if r.Code != 200 {
				t.Fatalf("status %d", r.Code)
			}
			if got := strings.Contains(r.Header().Get("Cache-Control"), "immutable"); got != tc.immutable {
				t.Fatalf("unexpected cache policy: %s", r.Header().Get("Cache-Control"))
			}
		})
	}
}
