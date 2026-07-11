package web

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"pulsar/internal/config"
	"pulsar/internal/middleware"
	"pulsar/internal/models"
	"pulsar/internal/repository"
	"pulsar/internal/service"
	"pulsar/web/views/pages"
)

// SettingsHandler serves the profile + password change page.
type SettingsHandler struct {
	cfg  *config.Config
	auth *service.AuthService
	users *repository.UsersRepo
}

// NewSettingsHandler wires dependencies.
func NewSettingsHandler(cfg *config.Config, auth *service.AuthService, users *repository.UsersRepo) *SettingsHandler {
	return &SettingsHandler{cfg: cfg, auth: auth, users: users}
}

// Routes registers settings routes (auth-protected).
func (h *SettingsHandler) Routes(r chi.Router) {
	r.Get("/settings", h.show)
	r.Post("/settings", h.updateProfile)
	r.Post("/settings/password", h.updatePassword)
}

func (h *SettingsHandler) show(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, nil, "")
}

func (h *SettingsHandler) updateProfile(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	uid := mustUserID(r)
	name := strings.TrimSpace(r.FormValue("name"))
	if err := h.auth.UpdateProfile(r.Context(), uid, name); err != nil {
		h.render(w, r, []string{"Не удалось сохранить: " + err.Error()}, "")
		return
	}
	h.render(w, r, nil, "Профиль обновлён.")
}

func (h *SettingsHandler) updatePassword(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	uid := mustUserID(r)
	current := r.FormValue("current_password")
	newPw := r.FormValue("new_password")
	confirm := r.FormValue("new_password_confirm")

	if newPw != confirm {
		h.render(w, r, []string{"Новые пароли не совпадают."}, "")
		return
	}
	if len(newPw) < 8 {
		h.render(w, r, []string{"Новый пароль должен быть не короче 8 символов."}, "")
		return
	}
	if err := h.auth.UpdatePassword(r.Context(), uid, current, newPw, clientIP(r), r.UserAgent()); err != nil {
		h.render(w, r, []string{humanizeErr(err)}, "")
		return
	}
	// Password change destroys all sessions → redirect to login.
	clearSessionCookie(w, h.cfg)
	http.Redirect(w, r, "/login?reset=1", http.StatusSeeOther)
}

func (h *SettingsHandler) render(w http.ResponseWriter, r *http.Request, errs []string, success string) {
	uid := mustUserID(r)
	user, err := h.users.FindByID(r.Context(), uid)
	if err != nil {
		http.Error(w, "user not found", http.StatusInternalServerError)
		return
	}
	props := baseProps(h.cfg, r, "Настройки", "", "settings")
	_ = middleware.CtxUserID
	_ = models.UserStatusActive
	Render(w, r, 0, pages.SettingsPage(props, user, errs, success))
}
