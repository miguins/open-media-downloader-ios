package extractor

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestYTDLPRejectsNonLoopbackProxy(t *testing.T) {
	if validProxyURL("http://example.com:8080") {
		t.Fatal("accepted remote proxy")
	}
	if !validProxyURL("http://127.0.0.1:8080") {
		t.Fatal("rejected loopback proxy")
	}
}

func TestYTDLPArguments(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	tool := testTool(t, `printf '%s\n' "$@" > '`+argsFile+`'`)
	media := NewMediaTools(probeTool(t, probeJSON("mp4", "h264", "aac")), "/bin/true", NewRunner())
	dir := t.TempDir()
	regular(t, dir, "media.mp4")
	if _, err := NewYTDLP(tool, "/bin/true", NewRunner(), media).Extract(t.Context(), adapterRequest(dir, "tiktok", 1), "http://127.0.0.1:8080"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(argsFile) //nolint:gosec // Path is under the test's temporary directory.
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSpace(string(raw)), "\n")
	// --max-downloads makes yt-dlp exit 101 after a successful download.
	if slices.Contains(args, "--max-downloads") || slices.Contains(args, "--print") {
		t.Fatalf("unexpected arguments: %q", args)
	}
	i := slices.Index(args, "--format")
	if i < 0 || !strings.Contains(args[i+1], "h264") || !strings.Contains(args[i+1], "aac") {
		t.Fatalf("format = %q", args)
	}
	if args[len(args)-2] != "--" || args[len(args)-1] != "https://example.invalid/post" {
		t.Fatalf("URL is not the final operand: %q", args)
	}
}

func TestYTDLPOversizedSkipIsTooLarge(t *testing.T) {
	tool := testTool(t, `printf '%s\n' '[download] File is larger than max-filesize (2 bytes > 1 bytes). Aborting.'`)
	media := NewMediaTools(probeTool(t, probeJSON("mp4", "h264", "aac")), "/bin/true", NewRunner())
	if _, err := NewYTDLP(tool, "/bin/true", NewRunner(), media).Extract(t.Context(), adapterRequest(t.TempDir(), "youtube", 1), "http://127.0.0.1:8080"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("error = %v", err)
	}
}

func TestYTDLPURLUsesLoggedOutForms(t *testing.T) {
	for raw, want := range map[string]string{
		"https://vimeo.com/123":                          "https://player.vimeo.com/video/123",
		"https://vimeo.com/123/":                         "https://player.vimeo.com/video/123",
		"https://player.vimeo.com/video/123":             "https://player.vimeo.com/video/123",
		"https://redd.it/Ab9":                            "https://www.reddit.com/comments/Ab9/",
		"https://reddit.com/gallery/Ab9/":                "https://www.reddit.com/comments/Ab9/",
		"https://old.reddit.com/r/a/comments/b/":         "https://old.reddit.com/r/a/comments/b/",
		"https://www.youtube.com/watch?v=abc":            "https://www.youtube.com/watch?v=abc",
		"https://www.instagram.com/p/Ab_9/":              "https://www.instagram.com/p/Ab_9/",
		"https://www.tiktok.com/@creator/video/123":      "https://www.tiktok.com/@creator/video/123",
		"https://vimeo.com/123/extra-unnormalized-shape": "https://vimeo.com/123/extra-unnormalized-shape",
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := ytdlpURL(u); got != want {
			t.Errorf("ytdlpURL(%q) = %q; want %q", raw, got, want)
		}
	}
}

func TestYTDLPPassesLoggedOutURL(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	tool := testTool(t, `printf '%s\n' "$@" > '`+argsFile+`'; printf x > media.mp4`)
	media := NewMediaTools(probeTool(t, probeJSON("mp4", "h264", "aac")), "/bin/true", NewRunner())
	request := adapterRequest(t.TempDir(), "vimeo", 1)
	request.URL = "https://vimeo.com/123"
	if _, err := NewYTDLP(tool, "/bin/true", NewRunner(), media).Extract(t.Context(), request, "http://127.0.0.1:8080"); err != nil {
		t.Fatal(err)
	}
	args := readArgs(t, argsFile)
	if args[len(args)-1] != "https://player.vimeo.com/video/123" {
		t.Fatalf("URL operand = %q", args[len(args)-1])
	}
}

func TestYTDLPFormatAcceptsUndeclaredProgressiveMP4(t *testing.T) {
	// Instagram and Vimeo leave progressive MP4 codecs undeclared; a declared
	// incompatible codec or missing audio still fails the none-inclusive filters.
	if !strings.Contains(ytdlpFormat, "/b[ext=mp4][vcodec~=?'^(avc1|h264)'][acodec~=?'^(mp4a|aac)']/") {
		t.Fatalf("format = %q", ytdlpFormat)
	}
}

func readArgs(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path) //nolint:gosec // Path is under the test's temporary directory.
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

// postMediaProbe classifies .mp4 entries as video and every other entry as JPEG.
func postMediaProbe(t *testing.T) string {
	return testTool(t, `last=""; for arg in "$@"; do last="$arg"; done; case "$last" in *.mp4) printf '%s' '`+
		probeJSON("mp4", "h264", "aac")+`';; *) printf '%s' '`+probeJSON("jpeg_pipe", "mjpeg", "")+`';; esac`)
}

const missingFormats = `ERROR: [Instagram] Ab_9: No video formats found!`

