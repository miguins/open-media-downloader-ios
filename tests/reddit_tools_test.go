//go:build omdi_toolintegration

package tests

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/miguins/open-media-downloader-ios/internal/extractor"
	"github.com/miguins/open-media-downloader-ios/internal/job"
)

func TestRedditInstalledMediaTools(t *testing.T) {
	root := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	img.Set(0, 0, color.White)
	f, err := os.Create(filepath.Join(root, "one.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(f, img, nil); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Fatal(err)
	}
	python, err := exec.LookPath("python")
	if err != nil {
		t.Fatal(err)
	}
	// One synthetic blue H.264 frame, generated once with libx264 and stripped of SEI.
	// Runtime FFmpeg needs only its existing decoder/remuxer and AAC encoder.
	video, _ := base64.StdEncoding.DecodeString("AAAAAWdCwArZCWwEQAAAAwBAAAAFA8SJkgAAAAFoy4PLIAAAAWWIhA3xGKAAKp8cAAUKI4AAhGyddeA=")
	if err := os.WriteFile(filepath.Join(root, "blue.h264"), bytes.Repeat(video, 20), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), ffmpeg, "-v", "error", "-fflags", "+genpts", "-r", "10", "-i", filepath.Join(root, "blue.h264"), "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100", "-t", "2", "-seg_duration", "0.2", "-c:v", "copy", "-c:a", "aac", "-f", "dash", filepath.Join(root, "output.mpd"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("synthetic streams: %v %s", err, out)
	}
	cmd = exec.CommandContext(t.Context(), ffmpeg, "-v", "error", "-fflags", "+genpts", "-r", "10", "-i", filepath.Join(root, "blue.h264"), "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100", "-t", "2", "-c:v", "copy", "-c:a", "aac", filepath.Join(root, "progressive.mp4"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("synthetic progressive media: %v %s", err, out)
	}
	manifest := `<MPD xmlns="urn:mpeg:dash:schema:mpd:2011" type="static" mediaPresentationDuration="PT2S"><Period><AdaptationSet mimeType="video/mp4"><Representation id="combined" bandwidth="100000" codecs="avc1.42c00a,mp4a.40.2"><BaseURL>https://v.redd.it/progressive.mp4</BaseURL></Representation></AdaptationSet></Period></MPD>`
	if err := os.WriteFile(filepath.Join(root, "progressive.mpd"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/comments/") {
			id := strings.Split(r.URL.Path, "/")[2]
			if id == "acc123" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": 403, "reason": "private"})
				return
			}
			post := map[string]any{"id": id, "created_utc": 1, "title": "Synthetic fixture", "is_video": false, "is_self": false, "selftext_html": "", "url": "https://i.redd.it/one.jpg"}
			switch id {
			case "def456":
				post["url"] = "https://www.reddit.com/gallery/def456/"
				post["gallery_data"] = map[string]any{"items": []any{map[string]any{"media_id": "one"}, map[string]any{"media_id": "two"}}}
				post["media_metadata"] = map[string]any{"one": map[string]any{"status": "valid", "s": map[string]any{"u": "https://i.redd.it/one.jpg"}}, "two": map[string]any{"status": "valid", "s": map[string]any{"u": "https://preview.redd.it/two.jpg?width=32&amp;format=pjpg"}}}
			case "proc123":
				post["url"] = "https://v.redd.it/proc123"
			case "imgcut":
				post["url"] = "https://i.redd.it/truncated.jpg"
			case "galmax", "galbad", "galext", "galmis":
				post["url"] = "https://www.reddit.com/gallery/" + id + "/"
				post["gallery_data"] = map[string]any{"items": []any{map[string]any{"media_id": "one"}, map[string]any{"media_id": "two"}}}
				url := "https://i.redd.it/two.jpg"
				status := "valid"
				if id == "galext" {
					url = "https://external.example/two.jpg"
				}
				if id == "galmis" {
					url = "https://i.redd.it/missing.jpg"
				}
				if id == "galbad" {
					status = "failed"
				}
				if id == "galmax" {
					post["gallery_data"] = map[string]any{"items": []any{map[string]any{"media_id": "one"}, map[string]any{"media_id": "two"}, map[string]any{"media_id": "three"}}}
				}
				post["media_metadata"] = map[string]any{"one": map[string]any{"status": "valid", "s": map[string]any{"u": "https://i.redd.it/one.jpg"}}, "two": map[string]any{"status": status, "s": map[string]any{"u": url}}}
			case "vid789", "bad789", "nat789", "crs789", "prog123":
				post["url"] = "https://v.redd.it/vid789"
				post["is_video"] = true
				post["secure_media"] = map[string]any{"reddit_video": map[string]any{"dash_url": "https://v.redd.it/output.mpd", "fallback_url": "https://v.redd.it/video/DASH_720.mp4", "hls_url": "https://v.redd.it/missing.m3u8"}}
				if id == "prog123" {
					post["secure_media"] = map[string]any{"reddit_video": map[string]any{"dash_url": "https://v.redd.it/progressive.mpd", "hls_url": "https://v.redd.it/missing.m3u8", "fallback_url": "https://v.redd.it/video/DASH_720.mp4"}}
				}
				if id == "bad789" {
					post["secure_media"] = map[string]any{"reddit_video": map[string]any{"dash_url": "https://v.redd.it/broken/output.mpd", "fallback_url": "https://v.redd.it/video/DASH_720.mp4", "hls_url": "https://v.redd.it/missing.m3u8"}}
				}
			case "unsafe1":
				post["url"] = "http://i.redd.it/one.jpg"
			case "unsafe2":
				post["url"] = "https://i.redd.it:443/one.jpg"
			case "unsafe3":
				post["url"] = "https://user@i.redd.it/one.jpg"
			case "unsafe4":
				post["url"] = "https://i.redd.it/one.jpg#fragment"
			case "unsafe5":
				post["url"] = "https://i.redd.it/../one.jpg"
			case "unsafe6":
				post["url"] = "https://i.redd.it/one.svg"
			case "lnk123":
				post["url"] = "https://www.reddit.com/comments/nat789/"
			case "pre123":
				post["url"] = "https://preview.redd.it/one.jpg"
			case "ext123":
				post["url"] = "https://external.example/video"
			}
			if id == "crs789" {
				post["url"] = "https://www.reddit.com/comments/nat789/"
				post["crosspost_parent_list"] = []any{map[string]any{"secure_media": post["secure_media"]}}
				delete(post, "secure_media")
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]any{map[string]any{"data": map[string]any{"children": []any{map[string]any{"data": post}}}}, map[string]any{"data": map[string]any{"children": []any{}}}})
			return
		}
		if r.URL.Path == "/truncated.jpg" {
			data, err := os.ReadFile(filepath.Join(root, "one.jpg"))
			if err != nil {
				t.Error(err)
				return
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(data)+100))
			_, _ = w.Write(data)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/broken/") {
			if strings.Contains(r.URL.Path, "chunk-stream0-00002") {
				http.NotFound(w, r)
				return
			}
			r.URL.Path = strings.TrimPrefix(r.URL.Path, "/broken")
		}
		http.FileServer(http.Dir(root)).ServeHTTP(w, r)
	}))
	defer server.Close()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// Different originals expose gallery order and accidental duplicate downloads.
	second := image.NewRGBA(image.Rect(0, 0, 32, 32))
	second.Set(1, 1, color.RGBA{R: 255, A: 255})
	f, err = os.Create(filepath.Join(root, "two.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(f, second, nil); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	for _, tc := range []struct {
		id         string
		items      int
		media      string
		limit      int64
		nativeOnly bool
	}{

		{"abc123", 1, "image/jpeg", 1 << 20, false}, {"def456", 2, "image/jpeg", 1 << 20, false}, {"vid789", 1, "video/mp4", 1 << 20, false},
		{"ext123", 0, "", 1 << 20, false}, {"pre123", 0, "", 1 << 20, false}, {"bad789", 0, "", 1 << 20, false}, {"abc123", 0, "", 1, false}, {"vid789", 0, "", 1, false},
		{"abc123", 1, "image/jpeg", 1 << 20, true}, {"def456", 2, "image/jpeg", 1 << 20, true}, {"abc123", 0, "", 1, true}, {"pre123", 0, "", 1 << 20, true}, {"ext123", 0, "", 1 << 20, true},
		{"bad789", 0, "", 1 << 20, true},
		{"unsafe1", 0, "", 1 << 20, true}, {"unsafe2", 0, "", 1 << 20, true}, {"unsafe3", 0, "", 1 << 20, true}, {"unsafe4", 0, "", 1 << 20, true}, {"unsafe5", 0, "", 1 << 20, true}, {"unsafe6", 0, "", 1 << 20, true},
		{"prog123", 1, "video/mp4", 1 << 20, true}, {"prog123", 0, "", 1, true}, {"acc123", 0, "", 1 << 20, true}, {"proc123", 0, "", 1 << 20, true}, {"imgcut", 0, "", 1 << 20, true}, {"galmax", 0, "", 1 << 20, true}, {"galbad", 0, "", 1 << 20, true}, {"galext", 0, "", 1 << 20, true}, {"galmis", 0, "", 1 << 20, true},
		{"nat789", 1, "video/mp4", 1 << 20, true}, {"crs789", 1, "video/mp4", 1 << 20, true}, {"lnk123", 0, "", 1 << 20, true},
	} {
		t.Run(fmt.Sprintf("%s-%d", tc.id, tc.limit), func(t *testing.T) {
			proxy := "http://127.0.0.1:1"
			fixture, _ := json.Marshal(map[string]any{"base": server.URL, "proxy": proxy, "ffmpeg": ffmpeg, "max_bytes": tc.limit, "native_only": tc.nativeOnly})
			wrapper := filepath.Join(t.TempDir(), "gallery-fixture")
			text := fmt.Sprintf("#!%s\nimport json,runpy\nfixture=json.loads(%q)\nrunpy.run_path(%q,init_globals={'fixture':fixture},run_name='__main__')\n", python, string(fixture), filepath.Join(cwd, "reddit_tools_fixture.py"))
			if tc.nativeOnly {
				if err := os.WriteFile(filepath.Join(filepath.Dir(wrapper), "python"), []byte(text), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(wrapper, []byte(text), 0o700); err != nil {
				t.Fatal(err)
			}
			runner := extractor.NewRunner()
			media := extractor.NewMediaTools(ffprobe, ffmpeg, runner)
			work := t.TempDir()
			adapter := extractor.NewGalleryDL(wrapper, runner, media)
			request := extractor.Request{URL: "https://www.reddit.com/comments/" + tc.id + "/", Platform: "reddit", WorkDir: work, MaxBytes: tc.limit, MaxItems: 2}
			var files []extractor.File
			var err error
			if tc.nativeOnly {
				files, err = extractor.NewYTDLP(wrapper, ffmpeg, runner, media).ExtractRedditMedia(t.Context(), request, proxy)
			} else {
				files, err = adapter.Extract(t.Context(), request, proxy)
			}
			if tc.id == "acc123" {
				var failure *extractor.Failure
				if !errors.As(err, &failure) || failure.Detail != job.DetailLoginRequired {
					t.Fatalf("access failure = %v", err)
				}
				for _, line := range failure.Diagnostics {
					if strings.Contains(line, tc.id) {
						t.Fatal("post identifier leaked through Python traceback")
					}
				}
				return
			}
			if tc.id == "proc123" {
				var failure *extractor.Failure
				if !errors.As(err, &failure) || failure.Detail != job.DetailUnavailable {
					t.Fatalf("processing native video = %v", err)
				}
				return
			}
			if strings.HasPrefix(tc.id, "gal") || strings.HasPrefix(tc.id, "unsafe") || tc.id == "imgcut" {
				if err == nil || len(files) != 0 {
					t.Fatalf("incomplete or unsafe gallery accepted: %v, %v", files, err)
				}
				if tc.id == "galmax" && !errors.Is(err, extractor.ErrTooLarge) {
					t.Fatalf("gallery item limit = %v", err)
				}
				return
			}
			if tc.id == "bad789" {
				if err == nil || len(files) != 0 {
					t.Fatalf("incomplete video accepted: files=%v, err=%v", files, err)
				}
				return
			}
			if tc.limit == 1 {
				if tc.id == "vid789" {
					var fail *extractor.Failure
					if !errors.As(err, &fail) || fail.Detail != job.DetailToolError || len(files) != 0 {
						t.Fatalf("fragment size limit = %v", err)
					}
				} else if !errors.Is(err, extractor.ErrTooLarge) {
					t.Fatalf("size limit = %v", err)
				}
				return
			}
			if tc.items == 0 {
				var fail *extractor.Failure
				if !errors.As(err, &fail) || fail.Detail != job.DetailNoMedia {
					t.Fatalf("external post = %v", err)
				}
				return
			}
			if err != nil {
				var fail *extractor.Failure
				errors.As(err, &fail)
				t.Fatalf("extract = %v; safe diagnostics %#v", err, fail)
			}
			if len(files) != tc.items {
				t.Fatalf("items=%d; want %d", len(files), tc.items)
			}
			for index, file := range files {
				if tc.nativeOnly && tc.media == "image/jpeg" {
					stat, err := os.Stat(filepath.Join(work, file.Name))
					if err != nil || stat.Mode().Perm() != 0o600 {
						t.Fatal("native original is not private")
					}
				}
				if file.MediaType != tc.media {
					t.Fatalf("type=%s", file.MediaType)
				}
				if tc.id == "def456" {
					original := "one.jpg"
					if index == 1 {
						original = "two.jpg"
					}
					want, err := os.ReadFile(filepath.Join(root, original))
					if err != nil {
						t.Fatal(err)
					}
					got, err := os.ReadFile(filepath.Join(work, file.Name))
					if err != nil || !bytes.Equal(got, want) {
						t.Fatal("gallery original or order changed")
					}
				}
				inspection, err := extractor.NewProbe(ffprobe, runner).Inspect(t.Context(), work, file.Name)
				if err != nil {
					t.Fatal(err)
				}
				if tc.media == "video/mp4" && (inspection.VideoCodec != "h264" || inspection.AudioCodec != "aac") {
					t.Fatalf("streams=%+v", inspection)
				}
			}
		})
	}
}
