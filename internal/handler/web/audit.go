package web

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"pulsar/internal/config"
	"pulsar/internal/repository"
	"pulsar/web/views/pages"
)

// AuditHandler renders the audit log page (read-only).
type AuditHandler struct {
	cfg  *config.Config
	audit *repository.AuditLogRepo
}

// NewAuditHandler wires dependencies.
func NewAuditHandler(cfg *config.Config, audit *repository.AuditLogRepo) *AuditHandler {
	return &AuditHandler{cfg: cfg, audit: audit}
}

// Routes registers the audit route (auth-protected).
func (h *AuditHandler) Routes(r chi.Router) {
	r.Get("/audit", h.show)
}

func (h *AuditHandler) show(w http.ResponseWriter, r *http.Request) {
	uid := mustUserID(r)
	entries, err := h.audit.List(r.Context(), uid, 100, 0)
	if err != nil {
		entries = nil
	}
	rows := make([]pages.AuditRow, 0, len(entries))
	for _, e := range entries {
		ua := e.UserAgent
		if len(ua) > 60 {
			ua = ua[:60] + "…"
		}
		rows = append(rows, pages.AuditRow{
			Action:    e.Action,
			IP:        e.IP,
			UserAgent: ua,
			CreatedAt: e.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	props := baseProps(h.cfg, r, "Журнал", "", "audit")
	Render(w, r, 0, pages.AuditPage(props, rows))
}

// keep strings referenced for future filtering.
var _ = strings.TrimSpace
var _ = chi.NewRouter
