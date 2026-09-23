package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/store"
)

const (
	bearerPrefix     = "Bearer "
	touchInterval    = time.Minute
	unauthorizedBody = "{\"error\":\"unauthorized\"}\n"
	internalBody     = "{\"error\":\"internal\"}\n"
)

// ErrUnauthorized reports missing, malformed, unknown, revoked, or incorrect credentials.
var ErrUnauthorized = errors.New("auth: unauthorized")

// KeyStore loads API keys and records their use.
type KeyStore interface {
	APIKey(ctx context.Context, id string) (store.APIKey, error)
	TouchAPIKey(ctx context.Context, id string, now time.Time) error
}

// Authenticator verifies API keys presented as bearer tokens.
type Authenticator struct {
	keys   KeyStore
	logger *slog.Logger
	now    func() time.Time
}

// NewAuthenticator returns an Authenticator backed by keys.
func NewAuthenticator(keys KeyStore, logger *slog.Logger) *Authenticator {
	return &Authenticator{keys: keys, logger: logger, now: time.Now}
}

type ownerKey struct{}

// OwnerID returns the authenticated API key ID stored in ctx by the middleware.
func OwnerID(ctx context.Context) (string, bool) {
	owner, ok := ctx.Value(ownerKey{}).(string)

	return owner, ok
}

// Middleware rejects requests without a valid bearer API key and stores the key ID in the request context.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		values := request.Header.Values("Authorization")
		authorization := ""
		if len(values) == 1 {
			authorization = values[0]
		}

		keyID, err := a.authenticate(request.Context(), authorization)
		switch {
		case errors.Is(err, ErrUnauthorized):
			response.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(response, http.StatusUnauthorized, unauthorizedBody)
		case err != nil:
			a.logger.ErrorContext(request.Context(), "authentication unavailable")
			writeJSON(response, http.StatusInternalServerError, internalBody)
		default:
			next.ServeHTTP(response, request.WithContext(context.WithValue(request.Context(), ownerKey{}, keyID)))
		}
	})
}

// authenticate verifies an Authorization header value and returns the key ID.
// Unknown keys are compared against a dummy hash so every well-formed key costs one comparison.
func (a *Authenticator) authenticate(ctx context.Context, authorization string) (string, error) {
	if len(authorization) <= len(bearerPrefix) || !strings.EqualFold(authorization[:len(bearerPrefix)], bearerPrefix) {
		return "", ErrUnauthorized
	}
	keyID, secret, ok := Parse(authorization[len(bearerPrefix):])
	if !ok {
		return "", ErrUnauthorized
	}

	record, err := a.keys.APIKey(ctx, keyID)
	found := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return "", fmt.Errorf("auth: load API key: %w", err)
	}
	expected := make([]byte, sha256.Size)
	if found {
		expected = record.SecretHash
	}
	actual := sha256.Sum256(secret)
	matches := subtle.ConstantTimeCompare(actual[:], expected) == 1
	if !found || !matches || !record.RevokedAt.IsZero() {
		return "", ErrUnauthorized
	}

	now := a.now()
	if now.Sub(record.LastUsedAt) >= touchInterval {
		if err := a.keys.TouchAPIKey(ctx, keyID, now); err != nil {
			a.logger.WarnContext(ctx, "record API key use failed")
		}
	}

	return keyID, nil
}

func writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_, _ = io.WriteString(response, body)
}
