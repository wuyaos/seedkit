package config

import (
	"slices"
	"testing"
)

func TestTorrentExcludeRoundtrip(t *testing.T) {
	dir := t.TempDir()
	c := Default(dir)
	if len(c.Torrent.Exclude) != 0 {
		t.Fatal("new installs must not exclude user content")
	}
	c.Torrent.Exclude = []string{"*.log", "**/Sample/**", "字幕/临时*.srt"}
	if err := Save(dir, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Torrent.Exclude, c.Torrent.Exclude) {
		t.Fatalf("rules changed: %+v", got.Torrent.Exclude)
	}
	got.Torrent.Exclude = []string{}
	if err = Save(dir, got); err != nil {
		t.Fatal(err)
	}
	got, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Torrent.Exclude) != 0 {
		t.Fatal("empty rules resurrected old values")
	}
}
