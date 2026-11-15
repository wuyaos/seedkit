package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/wuyaos/seedkit/internal/task"
	"github.com/wuyaos/seedkit/internal/upload"
)

type uploadFields struct {
	ThumbURL  string `json:"thumb_url,omitempty"`
	PageURL   string `json:"page_url,omitempty"`
	Status    string `json:"upload_status,omitempty"`
	Error     string `json:"upload_error,omitempty"`
	Attempts  int    `json:"upload_attempts"`
	Cached    bool   `json:"upload_cached"`
	Uncertain bool   `json:"upload_uncertain,omitempty"`
}

func fieldsFor(r upload.Result) uploadFields {
	return uploadFields{r.ThumbURL, r.PageURL, r.Status, r.Err, r.Attempts, r.Cached, r.Uncertain}
}
func logUpload(rep task.Reporter, r upload.Result) {
	if r.Err != "" {
		rep.Logf("%s: %s", filepath.Base(r.File), r.Err)
	} else {
		rep.Logf("%s: %s", filepath.Base(r.File), r.Status)
	}
}
func uploadOutcome(ctx context.Context, rows []upload.Result) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	failed := 0
	for _, r := range rows {
		if r.Status == "failed" || r.Status == "cancelled" || r.URL == "" {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d images failed; successful results were retained", failed, len(rows))
	}
	return nil
}

// Validate before queuing: never silently shorten the ordered input list.
func (s *Server) uploadPath(rel string) (string, error) {
	if s.output == nil || !filepath.IsLocal(rel) || strings.Contains(rel, "\\") && filepath.Separator != '\\' {
		return "", errors.New("invalid output file path")
	}
	abs, err := s.output.Path(rel)
	if err != nil {
		return "", errors.New("output file is not accessible")
	}
	st, err := os.Stat(abs)
	if err != nil || !st.Mode().IsRegular() {
		return "", errors.New("output file is not a regular file")
	}
	return abs, nil
}
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Files []string
		Host  string
		Force bool
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	if len(in.Files) == 0 {
		writeErr(w, 400, errors.New("no files selected"))
		return
	}
	cfg := s.configSnapshot()
	up, err := uploaderFor(cfg.Upload, in.Host)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	files := make([]string, len(in.Files))
	for i, f := range in.Files {
		files[i], err = s.uploadPath(f)
		if err != nil {
			writeErr(w, 400, err)
			return
		}
	}
	t := s.tasks.Submit("upload", fmt.Sprintf("上传 %d 张图片 → %s", len(files), up.Name()), false, func(ctx context.Context, rep task.Reporter) (any, error) {
		done := 0
		rows := s.uploadEngine.UploadAll(ctx, up, files, upload.Options{Concurrency: cfg.Upload.Concurrency, Force: in.Force}, func(row upload.Result) {
			done++
			rep.Progress(float64(done)/float64(len(files)), filepath.Base(row.File))
			logUpload(rep, row)
		})
		return rows, uploadOutcome(ctx, rows)
	})
	writeJSON(w, 202, t)
}
func (s *Server) handleReupload(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID    string
		Host  string
		All   bool
		Force bool
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	rec, ok := s.results.Get(in.ID)
	if !ok || rec.Kind != "screenshot" {
		writeErr(w, 404, errors.New("screenshot record not found"))
		return
	}
	var data map[string]json.RawMessage
	var shots []map[string]any
	if err := json.Unmarshal(rec.Data, &data); err != nil {
		writeErr(w, 500, errors.New("invalid screenshot record"))
		return
	}
	if err := json.Unmarshal(data["shots"], &shots); err != nil {
		writeErr(w, 500, errors.New("invalid screenshot list"))
		return
	}
	cfg := s.configSnapshot()
	up, err := uploaderFor(cfg.Upload, in.Host)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	var files []string
	var indices []int
	for i, sh := range shots {
		u, _ := sh["url"].(string)
		status, _ := sh["upload_status"].(string)
		if !in.All && u != "" && status != "failed" && status != "cancelled" {
			continue
		}
		p, _ := sh["path"].(string)
		if s.output == nil {
			writeErr(w, 400, errors.New("output directory unavailable"))
			return
		}
		rel, valid := s.output.Rel(p)
		if !valid {
			writeErr(w, 400, errors.New("screenshot is outside output directory"))
			return
		}
		abs, e := s.uploadPath(rel)
		if e != nil {
			writeErr(w, 400, e)
			return
		}
		files = append(files, abs)
		indices = append(indices, i)
	}
	if len(files) == 0 {
		writeErr(w, 400, errors.New("nothing to upload"))
		return
	}
	t := s.tasks.Submit("upload", fmt.Sprintf("补传 %d 张 → %s · %s", len(files), up.Name(), rec.Title), false, func(ctx context.Context, rep task.Reporter) (any, error) {
		s.uploadsMu.Lock()
		if s.reuploads == nil {
			s.reuploads = map[string]bool{}
		}
		if s.reuploads[in.ID] {
			s.uploadsMu.Unlock()
			return nil, errors.New("this screenshot is already uploading")
		}
		s.reuploads[in.ID] = true
		s.uploadsMu.Unlock()
		defer func() { s.uploadsMu.Lock(); delete(s.reuploads, in.ID); s.uploadsMu.Unlock() }()
		latest, exists := s.results.Get(in.ID)
		if !exists || string(latest.Data) != string(rec.Data) {
			return nil, errors.New("screenshot changed; reload and retry")
		}
		done := 0
		rows := s.uploadEngine.UploadAll(ctx, up, files, upload.Options{Concurrency: cfg.Upload.Concurrency, Force: in.Force}, func(row upload.Result) {
			done++
			rep.Progress(float64(done)/float64(len(files)), filepath.Base(row.File))
			logUpload(rep, row)
		})
		// Keep unknown screenshot metadata and unselected successful rows intact.
		full := make([]upload.Result, len(shots))
		_ = json.Unmarshal(data["uploads"], &full)
		if len(full) != len(shots) {
			full = make([]upload.Result, len(shots))
		}
		for i, sh := range shots {
			full[i].Index = i
			full[i].File, _ = sh["path"].(string)
			full[i].URL, _ = sh["url"].(string)
			if full[i].Status == "" && full[i].URL != "" {
				full[i].Status = "success"
			}
		}
		for _, row := range rows {
			if row.Index < 0 || row.Index >= len(indices) {
				return full, errors.New("invalid upload result index")
			}
			i := indices[row.Index]
			row.Index = i
			full[i] = row
			sh := shots[i]
			sh["upload_status"] = row.Status
			sh["upload_error"] = row.Err
			sh["upload_attempts"] = row.Attempts
			sh["upload_cached"] = row.Cached
			sh["upload_uncertain"] = row.Uncertain
			if row.URL != "" {
				sh["url"] = row.URL
				sh["thumb_url"] = row.ThumbURL
				sh["page_url"] = row.PageURL
			}
		}
		data["shots"], _ = json.Marshal(shots)
		data["uploads"], _ = json.Marshal(full)
		if err := s.results.Update(rec.ID, func(json.RawMessage) (any, error) { return data, nil }); err != nil {
			return full, errors.New("could not persist screenshot upload results")
		}
		appendURLs(filepath.Dir(files[0]), rows)
		return full, uploadOutcome(ctx, rows)
	})
	writeJSON(w, 202, t)
}