func TestYTDLPPostMediaMode(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	tool := testTool(t, `printf '%s\n' "$@" > '`+argsFile+`'
printf x > item-001.jpg; printf x > item-002.jpg; printf x > item-002.mp4
printf '%s\n' 'WARNING: No video formats found!' '`+missingFormats+`' >&2
exit 1`)
	media := NewMediaTools(postMediaProbe(t), "/bin/true", NewRunner())
	request := adapterRequest(t.TempDir(), "instagram", 2)
	files, err := NewYTDLP(tool, "/bin/true", NewRunner(), media).Extract(t.Context(), request, "http://127.0.0.1:8080")
	if err != nil || len(files) != 2 || files[0] != (File{"item-001.jpg", "image/jpeg"}) || files[1] != (File{"item-002.mp4", "video/mp4"}) {
		t.Fatalf("Extract = %#v, %v", files, err)
	}
	args := readArgs(t, argsFile)
	for _, want := range [][]string{
		{"--yes-playlist"}, {"--playlist-items", "1:3"}, {"--no-abort-on-error"}, {"--ignore-no-formats-error"},
		{"--write-thumbnail"}, {"--paths", "home:" + request.WorkDir}, {"--output", "item-%(playlist_index&{:03d}|000)s.%(ext)s"},
		{"--format", ytdlpFormat + "/bv*+ba/b"},
	} {
		i := slices.Index(args, want[0])
		if i < 0 || !slices.Equal(args[i:i+len(want)], want) {
			t.Errorf("missing %q in %q", want, args)
		}
	}
	for _, unwanted := range []string{"--no-playlist", "--abort-on-error", "--no-write-thumbnail", "media.%(ext)s"} {
		if slices.Contains(args, unwanted) {
			t.Errorf("unexpected %q", unwanted)
		}
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "temp:") {
			t.Errorf("unexpected temporary path %q", arg)
		}
	}
}

func TestYTDLPPostMediaFailures(t *testing.T) {
	media := NewMediaTools(postMediaProbe(t), "/bin/true", NewRunner())
	for name, test := range map[string]struct {
		platform, body string
		want           error
	}{
		"no error line":      {"instagram", `printf x > item-000.jpg; exit 1`, ErrExtractionFailed},
		"other error":        {"instagram", `printf x > item-000.jpg; printf '%s\n' '` + missingFormats + `' 'ERROR: [Instagram] Cd: Unable to download' >&2; exit 1`, ErrExtractionFailed},
		"other exit status":  {"instagram", `printf x > item-000.jpg; printf '%s\n' '` + missingFormats + `' >&2; exit 2`, ErrExtractionFailed},
		"single-video mode":  {"reddit", `printf x > media.mp4; printf '%s\n' '` + missingFormats + `' >&2; exit 1`, ErrExtractionFailed},
		"oversized skip":     {"instagram", `printf '%s\n' '[download] File is larger than max-filesize (2 bytes > 1 bytes). Aborting.'`, ErrTooLarge},
		"too many items":     {"instagram", `printf x > item-001.jpg; printf x > item-002.jpg`, ErrTooLarge},
		"unpaired thumbnail": {"instagram", `printf x > item-001.jpg; printf x > item-001.webp`, ErrExtractionFailed},
	} {
		t.Run(name, func(t *testing.T) {
			y := NewYTDLP(testTool(t, test.body), "/bin/true", NewRunner(), media)
			if _, err := y.Extract(t.Context(), adapterRequest(t.TempDir(), test.platform, 1), "http://127.0.0.1:8080"); !errors.Is(err, test.want) {
				t.Fatalf("error = %v; want %v", err, test.want)
			}
		})
	}
}

func TestDiscoverPostMedia(t *testing.T) {
	for name, test := range map[string]struct {
		entries, want []string
		err           error
	}{
		"single video":       {[]string{"item-000.jpg", "item-000.mp4"}, []string{"item-000.mp4"}, nil},
		"single photo":       {[]string{"item-000.jpg"}, []string{"item-000.jpg"}, nil},
		"mixed carousel":     {[]string{"item-001.jpg", "item-002.webp", "item-002.mp4"}, []string{"item-001.jpg", "item-002.mp4"}, nil},
		"two images":         {[]string{"item-001.jpg", "item-001.png"}, nil, ErrExtractionFailed},
		"three entries":      {[]string{"item-001.jpg", "item-001.mp4", "item-001.png"}, nil, ErrExtractionFailed},
		"gap":                {[]string{"item-002.jpg"}, nil, ErrExtractionFailed},
		"unexpected name":    {[]string{"media.mp4"}, nil, ErrExtractionFailed},
		"partial download":   {[]string{"item-001.mp4.part"}, nil, ErrExtractionFailed},
		"too many items":     {[]string{"item-001.jpg", "item-002.jpg", "item-003.mp4"}, nil, ErrTooLarge},
		"empty output":       {nil, nil, ErrExtractionFailed},
		"zero with carousel": {[]string{"item-000.jpg", "item-001.jpg"}, nil, ErrExtractionFailed},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			for _, entry := range test.entries {
				regular(t, dir, entry)
			}
			got, err := discoverPostMedia(dir, 2)
			if !errors.Is(err, test.err) || !slices.Equal(got, test.want) {
				t.Fatalf("discoverPostMedia = %q, %v; want %q, %v", got, err, test.want, test.err)
			}
		})
	}
	if _, err := discoverPostMedia(filepath.Join(t.TempDir(), "missing"), 2); err == nil {
		t.Fatal("missing directory accepted")
	}
}
