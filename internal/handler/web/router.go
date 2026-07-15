package web

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"pulsar/docs"
	"pulsar/internal/config"
	"pulsar/internal/middleware"
	"pulsar/internal/repository"
	"pulsar/internal/service"
	"pulsar/web/static"
	"pulsar/web/views/pages"
)

// StaticHandler serves immutable public assets under /static and /docs.
// In production these should ideally be fronted by a CDN; here we serve them
// from the binary-embedded filesystem (Phase 7 embeds web/static).
type StaticHandler struct{}

// NewRouter builds the browser-facing router (CSRF + session protected).
// Auth routes are public; /app/* requires an authenticated session.
func NewRouter(
	cfg *config.Config,
	logger *slog.Logger,
	auth *service.AuthService,
	storage *service.StorageService,
	apiKeys *service.APIKeyService,
	apiKeysRepo *repository.APIKeysRepo,
	billingH *BillingHandler,
	domainsH *DomainsHandler,
	usersRepo *repository.UsersRepo,
	auditRepo *repository.AuditLogRepo,
	subsRepo *repository.SubscriptionsRepo,
	rateLimiter middleware.RateLimiterProvider,
) http.Handler {
	r := chi.NewRouter()

	// Public pages (landing, pricing, auth, static).
	authH := NewAuthHandler(cfg, auth)
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		Render(w, r, 0, pages.Home(baseProps(cfg, r, "Pulsar — облачное хранилище", "S3-совместимое объектное хранилище с удобным веб-кабинетом и REST API.", "home")))
	})
	r.Get("/pricing", func(w http.ResponseWriter, r *http.Request) {
		Render(w, r, 0, pages.Pricing(baseProps(cfg, r, "Тарифы", "Простые честные тарифы без скрытых лимитов.", "pricing"), defaultPlans()))
	})

	// Auth routes with per-endpoint rate limiting.
	r.Group(func(r chi.Router) {
		if rateLimiter != nil {
			// Limit registration & login by email (anti-abuse).
			r.With(rateLimiter.RateLimit("/register", 10, "1m", middleware.LimitByEmail("email"))).
				Post("/register", authH.submitRegister)
			r.With(rateLimiter.RateLimit("/login", 10, "1m", middleware.LimitByEmail("email"))).
				Post("/login", authH.submitLogin)
			r.With(rateLimiter.RateLimit("/forgot-password", 5, "1m", middleware.LimitByEmail("email"))).
				Post("/forgot-password", authH.submitForgot)
		} else {
			r.Post("/register", authH.submitRegister)
			r.Post("/login", authH.submitLogin)
			r.Post("/forgot-password", authH.submitForgot)
		}
		authH.Routes(r)
	})

	// Static assets (embedded into the binary) + docs.
	r.Handle("/static/*", http.StripPrefix("/static/", static.Handler()))
	r.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(static.Favicon())
	})
	r.Get("/docs/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_, _ = w.Write(docs.OpenAPIYAML)
	})
	r.Get("/docs", func(w http.ResponseWriter, r *http.Request) {
		props := baseProps(cfg, r, "Документация API", "Pulsar REST API: аутентификация, бакеты, загрузка и скачивание файлов.", "")
		Render(w, r, 0, pages.Docs(props, cfg.HTTP.PublicBaseURL))
	})

	// Authenticated area (/app/*). All dashboard routes live under /app.
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(false))
		// Re-read the user's plan from the DB on each request so the UI shows
		// the live plan after an upgrade, instead of the login-time session cache.
		if subsRepo != nil {
			r.Use(middleware.RefreshPlan(subsRepo, logger))
		}
		r.Route("/app", func(r chi.Router) {
			// Dashboard overview at /app itself.
			r.Get("/", func(w http.ResponseWriter, r *http.Request) {
				email, _ := r.Context().Value(middleware.CtxEmail).(string)
				plan, _ := r.Context().Value(middleware.CtxPlanSlug).(string)
				if plan == "" {
					plan = "free"
				}
				Render(w, r, 0, pages.Dashboard(baseProps(cfg, r, "Кабинет", "", "dashboard"), email, plan))
			})
			// Storage (files + objects).
			if storage != nil {
				NewStorageHandler(cfg, storage).Routes(r)
			}
			if apiKeys != nil {
				NewAPIKeysHandler(cfg, apiKeys, apiKeysRepo).Routes(r)
			}
			if billingH != nil {
				billingH.Routes(r)
			}
			// Settings + audit are always available for authenticated users.
			NewSettingsHandler(cfg, auth, usersRepo).Routes(r)
			NewAuditHandler(cfg, auditRepo).Routes(r)
			// Note: domains handler exists in code but is intentionally not
			// mounted — this product is a file cloud, not a CDN service.
		})
	})

	// 404 fallback for everything else.
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		props := baseProps(cfg, r, "Не найдено", "", "")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<doctype html><html><body style="font-family:sans-serif;background:#020617;color:#e2e8f0;padding:3rem"><h1>404</h1><p>Страница не найдена.</p><a href="/" style="color:#818cf8">На главную</a></body></html>`))
		_ = props
	})

	return r
}

// defaultPlans returns the marketing pricing cards. In Phase 5 these are
// sourced from the database; we hardcode here so the landing page renders
// even before billing tables exist.
func defaultPlans() []pages.PlanViewModel {
	return []pages.PlanViewModel{
		{Slug: "free", Name: "Free", PriceMonthly: 0, PriceYearly: 0, StorageGB: 5, BandwidthGBMonth: 50, MaxBuckets: 3, CustomDomains: 0},
		{Slug: "pro", Name: "Pro", PriceMonthly: 9900, PriceYearly: 99000, StorageGB: 100, BandwidthGBMonth: 1000, MaxBuckets: 50, CustomDomains: 5, Highlighted: true},
		{Slug: "business", Name: "Business", PriceMonthly: 49900, PriceYearly: 499000, StorageGB: 1024, BandwidthGBMonth: 10240, MaxBuckets: 0, CustomDomains: 0},
	}
}
