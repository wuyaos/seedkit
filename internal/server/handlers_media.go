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

	"github.com/wuyaos/seedkit/internal/media"
	"github.com/wuyaos/seedkit/internal/screenshot"
	"github.com/wuyaos/seedkit/internal/task"
	"github.com/wuyaos/seedkit/internal/torrent"
	"github.com/wuyaos/seedkit/internal/upload"
)

// ---------- media / screenshot / upload / torrent ----------

func (s *Server) uploader(name string) (upload.Uploader, error) {
	if name == "" {
		name = s.cfg.Upload.Default
	}
	switch name {
	case "picgo":
		p := s.cfg.Upload.Picgo
		return &upload.Picgo{Endpoint: p.Endpoint, APIKey: p.APIKey, Album: p.Album}, nil
	case "pixhost":
		p := s.cfg.Upload.Pixhost
		return &upload.Pixhost{ContentType: p.ContentType, MaxTh: p.MaxTh}, nil
	}
	return nil, fmt.Errorf("unknown image host %q", name)
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
	t := s.tasks.Submit("mediainfo", "MediaInfo "+path.Base(in.Path), false,
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
			return map[string]string{"text": txt, "source": src}, nil
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
	t := s.tasks.Submit("bdinfo", "BDInfo "+path.Base(in.Path), true,
		func(ctx context.Context, rep task.Reporter) (any, error) {
			res, err := media.BDInfo(ctx, abs, media.BDInfoOptions{Playlist: in.Playlist, SummaryOnly: in.SummaryOnly},
				func(p float64, stage string) { rep.Progress(p, stage) })
			if res != nil {
				dir := s.outputDir(safeBase(in.Path))
				_ = os.MkdirAll(dir, 0o755)
				_ = os.WriteFile(filepath.Join(dir, "bdinfo.txt"), []byte(res.Report), 0o644)
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
	t := s.tasks.Submit("screenshot", "Screenshot "+path.Base(in.Path), true, s.screenshotJob(abs, in))
	writeJSON(w, 202, t)
}

func (s *Server) screenshotJob(abs string, in screenshotReq) task.Job {
	return func(ctx context.Context, rep task.Reporter) (any, error) {
		src, err := screenshot.ResolveSource(abs)
		if err != nil {
			return nil, err
		}
		opt := screenshot.Options{
			Count: in.Count, SkipPercent: s.cfg.Screenshot.SkipPercent,
			Language: in.Language, Tonemap: s.cfg.Screenshot.Tonemap, Compress: s.cfg.Screenshot.Compress,
		}
		if opt.Count == 0 {
			opt.Count = s.cfg.Screenshot.Count
		}
		if in.Language == "" && !s.cfg.Screenshot.Subtitles {
			opt.Language = "none"
		}
		if in.Compress != nil {
			opt.Compress = *in.Compress
		}
		outDir := s.outputDir(safeBase(in.Path))
		res, err := s.shots.Run(ctx, src, outDir, opt, rep)
		if err != nil {
			return res, err
		}
		type shotOut struct {
			screenshot.Shot
			URL string `json:"url"`
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
			up, err := s.uploader(in.Host)
			if err != nil {
				return out, err
			}
			paths := make([]string, len(res.Shots))
			for i, sh := range res.Shots {
				paths[i] = sh.Path
			}
			rep.Progress(-1, "uploading")
			i := 0
			out.Uploads = upload.UploadAll(ctx, up, paths, func(r upload.Result) {
				if r.Err != "" {
					rep.Logf("upload %s failed: %s", filepath.Base(r.File), r.Err)
				} else {
					rep.Logf("uploaded %s → %s", filepath.Base(r.File), r.URL)
					out.Shots[i].URL = r.URL
				}
				i++
			})
			appendURLs(outDir, out.Uploads)
		}
		return out, nil
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

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Files []string // relative to output dir
		Host  string
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	up, err := s.uploader(in.Host)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	abs := make([]string, 0, len(in.Files))
	for _, f := range in.Files {
		if p, err := s.output.Path(f); err == nil {
			abs = append(abs, p)
		}
	}
	t := s.tasks.Submit("upload", fmt.Sprintf("Upload %d image(s) to %s", len(abs), up.Name()), false,
		func(ctx context.Context, rep task.Reporter) (any, error) {
			n := 0
			res := upload.UploadAll(ctx, up, abs, func(r upload.Result) {
				n++
				rep.Progress(float64(n)/float64(len(abs)), filepath.Base(r.File))
				if r.Err != "" {
					rep.Logf("%s: %s", filepath.Base(r.File), r.Err)
				}
			})
			return res, nil
		})
	writeJSON(w, 202, t)
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
	t := s.tasks.Submit("torrent", "Torrent "+path.Base(in.Path), true, s.torrentJob(abs, in))
	writeJSON(w, 202, t)
}

func (s *Server) torrentJob(abs string, in torrentReq) task.Job {
	return func(ctx context.Context, rep task.Reporter) (any, error) {
		opt := torrent.Options{
			Announce: in.Announce, Source: in.Source, Comment: in.Comment,
			Private: s.cfg.Torrent.Private, Workers: s.cfg.Tasks.HeavyConcurrency + 1,
			CreatedBy: "seedkit " + s.version,
		}
		if opt.Announce == "" {
			opt.Announce = s.cfg.Torrent.Announce
		}
		if opt.Source == "" {
			opt.Source = s.cfg.Torrent.Source
		}
		if in.Private != nil {
			opt.Private = *in.Private
		}
		if in.PieceSize > 0 {
			opt.PieceSizeExp = uint(in.PieceSize)
		} else if s.cfg.Torrent.PieceSize > 0 {
			opt.PieceSizeExp = uint(s.cfg.Torrent.PieceSize)
		}
		dst := filepath.Join(s.outputDir(safeBase(in.Path)), filepath.Base(abs)+".torrent")
		res, err := torrent.Create(ctx, abs, dst, opt, rep.Progress)
		if err != nil {
			return nil, err
		}
		return map[string]any{"torrent": res, "web": s.outputWeb(res.Path)}, nil
	}
}

// handlePipeline runs mediainfo/bdinfo + screenshot(+upload) + torrent for one
// path as a single heavy task and returns everything needed to fill a template.
func (s *Server) handlePipeline(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path       string
		Screenshot *screenshotReq
		Torrent    *torrentReq
		Info       bool
		Playlist   string
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	abs, err := s.files.Resolve(in.Path)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	t := s.tasks.Submit("pipeline", "Pipeline "+path.Base(in.Path), true,
		func(ctx context.Context, rep task.Reporter) (any, error) {
			out := map[string]any{}
			kind := media.Detect(abs)
			if in.Info {
				rep.Logf("info: %s", kind)
				if kind == media.KindFile {
					txt, err := media.MediaInfo(ctx, abs)
					if err != nil {
						return out, err
					}
					out["mediainfo"] = txt
				} else {
					res, err := media.BDInfo(ctx, abs, media.BDInfoOptions{Playlist: in.Playlist},
						func(p float64, st string) { rep.Progress(p*0.3, "bdinfo "+st) })
					if err != nil {
						return out, err
					}
					out["bdinfo"] = res
				}
			}
			if in.Screenshot != nil {
				in.Screenshot.Path = in.Path
				res, err := s.screenshotJob(abs, *in.Screenshot)(ctx, rep)
				out["screenshot"] = res
				if err != nil {
					return out, err
				}
			}
			if in.Torrent != nil {
				in.Torrent.Path = in.Path
				res, err := s.torrentJob(abs, *in.Torrent)(ctx, rep)
				out["torrent"] = res
				if err != nil {
					return out, err
				}
			}
			return out, nil
		})
	writeJSON(w, 202, t)
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
