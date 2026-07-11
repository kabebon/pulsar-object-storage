package web

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"pulsar/internal/config"
	"pulsar/internal/repository"
	"pulsar/internal/service"
	"pulsar/web/views/pages"
)

// APIKeysHandler renders the API keys management page.
type APIKeysHandler struct {
	cfg  *config.Config
	keys *service.APIKeyService
	repo *repository.APIKeysRepo
}

// NewAPIKeysHandler wires dependencies.
func NewAPIKeysHandler(cfg *config.Config, keys *service.APIKeyService, repo *repository.APIKeysRepo) *APIKeysHandler {
	return &APIKeysHandler{cfg: cfg, keys: keys, repo: repo}
}

// Routes registers API key routes (auth-protected).
func (h *APIKeysHandler) Routes(r chi.Router) {
	r.Get("/api-keys", h.list)
	r.Post("/api-keys", h.create)
	r.Post("/api-keys/{id}/delete", h.delete)
}

func (h *APIKeysHandler) list(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, nil)
}

func (h *APIKeysHandler) create(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	uid := mustUserID(r)
	name := strings.TrimSpace(r.FormValue("name"))
	res, err := h.keys.Create(r.Context(), uid, name, nil)
	if err != nil {
		http.Error(w, "не удалось создать ключ", http.StatusBadRequest)
		return
	}
	h.render(w, r, &pages.CreatedKey{ID: res.ID.String(), Name: res.Name, Secret: res.Secret})
}

func (h *APIKeysHandler) delete(w http.ResponseWriter, r *http.Request) {
	uid := mustUserID(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if err := h.repo.Delete(r.Context(), uid, id); err != nil {
		http.Error(w, "не удалось удалить", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/app/api-keys", http.StatusSeeOther)
}

func (h *APIKeysHandler) render(w http.ResponseWriter, r *http.Request, created *pages.CreatedKey) {
	uid := mustUserID(r)
	ks, _ := h.repo.List(r.Context(), uid)
	rows := make([]pages.APIKeyRow, 0, len(ks))
	for _, k := range ks {
		last := "никогда"
		if k.LastUsedAt != nil {
			last = k.LastUsedAt.Format("2006-01-02 15:04")
		}
		rows = append(rows, pages.APIKeyRow{
			ID:         k.ID.String(),
			Name:       k.Name,
			Prefix:     k.KeyPrefix,
			Scopes:     k.Scopes,
			LastUsedAt: last,
			CreatedAt:  k.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	props := baseProps(h.cfg, r, "API-ключи", "", "api-keys")
	Render(w, r, 0, pages.APIKeysPage(props, rows, created))
}
