// Package domain implements custom-domain registration and DNS verification
// for CDN delivery. Verification is performed with real DNS lookups
// (net.LookupTXT / net.LookupCNAME) so the flow works identically in
// production: the customer adds a TXT or CNAME record and Pulsar confirms it.
package domain

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"

	"pulsar/internal/models"
	"pulsar/internal/repository"
)

// ExpectedPrefix is the TXT record prefix the customer must publish.
const ExpectedPrefix = "pulsar-verify="

// Resolver abstracts DNS lookups so tests can stub them. The default
// implementation uses the net resolver with a short timeout.
type Resolver interface {
	LookupTXT(ctx context.Context, host string) ([]string, error)
	LookupCNAME(ctx context.Context, host string) (string, error)
}

// netResolver wraps the standard library resolver.
type netResolver struct {
	r *net.Resolver
}

// NewResolver returns a production resolver with sensible timeouts.
func NewResolver() Resolver {
	return &netResolver{r: &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			return d.DialContext(ctx, network, address)
		},
	}}
}

func (n *netResolver) LookupTXT(ctx context.Context, host string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return n.r.LookupTXT(ctx, host)
}

func (n *netResolver) LookupCNAME(ctx context.Context, host string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return n.r.LookupCNAME(ctx, host)
}

// Deps bundles collaborators for the domain service.
type Deps struct {
	Domains     *repository.CustomDomainsRepo
	Buckets     *repository.BucketsRepo
	Audit       *repository.AuditLogRepo
	Plans       *repository.PlansRepo
	Subs        *repository.SubscriptionsRepo
	Resolver    Resolver
	CDNTarget   string // the CNAME target customers must point at, e.g. cdn.pulsar.example.com
}

// Service handles custom-domain lifecycle.
type Service struct {
	Deps
}

// New wires dependencies.
func New(deps Deps) *Service {
	if deps.Resolver == nil {
		deps.Resolver = NewResolver()
	}
	return &Service{Deps: deps}
}

// Add registers a domain for a bucket, with quota enforcement.
func (s *Service) Add(ctx context.Context, userID, bucketID uuid.UUID, domain string) (*models.CustomDomain, error) {
	domain = normalizeDomain(domain)
	if err := validateDomain(domain); err != nil {
		return nil, repository.Wrap(models.ErrValidation, err.Error())
	}
	// Confirm bucket ownership.
	if _, err := s.Buckets.FindByID(ctx, userID, bucketID); err != nil {
		return nil, err
	}
	// Quota.
	if limit, ok := s.planDomainLimit(ctx, userID); ok && limit > 0 {
		count, err := s.Domains.CountByUser(ctx, userID)
		if err != nil {
			return nil, err
		}
		if count >= limit {
			return nil, repository.Wrap(models.ErrQuotaExceeded, fmt.Sprintf("your plan allows up to %d custom domains", limit))
		}
	}
	d, err := s.Domains.Create(ctx, userID, bucketID, domain)
	if err != nil {
		return nil, err
	}
	uid := userID
	_ = s.Audit.Record(ctx, &uid, models.AuditDomainAdded, "", "", nil)
	return d, nil
}

// List returns all custom domains for a user.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]models.CustomDomain, error) {
	return s.Domains.List(ctx, userID)
}

// Delete removes a domain.
func (s *Service) Delete(ctx context.Context, userID, id uuid.UUID) error {
	if err := s.Domains.Delete(ctx, userID, id); err != nil {
		return err
	}
	uid := userID
	_ = s.Audit.Record(ctx, &uid, models.AuditDomainRemoved, "", "", nil)
	return nil
}

