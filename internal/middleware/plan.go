package middleware

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
)

// PlanResolver returns the current plan slug for a user. Implemented by
// *repository.SubscriptionsRepo (via its FindByUser method). Kept as an
// interface here to avoid importing the repository package.
type PlanResolver interface {
	PlanSlugForUser(ctx context.Context, userID uuid.UUID) (string, error)
}

// RefreshPlan re-reads the user's current plan from the database on every
// authenticated request and overwrites CtxPlanSlug. This is necessary because
// the session caches the plan slug at login time (and may not even set it),
// so after a plan change (e.g. a successful YooKassa/CryptoBot payment) the
// nav/header would otherwise keep showing the old plan until re-login.
//
// Mounted on /app/*. Anonymous requests pass through untouched. On resolver
// error it logs and falls back to whatever the session already held.
func RefreshPlan(resolver PlanResolver, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uidStr, _ := r.Context().Value(CtxUserID).(string)
			if uidStr != "" {
				if uid, err := uuid.Parse(uidStr); err == nil {
					if slug, err := resolver.PlanSlugForUser(r.Context(), uid); err == nil && slug != "" {
						ctx := context.WithValue(r.Context(), CtxPlanSlug, slug)
						r = r.WithContext(ctx)
					} else if err != nil && logger != nil {
						logger.Warn("refresh plan: resolve failed, keeping session value",
							slog.String("user_id", uidStr),
							slog.Any("err", err),
						)
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
