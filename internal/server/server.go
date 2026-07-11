// Package server assembles the HTTP server, middleware stack and graceful
// shutdown lifecycle. Route registration is delegated to handler packages.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/gorilla/csrf"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"pulsar/internal/config"
	"pulsar/internal/middleware"
)

// Deps bundles the collaborators a Server needs. Router is the browser-facing
// router (CSRF-protected). APIRouter is the REST v1 router (Bearer-auth).
// Both may be nil during early development.
type Deps struct {
	Cfg          *config.Config
	Logger       *slog.Logger
	Router       http.Handler
	APIRouter    http.Handler
	WebhookRouter http.Handler
	SessionStore middleware.SessionStore
	RateLimiter  middleware.RateLimiterProvider
	DBChecker    func(ctx context.Context) error
	RedisChecker func(ctx context.Context) error
	S3Checker    func(ctx context.Context) error
}

// Server wraps *http.Server with graceful shutdown helpers.
type Server struct {
	httpServer *http.Server
	cfg        *config.Config
	logger     *slog.Logger
	deps       Deps
}

// New constructs the server with the middleware stack shared by all routes.
func New(deps Deps) (*Server, error) {
	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recover(deps.Logger))
	r.Use(middleware.Logger(deps.Logger))
	r.Use(middleware.Metrics)

	if deps.SessionStore != nil {
		r.Use(middleware.WithSession(deps.SessionStore, deps.Cfg.Session.CookieName))
	}

	csrfSecret := []byte(deps.Cfg.JWT.Secret)
	if len(csrfSecret) < 32 {
		padded := make([]byte, 32)
		copy(padded, csrfSecret)
		csrfSecret = padded
	}
	// Build the trusted-origins list. gorilla/csrf compares against the
	// Origin/Referer *host* (user:port, no scheme), so we parse PUBLIC_BASE_URL
	// and pass both host-with-port and host-without-port to cover HTTP and
	// behind-proxy deployments.
	trusted := trustedOrigins(deps.Cfg.HTTP.PublicBaseURL, deps.Cfg.HTTP.Addr)

	csrfMW := csrf.Protect(csrfSecret,
		csrf.Secure(deps.Cfg.CSRF.Secure),
		csrf.HttpOnly(true),
		// Lax instead of Strict: Strict breaks cross-site navigation to the
		// authenticated area, and Lax still protects state-changing POSTs.
		csrf.SameSite(csrf.SameSiteLaxMode),
		csrf.RequestHeader("X-CSRF-Token"),
		csrf.FieldName("csrf_token"),
		csrf.TrustedOrigins(trusted),
		csrf.ErrorHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reason := csrf.FailureReason(r)
			deps.Logger.Warn("csrf rejected",
				slog.String("reason", reason.Error()),
				slog.String("path", r.URL.Path),
				slog.String("origin", r.Header.Get("Origin")),
				slog.String("referer", r.Header.Get("Referer")),
				slog.String("host", r.Host),
				slog.Bool("has_cookie", r.Header.Get("Cookie") != ""),
			)
			w.Header().Set("Content-Type", "application/json")
			http.Error(w, `{"code":"csrf_invalid","title":"CSRF token invalid"}`, http.StatusForbidden)
		})),
	)

	// Health endpoints bypass everything (cheap LB probes).
	r.Group(func(r chi.Router) {
		r.Get("/healthz", healthz)
		r.Get("/readyz", readyz(deps))
	})

	// Prometheus metrics (scrape from your monitoring stack).
	r.Handle("/metrics", promhttpHandler())

	// REST API: CORS + bearer auth, no CSRF.
	r.Route("/api", func(r chi.Router) {
		r.Use(cors.Handler(cors.Options{
			AllowedOrigins:   []string{deps.Cfg.HTTP.PublicBaseURL},
			AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Authorization", "Content-Type", "X-API-Key", "X-CSRF-Token"},
			ExposedHeaders:   []string{"X-Request-ID", "X-RateLimit-Remaining"},
			AllowCredentials: true,
			MaxAge:           300,
		}))
		if deps.APIRouter != nil {
			r.Mount("/v1", deps.APIRouter)
		} else {
			r.Mount("/v1", apiStub())
		}
	})

	// Webhook endpoints (Stripe, etc.): no CSRF (signed externally).
	if deps.WebhookRouter != nil {
		r.Mount("/webhooks", deps.WebhookRouter)
	}

	// Browser routes: CSRF-protected.
	if deps.Router != nil {
		r.Group(func(r chi.Router) {
			r.Use(csrfWrap(csrfMW))
			r.Mount("/", deps.Router)
		})
	} else {
		r.Group(func(r chi.Router) {
			r.Use(csrfWrap(csrfMW))
			r.Get("/", placeholder)
		})
	}

	srv := &http.Server{
		Addr:         deps.Cfg.HTTP.Addr,
		Handler:      r,
		ReadTimeout:  deps.Cfg.HTTP.ReadTimeout,
		WriteTimeout: deps.Cfg.HTTP.WriteTimeout,
		IdleTimeout:  deps.Cfg.HTTP.IdleTimeout,
	}
	return &Server{httpServer: srv, cfg: deps.Cfg, logger: deps.Logger, deps: deps}, nil
}

