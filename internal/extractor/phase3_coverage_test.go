package extractor

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/urlpolicy"
)

func testTool(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func probeJSON(format, video, audio string) string {
	streams := ""
	if video != "" {
		streams += `{"codec_type":"video","codec_name":"` + video + `"}`
	}
	if audio != "" {
		if streams != "" {
			streams += ","
		}
		streams += `{"codec_type":"audio","codec_name":"` + audio + `"}`
	}
	return `{"streams":[` + streams + `],"format":{"format_name":"` + format + `","tags":{}}}`
}

func probeTool(t *testing.T, document string) string {
	return testTool(t, "printf '%s' '"+document+"'")
}

func regular(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestProbeInspectMatrix(t *testing.T) {
	dir := t.TempDir()
	regular(t, dir, "media.bin")
	good := NewProbe(probeTool(t, probeJSON("mp4", "h264", "aac")), NewRunner())
	if got, err := good.Inspect(t.Context(), dir, "media.bin"); err != nil || got.MediaType != "video/mp4" {
		t.Fatalf("Inspect = %#v, %v", got, err)
	}
	for _, test := range []struct{ name, path string }{
		{"empty", "/bin/true"}, {"exit", "/bin/false"},
		{"malformed", probeTool(t, "{")}, {"trailing", probeTool(t, probeJSON("mp4", "h264", "aac")+" {}")},
		{"unsupported", probeTool(t, probeJSON("webm", "vp9", "opus"))},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewProbe(test.path, NewRunner()).Inspect(t.Context(), dir, "media.bin"); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	if _, err := good.Inspect(t.Context(), dir, "missing"); err == nil {
		t.Fatal("missing accepted")
	}
	if _, err := good.Inspect(t.Context(), dir, "../bad"); err == nil {
		t.Fatal("bad name accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := good.Inspect(ctx, dir, "media.bin"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
	missing := NewProbe(filepath.Join(dir, "missing-tool"), NewRunner())
	if _, err := missing.Inspect(t.Context(), dir, "media.bin"); err == nil || errors.Is(err, ErrExtractionFailed) {
		t.Fatalf("launch = %v", err)
	}
}

func TestProbeAllClassifications(t *testing.T) {
	tests := []struct {
		format, video, audio, media string
		remux                       bool
		brand                       string
	}{
		{"mov,mp4,m4a,3gp,3g2,mj2", "h264", "aac", "video/mp4", false, ""},
		{"mov", "h264", "", "video/quicktime", false, "qt  "}, {"m4a", "", "aac", "audio/mp4", false, ""},
		{"mp3", "", "mp3", "audio/mpeg", false, ""}, {"jpeg_pipe", "mjpeg", "", "image/jpeg", false, ""},
		{"png_pipe", "png", "", "image/png", false, ""}, {"webp_pipe", "webp", "", "image/webp", false, ""},
		{"gif", "gif", "", "image/gif", false, ""}, {"mpegts", "h264", "aac", "video/mp2t", true, ""},
	}
	for _, tt := range tests {
		var d probeDocument
		d.Format.FormatName = tt.format
		d.Format.Tags.MajorBrand = tt.brand
		if tt.video != "" {
			d.Streams = append(d.Streams, struct {
				CodecType string `json:"codec_type"`
				CodecName string `json:"codec_name"`
			}{"video", tt.video})
		}
		if tt.audio != "" {
			d.Streams = append(d.Streams, struct {
				CodecType string `json:"codec_type"`
				CodecName string `json:"codec_name"`
			}{"audio", tt.audio})
		}
		got, ok := classifyProbe("x", d)
		if !ok || got.MediaType != tt.media || got.NeedsRemux != tt.remux {
			t.Errorf("%s = %#v, %v", tt.format, got, ok)
		}
	}
	var duplicate probeDocument
	duplicate.Format.FormatName = "mp4"
	duplicate.Streams = append(duplicate.Streams, struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
	}{"video", "h264"}, struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
	}{"video", "h264"})
	if _, ok := classifyProbe("x", duplicate); ok {
		t.Fatal("duplicate stream accepted")
	}
	duplicate.Streams = []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
	}{{"audio", "aac"}, {"audio", "aac"}}
	if _, ok := classifyProbe("x", duplicate); ok {
		t.Fatal("duplicate audio accepted")
	}
	for _, typ := range []string{"subtitle", "data"} {
		d := probeDocument{}
		d.Format.FormatName = "mp4"
		d.Streams = append(d.Streams, struct {
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
		}{typ, "x"})
		if _, ok := classifyProbe("x", d); ok {
			t.Fatal("extra stream accepted")
		}
	}
	if _, ok := classifyProbe("x", probeDocument{}); ok {
		t.Fatal("empty accepted")
	}
}

func TestMediaToolsPassThroughAndRemux(t *testing.T) {
	dir := t.TempDir()
	regular(t, dir, "input.mp4")
	m := NewMediaTools(probeTool(t, probeJSON("mp4", "h264", "aac")), "/bin/false", NewRunner())
	files, err := m.Finalize(t.Context(), dir, []string{"input.mp4"})
	if err != nil || len(files) != 1 || files[0].MediaType != "video/mp4" {
		t.Fatalf("Finalize = %#v, %v", files, err)
	}
	if _, err := m.Finalize(t.Context(), dir, []string{"input.mp4", "input.mp4"}); err == nil {
		t.Fatal("duplicate accepted")
	}

	dir = t.TempDir()
	regular(t, dir, "input.ts")
	probe := testTool(t, `last=""; for arg in "$@"; do last="$arg"; done; case "$last" in *.mp4) printf '%s' '`+probeJSON("mp4", "h264", "aac")+`';; *) printf '%s' '`+probeJSON("mpegts", "h264", "aac")+`';; esac`)
	ffmpeg := testTool(t, `last=""; for arg in "$@"; do last="$arg"; done; printf media > "$last"`)
	m = NewMediaTools(probe, ffmpeg, NewRunner())
	files, err = m.Finalize(t.Context(), dir, []string{"input.ts"})
	if err != nil || files[0].Name != "media-001.mp4" {
		t.Fatalf("remux = %#v, %v", files, err)
	}
}

func TestMediaToolsFailureMatrix(t *testing.T) {
	dir := t.TempDir()
	regular(t, dir, "bad.bin")
	bad := NewMediaTools(probeTool(t, probeJSON("webm", "vp9", "opus")), "/bin/false", NewRunner())
	if _, err := bad.Finalize(t.Context(), dir, []string{"bad.bin"}); err == nil {
		t.Fatal("bad probe accepted")
	}
	input := Inspection{Name: "input.ts", NeedsRemux: true}
	regular(t, dir, input.Name)
	goodProbe := probeTool(t, probeJSON("mp4", "h264", "aac"))
	for _, name := range []string{".omdi-remux-001.mp4", "media-001.mp4"} {
		regular(t, dir, name)
		m := NewMediaTools(goodProbe, "/bin/true", NewRunner())
		if _, err := m.remux(t.Context(), dir, input, 1); err == nil {
			t.Fatalf("existing %s accepted", name)
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
	m := NewMediaTools(goodProbe, "/bin/false", NewRunner())
	if _, err := m.remux(t.Context(), dir, input, 1); !errors.Is(err, ErrExtractionFailed) {
		t.Fatalf("exit = %v", err)
	}
	tsMedia := NewMediaTools(probeTool(t, probeJSON("mpegts", "h264", "aac")), "/bin/false", NewRunner())
	if _, err := tsMedia.Finalize(t.Context(), dir, []string{input.Name}); !errors.Is(err, ErrExtractionFailed) {
		t.Fatalf("finalize remux = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.remux(ctx, dir, input, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
	m = NewMediaTools(goodProbe, filepath.Join(dir, "missing-tool"), NewRunner())
	if _, err := m.remux(t.Context(), dir, input, 1); err == nil || errors.Is(err, ErrExtractionFailed) {
		t.Fatalf("launch = %v", err)
	}
	writer := testTool(t, `last=""; for arg in "$@"; do last="$arg"; done; printf media > "$last"`)
	m = NewMediaTools(probeTool(t, probeJSON("webm", "vp9", "opus")), writer, NewRunner())
	if _, err := m.remux(t.Context(), dir, input, 1); !errors.Is(err, ErrExtractionFailed) {
		t.Fatalf("reprobe = %v", err)
	}
	m = NewMediaTools(probeTool(t, probeJSON("m4a", "", "aac")), writer, NewRunner())
	if _, err := m.remux(t.Context(), dir, input, 1); !errors.Is(err, ErrExtractionFailed) {
		t.Fatalf("non-video = %v", err)
	}
	m = NewMediaTools(goodProbe, writer, NewRunner())
	if _, err := m.remux(t.Context(), dir, Inspection{Name: "missing.ts"}, 1); err == nil {
		t.Fatal("missing original accepted")
	}
	regular(t, dir, input.Name)
	collision := testTool(t, `last=""; for arg in "$@"; do last="$arg"; done; printf media > "$last"; printf collision > "$(dirname "$last")/media-001.mp4"`)
	m = NewMediaTools(goodProbe, collision, NewRunner())
	if _, err := m.remux(t.Context(), dir, input, 1); err == nil {
		t.Fatal("rename collision accepted")
	}
	_ = os.Remove(filepath.Join(dir, "media-001.mp4"))
	regular(t, dir, input.Name)
	vanishProbe := testTool(t, `last=""; for arg in "$@"; do last="$arg"; done; rm -f "$last"; printf '%s' '`+probeJSON("mp4", "h264", "aac")+`'`)
	m = NewMediaTools(vanishProbe, writer, NewRunner())
	if _, err := m.remux(t.Context(), dir, input, 1); err == nil {
		t.Fatal("vanished output accepted")
	}
}

func adapterRequest(dir, platform string, maxItems int) Request {
	return Request{URL: "https://example.invalid/post", Platform: platform, WorkDir: dir, MaxBytes: 1024, MaxItems: maxItems}
}

func TestYTDLPAdapterMatrix(t *testing.T) {
	dir := t.TempDir()
	regular(t, dir, "media.mp4")
	media := NewMediaTools(probeTool(t, probeJSON("mp4", "h264", "aac")), "/bin/true", NewRunner())
	y := NewYTDLP("/bin/true", "/bin/true", NewRunner(), media)
	if files, err := y.Extract(t.Context(), adapterRequest(dir, "youtube", 1), "http://127.0.0.1:8080"); err != nil || len(files) != 1 {
		t.Fatalf("Extract = %#v, %v", files, err)
	}
	if _, err := y.Extract(t.Context(), adapterRequest(t.TempDir(), "bad", 1), "http://127.0.0.1:8080"); err == nil {
		t.Fatal("invalid accepted")
	}
	if _, err := NewYTDLP("/bin/false", "/bin/true", NewRunner(), media).Extract(t.Context(), adapterRequest(t.TempDir(), "vimeo", 1), "http://127.0.0.1:8080"); !errors.Is(err, ErrExtractionFailed) {
		t.Fatalf("exit = %v", err)
	}
	if _, err := y.Extract(t.Context(), adapterRequest(t.TempDir(), "tiktok", 1), "http://127.0.0.1:8080"); !errors.Is(err, ErrExtractionFailed) {
		t.Fatalf("empty = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := y.Extract(ctx, adapterRequest(t.TempDir(), "youtube", 1), "http://127.0.0.1:8080"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
	dir = t.TempDir()
	regular(t, dir, "media.bin")
	unsupported := NewMediaTools(probeTool(t, probeJSON("webm", "vp9", "opus")), "/bin/true", NewRunner())
	if _, err := NewYTDLP("/bin/true", "/bin/true", NewRunner(), unsupported).Extract(t.Context(), adapterRequest(dir, "youtube", 1), "http://127.0.0.1:8080"); !errors.Is(err, ErrExtractionFailed) {
		t.Fatalf("media = %v", err)
	}
	dir = t.TempDir()
	regular(t, dir, "media.bin")
	internalMedia := NewMediaTools(filepath.Join(dir, "missing-probe"), "/bin/true", NewRunner())
	if _, err := NewYTDLP("/bin/true", "/bin/true", NewRunner(), internalMedia).Extract(t.Context(), adapterRequest(dir, "youtube", 1), "http://127.0.0.1:8080"); err == nil || errors.Is(err, ErrExtractionFailed) {
		t.Fatalf("internal = %v", err)
	}
	dir = t.TempDir()
	regular(t, dir, "media.bin")
	slowMedia := NewMediaTools(testTool(t, "sleep 1"), "/bin/true", NewRunner())
	slowY := NewYTDLP("/bin/true", "/bin/true", NewRunner(), slowMedia)
	timed, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	if _, err := slowY.Extract(timed, adapterRequest(dir, "youtube", 1), "http://127.0.0.1:8080"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("media timeout = %v", err)
	}
}

func TestDiscoveryAndAdapterErrors(t *testing.T) {
	dir := t.TempDir()
	regular(t, dir, "media.mp4")
	if names, err := discoverYTDLP(dir); err != nil || len(names) != 1 {
		t.Fatal(names, err)
	}
	regular(t, dir, "extra")
	if _, err := discoverYTDLP(dir); err == nil {
		t.Fatal("extra accepted")
	}
	if _, err := discoverYTDLP(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing accepted")
	}
	for _, raw := range []string{"%", "https://127.0.0.1:2", "http://localhost:2", "http://127.0.0.1:0", "http://127.0.0.1:2/path"} {
		if validProxyURL(raw) {
			t.Errorf("proxy accepted %q", raw)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(adapterCommandError(ctx, errors.New("x")), context.Canceled) {
		t.Fatal("cancel lost")
	}
	if !errors.Is(adapterCommandError(context.Background(), ErrOutputLimit), ErrExtractionFailed) {
		t.Fatal("limit not mapped")
	}
	internal := errors.New("internal")
	if adapterCommandError(context.Background(), internal) != internal {
		t.Fatal("internal changed")
	}
	if !errors.Is(adapterCommandError(context.Background(), context.DeadlineExceeded), context.DeadlineExceeded) {
		t.Fatal("deadline changed")
	}
	if secureRegular(filepath.Join(dir, "missing")) {
		t.Fatal("missing regular")
	}
	symlinkDir := t.TempDir()
	regular(t, symlinkDir, "target")
	if err := os.Symlink("target", filepath.Join(symlinkDir, "media.mp4")); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverYTDLP(symlinkDir); !errors.Is(err, ErrExtractionFailed) {
		t.Fatalf("symlink = %v", err)
	}
}

func TestGalleryAdapterAndDiscovery(t *testing.T) {
	dir := t.TempDir()
	regular(t, dir, "item-001.jpg")
	media := NewMediaTools(probeTool(t, probeJSON("jpeg_pipe", "mjpeg", "")), "/bin/true", NewRunner())
	g := NewGalleryDL("/bin/true", NewRunner(), media)
	if files, err := g.Extract(t.Context(), adapterRequest(dir, "instagram", 1), "http://127.0.0.1:8080"); err != nil || len(files) != 1 {
		t.Fatalf("Extract = %#v, %v", files, err)
	}
	if _, err := g.Extract(t.Context(), adapterRequest(t.TempDir(), "bad", 1), "http://127.0.0.1:8080"); err == nil {
		t.Fatal("invalid accepted")
	}
	if _, err := NewGalleryDL("/bin/false", NewRunner(), media).Extract(t.Context(), adapterRequest(t.TempDir(), "x", 1), "http://127.0.0.1:8080"); !errors.Is(err, ErrExtractionFailed) {
		t.Fatalf("exit = %v", err)
	}
	dir = t.TempDir()
	regular(t, dir, "item-002.jpg")
	if _, err := discoverGallery(dir, 2); !errors.Is(err, ErrExtractionFailed) {
		t.Fatalf("gap = %v", err)
	}
	if _, err := discoverGallery(filepath.Join(dir, "missing"), 2); err == nil {
		t.Fatal("missing accepted")
	}
	if _, err := discoverGallery(t.TempDir(), 2); !errors.Is(err, ErrExtractionFailed) {
		t.Fatalf("empty = %v", err)
	}
	dir = t.TempDir()
	regular(t, dir, "item-001.jpg")
	regular(t, dir, "item-002.jpg")
	if names, err := discoverGallery(dir, 2); err != nil || len(names) != 2 {
		t.Fatalf("ordered = %v, %v", names, err)
	}
	dir = t.TempDir()
	regular(t, dir, "unexpected")
	if _, err := discoverGallery(dir, 2); !errors.Is(err, ErrExtractionFailed) {
		t.Fatalf("hostile = %v", err)
	}
	if _, err := g.Extract(t.Context(), adapterRequest(t.TempDir(), "reddit", 1), "http://127.0.0.1:8080"); !errors.Is(err, ErrExtractionFailed) {
		t.Fatalf("discovery = %v", err)
	}
	dir = t.TempDir()
	regular(t, dir, "item-001.bin")
	unsupported := NewMediaTools(probeTool(t, probeJSON("webm", "vp9", "opus")), "/bin/true", NewRunner())
	if _, err := NewGalleryDL("/bin/true", NewRunner(), unsupported).Extract(t.Context(), adapterRequest(dir, "x", 1), "http://127.0.0.1:8080"); !errors.Is(err, ErrExtractionFailed) {
		t.Fatalf("media = %v", err)
	}
	dir = t.TempDir()
	regular(t, dir, "item-001.bin")
	internalMedia := NewMediaTools(filepath.Join(dir, "missing-probe"), "/bin/true", NewRunner())
	if _, err := NewGalleryDL("/bin/true", NewRunner(), internalMedia).Extract(t.Context(), adapterRequest(dir, "x", 1), "http://127.0.0.1:8080"); err == nil || errors.Is(err, ErrExtractionFailed) {
		t.Fatalf("internal = %v", err)
	}
	dir = t.TempDir()
	regular(t, dir, "item-001.bin")
	slowMedia := NewMediaTools(testTool(t, "sleep 1"), "/bin/true", NewRunner())
	slowG := NewGalleryDL("/bin/true", NewRunner(), slowMedia)
	timed, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	if _, err := slowG.Extract(timed, adapterRequest(dir, "x", 1), "http://127.0.0.1:8080"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("media timeout = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	if _, err := g.Extract(ctx, adapterRequest(t.TempDir(), "reddit", 1), "http://127.0.0.1:8080"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline = %v", err)
	}
}

type scriptedSession struct {
	err    error
	closed *bool
}

func (s scriptedSession) URL() string { return "http://127.0.0.1:1" }
func (s scriptedSession) Err() error  { return s.err }
func (s scriptedSession) Close() error {
	if s.closed != nil {
		*s.closed = true
	}
	return nil
}

func TestRealMatrix(t *testing.T) {
	closed := false
	gallery := false
	r := &Real{beginSession: func(context.Context, int64) (proxySession, error) { return scriptedSession{closed: &closed}, nil }, gallery: func(context.Context, Request, string) ([]File, error) {
		gallery = true
		return nil, ErrExtractionFailed
	}}
	_, err := r.Extract(t.Context(), adapterRequest(t.TempDir(), "reddit", 1))
	if !errors.Is(err, ErrExtractionFailed) || !gallery || !closed {
		t.Fatalf("route = %v %v %v", err, gallery, closed)
	}
	r.beginSession = func(context.Context, int64) (proxySession, error) {
		return scriptedSession{err: urlpolicy.ErrEgressTooLarge}, nil
	}
	_, err = r.Extract(t.Context(), adapterRequest(t.TempDir(), "reddit", 1))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("budget = %v", err)
	}
	r.beginSession = func(context.Context, int64) (proxySession, error) { return nil, errors.New("busy") }
	if _, err = r.Extract(t.Context(), adapterRequest(t.TempDir(), "youtube", 1)); err == nil {
		t.Fatal("begin accepted")
	}
	if _, err = r.Extract(t.Context(), Request{}); err == nil {
		t.Fatal("invalid accepted")
	}
	if _, err = (&Real{beginSession: func(context.Context, int64) (proxySession, error) { return scriptedSession{}, nil }}).Extract(t.Context(), adapterRequest(t.TempDir(), "unknown", 1)); err == nil {
		t.Fatal("unknown accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r = &Real{beginSession: func(context.Context, int64) (proxySession, error) { return scriptedSession{}, nil }, ytdlp: func(context.Context, Request, string) ([]File, error) { return nil, nil }}
	if _, err = r.Extract(ctx, adapterRequest(t.TempDir(), "youtube", 1)); !errors.Is(err, context.Canceled) {
		t.Fatalf("context = %v", err)
	}
}

func TestNewReal(t *testing.T) {
	// Constructor wiring is covered without starting the proxy server.
	p, err := urlpolicy.NewProxy(netResolver{}, netDialer{})
	if err != nil {
		t.Fatal(err)
	}
	r := NewReal(p, &YTDLP{}, &GalleryDL{})
	r.ytdlp = func(context.Context, Request, string) ([]File, error) { return nil, nil }
	if _, err := r.Extract(t.Context(), adapterRequest(t.TempDir(), "youtube", 1)); err != nil {
		t.Fatal(err)
	}
}

type netResolver struct{}

func (netResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return nil, errors.New("unused")
}

type netDialer struct{}

func (netDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, errors.New("unused")
}
