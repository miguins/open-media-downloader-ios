package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/job"
)

// Bundle returns metadata for a bundle belonging to ownerID.
func (s *Store) Bundle(ctx context.Context, ownerID, jobID string) (job.Bundle, error) {
	var b job.Bundle
	var created int64
	err := s.db.QueryRowContext(ctx, `SELECT b.job_id,b.file_name,b.size_bytes,b.created_at
 FROM bundles b JOIN jobs j ON j.id=b.job_id WHERE j.owner_id=? AND j.id=?`, ownerID, jobID).
		Scan(&b.JobID, &b.FileName, &b.SizeBytes, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return job.Bundle{}, ErrNotFound
	}
	if err != nil {
		return job.Bundle{}, fmt.Errorf("store: get bundle: %w", err)
	}
	b.CreatedAt = fromMillis(created)
	return b, nil
}

// ReplaceBundleToken replaces the current token only for its authorized job owner.
func (s *Store) ReplaceBundleToken(ctx context.Context, token job.BundleToken) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: replace bundle token: %w", err)
	}
	err = replaceBundleToken(ctx, tx, token)
	if err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("store: replace bundle token: %w", err)
	}
	return nil
}

func replaceBundleToken(ctx context.Context, tx *sql.Tx, token job.BundleToken) error {
	var exists bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM bundles b JOIN jobs j ON j.id=b.job_id
 WHERE j.id=? AND j.owner_id=? AND j.status='succeeded' AND j.expires_at>?)`, token.JobID, token.OwnerID, toMillis(token.CreatedAt)).Scan(&exists)
	if err != nil {
		return fmt.Errorf("store: authorize bundle token: %w", err)
	}
	if !exists {
		return ErrNotFound
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM bundle_tokens WHERE job_id=?", token.JobID); err != nil {
		return fmt.Errorf("store: replace bundle token: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO bundle_tokens(token_hash,job_id,owner_id,created_at,expires_at) VALUES(?,?,?,?,?)`, token.Hash, token.JobID, token.OwnerID, toMillis(token.CreatedAt), toMillis(token.ExpiresAt))
	if err != nil {
		return fmt.Errorf("store: replace bundle token: %w", err)
	}
	return nil
}

// ValidBundleToken resolves only a live token for a retained succeeded job.
func (s *Store) ValidBundleToken(ctx context.Context, hash []byte, now time.Time) (job.BundleToken, error) {
	var token job.BundleToken
	var created, expires int64
	err := s.db.QueryRowContext(ctx, `SELECT t.token_hash,t.job_id,t.owner_id,t.created_at,t.expires_at
 FROM bundle_tokens t JOIN bundles b ON b.job_id=t.job_id JOIN jobs j ON j.id=b.job_id
 WHERE t.token_hash=? AND t.expires_at>? AND j.expires_at>? AND j.status='succeeded' AND j.owner_id=t.owner_id`, hash, toMillis(now), toMillis(now)).
		Scan(&token.Hash, &token.JobID, &token.OwnerID, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return job.BundleToken{}, ErrNotFound
	}
	if err != nil {
		return job.BundleToken{}, fmt.Errorf("store: get bundle token: %w", err)
	}
	token.CreatedAt = fromMillis(created)
	token.ExpiresAt = fromMillis(expires)
	return token, nil
}

// DeleteExpiredBundleTokens removes expired credentials without deleting media.
func (s *Store) DeleteExpiredBundleTokens(ctx context.Context, now time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, "DELETE FROM bundle_tokens WHERE expires_at<=?", toMillis(now))
	if err != nil {
		return 0, fmt.Errorf("store: delete expired bundle tokens: %w", err)
	}
	return result.RowsAffected()
}
