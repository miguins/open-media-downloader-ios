package extractor

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miguins/open-media-downloader-ios/internal/job"
)

func TestRedditGalleryOptions(t *testing.T) {
	argsPath := filepath.Join(t.TempDir(), "args")
	tool := testTool(t, `printf '%s\n' "$@" > '`+argsPath+`'; printf x > item-000.jpg`)
	media := NewMediaTools(postMediaProbe(t), "/configured/ffmpeg", NewRunner())
	req := adapterRequest(t.TempDir(), "reddit", 2)
	files, err := NewGalleryDL(tool, NewRunner(), media).Extract(t.Context(), req, "http://127.0.0.1:8080")
	if err != nil || len(files) != 1 || files[0].MediaType != "image/jpeg" {
		t.Fatalf("image = %v, %v", files, err)
	}
	args := readArgs(t, argsPath)
	for _, want := range []string{"extractor.reddit.api=rest", "extractor.reddit.comments=0", "extractor.reddit.recursion=0", "extractor.reddit.selftext=false", "extractor.reddit.previews=false", "extractor.reddit.videos=dash", "extractor.whitelist=[]", "downloader.ytdl.module=yt_dlp", "item-{num:03}.{extension}", "1-3"} {
		if !strings.Contains(strings.Join(args, "\n"), want+"\n") {
			t.Errorf("missing %s", want)
		}
	}
	var raw map[string]any
	for _, arg := range args {
		if strings.HasPrefix(arg, "downloader.ytdl.raw-options=") {
			if err := json.Unmarshal([]byte(strings.TrimPrefix(arg, "downloader.ytdl.raw-options=")), &raw); err != nil {
				t.Fatal(err)
			}
		}
	}
	for name, want := range map[string]any{
		"proxy": "http://127.0.0.1:8080", "ffmpeg_location": "/configured/ffmpeg", "merge_output_format": "mp4",
		"format": ytdlpFormat, "max_filesize": float64(req.MaxBytes), "retries": float64(3), "socket_timeout": float64(30),
		"concurrent_fragment_downloads": float64(1), "ignoreerrors": false, "skip_unavailable_fragments": false, "writethumbnail": false, "writeinfojson": false,
		"writesubtitles": false, "writeautomaticsub": false, "getcomments": false, "cachedir": false, "enable_file_urls": false,
	} {
		if raw[name] != want {
			t.Errorf("raw option %s = %v; want %v", name, raw[name], want)
		}
	}
	if components, ok := raw["remote_components"].([]any); !ok || len(components) != 0 {
		t.Fatal("remote components enabled")
	}
}

func TestRedditGalleryOutputs(t *testing.T) {
	media := NewMediaTools(postMediaProbe(t), "/bin/true", NewRunner())
	for name, test := range map[string]struct {
		script   string
		want     []File
		tooLarge bool
		detail   job.ErrorDetail
	}{
		"gallery":             {script: `printf x > item-002.jpg; printf y > item-001.jpg`, want: []File{{"item-001.jpg", "image/jpeg"}, {"item-002.jpg", "image/jpeg"}}},
		"video":               {script: `printf x > item-000.mp4`, want: []File{{"item-000.mp4", "video/mp4"}}},
		"empty":               {script: `true`, detail: job.DetailNoMedia},
		"excess items":        {script: `printf x > item-001.jpg; printf x > item-002.jpg; printf x > item-003.jpg`, tooLarge: true},
		"oversized stderr":    {script: `printf '%s\n' 'File is larger than max-filesize' >&2`, tooLarge: true},
		"oversized with exit": {script: `printf '%s\n' 'File is larger than max-filesize' >&2; exit 4`, tooLarge: true},
		"oversized stdout":    {script: `printf '%s\n' 'File is larger than max-filesize'`, tooLarge: true},
		"partial failure":     {script: `printf x > item-001.jpg; printf '%s\n' 'ERROR: download failed' >&2; exit 1`, detail: job.DetailToolError},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := NewGalleryDL(testTool(t, test.script), NewRunner(), media).Extract(t.Context(), adapterRequest(t.TempDir(), "reddit", 2), "http://127.0.0.1:8080")
			if test.tooLarge {
				if !errors.Is(err, ErrTooLarge) {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if test.detail != "" {
				_ = requireFailure(t, err, test.detail, "gallery-dl")
				if got != nil {
					t.Fatal("partial output returned")
				}
				return
			}
			if err != nil || len(got) != len(test.want) {
				t.Fatalf("outputs = %v, %v", got, err)
			}
			for i := range got {
				if got[i] != test.want[i] {
					t.Fatalf("output %d = %v", i, got[i])
				}
			}
		})
	}
	// A failed discovery is not an empty post and must retain its failure.
	request := adapterRequest(t.TempDir(), "reddit", 1)
	if err := os.Remove(request.WorkDir); err != nil {
		t.Fatal(err)
	}
	_, err := NewGalleryDL("/bin/true", NewRunner(), media).Extract(t.Context(), request, "http://127.0.0.1:8080")
	if err == nil {
		t.Fatal("missing work directory accepted")
	}
}

