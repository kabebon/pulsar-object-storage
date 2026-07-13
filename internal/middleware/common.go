// Package middleware contains HTTP middleware used across web and API routes.
package middleware

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// contextKey is an unexported type to avoid key collisions across packages.
type contextKey string

// Keys used to stash request-scoped values.
const (
	CtxRequestID contextKey = "request_id"
	CtxUserID    contextKey = "user_id"
	CtxEmail     contextKey = "email"
	CtxUserName  contextKey = "user_name"
	CtxPlanSlug  contextKey = "plan_slug"
	CtxSessionID contextKey = "session_id"
	CtxAuthVia   contextKey = "auth_via" // "session" | "apikey"
	CtxScopes    contextKey = "scopes"
)

// RequestID exposes chi's request id under our own key + header for downstream logs.
func RequestID(next http.Handler) http.Handler {
	return middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := middleware.GetReqID(r.Context())
		ctx := context.WithValue(r.Context(), CtxRequestID, rid)
		w.Header().Set("X-Request-ID", rid)
		next.ServeHTTP(w, r.WithContext(ctx))
	}))
}

// Logger logs each request as a structured slog entry. It uses the chi
// RequestID when present so logs correlate with the X-Request-ID header.
func Logger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			dur := time.Since(start)
			attrs := []slog.Attr{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", ww.Status()),
				slog.Int("bytes", ww.BytesWritten()),
				slog.Duration("dur", dur),
				slog.String("ip", r.RemoteAddr),
			}
			if rid, ok := r.Context().Value(CtxRequestID).(string); ok && rid != "" {
				attrs = append(attrs, slog.String("req_id", rid))
			}
			logger.LogAttrs(r.Context(), slog.LevelInfo, "http", attrs...)
		})
	}
}

// Recover traps panics, logs a stack trace and returns 500. Prevents a single
// panic from killing the whole process.
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic recovered",
						slog.Any("panic", rec),
						slog.String("stack", string(debug.Stack())),
						slog.String("path", r.URL.Path),
					)
					http.Error(w, `{"code":"internal_error","title":"Internal server error"}`, http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// RealIP forwards the real client IP from X-Forwarded-For behind a proxy.
// It only trusts headers if the direct client IP is in the trusted proxies list.
func RealIP(trusted []string) func(http.Handler) http.Handler {
	var trustedNets []*net.IPNet
	for _, t := range trusted {
		if !strings.Contains(t, "/") {
			if strings.Contains(t, ":") {
				t = t + "/128"
			} else {
				t = t + "/32"
			}
		}
		if _, n, err := net.ParseCIDR(t); err == nil {
			trustedNets = append(trustedNets, n)
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				host = r.RemoteAddr
			}
			ip := net.ParseIP(host)

			isTrusted := false
			for _, n := range trustedNets {
				if n.Contains(ip) {
					isTrusted = true
					break
				}
			}

			if isTrusted {
				if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
					r.RemoteAddr = xrip
				} else if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
					i := strings.Index(xff, ",")
					if i == -1 {
						i = len(xff)
					}
					r.RemoteAddr = strings.TrimSpace(xff[:i])
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
