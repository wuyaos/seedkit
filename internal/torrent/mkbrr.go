package torrent

import (
	"context"
	"fmt"
	mkbrr "github.com/autobrr/mkbrr/torrent"
	"os"
	"path/filepath"
)

func createMkbrr(ctx context.Context, src, dst string, opt Options, progress func(float64, string)) (*Result, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return nil, err
	}
	o := mkbrr.CreateOptions{Path: src, OutputPath: dst, Comment: opt.Comment, Source: opt.Source, IsPrivate: opt.Private, Workers: opt.Workers, Quiet: true, SkipPrefix: true, Version: opt.CreatedBy, ProgressCallback: func(d, t int, _ float64) {
		if progress != nil && t > 0 {
			progress(float64(d)/float64(t), fmt.Sprintf("校验分块 %d/%d", d, t))
		}
	}}
	if opt.Announce != "" {
		o.TrackerURLs = []string{opt.Announce}
	}
	if opt.PieceSizeExp > 0 {
		e := opt.PieceSizeExp
		o.PieceLengthExp = &e
	}
	type rr struct {
		t   *mkbrr.Torrent
		err error
	}
	ch := make(chan rr, 1)
	go func() { t, e := mkbrr.CreateTorrent(o); ch <- rr{t, e} }()
	var x rr
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case x = <-ch:
	}
	if x.err != nil {
		return nil, x.err
	}
	info, e := x.t.MetaInfo.UnmarshalInfo()
	if e != nil {
		return nil, e
	}
	f, e := os.CreateTemp(filepath.Dir(dst), ".seedkit-torrent-*.tmp")
	if e != nil {
		return nil, e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	writeErr := x.t.Write(f)
	closeErr := f.Close()
	if writeErr != nil {
		return nil, writeErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	if e = os.Rename(tmp, dst); e != nil {
		return nil, e
	}
	total := info.Length
	files := len(info.Files)
	if files == 0 {
		files = 1
	} else {
		total = 0
		for _, fi := range info.Files {
			total += fi.Length
		}
	}
	if progress != nil {
		progress(1, "")
	}
	return &Result{Path: dst, InfoHash: x.t.MetaInfo.HashInfoBytes().String(), TotalBytes: total, Pieces: info.NumPieces(), PieceSize: info.PieceLength, Files: files}, nil
}
