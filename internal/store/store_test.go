package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	return s
}

func TestOpenCreatesPrivateDatabase(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	s, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = s.Close() }()

	dirInfo, err := os.Stat(dir)
	if err != nil || dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("data dir mode = %v, %v; want 0700", dirInfo.Mode().Perm(), err)
	}
	fileInfo, err := os.Stat(filepath.Join(dir, databaseFile))
	if err != nil || fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("database mode = %v, %v; want 0600", fileInfo.Mode().Perm(), err)
	}
}

func TestOpenTightensExistingDatabasePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, databaseFile)
	if err := os.WriteFile(path, nil, 0o644); err != nil { //nolint:gosec // Deliberately permissive to verify tightening.
		t.Fatal(err)
	}
	s, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = s.Close() }()
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("database mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}
}

func TestOpenRejectsUnsafePaths(t *testing.T) {
	t.Run("symlinked data dir", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "target")
		link := filepath.Join(root, "link")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		assertOpenFails(t, link)
	})
	t.Run("data dir is a file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		assertOpenFails(t, path)
	})
	t.Run("uncreatable data dir", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		assertOpenFails(t, filepath.Join(path, "child"))
	})
	t.Run("symlinked database", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(t.TempDir(), "elsewhere.db")
		if err := os.WriteFile(target, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(dir, databaseFile)); err != nil {
			t.Fatal(err)
		}
		assertOpenFails(t, dir)
	})
	t.Run("database is a directory", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, databaseFile), 0o700); err != nil {
			t.Fatal(err)
		}
		assertOpenFails(t, dir)
	})
	t.Run("unwritable data dir", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root bypasses permissions")
		}
		dir := t.TempDir()
		if err := os.Chmod(dir, 0o500); err != nil { //nolint:gosec // Read-only directory fixture.
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // Restores the fixture for cleanup.
		assertOpenFails(t, dir)
	})
}

func assertOpenFails(t *testing.T, dir string) {
	t.Helper()
	s, err := Open(context.Background(), dir)
	if err == nil {
		_ = s.Close()
		t.Fatal("Open() error = nil")
	}
	if strings.Contains(err.Error(), dir) {
		t.Fatalf("Open() leaked path in error: %v", err)
	}
}

func TestOpenConfiguresConnection(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	checks := map[string]string{
		"PRAGMA foreign_keys": "1",
		"PRAGMA journal_mode": "wal",
		"PRAGMA busy_timeout": "5000",
		"PRAGMA synchronous":  "1",
	}
	for query, want := range checks {
		var got string
		if err := s.db.QueryRowContext(ctx, query).Scan(&got); err != nil || got != want {
			t.Fatalf("%s = %q, %v; want %q", query, got, err, want)
		}
	}
}

func TestOpenAppliesMigrationsIdempotently(t *testing.T) {
	dir := t.TempDir()
	for range 2 {
		s, err := Open(context.Background(), dir)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		if err := s.Check(context.Background()); err != nil {
			t.Fatalf("Check() error = %v", err)
		}
		for _, table := range []string{"api_keys", "jobs", "items", "download_tokens"} {
			var name string
			err := s.db.QueryRowContext(context.Background(),
				"SELECT name FROM sqlite_schema WHERE type = 'table' AND name = ?", table).Scan(&name)
			if err != nil {
				t.Fatalf("table %s missing: %v", table, err)
			}
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOpenRejectsNewerSchema(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(context.Background(), "PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	if err := s.Check(context.Background()); err == nil {
		t.Fatal("Check() error = nil for mismatched schema version")
	}
	_ = s.Close()

	assertOpenFails(t, dir)
}

func TestCheckFailsAfterClose(t *testing.T) {
	s, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if err := s.Check(context.Background()); err == nil {
		t.Fatal("Check() error = nil after Close")
	}
}

func TestMigrateRejectsInvalidMigrationSets(t *testing.T) {
	tests := []struct {
		name   string
		source fstest.MapFS
	}{
		{name: "gap in numbering", source: fstest.MapFS{
			"migrations/0001_a.sql": {Data: []byte("CREATE TABLE a (id INTEGER);")},
			"migrations/0003_c.sql": {Data: []byte("CREATE TABLE c (id INTEGER);")},
		}},
		{name: "unnumbered file", source: fstest.MapFS{
			"migrations/initial.sql": {Data: []byte("CREATE TABLE a (id INTEGER);")},
		}},
		{name: "invalid SQL", source: fstest.MapFS{
			"migrations/0001_a.sql": {Data: []byte("CREATE TABLE")},
		}},
		{name: "missing directory", source: fstest.MapFS{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := openTestStore(t)
			if _, err := s.db.ExecContext(context.Background(), "PRAGMA user_version = 0"); err != nil {
				t.Fatal(err)
			}
			if _, err := migrate(context.Background(), s.db, tt.source); err == nil {
				t.Fatal("migrate() error = nil")
			}
		})
	}
}

func TestMigrateRollsBackFailedMigration(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, "PRAGMA user_version = 0"); err != nil {
		t.Fatal(err)
	}
	source := fstest.MapFS{
		"migrations/0001_a.sql": {Data: []byte("CREATE TABLE rollback_probe (id INTEGER); SELECT * FROM missing_table;")},
	}
	if _, err := migrate(ctx, s.db, source); err == nil {
		t.Fatal("migrate() error = nil")
	}
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE name = 'rollback_probe'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial migration persisted: count=%d err=%v", count, err)
	}
}
