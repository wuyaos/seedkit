package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/wuyaos/seedkit/internal/torrent"
)

// Preview uses the same sandbox and scanner as creation, never client absolute paths.
func (s *Server) handleTorrentPreview(w http.ResponseWriter, r *http.Request) {
	var in torrentReq
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	abs, err := s.files.Resolve(in.Path)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	cfg := s.configSnapshot()
	exclude := cfg.Torrent.Exclude
	if in.Exclude != nil {
		exclude = *in.Exclude
	}
	preview, err := torrent.Scan(r.Context(), abs, exclude)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, preview)
}

func (s *Server) validateTorrentSelection(ctx context.Context, abs string, in torrentReq) error {
	cfg := s.configSnapshot()
	exclude := cfg.Torrent.Exclude
	if in.Exclude != nil {
		exclude = *in.Exclude
	}
	// Keep the old unfiltered single-file/directory path unchanged.
	if len(exclude) == 0 && in.Selection == nil {
		return nil
	}
	p, err := torrent.Scan(ctx, abs, exclude)
	if err != nil {
		return err
	}
	available := make(map[string]bool)
	for _, f := range p.Files {
		if f.Reason == "" {
			available[f.Path] = true
		}
	}
	if in.Selection == nil {
		if len(available) == 0 {
			return errors.New("排除后没有可制种文件")
		}
		return nil
	}
	if in.Selection.Snapshot == "" || in.Selection.Snapshot != p.Snapshot {
		return errors.New("源文件或规则已变化，请重新选择种子内容")
	}
	if len(in.Selection.Paths) == 0 {
		return errors.New("至少选择一个文件")
	}
	seen := make(map[string]bool)
	for _, path := range in.Selection.Paths {
		if !available[path] || seen[path] {
			return errors.New("选择清单包含不可选或重复的路径，请重新选择")
		}
		seen[path] = true
	}
	return nil
}
