package auth

import (
	"crypto/rand"
	"crypto/sha256"
)

// NewDownloadToken returns an opaque download token, which is handed to the client and never stored,
// and the SHA-256 hash to persist.
func NewDownloadToken() (string, []byte) {
	secret := make([]byte, secretBytes)
	_, _ = rand.Read(secret) // crypto/rand.Read never returns an error.
	hash := sha256.Sum256(secret)

	return secretEncoding.EncodeToString(secret), hash[:]
}

// HashDownloadToken parses a token in the exact format produced by NewDownloadToken and returns its hash.
func HashDownloadToken(value string) ([]byte, bool) {
	if len(value) != encodedSecretLength {
		return nil, false
	}
	secret, err := secretEncoding.DecodeString(value)
	if err != nil {
		return nil, false
	}
	hash := sha256.Sum256(secret)

	return hash[:], true
}
