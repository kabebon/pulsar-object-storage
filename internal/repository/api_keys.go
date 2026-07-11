package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"pulsar/internal/models"
)

// APIKeysRepo persists long-lived API tokens.
type APIKeysRepo struct {
	db *DB
}

func NewAPIKeysRepo(db *DB) *APIKeysRepo { return &APIKeysRepo{db: db} }

// Create inserts a new API key. keyHash is sha256 of the full secret;
// keyPrefix is the displayed prefix (e.g. "pk_live_abc123…").
func (r *APIKeysRepo) Create(ctx context.Context, userID uuid.UUID, name, keyHash, keyPrefix string, scopes []string) (*models.APIKey, error) {
	if len(scopes) == 0 {
		scopes = []string{"*"}
	}
	const q = `
        INSERT INTO api_keys (user_id, name, key_prefix, key_hash, scopes)
        VALUES ($1, $2, $3, $4, $5)
        RETURNING id, user_id, name, key_prefix, scopes, last_used_at, created_at`
	var k models.APIKey
	err := r.db.Pool.QueryRow(ctx, q, userID, name, keyPrefix, keyHash, scopes).Scan(
		&k.ID, &k.UserID, &k.Name, &k.KeyPrefix, &k.Scopes, &k.LastUsedAt, &k.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// FindByHash looks up an API key by its hash (used during bearer auth).
func (r *APIKeysRepo) FindByHash(ctx context.Context, keyHash string) (*models.APIKey, error) {
	const q = `
        SELECT id, user_id, name, key_prefix, scopes, last_used_at, created_at
        FROM api_keys WHERE key_hash = $1`
	var k models.APIKey
	err := r.db.Pool.QueryRow(ctx, q, keyHash).Scan(
		&k.ID, &k.UserID, &k.Name, &k.KeyPrefix, &k.Scopes, &k.LastUsedAt, &k.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// List returns all keys for a user (without the hash).
func (r *APIKeysRepo) List(ctx context.Context, userID uuid.UUID) ([]models.APIKey, error) {
	const q = `
        SELECT id, user_id, name, key_prefix, scopes, last_used_at, created_at
        FROM api_keys WHERE user_id = $1 ORDER BY created_at DESC`
	rows, err := r.db.Pool.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.APIKey
	for rows.Next() {
		var k models.APIKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.KeyPrefix, &k.Scopes, &k.LastUsedAt, &k.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// Delete removes an API key.
func (r *APIKeysRepo) Delete(ctx context.Context, userID, id uuid.UUID) error {
	const q = `DELETE FROM api_keys WHERE id = $1 AND user_id = $2`
	tag, err := r.db.Pool.Exec(ctx, q, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return models.ErrNotFound
	}
	return nil
}

// TouchLastUsed updates the last_used_at timestamp for a key.
func (r *APIKeysRepo) TouchLastUsed(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE api_keys SET last_used_at = now() WHERE id = $1`
	_, err := r.db.Pool.Exec(ctx, q, id)
	return err
}
