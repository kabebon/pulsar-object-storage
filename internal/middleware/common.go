// Package middleware contains HTTP middleware used across web and API routes.
package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
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
// We delegate to chi's implementation.
func RealIP(next http.Handler) http.Handler {
	return middleware.RealIP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	}))
}
