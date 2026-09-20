// Command omdi runs the Open Media Downloader service.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/miguins/open-media-downloader-ios/internal/api"
	"github.com/miguins/open-media-downloader-ios/internal/app"
	"github.com/miguins/open-media-downloader-ios/internal/config"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if len(args) != 1 || args[0] != "serve" {
		logger.Error("usage: omdi serve")

		return 2
	}

	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		logger.Error("invalid configuration", "error", err)

		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, cfg.HTTPAddr, api.NewRouter(), logger); err != nil {
		logger.Error("application stopped unexpectedly", "error", err)

		return 1
	}

	return 0
}
