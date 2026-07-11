package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	httperr "pulsar/internal/errors"
	"pulsar/internal/models"
	"pulsar/internal/repository"
	"pulsar/internal/service"
)

// APIKeysHandler manages long-lived bearer tokens.
type APIKeysHandler struct {
	keys *service.APIKeyService
	repo *repository.APIKeysRepo
}

// NewAPIKeysHandler wires dependencies.
func NewAPIKeysHandler(keys *service.APIKeyService, repo *repository.APIKeysRepo) *APIKeysHandler {
	return &APIKeysHandler{keys: keys, repo: repo}
}

// Routes registers /api-keys routes and returns the sub-router.
func (h *APIKeysHandler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/", h.list)
	r.Post("/", h.create)
	r.Delete("/{id}", h.delete)
	return r
}

func (h *APIKeysHandler) list(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	keys, err := h.repo.List(r.Context(), uid)
	if err != nil {
		writeError(w, httperr.From(err))
		return
	}
	// Strip the hash from the response (it's already tagged json:"-").
	writeJSON(w, http.StatusOK, map[string]any{"api_keys": keys, "count": len(keys)})
}

func (h *APIKeysHandler) create(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	var req struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	if e := decode(r, &req); e != nil {
		writeError(w, e)
		return
	}
	res, err := h.keys.Create(r.Context(), uid, req.Name, req.Scopes)
	if err != nil {
		writeError(w, httperr.From(err))
		return
	}
	// IMPORTANT: the secret is shown exactly once here.
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":     res.ID,
		"name":   res.Name,
		"prefix": res.Prefix,
		"secret": res.Secret,
		"hint":   "Сохраните secret — он показывается только один раз.",
	})
}

func (h *APIKeysHandler) delete(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, httperr.BadRequest("bad_id", "Invalid key id"))
		return
	}
	if err := h.repo.Delete(r.Context(), uid, id); err != nil {
		writeError(w, httperr.From(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// keep models import referenced for future field extensions.
var _ = models.APIKey{}
