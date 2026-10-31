package main

import (
	"bytes"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/wuyaos/seedkit/internal/auth"
	"github.com/wuyaos/seedkit/internal/config"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

const storedConfig = `# keep comment
server:
  listen: ':8080'
auth:
  username: owner
  password_hash: old-hash
  session_days: 7
paths:
  roots:
    media: /media
upload:
  picgo:
    api_key: keep-this-key
future_setting: keep-me
`

func passwordReader(values ...string) func(string) ([]byte, error) {
	return func(string) ([]byte, error) {
		if len(values) == 0 {
			return nil, io.EOF
		}
		v := []byte(values[0])
		values = values[1:]
		return v, nil
	}
}

func TestResetPassword(t *testing.T) {
	// Environment overrides must not leak into the saved YAML.
	t.Setenv("SEEDKIT_SERVER_LISTEN", ":9999")
	t.Setenv("SEEDKIT_UPLOAD_PICGO_API_KEY", "override-key")
	dir := t.TempDir()
	path := config.Path(dir)
	if err := os.WriteFile(path, []byte(storedConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := resetPassword(dir, passwordReader("new-secret-123", "new-secret-123"), &output); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg config.Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(cfg.Auth.PasswordHash), []byte("new-secret-123")); err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.Username != "owner" || cfg.Auth.SessionDays != 7 || cfg.Server.Listen != ":8080" || cfg.Upload.Picgo.APIKey != "keep-this-key" || cfg.Paths.Roots["media"] != "/media" {
		t.Fatal("unrelated config changed")
	}
	if !bytes.Contains(b, []byte("future_setting: keep-me")) || !bytes.Contains(b, []byte("# keep comment")) {
		t.Fatal("unknown keys or comments erased")
	}
	if bytes.Contains(b, []byte("new-secret-123")) || strings.Contains(output.String(), "new-secret-123") {
		t.Fatal("plaintext password leaked")
	}
	m := auth.New(cfg.Auth.Username, cfg.Auth.PasswordHash, 0)
	if _, ok, _ := m.Login(httptest.NewRequest("POST", "/api/auth/login", nil), "owner", "new-secret-123"); !ok {
		t.Fatal("new password rejected by login")
	}
	if _, ok, _ := m.Login(httptest.NewRequest("POST", "/api/auth/login", nil), "owner", "old-password"); ok {
		t.Fatal("old password accepted")
	}
}

func TestResetPasswordFailuresLeaveFileUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		input []string
	}{
		{"mismatch", storedConfig, []string{"new-secret", "different"}},
		{"short", storedConfig, []string{"short"}},
		{"long", storedConfig, []string{strings.Repeat("x", 73)}},
		{"cancel", storedConfig, nil},
		{"cancel-confirm", storedConfig, []string{"new-secret"}},
		{"uninitialized", "auth: {}\n", nil},
		{"empty", "", nil},
		{"malformed", "auth: [", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := config.Path(dir)
			if err := os.WriteFile(path, []byte(tc.config), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := resetPassword(dir, passwordReader(tc.input...), io.Discard); err == nil {
				t.Fatal("expected error")
			}
			b, _ := os.ReadFile(path)
			if string(b) != tc.config {
				t.Fatal("config changed after failure")
			}
		})
	}
}

func TestResetPasswordRejectsEnvironmentHash(t *testing.T) {
	t.Setenv("SEEDKIT_AUTH_PASSWORD_HASH", "")
	called := false
	err := resetPassword(t.TempDir(), func(string) ([]byte, error) {
		called = true
		return nil, errors.New("must not read password")
	}, io.Discard)
	if err == nil || called || !strings.Contains(err.Error(), "SEEDKIT_AUTH_PASSWORD_HASH") {
		t.Fatal("environment hash override was not rejected")
	}
}

func TestPasswdRejectsNonTerminal(t *testing.T) {
	in, out, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	defer out.Close()
	if err := passwd(t.TempDir(), in, io.Discard); err == nil {
		t.Fatal("non-terminal accepted")
	}
}
