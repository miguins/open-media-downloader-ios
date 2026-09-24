package extractor

import (
	"errors"
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
