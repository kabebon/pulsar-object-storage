package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"pulsar/internal/models"
)

// PlansRepo reads billing plan definitions.
type PlansRepo struct {
	db *DB
}

func NewPlansRepo(db *DB) *PlansRepo { return &PlansRepo{db: db} }

// All returns all plans ordered by sort_order.
func (r *PlansRepo) All(ctx context.Context) ([]models.Plan, error) {
	const q = `
        SELECT id, slug, name, storage_gb, bandwidth_gb_month, price_monthly_cents,
               price_yearly_cents, max_buckets, custom_domains_allowed
        FROM plans ORDER BY sort_order ASC`
	rows, err := r.db.Pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Plan
	for rows.Next() {
		var p models.Plan
		if err := rows.Scan(&p.ID, &p.Slug, &p.Name, &p.StorageGB, &p.BandwidthGBMonth,
			&p.PriceMonthlyCents, &p.PriceYearlyCents, &p.MaxBuckets, &p.CustomDomainsAllowed); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// FindBySlug returns a single plan by its slug.
func (r *PlansRepo) FindBySlug(ctx context.Context, slug string) (*models.Plan, error) {
	const q = `
        SELECT id, slug, name, storage_gb, bandwidth_gb_month, price_monthly_cents,
               price_yearly_cents, max_buckets, custom_domains_allowed
        FROM plans WHERE slug = $1`
	var p models.Plan
	err := r.db.Pool.QueryRow(ctx, q, slug).Scan(
		&p.ID, &p.Slug, &p.Name, &p.StorageGB, &p.BandwidthGBMonth,
		&p.PriceMonthlyCents, &p.PriceYearlyCents, &p.MaxBuckets, &p.CustomDomainsAllowed,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// FindByID returns a plan by id.
func (r *PlansRepo) FindByID(ctx context.Context, id uuid.UUID) (*models.Plan, error) {
	const q = `
        SELECT id, slug, name, storage_gb, bandwidth_gb_month, price_monthly_cents,
               price_yearly_cents, max_buckets, custom_domains_allowed
        FROM plans WHERE id = $1`
	var p models.Plan
	err := r.db.Pool.QueryRow(ctx, q, id).Scan(
		&p.ID, &p.Slug, &p.Name, &p.StorageGB, &p.BandwidthGBMonth,
		&p.PriceMonthlyCents, &p.PriceYearlyCents, &p.MaxBuckets, &p.CustomDomainsAllowed,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// SubscriptionsRepo reads/writes user subscriptions.
type SubscriptionsRepo struct {
	db *DB
}

func NewSubscriptionsRepo(db *DB) *SubscriptionsRepo { return &SubscriptionsRepo{db: db} }

// FindByUser returns the user's active subscription, joined with plan slug.
func (r *SubscriptionsRepo) FindByUser(ctx context.Context, userID uuid.UUID) (*models.Subscription, error) {
	const q = `
        SELECT s.user_id, s.plan_id, p.slug, s.status, s.stripe_customer_id,
               s.stripe_subscription_id, s.current_period_end, s.created_at, s.updated_at
        FROM subscriptions s
        JOIN plans p ON p.id = s.plan_id
        WHERE s.user_id = $1`
	var s models.Subscription
	err := r.db.Pool.QueryRow(ctx, q, userID).Scan(
		&s.UserID, &s.PlanID, &s.PlanSlug, &s.Status, &s.StripeCustomerID,
		&s.StripeSubscriptionID, &s.CurrentPeriodEnd, &s.CreatedAt, &s.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// Upsert sets the user's plan, status and Stripe identifiers.
func (r *SubscriptionsRepo) Upsert(ctx context.Context, userID, planID uuid.UUID, status models.SubscriptionStatus, customerID, subscriptionID string) error {
	const q = `
        INSERT INTO subscriptions (user_id, plan_id, status, stripe_customer_id, stripe_subscription_id)
        VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($5,''))
        ON CONFLICT (user_id) DO UPDATE
          SET plan_id = EXCLUDED.plan_id,
              status = EXCLUDED.status,
              stripe_customer_id = EXCLUDED.stripe_customer_id,
              stripe_subscription_id = EXCLUDED.stripe_subscription_id,
              updated_at = now()`
	_, err := r.db.Pool.Exec(ctx, q, userID, planID, string(status), customerID, subscriptionID)
	return err
}

// SetPeriodEnd updates the current_period_end column.
func (r *SubscriptionsRepo) SetPeriodEnd(ctx context.Context, userID uuid.UUID, periodEnd interface{}) error {
	const q = `UPDATE subscriptions SET current_period_end = $2, updated_at = now() WHERE user_id = $1`
	_, err := r.db.Pool.Exec(ctx, q, userID, periodEnd)
	return err
}
