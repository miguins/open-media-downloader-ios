// Package id generates and validates opaque random record identifiers.
package id

import (
	"crypto/rand"
	"strings"
)

const length = 26

// New returns a 128-bit random identifier encoded as lowercase unpadded base32.
func New() string {
	return strings.ToLower(rand.Text())
}

// Valid reports whether value has the identifier format produced by New.
func Valid(value string) bool {
	if len(value) != length {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < '2' || r > '7') {
			return false
		}
	}

	return true
}
