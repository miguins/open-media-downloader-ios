package extractor

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/miguins/open-media-downloader-ios/internal/job"
	"github.com/miguins/open-media-downloader-ios/internal/urlpolicy"
)

const (
	maxDiagnosticLines  = 5
	maxDiagnosticLength = 240
)

// Failure explains an ErrExtractionFailed with fixed values. Diagnostics holds sanitized tool error
// lines for debug logs only; it never reaches clients and is not part of Error.
type Failure struct {
	Detail      job.ErrorDetail
	Tool        string
	ExitCode    int
	Signal      string
	Egress      urlpolicy.EgressStats
	Diagnostics []string
}

func (f *Failure) Error() string { return ErrExtractionFailed.Error() + ": " + string(f.Detail) }

func (f *Failure) Unwrap() error { return ErrExtractionFailed }

// toolFailure maps a tool invocation error to a Failure. Extractors classify their error output;
// media tools always report fallback. A SIGKILL the runner did not send comes from the kernel's
// out-of-memory killer, because the runner's own terminations surface as context or limit errors.
func toolFailure(ctx context.Context, tool string, fallback job.ErrorDetail, result Result, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, ErrOutputLimit):
		return &Failure{Detail: fallback, Tool: tool, ExitCode: result.ExitCode}
	case !errors.Is(err, ErrCommandExit):
		return err
	}
	detail := fallback
	switch {
	case result.Signal == "SIGKILL":
		detail = job.DetailOutOfMemory
	case fallback == job.DetailToolError:
		detail = classifyStderr(result.Stderr)
	}

	return &Failure{
		Detail: detail, Tool: tool, ExitCode: result.ExitCode, Signal: result.Signal,
		Diagnostics: sanitizeDiagnostics(result.Stderr),
	}
}

// explainFailure completes an extraction failure with the egress session's counts. A failure without
// a specific reason is attributed to the egress policy or the network when the proxy observed one.
// Other errors are returned unchanged.
func explainFailure(err error, stats urlpolicy.EgressStats) error {
	if !errors.Is(err, ErrExtractionFailed) {
		return err
	}
	var failure *Failure
	if !errors.As(err, &failure) {
		failure = &Failure{Detail: job.DetailInvalidOutput}
	}
	failure.Egress = stats
	if failure.Detail == job.DetailToolError || failure.Detail == job.DetailNetworkError {
		switch {
		case stats.Rejected > 0:
			failure.Detail = job.DetailEgressDenied
		case stats.UpstreamFailures > 0:
			failure.Detail = job.DetailNetworkError
		}
	}

	return failure
}

// stderrPatterns are checked in order against lowercased error lines; earlier entries win when a
// message matches several, such as a bot check that also asks the user to sign in.
var stderrPatterns = []struct {
	detail   job.ErrorDetail
	patterns []string
}{
	{job.DetailAgeRestricted, []string{"confirm your age", "age-restricted", "age restricted", "inappropriate for some users"}},
	{job.DetailBlocked, []string{"not a bot", "http error 403", "403 forbidden", "403: forbidden"}},
	{job.DetailGeoRestricted, []string{"in your country", "geo restrict", "geo-restrict", "from your location"}},
	{job.DetailUnavailable, []string{
		"private video", "video unavailable", "has been removed", "been deleted", "does not exist", "no longer available",
		"http error 404", "404 not found", "404: not found",
	}},
	{job.DetailLoginRequired, []string{"login required", "log in", "sign in", "--cookies", "authorizationerror", "requires authentication"}},
	{job.DetailRateLimited, []string{"http error 429", "too many requests", "rate-limit", "rate limit"}},
	{job.DetailNoMedia, []string{"no video formats", "requested format is not available", "no media", "no video could be found"}},
	{job.DetailNetworkError, []string{
		"timed out", "connection refused", "connection reset", "remote end closed", "name resolution",
		"name or service not known", "network is unreachable", "unable to download webpage", "bad gateway",
	}},
}

// classifyStderr derives a fixed reason from a tool's error lines. The text itself is never kept.
func classifyStderr(stderr []byte) job.ErrorDetail {
	lines := errorLines(stderr)
	for _, entry := range stderrPatterns {
		for _, line := range lines {
			lower := strings.ToLower(line)
			for _, pattern := range entry.patterns {
				if strings.Contains(lower, pattern) {
					return entry.detail
				}
			}
		}
	}

	return job.DetailToolError
}

func errorLines(stderr []byte) []string {
	var lines []string
	for line := range bytes.Lines(stderr) {
		text := strings.TrimSpace(string(line))
		if strings.Contains(strings.ToLower(text), "error") {
			lines = append(lines, text)
		}
	}

	return lines
}

var (
	diagnosticPostID = regexp.MustCompile(`^((?:ERROR:\s*)?\[[^\]]+\]) [^\s:]+: `)
	diagnosticURL    = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s'"<>]+`)
	diagnosticPath   = regexp.MustCompile(`(^|[\s'"(=])/[^\s'"]+`)
)

// sanitizeDiagnostics returns a few bounded error lines with post IDs, URLs, paths, and
// non-printable characters removed, for debug logs only.
func sanitizeDiagnostics(stderr []byte) []string {
	var lines []string
	for _, line := range errorLines(stderr) {
		if len(lines) == maxDiagnosticLines {
			break
		}
		line = strings.Map(func(r rune) rune {
			if r < ' ' || r > '~' {
				return '?'
			}
			return r
		}, line)
		line = diagnosticPostID.ReplaceAllString(line, "$1 ")
		line = diagnosticURL.ReplaceAllString(line, "<url>")
		line = diagnosticPath.ReplaceAllString(line, "${1}<path>")
		if len(line) > maxDiagnosticLength {
			line = line[:maxDiagnosticLength]
		}
		lines = append(lines, line)
	}

	return lines
}
