package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"pulsar/internal/config"
	"pulsar/internal/middleware"
	"pulsar/internal/models"
	"pulsar/internal/service"
	"pulsar/web/views/layouts"
	"pulsar/web/views/pages"
)

// AuthHandler serves the registration, login, verification and password-reset
// pages. All forms are CSRF-protected and rate-limited at the route layer.
type AuthHandler struct {
	cfg  *config.Config
	auth *service.AuthService
}

// NewAuthHandler wires dependencies.
func NewAuthHandler(cfg *config.Config, auth *service.AuthService) *AuthHandler {
	return &AuthHandler{cfg: cfg, auth: auth}
}

// Routes registers the public auth pages onto the provided router.
func (h *AuthHandler) Routes(r chi.Router) {
	r.Get("/register", h.showRegister)
	r.Post("/register", h.submitRegister)
	r.Get("/login", h.showLogin)
	r.Post("/login", h.submitLogin)
	r.Get("/logout", h.logout)
	r.Post("/logout", h.logout)
	r.Get("/forgot-password", h.showForgot)
	r.Post("/forgot-password", h.submitForgot)
	r.Get("/reset-password", h.showReset)
	r.Post("/reset-password", h.submitReset)
	r.Get("/verify-email", h.verifyEmail)
	r.Get("/verify-email/resend", h.showResend)
	r.Post("/verify-email/resend", h.submitResend)
}

// --- show pages ---

func (h *AuthHandler) showRegister(w http.ResponseWriter, r *http.Request) {
	if isAuthed(r) {
		http.Redirect(w, r, "/app", http.StatusSeeOther)
		return
	}
	props := baseProps(h.cfg, r, "Регистрация", "", "")
	Render(w, r, 0, pages.Register(pages.AuthPageProps{Layout: props, Email: r.URL.Query().Get("email")}))
}

func (h *AuthHandler) showLogin(w http.ResponseWriter, r *http.Request) {
	if isAuthed(r) {
		http.Redirect(w, r, "/app", http.StatusSeeOther)
		return
	}
	props := baseProps(h.cfg, r, "Вход", "", "")
	Render(w, r, 0, pages.Login(pages.AuthPageProps{Layout: props}))
}

func (h *AuthHandler) showForgot(w http.ResponseWriter, r *http.Request) {
	props := baseProps(h.cfg, r, "Восстановление пароля", "", "")
	Render(w, r, 0, pages.ForgotPassword(pages.AuthPageProps{Layout: props}))
}

func (h *AuthHandler) showReset(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		http.Redirect(w, r, "/forgot-password", http.StatusSeeOther)
		return
	}
	props := baseProps(h.cfg, r, "Новый пароль", "", "")
	Render(w, r, 0, pages.ResetPassword(pages.AuthPageProps{Layout: props, Token: token}))
}

func (h *AuthHandler) showResend(w http.ResponseWriter, r *http.Request) {
	props := baseProps(h.cfg, r, "Отправить письмо повторно", "", "")
	Render(w, r, 0, pages.VerifyEmail(pages.AuthPageProps{Layout: props, Success: ""}))
}

func (h *AuthHandler) verifyEmail(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	props := baseProps(h.cfg, r, "Подтверждение email", "", "")
	if token == "" {
		Render(w, r, 0, pages.VerifyEmail(pages.AuthPageProps{
			Layout: props, Errors: []string{"Ссылка недействительна."},
		}))
		return
	}
	if err := h.auth.VerifyEmail(r.Context(), token, clientIP(r), r.UserAgent()); err != nil {
		Render(w, r, 0, pages.VerifyEmail(pages.AuthPageProps{
			Layout: props, Errors: []string{humanizeErr(err)},
		}))
		return
	}
	Render(w, r, 0, pages.VerifyEmail(pages.AuthPageProps{
		Layout: props, Success: "Email подтверждён. Теперь можно войти.",
	}))
}

// --- submit handlers ---

