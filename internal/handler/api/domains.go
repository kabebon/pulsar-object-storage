package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	domainsvc "pulsar/internal/domain"
)

// DomainsHandler exposes the public on-demand TLS ask endpoint used by Caddy.
// It is intentionally unauthenticated: Caddy calls it before issuing a cert
// and the response is a simple yes/no based on DB state.
type DomainsHandler struct {
	domains *domainsvc.Service
}

// NewDomainsHandler wires dependencies.
func NewDomainsHandler(d *domainsvc.Service) *DomainsHandler {
	return &DomainsHandler{domains: d}
}

// PublicRoutes registers public (no-auth) domain endpoints.
func (h *DomainsHandler) PublicRoutes(r chi.Router) {
	r.Get("/domains/verify-tls", h.verifyTLS)
}

// verifyTLS returns 200 if the ?domain= is registered and DNS-verified,
// otherwise 404. Caddy treats 4xx as "do not issue".
func (h *DomainsHandler) verifyTLS(w http.ResponseWriter, r *http.Request) {
	domain := strings.TrimSpace(r.URL.Query().Get("domain"))
	if domain == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"domain query param required"}`))
		return
	}
	if _, err := h.domains.VerifyTLS(r.Context(), domain); err != nil {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"verified":false}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"verified":true}`))
}

// keep chi referenced.
var _ = chi.NewRouter
