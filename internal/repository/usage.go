package repository

import (
	"context"

	"github.com/google/uuid"

	"pulsar/internal/models"
)

// UsageRepo records consumption events and computes aggregated usage.
type UsageRepo struct {
	db *DB
}

func NewUsageRepo(db *DB) *UsageRepo { return &UsageRepo{db: db} }

// Record writes a single usage event.
func (r *UsageRepo) Record(ctx context.Context, userID uuid.UUID, typ models.UsageType, amount int64, bucketID *uuid.UUID) error {
	const q = `
        INSERT INTO usage_events (user_id, type, amount, bucket_id)
        VALUES ($1, $2, $3, $4)`
	_, err := r.db.Pool.Exec(ctx, q, userID, string(typ), amount, bucketID)
	return err
}

// Summary aggregates the latest counters for a user. Storage is computed
// from objects directly (the source of truth); bandwidth & api_calls from
// the rolling 30-day window of usage_events.
func (r *UsageRepo) Summary(ctx context.Context, userID uuid.UUID) (models.UsageSummary, error) {
	var s models.UsageSummary
	// Storage bytes from live objects.
	if err := r.db.Pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(size),0) FROM objects o
         JOIN buckets b ON b.id = o.bucket_id WHERE b.user_id = $1`, userID,
	).Scan(&s.StorageBytes); err != nil {
		return s, err
	}
	// Bandwidth over trailing 30 days.
	if err := r.db.Pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount),0) FROM usage_events
         WHERE user_id = $1 AND type = 'bandwidth_bytes'
         AND recorded_at > now() - interval '30 days'`, userID,
	).Scan(&s.BandwidthBytes); err != nil {
		return s, err
	}
	// API calls trailing 30 days.
	if err := r.db.Pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount),0) FROM usage_events
         WHERE user_id = $1 AND type = 'api_calls'
         AND recorded_at > now() - interval '30 days'`, userID,
	).Scan(&s.APICalls); err != nil {
		return s, err
	}
	return s, nil
}
