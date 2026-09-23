// Package config loads and validates application configuration.
package config

import (
	"errors"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/urlpolicy"
)

const (
	mebibyte = int64(1) << 20
	gibibyte = int64(1) << 30
	tebibyte = int64(1) << 40
)

// Tools contains absolute paths to external media tools.
type Tools struct {
	YTDLP     string
	GalleryDL string
	FFmpeg    string
	FFprobe   string
}

// Config contains validated application configuration.
type Config struct {
	HTTPAddr         string
	DataDir          string
	AllowedPlatforms []string
	MaxURLLength     int
	MaxRequestBytes  int64
	JobTimeout       time.Duration
	MaxJobBytes      int64
	MinFreeBytes     int64
	JobRetention     time.Duration
	TokenTTL         time.Duration
	PublicURL        string
	MaxQueuedJobs    int
	Tools            Tools
}

// loader reads variables and accumulates validation errors.
type loader struct {
	lookup func(string) (string, bool)
	errs   []error
}

// Load reads and validates application configuration through lookup.
// Errors name the offending variable and rule but never the supplied value.
func Load(lookup func(string) (string, bool)) (Config, error) {
	l := &loader{lookup: lookup}
	cfg := Config{
		HTTPAddr:         l.httpAddr("OMDI_HTTP_ADDR", ":8080"),
		DataDir:          l.path("OMDI_DATA_DIR", "/data"),
		AllowedPlatforms: l.platforms("OMDI_ALLOWED_PLATFORMS"),
		MaxURLLength:     int(l.integer("OMDI_MAX_URL_LENGTH", 2048, 256, 8192)),
		MaxRequestBytes:  l.integer("OMDI_MAX_REQUEST_BYTES", 16384, 1024, mebibyte),
		JobTimeout:       l.duration("OMDI_JOB_TIMEOUT", 10*time.Minute, 30*time.Second, 2*time.Hour),
		MaxJobBytes:      l.integer("OMDI_MAX_JOB_BYTES", 2*gibibyte, mebibyte, 100*gibibyte),
		MinFreeBytes:     l.integer("OMDI_MIN_FREE_BYTES", gibibyte, 0, tebibyte),
		JobRetention:     l.duration("OMDI_JOB_RETENTION", 24*time.Hour, 5*time.Minute, 720*time.Hour),
		TokenTTL:         l.duration("OMDI_TOKEN_TTL", 15*time.Minute, time.Minute, 24*time.Hour),
		PublicURL:        l.publicURL("OMDI_PUBLIC_URL", "http://localhost:8080"),
		MaxQueuedJobs:    int(l.integer("OMDI_MAX_QUEUED_JOBS", 10, 1, 1000)),
		Tools: Tools{
			YTDLP:     l.path("OMDI_YTDLP_PATH", "/opt/media-tools/bin/yt-dlp"),
			GalleryDL: l.path("OMDI_GALLERYDL_PATH", "/opt/media-tools/bin/gallery-dl"),
			FFmpeg:    l.path("OMDI_FFMPEG_PATH", "/opt/ffmpeg/bin/ffmpeg"),
			FFprobe:   l.path("OMDI_FFPROBE_PATH", "/opt/ffmpeg/bin/ffprobe"),
		},
	}
	if cfg.TokenTTL > 0 && cfg.JobRetention > 0 && cfg.TokenTTL > cfg.JobRetention {
		l.fail("OMDI_TOKEN_TTL must not exceed OMDI_JOB_RETENTION")
	}
	if cfg.JobTimeout > 0 && cfg.JobRetention > 0 && cfg.JobRetention <= cfg.JobTimeout {
		l.fail("OMDI_JOB_RETENTION must exceed OMDI_JOB_TIMEOUT")
	}
	if err := errors.Join(l.errs...); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (l *loader) fail(message string) {
	l.errs = append(l.errs, errors.New(message))
}

// value returns the raw value when it is set and well formed. A set value must be
// non-empty without surrounding whitespace; otherwise a failure is recorded and
// callers fall back to their default, which is discarded because Load returns an error.
func (l *loader) value(name string) (string, bool) {
	value, present := l.lookup(name)
	if !present {
		return "", false
	}
	if value == "" || value != strings.TrimSpace(value) {
		l.fail(name + " must not be empty or contain surrounding whitespace")

		return "", false
	}

	return value, true
}

func (l *loader) httpAddr(name, fallback string) string {
	value, ok := l.value(name)
	if !ok {
		return fallback
	}
	_, port, err := net.SplitHostPort(value)
	if err != nil {
		l.fail(name + " must use host:port syntax")

		return ""
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		l.fail(name + " must contain a port from 1 to 65535")

		return ""
	}

	return value
}

func (l *loader) path(name, fallback string) string {
	value, ok := l.value(name)
	if !ok {
		return fallback
	}
	if !filepath.IsAbs(value) || filepath.Clean(value) != value {
		l.fail(name + " must be an absolute, clean path")

		return ""
	}

	return value
}

func (l *loader) publicURL(name, fallback string) string {
	value, ok := l.value(name)
	if !ok {
		return fallback
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Opaque != "" {
		l.fail(name + " must be an absolute http or https URL without user information, query, or fragment")

		return ""
	}

	return strings.TrimSuffix(value, "/")
}

func (l *loader) platforms(name string) []string {
	value, ok := l.value(name)
	if !ok {
		return urlpolicy.PlatformIDs()
	}
	ids := strings.Split(value, ",")
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !urlpolicy.IsPlatform(id) || seen[id] {
			l.fail(name + " must be a comma-separated list of unique supported platform identifiers")

			return nil
		}
		seen[id] = true
	}

	return ids
}

func (l *loader) integer(name string, fallback, minimum, maximum int64) int64 {
	value, ok := l.value(name)
	if !ok {
		return fallback
	}
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil || strings.HasPrefix(value, "+") || number < minimum || number > maximum {
		l.fail(name + " must be an integer from " + strconv.FormatInt(minimum, 10) + " to " + strconv.FormatInt(maximum, 10))

		return 0
	}

	return number
}

func (l *loader) duration(name string, fallback, minimum, maximum time.Duration) time.Duration {
	value, ok := l.value(name)
	if !ok {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < minimum || parsed > maximum {
		l.fail(name + " must be a duration from " + minimum.String() + " to " + maximum.String())

		return 0
	}

	return parsed
}
