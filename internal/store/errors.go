package store

import (
	"database/sql"
	"errors"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var (
	// ErrNotFound reports that no record matched, including records owned by someone else.
	ErrNotFound = errors.New("store: not found")
	// ErrConflict reports that a record with the same unique value already exists.
	ErrConflict = errors.New("store: conflict")
)

func isUniqueViolation(err error) bool {
	var sqliteError *sqlite.Error
	if !errors.As(err, &sqliteError) {
		return false
	}
	code := sqliteError.Code()

	return code == sqlite3.SQLITE_CONSTRAINT_UNIQUE || code == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
}

func toMillis(t time.Time) int64 {
	return t.UnixMilli()
}

func nullableMillis(t time.Time) sql.NullInt64 {
	if t.IsZero() {
		return sql.NullInt64{}
	}

	return sql.NullInt64{Int64: t.UnixMilli(), Valid: true}
}

func fromMillis(value int64) time.Time {
	return time.UnixMilli(value).UTC()
}

func fromNullableMillis(value sql.NullInt64) time.Time {
	if !value.Valid {
		return time.Time{}
	}

	return fromMillis(value.Int64)
}

// requireAffected converts a zero-row update into ErrNotFound.
func requireAffected(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}

	return nil
}
