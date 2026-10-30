// Package server wires HTTP routes to services and streams task events via SSE.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/wuyaos/seedkit/internal/auth"
	"github.com/wuyaos/seedkit/internal/bbcode"
	"github.com/wuyaos/seedkit/internal/config"
	"github.com/wuyaos/seedkit/internal/fsroot"
	"github.com/wuyaos/seedkit/internal/screenshot"
	"github.com/wuyaos/seedkit/internal/task"
	"github.com/wuyaos/seedkit/internal/tools"
	"github.com/wuyaos/seedkit/web"
)

type Server struct {
	cfg     *config.Config
	dataDir string
	version string
	auth    *auth.Manager
	tasks   *task.Manager
	files   *fsroot.Service
	output  *fsroot.Single // screenshots / reports / torrents
	tools   tools.Set
	shots   *screenshot.Service
	tmpl    *bbcode.Store
	log     *slog.Logger
	mux     *http.ServeMux
}

func New(cfg *config.Config, dataDir, version string, log *slog.Logger) *Server {
	ctx := context.Background()
	s := &Server{cfg: cfg, dataDir: dataDir, version: version, log: log}
	s.auth = auth.New(cfg.Auth.Username, cfg.Auth.PasswordHash, time.Duration(cfg.Auth.SessionDays)*24*time.Hour)
	s.tasks = task.New(cfg.Tasks.HeavyConcurrency, cfg.Tasks.HistoryKeep, dataDir)
	s.files = s.openRoots(cfg.Paths.Roots)
	s.tools = tools.Resolve(ctx, dataDir, cfg.Tools.FFmpeg, cfg.Tools.FFprobe)
	s.shots = screenshot.New(ctx, s.tools.FFmpeg.Path, s.tools.FFprobe.Path)
	s.tmpl = bbcode.NewStore(filepath.Join(dataDir, "templates"))
	out, err := fsroot.OpenSingle(cfg.Paths.Output)
	if err != nil {
		log.Error("output dir unusable", "path", cfg.Paths.Output, "err", err)
	}
	s.output = out
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

// openRoots builds the sandbox; unusable roots are logged and skipped.
func (s *Server) openRoots(roots map[string]string) *fsroot.Service {
	svc, errs := fsroot.New(roots)
	for _, e := range errs {
		s.log.Warn("root skipped", "err", e)
	}
	return svc
}

func (s *Server) routes() {
	m := http.NewServeMux()
	s.mux = m

	// public
	m.HandleFunc("GET /api/status", s.handleStatus)
	m.HandleFunc("POST /api/auth/setup", s.handleSetup)
	m.HandleFunc("POST /api/auth/login", s.handleLogin)
	m.HandleFunc("POST /api/auth/logout", s.handleLogout)

	// protected
	p := http.NewServeMux()
	p.HandleFunc("GET /api/fs/roots", s.handleRoots)
	p.HandleFunc("GET /api/fs/list", s.handleList)
	p.HandleFunc("GET /api/fs/stat", s.handleStat)
	p.HandleFunc("GET /api/fs/text", s.handleText)
	p.HandleFunc("GET /api/fs/download", s.handleDownload)
	p.HandleFunc("POST /api/fs/mkdir", s.handleMkdir)
	p.HandleFunc("POST /api/fs/rename", s.handleRename)
	p.HandleFunc("POST /api/fs/delete", s.handleDelete)
	p.HandleFunc("POST /api/fs/move", s.handleMoveCopy(true))
	p.HandleFunc("POST /api/fs/copy", s.handleMoveCopy(false))

	p.HandleFunc("GET /api/tasks", s.handleTasks)
	p.HandleFunc("GET /api/tasks/events", s.handleEvents)
	p.HandleFunc("GET /api/tasks/{id}", s.handleTask)
	p.HandleFunc("POST /api/tasks/{id}/cancel", s.handleCancel)
	p.HandleFunc("DELETE /api/tasks/{id}", s.handleTaskDelete)

	p.HandleFunc("POST /api/media/info", s.handleMediaInfo)
	p.HandleFunc("GET /api/media/playlists", s.handlePlaylists)
	p.HandleFunc("POST /api/media/bdinfo", s.handleBDInfo)
	p.HandleFunc("POST /api/screenshot", s.handleScreenshot)
	p.HandleFunc("POST /api/upload", s.handleUpload)
	p.HandleFunc("POST /api/torrent", s.handleTorrent)
	p.HandleFunc("POST /api/pipeline", s.handlePipeline)

	p.HandleFunc("GET /api/output/", s.handleOutput)
	p.HandleFunc("GET /api/templates", s.handleTemplates)
	p.HandleFunc("PUT /api/templates/{name}", s.handleTemplateSave)
	p.HandleFunc("DELETE /api/templates/{name}", s.handleTemplateDelete)
	p.HandleFunc("POST /api/bbcode/render", s.handleRender)
	p.HandleFunc("GET /api/settings", s.handleSettingsGet)
	p.HandleFunc("PUT /api/settings", s.handleSettingsPut)

	m.Handle("/api/", s.auth.Middleware(p))

	// SPA
	dist, err := fs.Sub(web.Dist, "dist")
	if err == nil {
		fileServer := http.FileServer(http.FS(dist))
		m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
			if p == "" {
				p = "index.html"
			}
			if _, err := fs.Stat(dist, p); err != nil {
				r.URL.Path = "/"
			}
			fileServer.ServeHTTP(w, r)
		})
	}
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(v)
}

func statusFor(err error) int {
	switch {
	case errors.Is(err, os.ErrNotExist), errors.Is(err, fsroot.ErrNoRoot):
		return http.StatusNotFound
	case errors.Is(err, fsroot.ErrOutsideRoot), errors.Is(err, fsroot.ErrIsRoot):
		return http.StatusForbidden
	}
	return http.StatusBadRequest
}
