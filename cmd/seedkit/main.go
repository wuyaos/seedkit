package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wuyaos/seedkit/internal/config"
	"github.com/wuyaos/seedkit/internal/server"
	"github.com/wuyaos/seedkit/internal/tools"
)

var version = "dev"

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	dataDir := config.DataDir()
	switch cmd {
	case "serve":
		if err := serve(dataDir, log); err != nil {
			log.Error("fatal", "err", err)
			os.Exit(1)
		}
	case "passwd":
		if len(os.Args) != 2 {
			fmt.Fprintln(os.Stderr, "usage: seedkit passwd (interactive; do not pass passwords as arguments)")
			os.Exit(2)
		}
		if err := passwd(dataDir, os.Stdin, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, "passwd:", err)
			os.Exit(1)
		}
	case "doctor":
		cfg, err := config.Load(dataDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		set := tools.Resolve(context.Background(), dataDir, cfg.Tools.FFmpeg, cfg.Tools.FFprobe)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{"version": version, "dataDir": dataDir, "config": config.Path(dataDir), "tools": set, "roots": cfg.Paths.Roots})
		if !set.ScreenshotReady() {
			os.Exit(2)
		}
	case "version", "-v", "--version":
		fmt.Println("seedkit", version)
	default:
		fmt.Fprintf(os.Stderr, "usage: seedkit [serve|doctor|passwd|version]\n  SEEDKIT_DATA=<dir> (default ./data or /data)\n")
		os.Exit(2)
	}
}

func serve(dataDir string, log *slog.Logger) error {
	cfg, err := config.Load(dataDir)
	if err != nil {
		return err
	}
	srv := server.New(cfg, dataDir, version, log)
	hs := &http.Server{
		Addr: cfg.Server.Listen, Handler: srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = hs.Shutdown(sctx)
	}()
	log.Info("seedkit listening", "addr", cfg.Server.Listen, "data", dataDir, "version", version)
	if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
