package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// APIKey is a stored API key. Only a hash of its secret is persisted.
type APIKey struct {
	ID         string
	Name       string
	SecretHash []byte
	CreatedAt  time.Time
	LastUsedAt time.Time
	RevokedAt  time.Time
}

const apiKeyColumns = "id, name, secret_hash, created_at, last_used_at, revoked_at"

// CreateAPIKey stores a new API key. It returns ErrConflict when the ID or name is taken.
func (s *Store) CreateAPIKey(ctx context.Context, key APIKey) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO api_keys (id, name, secret_hash, created_at) VALUES (?, ?, ?, ?)",
		key.ID, key.Name, key.SecretHash, toMillis(key.CreatedAt))
	if isUniqueViolation(err) {
		return ErrConflict
	}
	if err != nil {
		return fmt.Errorf("store: create API key: %w", err)
	}

	return nil
}

// APIKey returns the API key with id, or ErrNotFound.
func (s *Store) APIKey(ctx context.Context, id string) (APIKey, error) {
	row := s.db.QueryRowContext(ctx, "SELECT "+apiKeyColumns+" FROM api_keys WHERE id = ?", id)
	key, err := scanAPIKey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return APIKey{}, ErrNotFound
	}
	if err != nil {
		return APIKey{}, fmt.Errorf("store: get API key: %w", err)
	}

	return key, nil
}

// APIKeys returns every API key ordered by creation time.
func (s *Store) APIKeys(ctx context.Context) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+apiKeyColumns+" FROM api_keys ORDER BY created_at, id")
	if err != nil {
		return nil, fmt.Errorf("store: list API keys: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var keys []APIKey
	for rows.Next() {
		key, err := scanAPIKey(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list API keys: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list API keys: %w", err)
	}

	return keys, nil
}

// RevokeAPIKey marks the key revoked, keeping the first revocation time. It returns ErrNotFound for unknown IDs.
func (s *Store) RevokeAPIKey(ctx context.Context, id string, now time.Time) error {
	err := requireAffected(s.db.ExecContext(ctx,
		"UPDATE api_keys SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ?", toMillis(now), id))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("store: revoke API key: %w", err)
	}

	return err
}

// PurgeRevokedAPIKeys deletes every revoked API key and all of its jobs, canceling queued and
// running ones first. It returns the number of deleted keys and the deleted job IDs so their
// files can be removed.
func (s *Store) PurgeRevokedAPIKeys(ctx context.Context, now time.Time) (int64, []string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("store: purge revoked API keys: %w", err)
	}
	keys, ids, err := purgeRevokedAPIKeys(ctx, tx, now)
	if err != nil {
		return 0, nil, errors.Join(err, tx.Rollback())
	}
	if err := tx.Commit(); err != nil {
		return 0, nil, fmt.Errorf("store: purge revoked API keys: %w", err)
	}

	return keys, ids, nil
}

func purgeRevokedAPIKeys(ctx context.Context, tx *sql.Tx, now time.Time) (int64, []string, error) {
	const revoked = "SELECT id FROM api_keys WHERE revoked_at IS NOT NULL"
	// Cancel first so a worker polling the row observes cancellation even if it reads before the delete commits.
	if _, err := tx.ExecContext(ctx,
		`UPDATE jobs SET status = 'canceled', error_code = NULL, updated_at = ?, finished_at = ?
		WHERE owner_id IN (`+revoked+`) AND status IN ('queued', 'running')`,
		toMillis(now), toMillis(now)); err != nil {
		return 0, nil, fmt.Errorf("store: purge revoked API keys: %w", err)
	}
	rows, err := tx.QueryContext(ctx, "DELETE FROM jobs WHERE owner_id IN ("+revoked+") RETURNING id")
	if err != nil {
		return 0, nil, fmt.Errorf("store: purge revoked API keys: %w", err)
	}
	ids, err := scanIDs(rows)
	if err != nil {
		return 0, nil, err
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM api_keys WHERE revoked_at IS NOT NULL")
	if err != nil {
		return 0, nil, fmt.Errorf("store: purge revoked API keys: %w", err)
	}
	keys, err := result.RowsAffected()
	if err != nil {
		return 0, nil, fmt.Errorf("store: purge revoked API keys: %w", err)
	}

	return keys, ids, nil
}

// TouchAPIKey records that the key was used at now. It returns ErrNotFound for unknown IDs.
func (s *Store) TouchAPIKey(ctx context.Context, id string, now time.Time) error {
	err := requireAffected(s.db.ExecContext(ctx,
		"UPDATE api_keys SET last_used_at = ? WHERE id = ?", toMillis(now), id))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("store: touch API key: %w", err)
	}

	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanAPIKey(row scanner) (APIKey, error) {
	var (
		key                 APIKey
		createdAt           int64
		lastUsed, revokedAt sql.NullInt64
	)
	if err := row.Scan(&key.ID, &key.Name, &key.SecretHash, &createdAt, &lastUsed, &revokedAt); err != nil {
		return APIKey{}, err
	}
	key.CreatedAt = fromMillis(createdAt)
	key.LastUsedAt = fromNullableMillis(lastUsed)
	key.RevokedAt = fromNullableMillis(revokedAt)

	return key, nil
}
