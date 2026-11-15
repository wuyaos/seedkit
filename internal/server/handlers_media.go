package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/wuyaos/seedkit/internal/config"
	"github.com/wuyaos/seedkit/internal/media"
	"github.com/wuyaos/seedkit/internal/screenshot"
	"github.com/wuyaos/seedkit/internal/task"
	"github.com/wuyaos/seedkit/internal/torrent"
	"github.com/wuyaos/seedkit/internal/upload"
)

// ---------- media / screenshot / upload / torrent ----------

func (s *Server) uploader(name string) (upload.Uploader, error) {
	return uploaderFor(s.configSnapshot().Upload, name)
}

func uploaderFor(c config.Upload, name string) (upload.Uploader, error) {
	if name == "" {
		name = c.Default
	}
	switch name {
	case "picgo":
		return &upload.Picgo{Endpoint: c.Picgo.Endpoint, APIKey: c.Picgo.APIKey, Album: c.Picgo.Album}, nil
	case "pixhost":
		return &upload.Pixhost{ContentType: c.Pixhost.ContentType, MaxTh: c.Pixhost.MaxTh, Domain: c.Pixhost.Domain}, nil
	case "imgbb":
		return &upload.Imgbb{APIKey: c.Imgbb.APIKey}, nil
	case "freeimage":
		return &upload.Freeimage{APIKey: c.Freeimage.APIKey}, nil
	case "agsv":
		return &upload.AGSV{Token: c.AGSV.Token}, nil
	case "pterclub":
		return &upload.Pterclub{Cookie: c.Pterclub.Cookie}, nil
	default:
		return nil, errors.New("unknown image host")
	}
}

func (s *Server) handleMediaInfo(w http.ResponseWriter, r *http.Request) {
	var in struct{ Path string }
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	abs, err := s.files.Resolve(in.Path)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	t := s.tasks.Submit("mediainfo", "MediaInfo · "+path.Base(in.Path), false,
		func(ctx context.Context, rep task.Reporter) (any, error) {
			src := abs
			if media.Detect(abs) == media.KindBDMV {
				if m, err := screenshot.ResolveSource(abs); err == nil {
					src = m
				}
			}
			txt, err := media.MediaInfo(ctx, src)
			if err != nil {
				return nil, err
			}
			out := map[string]string{"text": txt, "source": src}
			s.record("mediainfo", in.Path, rep.ID(), out)
			return out, nil
		})
	writeJSON(w, 202, t)
}

func (s *Server) handlePlaylists(w http.ResponseWriter, r *http.Request) {
	abs, err := s.files.Resolve(r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	pls, disc, err := media.Playlists(ctx, abs)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]any{"disc": disc, "playlists": pls})
}

type bdinfoReq struct {
	Path        string
	Playlist    string
	SummaryOnly bool
}

func (s *Server) handleBDInfo(w http.ResponseWriter, r *http.Request) {
	var in bdinfoReq
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	abs, err := s.files.Resolve(in.Path)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	t := s.tasks.Submit("bdinfo", "BDInfo · "+path.Base(in.Path), true,
		func(ctx context.Context, rep task.Reporter) (any, error) {
			res, err := media.BDInfo(ctx, abs, media.BDInfoOptions{Playlist: in.Playlist, SummaryOnly: in.SummaryOnly},
				func(p float64, stage string) { rep.Progress(p, stage) })
			if res != nil {
				dir := s.outputDir(safeBase(in.Path))
				_ = os.WriteFile(filepath.Join(dir, "bdinfo.txt"), []byte(res.Report), 0o644)
			}
			if err == nil {
				s.record("bdinfo", in.Path, rep.ID(), res)
			}
			return res, err
		})
	writeJSON(w, 202, t)
}

type screenshotReq struct {
	Path     string
	Count    int
	Language string
	Upload   bool
	Host     string
	Compress *bool
}

func (s *Server) handleScreenshot(w http.ResponseWriter, r *http.Request) {
	var in screenshotReq
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	if !s.tools.ScreenshotReady() {
		writeErr(w, 503, errors.New("ffmpeg/ffprobe not found; configure in settings"))
		return
	}
	abs, err := s.files.Resolve(in.Path)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	t := s.tasks.Submit("screenshot", "截图 · "+path.Base(in.Path), true, s.screenshotJob(abs, in))
	writeJSON(w, 202, t)
}

