// Package fsroot exposes a sandboxed view over configured root directories.
//
// Every public method takes a virtual path "<root>/<rel>". All file access goes
// through os.Root, so neither ".." nor symlinks can escape a root: the kernel
// (openat2/RESOLVE_BENEATH on Linux, equivalent checks on Windows) enforces it,
// not string prefix matching.
//
// Resolve returns an absolute OS path for handing to subprocesses (ffmpeg,
// bdinfo). It verifies the target through the Root first, so a symlink that
// points outside is rejected before the path is ever used.
package fsroot

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var (
	ErrOutsideRoot = errors.New("path escapes root")
	ErrNoRoot      = errors.New("unknown root")
	ErrIsRoot      = errors.New("refusing to operate on root itself")
)

type root struct {
	name string
	abs  string
	h    *os.Root
}

type Service struct {
	roots map[string]*root
}

// New opens every configured root. Roots that cannot be opened are skipped and
// returned in the error list so the caller can log them; the service remains
// usable with the rest.
func New(roots map[string]string) (*Service, []error) {
	s := &Service{roots: map[string]*root{}}
	var errs []error
	for name, p := range roots {
		abs, err := filepath.Abs(p)
		if err != nil {
			errs = append(errs, fmt.Errorf("root %s: %w", name, err))
			continue
		}
		_ = os.MkdirAll(abs, 0o755)
		h, err := os.OpenRoot(abs)
		if err != nil {
			errs = append(errs, fmt.Errorf("root %s (%s): %w", name, abs, err))
			continue
		}
		s.roots[name] = &root{name: name, abs: abs, h: h}
	}
	return s, errs
}

// Close releases the root handles. Call when replacing the service.
func (s *Service) Close() {
	for _, r := range s.roots {
		_ = r.h.Close()
	}
}

