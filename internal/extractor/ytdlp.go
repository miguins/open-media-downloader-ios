package extractor

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/miguins/open-media-downloader-ios/internal/job"
)

// ytdlpFormat prefers H.264/AAC without transcoding. Extractors name H.264
// "avc1.*" or "h264" and AAC "mp4a.*" or "aac". Instagram and Vimeo leave the
// codecs of progressive MP4 formats undeclared, so the third choice also accepts
// undeclared codecs; the none-inclusive filters still reject a declared
// incompatible codec or missing audio, and inspection checks the actual streams.
// Audio-only posts fall back to M4A and then MP3.
const ytdlpFormat = "bv[vcodec~='^(avc1|h264)']+ba[acodec~='^(mp4a|aac)']/" +
	"b[vcodec~='^(avc1|h264)'][acodec~='^(mp4a|aac)']/" +
	"b[ext=mp4][vcodec~=?'^(avc1|h264)'][acodec~=?'^(mp4a|aac)']/ba[ext=m4a]/ba[ext=mp3]"

// ytdlpPostFormat ends with any format, so an entry that has formats is always
// downloaded and inspected instead of being replaced by its thumbnail.
const ytdlpPostFormat = ytdlpFormat + "/bv*+ba/b"

var ytdlpTooLarge = []byte("File is larger than max-filesize")

// ytdlpMissingFormats is the error yt-dlp reports for an Instagram photo entry,
// which has no formats and is delivered as its thumbnail.
var ytdlpMissingFormats = regexp.MustCompile(`^ERROR: \[Instagram\] [A-Za-z0-9_-]+: No video formats found!$`)

var (
	vimeoVideo    = regexp.MustCompile(`^/([0-9]+)/?$`)
	redditShort   = regexp.MustCompile(`^/([A-Za-z0-9]+)/?$`)
	redditGallery = regexp.MustCompile(`^/gallery/([A-Za-z0-9]+)/?$`)
)

// YTDLP extracts public posts with yt-dlp.
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
	if !validAdapterRequest(request, "youtube", "vimeo", "tiktok", "instagram", "reddit") || !validProxyURL(proxyURL) {
		return nil, errors.New("extractor: invalid adapter request")
	}
	// Instagram posts may hold photos, which yt-dlp exposes only as entry thumbnails.
	postMedia := request.Platform == "instagram"
	result, err := y.runner.Run(ctx, Command{Path: y.path, Args: y.args(request, proxyURL, postMedia), Dir: request.WorkDir, StdoutLimit: 256 << 10, StderrLimit: 64 << 10})
	// A photo entry makes yt-dlp exit with status 1 after writing its thumbnail.
	if err != nil && (!postMedia || !errors.Is(err, ErrCommandExit) || result.ExitCode != 1 || !onlyMissingFormats(result.Stderr)) {
		return nil, toolFailure(ctx, "yt-dlp", job.DetailToolError, result, err)
	}
	// yt-dlp skips an oversized download with a successful exit and only reports it on stdout.
	if bytes.Contains(result.Stdout, ytdlpTooLarge) {
		return nil, ErrTooLarge
	}
	var names []string
	if postMedia {
		names, err = discoverPostMedia(request.WorkDir, request.MaxItems)
	} else if names, err = discoverYTDLP(request.WorkDir); err != nil {
		err = ErrExtractionFailed
	}
	if err != nil {
		return nil, err
	}
	files, err := y.media.Finalize(ctx, request.WorkDir, names)
	if err != nil {
		return nil, err
	}
	return files, nil
}

