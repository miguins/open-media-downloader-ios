package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/store"
)

// RestoreAPIKey inserts a configured credential if absent. Existing records,
// including revocations, are preserved; a different secret for the same ID fails.
func RestoreAPIKey(ctx context.Context, st *store.Store, plaintext string, now time.Time) error {
	keyID, secret, ok := Parse(plaintext)
	if !ok {
		return errors.New("auth: invalid restoration key")
	}
	hash := sha256.Sum256(secret)
	record, err := st.APIKey(ctx, keyID)
	switch {
	case err == nil:
		if subtle.ConstantTimeCompare(record.SecretHash, hash[:]) != 1 {
			return errors.New("auth: restoration key conflicts with stored credential")
		}
		return nil
	case errors.Is(err, store.ErrNotFound):
		if err := st.CreateAPIKey(ctx, store.APIKey{
			ID: keyID, Name: "startup-" + keyID, SecretHash: hash[:], CreatedAt: now,
		}); err != nil {
			return errors.New("auth: could not store restoration key")
		}
		return nil
	default:
		return errors.New("auth: could not look up restoration key")
	}
}
