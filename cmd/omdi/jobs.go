package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"text/tabwriter"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/config"
	"github.com/miguins/open-media-downloader-ios/internal/id"
	"github.com/miguins/open-media-downloader-ios/internal/job"
	"github.com/miguins/open-media-downloader-ios/internal/storage"
	"github.com/miguins/open-media-downloader-ios/internal/store"
)

// jobsCommand is a parsed "omdi jobs" subcommand.
type jobsCommand struct {
	action string
	jobID  string
	filter store.JobFilter
}

func parseJobsCommand(args []string) (jobsCommand, bool) {
	switch args[0] {
	case "list":
		flags := newFlagSet("list")
		owner := flags.String("owner", "", "key ID")
		status := flags.String("status", "", "job status")
		if flags.Parse(args[1:]) != nil || flags.NArg() != 0 ||
			(*owner != "" && !id.Valid(*owner)) || (*status != "" && !job.Status(*status).Valid()) {
			return jobsCommand{}, false
		}

		return jobsCommand{action: "list", filter: store.JobFilter{OwnerID: *owner, Status: job.Status(*status)}}, true
	case "delete":
		if len(args) != 2 {
			return jobsCommand{}, false
		}

		return jobsCommand{action: "delete", jobID: args[1]}, true
	case "purge":
		flags := newFlagSet("purge")
		confirmed := flags.Bool("yes", false, "confirm")
		owner := flags.String("owner", "", "key ID")
		if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || !*confirmed || (*owner != "" && !id.Valid(*owner)) {
			return jobsCommand{}, false
		}

		return jobsCommand{action: "purge", filter: store.JobFilter{OwnerID: *owner}}, true
	default:
		return jobsCommand{}, false
	}
}

func newFlagSet(name string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	return flags
}

func (c jobsCommand) run(ctx context.Context, cfg config.Config, st *store.Store, stdout io.Writer, logger *slog.Logger) int {
	if c.action == "list" {
		return listJobs(ctx, st, c.filter, stdout, logger)
	}
	layout, err := storage.New(cfg.DataDir)
	if err != nil {
		logger.ErrorContext(ctx, "prepare storage", "error", err)

		return 1
	}
	if c.action == "delete" {
		return deleteJob(ctx, st, layout, c.jobID, logger)
	}

	return purgeJobs(ctx, st, layout, c.filter.OwnerID, stdout, logger)
}

// listJobs prints job metadata. Source URLs are omitted because they reveal what each key downloaded.
func listJobs(ctx context.Context, st *store.Store, filter store.JobFilter, stdout io.Writer, logger *slog.Logger) int {
	jobs, err := st.Jobs(ctx, filter)
	if err != nil {
		logger.ErrorContext(ctx, "list jobs", "error", err)

		return 1
	}
	table := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(table, "ID\tOWNER\tPLATFORM\tSTATUS\tCREATED\tEXPIRES")
	for _, j := range jobs {
		_, _ = fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\n",
			j.ID, j.OwnerID, j.Platform, j.Status, formatTime(j.CreatedAt), formatTime(j.ExpiresAt))
	}
	if err := table.Flush(); err != nil {
		logger.ErrorContext(ctx, "write job list", "error", err)

		return 1
	}

	return 0
}

// deleteJob removes a job that is not running. The row goes first; leftover files are
// removed by the server's orphan sweep if this process fails in between.
func deleteJob(ctx context.Context, st *store.Store, layout *storage.Layout, jobID string, logger *slog.Logger) int {
	if !id.Valid(jobID) {
		logger.ErrorContext(ctx, "job not found")

		return 1
	}
	err := st.DeleteJob(ctx, jobID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		logger.ErrorContext(ctx, "job not found")

		return 1
	case errors.Is(err, store.ErrConflict):
		logger.ErrorContext(ctx, "job is running; cancel it through the API or use omdi jobs purge")

		return 1
	case err != nil:
		logger.ErrorContext(ctx, "delete job", "error", err)

		return 1
	}
	if err := layout.RemoveJob(jobID); err != nil {
		logger.ErrorContext(ctx, "remove job files", "error", err)

		return 1
	}
	logger.InfoContext(ctx, "job deleted", "id", jobID)

	return 0
}

// purgeJobs cancels and removes every job in scope, including running ones. A worker running one
// of them observes the change within a second and discards its output.
func purgeJobs(ctx context.Context, st *store.Store, layout *storage.Layout, ownerID string, stdout io.Writer, logger *slog.Logger) int {
	ids, err := st.PurgeJobs(ctx, ownerID, time.Now().UTC())
	if err != nil {
		logger.ErrorContext(ctx, "purge jobs", "error", err)

		return 1
	}
	failed := 0
	for _, jobID := range ids {
		if err := layout.RemoveJob(jobID); err != nil {
			failed++
		}
	}
	_, _ = fmt.Fprintf(stdout, "purged %d jobs\n", len(ids))
	if failed > 0 {
		logger.ErrorContext(ctx, "some job files could not be removed; the server removes them on its next sweep", "count", failed)

		return 1
	}

	return 0
}
