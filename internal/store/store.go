// Package store persists application state in SQLite.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	// Register the pure-Go SQLite driver.
	_ "modernc.org/sqlite"
)

const databaseFile = "omdi.db"

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// Store provides access to the application database.
type Store struct {
	db            *sql.DB
	schemaVersion int
}

// Open prepares the private data directory, opens the database, and applies pending migrations.
// Returned errors never contain filesystem paths.
func Open(ctx context.Context, dataDir string) (*Store, error) {
	if err := prepareDataDir(dataDir); err != nil {
		return nil, err
	}
	databasePath := filepath.Join(dataDir, databaseFile)
	if err := prepareDatabaseFile(databasePath); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dataSourceName(databasePath))
	if err != nil {
		return nil, fmt.Errorf("store: open database: %w", err)
	}
	db.SetMaxOpenConns(1)

	version, err := migrate(ctx, db, embeddedMigrations)
	if err != nil {
		return nil, errors.Join(err, db.Close())
	}

	return &Store{db: db, schemaVersion: version}, nil
}

// Close releases the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// Check verifies that the database answers queries and has the expected schema version.
func (s *Store) Check(ctx context.Context) error {
	version, err := userVersion(ctx, s.db)
	if err != nil {
		return err
	}
	if version != s.schemaVersion {
		return errors.New("store: unexpected schema version")
	}

	return nil
}

func dataSourceName(databasePath string) string {
	query := url.Values{}
	for _, pragma := range []string{"foreign_keys(1)", "journal_mode(WAL)", "busy_timeout(5000)", "synchronous(NORMAL)"} {
		query.Add("_pragma", pragma)
	}
	query.Set("_txlock", "immediate")

	return (&url.URL{Scheme: "file", Path: databasePath, RawQuery: query.Encode()}).String()
}

func prepareDataDir(dataDir string) error {
	info, err := os.Lstat(dataDir)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(dataDir, 0o700); err != nil {
			return pathlessError("store: create data directory", err)
		}

		return nil
	}
	if err != nil {
		return pathlessError("store: inspect data directory", err)
	}
	if !info.IsDir() {
		return errors.New("store: data directory must be a real directory")
	}

	return nil
}

func prepareDatabaseFile(databasePath string) error {
	info, err := os.Lstat(databasePath)
	if errors.Is(err, fs.ErrNotExist) {
		// The path is operator configuration joined with a constant file name.
		file, err := os.OpenFile(databasePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // See above.
		if err != nil {
			return pathlessError("store: create database", err)
		}

		return pathlessError("store: create database", file.Close())
	}
	if err != nil {
		return pathlessError("store: inspect database", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("store: database must be a regular file")
	}

	return pathlessError("store: restrict database permissions", os.Chmod(databasePath, 0o600))
}

// pathlessError wraps err under message after removing any filesystem path it carries.
func pathlessError(message string, err error) error {
	if err == nil {
		return nil
	}
	var pathError *fs.PathError
	if errors.As(err, &pathError) {
		err = pathError.Err
	}

	return fmt.Errorf("%s: %w", message, err)
}

type migration struct {
	version int
	sql     string
}

func loadMigrations(source fs.FS) ([]migration, error) {
	names, err := fs.Glob(source, "migrations/*.sql")
	if err != nil || len(names) == 0 {
		return nil, errors.New("store: no migrations found")
	}
	migrations := make([]migration, 0, len(names))
	for index, name := range names {
		prefix, _, _ := strings.Cut(path.Base(name), "_")
		version, err := strconv.Atoi(prefix)
		if err != nil || version != index+1 {
			return nil, errors.New("store: migrations must be numbered consecutively from 0001")
		}
		contents, err := fs.ReadFile(source, name)
		if err != nil {
			return nil, fmt.Errorf("store: read migration %d: %w", version, err)
		}
		migrations = append(migrations, migration{version: version, sql: string(contents)})
	}

	return migrations, nil
}

// migrate applies pending migrations from source and returns the resulting schema version.
func migrate(ctx context.Context, db *sql.DB, source fs.FS) (int, error) {
	migrations, err := loadMigrations(source)
	if err != nil {
		return 0, err
	}
	current, err := userVersion(ctx, db)
	if err != nil {
		return 0, err
	}
	if current > len(migrations) {
		return 0, errors.New("store: database schema is newer than this binary")
	}
	for _, m := range migrations[current:] {
		if err := applyMigration(ctx, db, m); err != nil {
			return 0, err
		}
	}

	return len(migrations), nil
}

func applyMigration(ctx context.Context, db *sql.DB, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin migration %d: %w", m.version, err)
	}
	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return errors.Join(fmt.Errorf("store: apply migration %d: %w", m.version, err), tx.Rollback())
	}
	if _, err := tx.ExecContext(ctx, "PRAGMA user_version = "+strconv.Itoa(m.version)); err != nil {
		return errors.Join(fmt.Errorf("store: record migration %d: %w", m.version, err), tx.Rollback())
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit migration %d: %w", m.version, err)
	}

	return nil
}

func userVersion(ctx context.Context, db *sql.DB) (int, error) {
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return 0, fmt.Errorf("store: read schema version: %w", err)
	}

	return version, nil
}
