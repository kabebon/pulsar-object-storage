package middleware

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// RateLimiterProvider decouples handlers from the concrete cache.RateLimiter.
// Handlers depend on this interface so the web package does not import cache.
type RateLimiterProvider interface {
	// RateLimit returns a chi-compatible middleware enforcing (limit,window)
	// per the key extracted by keyFn. name is informational (for metrics).
	RateLimit(name string, limit int64, window string, keyFn func(*http.Request) string) func(http.Handler) http.Handler
}

// limitFunc is the narrow contract a provider needs from a backend limiter:
// given a resolved key, (limit, window) decide if the request may proceed.
type limitFunc func(key string, limit int64, window time.Duration) (allowed bool, retryAfter time.Duration)

// limiterProvider adapts a limitFunc into the provider interface.
type limiterProvider struct {
	allow limitFunc
}

// NewRateLimiterProvider wraps a limitFunc into the provider interface.
// limitFunc is typically a closure over *cache.RateLimiter.Allow.
func NewRateLimiterProvider(allow limitFunc) RateLimiterProvider {
	return &limiterProvider{allow: allow}
}

func (p *limiterProvider) RateLimit(name string, limit int64, window string, keyFn func(*http.Request) string) func(http.Handler) http.Handler {
	d, err := time.ParseDuration(window)
	if err != nil || d <= 0 {
		d = time.Minute
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if p.allow == nil || keyFn == nil {
				next.ServeHTTP(w, r)
				return
			}
			key := keyFn(r)
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}
			allowed, retry := p.allow(name+":"+key, limit, d)
			if !allowed {
				secs := int(retry.Round(time.Second) / time.Second)
				if secs <= 0 {
					secs = 1
				}
				w.Header().Set("Retry-After", itoa(secs))
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"code":"rate_limited","title":"Rate limited"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// itoa keeps this file free of strconv import for a single call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// chi compatibility marker so chi.Router is referenced.
var _ chi.Router = (chi.Router)(nil)
