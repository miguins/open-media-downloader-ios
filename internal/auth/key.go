// Package auth generates, verifies, and enforces API keys.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/id"
	"github.com/miguins/open-media-downloader-ios/internal/store"
)

const (
	keyPrefix           = "omdi_"
	idLength            = 26
	secretBytes         = 32
	encodedSecretLength = 43
	keyLength           = len(keyPrefix) + idLength + 1 + encodedSecretLength
	maxKeyNameLength    = 64
)

var secretEncoding = base64.RawURLEncoding.Strict()

// Generate creates a new API key named name. It returns the plaintext key, which must be
// shown once and never stored, and the record to persist, which holds only a secret hash.
func Generate(name string, now time.Time) (string, store.APIKey) {
	keyID := id.New()
	secret := make([]byte, secretBytes)
	_, _ = rand.Read(secret) // crypto/rand.Read never returns an error.
	hash := sha256.Sum256(secret)

	plaintext := keyPrefix + keyID + "_" + secretEncoding.EncodeToString(secret)

	return plaintext, store.APIKey{ID: keyID, Name: name, SecretHash: hash[:], CreatedAt: now}
}

// Parse splits a plaintext key into its ID and decoded secret, accepting only the exact
// format produced by Generate.
func Parse(value string) (string, []byte, bool) {
	if len(value) != keyLength || value[:len(keyPrefix)] != keyPrefix {
		return "", nil, false
	}
	keyID := value[len(keyPrefix) : len(keyPrefix)+idLength]
	separator := value[len(keyPrefix)+idLength]
	if !id.Valid(keyID) || separator != '_' {
		return "", nil, false
	}
	secret, err := secretEncoding.DecodeString(value[len(keyPrefix)+idLength+1:])
	if err != nil {
		return "", nil, false
	}

	return keyID, secret, true
}

// ValidKeyName reports whether name is 1–64 characters of letters, digits, '.', '_', or '-'.
func ValidKeyName(name string) bool {
	if name == "" || len(name) > maxKeyNameLength {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
		default:
			return false
		}
	}

	return true
}
