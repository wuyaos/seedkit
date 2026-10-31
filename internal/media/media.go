// Package media wraps go-mediainfo and go-bdinfo behind context-aware calls.
package media

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/autobrr/go-bdinfo/pkg/bdinfo"
	"github.com/autobrr/go-mediainfo"
)

type Kind string

const (
	KindFile Kind = "file" // plain video → MediaInfo
	KindBDMV Kind = "bdmv" // folder with BDMV/ → BDInfo
	KindISO  Kind = "iso"  // .iso → BDInfo (UDF) ; fallback MediaInfo
)

func Detect(abs string) Kind {
	if info, err := os.Stat(abs); err == nil && info.IsDir() {
		if _, err := os.Stat(filepath.Join(abs, "BDMV")); err == nil {
			return KindBDMV
		}
		return KindFile
	}
	if strings.EqualFold(filepath.Ext(abs), ".iso") {
		return KindISO
	}
	return KindFile
}

// MediaInfo renders the MediaInfo text report for one file.
func MediaInfo(ctx context.Context, abs string) (string, error) {
	rep, err := mediainfo.AnalyzeFileContext(ctx, abs, mediainfo.WithParseSpeed(0.5))
	if err != nil {
		return "", err
	}
	return mediainfo.Render([]mediainfo.Report{rep}, mediainfo.OutputText)
}

// MediaInfoJSON returns the structured report (for parsing resolution/HDR hints).
func MediaInfoJSON(ctx context.Context, abs string) (string, error) {
	rep, err := mediainfo.AnalyzeFileContext(ctx, abs, mediainfo.WithParseSpeed(0.5))
	if err != nil {
		return "", err
	}
	return mediainfo.Render([]mediainfo.Report{rep}, mediainfo.OutputJSON)
}

type BDInfoResult struct {
	Report       string                `json:"report"`
	QuickSummary string                `json:"quickSummary"`
	Forums       string                `json:"forums"`
	Disc         bdinfo.DiscInfo       `json:"disc"`
	Playlists    []bdinfo.PlaylistInfo `json:"playlists"`
	Errors       map[string]string     `json:"errors,omitempty"`
}

type BDInfoOptions struct {
	Playlist    string // "" = auto (largest)
	MainOnly    bool
	SummaryOnly bool
}

// BDInfo scans a BDMV folder or ISO. onProgress gets 0..1.
func BDInfo(ctx context.Context, abs string, opt BDInfoOptions, onProgress func(p float64, stage string)) (*BDInfoResult, error) {
	st := bdinfo.DefaultSettings("")
	st.GenerateTextSummary = true
	st.PlaylistOnly = opt.Playlist
	st.MainPlaylistOnly = opt.MainOnly || opt.Playlist == ""
	st.SummaryOnly = opt.SummaryOnly
	res, err := bdinfo.Run(ctx, bdinfo.Options{
		Path:     abs,
		Settings: st,
		OnProgress: func(e bdinfo.ProgressEvent) {
			if onProgress == nil {
				return
			}
			p := 0.0
			if e.TotalBytes > 0 {
				p = float64(e.ProcessedBytes) / float64(e.TotalBytes)
			}
			onProgress(p, string(e.Stage))
		},
	})
	if err != nil {
		return nil, err
	}
	out := &BDInfoResult{
		Report: res.Report, QuickSummary: res.QuickSummary, Forums: res.ForumsBlock,
		Disc: res.Disc, Playlists: res.Playlists, Errors: res.Scan.FileErrors,
	}
	if res.Scan.ScanError != "" {
		return out, fmt.Errorf("scan: %s", res.Scan.ScanError)
	}
	return out, nil
}

// Playlists lists playlists without scanning streams (fast).
func Playlists(ctx context.Context, abs string) ([]bdinfo.PlaylistInfo, bdinfo.DiscInfo, error) {
	res, err := bdinfo.DiscoverPlaylists(ctx, bdinfo.Options{Path: abs, Settings: bdinfo.DefaultSettings("")})
	if err != nil {
		return nil, bdinfo.DiscInfo{}, err
	}
	return res.Playlists, res.Disc, nil
}
