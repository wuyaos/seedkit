// Package tools locates external executables (ffmpeg/ffprobe). Resolution
// order: explicit config path → bundled binary (build tag bundleffmpeg) → PATH.
package tools

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

type Tool struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Version string `json:"version"`
	Found   bool   `json:"found"`
	Source  string `json:"source"` // config|bundled|path
}

type Set struct {
	FFmpeg  Tool `json:"ffmpeg"`
	FFprobe Tool `json:"ffprobe"`
}

func (s Set) ScreenshotReady() bool { return s.FFmpeg.Found && s.FFprobe.Found }

// Resolve probes tools. cfgFFmpeg/cfgFFprobe are optional explicit paths.
// dataDir is where bundled binaries get extracted (if compiled in).
func Resolve(ctx context.Context, dataDir, cfgFFmpeg, cfgFFprobe string) Set {
	return Set{
		FFmpeg:  resolve(ctx, "ffmpeg", dataDir, cfgFFmpeg),
		FFprobe: resolve(ctx, "ffprobe", dataDir, cfgFFprobe),
	}
}

func resolve(ctx context.Context, name, dataDir, explicit string) Tool {
	t := Tool{Name: name}
	try := func(p, src string) bool {
		if p == "" {
			return false
		}
		if v, ok := version(ctx, p); ok {
			t.Path, t.Version, t.Found, t.Source = p, v, true, src
			return true
		}
		return false
	}
	if try(explicit, "config") {
		return t
	}
	if p, err := bundled(name, dataDir); err == nil && try(p, "bundled") {
		return t
	}
	if p, err := exec.LookPath(name); err == nil && try(p, "path") {
		return t
	}
	return t
}

func version(ctx context.Context, path string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "-version").Output()
	if err != nil {
		return "", false
	}
	line, _, _ := bytes.Cut(out, []byte("\n"))
	f := strings.Fields(string(line))
	if len(f) >= 3 {
		return f[2], true
	}
	return strings.TrimSpace(string(line)), true
}
