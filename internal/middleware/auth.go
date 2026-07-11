package middleware

import (
	"context"
	"net/http"
	"strings"

	"pulsar/internal/cache"
	httperr "pulsar/internal/errors"
)

// SessionStore is the minimal interface the auth middleware needs from the
// session layer. Implemented by cache.SessionStore.
type SessionStore interface {
	Get(ctx context.Context, id string) (*cache.SessionData, error)
	Touch(ctx context.Context, id string) error
}

// readSession extracts and validates the session cookie, stashing the user id
// into the request context. It does not block anonymous requests; downstream
// handlers decide whether authentication is required.
func readSession(r *http.Request, store SessionStore, cookieName string) *cache.SessionData {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	data, err := store.Get(r.Context(), c.Value)
	if err != nil {
		return nil
	}
	// Best-effort sliding expiration; ignore error.
	_ = store.Touch(r.Context(), c.Value)
	return data
}

// WithSession populates user context values when a valid session exists. It
// always calls next, so it is safe to mount globally.
func WithSession(store SessionStore, cookieName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if data := readSession(r, store, cookieName); data != nil {
				ctx := r.Context()
				ctx = context.WithValue(ctx, CtxUserID, data.UserID)
				ctx = context.WithValue(ctx, CtxEmail, data.Email)
				ctx = context.WithValue(ctx, CtxUserName, data.Name)
				ctx = context.WithValue(ctx, CtxPlanSlug, data.PlanSlug)
				ctx = context.WithValue(ctx, CtxSessionID, sidFromCookie(r, cookieName))
				ctx = context.WithValue(ctx, CtxAuthVia, "session")
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAuth rejects requests without an authenticated user. For API routes
// it returns RFC 9457 JSON; for browser navigation it redirects to /login.
func RequireAuth(isAPI bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if uid, _ := r.Context().Value(CtxUserID).(string); uid != "" {
				next.ServeHTTP(w, r)
				return
			}
			if isAPI {
				writeProblem(w, httperr.Unauthorized(""))
			} else {
				nextPath := r.URL.RequestURI()
				http.Redirect(w, r, "/login?next="+nextPath, http.StatusSeeOther)
			}
		})
	}
}

// APIKeyAuth authenticates REST API callers using a Bearer token resolved by
// the provided resolver. On success it tags the request with user id + via.
func APIKeyAuth(resolve func(ctx context.Context, token string) (userID, email string, err error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// If already authenticated via session, allow pass-through.
			if uid, _ := r.Context().Value(CtxUserID).(string); uid != "" {
				next.ServeHTTP(w, r)
				return
			}
			token := bearerToken(r)
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}
			uid, email, err := resolve(r.Context(), token)
			if err != nil {
				writeProblem(w, httperr.Unauthorized("Invalid API key"))
				return
			}
			ctx := r.Context()
			ctx = context.WithValue(ctx, CtxUserID, uid)
			ctx = context.WithValue(ctx, CtxEmail, email)
			ctx = context.WithValue(ctx, CtxAuthVia, "apikey")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	const prefix = "Bearer "
	if len(h) >= len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	// Also accept raw keys via X-API-Key for S3-like ergonomics.
	if k := r.Header.Get("X-API-Key"); k != "" {
		return k
	}
	return ""
}

func sidFromCookie(r *http.Request, name string) string {
	if c, err := r.Cookie(name); err == nil {
		return c.Value
	}
	return ""
}