func (s *Server) screenshotJob(abs string, in screenshotReq) task.Job {
	cfg := s.configSnapshot()
	shotsService := s.shots
	return func(ctx context.Context, rep task.Reporter) (any, error) {
		src, err := screenshot.ResolveSource(abs)
		if err != nil {
			return nil, err
		}
		opt := screenshot.Options{
			Count: in.Count, SkipPercent: cfg.Screenshot.SkipPercent,
			Language: in.Language, Tonemap: cfg.Screenshot.Tonemap, Compress: cfg.Screenshot.Compress,
		}
		if opt.Count == 0 {
			opt.Count = cfg.Screenshot.Count
		}
		if in.Language == "" && !cfg.Screenshot.Subtitles {
			opt.Language = "none"
		}
		if in.Compress != nil {
			opt.Compress = *in.Compress
		}
		outDir := s.outputDir(safeBase(in.Path))
		res, err := shotsService.Run(ctx, src, outDir, opt, rep)
		if err != nil {
			return res, err
		}
		type shotOut struct {
			screenshot.Shot
			URL string `json:"url"`
			uploadFields
			Web string `json:"web"`
		}
		out := struct {
			*screenshot.Result
			Shots   []shotOut       `json:"shots"`
			Uploads []upload.Result `json:"uploads,omitempty"`
		}{Result: res}
		for _, sh := range res.Shots {
			out.Shots = append(out.Shots, shotOut{Shot: sh, Web: s.outputWeb(sh.Path)})
		}
		if in.Upload {
			up, err := uploaderFor(cfg.Upload, in.Host)
			if err != nil {
				s.record("screenshot", in.Path, rep.ID(), out)
				return out, err
			}
			paths := make([]string, len(res.Shots))
			for i, sh := range res.Shots {
				paths[i] = sh.Path
			}
			rep.Progress(-1, "uploading")
			out.Uploads = s.uploadEngine.UploadAll(ctx, up, paths, upload.Options{Concurrency: cfg.Upload.Concurrency}, func(r upload.Result) {
				if r.Index >= 0 && r.Index < len(out.Shots) {
					out.Shots[r.Index].uploadFields = fieldsFor(r)
					out.Shots[r.Index].URL = r.URL
				}
				logUpload(rep, r)
			})
			appendURLs(outDir, out.Uploads)
		}
		s.record("screenshot", in.Path, rep.ID(), out)
		return out, uploadOutcome(ctx, out.Uploads)
	}
}

func appendURLs(dir string, ups []upload.Result) {
	f, err := os.OpenFile(filepath.Join(dir, "urls.txt"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	for _, u := range ups {
		if u.URL != "" {
			fmt.Fprintf(f, "%s\t%s\n", filepath.Base(u.File), u.URL)
		}
	}
}

// record stores a task result in the results library; failures are logged only.
func (s *Server) record(kind, vpath, taskID string, data any) {
	if _, err := s.results.Add(kind, strings.Trim(vpath, "/"), path.Base(vpath), taskID, data); err != nil {
		s.log.Warn("results save failed", "err", err)
	}
}

type torrentReq struct {
	Path      string
	Announce  string
	Source    string
	Private   *bool
	Comment   string
	PieceSize int
}

func (s *Server) handleTorrent(w http.ResponseWriter, r *http.Request) {
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
	t := s.tasks.Submit("torrent", "制种 · "+path.Base(in.Path), true, s.torrentJob(abs, in))
	writeJSON(w, 202, t)
}

func (s *Server) torrentJob(abs string, in torrentReq) task.Job {
	cfg := s.configSnapshot()
	return func(ctx context.Context, rep task.Reporter) (any, error) {
		opt := torrent.Options{
			Announce: in.Announce, Source: in.Source, Comment: in.Comment,
			Private: cfg.Torrent.Private, Workers: cfg.Tasks.HeavyConcurrency + 1,
			CreatedBy: "seedkit " + s.version,
		}
		if opt.Announce == "" {
			opt.Announce = cfg.Torrent.Announce
		}
		if opt.Source == "" {
			opt.Source = cfg.Torrent.Source
		}
		if in.Private != nil {
			opt.Private = *in.Private
		}
		if in.PieceSize > 0 {
			opt.PieceSizeExp = uint(in.PieceSize)
		} else if cfg.Torrent.PieceSize > 0 {
			opt.PieceSizeExp = uint(cfg.Torrent.PieceSize)
		}
		dst := filepath.Join(s.outputDir(safeBase(in.Path)), filepath.Base(abs)+".torrent")
		res, err := torrent.Create(ctx, abs, dst, opt, rep.Progress)
		if err != nil {
			return nil, err
		}
		out := map[string]any{"torrent": res, "web": s.outputWeb(res.Path)}
		s.record("torrent", in.Path, rep.ID(), out)
		return out, nil
	}
}

func safeBase(vpath string) string {
	b := path.Base(strings.Trim(vpath, "/"))
	b = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\\|?*`, r) {
			return '_'
		}
		return r
	}, b)
	if b == "" || b == "." {
		b = "_"
	}
	return b
}
