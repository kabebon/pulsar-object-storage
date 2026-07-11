// Package web implements server-rendered handlers (htmx + templ) for the
// public site and authenticated dashboard. Routes are registered on a chi
// Router mounted under the browser-facing middleware stack (CSRF, session).
package web

import (
	"net/http"

	"github.com/a-h/templ"

	"pulsar/internal/config"
	"pulsar/internal/middleware"
	"pulsar/internal/server"
	"pulsar/web/views/layouts"
)

// Render writes a templ.Component as an HTML response. Single point through
// which all view rendering flows so we can add cache headers centrally.
func Render(w http.ResponseWriter, r *http.Request, status int, cmp templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_ = cmp.Render(r.Context(), w)
}

// baseProps builds the LayoutProps every page consumes, pulling the CSRF token
// and the authenticated user from the request context.
func baseProps(cfg *config.Config, r *http.Request, title, description, active string) layouts.LayoutProps {
	props := layouts.LayoutProps{
		Title:       title,
		Description: description,
		AppName:     cfg.AppName,
		PublicURL:   cfg.HTTP.PublicBaseURL,
		Active:      active,
		CSRFToken:   server.CSRFToken(r.Context()),
	}
	if uid, _ := r.Context().Value(middleware.CtxUserID).(string); uid != "" {
		email, _ := r.Context().Value(middleware.CtxEmail).(string)
		name, _ := r.Context().Value(middleware.CtxUserName).(string)
		plan, _ := r.Context().Value(middleware.CtxPlanSlug).(string)
		props.Auth = &layouts.AuthInfo{
			ID:       uid,
			Email:    email,
			Name:     name,
			Status:   "active",
			PlanSlug: plan,
		}
	}
	return props
}
