package server

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"path/filepath"

	"github.com/wuyaos/seedkit/internal/task"
)

// ---------- files ----------

func (s *Server) handleRoots(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.files.Roots())
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" {
		writeJSON(w, 200, []any{})
		return
	}
	list, err := s.files.List(p)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, 200, list)
}

func (s *Server) handleStat(w http.ResponseWriter, r *http.Request) {
	en, err := s.files.Stat(r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, 200, en)
}

func (s *Server) handleText(w http.ResponseWriter, r *http.Request) {
	txt, trunc, err := s.files.ReadText(r.URL.Query().Get("path"), 512<<10)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, 200, map[string]any{"text": txt, "truncated": trunc})
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	en, err := s.files.Stat(p)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	if en.IsDir {
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", en.Name+".zip"))
		if err := s.files.Zip(p, w); err != nil {
			s.log.Warn("zip", "err", err)
		}
		return
	}
	f, info, err := s.files.Open(p)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	defer f.Close()
	if r.URL.Query().Get("inline") == "" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", en.Name))
	}
	http.ServeContent(w, r, en.Name, info.ModTime(), f)
}

func (s *Server) handleMkdir(w http.ResponseWriter, r *http.Request) {
	var in struct{ Path string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := s.files.Mkdir(in.Path); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) {
	var in struct{ Path, Name string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := s.files.Rename(in.Path, in.Name); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	var in struct{ Paths []string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	for _, p := range in.Paths {
		if err := s.files.Delete(p); err != nil {
			writeErr(w, statusFor(err), fmt.Errorf("%s: %w", p, err))
			return
		}
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleMoveCopy(move bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Paths []string
			Dest  string
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, 400, err)
			return
		}
		verb := "复制"
		if move {
			verb = "移动"
		}
		t := s.tasks.Submit("fileop", fmt.Sprintf("%s %d 项 → %s", verb, len(in.Paths), in.Dest), false,
			func(ctx context.Context, rep task.Reporter) (any, error) {
				for i, p := range in.Paths {
					if ctx.Err() != nil {
						return nil, ctx.Err()
					}
					rep.Logf("%s %s", verb, p)
					prog := func(done, total int64) {
						if total > 0 {
							rep.Progress((float64(i)+float64(done)/float64(total))/float64(len(in.Paths)), path.Base(p))
						}
					}
					var err error
					if move {
						err = s.files.Move(p, in.Dest, prog)
					} else {
						err = s.files.Copy(p, in.Dest, prog)
					}
					if err != nil {
						return nil, fmt.Errorf("%s: %w", p, err)
					}
				}
				return nil, nil
			})
		writeJSON(w, 202, t)
	}
}

// ---------- output dir (screenshots/reports/torrents) ----------

// outputDir returns <output>/<name>, creating it. name must already be sanitised.
func (s *Server) outputDir(name string) string {
	_ = s.output.Root().MkdirAll(name, 0o755)
	return filepath.Join(s.output.Abs(), name)
}

// outputWeb converts an absolute path under the output dir to its /api/output URL.
func (s *Server) outputWeb(abs string) string {
	rel, ok := s.output.Rel(abs)
	if !ok {
		return ""
	}
	return "/api/output/" + rel
}

// handleOutput serves files from the output sandbox. os.Root.FS rejects any
// path that would leave the directory, so no manual prefix check is needed.
// ?dl=1 forces a download with the file's own name.
func (s *Server) handleOutput(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("dl") == "1" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", path.Base(r.URL.Path)))
	}
	http.StripPrefix("/api/output/", http.FileServerFS(s.output.FS())).ServeHTTP(w, r)
}
