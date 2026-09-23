package id

import "testing"

func TestNewProducesUniqueValidIDs(t *testing.T) {
	seen := make(map[string]bool)
	for range 1000 {
		value := New()
		if !Valid(value) {
			t.Fatalf("New() = %q is not valid", value)
		}
		if seen[value] {
			t.Fatalf("New() repeated %q", value)
		}
		seen[value] = true
	}
}

func TestValid(t *testing.T) {
	tests := map[string]bool{
		"abcdefghijklmnopqrstuvwxyz":  true,
		"234567abcdefghijklmnopqrst":  true,
		"":                            false,
		"abcdefghijklmnopqrstuvwxy":   false,
		"abcdefghijklmnopqrstuvwxyza": false,
		"ABCDEFGHIJKLMNOPQRSTUVWXYZ":  false,
		"abcdefghijklmnopqrstuvwxy1":  false,
		"abcdefghijklmnopqrstuvwxy_":  false,
	}
	for value, want := range tests {
		if got := Valid(value); got != want {
			t.Fatalf("Valid(%q) = %v; want %v", value, got, want)
		}
	}
}
