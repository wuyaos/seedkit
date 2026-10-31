// Package screenshot ports bd.sh: pick time points, burn subtitles (text via
// subtitles=, bitmap via overlay), tonemap HDR, encode PNG, then quantize.
package screenshot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Service struct {
	ffmpeg    string
	ffprobe   string
	hasZscale bool
}

func New(ctx context.Context, ffmpeg, ffprobe string) *Service {
	s := &Service{ffmpeg: ffmpeg, ffprobe: ffprobe}
	if ffmpeg != "" {
		out, _ := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-filters").Output()
		s.hasZscale = bytes.Contains(out, []byte(" zscale "))
	}
	return s
}

func (s *Service) HasZscale() bool { return s.hasZscale }

type Options struct {
	Count       int
	SkipPercent int    // margin at head and tail
	Language    string // none|default|<iso639>
	Tonemap     string // hable|mobius|reinhard|bt2390|none
	Compress    bool
	Quality     int // 0-100 target for quantizer; informational
}

type Shot struct {
	Index    int     `json:"index"`
	Seconds  float64 `json:"seconds"`
	Path     string  `json:"path"`
	Bytes    int64   `json:"bytes"`
	RawBytes int64   `json:"rawBytes"`
}

type Result struct {
	Source   string   `json:"source"`
	Duration float64  `json:"duration"`
	HDR      bool     `json:"hdr"`
	Subtitle string   `json:"subtitle"`
	Shots    []Shot   `json:"shots"`
	Warnings []string `json:"warnings,omitempty"`
}

type Logger interface {
	Logf(format string, a ...any)
	Progress(p float64, msg string)
}

// ResolveSource maps a BDMV folder to its largest .m2ts; files pass through.
func ResolveSource(abs string) (string, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		if strings.EqualFold(filepath.Ext(abs), ".iso") {
			return "", errors.New("ISO screenshots need the image mounted; mount it and point at the BDMV folder (BDInfo works on ISO directly)")
		}
		return abs, nil
	}
	for _, sub := range []string{filepath.Join("BDMV", "STREAM"), "VIDEO_TS"} {
		dir := filepath.Join(abs, sub)
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		var best string
		var bestSize int64
		for _, e := range ents {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if ext != ".m2ts" && ext != ".vob" {
				continue
			}
			if fi, err := e.Info(); err == nil && fi.Size() > bestSize {
				best, bestSize = filepath.Join(dir, e.Name()), fi.Size()
			}
		}
		if best != "" {
			return best, nil
		}
	}
	return "", errors.New("no BDMV/STREAM/*.m2ts or VIDEO_TS/*.vob found")
}

func (s *Service) Run(ctx context.Context, src, outDir string, opt Options, log Logger) (*Result, error) {
	if s.ffmpeg == "" || s.ffprobe == "" {
		return nil, errors.New("ffmpeg/ffprobe not available")
	}
	if opt.Count <= 0 {
		opt.Count = 5
	}
	mpegPS := isMpegPS(src)
	pr, err := s.probe(ctx, src, mpegPS)
	if err != nil {
		return nil, fmt.Errorf("ffprobe: %w", err)
	}
	if pr.Video == nil {
		return nil, errors.New("no video stream")
	}
	if pr.Duration <= 0 {
		return nil, errors.New("invalid duration")
	}
	margin := pr.Duration * float64(opt.SkipPercent) / 100
	if margin < 1 {
		margin = 1
	}
	avail := pr.Duration - 2*margin
	if avail <= 0 {
		return nil, fmt.Errorf("video too short (%.0fs)", pr.Duration)
	}
	interval := avail / float64(opt.Count)
	points := make([]float64, opt.Count)
	for i := range points {
		points[i] = margin + float64(i)*interval + interval/2
	}

	subIdx, subKind := pickSubtitle(pr.Subs, opt.Language)
	res := &Result{Source: src, Duration: pr.Duration, HDR: pr.IsHDR}
	if subIdx >= 0 {
		res.Subtitle = fmt.Sprintf("s:%d (%s)", subIdx, subKind)
	} else {
		res.Subtitle = "none"
	}
	tonemap := opt.Tonemap
	if pr.IsHDR && tonemap != "none" && !s.hasZscale {
		res.Warnings = append(res.Warnings, "ffmpeg lacks zscale; HDR frames will not be tonemapped")
		tonemap = "none"
	}
	if tonemap == "" {
		tonemap = "hable"
	}
	log.Logf("duration %.0fs, hdr=%v, subtitle=%s, count=%d", pr.Duration, pr.IsHDR, res.Subtitle, opt.Count)

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	stamp := time.Now().Unix()
	for i, ts := range points {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		out := filepath.Join(outDir, fmt.Sprintf("%d_%02d.png", stamp, i+1))
		args := s.buildArgs(src, out, ts, mpegPS, pr, subIdx, subKind, tonemap)
		log.Progress(float64(i)/float64(opt.Count), fmt.Sprintf("frame %d/%d @ %.0fs", i+1, opt.Count, ts))
		cmd := exec.CommandContext(ctx, s.ffmpeg, args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err := cmd.Run()
		fi, statErr := os.Stat(out)
		if err != nil || statErr != nil || fi.Size() == 0 {
			msg := strings.TrimSpace(stderr.String())
			if len(msg) > 600 {
				msg = msg[len(msg)-600:]
			}
			log.Logf("frame %d failed: %v %s", i+1, err, msg)
			res.Warnings = append(res.Warnings, fmt.Sprintf("frame %d failed", i+1))
			continue
		}
		shot := Shot{Index: i + 1, Seconds: ts, Path: out, RawBytes: fi.Size(), Bytes: fi.Size()}
		if opt.Compress {
			if n, err := Quantize(out, out); err == nil {
				shot.Bytes = n
			} else {
				log.Logf("quantize %s: %v", filepath.Base(out), err)
			}
		}
		if err := Uniquify(out); err != nil {
			log.Logf("uniquify: %v", err)
		}
		log.Logf("frame %d ok @ %.0fs %dKB -> %dKB", i+1, ts, shot.RawBytes/1024, shot.Bytes/1024)
		res.Shots = append(res.Shots, shot)
	}
	if len(res.Shots) == 0 {
		return res, errors.New("no screenshot produced")
	}
	sort.Slice(res.Shots, func(a, b int) bool { return res.Shots[a].Index < res.Shots[b].Index })
	log.Progress(1, "")
	return res, nil
}

func isMpegPS(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".vob", ".mpg", ".mpeg":
		return true
	}
	return false
}

