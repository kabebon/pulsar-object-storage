// Package httperr provides a unified error model for HTTP responses, following
// RFC 9457 (Problem Details for HTTP APIs). Services return errors tagged with
// an AppError; handlers translate them into consistent JSON responses.
package httperr

import (
	"errors"
	"fmt"
	"net/http"

	"pulsar/internal/models"
)

// AppError carries an HTTP status plus a stable machine-readable code so that
// API consumers can branch on code rather than parsing message text.
type AppError struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Title   string `json:"title"`
	Detail  string `json:"detail,omitempty"`
	Fields  map[string]string `json:"fields,omitempty"`
	cause   error
}

func (e *AppError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %v", e.Code, e.cause)
	}
	return e.Code
}

func (e *AppError) Unwrap() error { return e.cause }

// WithCause attaches an underlying error for logging while keeping the public
// message stable.
func (e *AppError) WithCause(err error) *AppError { e.cause = err; return e }

// New constructs an AppError.
func New(status int, code, title, detail string) *AppError {
	return &AppError{Status: status, Code: code, Title: title, Detail: detail}
}

// Validation builds a 400 with per-field violations.
func Validation(detail string, fields map[string]string) *AppError {
	return &AppError{Status: http.StatusBadRequest, Code: "validation_error", Title: "Validation failed", Detail: detail, Fields: fields}
}

func BadRequest(code, detail string) *AppError {
	return New(http.StatusBadRequest, code, "Bad request", detail)
}

func Unauthorized(detail string) *AppError {
	if detail == "" {
		detail = "Authentication required"
	}
	return New(http.StatusUnauthorized, "unauthorized", "Unauthorized", detail)
}

func Forbidden(detail string) *AppError {
	if detail == "" {
		detail = "You do not have access to this resource"
	}
	return New(http.StatusForbidden, "forbidden", "Forbidden", detail)
}

func NotFound(detail string) *AppError {
	if detail == "" {
		detail = "Resource not found"
	}
	return New(http.StatusNotFound, "not_found", "Not found", detail)
}

func Conflict(code, detail string) *AppError {
	return New(http.StatusConflict, code, "Conflict", detail)
}

func RateLimited(detail string) *AppError {
	if detail == "" {
		detail = "Too many requests, please slow down"
	}
	return New(http.StatusTooManyRequests, "rate_limited", "Rate limited", detail)
}

func PaymentRequired(detail string) *AppError {
	if detail == "" {
		detail = "This action requires an active subscription"
	}
	return New(http.StatusPaymentRequired, "payment_required", "Payment required", detail)
}

func Internal(err error) *AppError {
	return New(http.StatusInternalServerError, "internal_error", "Internal server error", "").
		WithCause(err)
}

// From maps a domain/model error to an AppError. Unknown errors become 500.
func From(err error) *AppError {
	if err == nil {
		return nil
	}
	var ae *AppError
	if errors.As(err, &ae) {
		return ae
	}
	switch {
	case errors.Is(err, models.ErrNotFound):
		return NotFound("")
	case errors.Is(err, models.ErrUnauthorized), errors.Is(err, models.ErrInvalidCredentials):
		return Unauthorized("")
	case errors.Is(err, models.ErrForbidden):
		return Forbidden("")
	case errors.Is(err, models.ErrConflict):
		return Conflict("conflict", "")
	case errors.Is(err, models.ErrValidation):
		return Validation(err.Error(), nil)
	case errors.Is(err, models.ErrQuotaExceeded):
		return New(http.StatusTooManyRequests, "quota_exceeded", "Quota exceeded", err.Error())
	case errors.Is(err, models.ErrRateLimited):
		return RateLimited("")
	case errors.Is(err, models.ErrUnverifiedEmail):
		return New(http.StatusForbidden, "email_not_verified", "Email not verified", "Please verify your email address")
	case errors.Is(err, models.ErrTokenExpired):
		return BadRequest("token_expired", "The link has expired")
	case errors.Is(err, models.ErrTokenConsumed):
		return BadRequest("token_consumed", "The link has already been used")
	case errors.Is(err, models.ErrPaymentRequired):
		return PaymentRequired("")
	}
	return Internal(err)
}
