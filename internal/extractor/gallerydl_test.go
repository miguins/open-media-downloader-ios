package extractor

import (
	"errors"
	"os"
	"path/filepath"
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