// buildArgs reproduces bd.sh's command shapes:
//   - MPEG-PS/VOB: -i then -ss (accurate seek only; fast seek corrupts)
//   - others:      -ss (t-1) -i file -ss 1  (fast seek then 1s accurate)
//   - text subs:   -vf subtitles=<escaped>:si=N
//   - bitmap subs: -filter_complex [0:v][0:s:N]overlay=... (bottom-centred)
//   - HDR:         zscale linear → tonemap → bt709
func (s *Service) buildArgs(src, out string, ts float64, mpegPS bool, pr *Probe, subIdx int, subKind, tonemap string) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	if mpegPS {
		args = append(args, "-fflags", "+genpts", "-analyzeduration", "100M", "-probesize", "100M", "-i", src, "-ss", ftoa(ts))
	} else {
		quick := ts - 1
		if quick < 0 {
			quick = 0
		}
		args = append(args, "-ss", ftoa(quick), "-i", src, "-ss", ftoa(ts-quick))
	}
	args = append(args, "-an", "-sn", "-dn")

	var chain []string
	if pr.IsHDR && tonemap != "none" {
		chain = append(chain,
			"zscale=t=linear:npl=100",
			"format=gbrpf32le",
			"zscale=p=bt709",
			"tonemap=tonemap="+tonemap+":desat=0",
			"zscale=t=bt709:m=bt709:r=tv",
			"format=yuv420p",
		)
	}
	switch subKind {
	case "text":
		chain = append(chain, "subtitles="+escapeSubPath(src)+":si="+strconv.Itoa(subIdx))
	case "graphic":
		pre := strings.Join(chain, ",")
		vin := "[0:v]"
		if pre != "" {
			vin = "[v0]"
			pre = "[0:v]" + pre + "[v0];"
		}
		fc := pre + vin + "[0:s:" + strconv.Itoa(subIdx) + "]" +
			"overlay=x=if(gte(overlay_w\\,main_w*95/100)\\,0\\,(main_w-overlay_w)/2):" +
			"y=if(gte(overlay_h\\,main_h*95/100)\\,0\\,main_h-overlay_h-main_h*0.025):format=auto[vout]"
		args = append(args, "-filter_complex", fc, "-map", "[vout]")
		chain = nil
	}
	if len(chain) > 0 {
		args = append(args, "-vf", strings.Join(chain, ","))
	}
	if mpegPS {
		args = append(args, "-pix_fmt", "rgb24")
	}
	args = append(args, "-frames:v", "1", "-c:v", "png", "-compression_level", "3", "-y", out)
	return args
}

func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', 3, 64) }

// escapeSubPath: the subtitles filter option value goes through two unescape
// passes (filtergraph then option). Verified on ffmpeg 8/9: ' : , need \\\X,
// a literal backslash needs \\\\. No surrounding quotes.
func escapeSubPath(p string) string {
	p = filepath.ToSlash(p)
	var b strings.Builder
	for _, r := range p {
		switch r {
		case '\\':
			b.WriteString(`\\\\`)
		case ':', '\'', ',', '[', ']', ';':
			b.WriteString(`\\\`)
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
