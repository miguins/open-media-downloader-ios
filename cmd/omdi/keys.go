package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"text/tabwriter"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/auth"
	"github.com/miguins/open-media-downloader-ios/internal/config"
	"github.com/miguins/open-media-downloader-ios/internal/id"
	"github.com/miguins/open-media-downloader-ios/internal/storage"
	"github.com/miguins/open-media-downloader-ios/internal/store"
)

// keysCommand is a parsed "omdi keys" subcommand.
type keysCommand struct {
	action string
	arg    string
}

func parseKeysCommand(args []string) (keysCommand, bool) {
	switch args[0] {
	case "create":
		flags := newFlagSet("create")
		name := flags.String("name", "", "key name")
		if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || !auth.ValidKeyName(*name) {
			return keysCommand{}, false
		}

		return keysCommand{action: "create", arg: *name}, true
	case "list":
		return keysCommand{action: "list"}, len(args) == 1
	case "purge":
		flags := newFlagSet("purge")
		confirmed := flags.Bool("yes", false, "confirm")
		if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || !*confirmed {
			return keysCommand{}, false
		}

		return keysCommand{action: "purge"}, true
	case "revoke":
		if len(args) != 2 {
			return keysCommand{}, false
		}

		return keysCommand{action: "revoke", arg: args[1]}, true
	default:
		return keysCommand{}, false
	}
}

func (c keysCommand) run(ctx context.Context, cfg config.Config, st *store.Store, stdout io.Writer, logger *slog.Logger) int {
	now := time.Now().UTC()
	switch c.action {
	case "purge":
		return purgeRevokedKeys(ctx, cfg, st, now, stdout, logger)
	case "create":
		plaintext, record := auth.Generate(c.arg, now)
		if err := st.CreateAPIKey(ctx, record); err != nil {
			if errors.Is(err, store.ErrConflict) {
				logger.ErrorContext(ctx, "API key name already exists")
			} else {
				logger.ErrorContext(ctx, "create API key", "error", err)
			}

			return 1
		}
		_, _ = fmt.Fprintln(stdout, plaintext)
		logger.InfoContext(ctx, "API key created; store it now because it cannot be shown again", "id", record.ID, "name", record.Name)

		return 0
	case "list":
		return listKeys(ctx, st, stdout, logger)
	default:
		if !id.Valid(c.arg) {
			logger.ErrorContext(ctx, "API key not found")

			return 1
		}
		if err := st.RevokeAPIKey(ctx, c.arg, now); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				logger.ErrorContext(ctx, "API key not found")
			} else {
				logger.ErrorContext(ctx, "revoke API key", "error", err)
			}

			return 1
		}
		logger.InfoContext(ctx, "API key revoked", "id", c.arg)

		return 0
	}
}

// purgeRevokedKeys deletes every revoked key with all of its jobs and job files. A worker running
// one of those jobs observes the change within a second and discards its output.
func purgeRevokedKeys(ctx context.Context, cfg config.Config, st *store.Store, now time.Time, stdout io.Writer, logger *slog.Logger) int {
	layout, err := storage.New(cfg.DataDir)
	if err != nil {
		logger.ErrorContext(ctx, "prepare storage", "error", err)

		return 1
	}
	keys, ids, err := st.PurgeRevokedAPIKeys(ctx, now)
	if err != nil {
		logger.ErrorContext(ctx, "purge revoked API keys", "error", err)

		return 1
	}
	failed := 0
	for _, jobID := range ids {
		if err := layout.RemoveJob(jobID); err != nil {
			failed++
		}
	}
	_, _ = fmt.Fprintf(stdout, "purged %d revoked keys and %d jobs\n", keys, len(ids))
	if failed > 0 {
		logger.ErrorContext(ctx, "some job files could not be removed; the server removes them on its next sweep", "count", failed)

		return 1
	}

	return 0
}

func listKeys(ctx context.Context, st *store.Store, stdout io.Writer, logger *slog.Logger) int {
	keys, err := st.APIKeys(ctx)
	if err != nil {
		logger.ErrorContext(ctx, "list API keys", "error", err)

		return 1
	}
	table := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(table, "ID\tNAME\tCREATED\tLAST USED\tREVOKED")
	for _, key := range keys {
		_, _ = fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n",
			key.ID, key.Name, formatTime(key.CreatedAt), formatTime(key.LastUsedAt), formatTime(key.RevokedAt))
	}
	if err := table.Flush(); err != nil {
		logger.ErrorContext(ctx, "write API key list", "error", err)

		return 1
	}

	return 0
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}

	return t.UTC().Format(time.RFC3339)
}
