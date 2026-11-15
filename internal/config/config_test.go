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

func TestUploadDefaultsAndCredentials(t *testing.T) {
	t.Setenv("SEEDKIT_UPLOAD_IMGBB_API_KEY", "env-imgbb")
	t.Setenv("SEEDKIT_UPLOAD_FREEIMAGE_API_KEY", "")
	t.Setenv("SEEDKIT_UPLOAD_AGSV_TOKEN", "")
	t.Setenv("SEEDKIT_UPLOAD_PTERCLUB_COOKIE", "")
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Upload.Concurrency != 3 || cfg.Upload.Pixhost.Domain != "to" {
		t.Fatalf("upload defaults: %+v", cfg.Upload)
	}
	if cfg.Upload.Imgbb.APIKey != "env-imgbb" {
		t.Fatalf("imgbb env override not applied: %+v", cfg.Upload.Imgbb)
	}
}

func TestUploadLegacyConfigGetsNewDefaults(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte("upload:\n  default: pixhost\n  pixhost:\n    max_th: 250\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Upload.Concurrency != 3 || cfg.Upload.Pixhost.Domain != "to" || cfg.Upload.Pixhost.MaxTh != 250 {
		t.Fatalf("legacy settings migration: %+v", cfg.Upload)
	}
}
