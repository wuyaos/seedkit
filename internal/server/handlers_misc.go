package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/wuyaos/seedkit/internal/auth"
	"github.com/wuyaos/seedkit/internal/bbcode"
	"github.com/wuyaos/seedkit/internal/config"
	"github.com/wuyaos/seedkit/internal/screenshot"
	"github.com/wuyaos/seedkit/internal/tools"
)

// ---------- auth ----------

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"version":    s.version,
		"needsSetup": s.auth.NeedsSetup(),
		"authed":     s.auth.Valid(auth.TokenFrom(r)),
		"tools":      s.tools,
		"zscale":     s.shots.HasZscale(),
	})
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if !s.auth.NeedsSetup() {
		writeErr(w, 409, errors.New("already configured"))
		return
	}
	var in struct{ Username, Password string }
	if err := readJSON(r, &in); err != nil || len(in.Username) == 0 || len(in.Password) < 6 {
		writeErr(w, 400, errors.New("username and password (>=6 chars) required"))
		return
	}
	hash, err := s.auth.SetCredentials(in.Username, in.Password)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	s.cfg.Auth.Username, s.cfg.Auth.PasswordHash = in.Username, hash
	if err := config.Save(s.dataDir, s.cfg); err != nil {
		writeErr(w, 500, err)
		return
	}
	tok, ok, _ := s.auth.Login(r, in.Username, in.Password)
	if ok {
		s.auth.SetCookie(w, r, tok)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in struct{ Username, Password string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	tok, ok, retry := s.auth.Login(r, in.Username, in.Password)
	if !ok {
		if retry > 0 {
			w.Header().Set("Retry-After", fmt.Sprint(int(retry.Seconds())))
			writeErr(w, 429, fmt.Errorf("too many attempts, retry in %s", retry.Round(time.Second)))
			return
		}
		writeErr(w, 401, errors.New("invalid credentials"))
		return
	}
	s.auth.SetCookie(w, r, tok)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.auth.Logout(auth.TokenFrom(r))
	auth.ClearCookie(w)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---------- tasks ----------

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.tasks.List())
}

func (s *Server) handleTask(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tasks.Get(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, errors.New("task not found"))
		return
	}
	writeJSON(w, 200, t)
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]bool{"ok": s.tasks.Cancel(r.PathValue("id"))})
}

func (s *Server) handleTaskDelete(w http.ResponseWriter, r *http.Request) {
	s.tasks.Delete(r.PathValue("id"))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, errors.New("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ch, unsub := s.tasks.Subscribe()
	defer unsub()
	fmt.Fprintf(w, "event: hello\ndata: {}\n\n")
	fl.Flush()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprintf(w, ": ping\n\n")
			fl.Flush()
		case ev := <-ch:
			b, _ := json.Marshal(ev)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, b)
			fl.Flush()
		}
	}
}

// ---------- settings / templates ----------

func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	c := *s.cfg
	c.Auth.PasswordHash = ""
	if c.Upload.Picgo.APIKey != "" {
		c.Upload.Picgo.APIKey = "••••" + lastN(c.Upload.Picgo.APIKey, 4)
	}
	writeJSON(w, 200, c)
}

func lastN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func (s *Server) handleSettingsPut(w http.ResponseWriter, r *http.Request) {
	var in config.Config
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	// Preserve secrets unless explicitly replaced.
	in.Auth = s.cfg.Auth
	if strings.HasPrefix(in.Upload.Picgo.APIKey, "••••") || in.Upload.Picgo.APIKey == "" {
		in.Upload.Picgo.APIKey = s.cfg.Upload.Picgo.APIKey
	}
	if in.Paths.Output == "" {
		in.Paths.Output = s.cfg.Paths.Output
	}
	if in.Tasks.HeavyConcurrency < 1 {
		in.Tasks.HeavyConcurrency = 1
	}
	*s.cfg = in
	if err := config.Save(s.dataDir, s.cfg); err != nil {
		writeErr(w, 500, err)
		return
	}
	old := s.files
	s.files = s.openRoots(s.cfg.Paths.Roots)
	old.Close()
	s.tools = tools.Resolve(r.Context(), s.dataDir, s.cfg.Tools.FFmpeg, s.cfg.Tools.FFprobe)
	s.shots = screenshot.New(r.Context(), s.tools.FFmpeg.Path, s.tools.FFprobe.Path)
	writeJSON(w, 200, map[string]any{"ok": true, "tools": s.tools, "restartRequired": in.Server.Listen != s.cfg.Server.Listen})
}

func (s *Server) handleTemplates(w http.ResponseWriter, r *http.Request) {
	list, err := s.tmpl.List()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, list)
}

func (s *Server) handleTemplateSave(w http.ResponseWriter, r *http.Request) {
	var in struct{ Body string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := s.tmpl.Save(r.PathValue("name"), in.Body); err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleTemplateDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.tmpl.Delete(r.PathValue("name")); err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleRender(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Template string
		Data     bbcode.Data
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	body, err := s.tmpl.Get(in.Template)
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	out, err := bbcode.Render(body, in.Data)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]string{"text": out})
}
