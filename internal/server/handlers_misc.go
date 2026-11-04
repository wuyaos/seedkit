package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"archive/zip"
	"github.com/wuyaos/seedkit/internal/auth"
	"github.com/wuyaos/seedkit/internal/bbcode"
	"github.com/wuyaos/seedkit/internal/config"
	"github.com/wuyaos/seedkit/internal/screenshot"
	"github.com/wuyaos/seedkit/internal/tools"
	"io"
	"path"
	"path/filepath"
)

// ---------- auth ----------

// rebuildTools re-probes ffmpeg/ffprobe and rebuilds the screenshot service.
// It must use a long-lived context, never a request context: a cancelled
// context stored in the screenshot service silently breaks every later
// screenshot, and a cancelled probe would wrongly persist found=false.
func (s *Server) rebuildTools(ctx context.Context) {
	s.tools = tools.Resolve(ctx, s.dataDir, s.cfg.Tools.FFmpeg, s.cfg.Tools.FFprobe)
	s.shots = screenshot.New(ctx, s.tools.FFmpeg.Path, s.tools.FFprobe.Path)
	s.persistResolvedTools()
}

// persistResolvedTools writes PATH-discovered binaries back into config when no
// explicit path is set, so the effective tool set is visible (and editable) in
// settings. Resolution order stays config→bundled→PATH, so a persisted path
// that later disappears simply falls through to bundled/PATH again.
func (s *Server) persistResolvedTools() {
	changed := false
	for _, p := range []struct {
		cfg, found *string
		src        string
	}{
		{&s.cfg.Tools.FFmpeg, &s.tools.FFmpeg.Path, s.tools.FFmpeg.Source},
		{&s.cfg.Tools.FFprobe, &s.tools.FFprobe.Path, s.tools.FFprobe.Source},
	} {
		if *p.cfg == "" && p.src == "path" && *p.found != "" {
			*p.cfg = *p.found
			changed = true
		}
	}
	if changed {
		if err := config.Save(s.dataDir, s.cfg); err != nil {
			s.log.Error("persist resolved tools", "err", err)
		}
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	// Self-heal: a transient probe failure (load, timeout) must not stick as
	// "ffmpeg not found" forever; re-resolve on read when tools are missing.
	if !s.tools.ScreenshotReady() {
		s.rebuildTools(context.Background())
	}
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

// ---------- results library ----------

func (s *Server) handleResults(w http.ResponseWriter, r *http.Request) {
	p := strings.Trim(r.URL.Query().Get("path"), "/")
	if p == "" {
		writeErr(w, 400, errors.New("path required"))
		return
	}
	writeJSON(w, 200, s.results.ForPath(p))
}

func (s *Server) handleResultsRecent(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(r.URL.Query().Get("n"))
	if n <= 0 {
		n = 50
	}
	writeJSON(w, 200, s.results.Recent(n))
}

func (s *Server) handleResult(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.results.Get(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, errors.New("result not found"))
		return
	}
	writeJSON(w, 200, rec)
}

// handleResultDelete removes a record; with ?files=1 the artefact files it
// points at (shots, .torrent) are removed from the output dir too.
func (s *Server) handleResultDelete(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.results.Delete(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, errors.New("result not found"))
		return
	}
	if r.URL.Query().Get("files") == "1" {
		for _, abs := range artefactPaths(rec.Data) {
			if rel, ok := s.output.Rel(abs); ok {
				_ = s.output.Root().Remove(rel)
			}
		}
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// handleResultZip streams every artefact file of a record as one zip
// (store method: PNG/torrent are already compressed). Only files inside the
// output sandbox are included.
func (s *Server) handleResultZip(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.results.Get(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, errors.New("result not found"))
		return
	}
	var rels []string
	for _, abs := range artefactPaths(rec.Data) {
		if rel, ok := s.output.Rel(abs); ok {
			if _, err := s.output.Root().Stat(rel); err == nil {
				rels = append(rels, rel)
			}
		}
	}
	if len(rels) == 0 {
		writeErr(w, 404, errors.New("no artefact files on disk"))
		return
	}
	base := path.Base(rec.Path)
	if base == "" || base == "." || base == "/" {
		base = rec.Kind
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", base+"."+rec.Kind+".zip"))
	zw := zip.NewWriter(w)
	defer zw.Close()
	for _, rel := range rels {
		f, err := s.output.Root().Open(rel)
		if err != nil {
			continue
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			continue
		}
		h, _ := zip.FileInfoHeader(info)
		h.Name = path.Base(filepath.ToSlash(rel))
		h.Method = zip.Store
		fw, err := zw.CreateHeader(h)
		if err == nil {
			_, _ = io.Copy(fw, f)
		}
		f.Close()
	}
}

// artefactPaths pulls absolute file paths out of a screenshot/torrent record.
func artefactPaths(raw json.RawMessage) []string {
	var d struct {
		Shots   []struct{ Path string } `json:"shots"`
		Torrent *struct{ Path string }  `json:"torrent"`
	}
	_ = json.Unmarshal(raw, &d)
	var out []string
	for _, sh := range d.Shots {
		out = append(out, sh.Path)
	}
	if d.Torrent != nil {
		out = append(out, d.Torrent.Path)
	}
	return out
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
	restartRequired := in.Server.Listen != s.cfg.Server.Listen
	*s.cfg = in
	if err := config.Save(s.dataDir, s.cfg); err != nil {
		writeErr(w, 500, err)
		return
	}
	old := s.files
	s.files = s.openRoots(s.cfg.Paths.Roots)
	old.Close()
	s.rebuildTools(context.Background())
	writeJSON(w, 200, map[string]any{"ok": true, "tools": s.tools, "restartRequired": restartRequired})
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
