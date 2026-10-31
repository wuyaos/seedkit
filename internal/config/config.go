package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the on-disk configuration. Every field has an env override
// SEEDKIT_<SECTION>_<FIELD> (upper snake), e.g. SEEDKIT_SERVER_LISTEN.
type Config struct {
	Server     Server     `yaml:"server" json:"server"`
	Auth       Auth       `yaml:"auth" json:"auth"`
	Paths      Paths      `yaml:"paths" json:"paths"`
	Tasks      Tasks      `yaml:"tasks" json:"tasks"`
	Tools      Tools      `yaml:"tools" json:"tools"`
	Screenshot Screenshot `yaml:"screenshot" json:"screenshot"`
	Upload     Upload     `yaml:"upload" json:"upload"`
	Torrent    Torrent    `yaml:"torrent" json:"torrent"`
}

type Server struct {
	Listen string `yaml:"listen" json:"listen"` // ":8080"
}

type Auth struct {
	Username     string `yaml:"username" json:"username"`
	PasswordHash string `yaml:"password_hash" json:"password_hash"` // bcrypt; empty => setup required
	SessionDays  int    `yaml:"session_days" json:"session_days"`
}

type Paths struct {
	// Roots are the only directories exposed to the file manager. Keys are
	// display names, values are absolute paths.
	Roots map[string]string `yaml:"roots" json:"roots"`
	// Output holds screenshots, reports and torrents. Inside DataDir by default.
	Output string `yaml:"output" json:"output"`
}

type Tasks struct {
	HeavyConcurrency int `yaml:"heavy_concurrency" json:"heavy_concurrency"` // BDInfo/screenshot/torrent
	HistoryKeep      int `yaml:"history_keep" json:"history_keep"`
}

type Tools struct {
	FFmpeg  string `yaml:"ffmpeg" json:"ffmpeg"`
	FFprobe string `yaml:"ffprobe" json:"ffprobe"`
}

type Screenshot struct {
	Count       int    `yaml:"count" json:"count"`
	Subtitles   bool   `yaml:"subtitles" json:"subtitles"`
	Compress    bool   `yaml:"compress" json:"compress"`
	SkipPercent int    `yaml:"skip_percent" json:"skip_percent"` // skip head/tail %
	Tonemap     string `yaml:"tonemap" json:"tonemap"`           // hable|mobius|reinhard|bt2390
}

type Upload struct {
	Default string            `yaml:"default" json:"default"`
	Picgo   PicgoUpload       `yaml:"picgo" json:"picgo"`
	Pixhost PixhostUpload     `yaml:"pixhost" json:"pixhost"`
	Extra   map[string]string `yaml:"extra,omitempty" json:"extra,omitempty"`
}

type PicgoUpload struct {
	Endpoint string `yaml:"endpoint" json:"endpoint"` // https://www.picgo.net/api/1/upload
	APIKey   string `yaml:"api_key" json:"api_key"`
	Album    string `yaml:"album,omitempty" json:"album,omitempty"`
}

type PixhostUpload struct {
	ContentType int `yaml:"content_type" json:"content_type"` // 0 safe, 1 adult
	MaxTh       int `yaml:"max_th" json:"max_th"`
}

type Torrent struct {
	Announce  string `yaml:"announce" json:"announce"`
	Private   bool   `yaml:"private" json:"private"`
	Source    string `yaml:"source" json:"source"`
	PieceSize int    `yaml:"piece_size" json:"piece_size"` // 0 = auto; else log2 bytes (e.g. 24 = 16MiB)
	Comment   string `yaml:"comment" json:"comment"`
}