func (h *AuthHandler) submitRegister(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderRegisterErr(w, r, nil, []string{"Некорректные данные формы."})
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	props := baseProps(h.cfg, r, "Регистрация", "", "")

	if errs := validateRegister(name, email, password); len(errs) > 0 {
		h.renderRegisterErr(w, r, &props, errs)
		return
	}

	if _, err := h.auth.Register(r.Context(), name, email, password, clientIP(r), r.UserAgent()); err != nil {
		h.renderRegisterErr(w, r, &props, []string{humanizeErr(err)})
		return
	}
	Render(w, r, 0, pages.VerifyEmail(pages.AuthPageProps{
		Layout: props, Success: "", // Empty success triggers the "Проверьте почту" view
	}))
}

func (h *AuthHandler) submitLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		props := baseProps(h.cfg, r, "Вход", "", "")
		props.Errors = []string{"Некорректные данные формы."}
		Render(w, r, 0, pages.Login(pages.AuthPageProps{Layout: props}))
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	props := baseProps(h.cfg, r, "Вход", "", "")

	// Create a session-scoped store through the auth service to keep cookie
	// concerns inside the handler layer.
	data, err := h.auth.Login(r.Context(), email, password, clientIP(r), r.UserAgent())
	if err != nil {
		Render(w, r, 0, pages.Login(pages.AuthPageProps{Layout: props, Email: email, Errors: []string{humanizeErr(err)}}))
		return
	}
	sid, err := h.auth.NewSession(r.Context(), *data)
	if err != nil {
		props.Errors = []string{"Не удалось создать сессию. Попробуйте ещё раз."}
		Render(w, r, 0, pages.Login(pages.AuthPageProps{Layout: props}))
		return
	}
	setSessionCookie(w, h.cfg, sid)
	http.Redirect(w, r, sanitizeNext(r.FormValue("next")), http.StatusSeeOther)
}

func (h *AuthHandler) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(h.cfg.Session.CookieName); err == nil {
		_ = h.auth.Logout(r.Context(), c.Value)
	}
	clearSessionCookie(w, h.cfg)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *AuthHandler) submitForgot(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	_ = h.auth.StartPasswordReset(r.Context(), email, clientIP(r), r.UserAgent())
	props := baseProps(h.cfg, r, "Восстановление пароля", "", "")
	Render(w, r, 0, pages.ForgotPassword(pages.AuthPageProps{
		Layout: props, Email: email,
		Success: "Если аккаунт с таким email существует, мы отправили инструкцию.",
	}))
}

func (h *AuthHandler) submitReset(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	token := strings.TrimSpace(r.FormValue("token"))
	password := r.FormValue("password")
	confirm := r.FormValue("password_confirm")
	props := baseProps(h.cfg, r, "Новый пароль", "", "")

	if token == "" {
		Render(w, r, 0, pages.ResetPassword(pages.AuthPageProps{
			Layout: props, Token: token, Errors: []string{"Ссылка недействительна."},
		}))
		return
	}
	if password != confirm {
		Render(w, r, 0, pages.ResetPassword(pages.AuthPageProps{
			Layout: props, Token: token, Errors: []string{"Пароли не совпадают."},
		}))
		return
	}
	if err := validatePassword(password); err != nil {
		Render(w, r, 0, pages.ResetPassword(pages.AuthPageProps{
			Layout: props, Token: token, Errors: []string{err.Error()},
		}))
		return
	}
	if err := h.auth.CompletePasswordReset(r.Context(), token, password, clientIP(r), r.UserAgent()); err != nil {
		Render(w, r, 0, pages.ResetPassword(pages.AuthPageProps{
			Layout: props, Token: token, Errors: []string{humanizeErr(err)},
		}))
		return
	}
	http.Redirect(w, r, "/login?verified=1", http.StatusSeeOther)
}

func (h *AuthHandler) submitResend(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	_ = h.auth.ResendVerification(r.Context(), email, clientIP(r), r.UserAgent())
	props := baseProps(h.cfg, r, "Отправить письмо повторно", "", "")
	Render(w, r, 0, pages.VerifyEmail(pages.AuthPageProps{
		Layout: props, 
		Success: "Если аккаунт существует и не подтверждён, мы отправили письмо повторно.",
	}))
}

// --- helpers ---

