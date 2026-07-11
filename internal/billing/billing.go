// Package billing wraps the Stripe SDK for subscription management. It is
// intentionally resilient to a missing API key: when STRIPE_SECRET_KEY is empty
// the service runs in "no-provider" mode (plans still render, but Checkout and
// the Portal are disabled). This lets local development work without a Stripe
// account.
package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	stripe "github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/webhook"
	stripeclient "github.com/stripe/stripe-go/v76/client"

	"pulsar/internal/config"
	"pulsar/internal/models"
	"pulsar/internal/repository"
)

// Service orchestrates Stripe-backed subscription lifecycle.
type Service struct {
	cfg    config.StripeConfig
	client *stripeclient.API
	plans  *repository.PlansRepo
	subs   *repository.SubscriptionsRepo
	users  *repository.UsersRepo
}

// New constructs a billing service. Returns nil + no error when the secret key
// is unset (no-provider mode); callers must check Enabled().
func New(cfg config.StripeConfig, plans *repository.PlansRepo, subs *repository.SubscriptionsRepo, users *repository.UsersRepo) *Service {
	s := &Service{cfg: cfg, plans: plans, subs: subs, users: users}
	if cfg.SecretKey != "" {
		sc := &stripeclient.API{}
		sc.Init(cfg.SecretKey, nil)
		s.client = sc
	}
	return s
}

// Enabled reports whether Stripe is configured.
func (s *Service) Enabled() bool { return s != nil && s.client != nil }

// PriceIDForPlan returns the configured Stripe price id for a plan + interval.
// Returns "" if not configured.
func (s *Service) PriceIDForPlan(planSlug, interval string) string {
	switch {
	case planSlug == "pro" && interval == "monthly":
		return s.cfg.PriceMonthlyPro
	case planSlug == "pro" && interval == "yearly":
		return s.cfg.PriceYearlyPro
	case planSlug == "business" && interval == "monthly":
		return s.cfg.PriceMonthlyBiz
	case planSlug == "business" && interval == "yearly":
		return s.cfg.PriceYearlyBiz
	}
	return ""
}

// CreateCheckoutURL builds a Stripe Checkout Session URL for subscribing to a
// plan. The customer is created/reused by email.
func (s *Service) CreateCheckoutURL(ctx context.Context, userID uuid.UUID, planSlug, interval, successURL, cancelURL string) (string, error) {
	if !s.Enabled() {
		return "", errors.New("billing is not configured")
	}
	priceID := s.PriceIDForPlan(planSlug, interval)
	if priceID == "" {
		return "", fmt.Errorf("no stripe price configured for plan %s/%s", planSlug, interval)
	}
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return "", err
	}
	params := &stripe.CheckoutSessionParams{
		Mode: stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{{
			Price:    stripe.String(priceID),
			Quantity: stripe.Int64(1),
		}},
		SuccessURL:        stripe.String(successURL),
		CancelURL:         stripe.String(cancelURL),
		ClientReferenceID: stripe.String(userID.String()),
		CustomerEmail:     stripe.String(user.Email),
		SubscriptionData: &stripe.CheckoutSessionSubscriptionDataParams{
			Metadata: map[string]string{"user_id": userID.String()},
		},
	}
	sess, err := s.client.CheckoutSessions.New(params)
	if err != nil {
		return "", fmt.Errorf("create checkout session: %w", err)
	}
	return sess.URL, nil
}

// CreatePortalURL builds a Stripe Billing Portal session for self-service
// management (invoices, payment methods, cancellation).
func (s *Service) CreatePortalURL(ctx context.Context, userID uuid.UUID, returnURL string) (string, error) {
	if !s.Enabled() {
		return "", errors.New("billing is not configured")
	}
	sub, err := s.subs.FindByUser(ctx, userID)
	if err != nil {
		return "", err
	}
	if sub.StripeCustomerID == "" {
		return "", errors.New("no stripe customer found; subscribe first")
	}
	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(sub.StripeCustomerID),
		ReturnURL: stripe.String(returnURL),
	}
	sess, err := s.client.BillingPortalSessions.New(params)
	if err != nil {
		return "", fmt.Errorf("create portal session: %w", err)
	}
	return sess.URL, nil
}

