package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTorrentSettingsRejectInvalidRulesWithoutMutation(t *testing.T) {
	s := torrentServer(t)
	next := s.configSnapshot()
	next.Torrent.Exclude = []string{"["}
	body, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.handleSettingsPutUpload(w, httptest.NewRequest("PUT", "/api/settings", strings.NewReader(string(body))))
	if w.Code != 400 {
		t.Fatalf("invalid rule status=%d body=%s", w.Code, w.Body.String())
	}
	if s.cfg.Torrent.Exclude[0] != "*.log" {
		t.Fatal("invalid rules replaced live settings")
	}
	copy := s.configSnapshot()
	copy.Torrent.Exclude[0] = "changed"
	if s.cfg.Torrent.Exclude[0] != "*.log" {
		t.Fatal("job settings snapshot shares rule backing array")
	}
}
