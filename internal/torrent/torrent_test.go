package torrent

import (
	"context"
	"github.com/autobrr/go-torrent/metainfo"
	"os"
	"path/filepath"
	"testing"
)

func TestScanAndSelectedTorrent(t *testing.T) {
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "a[1].txt"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "x.torrent"), []byte("ignored"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := Scan(context.Background(), d, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Files) != 2 || p.TotalBytes != 5 {
		t.Fatalf("preview=%+v", p)
	}
	out := filepath.Join(t.TempDir(), "out.torrent")
	r, err := Create(context.Background(), d, out, Options{Selection: &Selection{Snapshot: p.Snapshot, Paths: []string{"a[1].txt"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.TotalBytes != 5 {
		t.Fatalf("result=%+v", r)
	}
	mi, err := metainfo.LoadFromFile(out)
	if err != nil {
		t.Fatal(err)
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Files) != 1 || info.Files[0].Path[0] != "a[1].txt" {
		t.Fatalf("info=%+v", info)
	}
}
func TestValidateRules(t *testing.T) {
	if ValidateRules([]string{"../x"}) == nil {
		t.Fatal("expected invalid rule")
	}
	if ValidateRules([]string{"["}) == nil {
		t.Fatal("expected invalid glob")
	}
}