// ApplyWebhook processes a Stripe webhook payload. It verifies the signature
// and updates the local subscription state for the relevant events.
func (s *Service) ApplyWebhook(ctx context.Context, payload []byte, signature string) error {
	if !s.Enabled() {
		return errors.New("billing is not configured")
	}
	event, err := webhook.ConstructEvent(payload, signature, s.cfg.WebhookSecret)
	if err != nil {
		return fmt.Errorf("verify webhook signature: %w", err)
	}
	return s.handleEvent(ctx, event)
}

// handleEvent maps Stripe events to local state mutations.
func (s *Service) handleEvent(ctx context.Context, event stripe.Event) error {
	switch event.Type {
	case "checkout.session.completed":
		var sess stripe.CheckoutSession
		if err := parseEvent(event, &sess); err != nil {
			return err
		}
		_ = sess
		// Subscription details arrive via the customer.subscription.created/
		// updated events; we defer state mutation to those.
	case "customer.subscription.created", "customer.subscription.updated", "customer.subscription.deleted":
		var sub stripe.Subscription
		if err := parseEvent(event, &sub); err != nil {
			return err
		}
		return s.syncSubscription(ctx, &sub)
	}
	return nil
}

// syncSubscription upserts our subscriptions row from a Stripe Subscription.
// The user id is taken from subscription metadata (set at checkout time).
func (s *Service) syncSubscription(ctx context.Context, sub *stripe.Subscription) error {
	raw := ""
	if sub.Metadata != nil {
		raw = sub.Metadata["user_id"]
	}
	userID, err := uuid.Parse(raw)
	if err != nil {
		// No user mapping; we cannot attribute this subscription. Skip silently.
		return nil
	}
	planSlug := s.priceToPlanSlug(sub)
	plan, err := s.plans.FindBySlug(ctx, planSlug)
	if err != nil {
		plan, err = s.plans.FindBySlug(ctx, "free")
		if err != nil {
			return err
		}
	}
	status := mapSubStatus(sub.Status)
	customerID, subscriptionID := "", ""
	if sub.Customer != nil {
		customerID = sub.Customer.ID
	}
	subscriptionID = sub.ID
	return s.subs.Upsert(ctx, userID, plan.ID, status, customerID, subscriptionID)
}

// priceToPlanSlug maps a configured Stripe price back to a plan slug.
func (s *Service) priceToPlanSlug(sub *stripe.Subscription) string {
	if len(sub.Items.Data) == 0 {
		return "free"
	}
	priceID := sub.Items.Data[0].Price.ID
	switch priceID {
	case s.cfg.PriceMonthlyPro, s.cfg.PriceYearlyPro:
		return "pro"
	case s.cfg.PriceMonthlyBiz, s.cfg.PriceYearlyBiz:
		return "business"
	}
	return "free"
}

// --- helpers ---

// parseEvent unmarshals the stripe event payload into v. stripe v76 exposes
// event.Data.Raw as raw JSON; we decode it directly.
func parseEvent(event stripe.Event, v any) error {
	if len(event.Data.Raw) == 0 {
		return errors.New("empty event payload")
	}
	return json.Unmarshal(event.Data.Raw, v)
}

func mapSubStatus(s stripe.SubscriptionStatus) models.SubscriptionStatus {
	switch s {
	case stripe.SubscriptionStatusTrialing:
		return models.SubStatusTrialing
	case stripe.SubscriptionStatusActive:
		return models.SubStatusActive
	case stripe.SubscriptionStatusPastDue:
		return models.SubStatusPastDue
	case stripe.SubscriptionStatusCanceled:
		return models.SubStatusCanceled
	default:
		return models.SubStatusIncomplete
	}
}
