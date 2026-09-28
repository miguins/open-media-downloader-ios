package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/auth"
	"github.com/miguins/open-media-downloader-ios/internal/store"
)

func TestServeRestoresConfiguredKeyAcrossRestarts(t *testing.T) {
	ctx := context.Background()
	plaintext, original := auth.Generate("phone", time.Now().UTC())
	// Separate directories model a restart after all local data was lost.
	for range 2 {
		c := newCLI(t)
		c.env["OMDI_RESTORE_API_KEY_ON_STARTUP"] = "true"
		c.env["OMDI_API_KEY"] = plaintext
		// Stop at listen so this test exercises startup without a long-lived server.
		c.env["OMDI_HTTP_ADDR"] = "192.0.2.1:1"
		var first store.APIKey
		for attempt := range 2 {
			code, stdout, stderr := c.run(ctx, "serve")
			if code != 1 || !strings.Contains(stderr, "application stopped unexpectedly") {
				t.Fatalf("startup did not reach listener: code=%d", code)
			}
			if stdout != "" || strings.Contains(stderr, plaintext) || strings.Contains(stderr, plaintext[32:]) {
				t.Fatal("startup disclosed a secret")
			}
			st, err := store.Open(ctx, c.env["OMDI_DATA_DIR"])
			if err != nil {
				t.Fatal(err)
			}
			keys, err := st.APIKeys(ctx)
			_ = st.Close()
			if err != nil || len(keys) != 1 {
				t.Fatalf("expected one restored key, got %d: %v", len(keys), err)
			}
			got := keys[0]
			if got.ID != original.ID || !reflect.DeepEqual(got.SecretHash, original.SecretHash) {
				t.Fatal("restored key differs from configured credential")
			}
			if attempt == 0 {
				first = got
			} else if !reflect.DeepEqual(first, got) {
				t.Fatal("restart modified an existing key")
			}
		}
	}
}

func TestKeyRestorationIsOptInAndServeOnly(t *testing.T) {
	plaintext, key := auth.Generate("phone", time.Now().UTC())
	for _, tt := range []struct {
		name, toggle string
		args         []string
	}{
		{"default", "", []string{"serve"}},
		{"disabled", "false", []string{"serve"}},
		{"keys command", "true", []string{"keys", "list"}},
		{"jobs command", "true", []string{"jobs", "list"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := newCLI(t)
			c.env["OMDI_API_KEY"] = plaintext
			c.env["OMDI_HTTP_ADDR"] = "192.0.2.1:1"
			if tt.toggle != "" {
				c.env["OMDI_RESTORE_API_KEY_ON_STARTUP"] = tt.toggle
			}
			code, _, _ := c.run(context.Background(), tt.args...)
			want := 0
			if tt.args[0] == "serve" {
				want = 1
			}
			if code != want {
				t.Fatalf("exit=%d, want %d", code, want)
			}
			st, err := store.Open(context.Background(), c.env["OMDI_DATA_DIR"])
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = st.Close() }()
			if _, err := st.APIKey(context.Background(), key.ID); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("unexpected restoration: %v", err)
			}
		})
	}
}

func TestServeRejectsRestorationSecretConflict(t *testing.T) {
	ctx := context.Background()
	c := newCLI(t)
	plaintext, original := auth.Generate("phone", time.Now().UTC())
	other, _ := auth.Generate("other", time.Now().UTC())
	conflicting := plaintext[:32] + other[32:]
	st, err := store.Open(ctx, c.env["OMDI_DATA_DIR"])
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if err := st.CreateAPIKey(ctx, original); err != nil {
		t.Fatal(err)
	}
	c.env["OMDI_RESTORE_API_KEY_ON_STARTUP"] = "true"
	c.env["OMDI_API_KEY"] = conflicting
	c.env["OMDI_HTTP_ADDR"] = "192.0.2.1:1"
	code, stdout, stderr := c.run(ctx, "serve")
	if code != 1 || !strings.Contains(stderr, "restore API key") || strings.Contains(stderr, "application stopped unexpectedly") {
		t.Fatalf("conflict did not stop startup before listener: code=%d", code)
	}
	if stdout != "" || strings.Contains(stderr, other[32:]) {
		t.Fatal("startup disclosed a secret")
	}
	got, err := st.APIKey(ctx, original.ID)
	if err != nil || !reflect.DeepEqual(got.SecretHash, original.SecretHash) {
		t.Fatal("conflict changed stored credential")
	}
}