func TestRedditGalleryRejectsUnexpectedFiles(t *testing.T) {
	media := NewMediaTools(postMediaProbe(t), "/bin/true", NewRunner())
	_, err := NewGalleryDL(testTool(t, `printf x > unexpected`), NewRunner(), media).Extract(t.Context(), adapterRequest(t.TempDir(), "reddit", 2), "http://127.0.0.1:8080")
	if !errors.Is(err, ErrExtractionFailed) {
		t.Fatalf("unsafe output = %v", err)
	}
}

func TestRedditNativeMediaOptions(t *testing.T) {
	req := adapterRequest(t.TempDir(), "reddit", 2)
	argsPath := filepath.Join(t.TempDir(), "args")
	tool := testTool(t, `true`)
	installRedditPython(t, tool, `printf '%s\n' "$@" > '`+argsPath+`'; printf x > item-000.mp4`)
	media := NewMediaTools(postMediaProbe(t), "/configured/ffmpeg", NewRunner())
	files, err := NewYTDLP(tool, media.ffmpegPath, NewRunner(), media).ExtractRedditMedia(t.Context(), req, "http://127.0.0.1:1")
	if err != nil || len(files) != 1 {
		t.Fatalf("video = %v, %v", files, err)
	}
	args := readArgs(t, argsPath)
	var raw map[string]any
	for _, arg := range args {
		if strings.HasPrefix(arg, "{") {
			if err := json.Unmarshal([]byte(arg), &raw); err != nil {
				t.Fatal(err)
			}
		}
	}
	if raw["extract_flat"] != true || raw["skip_unavailable_fragments"] != false {
		t.Fatalf("unsafe video options: %v", raw)
	}
	if !strings.Contains(strings.Join(args, "\n"), "process=False") {
		t.Fatal("transparent results must remain unprocessed")
	}
	req.Platform = "x"
	if _, err := NewYTDLP(tool, media.ffmpegPath, NewRunner(), media).ExtractRedditMedia(t.Context(), req, "http://127.0.0.1:1"); err == nil {
		t.Fatal("accepted non-Reddit native media")
	}
}

func TestRedditNativeMediaFailures(t *testing.T) {
	for _, tc := range []struct {
		name, script string
		want         error
		detail       job.ErrorDetail
	}{
		{"too large", `printf 'File is larger than max-filesize\n' >&2; exit 4`, ErrTooLarge, ""},
		{"no media", `printf 'ERROR: No media\n' >&2; exit 4`, nil, job.DetailNoMedia},
		{"invalid", `printf x > unexpected`, ErrExtractionFailed, ""},
		{"empty", `true`, ErrExtractionFailed, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := testTool(t, `true`)
			installRedditPython(t, tool, tc.script)
			media := NewMediaTools(postMediaProbe(t), "/bin/true", NewRunner())
			files, err := NewYTDLP(tool, media.ffmpegPath, NewRunner(), media).ExtractRedditMedia(t.Context(), adapterRequest(t.TempDir(), "reddit", 2), "http://127.0.0.1:1")
			if tc.detail != "" {
				_ = requireFailure(t, err, tc.detail, "yt-dlp")
			} else if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v", err)
			}
			if len(files) != 0 {
				t.Fatal("partial files returned")
			}
		})
	}
}

func TestRedditNativeMediaURLForms(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"https://redd.it/abc123", "https://www.reddit.com/comments/abc123/"},
		{"https://www.reddit.com/gallery/abc123/", "https://www.reddit.com/comments/abc123/"},
		{"https://www.reddit.com/comments/abc123/", "https://www.reddit.com/comments/abc123/"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			argsPath := filepath.Join(t.TempDir(), "args")
			tool := testTool(t, `true`)
			installRedditPython(t, tool, `printf '%s\n' "$@" > '`+argsPath+`'; printf x > item-000.mp4`)
			req := adapterRequest(t.TempDir(), "reddit", 2)
			req.URL = tc.raw
			media := NewMediaTools(postMediaProbe(t), "/bin/true", NewRunner())
			if _, err := NewYTDLP(tool, media.ffmpegPath, NewRunner(), media).ExtractRedditMedia(t.Context(), req, "http://127.0.0.1:1"); err != nil {
				t.Fatal(err)
			}
			args := readArgs(t, argsPath)
			if args[len(args)-1] != tc.want || req.URL != tc.raw {
				t.Fatalf("extraction URL = %q, want %q", args[len(args)-1], tc.want)
			}
		})
	}
}

func installRedditPython(t *testing.T, tool, body string) {
	t.Helper()
	python := testTool(t, body)
	if err := os.Rename(python, filepath.Join(filepath.Dir(tool), "python")); err != nil {
		t.Fatal(err)
	}
}