// Default returns a fresh config with sane defaults for a 2 GB seedbox.
func Default(dataDir string) *Config {
	roots := map[string]string{}
	if runtime.GOOS == "windows" {
		roots["data"] = filepath.Join(dataDir, "media")
	} else {
		roots["media"] = "/media"
	}
	return &Config{
		Server:     Server{Listen: ":8080"},
		Auth:       Auth{SessionDays: 30},
		Paths:      Paths{Roots: roots, Output: filepath.Join(dataDir, "output")},
		Tasks:      Tasks{HeavyConcurrency: 2, HistoryKeep: 200},
		Screenshot: Screenshot{Count: 5, Subtitles: true, Compress: false, SkipPercent: 5, Tonemap: "hable"},
		Upload: Upload{
			Default: "picgo",
			Picgo:   PicgoUpload{Endpoint: "https://www.picgo.net/api/1/upload"},
			Pixhost: PixhostUpload{ContentType: 0, MaxTh: 350},
		},
		Torrent: Torrent{Private: true},
	}
}

// DataDir resolves the data directory: $SEEDKIT_DATA, else ./data next to cwd.
func DataDir() string {
	if v := os.Getenv("SEEDKIT_DATA"); v != "" {
		return v
	}
	if runtime.GOOS != "windows" {
		if _, err := os.Stat("/data"); err == nil {
			return "/data"
		}
	}
	wd, _ := os.Getwd()
	return filepath.Join(wd, "data")
}

func Path(dataDir string) string { return filepath.Join(dataDir, "config.yaml") }

// Load reads config.yaml from dataDir, creating it with defaults if absent,
// then applies env overrides. It never fails on a missing file.
func Load(dataDir string) (*Config, error) {
	cfg := Default(dataDir)
	p := Path(dataDir)
	b, err := os.ReadFile(p)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(b, cfg); err != nil {
			return nil, fmt.Errorf("parse %s: %w", p, err)
		}
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			return nil, err
		}
		if err := Save(dataDir, cfg); err != nil {
			return nil, err
		}
	default:
		return nil, err
	}
	applyEnv(cfg)
	if cfg.Paths.Output == "" {
		cfg.Paths.Output = filepath.Join(dataDir, "output")
	}
	if cfg.Tasks.HeavyConcurrency < 1 {
		cfg.Tasks.HeavyConcurrency = 1
	}
	return cfg, nil
}

func Save(dataDir string, cfg *Config) error {
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	tmp := Path(dataDir) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, Path(dataDir))
}

func applyEnv(c *Config) {
	str := func(key string, dst *string) {
		if v, ok := os.LookupEnv(key); ok {
			*dst = v
		}
	}
	num := func(key string, dst *int) {
		if v, ok := os.LookupEnv(key); ok {
			if n, err := strconv.Atoi(v); err == nil {
				*dst = n
			}
		}
	}
	str("SEEDKIT_SERVER_LISTEN", &c.Server.Listen)
	str("SEEDKIT_AUTH_USERNAME", &c.Auth.Username)
	str("SEEDKIT_AUTH_PASSWORD_HASH", &c.Auth.PasswordHash)
	str("SEEDKIT_PATHS_OUTPUT", &c.Paths.Output)
	num("SEEDKIT_TASKS_HEAVY_CONCURRENCY", &c.Tasks.HeavyConcurrency)
	str("SEEDKIT_TOOLS_FFMPEG", &c.Tools.FFmpeg)
	str("SEEDKIT_TOOLS_FFPROBE", &c.Tools.FFprobe)
	str("SEEDKIT_UPLOAD_PICGO_API_KEY", &c.Upload.Picgo.APIKey)
	str("SEEDKIT_TORRENT_ANNOUNCE", &c.Torrent.Announce)
	// SEEDKIT_PATHS_ROOTS="media=/media,downloads=/home/x/dl"
	if v, ok := os.LookupEnv("SEEDKIT_PATHS_ROOTS"); ok && v != "" {
		roots := map[string]string{}
		for _, kv := range strings.Split(v, ",") {
			name, path, found := strings.Cut(kv, "=")
			if !found {
				path, name = kv, filepath.Base(kv)
			}
			roots[strings.TrimSpace(name)] = strings.TrimSpace(path)
		}
		c.Paths.Roots = roots
	}
}