// ListenAndServe starts the HTTP server. Blocks until Shutdown is called.
func (s *Server) ListenAndServe() error {
	s.logger.Info("http server starting",
		slog.String("addr", s.cfg.HTTP.Addr),
		slog.String("env", s.cfg.AppEnv))
	if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown gracefully drains in-flight requests up to the configured timeout.
func (s *Server) Shutdown(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, s.cfg.HTTP.ShutdownTimeout)
	defer cancel()
	s.logger.Info("http server shutting down")
	return s.httpServer.Shutdown(shutdownCtx)
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// trustedOrigins builds the list of trusted Origin hosts for gorilla/csrf.
// gorilla/csrf compares the request's Origin/Referer *host* (host[:port],
// without scheme) against this list, so we extract the host from PUBLIC_BASE_URL
// and include both host:port and host-only forms to cover proxy/non-proxy setups.
func trustedOrigins(publicURL, httpAddr string) []string {
	var out []string
	if u, err := url.Parse(publicURL); err == nil && u.Host != "" {
		out = append(out, u.Host) // e.g. 46.224.112.113:8080 or pulsar.example.com
		if host, _, err := net.SplitHostPort(u.Host); err == nil {
			out = append(out, host) // e.g. 46.224.112.113
		}
	}
	return out
}

// promhttpHandler exposes the default Prometheus registry.
func promhttpHandler() http.Handler {
	return promhttp.Handler()
}

func readyz(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		ctx := r.Context()
		checks := map[string]string{}
		ok := true
		if deps.DBChecker != nil {
			if err := deps.DBChecker(ctx); err != nil {
				checks["db"] = "error"
				ok = false
			} else {
				checks["db"] = "ok"
			}
		}
		if deps.RedisChecker != nil {
			if err := deps.RedisChecker(ctx); err != nil {
				checks["redis"] = "error"
				ok = false
			} else {
				checks["redis"] = "ok"
			}
		}
		if deps.S3Checker != nil {
			if err := deps.S3Checker(ctx); err != nil {
				checks["s3"] = "error"
				ok = false
			} else {
				checks["s3"] = "ok"
			}
		}
		status := "ready"
		code := http.StatusOK
		if !ok {
			status = "degraded"
			code = http.StatusServiceUnavailable
		}
		w.WriteHeader(code)
		body := `{"status":"` + status + `"`
		for k, v := range checks {
			body += `,"` + k + `":"` + v + `"`
		}
		body += "}"
		_, _ = w.Write([]byte(body))
	}
}

func apiStub() http.Handler {
	r := chi.NewRouter()
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"Pulsar API","version":"v1","docs":"/docs/openapi.yaml"}`))
	})
	return r
}

func placeholder(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><title>Pulsar</title>
<body style="font-family:sans-serif;background:#020617;color:#e2e8f0;padding:3rem">
<h1>Pulsar</h1><p>Сервер запущен. Routes подключаются.</p></body>`))
}

// csrfWrap mounts gorilla/csrf on every request. gorilla/csrf itself only
// enforces tokens on state-changing methods (POST/PUT/PATCH/DELETE), so GETs
// pass through — but running the middleware on GETs is required so that
// csrf.Token(r) is populated and templates can render it into a meta tag.
//
// We also inject the plaintext-HTTP signal: gorilla/csrf defaults to assuming
// HTTPS (requestURL.Scheme="https"), which causes Origin checks to fail for
// plain-HTTP deployments. Marking the request as plaintext makes it use "http"
// so sameOrigin comparisons succeed, and skips the strict Referer allow-list
// that is only meaningful behind TLS.
func csrfWrap(csrfMW func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		protected := csrfMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = r.WithContext(context.WithValue(r.Context(), csrfKey{}, csrf.Token(r)))
			next.ServeHTTP(w, r)
		}))
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Signal plaintext HTTP unless the request arrived over TLS or
			// carries an X-Forwarded-Proto=https header (behind a TLS proxy).
			if r.TLS == nil && !strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
				r = csrf.PlaintextHTTPRequest(r)
			}
			protected.ServeHTTP(w, r)
		})
	}
}

// CSRFToken extracts the CSRF token previously stashed by csrfWrap.
func CSRFToken(ctx context.Context) string {
	if v, ok := ctx.Value(csrfKey{}).(string); ok {
		return v
	}
	return ""
}

type csrfKey struct{}

var _ = time.Now
