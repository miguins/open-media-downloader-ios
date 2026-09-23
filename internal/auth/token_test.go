package auth

import (
	"bytes"
	"strings"
	"testing"
)

func TestDownloadTokenRoundTrip(t *testing.T) {
	plaintext, hash := NewDownloadToken()
	if len(plaintext) != encodedSecretLength || strings.ContainsAny(plaintext, "+/=") {
		t.Fatalf("NewDownloadToken() plaintext has unexpected shape (length %d)", len(plaintext))
	}
	parsed, ok := HashDownloadToken(plaintext)
	if !ok || !bytes.Equal(parsed, hash) {
		t.Fatal("HashDownloadToken() does not match the generated hash")
	}
	if bytes.Contains(hash, []byte(plaintext)) {
		t.Fatal("hash contains the token")
	}
	other, _ := NewDownloadToken()
	if other == plaintext {
		t.Fatal("NewDownloadToken() repeated a token")
	}
}

func TestHashDownloadTokenRejectsMalformed(t *testing.T) {
	valid, _ := NewDownloadToken()
	for name, value := range map[string]string{
		"empty":         "",
		"short":         valid[:len(valid)-1],
		"long":          valid + "A",
		"invalid chars": strings.Repeat("*", encodedSecretLength),
		"padded":        valid[:len(valid)-1] + "=",
		"non-canonical": valid[:len(valid)-1] + flipUnusedBit(valid[len(valid)-1:]),
		"api key":       "omdi_" + valid,
	} {
		if _, ok := HashDownloadToken(value); ok {
			t.Fatalf("HashDownloadToken(%s) ok = true", name)
		}
	}
}