func (y *YTDLP) args(request Request, proxyURL string, postMedia bool) []string {
	args := []string{
		"--ignore-config", "--no-config-locations", "--no-cache-dir", "--no-plugin-dirs", "--no-remote-components",
		"--match-filters", "!is_live",
		"--concurrent-fragments", "1", "--retries", "3", "--fragment-retries", "3", "--file-access-retries", "1", "--socket-timeout", "30",
		"--no-write-comments", "--no-write-info-json", "--no-write-playlist-metafiles", "--no-write-subs",
		"--no-progress", "--color", "never", "--ies", "default,-generic", "--proxy", proxyURL,
		"--ffmpeg-location", y.ffmpegPath, "--max-filesize", strconv.FormatInt(request.MaxBytes, 10),
		"--paths", "home:" + request.WorkDir,
	}
	// --max-downloads is not used: yt-dlp exits with status 101 after reaching it,
	// even on success. The URL policy, with --no-playlist or the post-media
	// --playlist-items bound, already limits a job to one post.
	if postMedia {
		// No temporary path: yt-dlp moves a thumbnail out of it only after a
		// successful media download, which a photo entry never has.
		args = append(args,
			"--yes-playlist", "--playlist-items", "1:"+strconv.Itoa(request.MaxItems+1), "--no-abort-on-error",
			"--ignore-no-formats-error", "--write-thumbnail",
			"--output", "item-%(playlist_index&{:03d}|000)s.%(ext)s", "--format", ytdlpPostFormat)
	} else {
		args = append(args,
			"--abort-on-error", "--no-playlist", "--no-write-thumbnail",
			"--paths", "temp:"+filepath.Join(request.WorkDir, ".omdi-runtime/tmp"),
			"--output", "media.%(ext)s", "--format", ytdlpFormat)
	}
	u, _ := url.Parse(request.URL) // validAdapterRequest has parsed it.
	return append(args, "--merge-output-format", "mp4", "--", ytdlpURL(u))
}

// ytdlpURL returns the equivalent URL form that yt-dlp extracts without a login:
// the Vimeo embed player, and the Reddit comments route that yt-dlp recognizes.
func ytdlpURL(u *url.URL) string {
	switch {
	case u.Host == "player.vimeo.com":
	case onDomain(u.Host, "vimeo.com"):
		if m := vimeoVideo.FindStringSubmatch(u.Path); m != nil {
			return "https://player.vimeo.com/video/" + m[1]
		}
	case u.Host == "redd.it":
		if m := redditShort.FindStringSubmatch(u.Path); m != nil {
			return "https://www.reddit.com/comments/" + m[1] + "/"
		}
	case onDomain(u.Host, "reddit.com"):
		if m := redditGallery.FindStringSubmatch(u.Path); m != nil {
			return "https://www.reddit.com/comments/" + m[1] + "/"
		}
	}
	return u.String()
}

func onDomain(host, domain string) bool { return host == domain || strings.HasSuffix(host, "."+domain) }

// onlyMissingFormats reports whether yt-dlp's only errors are Instagram photo entries without formats.
func onlyMissingFormats(stderr []byte) bool {
	found := false
	for _, line := range strings.Split(string(stderr), "\n") {
		if !strings.Contains(line, "ERROR") {
			continue
		}
		if !ytdlpMissingFormats.MatchString(line) {
			return false
		}
		found = true
	}
	return found
}

// discoverPostMedia applies the gallery numbering rules to post-media output. An
// index holds one entry, or one MP4 video with its thumbnail, which is left out.
func discoverPostMedia(dir string, maxItems int) ([]string, error) {
	entries, err := numberedEntries(dir)
	if err != nil {
		return nil, err
	}
	byNumber := make(map[int][]numberedEntry)
	for _, entry := range entries {
		byNumber[entry.number] = append(byNumber[entry.number], entry)
	}
	items := make([]numberedEntry, 0, len(byNumber))
	for _, group := range byNumber {
		switch {
		case len(group) == 1:
			items = append(items, group[0])
		case len(group) == 2 && isMP4(group[0]) != isMP4(group[1]):
			if isMP4(group[0]) {
				items = append(items, group[0])
			} else {
				items = append(items, group[1])
			}
		default:
			return nil, ErrExtractionFailed
		}
	}
	return orderedNames(items, maxItems)
}

func isMP4(entry numberedEntry) bool { return strings.HasSuffix(entry.name, ".mp4") }

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
