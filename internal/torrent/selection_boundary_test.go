package torrent

import (
	"bytes"
	"context"
	"crypto/sha1"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
)

func fixtureFiles(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	for p, b := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, b, 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
func TestSelectionRulesShowAllReasons(t *testing.T) {
	dir := fixtureFiles(t, map[string][]byte{"Movie.mkv": {1}, "Sample/Clip.mkv": {2}, "deep/SAMPLE/clip.mkv": {3}, "deep/a.LOG": {4}, "@eaDir/cache": {5}, "out.torrent": {6}})
	p, err := Scan(context.Background(), dir, []string{`**\sample\**`, `*.log`})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Files) != 6 {
		t.Fatalf("excluded entries disappeared: %+v", p.Files)
	}
	for _, f := range p.Files {
		if f.Path == "Movie.mkv" {
			if f.Reason != "" {
				t.Fatal(f)
			}
		} else if f.Reason == "" {
			t.Fatalf("missing exclusion reason: %s", f.Path)
		}
	}
	if p.TotalBytes != 1 {
		t.Fatal(p.TotalBytes)
	}
}
func TestSelectedCrossFilePiecesAndMetadata(t *testing.T) {
	a := bytes.Repeat([]byte{0x17}, 14000)
	b := bytes.Repeat([]byte{0x39}, 21000)
	dir := fixtureFiles(t, map[string][]byte{"a[1],{x}.mkv": a, "中文/sub.srt": b, "skip.txt": []byte("skip")})
	p, err := Scan(context.Background(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "test.torrent")
	res, err := Create(context.Background(), dir, dst, Options{Selection: &Selection{p.Snapshot, []string{"中文/sub.srt", "a[1],{x}.mkv"}}, PieceSizeExp: 14, Private: true, Source: "source", Announce: "https://tracker.invalid/announce", Comment: "comment", CreatedBy: "fixture"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	mi, err := metainfo.LoadFromFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Files) != 2 || strings.Join(info.Files[0].Path, "/") != "a[1],{x}.mkv" || strings.Join(info.Files[1].Path, "/") != "中文/sub.srt" {
		t.Fatal(info.Files)
	}
	if info.Source != "source" || info.Private == nil || !*info.Private || mi.Comment != "comment" || mi.Announce != "https://tracker.invalid/announce" {
		t.Fatal("metadata lost")
	}
	concatenated := append(append([]byte{}, a...), b...)
	var expected []byte
	for len(concatenated) > 0 {
		n := 1 << 14
		if len(concatenated) < n {
			n = len(concatenated)
		}
		sum := sha1.Sum(concatenated[:n])
		expected = append(expected, sum[:]...)
		concatenated = concatenated[n:]
	}
	if !bytes.Equal(info.Pieces, expected) || res.TotalBytes != 35000 || res.Pieces != 3 {
		t.Fatalf("wrong cross-file hashes: %+v", res)
	}
	if res.InfoHash != mi.HashInfoBytes().String() {
		t.Fatal("wrong infohash")
	}
}
func TestSelectionChangesCancelAndNoPublishedFailure(t *testing.T) {
	dir := fixtureFiles(t, map[string][]byte{"a.bin": bytes.Repeat([]byte{1}, 40000), "skip.txt": []byte("old")})
	p, err := Scan(context.Background(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	opt := Options{Selection: &Selection{p.Snapshot, []string{"a.bin"}}, PieceSizeExp: 14}
	dst := filepath.Join(t.TempDir(), "out.torrent")
	old := []byte("old output")
	if err = os.WriteFile(dst, old, 0644); err != nil {
		t.Fatal(err)
	}
	changed := false
	_, err = Create(context.Background(), dir, dst, opt, func(float64, string) {
		if !changed {
			changed = true
			if e := os.WriteFile(filepath.Join(dir, "skip.txt"), []byte("changed unselected"), 0644); e != nil {
				t.Error(e)
			}
		}
	})
	if err == nil {
		t.Fatal("unselected mutation accepted")
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, old) {
		t.Fatal("failed task changed existing output")
	}
	p, err = Scan(context.Background(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	opt.Selection.Snapshot = p.Snapshot
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Create(ctx, dir, dst, opt, nil)
	if err == nil {
		t.Fatal("pre-cancel accepted")
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	_, err = Create(ctx, dir, dst, opt, func(float64, string) { cancel() })
	if err == nil {
		t.Fatal("mid-hash cancel accepted")
	}
	temp, _ := filepath.Glob(filepath.Join(filepath.Dir(dst), ".seedkit-torrent-*.tmp"))
	if len(temp) > 0 {
		t.Fatal("temp leak", temp)
	}
}
func TestSelectedZeroLengthAndOutputInsideSource(t *testing.T) {
	dir := fixtureFiles(t, map[string][]byte{"empty.txt": {}})
	p, err := Scan(context.Background(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Create(context.Background(), dir, filepath.Join(dir, "out.torrent"), Options{Selection: &Selection{p.Snapshot, []string{"empty.txt"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	single := filepath.Join(dir, "empty.txt")
	p, err = Scan(context.Background(), single, nil)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "empty.torrent")
	_, err = Create(context.Background(), single, dst, Options{Selection: &Selection{p.Snapshot, []string{"empty.txt"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	mi, err := metainfo.LoadFromFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err = bencode.Unmarshal(mi.InfoBytes, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["length"]; !ok {
		t.Fatal("missing required zero length")
	}
	if _, ok := raw["pieces"]; !ok {
		t.Fatal("missing required empty pieces")
	}
}
func TestSnapshotRejectionsAndExactCaseOnSensitiveFS(t *testing.T) {
	for _, rules := range [][]string{{"["}, {".."}, {"a/../b"}, {"C:\\root"}, {"/abs"}, {"!keep"}} {
		if ValidateRules(rules) == nil {
			t.Fatal("invalid rules accepted", rules)
		}
	}
	dir := fixtureFiles(t, map[string][]byte{"A.txt": []byte("A")})
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Skip("case-insensitive filesystem")
	}
	p, err := Scan(context.Background(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "case.torrent")
	_, err = Create(context.Background(), dir, dst, Options{Selection: &Selection{p.Snapshot, []string{"A.txt"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	mi, _ := metainfo.LoadFromFile(dst)
	info, _ := mi.UnmarshalInfo()
	if len(info.Files) != 1 || !reflect.DeepEqual(info.Files[0].Path, []string{"A.txt"}) {
		t.Fatal(info.Files)
	}
}
