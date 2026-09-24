package extractor

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// ytdlpFormat prefers H.264/AAC without transcoding. Extractors name H.264
// "avc1.*" or "h264" and AAC "mp4a.*" or "aac". Video without a compatible audio
// track is never selected; audio-only posts fall back to M4A and then MP3.
const ytdlpFormat = "bv[vcodec~='^(avc1|h264)']+ba[acodec~='^(mp4a|aac)']/" +
	"b[vcodec~='^(avc1|h264)'][acodec~='^(mp4a|aac)']/ba[ext=m4a]/ba[ext=mp3]"

var ytdlpTooLarge = []byte("File is larger than max-filesize")

// YTDLP extracts one public video post with yt-dlp.
type YTDLP struct {
	path, ffmpegPath string
	runner           *Runner
	media            *MediaTools
}

// NewYTDLP constructs the yt-dlp adapter.
func NewYTDLP(path, ffmpegPath string, runner *Runner, media *MediaTools) *YTDLP {
	return &YTDLP{path: path, ffmpegPath: ffmpegPath, runner: runner, media: media}
}

// Extract downloads and validates one supported public post.
func (y *YTDLP) Extract(ctx context.Context, request Request, proxyURL string) ([]File, error) {
	if !validAdapterRequest(request, "youtube", "vimeo", "tiktok") || !validProxyURL(proxyURL) {
		return nil, errors.New("extractor: invalid adapter request")
	}
	args := []string{
		"--ignore-config", "--no-config-locations", "--no-cache-dir", "--no-plugin-dirs", "--no-remote-components",
		"--abort-on-error", "--no-playlist", "--match-filters", "!is_live",
		"--concurrent-fragments", "1", "--retries", "3", "--fragment-retries", "3", "--file-access-retries", "1", "--socket-timeout", "30",
		"--no-write-comments", "--no-write-info-json", "--no-write-playlist-metafiles", "--no-write-thumbnail", "--no-write-subs",
		"--no-progress", "--color", "never", "--ies", "default,-generic", "--proxy", proxyURL,
		"--ffmpeg-location", y.ffmpegPath, "--max-filesize", strconv.FormatInt(request.MaxBytes, 10),
		"--paths", "home:" + request.WorkDir, "--paths", "temp:" + filepath.Join(request.WorkDir, ".omdi-runtime/tmp"),
		"--output", "media.%(ext)s",
		"--format", ytdlpFormat, "--merge-output-format", "mp4", "--", request.URL,
	}
	// --max-downloads is not used: yt-dlp exits with status 101 after reaching it,
	// even on success. The URL policy and --no-playlist already bound a job to one post.
	result, err := y.runner.Run(ctx, Command{Path: y.path, Args: args, Dir: request.WorkDir, StdoutLimit: 256 << 10, StderrLimit: 64 << 10})
	if err != nil {
		return nil, adapterCommandError(ctx, err)
	}
	// yt-dlp skips an oversized download with a successful exit and only reports it on stdout.
	if bytes.Contains(result.Stdout, ytdlpTooLarge) {
		return nil, ErrTooLarge
	}
	names, err := discoverYTDLP(request.WorkDir)
	if err != nil {
		return nil, ErrExtractionFailed
	}
	files, err := y.media.Finalize(ctx, request.WorkDir, names)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		if errors.Is(err, ErrExtractionFailed) {
			return nil, ErrExtractionFailed
		}
		return nil, err
	}
	return files, nil
}

func validAdapterRequest(r Request, platforms ...string) bool {
	ok := false
	for _, platform := range platforms {
		ok = ok || r.Platform == platform
	}
	u, err := url.Parse(r.URL)
	return ok && err == nil && u.Scheme == "https" && u.Host != "" && safeWorkDir(r.WorkDir) && r.MaxBytes > 0 && r.MaxItems > 0
}

func validProxyURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil || host != "127.0.0.1" {
		return false
	}
	n, err := strconv.Atoi(port)
	return err == nil && n > 0 && n <= 65535
}

func adapterCommandError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, ErrCommandExit) || errors.Is(err, ErrOutputLimit) {
		return ErrExtractionFailed
	}
	return err
}

func discoverYTDLP(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, errors.New("extractor: enumerate output")
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasPrefix(name, ".") || !strings.HasPrefix(name, "media.") || strings.TrimPrefix(name, "media.") == "" || strings.Contains(strings.TrimPrefix(name, "media."), ".") {
			return nil, ErrExtractionFailed
		}
		if !secureRegular(filepath.Join(dir, name)) {
			return nil, ErrExtractionFailed
		}
		names = append(names, name)
	}
	if len(names) != 1 {
		return nil, ErrExtractionFailed
	}
	return names, nil
}

func secureRegular(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink == 1
}
