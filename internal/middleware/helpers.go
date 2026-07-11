package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"pulsar/internal/cache"
	httperr "pulsar/internal/errors"
)

// writeProblem serializes an AppError as an RFC 9457 application/problem+json
// response, including Retry-After / X-RateLimit-* headers where applicable.
func writeProblem(w http.ResponseWriter, e *httperr.AppError) {
	if e == nil {
		e = httperr.Internal(nil)
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":   "https://pulsar.example.com/errors/" + e.Code,
		"title":  e.Title,
		"status": e.Status,
		"code":   e.Code,
		"detail": e.Detail,
		"fields": e.Fields,
	})
}

// RateLimit returns middleware that enforces a fixed-window limit per IP (or
// per-user when authenticated) using Redis. On overflow it returns 429 with a
// Retry-After header.
func RateLimit(limiter *cache.RateLimiter, keyFn func(r *http.Request) string, limit int64, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFn(r)
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}
			res, err := limiter.Allow(r.Context(), key, limit, window)
			if err != nil {
				// Fail open: log but do not block on Redis errors.
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("X-RateLimit-Limit", strconv.FormatInt(res.Limit, 10))
			w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(res.Remaining, 10))
			if !res.Allowed {
				retry := int(res.RetryAfter.Round(time.Second) / time.Second)
				if retry <= 0 {
					retry = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(retry))
				writeProblem(w, httperr.RateLimited(""))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// LimitByUserOrIP builds a rate-limit key preferring the authenticated user id
// and falling back to client IP.
func LimitByUserOrIP(r *http.Request) string {
	if uid, _ := r.Context().Value(CtxUserID).(string); uid != "" {
		return "u:" + uid
	}
	return "ip:" + clientIP(r)
}

// LimitByIP builds a rate-limit key solely from the client IP.
func LimitByIP(r *http.Request) string {
	return "ip:" + clientIP(r)
}

// LimitByEmail builds a rate-limit key from an email form value (used on login
// / register to slow credential stuffing).
func LimitByEmail(field string) func(*http.Request) string {
	return func(r *http.Request) string {
		_ = r.ParseForm()
		email := r.FormValue(field)
		if email == "" {
			return LimitByIP(r)
		}
		return "e:" + email
	}
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := indexOfComma(xff); i > 0 {
			return xff[:i]
		}
		return xff
	}
	if r.RemoteAddr == "" {
		return "unknown"
	}
	// Strip port.
	host := r.RemoteAddr
	for i := len(host) - 1; i >= 0; i-- {
		if host[i] == ':' {
			return host[:i]
		}
	}
	return host
}

func indexOfComma(s string) int {
	for i, c := range s {
		if c == ',' {
			return i
		}
	}
	return -1
}

// Ensure context import is used even when only aliases are referenced.
var _ context.Context
