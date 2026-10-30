package fsroot

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func newSvc(t *testing.T) (*Service, string, string) {
	t.Helper()
	base := t.TempDir()
	in := filepath.Join(base, "inside")
	out := filepath.Join(base, "outside")
	os.MkdirAll(filepath.Join(in, "sub"), 0o755)
	os.MkdirAll(out, 0o755)
	os.WriteFile(filepath.Join(in, "sub", "a.mkv"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(out, "secret.txt"), []byte("s"), 0o644)
	s, errs := New(map[string]string{"data": in})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	t.Cleanup(s.Close)
	return s, in, out
}

func TestDotDotEscape(t *testing.T) {
	s, _, _ := newSvc(t)
	for _, p := range []string{"data/../outside", "data/../../", "data/sub/../../outside/secret.txt", "nope/x"} {
		if _, err := s.Resolve(p); err == nil {
			t.Errorf("Resolve(%q) should fail", p)
		}
		if _, err := s.List(p); err == nil {
			t.Errorf("List(%q) should fail", p)
		}
	}
}

func TestSymlinkEscape(t *testing.T) {
	s, in, out := newSvc(t)
	link := filepath.Join(in, "link")
	if err := os.Symlink(out, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("symlink needs privilege on windows:", err)
		}
		t.Fatal(err)
	}
	_, err := s.Resolve("data/link/secret.txt")
	if err == nil {
		t.Fatal("symlink escape not blocked")
	}
	if !errors.Is(err, ErrOutsideRoot) {
		t.Logf("blocked with non-canonical error (ok): %v", err)
	}
	if _, err := s.List("data/link"); err == nil {
		t.Fatal("List through escaping symlink should fail")
	}
}

func TestNormalOps(t *testing.T) {
	s, in, _ := newSvc(t)
	l, err := s.List("data/sub")
	if err != nil || len(l) != 1 || l[0].Kind != "video" || l[0].Path != "data/sub/a.mkv" {
		t.Fatalf("List: %v %+v", err, l)
	}
	if err := s.Mkdir("data/new/deep"); err != nil {
		t.Fatal(err)
	}
	if err := s.Copy("data/sub", "data/new", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(in, "new", "sub", "a.mkv")); err != nil {
		t.Fatal("copy missing:", err)
	}
	if err := s.Rename("data/new/sub", "renamed"); err != nil {
		t.Fatal(err)
	}
	if err := s.Move("data/new/renamed", "data", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("data"); !errors.Is(err, ErrIsRoot) {
		t.Fatal("root delete must be refused, got", err)
	}
	if err := s.Delete("data/renamed"); err != nil {
		t.Fatal(err)
	}
	abs, err := s.Resolve("data/sub/a.mkv")
	if err != nil || abs != filepath.Join(in, "sub", "a.mkv") {
		t.Fatalf("Resolve: %v %q", err, abs)
	}
	if v := s.Virtual(abs); v != "data/sub/a.mkv" {
		t.Fatal("Virtual:", v)
	}
}

func TestSingle(t *testing.T) {
	d := t.TempDir()
	o, err := OpenSingle(d)
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	os.WriteFile(filepath.Join(d, "x.png"), []byte("p"), 0o644)
	if _, err := o.Path("x.png"); err != nil {
		t.Fatal(err)
	}
	// ".." is clamped at the sandbox top (like http.FileServer), never escapes.
	for _, p := range []string{"../x.png", "/../../x.png", "a/../../x.png"} {
		got, err := o.Path(p)
		if err != nil || got != filepath.Join(d, "x.png") {
			t.Fatalf("Path(%q) = %q, %v", p, got, err)
		}
	}
	if _, err := o.Path("../../../../Windows"); err == nil {
		t.Fatal("must not resolve outside")
	}
}
