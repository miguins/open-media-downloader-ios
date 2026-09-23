// Command omdi runs the Open Media Downloader service.
package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/miguins/open-media-downloader-ios/internal/api"
	"github.com/miguins/open-media-downloader-ios/internal/app"
	"github.com/miguins/open-media-downloader-ios/internal/config"
	"github.com/miguins/open-media-downloader-ios/internal/readiness"
	"github.com/miguins/open-media-downloader-ios/internal/store"
)

const usage = "usage: omdi serve | omdi keys create --name <name> | omdi keys list | omdi keys revoke <id>"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr, os.LookupEnv)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, lookup func(string) (string, bool)) int {
	logger := slog.New(slog.NewJSONHandler(stderr, nil))
	switch {
	case len(args) == 1 && args[0] == "serve":
		return withStore(ctx, logger, lookup, func(cfg config.Config, st *store.Store) int {
			return serve(ctx, cfg, st, logger)
		})
	case len(args) >= 2 && args[0] == "keys":
		command, ok := parseKeysCommand(args[1:])
		if !ok {
			logger.Error(usage)

			return 2
		}

		return withStore(ctx, logger, lookup, func(_ config.Config, st *store.Store) int {
			return command.run(ctx, st, stdout, logger)
		})
	default:
		logger.Error(usage)

		return 2
	}
}

// withStore loads configuration, opens the store, and runs fn with both.
func withStore(ctx context.Context, logger *slog.Logger, lookup func(string) (string, bool), fn func(config.Config, *store.Store) int) int {
	cfg, err := config.Load(lookup)
	if err != nil {
		logger.Error("invalid configuration", "error", err)

		return 1
	}
	st, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		logger.Error("open store", "error", err)

		return 1
	}
	defer func() {
		if err := st.Close(); err != nil {
			logger.Error("close store", "error", err)
		}
	}()

	return fn(cfg, st)
}

func serve(ctx context.Context, cfg config.Config, st *store.Store, logger *slog.Logger) int {
	checker := readiness.New(
		readiness.Database(st),
		readiness.Storage(cfg.DataDir, cfg.MinFreeBytes),
		readiness.Executable("yt-dlp", cfg.Tools.YTDLP),
		readiness.Executable("gallery-dl", cfg.Tools.GalleryDL),
		readiness.Executable("ffmpeg", cfg.Tools.FFmpeg),
		readiness.Executable("ffprobe", cfg.Tools.FFprobe),
	)
	if err := app.Run(ctx, cfg.HTTPAddr, api.NewRouter(checker, logger), logger); err != nil {
		logger.Error("application stopped unexpectedly", "error", err)

		return 1
	}

	return 0
}
