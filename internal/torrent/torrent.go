// Package torrent keeps ordinary mkbrr creation and adds exact v1 file selection.
package torrent

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/bmatcuk/doublestar/v4"
)

type Selection struct {
	Snapshot string   `json:"snapshot"`
	Paths    []string `json:"paths"`
}
type PreviewFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Mtime  int64  `json:"mtime"`
	Reason string `json:"reason"`
}
type Preview struct {
	Files      []PreviewFile `json:"files"`
	Snapshot   string        `json:"snapshot"`
	IsDir      bool          `json:"isDir"`
	TotalBytes int64         `json:"totalBytes"`
}
type Options struct {
	Announce     string
	Private      bool
	Source       string
	Comment      string
	PieceSizeExp uint
	Workers      int
	CreatedBy    string
	Exclude      []string
	Selection    *Selection
}
type Result struct {
	Path       string   `json:"path"`
	InfoHash   string   `json:"infoHash"`
	TotalBytes int64    `json:"totalBytes"`
	Pieces     int      `json:"pieces"`
	PieceSize  int64    `json:"pieceSize"`
	Files      int      `json:"files"`
	Excluded   []string `json:"excluded,omitempty"`
}

const maxScanEntries = 100000

func normalizeRule(r string) string { return strings.TrimSpace(strings.ReplaceAll(r, "\\", "/")) }
func ValidateRules(rules []string) error {
	if len(rules) > 1000 {
		return errors.New("排除规则不能超过 1000 条")
	}
	for _, raw := range rules {
		r := normalizeRule(raw)
		if r == "" || len(r) > 4096 || strings.HasPrefix(r, "/") || strings.Contains(r, ":") || strings.HasPrefix(r, "!") {
			return fmt.Errorf("无效排除规则 %q", raw)
		}
		for _, part := range strings.Split(r, "/") {
			if part == ".." {
				return fmt.Errorf("排除规则不能包含 ..: %q", raw)
			}
		}
		if _, err := doublestar.Match(r, "probe"); err != nil {
			return fmt.Errorf("无效排除规则 %q: %w", raw, err)
		}
	}
	return nil
}

// Directory rules apply to every descendant; keeping excluded entries in the
// scan makes both their reasons and later filesystem changes visible.
func ignored(rel string, rules []string) string {
	lower := strings.ToLower(rel)
	parts := strings.Split(lower, "/")
	for _, part := range parts {
		if part == "@eadir" {
			return "系统忽略：@eaDir"
		}
	}
	for _, s := range []string{".torrent", ".ds_store", "thumbs.db", "desktop.ini", "zone.identifier"} {
		if strings.HasSuffix(lower, s) {
			return "系统忽略：" + s
		}
	}
	for _, raw := range rules {
		r := strings.ToLower(normalizeRule(raw))
		if !strings.Contains(r, "/") {
			r = "**/" + r
		}
		for i := 1; i <= len(parts); i++ {
			p := strings.Join(parts[:i], "/")
			match, _ := doublestar.Match(r, p)
			if !match && i < len(parts) {
				match, _ = doublestar.Match(r, p+"/")
			}
			if match {
				return "规则：" + raw
			}
		}
	}
	return ""
}

type scanEntry struct {
	Path        string
	Size        int64
	Nanoseconds int64
	Mode        uint32
}

