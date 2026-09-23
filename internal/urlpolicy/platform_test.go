package urlpolicy

import (
	"reflect"
	"testing"
)

func TestPlatformIDs(t *testing.T) {
	want := []string{"youtube", "instagram", "tiktok", "x", "reddit", "vimeo"}
	if got := PlatformIDs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("PlatformIDs() = %v; want %v", got, want)
	}
}

func TestIsPlatform(t *testing.T) {
	for _, id := range PlatformIDs() {
		if !IsPlatform(id) {
			t.Fatalf("IsPlatform(%q) = false", id)
		}
	}
	for _, id := range []string{"", "YouTube", "facebook", "youtube.com"} {
		if IsPlatform(id) {
			t.Fatalf("IsPlatform(%q) = true", id)
		}
	}
}
