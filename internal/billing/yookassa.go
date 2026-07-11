// Package billing — YooKassa provider.
// Thin HTTP client for the YooKassa REST API v3 (https://yookassa.ru/developers/api).
// Runs in no-provider mode when YOOKASSA_SHOP_ID or YOOKASSA_SECRET_KEY is empty.
package billing

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"pulsar/internal/config"
	"pulsar/internal/models"
	"pulsar/internal/repository"
)

const yookassaAPIBase = "https://api.yookassa.ru/v3"

// yookassaTrustedCIDRs is the IP whitelist from official YooKassa documentation.
// https://yookassa.ru/developers/using-api/webhooks#ip
var yookassaTrustedCIDRs = mustParseCIDRs([]string{
	"185.71.76.0/27",
	"185.71.77.0/27",
	"77.75.153.0/25",
	"77.75.156.11/32",
	"77.75.156.35/32",
	"77.75.154.128/25",
	"2a02:5180::/32",
})

// YooKassaService orchestrates YooKassa subscription payments.
type YooKassaService struct {
	cfg   config.YooKassaConfig
	plans *repository.PlansRepo
	subs  *repository.SubscriptionsRepo
	users *repository.UsersRepo
	http  *http.Client
}

// NewYooKassa constructs a YooKassa service. Returns a disabled service when
// ShopID or SecretKey is empty — callers must check Enabled().
func NewYooKassa(
	cfg config.YooKassaConfig,
	plans *repository.PlansRepo,
	subs *repository.SubscriptionsRepo,
	users *repository.UsersRepo,
) *YooKassaService {
	return &YooKassaService{
		cfg:   cfg,
		plans: plans,
		subs:  subs,
		users: users,
		http:  &http.Client{Timeout: 15 * time.Second},
	}
}

// Enabled reports whether the YooKassa provider is configured.
func (s *YooKassaService) Enabled() bool {
	return s != nil && s.cfg.ShopID != "" && s.cfg.SecretKey != ""
}

// --- YooKassa API types ---

type ykAmount struct {
	Value    string `json:"value"`
	Currency string `json:"currency"`
}

type ykConfirmation struct {
	Type      string `json:"type"`
	ReturnURL string `json:"return_url,omitempty"`
}

type ykPaymentRequest struct {
	Amount       ykAmount       `json:"amount"`
	Confirmation ykConfirmation `json:"confirmation"`
	Capture      bool           `json:"capture"`
	Description  string         `json:"description"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

type ykConfirmationResponse struct {
	ConfirmationURL string `json:"confirmation_url"`
}

type ykPaymentResponse struct {
	ID           string                 `json:"id"`
	Status       string                 `json:"status"`
	Paid         bool                   `json:"paid"`
	Amount       ykAmount               `json:"amount"`
	Confirmation ykConfirmationResponse `json:"confirmation"`
	Metadata     map[string]string      `json:"metadata"`
}

type ykWebhookEvent struct {
	Type   string            `json:"type"`
	Event  string            `json:"event"`
	Object ykPaymentResponse `json:"object"`
}

// CreatePaymentURL creates a YooKassa payment and returns the confirmation URL
// the user must visit to complete the payment.
//
// planSlug and interval are stored in payment metadata so the webhook can later
// resolve the correct subscription.
func (s *YooKassaService) CreatePaymentURL(
	ctx context.Context,
	userID uuid.UUID,
	planSlug, interval string,
	amountRub int64,
	returnURL string,
) (string, error) {
	if !s.Enabled() {
		return "", errors.New("yookassa: not configured")
	}

	idempotencyKey, err := randomHex(16)
	if err != nil {
		return "", fmt.Errorf("yookassa: generate idempotency key: %w", err)
	}

	body := ykPaymentRequest{
		Amount: ykAmount{
			Value:    fmt.Sprintf("%d.00", amountRub),
			Currency: "RUB",
		},
		Confirmation: ykConfirmation{
			Type:      "redirect",
			ReturnURL: returnURL,
		},
		Capture:     true,
		Description: fmt.Sprintf("Pulsar %s (%s)", planSlug, interval),
		Metadata: map[string]any{
			"user_id":  userID.String(),
			"plan":     planSlug,
			"interval": interval,
		},
	}

	var resp ykPaymentResponse
	if err := s.post(ctx, "/payments", idempotencyKey, body, &resp); err != nil {
		return "", fmt.Errorf("yookassa: create payment: %w", err)
	}
	if resp.Confirmation.ConfirmationURL == "" {
		return "", errors.New("yookassa: empty confirmation URL in response")
	}
	return resp.Confirmation.ConfirmationURL, nil
}

// HandleWebhook processes a YooKassa webhook notification.
// It validates the originating IP against YooKassa's published whitelist,
// then on payment.succeeded upserts the subscription for the paying user.
func (s *YooKassaService) HandleWebhook(ctx context.Context, body []byte, remoteAddr string) error {
	if !s.Enabled() {
		return errors.New("yookassa: not configured")
	}

	// IP whitelist check.
	ip, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		ip = remoteAddr // remoteAddr may already be bare IP
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return fmt.Errorf("yookassa: cannot parse remote IP %q", ip)
	}
	if !yookassaIPAllowed(parsed) {
		return fmt.Errorf("yookassa: request from untrusted IP %s", ip)
	}

	var event ykWebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return fmt.Errorf("yookassa: decode webhook: %w", err)
	}

	switch event.Event {
	case "payment.succeeded":
		return s.onPaymentSucceeded(ctx, event.Object)
	default:
		// Other events (payment.waiting_for_capture, refund.succeeded, etc.) ignored.
		return nil
	}
}

func (s *YooKassaService) onPaymentSucceeded(ctx context.Context, payment ykPaymentResponse) error {
	meta := payment.Metadata
	if meta == nil {
		return nil // no metadata — not a Pulsar payment
	}
	rawUID, ok1 := meta["user_id"]
	planSlug, ok2 := meta["plan"]
	if !ok1 || !ok2 || rawUID == "" || planSlug == "" {
		return nil
	}

	userID, err := uuid.Parse(rawUID)
	if err != nil {
		return fmt.Errorf("yookassa: invalid user_id in metadata: %w", err)
	}

	plan, err := s.plans.FindBySlug(ctx, planSlug)
	if err != nil {
		plan, err = s.plans.FindBySlug(ctx, "free")
		if err != nil {
			return err
		}
	}

	// Upsert subscription with active status.
	// StripeCustomerID / StripeSubscriptionID are left empty — YooKassa uses payment IDs.
	return s.subs.Upsert(ctx, userID, plan.ID, models.SubStatusActive, "", payment.ID)
}

// --- HTTP helpers ---

func (s *YooKassaService) post(ctx context.Context, path, idempotencyKey string, reqBody, respBody any) error {
	data, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, yookassaAPIBase+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotence-Key", idempotencyKey)
	req.SetBasicAuth(s.cfg.ShopID, s.cfg.SecretKey)

	res, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return fmt.Errorf("yookassa API error %d: %s", res.StatusCode, strings.TrimSpace(string(raw)))
	}
	if respBody != nil {
		return json.Unmarshal(raw, respBody)
	}
	return nil
}

// --- IP whitelist ---

func mustParseCIDRs(cidrs []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, ipnet, err := net.ParseCIDR(c)
		if err != nil {
			panic("billing/yookassa: invalid CIDR " + c)
		}
		out = append(out, ipnet)
	}
	return out
}

func yookassaIPAllowed(ip net.IP) bool {
	for _, cidr := range yookassaTrustedCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// --- misc helpers ---

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