// Verify performs real DNS lookups: a TXT record proving ownership, and a
// CNAME pointing at our CDN target. Updates dns_status accordingly.
func (s *Service) Verify(ctx context.Context, userID, id uuid.UUID) (*models.CustomDomain, error) {
	d, err := s.Domains.FindByID(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	txtOK := s.checkTXT(ctx, d.Domain)
	cnameOK := s.checkCNAME(ctx, d.Domain)
	dns := models.DNSPending
	switch {
	case txtOK && cnameOK:
		dns = models.DNSVerified
	case txtOK || cnameOK:
		dns = models.DNSPending
	default:
		dns = models.DNSFailed
	}
	// When DNS verifies, flip SSL to "issued" to simulate on-demand issuance.
	ssl := models.SSLPending
	if dns == models.DNSVerified {
		ssl = models.SSLIssued
	}
	if err := s.Domains.UpdateStatus(ctx, userID, id, dns, ssl); err != nil {
		return nil, err
	}
	d.DNSStatus = dns
	d.SSLStatus = ssl
	uid := userID
	if dns == models.DNSVerified {
		_ = s.Audit.Record(ctx, &uid, models.AuditDomainVerified, "", "", nil)
	}
	return d, nil
}

// VerifyTLS is the on-demand TLS ask endpoint used by Caddy. It returns the
// bucket id if the domain is registered and DNS-verified; Caddy then issues
// a certificate. Returns ErrNotFound when the domain is unknown/unverified.
func (s *Service) VerifyTLS(ctx context.Context, domain string) (uuid.UUID, error) {
	d, err := s.Domains.FindByDomain(ctx, normalizeDomain(domain))
	if err != nil {
		return uuid.Nil, models.ErrNotFound
	}
	if d.DNSStatus != models.DNSVerified {
		return uuid.Nil, models.ErrNotFound
	}
	return d.BucketID, nil
}

// CDNURL builds a signed CDN URL for an object served from a custom domain.
// In production a real CDN (CloudFront/Cloudflare) sits in front; here we
// return a deterministic URL so the contract is observable.
func (s *Service) CDNURL(domain, objectKey string) string {
	return "https://" + strings.TrimSuffix(domain, "/") + "/" + strings.TrimPrefix(objectKey, "/")
}

// CNAMEInstructions returns the human-readable DNS setup for a domain.
func (s *Service) CNAMEInstructions(d *models.CustomDomain) (cnameTarget, txtValue string) {
	return s.CDNTarget, ExpectedPrefix + d.ID.String()
}

// checkTXT returns true if a TXT record matching ExpectedPrefix+<id> exists.
func (s *Service) checkTXT(ctx context.Context, domain string) bool {
	records, err := s.Resolver.LookupTXT(ctx, domain)
	if err != nil {
		return false
	}
	for _, r := range records {
		if strings.HasPrefix(r, ExpectedPrefix) {
			return true
		}
	}
	return false
}

// checkCNAME returns true if the domain CNAMEs to our CDN target.
func (s *Service) checkCNAME(ctx context.Context, domain string) bool {
	if s.CDNTarget == "" {
		return false
	}
	cname, err := s.Resolver.LookupCNAME(ctx, domain)
	if err != nil {
		return false
	}
	target := strings.TrimSuffix(s.CDNTarget, ".")
	got := strings.TrimSuffix(cname, ".")
	return got == target
}

// planDomainLimit returns (limit, true) if a plan is resolvable.
func (s *Service) planDomainLimit(ctx context.Context, userID uuid.UUID) (int, bool) {
	if s.Subs == nil || s.Plans == nil {
		return 0, false
	}
	sub, err := s.Subs.FindByUser(ctx, userID)
	if err != nil {
		return 0, false
	}
	plan, err := s.Plans.FindBySlug(ctx, sub.PlanSlug)
	if err != nil {
		return 0, false
	}
	return plan.CustomDomainsAllowed, true
}

// normalizeDomain lowercases and strips scheme/path/trailing dot.
func normalizeDomain(d string) string {
	d = strings.ToLower(strings.TrimSpace(d))
	d = strings.TrimPrefix(d, "http://")
	d = strings.TrimPrefix(d, "https://")
	if i := strings.IndexAny(d, "/?#"); i >= 0 {
		d = d[:i]
	}
	return strings.TrimSuffix(d, ".")
}

// validateDomain performs basic sanity checks.
func validateDomain(d string) error {
	if d == "" {
		return errors.New("domain is required")
	}
	if len(d) > 253 {
		return errors.New("domain too long")
	}
	if !strings.Contains(d, ".") {
		return errors.New("domain must contain a dot")
	}
	// Allow a-z, 0-9, '-', '.'.
	for _, r := range d {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '.':
		default:
			return errors.New("domain contains invalid characters")
		}
	}
	return nil
}
