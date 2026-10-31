package config

import (
	"os"
	"testing"
)

func TestScreenshotCompressionDefaultOff(t *testing.T) {
	t.Setenv("SEEDKIT_SCREENSHOT_COMPRESS", "")
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Screenshot.Compress {
		t.Fatal("fresh config must default to uncompressed PNG")
	}
	cfg, err = Load(dir)
	if err != nil || cfg.Screenshot.Compress {
		t.Fatalf("persisted default: cfg=%+v err=%v", cfg, err)
	}
}

func TestScreenshotCompressionExplicitChoicePreserved(t *testing.T) {
	t.Setenv("SEEDKIT_SCREENSHOT_COMPRESS", "")
	dir := t.TempDir()
	cfg := Default(dir)
	cfg.Screenshot.Compress = true
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Screenshot.Compress {
		t.Fatal("loading a config must not override an explicit compression choice")
	}
}

func TestScreenshotCompressionMissingUsesDefault(t *testing.T) {
	t.Setenv("SEEDKIT_SCREENSHOT_COMPRESS", "")
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte("screenshot:\n  count: 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Screenshot.Compress {
		t.Fatal("omitted compression must use the new false default")
	}
}
