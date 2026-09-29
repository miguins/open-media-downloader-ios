package auth

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/logging"
	"github.com/miguins/open-media-downloader-ios/internal/store"
)

type fakeKeys struct {
	keys     map[string]store.APIKey
	getErr   error
	touchErr error
	touched  []time.Time
	lookedUp []string
}

func (f *fakeKeys) APIKey(_ context.Context, keyID string) (store.APIKey, error) {
	f.lookedUp = append(f.lookedUp, keyID)
	if f.getErr != nil {
		return store.APIKey{}, f.getErr
	}
	key, ok := f.keys[keyID]
	if !ok {
		return store.APIKey{}, store.ErrNotFound
	}

	return key, nil
}

func (f *fakeKeys) TouchAPIKey(_ context.Context, keyID string, at time.Time) error {
	f.touched = append(f.touched, at)
	if f.touchErr != nil {
		return f.touchErr
	}
	key := f.keys[keyID]
	key.LastUsedAt = at
	f.keys[keyID] = key

	return nil
}

func newFixture(t *testing.T) (*Authenticator, *fakeKeys, string, *bytes.Buffer) {
	t.Helper()
	plaintext, record := Generate("phone", now)
	keys := &fakeKeys{keys: map[string]store.APIKey{record.ID: record}}
	logs := &bytes.Buffer{}
	a := NewAuthenticator(keys, slog.New(slog.NewJSONHandler(logs, nil)))
	a.now = func() time.Time { return now }

	return a, keys, plaintext, logs
}

func serve(a *Authenticator, request *http.Request) (*httptest.ResponseRecorder, string) {
	var owner string
	handler := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner, _ = OwnerID(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	return response, owner
}

func newRequest(authorization ...string) *http.Request {
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	for _, value := range authorization {
		request.Header.Add("Authorization", value)
	}

	return request
}

func TestMiddlewareAcceptsValidKey(t *testing.T) {
	a, keys, plaintext, _ := newFixture(t)
	for _, scheme := range []string{"Bearer ", "bearer "} {
		response, owner := serve(a, newRequest(scheme+plaintext))
		if response.Code != http.StatusNoContent {
			t.Fatalf("status = %d; want 204", response.Code)
		}
		keyID, _, _ := Parse(plaintext)
		if owner != keyID {
			t.Fatalf("OwnerID() = %q; want %q", owner, keyID)
		}
	}
	if len(keys.touched) != 1 {
		t.Fatalf("touched %d times; want 1 (throttled)", len(keys.touched))
	}

	a.now = func() time.Time { return now.Add(time.Minute) }
	serve(a, newRequest("Bearer "+plaintext))
	if len(keys.touched) != 2 {
		t.Fatalf("touched %d times; want 2 after a minute", len(keys.touched))
	}
}

func TestMiddlewareRejectsInvalidCredentials(t *testing.T) {
	a, keys, plaintext, logs := newFixture(t)
	keyID, _, _ := Parse(plaintext)
	unknown, _ := Generate("other", now)
	wrongSecret := plaintext[:32] + unknown[32:]

	revokedPlaintext, revoked := Generate("revoked", now)
	revoked.RevokedAt = now
	keys.keys[revoked.ID] = revoked

	tests := map[string]struct {
		request *http.Request
		reason  string
	}{
		"missing header":    {newRequest(), "missing_credentials"},
		"empty header":      {newRequest(""), "missing_credentials"},
		"basic scheme":      {newRequest("Basic " + plaintext), "missing_credentials"},
		"no scheme":         {newRequest(plaintext), "missing_credentials"},
		"malformed key":     {newRequest("Bearer omdi_short"), "malformed_key"},
		"unknown key":       {newRequest("Bearer " + unknown), "unknown_key"},
		"wrong secret":      {newRequest("Bearer " + wrongSecret), "invalid_secret"},
		"revoked key":       {newRequest("Bearer " + revokedPlaintext), "revoked_key"},
		"duplicate headers": {newRequest("Bearer "+plaintext, "Bearer "+plaintext), "missing_credentials"},
		"query string": {httptest.NewRequestWithContext(context.Background(), http.MethodGet,
			"/protected?api_key="+plaintext+"&access_token="+plaintext, nil), "missing_credentials"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			logs.Reset()
			response, owner := serve(a, test.request)
			if response.Code != http.StatusUnauthorized || owner != "" {
				t.Fatalf("status = %d owner = %q; want 401", response.Code, owner)
			}
			if got := response.Header().Get("WWW-Authenticate"); got != "Bearer" {
				t.Fatalf("WWW-Authenticate = %q", got)
			}
			if got := response.Body.String(); got != "{\"error\":\"unauthorized\"}\n" {
				t.Fatalf("body = %q", got)
			}
			if got := logs.String(); !strings.Contains(got, `"msg":"authentication rejected"`) ||
				!strings.Contains(got, `"reason":"`+test.reason+`"`) {
				t.Fatalf("logs = %s; want rejection reason %q", got, test.reason)
			}
			if got := logs.String(); strings.Contains(got, plaintext[32:]) || strings.Contains(got, keyID) ||
				strings.Contains(got, revokedPlaintext[32:]) || strings.Contains(got, revoked.ID) {
				t.Fatalf("logs disclosed credentials: %s", got)
			}
		})
	}
	if len(keys.touched) != 0 {
		t.Fatal("failed authentication recorded key use")
	}
}

