package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"pulsar/internal/models"
)

// BucketsRepo exposes bucket persistence.
type BucketsRepo struct {
	db *DB
}

func NewBucketsRepo(db *DB) *BucketsRepo { return &BucketsRepo{db: db} }

// Create inserts a new bucket. Returns ErrConflict when (user_id, name) exists.
func (r *BucketsRepo) Create(ctx context.Context, userID uuid.UUID, name, region string, visibility models.BucketVisibility, cdnEnabled bool) (*models.Bucket, error) {
	if visibility == "" {
		visibility = models.BucketPrivate
	}
	const q = `
        INSERT INTO buckets (user_id, name, region, visibility, cdn_enabled)
        VALUES ($1, $2, $3, $4, $5)
        RETURNING id, user_id, name, region, visibility, cdn_enabled, created_at, updated_at`
	var b models.Bucket
	err := r.db.Pool.QueryRow(ctx, q, userID, name, region, string(visibility), cdnEnabled).Scan(
		&b.ID, &b.UserID, &b.Name, &b.Region, &b.Visibility, &b.CDNEnabled, &b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		if IsUniqueViolation(err) {
			return nil, Wrap(models.ErrConflict, "bucket name already taken")
		}
		return nil, err
	}
	return &b, nil
}

// FindByID returns a bucket by id, scoped to the given user.
func (r *BucketsRepo) FindByID(ctx context.Context, userID, id uuid.UUID) (*models.Bucket, error) {
	const q = `
        SELECT id, user_id, name, region, visibility, cdn_enabled, created_at, updated_at
        FROM buckets WHERE id = $1 AND user_id = $2`
	var b models.Bucket
	err := r.db.Pool.QueryRow(ctx, q, id, userID).Scan(
		&b.ID, &b.UserID, &b.Name, &b.Region, &b.Visibility, &b.CDNEnabled, &b.CreatedAt, &b.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// FindByName returns a bucket by name for the user.
func (r *BucketsRepo) FindByName(ctx context.Context, userID uuid.UUID, name string) (*models.Bucket, error) {
	const q = `
        SELECT id, user_id, name, region, visibility, cdn_enabled, created_at, updated_at
        FROM buckets WHERE user_id = $1 AND name = $2`
	var b models.Bucket
	err := r.db.Pool.QueryRow(ctx, q, userID, name).Scan(
		&b.ID, &b.UserID, &b.Name, &b.Region, &b.Visibility, &b.CDNEnabled, &b.CreatedAt, &b.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// List returns buckets owned by a user, newest first.
func (r *BucketsRepo) List(ctx context.Context, userID uuid.UUID) ([]models.Bucket, error) {
	const q = `
        SELECT id, user_id, name, region, visibility, cdn_enabled, created_at, updated_at
        FROM buckets WHERE user_id = $1 ORDER BY created_at DESC`
	rows, err := r.db.Pool.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Bucket
	for rows.Next() {
		var b models.Bucket
		if err := rows.Scan(&b.ID, &b.UserID, &b.Name, &b.Region, &b.Visibility, &b.CDNEnabled, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// Update mutates mutable fields. Returns ErrNotFound if not owned.
func (r *BucketsRepo) Update(ctx context.Context, userID, id uuid.UUID, visibility models.BucketVisibility, cdnEnabled *bool) (*models.Bucket, error) {
	// Coalesce optional fields via COALESCE.
	q := `
        UPDATE buckets
        SET visibility = COALESCE(NULLIF($3,''), visibility),
            cdn_enabled = COALESCE($4, cdn_enabled)
        WHERE id = $1 AND user_id = $2
        RETURNING id, user_id, name, region, visibility, cdn_enabled, created_at, updated_at`
	var vis any
	if visibility != "" {
		vis = string(visibility)
	}
	var b models.Bucket
	err := r.db.Pool.QueryRow(ctx, q, id, userID, vis, cdnEnabled).Scan(
		&b.ID, &b.UserID, &b.Name, &b.Region, &b.Visibility, &b.CDNEnabled, &b.CreatedAt, &b.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update bucket: %w", err)
	}
	return &b, nil
}

// Delete removes a bucket (and its objects, via cascade).
func (r *BucketsRepo) Delete(ctx context.Context, userID, id uuid.UUID) error {
	const q = `DELETE FROM buckets WHERE id = $1 AND user_id = $2`
	tag, err := r.db.Pool.Exec(ctx, q, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return models.ErrNotFound
	}
	return nil
}

// CountByUser returns the number of buckets owned by a user (for quota checks).
func (r *BucketsRepo) CountByUser(ctx context.Context, userID uuid.UUID) (int, error) {
	const q = `SELECT count(*) FROM buckets WHERE user_id = $1`
	var n int
	err := r.db.Pool.QueryRow(ctx, q, userID).Scan(&n)
	return n, err
}
