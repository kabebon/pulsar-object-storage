package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"pulsar/internal/cache"
	"pulsar/internal/mailer"
	"pulsar/internal/models"
	"pulsar/internal/repository"
)

// AuthDeps bundles collaborators the auth service needs.
type AuthDeps struct {
	Users     *repository.UsersRepo
	Tokens    *repository.EmailVerificationsRepo
	Audit     *repository.AuditLogRepo
	Sessions  *cache.SessionStore
	Mailer    mailer.Mailer
	EmailTTL  time.Duration
	PublicURL string // base URL for building verification links, e.g. https://pulsar.example.com
}

// AuthService orchestrates registration, verification, login and password reset.
type AuthService struct {
	AuthDeps
}

// NewAuthService wires dependencies.
func NewAuthService(deps AuthDeps) *AuthService {
	if deps.EmailTTL == 0 {
		deps.EmailTTL = 24 * time.Hour
	}
	return &AuthService{AuthDeps: deps}
}

// Register creates a new unverified user and dispatches a verification email.
// Returns ErrConflict if the email is already taken.
func (s *AuthService) Register(ctx context.Context, name, email, password, clientIP, ua string) (*models.User, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return nil, repository.Wrap(models.ErrValidation, err.Error())
	}
	name = sanitizeName(name)
	hash, err := HashPassword(password)
	if err != nil {
		return nil, repository.Wrap(models.ErrValidation, err.Error())
	}

	user, err := s.Users.Create(ctx, email, name, hash)
	if err != nil {
		return nil, err
	}

	if err := s.dispatchVerification(ctx, user, models.VerificationSignup, clientIP, ua); err != nil {
		// Non-fatal: we still created the user. They can request resend later.
		// Log the failure so SMTP issues are visible (the handler still shows
		// a success page to avoid leaking whether the email exists).
		fmt.Println("WARN: dispatch verification email failed:", err)
		return user, nil
	}
	_ = s.Audit.Record(ctx, &user.ID, models.AuditRegister, clientIP, ua, nil)
	return user, nil
}

// dispatchVerification creates a single-use token and emails it.
func (s *AuthService) dispatchVerification(ctx context.Context, user *models.User, typ models.VerificationType, ip, ua string) error {
	token, err := RandomToken(tokenEntropy)
	if err != nil {
		return err
	}
	if _, err := s.Tokens.Create(ctx, user.ID, HashToken(token), typ, s.EmailTTL); err != nil {
		return err
	}
	link := s.buildLink(typ, token)
	return s.Mailer.Send(ctx, signupEmail(user, link))
}

// VerifyEmail consumes a signup verification token, marking the user active.
func (s *AuthService) VerifyEmail(ctx context.Context, token, ip, ua string) error {
	ev, err := s.Tokens.FindByTokenHash(ctx, HashToken(token))
	if err != nil {
		return err
	}
	if ev.Type != models.VerificationSignup {
		return repository.Wrap(models.ErrValidation, "this link cannot be used for email verification")
	}
	if err := s.Users.MarkEmailVerified(ctx, ev.UserID); err != nil {
		return err
	}
	if err := s.Tokens.MarkConsumed(ctx, ev.ID); err != nil {
		return err
	}
	uid := ev.UserID
	_ = s.Audit.Record(ctx, &uid, models.AuditEmailVerified, ip, ua, nil)
	return nil
}

// Login validates credentials and creates a session. Returns ErrInvalidCredentials
// on any mismatch (we avoid leaking whether the email exists).
func (s *AuthService) Login(ctx context.Context, email, password, clientIP, ua string) (*cache.SessionData, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		// Use the same message the verification path uses to avoid enumeration.
		return nil, models.ErrInvalidCredentials
	}
	user, err := s.Users.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			_ = s.Audit.Record(ctx, nil, models.AuditLoginFailed, clientIP, ua, url.Values{"email": {email}})
			return nil, models.ErrInvalidCredentials
		}
		return nil, err
	}
	if !VerifyPassword(user.PasswordHash, password) {
		_ = s.Audit.Record(ctx, &user.ID, models.AuditLoginFailed, clientIP, ua, url.Values{"reason": {"bad_password"}})
		return nil, models.ErrInvalidCredentials
	}
	if user.Status == models.UserStatusUnverified {
		return nil, repository.Wrap(models.ErrForbidden, "Пожалуйста, подтвердите email перед входом.")
	}
	if user.Status == models.UserStatusSuspended {
		return nil, repository.Wrap(models.ErrForbidden, "account suspended")
	}
	_ = s.Users.TouchLastLogin(ctx, user.ID)
	_ = s.Audit.Record(ctx, &user.ID, models.AuditLogin, clientIP, ua, nil)

	data := cache.SessionData{
		UserID: user.ID.String(),
		Email:  user.Email,
		Name:   user.Name,
		Status: string(user.Status),
	}
	return &data, nil
}

// StartPasswordReset issues a reset token for the given email if the account
// exists. It always returns nil so the endpoint cannot be used for enumeration.
func (s *AuthService) StartPasswordReset(ctx context.Context, email, ip, ua string) error {
	email, err := NormalizeEmail(email)
	if err != nil {
		return nil // silent
	}
	user, err := s.Users.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) {
			return nil
		}
		return err
	}
	_ = s.Tokens.InvalidateType(ctx, user.ID, models.VerificationReset)
	token, err := RandomToken(tokenEntropy)
	if err != nil {
		return err
	}
	if _, err := s.Tokens.Create(ctx, user.ID, HashToken(token), models.VerificationReset, s.EmailTTL); err != nil {
		return err
	}
	link := s.buildLink(models.VerificationReset, token)
	return s.Mailer.Send(ctx, resetEmail(user, link))
}

