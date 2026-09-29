// Command omdi runs the Open Media Downloader service.
package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/api"
	"github.com/miguins/open-media-downloader-ios/internal/app"
	"github.com/miguins/open-media-downloader-ios/internal/auth"
	"github.com/miguins/open-media-downloader-ios/internal/cleanup"
	"github.com/miguins/open-media-downloader-ios/internal/config"
	"github.com/miguins/open-media-downloader-ios/internal/logging"
	"github.com/miguins/open-media-downloader-ios/internal/readiness"
	"github.com/miguins/open-media-downloader-ios/internal/storage"
	"github.com/miguins/open-media-downloader-ios/internal/store"
	"github.com/miguins/open-media-downloader-ios/internal/urlpolicy"
	"github.com/miguins/open-media-downloader-ios/internal/worker"
)

const usage = "usage: omdi serve | omdi keys create --name <name> | omdi keys list | omdi keys revoke <id> | omdi keys purge --yes | " +
	"omdi jobs list [--owner <key-id>] [--status <status>] | omdi jobs delete <id> | omdi jobs purge --yes [--owner <key-id>]"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr, os.LookupEnv)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, lookup func(string) (string, bool)) int {
	// The level is lowered or raised once configuration loads; each invocation has one trace.
	level := new(slog.LevelVar)
	logger := logging.New(stderr, level)
	ctx = logging.NewTrace(ctx)
	switch {
	case len(args) == 1 && args[0] == "serve":
		return withStore(ctx, logger, level, lookup, func(cfg config.Config, st *store.Store) int {
			return serve(ctx, cfg, st, logger)
		})
	case len(args) >= 2 && args[0] == "keys":
		command, ok := parseKeysCommand(args[1:])
		if !ok {
			logger.ErrorContext(ctx, usage)

			return 2
		}

		return withStore(ctx, logger, level, lookup, func(cfg config.Config, st *store.Store) int {
			return command.run(ctx, cfg, st, stdout, logger)
		})
	case len(args) >= 2 && args[0] == "jobs":
		command, ok := parseJobsCommand(args[1:])
		if !ok {
			logger.ErrorContext(ctx, usage)

			return 2
		}

		return withStore(ctx, logger, level, lookup, func(cfg config.Config, st *store.Store) int {
			return command.run(ctx, cfg, st, stdout, logger)
		})
	default:
		logger.ErrorContext(ctx, usage)

		return 2
	}
}

// withStore loads configuration, applies its log level, opens the store, and runs fn with both.
func withStore(
	ctx context.Context,
	logger *slog.Logger,
	level *slog.LevelVar,
	lookup func(string) (string, bool),
	fn func(config.Config, *store.Store) int,
) int {
	cfg, err := config.Load(lookup)
	if err != nil {
		logger.ErrorContext(ctx, "invalid configuration", "error", err)

		return 1
	}
	level.Set(cfg.LogLevel)
	st, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		logger.ErrorContext(ctx, "open store", "error", err)

		return 1
	}
	defer func() {
		if err := st.Close(); err != nil {
			logger.ErrorContext(ctx, "close store", "error", err)
		}
	}()

	return fn(cfg, st)
}

func serve(ctx context.Context, cfg config.Config, st *store.Store, logger *slog.Logger) int {
	if cfg.RestoreAPIKeyOnStartup {
		record, created, err := auth.RestoreAPIKey(ctx, st, cfg.APIKey, time.Now().UTC())
		if err != nil {
			logger.ErrorContext(ctx, "restore API key", "error", err)
			return 1
		}
		message, level := "API key already present", slog.LevelInfo
		if created {
			message = "API key restored"
		}
		revoked := !record.RevokedAt.IsZero()
		if revoked {
			level = slog.LevelWarn
		}
		logger.Log(ctx, level, message, "key_id", record.ID, "key_name", record.Name, "revoked", revoked)
	}
	layout, err := storage.New(cfg.DataDir)
	if err != nil {
		logger.ErrorContext(ctx, "prepare storage", "error", err)

		return 1
	}
	policy, err := urlpolicy.New(cfg.AllowedPlatforms, cfg.MaxURLLength)
	if err != nil {
		logger.ErrorContext(ctx, "configure URL policy", "error", err)

		return 1
	}
	cleaner := cleanup.New(st, layout, logger)
	if err := cleaner.Recover(ctx); err != nil {
		logger.ErrorContext(ctx, "recover state", "error", err)

		return 1
	}
	components, err := newExtractorComponents(cfg)
	if err != nil {
		logger.ErrorContext(ctx, "configure extractors")
		return 1
	}
	jobWorker := worker.New(st, layout, components.extractor, worker.Settings{
		JobTimeout:   cfg.JobTimeout,
		MaxJobBytes:  cfg.MaxJobBytes,
		MinFreeBytes: cfg.MinFreeBytes,
		MaxJobItems:  cfg.MaxJobItems,
	}, logger)
	checker := readiness.New(
		readiness.Database(st),
		readiness.Storage(cfg.DataDir, cfg.MinFreeBytes),
		readiness.Executable("yt-dlp", cfg.Tools.YTDLP),
		readiness.Executable("gallery-dl", cfg.Tools.GalleryDL),
		readiness.Executable("ffmpeg", cfg.Tools.FFmpeg),
		readiness.Executable("ffprobe", cfg.Tools.FFprobe),
	)
	router := api.NewRouter(api.Dependencies{
		Readiness:     checker,
		Authenticator: auth.NewAuthenticator(st, logger),
		Store:         st,
		Layout:        layout,
		Policy:        policy,
		Notify:        jobWorker.Notify,
		Settings: api.Settings{
			PublicURL:       cfg.PublicURL,
			MaxRequestBytes: cfg.MaxRequestBytes,
			MaxQueuedJobs:   cfg.MaxQueuedJobs,
			JobRetention:    cfg.JobRetention,
			TokenTTL:        cfg.TokenTTL,
		},
		Logger: logger,
	})
	logger.InfoContext(ctx, "starting server",
		"http_addr", cfg.HTTPAddr,
		"public_url", cfg.PublicURL,
		"allowed_platforms", cfg.AllowedPlatforms,
		"log_level", cfg.LogLevel.String(),
		"job_timeout", cfg.JobTimeout.String(),
		"job_retention", cfg.JobRetention.String(),
		"token_ttl", cfg.TokenTTL.String(),
		"max_queued_jobs", cfg.MaxQueuedJobs,
		"max_job_items", cfg.MaxJobItems,
		"max_job_bytes", cfg.MaxJobBytes,
		"min_free_bytes", cfg.MinFreeBytes)
	if err := app.Run(ctx, cfg.HTTPAddr, router, logger, components.run, jobWorker.Run, cleaner.Run); err != nil {
		logger.ErrorContext(ctx, "application stopped unexpectedly", "error", err)

		return 1
	}

	return 0
}
