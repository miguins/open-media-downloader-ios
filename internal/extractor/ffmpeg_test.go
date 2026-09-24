package extractor

import "testing"

func TestMediaToolsRejectsEmptyInput(t *testing.T) {
	m := NewMediaTools("/bin/false", "/bin/false", NewRunner())
	if _, err := m.Finalize(t.Context(), t.TempDir(), nil); err == nil {
		t.Fatal("Finalize accepted no files")
	}
}
