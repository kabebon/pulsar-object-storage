package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"pulsar/internal/billing"
)

// WebhooksHandler exposes payment webhook endpoints. All webhook routes must
// bypass CSRF (each provider signs requests itself) and read the raw body exactly
// as received.
//
// YooKassa→ POST /webhooks/yookassa  (IP whitelist verification)
// CryptoBot→POST /webhooks/cryptobot (HMAC-SHA256 via Crypto-Pay-Api-Signature header)
type WebhooksHandler struct {
	yookassa   *billing.YooKassaService
	cryptobot  *billing.CryptoBotService
}

// NewWebhooksHandler wires dependencies.
func NewWebhooksHandler(
	yk *billing.YooKassaService,
	cb *billing.CryptoBotService,
) *WebhooksHandler {
	return &WebhooksHandler{yookassa: yk, cryptobot: cb}
}

// Routes registers webhook endpoints under /webhooks.
func (h *WebhooksHandler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Post("/yookassa", h.yookassa_)
	r.Post("/cryptobot", h.cryptobot_)
	return r
}

// yookassa_ handles YooKassa webhook events.
// YooKassa verifies authenticity via IP whitelist (no signature header).
// Must respond with 200 immediately; retries for up to 24 h otherwise.
func (h *WebhooksHandler) yookassa_(w http.ResponseWriter, r *http.Request) {
	if h.yookassa == nil || !h.yookassa.Enabled() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"yookassa not configured"}`))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB limit
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Respond 200 immediately so YooKassa doesn't retry; process in background.
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"received":true}`))

	bgCtx := context.WithoutCancel(r.Context())
	ip := r.RemoteAddr
	if idx := strings.LastIndexByte(ip, ':'); idx > 0 {
		ip = ip[:idx]
	}

	// Background processing — do NOT block the response.
	go func() {
		if err := h.yookassa.HandleWebhook(bgCtx, payload, ip); err != nil {
			slog.Error("yookassa webhook processing failed", "error", err)
		}
	}()
}

// cryptobot_ handles Crypto Pay API webhook events.
// Crypto Pay signs each request with HMAC-SHA256 using the API token.
func (h *WebhooksHandler) cryptobot_(w http.ResponseWriter, r *http.Request) {
	if h.cryptobot == nil || !h.cryptobot.Enabled() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"cryptobot not configured"}`))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB limit
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	sig := r.Header.Get("Crypto-Pay-Api-Signature")

	if err := h.cryptobot.HandleWebhook(r.Context(), payload, sig); err != nil {
		slog.Error("cryptobot webhook processing failed", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"webhook processing failed"}`))
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"received":true}`))
}
