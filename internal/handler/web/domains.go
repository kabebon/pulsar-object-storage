package web

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"pulsar/internal/config"
	domainsvc "pulsar/internal/domain"
	"pulsar/internal/repository"
	"pulsar/web/views/pages"
)

// DomainsHandler serves the custom-domain management page.
type DomainsHandler struct {
	cfg     *config.Config
	domains *domainsvc.Service
	buckets *repository.BucketsRepo
}

// NewDomainsHandler wires dependencies.
func NewDomainsHandler(cfg *config.Config, d *domainsvc.Service, buckets *repository.BucketsRepo) *DomainsHandler {
	return &DomainsHandler{cfg: cfg, domains: d, buckets: buckets}
}

// Routes registers domain routes (auth-protected).
func (h *DomainsHandler) Routes(r chi.Router) {
	r.Get("/domains", h.list)
	r.Post("/domains", h.add)
	r.Post("/domains/{id}/verify", h.verify)
	r.Post("/domains/{id}/delete", h.delete)
}

func (h *DomainsHandler) list(w http.ResponseWriter, r *http.Request) {
	uid := mustUserID(r)
	ds, err := h.domains.List(r.Context(), uid)
	if err != nil {
		ds = nil
	}
	rows := make([]pages.DomainRow, 0, len(ds))
	for _, d := range ds {
		cnameTarget, txtValue := h.domains.CNAMEInstructions(&d)
		bucketName := ""
		if b, err := h.buckets.FindByID(r.Context(), uid, d.BucketID); err == nil {
			bucketName = b.Name
		}
		rows = append(rows, pages.DomainRow{
			ID: d.ID.String(), Domain: d.Domain, BucketName: bucketName,
			DNSStatus: string(d.DNSStatus), SSLStatus: string(d.SSLStatus),
			CNAMETarget: cnameTarget, TXTValue: txtValue,
			CreatedAt: d.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	props := baseProps(h.cfg, r, "Домены", "", "domains")
	Render(w, r, 0, pages.DomainsPage(props, rows))
}

func (h *DomainsHandler) add(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	uid := mustUserID(r)
	domain := strings.TrimSpace(r.FormValue("domain"))
	bucketName := strings.TrimSpace(r.FormValue("bucket"))
	bucket, err := h.buckets.FindByName(r.Context(), uid, bucketName)
	if err != nil {
		http.Error(w, "бакет не найден", http.StatusBadRequest)
		return
	}
	if _, err := h.domains.Add(r.Context(), uid, bucket.ID, domain); err != nil {
		http.Error(w, "не удалось добавить домен: "+err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/app/domains", http.StatusSeeOther)
}

func (h *DomainsHandler) verify(w http.ResponseWriter, r *http.Request) {
	uid := mustUserID(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if _, err := h.domains.Verify(r.Context(), uid, id); err != nil {
		http.Error(w, "ошибка проверки: "+err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/app/domains", http.StatusSeeOther)
}

func (h *DomainsHandler) delete(w http.ResponseWriter, r *http.Request) {
	uid := mustUserID(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if err := h.domains.Delete(r.Context(), uid, id); err != nil {
		http.Error(w, "не удалось удалить", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/app/domains", http.StatusSeeOther)
}