type Root struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func (s *Service) Roots() []Root {
	out := make([]Root, 0, len(s.roots))
	for _, r := range s.roots {
		out = append(out, Root{Name: r.name, Path: r.abs})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// split turns "root/a/b" into (root, "a/b"). rel is "." for the root itself.
func (s *Service) split(vpath string) (*root, string, error) {
	vpath = strings.TrimPrefix(path.Clean("/"+vpath), "/")
	name, rel, _ := strings.Cut(vpath, "/")
	r, ok := s.roots[name]
	if !ok {
		return nil, "", ErrNoRoot
	}
	if rel == "" {
		rel = "."
	}
	return r, filepath.FromSlash(rel), nil
}

// Resolve returns the absolute OS path for a virtual path, after verifying
// through os.Root that it exists inside the root (symlinks included).
func (s *Service) Resolve(vpath string) (string, error) {
	r, rel, err := s.split(vpath)
	if err != nil {
		return "", err
	}
	if _, err := r.h.Stat(rel); err != nil {
		return "", mapErr(err)
	}
	return filepath.Join(r.abs, rel), nil
}

// Virtual converts an absolute path back to "root/rel", or "" if not under any root.
func (s *Service) Virtual(abs string) string {
	for _, r := range s.roots {
		if abs == r.abs {
			return r.name
		}
		if strings.HasPrefix(abs, r.abs+string(filepath.Separator)) {
			return r.name + "/" + filepath.ToSlash(abs[len(r.abs)+1:])
		}
	}
	return ""
}

// mapErr normalises os.Root escape errors to ErrOutsideRoot.
func mapErr(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) && strings.Contains(pe.Err.Error(), "escapes from parent") {
		return ErrOutsideRoot
	}
	return err
}

type Entry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"` // virtual
	IsDir   bool      `json:"isDir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
	Kind    string    `json:"kind"` // video|image|text|archive|torrent|bdmv|iso|other|dir
}

func (s *Service) List(vpath string) ([]Entry, error) {
	r, rel, err := s.split(vpath)
	if err != nil {
		return nil, err
	}
	ents, err := fs.ReadDir(r.h.FS(), filepath.ToSlash(rel))
	if err != nil {
		return nil, mapErr(err)
	}
	vbase := strings.Trim(vpath, "/")
	out := make([]Entry, 0, len(ents))
	for _, e := range ents {
		info, err := e.Info()
		if err != nil {
			continue
		}
		en := Entry{
			Name: e.Name(), IsDir: e.IsDir(), ModTime: info.ModTime(),
			Path: path.Join(vbase, e.Name()),
		}
		if !e.IsDir() {
			en.Size = info.Size()
			en.Kind = kindOf(e.Name())
		} else {
			en.Kind = "dir"
			if _, err := r.h.Stat(filepath.Join(rel, e.Name(), "BDMV")); err == nil {
				en.Kind = "bdmv"
			}
		}
		out = append(out, en)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func kindOf(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mkv", ".mp4", ".m2ts", ".ts", ".avi", ".mov", ".wmv", ".webm", ".flv", ".vob", ".mpg", ".mpeg", ".m4v":
		return "video"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return "image"
	case ".txt", ".nfo", ".md", ".log", ".srt", ".ass", ".ssa", ".sub", ".json", ".yaml", ".yml", ".xml":
		return "text"
	case ".zip", ".rar", ".7z", ".tar", ".gz":
		return "archive"
	case ".torrent":
		return "torrent"
	case ".iso":
		return "iso"
	}
	return "other"
}

func (s *Service) Stat(vpath string) (Entry, error) {
	r, rel, err := s.split(vpath)
	if err != nil {
		return Entry{}, err
	}
	info, err := r.h.Stat(rel)
	if err != nil {
		return Entry{}, mapErr(err)
	}
	name := info.Name()
	if rel == "." {
		name = r.name
	}
	en := Entry{Name: name, Path: strings.Trim(vpath, "/"), IsDir: info.IsDir(), Size: info.Size(), ModTime: info.ModTime()}
	if en.IsDir {
		en.Kind = "dir"
		if _, err := r.h.Stat(filepath.Join(rel, "BDMV")); err == nil {
			en.Kind = "bdmv"
		}
	} else {
		en.Kind = kindOf(en.Name)
	}
	return en, nil
}

func (s *Service) Mkdir(vpath string) error {
	r, rel, err := s.split(vpath)
	if err != nil {
		return err
	}
	return mapErr(r.h.MkdirAll(rel, 0o755))
}

func validName(n string) bool {
	return n != "" && n != "." && n != ".." && !strings.ContainsAny(n, `/\`)
}

func (s *Service) Rename(vpath, newName string) error {
	if !validName(newName) {
		return fmt.Errorf("invalid name %q", newName)
	}
	r, rel, err := s.split(vpath)
	if err != nil {
		return err
	}
	if rel == "." {
		return ErrIsRoot
	}
	return mapErr(r.h.Rename(rel, filepath.Join(filepath.Dir(rel), newName)))
}

func (s *Service) Delete(vpath string) error {
	r, rel, err := s.split(vpath)
	if err != nil {
		return err
	}
	if rel == "." {
		return ErrIsRoot
	}
	return mapErr(r.h.RemoveAll(rel))
}

// Move moves src into dstDir (both virtual). Same root: rename, falling back to
// copy+delete across devices. Different roots: copy+delete.
func (s *Service) Move(src, dstDir string, progress func(done, total int64)) error {
	sr, srel, err := s.split(src)
	if err != nil {
		return err
	}
	if srel == "." {
		return ErrIsRoot
	}
	dr, drel, err := s.split(dstDir)
	if err != nil {
		return err
	}
	target := filepath.Join(drel, filepath.Base(srel))
	if sr == dr {
		if err := sr.h.Rename(srel, target); err == nil {
			return nil
		}
	}
	if err := copyTree(sr.h, srel, dr.h, target, progress); err != nil {
		return err
	}
	return mapErr(sr.h.RemoveAll(srel))
}

func (s *Service) Copy(src, dstDir string, progress func(done, total int64)) error {
	sr, srel, err := s.split(src)
	if err != nil {
		return err
	}
	dr, drel, err := s.split(dstDir)
	if err != nil {
		return err
	}
	return copyTree(sr.h, srel, dr.h, filepath.Join(drel, filepath.Base(srel)), progress)
}

func treeSize(h *os.Root, rel string) (int64, error) {
	var n int64
	err := fs.WalkDir(h.FS(), filepath.ToSlash(rel), func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n, err
}

func copyTree(sh *os.Root, src string, dh *os.Root, dst string, progress func(done, total int64)) error {
	total, err := treeSize(sh, src)
	if err != nil {
		return mapErr(err)
	}
	var done int64
	onWrite := func(n int64) {
		done += n
		if progress != nil {
			progress(done, total)
		}
	}
	info, err := sh.Stat(src)
	if err != nil {
		return mapErr(err)
	}
	if !info.IsDir() {
		return copyFile(sh, src, dh, dst, onWrite)
	}
	srcSlash := filepath.ToSlash(src)
	return fs.WalkDir(sh.FS(), srcSlash, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, srcSlash), "/")
		target := filepath.Join(dst, filepath.FromSlash(rel))
		if d.IsDir() {
			return mapErr(dh.MkdirAll(target, 0o755))
		}
		return copyFile(sh, filepath.FromSlash(p), dh, target, onWrite)
	})
}

func copyFile(sh *os.Root, src string, dh *os.Root, dst string, onWrite func(int64)) error {
	in, err := sh.Open(src)
	if err != nil {
		return mapErr(err)
	}
	defer in.Close()
	if err := dh.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return mapErr(err)
	}
	out, err := dh.Create(dst)
	if err != nil {
		return mapErr(err)
	}
	buf := make([]byte, 1<<20)
	for {
		n, rerr := in.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				return werr
			}
			onWrite(int64(n))
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			return rerr
		}
	}
	return out.Close()
}

// Open returns a file for streaming download.
func (s *Service) Open(vpath string) (*os.File, os.FileInfo, error) {
	r, rel, err := s.split(vpath)
	if err != nil {
		return nil, nil, err
	}
	f, err := r.h.Open(rel)
	if err != nil {
		return nil, nil, mapErr(err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return f, info, nil
}

// Zip streams a directory as a zip archive (store, no compression: media is incompressible).
func (s *Service) Zip(vpath string, w io.Writer) error {
	r, rel, err := s.split(vpath)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(w)
	defer zw.Close()
	base := filepath.Base(rel)
	if rel == "." {
		base = r.name
	}
	relSlash := filepath.ToSlash(rel)
	return fs.WalkDir(r.h.FS(), relSlash, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		sub := strings.TrimPrefix(strings.TrimPrefix(p, relSlash), "/")
		name := path.Join(base, sub)
		if d.IsDir() {
			if sub == "" {
				return nil
			}
			_, err := zw.Create(name + "/")
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		h, _ := zip.FileInfoHeader(info)
		h.Name = name
		h.Method = zip.Store
		fw, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		f, err := r.h.Open(filepath.FromSlash(p))
		if err != nil {
			return err
		}
		_, err = io.Copy(fw, f)
		f.Close()
		return err
	})
}

// ReadText reads at most limit bytes of a text file for preview.
func (s *Service) ReadText(vpath string, limit int64) (string, bool, error) {
	f, info, err := s.Open(vpath)
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return "", false, err
	}
	return string(b), info.Size() > limit, nil
}

// VideoFiles lists absolute paths of video files under a virtual path
// (recursively if dir), for batch ops.
func (s *Service) VideoFiles(vpath string) ([]string, error) {
	r, rel, err := s.split(vpath)
	if err != nil {
		return nil, err
	}
	info, err := r.h.Stat(rel)
	if err != nil {
		return nil, mapErr(err)
	}
	if !info.IsDir() {
		return []string{filepath.Join(r.abs, rel)}, nil
	}
	var out []string
	err = fs.WalkDir(r.h.FS(), filepath.ToSlash(rel), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && kindOf(d.Name()) == "video" {
			out = append(out, filepath.Join(r.abs, filepath.FromSlash(p)))
		}
		return nil
	})
	return out, err
}

// Single wraps one directory as a root-less sandbox (used for the output dir).
type Single struct {
	abs string
	h   *os.Root
}

func OpenSingle(dir string) (*Single, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	h, err := os.OpenRoot(abs)
	if err != nil {
		return nil, err
	}
	return &Single{abs: abs, h: h}, nil
}

func (o *Single) Abs() string    { return o.abs }
func (o *Single) FS() fs.FS      { return o.h.FS() }
func (o *Single) Close() error   { return o.h.Close() }
func (o *Single) Root() *os.Root { return o.h }

// Path verifies rel through the Root and returns its absolute path.
func (o *Single) Path(rel string) (string, error) {
	rel = filepath.FromSlash(strings.TrimPrefix(path.Clean("/"+filepath.ToSlash(rel)), "/"))
	if rel == "" {
		rel = "."
	}
	if _, err := o.h.Stat(rel); err != nil {
		return "", mapErr(err)
	}
	return filepath.Join(o.abs, rel), nil
}

// Rel converts an absolute path under the dir to a slash-separated relative one.
func (o *Single) Rel(abs string) (string, bool) {
	r, err := filepath.Rel(o.abs, abs)
	if err != nil || strings.HasPrefix(r, "..") {
		return "", false
	}
	return filepath.ToSlash(r), true
}
