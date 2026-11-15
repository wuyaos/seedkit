package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/wuyaos/seedkit/internal/config"
)

const secretMask = "••••"

func maskSecret(v string) string {
	if v == "" {
		return ""
	}
	if len(v) <= 4 {
		return secretMask
	}
	return secretMask + v[len(v)-4:]
}

func preserveSecret(next, current string) string {
	if next == "" || strings.HasPrefix(next, secretMask) {
		return current
	}
	return next
}

func (s *Server) handleSettingsGetUpload(w http.ResponseWriter, r *http.Request) {
	c := s.configSnapshot()
	c.Auth.PasswordHash = ""
	c.Upload.Picgo.APIKey = maskSecret(c.Upload.Picgo.APIKey)
	c.Upload.Imgbb.APIKey = maskSecret(c.Upload.Imgbb.APIKey)
	c.Upload.Freeimage.APIKey = maskSecret(c.Upload.Freeimage.APIKey)
	c.Upload.AGSV.Token = maskSecret(c.Upload.AGSV.Token)
	c.Upload.Pterclub.Cookie = maskSecret(c.Upload.Pterclub.Cookie)
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleSettingsPutUpload(w http.ResponseWriter, r *http.Request) {
	var in config.Config
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	old := s.configSnapshot()
	in.Auth = old.Auth
	in.Upload.Picgo.APIKey = preserveSecret(in.Upload.Picgo.APIKey, old.Upload.Picgo.APIKey)
	in.Upload.Imgbb.APIKey = preserveSecret(in.Upload.Imgbb.APIKey, old.Upload.Imgbb.APIKey)
	in.Upload.Freeimage.APIKey = preserveSecret(in.Upload.Freeimage.APIKey, old.Upload.Freeimage.APIKey)
	in.Upload.AGSV.Token = preserveSecret(in.Upload.AGSV.Token, old.Upload.AGSV.Token)
	in.Upload.Pterclub.Cookie = preserveSecret(in.Upload.Pterclub.Cookie, old.Upload.Pterclub.Cookie)
	if in.Paths.Output == "" {
		in.Paths.Output = old.Paths.Output
	}
	if in.Tasks.HeavyConcurrency < 1 {
		in.Tasks.HeavyConcurrency = 1
	}
	if in.Upload.Concurrency == 0 {
		in.Upload.Concurrency = 3
	}
	if in.Upload.Concurrency < 1 || in.Upload.Concurrency > 8 {
		writeErr(w, 400, errors.New("upload concurrency must be between 1 and 8"))
		return
	}
	if in.Upload.Pixhost.Domain == "" {
		in.Upload.Pixhost.Domain = "to"
	}
	if in.Upload.Pixhost.Domain != "to" && in.Upload.Pixhost.Domain != "cc" {
		writeErr(w, 400, errors.New("unsupported Pixhost domain"))
		return
	}
	if _, err := uploaderFor(in.Upload, ""); err != nil {
		writeErr(w, 400, err)
		return
	}
	restartRequired := in.Server.Listen != old.Server.Listen || in.Paths.Output != old.Paths.Output || in.Tasks.HeavyConcurrency != old.Tasks.HeavyConcurrency
	s.cfgMu.Lock()
	if err := config.Save(s.dataDir, &in); err != nil {
		s.cfgMu.Unlock()
		writeErr(w, 500, errors.New("could not save settings"))
		return
	}
	*s.cfg = in
	s.cfgMu.Unlock()
	oldFiles := s.files
	s.files = s.openRoots(in.Paths.Roots)
	oldFiles.Close()
	s.rebuildTools(context.Background())
	writeJSON(w, 200, map[string]any{"ok": true, "tools": s.tools, "restartRequired": restartRequired})
}
