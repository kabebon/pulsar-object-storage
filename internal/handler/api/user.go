package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	httperr "pulsar/internal/errors"
	"pulsar/internal/repository"
	"pulsar/internal/middleware"
)

// UserHandler exposes the current user profile and usage summary.
type UserHandler struct {
	users *repository.UsersRepo
	usage *repository.UsageRepo
}

// NewUserHandler wires dependencies.
func NewUserHandler(users *repository.UsersRepo, usage *repository.UsageRepo) *UserHandler {
	return &UserHandler{users: users, usage: usage}
}

// Routes registers /me routes and returns the sub-router.
func (h *UserHandler) Routes() http.Handler {
	r := chi.NewRouter()
	r.With(middleware.RequireScope("profile:read")).Get("/", h.me)
	r.With(middleware.RequireScope("profile:read")).Get("/usage", h.usageSummary)
	return r
}

func (h *UserHandler) me(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	user, err := h.users.FindByID(r.Context(), uid)
	if err != nil {
		writeError(w, httperr.From(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
}

func (h *UserHandler) usageSummary(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	summary, err := h.usage.Summary(r.Context(), uid)
	if err != nil {
		writeError(w, httperr.From(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"usage": summary})
}
