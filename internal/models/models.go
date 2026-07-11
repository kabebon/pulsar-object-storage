// Package models defines the core domain types used across services and handlers.
package models

import (
	"errors"
	"net/url"
	"time"

	"github.com/google/uuid"
)

// Common sentinel errors used by the service layer. Handlers map these to HTTP
// responses in a single place (internal/errors).
var (
	ErrNotFound          = errors.New("not found")
	ErrUnauthorized      = errors.New("unauthorized")
	ErrForbidden         = errors.New("forbidden")
	ErrConflict          = errors.New("conflict")
	ErrValidation        = errors.New("validation error")
	ErrQuotaExceeded     = errors.New("quota exceeded")
	ErrRateLimited       = errors.New("rate limited")
	ErrUnverifiedEmail   = errors.New("email not verified")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrTokenExpired      = errors.New("token expired")
	ErrTokenConsumed     = errors.New("token already used")
	ErrPaymentRequired   = errors.New("payment required")
)

// UserStatus represents the lifecycle state of a user account.
type UserStatus string

const (
	UserStatusActive    UserStatus = "active"
	UserStatusUnverified UserStatus = "unverified"
	UserStatusSuspended UserStatus = "suspended"
)

// User is a registered account owner.
type User struct {
	ID              uuid.UUID  `json:"id"`
	Email           string     `json:"email"`
	Name            string     `json:"name"`
	PasswordHash    string     `json:"-"`
	Status          UserStatus `json:"status"`
	EmailVerifiedAt *time.Time `json:"email_verified_at"`
	LastLoginAt     *time.Time `json:"last_login_at"`
	TOTPEnabled     bool       `json:"totp_enabled"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// VerificationType discriminates the kind of email verification token.
type VerificationType string

const (
	VerificationSignup VerificationType = "signup"
	VerificationReset  VerificationType = "reset"
)

// EmailVerification is a single-use token sent to a user's inbox.
type EmailVerification struct {
	ID        uuid.UUID        `json:"id"`
	UserID    uuid.UUID        `json:"user_id"`
	Token     string           `json:"-"`
	Type      VerificationType `json:"type"`
	ExpiresAt time.Time        `json:"expires_at"`
	ConsumedAt *time.Time      `json:"consumed_at"`
	CreatedAt time.Time        `json:"created_at"`
}

// BucketVisibility controls whether objects are reachable via public CDN URLs.
type BucketVisibility string

const (
	BucketPrivate BucketVisibility = "private"
	BucketPublic  BucketVisibility = "public"
)

// Bucket is a container of objects owned by a user.
type Bucket struct {
	ID         uuid.UUID        `json:"id"`
	UserID     uuid.UUID        `json:"user_id"`
	Name       string           `json:"name"`
	Region     string           `json:"region"`
	Visibility BucketVisibility `json:"visibility"`
	CDNEnabled bool             `json:"cdn_enabled"`
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

// Object is a stored blob within a bucket.
type Object struct {
	ID           uuid.UUID `json:"id"`
	BucketID     uuid.UUID `json:"bucket_id"`
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	ContentType  string    `json:"content_type"`
	ETag         string    `json:"etag"`
	SHA256       string    `json:"sha256"`
	Version      int       `json:"version"`
	StorageClass string    `json:"storage_class"`
	UploadedAt   time.Time `json:"uploaded_at"`
}

// APIKey is a long-lived credential used to call the REST API programmatically.
type APIKey struct {
	ID         uuid.UUID  `json:"id"`
	UserID     uuid.UUID  `json:"user_id"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"key_prefix"`
	KeyHash    string     `json:"-"`
	Scopes     []string   `json:"scopes"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// Plan is a billing tier.
type Plan struct {
	ID                    uuid.UUID `json:"id"`
	Slug                  string    `json:"slug"`
	Name                  string    `json:"name"`
	StorageGB             int64     `json:"storage_gb"`
	BandwidthGBMonth      int64     `json:"bandwidth_gb_month"`
	PriceMonthlyCents     int64     `json:"price_monthly_cents"`
	PriceYearlyCents      int64     `json:"price_yearly_cents"`
	MaxBuckets            int       `json:"max_buckets"`
	CustomDomainsAllowed  int       `json:"custom_domains_allowed"`
}

// SubscriptionStatus mirrors the relevant subset of Stripe subscription states.
type SubscriptionStatus string

const (
	SubStatusTrialing SubscriptionStatus = "trialing"
	SubStatusActive   SubscriptionStatus = "active"
	SubStatusPastDue  SubscriptionStatus = "past_due"
	SubStatusCanceled SubscriptionStatus = "canceled"
	SubStatusIncomplete SubscriptionStatus = "incomplete"
)

// Subscription links a user to a Stripe subscription and current plan.
type Subscription struct {
	UserID               uuid.UUID          `json:"user_id"`
	PlanID               uuid.UUID          `json:"plan_id"`
	PlanSlug             string             `json:"plan_slug"`
	Status               SubscriptionStatus `json:"status"`
	StripeCustomerID     string             `json:"stripe_customer_id"`
	StripeSubscriptionID string             `json:"stripe_subscription_id"`
	CurrentPeriodEnd     time.Time          `json:"current_period_end"`
	CreatedAt            time.Time          `json:"created_at"`
	UpdatedAt            time.Time          `json:"updated_at"`
}

// UsageType is a counter that feeds into billing & quota enforcement.
type UsageType string

const (
	UsageStorageBytes   UsageType = "storage_bytes"
	UsageBandwidthBytes UsageType = "bandwidth_bytes"
	UsageAPICalls       UsageType = "api_calls"
)

// UsageEvent is an immutable record of resource consumption.
type UsageEvent struct {
	ID         uuid.UUID  `json:"id"`
	UserID     uuid.UUID  `json:"user_id"`
	Type       UsageType  `json:"type"`
	Amount     int64      `json:"amount"`
	BucketID   *uuid.UUID `json:"bucket_id,omitempty"`
	RecordedAt time.Time  `json:"recorded_at"`
}

// UsageSummary aggregates the latest counters for a user.
type UsageSummary struct {
	StorageBytes   int64 `json:"storage_bytes"`
	BandwidthBytes int64 `json:"bandwidth_bytes"`
	APICalls       int64 `json:"api_calls"`
}

// DNSStatus reflects whether a custom domain is correctly pointed at Pulsar.
type DNSStatus string

const (
	DNSPending  DNSStatus = "pending"
	DNSVerified DNSStatus = "verified"
	DNSFailed   DNSStatus = "failed"
)

// SSLStatus reflects certificate issuance state.
type SSLStatus string

const (
	SSLPending  SSLStatus = "pending"
	SSLIssued   SSLStatus = "issued"
	SSLFailed   SSLStatus = "failed"
)

// CustomDomain maps a customer hostname to a bucket for CDN delivery.
type CustomDomain struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"user_id"`
	BucketID  uuid.UUID  `json:"bucket_id"`
	Domain    string     `json:"domain"`
	DNSStatus DNSStatus  `json:"dns_status"`
	SSLStatus SSLStatus  `json:"ssl_status"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// AuditAction enumerates recorded security-relevant events.
type AuditAction string

const (
	AuditLogin                AuditAction = "auth.login"
	AuditLoginFailed          AuditAction = "auth.login_failed"
	AuditLogout               AuditAction = "auth.logout"
	AuditRegister             AuditAction = "auth.register"
	AuditPasswordReset        AuditAction = "auth.password_reset"
	AuditEmailVerified        AuditAction = "auth.email_verified"
	AuditAPIKeyCreated        AuditAction = "api_key.created"
	AuditAPIKeyRevoked        AuditAction = "api_key.revoked"
	AuditBucketCreated        AuditAction = "bucket.created"
	AuditBucketDeleted        AuditAction = "bucket.deleted"
	AuditObjectDeleted        AuditAction = "object.deleted"
	AuditSubscriptionChanged  AuditAction = "subscription.changed"
	AuditDomainAdded          AuditAction = "domain.added"
	AuditDomainVerified       AuditAction = "domain.verified"
	AuditDomainRemoved        AuditAction = "domain.removed"
)

// AuditLogEntry is an append-only security record.
type AuditLogEntry struct {
	ID        uuid.UUID  `json:"id"`
	UserID    *uuid.UUID `json:"user_id,omitempty"`
	Action    AuditAction `json:"action"`
	IP        string     `json:"ip"`
	UserAgent string     `json:"user_agent"`
	Metadata  url.Values `json:"metadata,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}
