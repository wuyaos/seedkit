package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wuyaos/seedkit/internal/auth"
	"github.com/wuyaos/seedkit/internal/config"
)

func TestSettingsKeyReveal(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Upload.Picgo.APIKey = "test-only-complete-key-1234"
	mgr := auth.New("", "", time.Hour)
	if _, err := mgr.SetCredentials("tester", "test-password"); err != nil {
		t.Fatal(err)
	}
	token, ok, _ := mgr.Login(httptest.NewRequest("POST", "/api/login", nil), "tester", "test-password")
	if !ok {
		t.Fatal("test login failed")
	}
	s := &Server{cfg: cfg, auth: mgr}
	s.routes()
	for _, tc := range []struct {
		name, method, path string
		loggedIn           bool
		status             int
	}{
		{"unauthenticated reveal", "POST", "/api/settings/picgo-key", false, 401},
		{"unauthenticated snapshot", "GET", "/api/settings", false, 401},
		{"masked snapshot", "GET", "/api/settings", true, 200},
		{"explicit reveal", "POST", "/api/settings/picgo-key", true, 200},
		{"no GET reveal", "GET", "/api/settings/picgo-key", true, 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.loggedIn {
				req.AddCookie(&http.Cookie{Name: "seedkit_session", Value: token})
			}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("status %d, expected %d", w.Code, tc.status)
			}
			if w.Code != 200 {
				return
			}
			if tc.path == "/api/settings" {
				var got config.Config
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if got.Upload.Picgo.APIKey != "••••1234" || got.Auth.PasswordHash != "" {
					t.Fatal("settings snapshot must remain masked")
				}
			} else {
				var got map[string]string
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if got["key"] != cfg.Upload.Picgo.APIKey || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("reveal must return the complete key without caching")
				}
			}
		})
	}
	if cfg.Upload.Picgo.APIKey != "test-only-complete-key-1234" {
		t.Fatal("snapshot changed the stored key")
	}
}