func TestMiddlewareAddsKeyToLogScope(t *testing.T) {
	a, keys, plaintext, _ := newFixture(t)
	keyID, _, _ := Parse(plaintext)
	ctx := logging.With(context.Background())
	request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/protected", nil)
	request.Header.Set("Authorization", "Bearer "+plaintext)
	serve(a, request)

	var logs bytes.Buffer
	logging.New(&logs, slog.LevelInfo).InfoContext(ctx, "request completed")
	if got := logs.String(); !strings.Contains(got, `"key_id":"`+keyID+`"`) ||
		!strings.Contains(got, `"key_name":"`+keys.keys[keyID].Name+`"`) || strings.Contains(got, plaintext[32:]) {
		t.Fatalf("logs = %s; want the key ID and name without the secret", got)
	}
}

func TestMiddlewareStoreFailure(t *testing.T) {
	a, keys, plaintext, logs := newFixture(t)
	keys.getErr = errors.New("database is locked")

	response, owner := serve(a, newRequest("Bearer "+plaintext))
	if response.Code != http.StatusInternalServerError || owner != "" {
		t.Fatalf("status = %d; want 500", response.Code)
	}
	if got := response.Body.String(); got != "{\"error\":\"internal\"}\n" {
		t.Fatalf("body = %q", got)
	}
	if !strings.Contains(logs.String(), "authentication unavailable") || strings.Contains(logs.String(), plaintext[32:]) {
		t.Fatalf("unexpected logs: %s", logs.String())
	}
}

func TestMiddlewareToleratesTouchFailure(t *testing.T) {
	a, keys, plaintext, logs := newFixture(t)
	keys.touchErr = errors.New("disk full")

	response, _ := serve(a, newRequest("Bearer "+plaintext))
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d; want 204", response.Code)
	}
	if !strings.Contains(logs.String(), "record API key use failed") {
		t.Fatalf("missing warning: %s", logs.String())
	}
}

func TestOwnerIDWithoutAuthentication(t *testing.T) {
	if owner, ok := OwnerID(context.Background()); ok || owner != "" {
		t.Fatalf("OwnerID() = %q, %v", owner, ok)
	}
}

func TestNewAuthenticatorUsesWallClock(t *testing.T) {
	a := NewAuthenticator(&fakeKeys{}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if got := a.now(); time.Since(got) > time.Minute {
		t.Fatalf("now() = %v", got)
	}
}

func TestRejectionIsUnauthorized(t *testing.T) {
	err := error(rejectedRevoked)
	if !errors.Is(err, ErrUnauthorized) || err.Error() != "auth: unauthorized: revoked_key" {
		t.Fatalf("rejection = %v; want an ErrUnauthorized with its reason", err)
	}
}
