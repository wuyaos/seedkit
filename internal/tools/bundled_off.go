//go:build !bundleffmpeg

package tools

import "errors"

var ErrNotBundled = errors.New("ffmpeg not bundled in this build")

func bundled(name, dataDir string) (string, error) { return "", ErrNotBundled }
