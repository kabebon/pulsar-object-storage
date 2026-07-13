// Package config loads application configuration from environment variables.
// All settings have sensible defaults for local development; production values
// are injected via the environment (see deploy/.env.example).
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration for the Pulsar service.
type Config struct {
	AppEnv      string // "local" | "production" | "test"
	AppName     string
	HTTP        HTTPConfig
	DB          DBConfig
	Redis       RedisConfig
	S3          S3Config
	JWT         JWTConfig
	SMTP        SMTPConfig
	Stripe      StripeConfig
	YooKassa    YooKassaConfig
	CryptoBot   CryptoBotConfig
	Session     SessionConfig
	CSRF        CSRFConfig
	CDN         CDNConfig
	StorageRoot string // public base URL used to build CDN links
}

type HTTPConfig struct {
	Addr            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
	// PublicBaseURL is the canonical external URL of the service, used to build
	// absolute links (e.g. in verification emails). Example: https://pulsar.example.com
	PublicBaseURL string
	// TrustedProxies is a comma-separated list of CIDRs trusted to set X-Forwarded-*.
	TrustedProxies []string
}

type DBConfig struct {
	DSN             string
	MaxOpenConns    int32
	MaxIdleConns    int32
	ConnMaxLifetime time.Duration
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

type S3Config struct {
	Endpoint      string // e.g. http://minio:9000
	Region        string
	AccessKey     string
	SecretKey     string
	Bucket        string // default bucket for the platform
	UsePathStyle  bool
	PresignExpiry time.Duration
	// PublicEndpoint is the scheme+host clients use to reach the bucket through
	// a CDN (e.g. https://cdn.example.com). When set, presigned upload/download
	// URLs are re-signed against this host so the origin Endpoint is never
	// exposed to clients. Empty = fall back to the internal Endpoint (local dev).
	PublicEndpoint string
}

type JWTConfig struct {
	Secret          string
	AccessTokenTTL  time.Duration
}

type SessionConfig struct {
	CookieName string
	TTL        time.Duration
	Secure     bool
	Domain     string
}

type CSRFConfig struct {
	Secure bool
	Domain string
}

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string // e.g. "Pulsar <no-reply@pulsar.example.com>"
}

type StripeConfig struct {
	SecretKey          string
	WebhookSecret      string
	PriceMonthlyPro    string
	PriceYearlyPro     string
	PriceMonthlyBiz    string
	PriceYearlyBiz     string
}

type CDNConfig struct {
	// DefaultDomain is the host used for generated CDN URLs, e.g. cdn.pulsar.example.com
	DefaultDomain string
	// SignKey used for signed CDN URLs (HMAC-SHA256)
	SignKey string
}

// YooKassaConfig holds credentials for the YooKassa payment gateway.
// When ShopID or SecretKey is empty the service runs in no-provider mode.
type YooKassaConfig struct {
	ShopID    string // YOOKASSA_SHOP_ID
	SecretKey string // YOOKASSA_SECRET_KEY
}

// CryptoBotConfig holds credentials for the Crypto Pay API (CryptoBot / Telegram).
// Network selects the API host: "mainnet" → pay.crypt.bot, "testnet" → testnet-pay.crypt.bot.
// When Token is empty the service runs in no-provider mode.
type CryptoBotConfig struct {
	Token   string // CRYPTOBOT_TOKEN
	Network string // "mainnet" | "testnet" (default: "mainnet")
}

