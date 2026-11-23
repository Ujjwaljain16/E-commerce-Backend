package account

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrRefreshTokenUnknown is returned for a refresh token that was never issued, has expired, or whose user logged out.
	ErrRefreshTokenUnknown = errors.New("refresh token not recognised")
	// ErrRefreshTokenReused is returned when an already-rotated refresh token is presented again,
	// which means a copy of it leaked; every session of that user is revoked.
	ErrRefreshTokenReused = errors.New("refresh token reused")
)

// TokenStore tracks issued refresh tokens so they can be rotated and revoked.
type TokenStore interface {
	Save(ctx context.Context, jti, userID string, expiresAt time.Time) error
	// Consume marks a refresh token as used and returns its owner. A token can be consumed once.
	Consume(ctx context.Context, jti string) (userID string, err error)
	// Revoke invalidates one refresh token (logout). Unknown tokens are ignored.
	Revoke(ctx context.Context, jti string) error
	// RevokeAll invalidates every refresh token of a user.
	RevokeAll(ctx context.Context, userID string) error
}

type postgresTokenStore struct {
	db *sql.DB
}

// NewPostgresTokenStore stores refresh tokens in the refresh_tokens table.
func NewPostgresTokenStore(db *sql.DB) TokenStore {
	return &postgresTokenStore{db: db}
}

func (s *postgresTokenStore) Save(ctx context.Context, jti, userID string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO refresh_tokens (jti, user_id, expires_at) VALUES ($1, $2, $3)`, jti, userID, expiresAt)
	if err != nil {
		return fmt.Errorf("save refresh token: %w", err)
	}
	return nil
}

func (s *postgresTokenStore) Consume(ctx context.Context, jti string) (string, error) {
	var userID string
	err := s.db.QueryRowContext(ctx, `
		UPDATE refresh_tokens SET used_at = NOW()
		WHERE jti = $1 AND used_at IS NULL AND revoked_at IS NULL AND expires_at > NOW()
		RETURNING user_id`, jti).Scan(&userID)
	if err == nil {
		return userID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("consume refresh token: %w", err)
	}

	var owner string
	var used bool
	err = s.db.QueryRowContext(ctx,
		`SELECT user_id, used_at IS NOT NULL FROM refresh_tokens WHERE jti = $1`, jti).Scan(&owner, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrRefreshTokenUnknown
	}
	if err != nil {
		return "", fmt.Errorf("look up refresh token: %w", err)
	}
	if used {
		if err := s.RevokeAll(ctx, owner); err != nil {
			return "", err
		}
		return "", ErrRefreshTokenReused
	}
	return "", ErrRefreshTokenUnknown
}

func (s *postgresTokenStore) Revoke(ctx context.Context, jti string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE refresh_tokens SET revoked_at = NOW() WHERE jti = $1 AND revoked_at IS NULL`, jti)
	if err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	return nil
}

func (s *postgresTokenStore) RevokeAll(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked_at = NOW() WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	if err != nil {
		return fmt.Errorf("revoke user refresh tokens: %w", err)
	}
	return nil
}

// PruneExpiredRefreshTokens deletes refresh tokens that expired more than a day ago.
func PruneExpiredRefreshTokens(ctx context.Context, db *sql.DB) (int64, error) {
	res, err := db.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE expires_at < NOW() - INTERVAL '1 day'`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
