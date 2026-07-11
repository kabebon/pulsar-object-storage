// Package billing — CryptoBot (Crypto Pay API) provider.
// Thin HTTP client for the Crypto Pay REST API (https://pay.crypt.bot/api).
// Docs: https://help.send.tg/en/articles/10279948-crypto-pay-api
//
// Runs in no-provider mode when CRYPTOBOT_TOKEN is empty.
package billing

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"pulsar/internal/config"
	"pulsar/internal/models"
	"pulsar/internal/repository"
)

const (
	cryptoBotMainnet = "https://pay.crypt.bot/api"
	cryptoBotTestnet = "https://testnet-pay.crypt.bot/api"
)

// DefaultCryptoAsset is the cryptocurrency used for invoices when none is specified.
// USDT is the most stable choice for subscription payments.
const DefaultCryptoAsset = "USDT"

// CryptoBotService orchestrates crypto subscription payments via Crypto Pay API.
type CryptoBotService struct {
	cfg     config.CryptoBotConfig
	apiBase string
	plans   *repository.PlansRepo
	subs    *repository.SubscriptionsRepo
	users   *repository.UsersRepo
	http    *http.Client
}

// NewCryptoBot constructs a CryptoBot service.
// Returns a disabled service when Token is empty — callers must check Enabled().
func NewCryptoBot(
	cfg config.CryptoBotConfig,
	plans *repository.PlansRepo,
	subs *repository.SubscriptionsRepo,
	users *repository.UsersRepo,
) *CryptoBotService {
	base := cryptoBotMainnet
	if strings.EqualFold(cfg.Network, "testnet") {
		base = cryptoBotTestnet
	}
	return &CryptoBotService{
		cfg:     cfg,
		apiBase: base,
		plans:   plans,
		subs:    subs,
		users:   users,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// Enabled reports whether the CryptoBot provider is configured.
func (s *CryptoBotService) Enabled() bool {
	return s != nil && s.cfg.Token != ""
}

// --- Crypto Pay API types ---

type cbResult[T any] struct {
	OK     bool   `json:"ok"`
	Result T      `json:"result"`
	Error  string `json:"error,omitempty"`
}

type cbInvoice struct {
	InvoiceID   int64     `json:"invoice_id"`
	Status      string    `json:"status"` // "active" | "paid" | "expired"
	PayURL      string    `json:"pay_url"`
	Asset       string    `json:"asset"` // e.g. "USDT"
	Amount      string    `json:"amount"`
	Payload     string    `json:"payload"` // our metadata JSON
	Description string    `json:"description"`
	ExpiresAt   time.Time `json:"expiration_date"`
	PaidAt      time.Time `json:"paid_at"`
}

// cbPayload is the structured metadata embedded in every invoice's Payload field.
type cbPayload struct {
	UserID   string `json:"user_id"`
	PlanSlug string `json:"plan"`
	Interval string `json:"interval"`
}

// cbWebhookUpdate is the JSON envelope sent by Crypto Pay on invoice_paid.
type cbWebhookUpdate struct {
	UpdateID  int64     `json:"update_id"`
	UpdateAt  time.Time `json:"update_time"`
	PayloadType string  `json:"payload_type"` // "invoice_paid"
	Invoice   cbInvoice `json:"invoice"`
}

// CreateInvoice creates a Crypto Pay invoice and returns the pay_url
// the user must open to complete the payment.
//
//   - asset: cryptocurrency ticker (e.g. "USDT", "TON", "BTC")
//   - amount: amount in the chosen asset (e.g. "9.90" for $9.90 USDT)
//   - planSlug, interval: stored in payload for webhook resolution
func (s *CryptoBotService) CreateInvoice(
	ctx context.Context,
	userID uuid.UUID,
	planSlug, interval, asset, amount, description, returnURL string,
) (string, error) {
	if !s.Enabled() {
		return "", errors.New("cryptobot: not configured")
	}
	if asset == "" {
		asset = DefaultCryptoAsset
	}

	payloadData, err := json.Marshal(cbPayload{
		UserID:   userID.String(),
		PlanSlug: planSlug,
		Interval: interval,
	})
	if err != nil {
		return "", fmt.Errorf("cryptobot: marshal payload: %w", err)
	}

	reqBody := map[string]string{
		"asset":       asset,
		"amount":      amount,
		"description": description,
		"payload":     string(payloadData),
		"expires_in":  strconv.Itoa(30 * 60), // 30 minutes
	}
	if returnURL != "" {
		reqBody["return_url"] = returnURL
	}

	var result cbResult[cbInvoice]
	if err := s.call(ctx, "createInvoice", reqBody, &result); err != nil {
		return "", fmt.Errorf("cryptobot: createInvoice: %w", err)
	}
	if !result.OK {
		return "", fmt.Errorf("cryptobot: API error: %s", result.Error)
	}
	if result.Result.PayURL == "" {
		return "", errors.New("cryptobot: empty pay_url in response")
	}
	return result.Result.PayURL, nil
}

// HandleWebhook processes a Crypto Pay webhook update.
// It verifies the HMAC-SHA256 signature from the Crypto-Pay-Api-Signature header,
// then on invoice_paid upserts the subscription for the paying user.
func (s *CryptoBotService) HandleWebhook(ctx context.Context, body []byte, signature string) error {
	if !s.Enabled() {
		return errors.New("cryptobot: not configured")
	}

	if !s.verifySignature(body, signature) {
		return errors.New("cryptobot: invalid webhook signature")
	}

	var update cbWebhookUpdate
	if err := json.Unmarshal(body, &update); err != nil {
		return fmt.Errorf("cryptobot: decode webhook: %w", err)
	}

	// Only invoice_paid is actionable for subscription flow.
	if update.PayloadType != "invoice_paid" {
		return nil
	}
	if update.Invoice.Status != "paid" {
		return nil
	}

	return s.onInvoicePaid(ctx, update.Invoice)
}

func (s *CryptoBotService) onInvoicePaid(ctx context.Context, invoice cbInvoice) error {
	if invoice.Payload == "" {
		return nil
	}

	var meta cbPayload
	if err := json.Unmarshal([]byte(invoice.Payload), &meta); err != nil {
		return nil // not our invoice format, ignore
	}
	if meta.UserID == "" || meta.PlanSlug == "" {
		return nil
	}

	userID, err := uuid.Parse(meta.UserID)
	if err != nil {
		return fmt.Errorf("cryptobot: invalid user_id in payload: %w", err)
	}

	plan, err := s.plans.FindBySlug(ctx, meta.PlanSlug)
	if err != nil {
		plan, err = s.plans.FindBySlug(ctx, "free")
		if err != nil {
			return err
		}
	}

	// Upsert subscription — CryptoBot has no customer ID concept; we use invoice_id as reference.
	invoiceRef := strconv.FormatInt(invoice.InvoiceID, 10)
	return s.subs.Upsert(ctx, userID, plan.ID, models.SubStatusActive, "", invoiceRef)
}

// verifySignature validates the HMAC-SHA256 signature sent by Crypto Pay.
// The signature is computed as HMAC-SHA256(token_secret, sorted_body_fields)
// where token_secret = SHA256(token).
//
// Per the official spec: https://help.send.tg/en/articles/10279948-crypto-pay-api#webhooks
func (s *CryptoBotService) verifySignature(body []byte, signature string) bool {
	// Derive the secret: SHA256 of the API token.
	tokenHash := sha256.Sum256([]byte(s.cfg.Token))

	// Parse the JSON body and rebuild a canonical form for signing.
	// Crypto Pay signs the concatenation of all body fields sorted alphabetically.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return false
	}

	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		var v any
		_ = json.Unmarshal(raw[k], &v)
		parts = append(parts, fmt.Sprintf("%s=%v", k, flatten(v)))
	}
	canonical := strings.Join(parts, "\n")

	mac := hmac.New(sha256.New, tokenHash[:])
	mac.Write([]byte(canonical))
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expected), []byte(signature))
}

// flatten converts a JSON value to a flat string for canonical signing.
func flatten(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "true"
		}
		return "false"
	case nil:
		return ""
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

// --- HTTP helper ---

func (s *CryptoBotService) call(ctx context.Context, method string, params map[string]string, out any) error {
	data, err := json.Marshal(params)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.apiBase+"/"+method, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Crypto-Pay-API-Token", s.cfg.Token)

	res, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return fmt.Errorf("cryptobot API HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}