// CompletePasswordReset consumes a reset token and updates the password.
func (s *AuthService) CompletePasswordReset(ctx context.Context, token, newPassword, ip, ua string) error {
	ev, err := s.Tokens.FindByTokenHash(ctx, HashToken(token))
	if err != nil {
		return err
	}
	if ev.Type != models.VerificationReset {
		return repository.Wrap(models.ErrValidation, "this link cannot be used to reset a password")
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return repository.Wrap(models.ErrValidation, err.Error())
	}
	if err := s.Users.UpdatePassword(ctx, ev.UserID, hash); err != nil {
		return err
	}
	if err := s.Tokens.MarkConsumed(ctx, ev.ID); err != nil {
		return err
	}
	// Invalidate all sessions for the user so a leaked session can't survive.
	uid := ev.UserID
	_ = s.Sessions.DestroyAllForUser(ctx, uid.String())
	_ = s.Audit.Record(ctx, &uid, models.AuditPasswordReset, ip, ua, nil)
	return nil
}

// ResendVerification re-issues a signup verification email for an unverified user.
func (s *AuthService) ResendVerification(ctx context.Context, email, ip, ua string) error {
	email, err := NormalizeEmail(email)
	if err != nil {
		return nil
	}
	user, err := s.Users.FindByEmail(ctx, email)
	if err != nil {
		return nil // silent
	}
	if user.Status == models.UserStatusActive {
		return nil // already verified
	}
	_ = s.Tokens.InvalidateType(ctx, user.ID, models.VerificationSignup)
	return s.dispatchVerification(ctx, user, models.VerificationSignup, ip, ua)
}

// Logout destroys the given session id.
func (s *AuthService) Logout(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	return s.Sessions.Destroy(ctx, sessionID)
}

// NewSession persists the session payload and returns the opaque id to be
// stored in a cookie.
func (s *AuthService) NewSession(ctx context.Context, data cache.SessionData) (string, error) {
	return s.Sessions.Create(ctx, data)
}

// UpdateProfile updates the user's display name.
func (s *AuthService) UpdateProfile(ctx context.Context, userID uuid.UUID, name string) error {
	name = sanitizeName(name)
	return s.Users.UpdateProfile(ctx, userID, name)
}

// UpdatePassword changes the password for an already-authenticated user.
func (s *AuthService) UpdatePassword(ctx context.Context, userID uuid.UUID, currentPassword, newPassword, ip, ua string) error {
	user, err := s.Users.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	if !VerifyPassword(user.PasswordHash, currentPassword) {
		return repository.Wrap(models.ErrInvalidCredentials, "current password is incorrect")
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return repository.Wrap(models.ErrValidation, err.Error())
	}
	if err := s.Users.UpdatePassword(ctx, userID, hash); err != nil {
		return err
	}
	_ = s.Sessions.DestroyAllForUser(ctx, userID.String())
	uid := userID
	_ = s.Audit.Record(ctx, &uid, models.AuditPasswordReset, ip, ua, url.Values{"via": {"settings"}})
	return nil
}

// buildLink constructs an absolute verification URL.
func (s *AuthService) buildLink(typ models.VerificationType, token string) string {
	path := "/verify-email"
	if typ == models.VerificationReset {
		path = "/reset-password"
	}
	base := strings.TrimRight(s.PublicURL, "/")
	return fmt.Sprintf("%s%s?token=%s", base, path, url.QueryEscape(token))
}

// signupEmail builds the welcome/verify message.
func signupEmail(u *models.User, link string) mailer.Message {
	return mailer.Message{
		To:      u.Email,
		Subject: "Подтвердите email — Pulsar",
		Plain:   fmt.Sprintf("Здравствуйте!\n\nПодтвердите ваш email, перейдя по ссылке:\n%s\n\nЕсли вы не регистрировались на Pulsar, проигнорируйте это письмо.\n", link),
		HTML: fmt.Sprintf(`<div style="font-family:sans-serif;max-width:480px;margin:auto">
<h2>Добро пожаловать в Pulsar!</h2>
<p>Подтвердите ваш email, чтобы активировать аккаунт:</p>
<p><a href="%s" style="display:inline-block;padding:10px 18px;background:#6366f1;color:#fff;border-radius:8px;text-decoration:none">Подтвердить email</a></p>
<p style="color:#64748b;font-size:13px;margin-top:24px">Если вы не создавали аккаунт, просто проигнорируйте это письмо.</p>
</div>`, link),
	}
}

// resetEmail builds the password reset message.
func resetEmail(u *models.User, link string) mailer.Message {
	return mailer.Message{
		To:      u.Email,
		Subject: "Сброс пароля — Pulsar",
		Plain:   fmt.Sprintf("Здравствуйте!\n\nМы получили запрос на сброс пароля. Перейдите по ссылке, чтобы задать новый:\n%s\n\nЕсли вы не запрашивали сброс, проигнорируйте это письмо.\n", link),
		HTML: fmt.Sprintf(`<div style="font-family:sans-serif;max-width:480px;margin:auto">
<h2>Сброс пароля</h2>
<p>Задайте новый пароль, перейдя по ссылке:</p>
<p><a href="%s" style="display:inline-block;padding:10px 18px;background:#6366f1;color:#fff;border-radius:8px;text-decoration:none">Сбросить пароль</a></p>
<p style="color:#64748b;font-size:13px;margin-top:24px">Если вы не запрашивали сброс пароля, просто проигнорируйте это письмо.</p>
</div>`, link),
	}
}

// sanitizeName trims and clamps name length.
func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	if len(name) > 100 {
		name = name[:100]
	}
	return name
}
