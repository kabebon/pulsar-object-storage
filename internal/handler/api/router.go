package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	domainsvc "pulsar/internal/domain"
	"pulsar/internal/middleware"
	"pulsar/internal/repository"
	"pulsar/internal/service"
)

// RouterDeps bundles collaborators needed to build the v1 API router.
type RouterDeps struct {
	Storage  *service.StorageService
	Keys     *service.APIKeyService
	KeysRepo *repository.APIKeysRepo
	Users    *repository.UsersRepo
	Usage    *repository.UsageRepo
	Domains  *domainsvc.Service
}

// NewRouter builds the /api/v1 router with bearer-token auth + rate limit.
// Public (no-auth) endpoints (on-demand TLS ask) are mounted before the auth
// middleware so Caddy can reach them without credentials.
func NewRouter(deps RouterDeps, rateLimit func(http.Handler) http.Handler) http.Handler {
	r := chi.NewRouter()

	// Public routes: skip bearer auth.
	if deps.Domains != nil {
		r.Group(func(r chi.Router) {
			NewDomainsHandler(deps.Domains).PublicRoutes(r)
		})
	}

	// Bearer (API key) authentication. Resolves via the key service.
	r.Group(func(r chi.Router) {
		r.Use(middleware.APIKeyAuth(func(ctx context.Context, token string) (string, string, error) {
			uid, name, err := deps.Keys.Resolve(ctx, token)
			if err != nil {
				return "", "", err
			}
			return uid.String(), name, nil
		}))

		if rateLimit != nil {
			r.Use(rateLimit)
		}

		// Require an authenticated caller on every protected endpoint.
		r.Use(middleware.RequireAuth(true))

		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{
				"name":    "Pulsar API",
				"version": "v1",
				"docs":    "/docs/openapi.yaml",
			})
		})

		r.Mount("/buckets", NewBucketsHandler(deps.Storage).Routes())
		r.Mount("/api-keys", NewAPIKeysHandler(deps.Keys, deps.KeysRepo).Routes())
		r.Mount("/me", NewUserHandler(deps.Users, deps.Usage).Routes())
	})

	return r
}
