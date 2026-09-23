package auth

import (
	"bytes"
	"crypto/sha256"
	"strings"
	"testing"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/id"
)

var now = time.UnixMilli(1_800_000_000_000).UTC()

func TestGenerate(t *testing.T) {
	plaintext, record := Generate("phone", now)

	if len(plaintext) != keyLength || !strings.HasPrefix(plaintext, "omdi_") {
		t.Fatalf("Generate() plaintext has unexpected shape (length %d)", len(plaintext))
	}
	if !id.Valid(record.ID) || record.Name != "phone" || !record.CreatedAt.Equal(now) {
		t.Fatalf("Generate() record = %#v", record)
	}
	keyID, secret, ok := Parse(plaintext)
	if !ok || keyID != record.ID {
		t.Fatalf("Parse(Generate()) = %q, ok=%v", keyID, ok)
	}
	hash := sha256.Sum256(secret)
	if !bytes.Equal(record.SecretHash, hash[:]) {
		t.Fatal("record hash does not match the secret")
	}
	if bytes.Contains(record.SecretHash, secret) || strings.Contains(string(record.SecretHash), plaintext) {
		t.Fatal("record stores the secret")
	}

	other, _ := Generate("phone", now)
	if other == plaintext {
		t.Fatal("Generate() repeated a key")
	}
}

func TestParseRejectsMalformedKeys(t *testing.T) {
	valid, _ := Generate("phone", now)
	tests := map[string]string{
		"empty":             "",
		"missing prefix":    "xxxx" + valid[4:],
		"short":             valid[:len(valid)-1],
		"long":              valid + "A",
		"invalid id":        "omdi_" + strings.Repeat("A", 26) + valid[31:],
		"missing separator": valid[:31] + "-" + valid[32:],
		"invalid secret":    valid[:32] + strings.Repeat("*", 43),
		"non-canonical":     valid[:len(valid)-1] + flipUnusedBit(valid[len(valid)-1:]),
	}
	for name, value := range tests {
		t.Run(name, func(t *testing.T) {
			if _, _, ok := Parse(value); ok {
				t.Fatalf("Parse(%s) ok = true", name)
			}
		})
	}
}

func TestValidKeyName(t *testing.T) {
	for _, name := range []string{"a", "phone", "Shortcut-1.home_2", strings.Repeat("n", 64)} {
		if !ValidKeyName(name) {
			t.Fatalf("ValidKeyName(%q) = false", name)
		}
	}
	for _, name := range []string{"", strings.Repeat("n", 65), "with space", "semi;colon", "ümlaut", "new\nline"} {
		if ValidKeyName(name) {
			t.Fatalf("ValidKeyName(%q) = true", name)
		}
	}
}

// flipUnusedBit returns the base64url character whose value differs from last only in
// the lowest bit, which a 32-byte secret leaves unused.
func flipUnusedBit(last string) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	index := strings.Index(alphabet, last)

	return string(alphabet[index^1])
}
