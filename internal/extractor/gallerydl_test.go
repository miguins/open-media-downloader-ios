package extractor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGalleryDLMaxPlusOne(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"item-001.jpg", "item-002.jpg"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := discoverGallery(dir, 1); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("error = %v", err)
	}
}

func TestGalleryDLSingleMediaNumberedZero(t *testing.T) {
	dir := t.TempDir()
	regular(t, dir, "item-000.jpg")
	if names, err := discoverGallery(dir, 1); err != nil || len(names) != 1 || names[0] != "item-000.jpg" {
		t.Fatalf("names = %v, error = %v", names, err)
	}
	regular(t, dir, "item-001.jpg")
	if _, err := discoverGallery(dir, 2); !errors.Is(err, ErrExtractionFailed) {
		t.Fatalf("mixed numbering error = %v", err)
	}
}

func TestGalleryDLArgumentsAndOversizedSkip(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	tool := testTool(t, `printf '%s\n' "$@" > '`+argsFile+`'
printf '%s\n' '[downloader.http][warning] File size larger than allowed maximum (2 > 1)' >&2`)
	media := NewMediaTools(probeTool(t, probeJSON("jpeg_pipe", "mjpeg", "")), "/bin/true", NewRunner())
	if _, err := NewGalleryDL(tool, NewRunner(), media).Extract(t.Context(), adapterRequest(t.TempDir(), "x", 1), "http://127.0.0.1:8080"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("error = %v", err)
	}
	raw, err := os.ReadFile(argsFile) //nolint:gosec // Path is under the test's temporary directory.
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "extractor.whitelist=[]\n") {
		t.Fatalf("child extractors are not disabled: %q", raw)
	}
}