// Load reads configuration from environment variables. Missing required values
// fall back to local-development defaults so that `go run` works out of the box.
func Load() (*Config, error) {
	cfg := &Config{
		AppEnv:  env("APP_ENV", "local"),
		AppName: env("APP_NAME", "Pulsar"),
		HTTP: HTTPConfig{
			Addr:            env("HTTP_ADDR", ":8080"),
			ReadTimeout:     envDuration("HTTP_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:    envDuration("HTTP_WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:     envDuration("HTTP_IDLE_TIMEOUT", 60*time.Second),
			ShutdownTimeout: envDuration("HTTP_SHUTDOWN_TIMEOUT", 15*time.Second),
			PublicBaseURL:   env("PUBLIC_BASE_URL", "http://localhost:8080"),
			TrustedProxies:  envSlice("TRUSTED_PROXIES"),
		},
		DB: DBConfig{
			DSN:             env("DATABASE_URL", "postgres://pulsar:pulsar@localhost:5432/pulsar?sslmode=disable"),
			MaxOpenConns:    int32(envInt("DB_MAX_OPEN_CONNS", 25)),
			MaxIdleConns:    int32(envInt("DB_MAX_IDLE_CONNS", 5)),
			ConnMaxLifetime: envDuration("DB_CONN_MAX_LIFETIME", time.Hour),
		},
		Redis: RedisConfig{
			Addr:     env("REDIS_ADDR", "localhost:6379"),
			Password: env("REDIS_PASSWORD", ""),
			DB:       envInt("REDIS_DB", 0),
		},
		S3: S3Config{
			Endpoint:        env("S3_ENDPOINT", "http://localhost:9000"),
			Region:          env("S3_REGION", "us-east-1"),
			AccessKey:       env("S3_ACCESS_KEY", "pulsar"),
			SecretKey:       env("S3_SECRET_KEY", "pulsar12345"),
			Bucket:          env("S3_BUCKET", "pulsar"),
			UsePathStyle:    envBool("S3_USE_PATH_STYLE", true),
			PresignExpiry:  envDuration("S3_PRESIGN_EXPIRY", 15*time.Minute),
			PublicEndpoint: env("S3_PUBLIC_ENDPOINT", ""),
		},
		JWT: JWTConfig{
			Secret:         env("JWT_SECRET", "change-me-in-production-please-32-bytes-min"),
			AccessTokenTTL: envDuration("JWT_ACCESS_TOKEN_TTL", 15*time.Minute),
		},
		Session: SessionConfig{
			CookieName: env("SESSION_COOKIE_NAME", "pulsar_sid"),
			TTL:        envDuration("SESSION_TTL", 7*24*time.Hour),
			Secure:     envBool("SESSION_COOKIE_SECURE", false),
			Domain:     env("SESSION_COOKIE_DOMAIN", ""),
		},
		CSRF: CSRFConfig{
			Secure: envBool("CSRF_SECURE", false),
			Domain: env("CSRF_DOMAIN", ""),
		},
		SMTP: SMTPConfig{
			Host:     env("SMTP_HOST", "localhost"),
			Port:     envInt("SMTP_PORT", 1025),
			Username: env("SMTP_USERNAME", ""),
			Password: env("SMTP_PASSWORD", ""),
			From:     env("SMTP_FROM", "Pulsar <no-reply@localhost>"),
		},
		Stripe: StripeConfig{
			SecretKey:       env("STRIPE_SECRET_KEY", ""),
			WebhookSecret:   env("STRIPE_WEBHOOK_SECRET", ""),
			PriceMonthlyPro: env("STRIPE_PRICE_MONTHLY_PRO", ""),
			PriceYearlyPro:  env("STRIPE_PRICE_YEARLY_PRO", ""),
			PriceMonthlyBiz: env("STRIPE_PRICE_MONTHLY_BUSINESS", ""),
			PriceYearlyBiz:  env("STRIPE_PRICE_YEARLY_BUSINESS", ""),
		},
		YooKassa: YooKassaConfig{
			ShopID:    env("YOOKASSA_SHOP_ID", ""),
			SecretKey: env("YOOKASSA_SECRET_KEY", ""),
		},
		CryptoBot: CryptoBotConfig{
			Token:   env("CRYPTOBOT_TOKEN", ""),
			Network: env("CRYPTOBOT_NETWORK", "mainnet"),
		},
		CDN: CDNConfig{
			DefaultDomain: env("CDN_DEFAULT_DOMAIN", "cdn.localhost"),
			SignKey:       env("CDN_SIGN_KEY", "change-me-cdn-signKey"),
		},
		StorageRoot: env("STORAGE_ROOT", ""),
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// IsProduction reports whether the service runs in production mode.
func (c *Config) IsProduction() bool { return c.AppEnv == "production" }

// IsTest reports whether the service runs in test mode.
func (c *Config) IsTest() bool { return c.AppEnv == "test" }

func (c *Config) validate() error {
	var errs []string
	if c.HTTP.PublicBaseURL == "" {
		errs = append(errs, "PUBLIC_BASE_URL must be set")
	}
	if c.JWT.Secret != "" && len(c.JWT.Secret) < 32 && c.IsProduction() {
		errs = append(errs, "JWT_SECRET must be at least 32 bytes in production")
	}
	if c.S3.Bucket == "" {
		errs = append(errs, "S3_BUCKET must be set")
	}
	if !c.Session.Secure && c.IsProduction() {
		errs = append(errs, "SESSION_COOKIE_SECURE must be true in production")
	}
	if !c.CSRF.Secure && c.IsProduction() {
		errs = append(errs, "CSRF_SECURE must be true in production")
	}
	if len(errs) > 0 {
		return fmt.Errorf("invalid config: %s", strings.Join(errs, "; "))
	}
	return nil
}

// --- helpers ---

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func envSlice(key string) []string {
	v := strings.TrimSpace(env(key, ""))
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}
