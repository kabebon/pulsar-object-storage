package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"pulsar/internal/models"
)

// EmailVerificationsRepo stores single-use tokens for signup/reset flows.
type EmailVerificationsRepo struct {
	db *DB
}

func NewEmailVerificationsRepo(db *DB) *EmailVerificationsRepo {
	return &EmailVerificationsRepo{db: db}
}

// Create persists a new token (hash) for the given user and purpose.
func (r *EmailVerificationsRepo) Create(ctx context.Context, userID uuid.UUID, tokenHash string, typ models.VerificationType, ttl time.Duration) (*models.EmailVerification, error) {
	const q = `
        INSERT INTO email_verifications (user_id, token_hash, type, expires_at)
        VALUES ($1, $2, $3, now() + $4)
        RETURNING id, user_id, type, expires_at, consumed_at, created_at`
	var ev models.EmailVerification
	ev.Token = ""
	err := r.db.Pool.QueryRow(ctx, q, userID, tokenHash, string(typ), ttl).Scan(
		&ev.ID, &ev.UserID, &ev.Type, &ev.ExpiresAt, &ev.ConsumedAt, &ev.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &ev, nil
}

// FindByTokenHash returns the active (unconsumed, non-expired) verification by hash.
func (r *EmailVerificationsRepo) FindByTokenHash(ctx context.Context, tokenHash string) (*models.EmailVerification, error) {
	const q = `
        SELECT id, user_id, type, expires_at, consumed_at, created_at
        FROM email_verifications
        WHERE token_hash = $1 AND consumed_at IS NULL`
	var ev models.EmailVerification
	err := r.db.Pool.QueryRow(ctx, q, tokenHash).Scan(
		&ev.ID, &ev.UserID, &ev.Type, &ev.ExpiresAt, &ev.ConsumedAt, &ev.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !ev.ExpiresAt.IsZero() && time.Now().After(ev.ExpiresAt) {
		return nil, models.ErrTokenExpired
	}
	return &ev, nil
}

// MarkConsumed marks a verification token as consumed (single-use enforcement).
func (r *EmailVerificationsRepo) MarkConsumed(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE email_verifications SET consumed_at = now() WHERE id = $1`
	tag, err := r.db.Pool.Exec(ctx, q, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return models.ErrNotFound
	}
	return nil
}

// InvalidateType marks all existing tokens of a type for a user as consumed.
// Useful when issuing a new reset password token.
func (r *EmailVerificationsRepo) InvalidateType(ctx context.Context, userID uuid.UUID, typ models.VerificationType) error {
	const q = `UPDATE email_verifications SET consumed_at = now()
               WHERE user_id = $1 AND type = $2 AND consumed_at IS NULL`
	_, err := r.db.Pool.Exec(ctx, q, userID, string(typ))
	return err
}
