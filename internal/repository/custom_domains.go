package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"pulsar/internal/models"
)

// CustomDomainsRepo persists custom-domain mappings for CDN.
type CustomDomainsRepo struct {
	db *DB
}

func NewCustomDomainsRepo(db *DB) *CustomDomainsRepo { return &CustomDomainsRepo{db: db} }

// Create inserts a new custom domain mapping.
func (r *CustomDomainsRepo) Create(ctx context.Context, userID, bucketID uuid.UUID, domain string) (*models.CustomDomain, error) {
	const q = `
        INSERT INTO custom_domains (user_id, bucket_id, domain)
        VALUES ($1, $2, $3)
        RETURNING id, user_id, bucket_id, domain, dns_status, ssl_status, created_at, updated_at`
	var d models.CustomDomain
	err := r.db.Pool.QueryRow(ctx, q, userID, bucketID, domain).Scan(
		&d.ID, &d.UserID, &d.BucketID, &d.Domain, &d.DNSStatus, &d.SSLStatus, &d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		if IsUniqueViolation(err) {
			return nil, Wrap(models.ErrConflict, "domain already registered")
		}
		return nil, err
	}
	return &d, nil
}

// FindByID returns a domain, scoped to a user.
func (r *CustomDomainsRepo) FindByID(ctx context.Context, userID, id uuid.UUID) (*models.CustomDomain, error) {
	const q = `
        SELECT id, user_id, bucket_id, domain, dns_status, ssl_status, created_at, updated_at
        FROM custom_domains WHERE id = $1 AND user_id = $2`
	var d models.CustomDomain
	err := r.db.Pool.QueryRow(ctx, q, id, userID).Scan(
		&d.ID, &d.UserID, &d.BucketID, &d.Domain, &d.DNSStatus, &d.SSLStatus, &d.CreatedAt, &d.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// FindByDomain looks up a domain by hostname (used for on-demand TLS ask).
func (r *CustomDomainsRepo) FindByDomain(ctx context.Context, domain string) (*models.CustomDomain, error) {
	const q = `
        SELECT id, user_id, bucket_id, domain, dns_status, ssl_status, created_at, updated_at
        FROM custom_domains WHERE domain = $1`
	var d models.CustomDomain
	err := r.db.Pool.QueryRow(ctx, q, domain).Scan(
		&d.ID, &d.UserID, &d.BucketID, &d.Domain, &d.DNSStatus, &d.SSLStatus, &d.CreatedAt, &d.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// List returns all domains for a user.
func (r *CustomDomainsRepo) List(ctx context.Context, userID uuid.UUID) ([]models.CustomDomain, error) {
	const q = `
        SELECT id, user_id, bucket_id, domain, dns_status, ssl_status, created_at, updated_at
        FROM custom_domains WHERE user_id = $1 ORDER BY created_at DESC`
	rows, err := r.db.Pool.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.CustomDomain
	for rows.Next() {
		var d models.CustomDomain
		if err := rows.Scan(&d.ID, &d.UserID, &d.BucketID, &d.Domain, &d.DNSStatus, &d.SSLStatus, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// UpdateStatus sets dns_status / ssl_status.
func (r *CustomDomainsRepo) UpdateStatus(ctx context.Context, userID, id uuid.UUID, dns models.DNSStatus, ssl models.SSLStatus) error {
	q := `
        UPDATE custom_domains
        SET dns_status = COALESCE(NULLIF($3,''), dns_status),
            ssl_status = COALESCE(NULLIF($4,''), ssl_status)
        WHERE id = $1 AND user_id = $2`
	tag, err := r.db.Pool.Exec(ctx, q, id, userID, string(dns), string(ssl))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return models.ErrNotFound
	}
	return nil
}

// Delete removes a domain mapping.
func (r *CustomDomainsRepo) Delete(ctx context.Context, userID, id uuid.UUID) error {
	const q = `DELETE FROM custom_domains WHERE id = $1 AND user_id = $2`
	tag, err := r.db.Pool.Exec(ctx, q, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return models.ErrNotFound
	}
	return nil
}

// CountByUser returns how many custom domains a user has (for quota).
func (r *CustomDomainsRepo) CountByUser(ctx context.Context, userID uuid.UUID) (int, error) {
	const q = `SELECT count(*) FROM custom_domains WHERE user_id = $1`
	var n int
	err := r.db.Pool.QueryRow(ctx, q, userID).Scan(&n)
	return n, err
}