func (h *AuthHandler) renderRegisterErr(w http.ResponseWriter, r *http.Request, props *layouts.LayoutProps, errs []string) {
	if props == nil {
		p := baseProps(h.cfg, r, "Регистрация", "", "")
		props = &p
	}
	Render(w, r, http.StatusOK, pages.Register(pages.AuthPageProps{
		Layout: *props, Errors: errs,
		Email: r.FormValue("email"), Name: r.FormValue("name"),
	}))
}

// validateRegister returns a list of human-readable field errors.
func validateRegister(name, email, password string) []string {
	var errs []string
	if strings.TrimSpace(name) == "" {
		errs = append(errs, "Укажите имя.")
	}
	if !looksLikeEmail(email) {
		errs = append(errs, "Укажите корректный email.")
	}
	if err := validatePassword(password); err != nil {
		errs = append(errs, err.Error())
	}
	return errs
}

func validatePassword(p string) error {
	if len(p) < 8 {
		return errors.New("Пароль должен быть не короче 8 символов.")
	}
	if len(p) > 72 {
		return errors.New("Пароль не должен превышать 72 символа.")
	}
	return nil
}

func looksLikeEmail(s string) bool {
	s = strings.TrimSpace(s)
	at := strings.IndexByte(s, '@')
	return at > 0 && at < len(s)-1 && strings.IndexByte(s[at+1:], '.') >= 0
}

// humanizeErr translates a service error into a user-facing Russian message.
func humanizeErr(err error) string {
	switch {
	case errors.Is(err, models.ErrInvalidCredentials):
		return "Неверный email или пароль."
	case errors.Is(err, models.ErrConflict):
		return "Аккаунт с таким email уже существует."
	case errors.Is(err, models.ErrTokenExpired):
		return "Срок действия ссылки истёк. Запросите новую."
	case errors.Is(err, models.ErrTokenConsumed):
		return "Эта ссылка уже была использована."
	case errors.Is(err, models.ErrNotFound):
		return "Объект не найден."
	case errors.Is(err, models.ErrValidation):
		if msg := unwrapMessage(err); msg != "" {
			return msg
		}
		return "Проверьте введённые данные."
	case errors.Is(err, models.ErrForbidden):
		return "Доступ запрещён."
	default:
		return "Произошла ошибка. Попробуйте позже."
	}
}

// unwrapMessage returns the message attached via repository.Wrap, if any.
// repository.Wrap returns a *wrappedErr whose Error() is the message text.
func unwrapMessage(err error) string {
	if err == nil {
		return ""
	}
	// Sentinel values have stable messages; wrapped ones carry a real message.
	msg := err.Error()
	for _, sentinel := range []error{
		models.ErrNotFound, models.ErrConflict, models.ErrValidation,
		models.ErrForbidden, models.ErrInvalidCredentials, models.ErrUnauthorized,
	} {
		if msg == sentinel.Error() {
			return ""
		}
	}
	return msg
}

func isAuthed(r *http.Request) bool {
	uid, _ := r.Context().Value(middleware.CtxUserID).(string)
	return uid != ""
}

// setSessionCookie attaches the session cookie with sensible defaults.
func setSessionCookie(w http.ResponseWriter, cfg *config.Config, sid string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cfg.Session.CookieName,
		Value:    sid,
		Path:     "/",
		MaxAge:   int(cfg.Session.TTL.Seconds()),
		HttpOnly: true,
		Secure:   cfg.Session.Secure,
		SameSite: http.SameSiteLaxMode,
		Domain:   cfg.Session.Domain,
	})
}

func clearSessionCookie(w http.ResponseWriter, cfg *config.Config) {
	http.SetCookie(w, &http.Cookie{
		Name: cfg.Session.CookieName, Value: "", Path: "/",
		MaxAge: -1, HttpOnly: true, Secure: cfg.Session.Secure,
		SameSite: http.SameSiteLaxMode, Domain: cfg.Session.Domain,
	})
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host := r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i > 0 {
		return host[:i]
	}
	return host
}

func sanitizeNext(next string) string {
	u, err := url.Parse(next)
	if err != nil || u == nil || u.Host != "" || u.Scheme != "" {
		return "/app"
	}
	if !strings.HasPrefix(u.Path, "/") {
		return "/app"
	}
	return u.Path
}
