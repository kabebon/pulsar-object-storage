// Command pulsar is the entry point for the Pulsar object storage service.
// It wires configuration, infrastructure (DB, Redis, S3), services and HTTP
// handlers, then runs the server with graceful shutdown on SIGINT/SIGTERM.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"

	"pulsar/internal/billing"
	"pulsar/internal/cache"
	"pulsar/internal/config"
	domain "pulsar/internal/domain"
	apihandler "pulsar/internal/handler/api"
	webhandler "pulsar/internal/handler/web"
	"pulsar/internal/mailer"
	"pulsar/internal/middleware"
	"pulsar/internal/repository"
	"pulsar/internal/server"
	"pulsar/internal/service"
	s3store "pulsar/internal/storage/s3"
)

func main() {
	logger := newLogger()
	if err := run(logger); err != nil {
		logger.Error("fatal error", slog.Any("err", err))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger.Info("configuration loaded",
		slog.String("env", cfg.AppEnv),
		slog.String("http_addr", cfg.HTTP.Addr),
	)

	// --- Infrastructure ---
	db, err := repository.New(ctx, cfg.DB)
	if err != nil {
		return err
	}
	defer db.Close()
	logger.Info("database connected")

	migrationsDir := os.Getenv("MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "migrations"
	}
	if err := repository.Migrate(cfg.DB, migrationsDir); err != nil {
		logger.Warn("migrations failed; continuing", slog.Any("err", err))
	} else {
		logger.Info("database migrations applied")
	}

	rc, err := cache.New(ctx, cfg.Redis)
	if err != nil {
		return err
	}
	defer rc.Close()
	logger.Info("redis connected")

	// --- Repositories & stores ---
	usersRepo := repository.NewUsersRepo(db)
	tokensRepo := repository.NewEmailVerificationsRepo(db)
	auditRepo := repository.NewAuditLogRepo(db)
	bucketsRepo := repository.NewBucketsRepo(db)
	objectsRepo := repository.NewObjectsRepo(db)
	usageRepo := repository.NewUsageRepo(db)
	plansRepo := repository.NewPlansRepo(db)
	subsRepo := repository.NewSubscriptionsRepo(db)
	sessionStore := cache.NewSessionStore(rc, cfg.Session.TTL)
	rateLimiter := cache.NewRateLimiter(rc)

	// --- Mailer ---
	var mailSvc mailer.Mailer
	if cfg.SMTP.Host != "" && cfg.SMTP.Port > 0 {
		mailSvc = mailer.New(cfg.SMTP)
		logger.Info("smtp mailer configured", slog.String("host", cfg.SMTP.Host))
	} else {
		mailSvc = mailer.NewLogMailer()
		logger.Warn("smtp not configured; using in-memory mailer")
	}

	// --- Services ---
	authService := service.NewAuthService(service.AuthDeps{
		Users:     usersRepo,
		Tokens:    tokensRepo,
		Audit:     auditRepo,
		Sessions:  sessionStore,
		Mailer:    mailSvc,
		EmailTTL:  24 * time.Hour,
		PublicURL: cfg.HTTP.PublicBaseURL,
	})

	// --- Object storage (S3) ---
	s3Client, err := s3store.New(ctx, cfg.S3)
	if err != nil {
		logger.Warn("s3 client init failed; storage disabled", slog.Any("err", err))
	}
	if s3Client != nil {
		if err := s3Client.EnsureBucket(ctx); err != nil {
			logger.Warn("s3 ensure bucket failed", slog.Any("err", err))
		} else {
			logger.Info("s3 bucket ready", slog.String("bucket", cfg.S3.Bucket))
		}
		// Presigned upload/download URLs are signed against S3_PUBLIC_ENDPOINT
		// when set. Without it they fall back to the internal S3 endpoint,
		// which leaks the origin host/IP to every client — unacceptable behind
		// a CDN. Warn loudly in production.
		if cfg.S3.PublicEndpoint == "" {
			if cfg.IsProduction() {
				logger.Warn("S3_PUBLIC_ENDPOINT is not set: presigned URLs will expose the origin host — set it to your CDN URL")
			} else {
				logger.Info("S3_PUBLIC_ENDPOINT not set; presigned URLs use the internal endpoint (fine for local dev)")
			}
		} else {
			logger.Info("s3 public endpoint configured", slog.String("endpoint", cfg.S3.PublicEndpoint))
		}
	}

	// Plan resolver: returns current limits for the user (used by quotas).
	planResolver := func(ctx context.Context, userID uuid.UUID) (service.PlanLimits, error) {
		return resolvePlanLimits(ctx, plansRepo, subsRepo, userID)
	}

	storageService := service.NewStorageService(service.StorageDeps{
		Buckets:      bucketsRepo,
		Objects:      objectsRepo,
		Audit:        auditRepo,
		Usage:        usageRepo,
		S3:           s3Client,
		PlanResolver: planResolver,
	})

	// API keys service.
	apiKeysRepo := repository.NewAPIKeysRepo(db)
	apiKeyService := service.NewAPIKeyService(service.APIKeyDeps{
		Keys:  apiKeysRepo,
		Audit: auditRepo,
	})

	// Billing — YooKassa, CryptoBot.
	// Each provider runs in no-provider mode when its key/token is missing.

	yookassaSvc := billing.NewYooKassa(cfg.YooKassa, plansRepo, subsRepo, usersRepo)
	if yookassaSvc.Enabled() {
		logger.Info("yookassa billing enabled", slog.String("shop_id", cfg.YooKassa.ShopID))
	} else {
		logger.Info("yookassa billing disabled (no YOOKASSA_SHOP_ID / YOOKASSA_SECRET_KEY)")
	}

	cryptobotSvc := billing.NewCryptoBot(cfg.CryptoBot, plansRepo, subsRepo, usersRepo)
	if cryptobotSvc.Enabled() {
		logger.Info("cryptobot billing enabled", slog.String("network", cfg.CryptoBot.Network))
	} else {
		logger.Info("cryptobot billing disabled (no CRYPTOBOT_TOKEN)")
	}

	billingH := webhandler.NewBillingHandler(cfg, yookassaSvc, cryptobotSvc, subsRepo, plansRepo, usageRepo)

	// Custom domains + CDN.
	domainsRepo := repository.NewCustomDomainsRepo(db)
	domainSvc := domain.New(domain.Deps{
		Domains:   domainsRepo,
		Buckets:   bucketsRepo,
		Audit:     auditRepo,
		Plans:     plansRepo,
		Subs:      subsRepo,
		CDNTarget: cfg.CDN.DefaultDomain,
	})
	domainsH := webhandler.NewDomainsHandler(cfg, domainSvc, bucketsRepo)

	// --- HTTP server ---
	rlProvider := newRateLimitProvider(rateLimiter)
	var webRouter http.Handler
	if s3Client != nil {
		webRouter = webhandler.NewRouter(cfg, logger, authService, storageService, apiKeyService, apiKeysRepo, billingH, domainsH, usersRepo, auditRepo, subsRepo, rlProvider)
	} else {
		webRouter = webhandler.NewRouter(cfg, logger, authService, nil, apiKeyService, apiKeysRepo, billingH, domainsH, usersRepo, auditRepo, subsRepo, rlProvider)
	}
	_ = rlProvider // used inside router

	// Webhook router (Stripe + YooKassa + CryptoBot) — bypasses CSRF, always mounted.
	var webhookRouter http.Handler
	webhookRouter = apihandler.NewWebhooksHandler(yookassaSvc, cryptobotSvc).Routes()

	// REST API v1 router (bearer-token auth, no CSRF).
	var apiRouter http.Handler
	if s3Client != nil {
		apiRateLimit := middleware.RateLimit(rateLimiter, middleware.LimitByUserOrIP, 300, time.Minute)
		apiRouter = apihandler.NewRouter(apihandler.RouterDeps{
			Storage:  storageService,
			Keys:     apiKeyService,
			KeysRepo: apiKeysRepo,
			Users:    usersRepo,
			Usage:    usageRepo,
			Domains:  domainSvc,
		}, apiRateLimit)
	} else {
		// Even without S3, mount the public on-demand TLS endpoint so Caddy
		// can reach it.
		apiRouter = apihandler.NewRouter(apihandler.RouterDeps{Domains: domainSvc}, nil)
	}

	srv, err := server.New(server.Deps{
		Cfg:           cfg,
		Logger:        logger,
		SessionStore:  sessionStore,
		Router:        webRouter,
		APIRouter:     apiRouter,
		WebhookRouter: webhookRouter,
		RateLimiter:   rlProvider,
		DBChecker: func(ctx context.Context) error {
			return db.Pool.Ping(ctx)
		},
		RedisChecker: func(ctx context.Context) error {
			return rc.Raw().Ping(ctx).Err()
		},
		S3Checker: func(ctx context.Context) error {
			if s3Client == nil {
				return nil
			}
			return s3Client.Ping(ctx)
		},
	})
	if err != nil {
		return err
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		if err != nil {
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", slog.Any("err", err))
		return err
	}
	logger.Info("shutdown complete")
	return nil
}

// newRateLimitProvider bridges the cache rate-limiter into the middleware
// provider interface used by handlers. The closure captures rl so each call
// performs a real Redis-backed check.
func newRateLimitProvider(rl *cache.RateLimiter) middleware.RateLimiterProvider {
	return middleware.NewRateLimiterProvider(func(key string, limit int64, window time.Duration) (bool, time.Duration) {
		// Use the request context of the caller would be ideal, but the
		// provider signature is context-free for simplicity. Fall back to
		// a background context with a short timeout to avoid stalling.
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		res, err := rl.Allow(ctx, key, limit, window)
		if err != nil {
			// Fail open: never block on Redis errors.
			return true, 0
		}
		return res.Allowed, res.RetryAfter
	})
}

// resolvePlanLimits returns the effective quota limits for a user based on
// their active subscription. Falls back to the free plan on any error.
func resolvePlanLimits(ctx context.Context, plans *repository.PlansRepo, subs *repository.SubscriptionsRepo, userID uuid.UUID) (service.PlanLimits, error) {
	sub, err := subs.FindByUser(ctx, userID)
	planSlug := "free"
	if err == nil && sub != nil {
		planSlug = sub.PlanSlug
	}
	plan, err := plans.FindBySlug(ctx, planSlug)
	if err != nil || plan == nil {
		// Conservative fallback: tiny free limits.
		return service.PlanLimits{Slug: "free", StorageBytes: 5 << 30, MaxBuckets: 3}, nil
	}
	return service.PlanLimits{
		Slug:                plan.Slug,
		StorageBytes:        plan.StorageGB << 30,
		BandwidthBytesMonth: plan.BandwidthGBMonth << 30,
		MaxBuckets:          plan.MaxBuckets,
	}, nil
}

func newLogger() *slog.Logger {
	level := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		level = slog.LevelDebug
	}
	if os.Getenv("APP_ENV") == "production" {
		return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}

// guard against accidental removal of imports used only in fallback paths.
var _ = errors.Is
