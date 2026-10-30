// Package torrent is a thin adapter over github.com/autobrr/mkbrr/torrent.
// It adds context cancellation and maps mkbrr's progress callback to ours.
package torrent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	mkbrr "github.com/autobrr/mkbrr/torrent"
)

type Options struct {
	Announce     string
	Private      bool
	Source       string
	Comment      string
	PieceSizeExp uint // 0 = auto (mkbrr chooses by tracker rules / size)
	Workers      int
	CreatedBy    string
}

type Result struct {
	Path       string `json:"path"`
	InfoHash   string `json:"infoHash"`
	TotalBytes int64  `json:"totalBytes"`
	Pieces     int    `json:"pieces"`
	PieceSize  int64  `json:"pieceSize"`
	Files      int    `json:"files"`
}

// Create writes a .torrent for src to dst. progress receives 0..1.
// mkbrr's hasher has no context hook, so cancellation returns early while the
// hashing goroutine finishes in the background; its output file is removed.
func Create(ctx context.Context, src, dst string, opt Options, progress func(p float64, msg string)) (*Result, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return nil, err
	}
	o := mkbrr.CreateOptions{
		Path:       src,
		OutputPath: dst,
		Comment:    opt.Comment,
		Source:     opt.Source,
		IsPrivate:  opt.Private,
		Workers:    opt.Workers,
		Quiet:      true,
		SkipPrefix: true,
		Version:    opt.CreatedBy,
		ProgressCallback: func(done, total int, rate float64) {
			if progress != nil && total > 0 {
				progress(float64(done)/float64(total), fmt.Sprintf("hashing %d/%d pieces", done, total))
			}
		},
	}
	if opt.Announce != "" {
		o.TrackerURLs = []string{opt.Announce}
	}
	if opt.PieceSizeExp > 0 {
		e := opt.PieceSizeExp
		o.PieceLengthExp = &e
	}

	// mkbrr.Create() leaves TorrentInfo.MetaInfo nil, so use CreateTorrent and
	// write the file ourselves.
	type res struct {
		t   *mkbrr.Torrent
		err error
	}
	ch := make(chan res, 1)
	go func() {
		t, err := mkbrr.CreateTorrent(o)
		ch <- res{t, err}
	}()
	var r res
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r = <-ch:
	}
	if r.err != nil {
		return nil, r.err
	}
	info, err := r.t.MetaInfo.UnmarshalInfo()
	if err != nil {
		return nil, err
	}
	f, err := os.Create(dst)
	if err != nil {
		return nil, err
	}
	if err := r.t.Write(f); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	if progress != nil {
		progress(1, "done")
	}
	files := len(info.Files)
	total := info.Length
	if files == 0 {
		files = 1
	} else {
		total = 0
		for _, fi := range info.Files {
			total += fi.Length
		}
	}
	return &Result{
		Path:       dst,
		InfoHash:   r.t.MetaInfo.HashInfoBytes().String(),
		TotalBytes: total,
		Pieces:     info.NumPieces(),
		PieceSize:  info.PieceLength,
		Files:      files,
	}, nil
}
