//go:build bundleffmpeg

package tools

import (
	"bytes"
	"embed"
	"io"
	"os"
	"path/filepath"
)

// Place static linux binaries at internal/tools/bundled/ffmpeg and
// internal/tools/bundled/ffprobe before building with -tags bundleffmpeg.
//
//go:embed bundled/ffmpeg bundled/ffprobe
var bundledFS embed.FS

// bundled extracts the embedded binary into <dataDir>/bin once and returns it.
func bundled(name, dataDir string) (string, error) {
	src, err := bundledFS.ReadFile("bundled/" + name)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(dataDir, "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, name)
	if cur, err := os.ReadFile(dst); err == nil && bytes.Equal(cur, src) {
		return dst, nil
	}
	f, err := os.OpenFile(dst+".tmp", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, bytes.NewReader(src)); err != nil {
		f.Close()
		return "", err
	}
	f.Close()
	return dst, os.Rename(dst+".tmp", dst)
}