func Scan(ctx context.Context, src string, exclude []string) (*Preview, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ValidateRules(exclude); err != nil {
		return nil, err
	}
	src, err := filepath.Abs(src)
	if err != nil {
		return nil, err
	}
	st, err := os.Lstat(src)
	if err != nil {
		return nil, err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("不支持以符号链接作为制种源")
	}
	if !st.IsDir() && !st.Mode().IsRegular() {
		return nil, errors.New("制种源必须是普通文件或目录")
	}
	p := &Preview{Files: []PreviewFile{}, IsDir: st.IsDir()}
	entries := []scanEntry{}
	add := func(rel string, info fs.FileInfo) error {
		if len(entries) >= maxScanEntries {
			return fmt.Errorf("目录条目超过 %d，请缩小制种源范围", maxScanEntries)
		}
		entries = append(entries, scanEntry{rel, info.Size(), info.ModTime().UnixNano(), uint32(info.Mode())})
		if info.IsDir() {
			return nil
		}
		reason := ignored(rel, exclude)
		if !info.Mode().IsRegular() {
			reason = "不支持符号链接或非普通文件"
		}
		p.Files = append(p.Files, PreviewFile{Path: rel, Size: info.Size(), Mtime: info.ModTime().UnixMilli(), Reason: reason})
		if reason == "" {
			if info.Size() > int64(^uint64(0)>>1)-p.TotalBytes {
				return errors.New("文件总大小溢出")
			}
			p.TotalBytes += info.Size()
		}
		return nil
	}
	if !st.IsDir() {
		// User directory rules do not apply to a single-file source.
		if err = add(filepath.Base(src), st); err != nil {
			return nil, err
		}
		if p.Files[0].Reason != "" && strings.HasPrefix(p.Files[0].Reason, "规则：") {
			p.Files[0].Reason = ""
			p.TotalBytes = st.Size()
		}
	} else {
		root, e := os.OpenRoot(src)
		if e != nil {
			return nil, e
		}
		defer root.Close()
		err = fs.WalkDir(root.FS(), ".", func(rel string, d fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				return walkErr
			}
			info, e := d.Info()
			if e != nil {
				return e
			}
			// Do not bind root directory mtime: writing a .torrent beside the source
			// changes it without changing the scanned file collection.
			if rel == "." {
				return nil
			}
			return add(rel, info)
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(p.Files, func(i, j int) bool { return p.Files[i].Path < p.Files[j].Path })
	data, err := json.Marshal(struct {
		Source  string
		Rules   []string
		Entries []scanEntry
	}{src, exclude, entries})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	p.Snapshot = hex.EncodeToString(sum[:])
	return p, nil
}

func Create(ctx context.Context, src, dst string, opt Options, progress func(float64, string)) (*Result, error) {
	if len(opt.Exclude) == 0 && opt.Selection == nil {
		return createMkbrr(ctx, src, dst, opt, progress)
	}
	preview, err := Scan(ctx, src, opt.Exclude)
	if err != nil {
		return nil, err
	}
	chosen := map[string]bool{}
	if opt.Selection != nil {
		if opt.Selection.Snapshot == "" || opt.Selection.Snapshot != preview.Snapshot {
			return nil, errors.New("源文件或规则已变化，请重新选择种子内容")
		}
		for _, p := range opt.Selection.Paths {
			if chosen[p] {
				return nil, errors.New("选择清单有重复路径")
			}
			chosen[p] = true
		}
	} else {
		for _, f := range preview.Files {
			if f.Reason == "" {
				chosen[f.Path] = true
			}
		}
	}
	files := []PreviewFile{}
	excluded := []string{}
	for _, f := range preview.Files {
		if chosen[f.Path] {
			if f.Reason != "" {
				return nil, fmt.Errorf("选中文件不可用：%s", f.Path)
			}
			files = append(files, f)
			delete(chosen, f.Path)
		} else {
			excluded = append(excluded, f.Path)
		}
	}
	if len(chosen) != 0 {
		return nil, errors.New("选择清单包含不存在的精确路径")
	}
	if len(files) == 0 {
		return nil, errors.New("至少选择一个可用文件")
	}
	return createSelected(ctx, src, dst, opt, preview, files, excluded, progress)
}

func createSelected(ctx context.Context, src, dst string, opt Options, preview *Preview, files []PreviewFile, excluded []string, progress func(float64, string)) (*Result, error) {
	total := int64(0)
	for _, f := range files {
		total += f.Size
	}
	exp := opt.PieceSizeExp
	if exp == 0 {
		exp = 18
		for exp < 24 && total/(int64(1)<<exp) > 2048 {
			exp++
		}
	}
	if exp < 14 || exp > 26 {
		return nil, errors.New("分块指数必须在 14–26 之间")
	}
	piece := int64(1) << exp
	count := total / piece
	if total%piece != 0 {
		count++
	}
	if count > 32*1024*1024/sha1.Size {
		return nil, errors.New("分块数据超过 32 MiB，请增大分块或缩小内容")
	}
	pieces := make([]byte, 0, int(count)*sha1.Size)
	h := sha1.New()
	used := int64(0)
	done := int64(0)
	buffer := make([]byte, 64*1024)
	var lastProgress time.Time
	base := src
	if !preview.IsDir {
		base = filepath.Dir(src)
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	for _, f := range files {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		path := filepath.FromSlash(f.Path)
		st, e := root.Lstat(path)
		if e != nil {
			return nil, e
		}
		if !st.Mode().IsRegular() || st.Size() != f.Size || st.ModTime().UnixMilli() != f.Mtime {
			return nil, fmt.Errorf("文件已变化：%s", f.Path)
		}
		in, e := root.Open(path)
		if e != nil {
			return nil, e
		}
		opened, e := in.Stat()
		if e != nil || !os.SameFile(st, opened) {
			in.Close()
			return nil, errors.New("文件在打开时发生变化")
		}
		remaining := f.Size
		for remaining > 0 {
			if err = ctx.Err(); err != nil {
				in.Close()
				return nil, err
			}
			nmax := int64(len(buffer))
			if nmax > piece-used {
				nmax = piece - used
			}
			if nmax > remaining {
				nmax = remaining
			}
			n, e := io.ReadFull(in, buffer[:nmax])
			if e != nil {
				in.Close()
				return nil, fmt.Errorf("读取文件失败：%s: %w", f.Path, e)
			}
			_, _ = h.Write(buffer[:n])
			used += int64(n)
			done += int64(n)
			remaining -= int64(n)
			if used == piece {
				pieces = h.Sum(pieces)
				h.Reset()
				used = 0
			}
			if progress != nil && total > 0 && (lastProgress.IsZero() || time.Since(lastProgress) >= 200*time.Millisecond) {
				lastProgress = time.Now()
				progress(float64(done)/float64(total), "校验文件 "+f.Path)
			}
		}
		final, e := in.Stat()
		closeErr := in.Close()
		if e != nil {
			return nil, e
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if !os.SameFile(st, final) || final.Size() != st.Size() || !final.ModTime().Equal(st.ModTime()) {
			return nil, fmt.Errorf("哈希期间文件变化：%s", f.Path)
		}
	}
	if used > 0 {
		pieces = h.Sum(pieces)
	}
	after, err := Scan(ctx, src, opt.Exclude)
	if err != nil {
		return nil, err
	}
	if after.Snapshot != preview.Snapshot {
		return nil, errors.New("哈希期间目录内容已变化，请重新选择")
	}
	// Required v1 fields must also be encoded for an all-zero-length selection.
	// metainfo.Info uses omitempty on pieces/length, so use an explicit dictionary.
	info := map[string]any{"name": filepath.Base(filepath.Clean(src)), "piece length": piece, "pieces": string(pieces), "private": int64(0)}
	if opt.Private {
		info["private"] = int64(1)
	}
	if opt.Source != "" {
		info["source"] = opt.Source
	}
	if preview.IsDir {
		list := make([]metainfo.FileInfo, 0, len(files))
		for _, f := range files {
			list = append(list, metainfo.FileInfo{Length: f.Size, Path: strings.Split(f.Path, "/")})
		}
		info["files"] = list
	} else {
		info["length"] = files[0].Size
	}
	encoded, err := bencode.Marshal(info)
	if err != nil {
		return nil, err
	}
	mi := &metainfo.MetaInfo{InfoBytes: encoded, Announce: opt.Announce, Comment: opt.Comment, CreatedBy: opt.CreatedBy, CreationDate: time.Now().Unix()}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".seedkit-torrent-*.tmp")
	if err != nil {
		return nil, err
	}
	name := tmp.Name()
	defer os.Remove(name)
	writeErr := mi.Write(tmp)
	closeErr := tmp.Close()
	if writeErr != nil {
		return nil, writeErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = os.Rename(name, dst); err != nil {
		return nil, err
	}
	if progress != nil {
		progress(1, "")
	}
	return &Result{Path: dst, InfoHash: mi.HashInfoBytes().String(), TotalBytes: total, Pieces: len(pieces) / sha1.Size, PieceSize: piece, Files: len(files), Excluded: excluded}, nil
}
