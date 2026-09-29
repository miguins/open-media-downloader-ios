package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/store"
)

// RestoreAPIKey inserts a configured credential if absent and returns its record and whether it was
// inserted. Existing records, including revocations, are preserved; a different secret for the same ID fails.
func RestoreAPIKey(ctx context.Context, st *store.Store, plaintext string, now time.Time) (store.APIKey, bool, error) {
	keyID, secret, ok := Parse(plaintext)
	if !ok {
		return store.APIKey{}, false, errors.New("auth: invalid restoration key")
	}
	hash := sha256.Sum256(secret)
	record, err := st.APIKey(ctx, keyID)
	switch {
	case err == nil:
		if subtle.ConstantTimeCompare(record.SecretHash, hash[:]) != 1 {
			return store.APIKey{}, false, errors.New("auth: restoration key conflicts with stored credential")
		}
		return record, false, nil
	case errors.Is(err, store.ErrNotFound):
		record = store.APIKey{ID: keyID, Name: "startup-" + keyID, SecretHash: hash[:], CreatedAt: now}
		if err := st.CreateAPIKey(ctx, record); err != nil {
			return store.APIKey{}, false, errors.New("auth: could not store restoration key")
		}
		return record, true, nil
	default:
		return store.APIKey{}, false, errors.New("auth: could not look up restoration key")
	}
}
