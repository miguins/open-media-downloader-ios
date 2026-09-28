package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/store"
)

func restorationStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestRestoreAPIKeyCreatesUsableCredential(t *testing.T) {
	ctx := context.Background()
	st := restorationStore(t)
	plaintext, original := Generate("phone", now)
	if err := RestoreAPIKey(ctx, st, plaintext, now); err != nil {
		t.Fatal(err)
	}
	got, err := st.APIKey(ctx, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.SecretHash, original.SecretHash) || !got.CreatedAt.Equal(now) || !ValidKeyName(got.Name) {
		t.Fatal("restoration did not persist a valid hashed credential")
	}
	authenticator := NewAuthenticator(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	owner, err := authenticator.authenticate(ctx, "Bearer "+plaintext)
	if err != nil || owner != original.ID {
		t.Fatalf("restored credential cannot authenticate: %v", err)
	}
}

func TestRestoreAPIKeyPreservesExistingRecords(t *testing.T) {
	ctx := context.Background()
	for _, revoked := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "revoked"}[revoked], func(t *testing.T) {
			st := restorationStore(t)
			plaintext, original := Generate("phone", now)
			if err := st.CreateAPIKey(ctx, original); err != nil {
				t.Fatal(err)
			}
			if err := st.TouchAPIKey(ctx, original.ID, now.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			if revoked {
				if err := st.RevokeAPIKey(ctx, original.ID, now.Add(2*time.Minute)); err != nil {
					t.Fatal(err)
				}
			}
			before, err := st.APIKey(ctx, original.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := RestoreAPIKey(ctx, st, plaintext, now.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			after, err := st.APIKey(ctx, original.ID)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("restoration changed an existing record")
			}
			keys, err := st.APIKeys(ctx)
			if err != nil || len(keys) != 1 {
				t.Fatal("restoration duplicated a key")
			}
			if revoked {
				authenticator := NewAuthenticator(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
				if _, err := authenticator.authenticate(ctx, "Bearer "+plaintext); !errors.Is(err, ErrUnauthorized) {
					t.Fatal("restoration reactivated a revoked key")
				}
			}
		})
	}
}

func TestRestoreAPIKeyRejectsErrorsWithoutChangingKeys(t *testing.T) {
	ctx := context.Background()
	for _, scenario := range []string{"malformed", "secret conflict", "name conflict", "database unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			st := restorationStore(t)
			plaintext, record := Generate("phone", now)
			switch scenario {
			case "malformed":
				plaintext = "private-malformed-key"
			case "secret conflict":
				if err := st.CreateAPIKey(ctx, record); err != nil {
					t.Fatal(err)
				}
				other, _ := Generate("other", now)
				plaintext = plaintext[:32] + other[32:]
			case "name conflict":
				_, occupied := Generate("startup-"+record.ID, now)
				if err := st.CreateAPIKey(ctx, occupied); err != nil {
					t.Fatal(err)
				}
			case "database unavailable":
				if err := st.Close(); err != nil {
					t.Fatal(err)
				}
			}
			var before []store.APIKey
			if scenario != "database unavailable" {
				var err error
				before, err = st.APIKeys(ctx)
				if err != nil {
					t.Fatal(err)
				}
			}
			err := RestoreAPIKey(ctx, st, plaintext, now)
			if err == nil {
				t.Fatal("restoration unexpectedly succeeded")
			}
			if strings.Contains(err.Error(), plaintext) || (len(plaintext) > 32 && strings.Contains(err.Error(), plaintext[32:])) {
				t.Fatal("restoration error disclosed a credential")
			}
			if scenario != "database unavailable" {
				after, err := st.APIKeys(ctx)
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatal("failed restoration changed keys")
				}
			}
		})
	}
}
