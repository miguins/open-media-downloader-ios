package store

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

var testNow = time.UnixMilli(1_800_000_000_000).UTC()

func createTestKey(t *testing.T, s *Store, id, name string) APIKey {
	t.Helper()
	key := APIKey{ID: id, Name: name, SecretHash: []byte("hash-" + id), CreatedAt: testNow}
	if err := s.CreateAPIKey(context.Background(), key); err != nil {
		t.Fatalf("CreateAPIKey() error = %v", err)
	}

	return key
}

func TestAPIKeyLifecycle(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	created := createTestKey(t, s, "key1", "phone")

	got, err := s.APIKey(ctx, "key1")
	if err != nil {
		t.Fatalf("APIKey() error = %v", err)
	}
	if got.ID != created.ID || got.Name != created.Name || !bytes.Equal(got.SecretHash, created.SecretHash) ||
		!got.CreatedAt.Equal(testNow) || !got.LastUsedAt.IsZero() || !got.RevokedAt.IsZero() {
		t.Fatalf("APIKey() = %#v", got)
	}

	used := testNow.Add(time.Minute)
	if err := s.TouchAPIKey(ctx, "key1", used); err != nil {
		t.Fatalf("TouchAPIKey() error = %v", err)
	}
	revoked := testNow.Add(time.Hour)
	if err := s.RevokeAPIKey(ctx, "key1", revoked); err != nil {
		t.Fatalf("RevokeAPIKey() error = %v", err)
	}
	if err := s.RevokeAPIKey(ctx, "key1", revoked.Add(time.Hour)); err != nil {
		t.Fatalf("second RevokeAPIKey() error = %v", err)
	}
	got, err = s.APIKey(ctx, "key1")
	if err != nil || !got.LastUsedAt.Equal(used) || !got.RevokedAt.Equal(revoked) {
		t.Fatalf("APIKey() = %#v, %v", got, err)
	}
}

func TestAPIKeyErrors(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	createTestKey(t, s, "key1", "phone")

	if err := s.CreateAPIKey(ctx, APIKey{ID: "key2", Name: "phone", SecretHash: []byte("x"), CreatedAt: testNow}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate name error = %v; want ErrConflict", err)
	}
	if err := s.CreateAPIKey(ctx, APIKey{ID: "key1", Name: "other", SecretHash: []byte("x"), CreatedAt: testNow}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate id error = %v; want ErrConflict", err)
	}
	if _, err := s.APIKey(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("APIKey(missing) error = %v", err)
	}
	if err := s.RevokeAPIKey(ctx, "missing", testNow); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RevokeAPIKey(missing) error = %v", err)
	}
	if err := s.TouchAPIKey(ctx, "missing", testNow); !errors.Is(err, ErrNotFound) {
		t.Fatalf("TouchAPIKey(missing) error = %v", err)
	}
}

func TestAPIKeysListsInCreationOrder(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if keys, err := s.APIKeys(ctx); err != nil || len(keys) != 0 {
		t.Fatalf("APIKeys() = %#v, %v", keys, err)
	}
	later := APIKey{ID: "a", Name: "later", SecretHash: []byte("x"), CreatedAt: testNow.Add(time.Second)}
	if err := s.CreateAPIKey(ctx, later); err != nil {
		t.Fatal(err)
	}
	createTestKey(t, s, "b", "earlier")

	keys, err := s.APIKeys(ctx)
	if err != nil || len(keys) != 2 || keys[0].Name != "earlier" || keys[1].Name != "later" {
		t.Fatalf("APIKeys() = %#v, %v", keys, err)
	}
}

func TestRepositoriesFailAfterClose(t *testing.T) {
	s, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	ctx := context.Background()
	checks := map[string]error{
		"CreateAPIKey": s.CreateAPIKey(ctx, APIKey{ID: "k", Name: "n", SecretHash: []byte("x"), CreatedAt: testNow}),
		"RevokeAPIKey": s.RevokeAPIKey(ctx, "k", testNow),
		"TouchAPIKey":  s.TouchAPIKey(ctx, "k", testNow),
	}
	_, checks["APIKey"] = s.APIKey(ctx, "k")
	_, checks["APIKeys"] = s.APIKeys(ctx)
	for name, err := range checks {
		if err == nil || errors.Is(err, ErrNotFound) {
			t.Fatalf("%s() error = %v; want database error", name, err)
		}
	}
}
