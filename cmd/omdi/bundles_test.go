package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/auth"
	"github.com/miguins/open-media-downloader-ios/internal/job"
	"github.com/miguins/open-media-downloader-ios/internal/store"
)

func TestOperatorRemovesBundles(t *testing.T) {
	for _, action := range []string{"delete", "purge", "key purge"} {
		t.Run(action, func(t *testing.T) {
			c := newCLI(t)
			ctx := context.Background()
			code, key, _ := c.run(ctx, "keys", "create", "--name", "bundle-owner")
			if code != 0 {
				t.Fatal("create key")
			}
			owner, _, _ := auth.Parse(strings.TrimSpace(key))
			st, err := store.Open(ctx, c.env["OMDI_DATA_DIR"])
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = st.Close() }()
			now := time.Now().UTC()
			j := job.New(owner, "https://vimeo.com/1", "vimeo", now, time.Hour)
			if err := st.CreateJob(ctx, j); err != nil {
				t.Fatal(err)
			}
			claimed, err := st.ClaimNextJob(ctx, now)
			if err != nil {
				t.Fatal(err)
			}
			if err := claimed.Transition(job.StatusSucceeded, "", now); err != nil {
				t.Fatal(err)
			}
			items := []job.Item{{ID: "one", JobID: j.ID, Position: 0, FileName: "one.mp4", SizeBytes: 3, CreatedAt: now}, {ID: "two", JobID: j.ID, Position: 1, FileName: "two.jpg", SizeBytes: 4, CreatedAt: now}}
			b := job.Bundle{JobID: j.ID, FileName: "bundle.zip", SizeBytes: 300, CreatedAt: now}
			if err := st.CompleteJob(ctx, claimed, items, &b); err != nil {
				t.Fatal(err)
			}
			tok := job.BundleToken{Hash: []byte("bundle token"), JobID: j.ID, OwnerID: owner, CreatedAt: now, ExpiresAt: now.Add(time.Minute)}
			if err := st.ReplaceBundleToken(ctx, tok); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(c.env["OMDI_DATA_DIR"], "jobs", j.ID)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "bundle.zip"), []byte("fixture"), 0o600); err != nil {
				t.Fatal(err)
			}
			args := []string{"jobs", action, j.ID}
			if action == "purge" {
				args = []string{"jobs", "purge", "--yes"}
			}
			if action == "key purge" {
				if code, _, _ := c.run(ctx, "keys", "revoke", owner); code != 0 {
					t.Fatal("revoke")
				}
				args = []string{"keys", "purge", "--yes"}
			}
			if code, _, logs := c.run(ctx, args...); code != 0 {
				t.Fatalf("operator %d %s", code, logs)
			}
			if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("operator left bundle directory")
			}
			if _, err := st.ValidBundleToken(ctx, tok.Hash, now); !errors.Is(err, store.ErrNotFound) {
				t.Fatal("operator left token")
			}
		})
	}
}
