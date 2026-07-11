// Package service implements business logic for authentication, accounts,
// storage, billing and domain management. It depends on repository (persistence),
// cache (sessions/rate-limit), storage (S3) and mailer abstractions only — no
// HTTP concerns leak in here.
package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// Password hashing policy. bcrypt cost 12 balances security and latency on
// modest hardware. Tokens use 32 random bytes (256-bit entropy).
const (
	bcryptCost     = 12
	tokenEntropy   = 32
)

// HashPassword returns a bcrypt hash of the given plain password.
func HashPassword(plain string) (string, error) {
	if err := validatePassword(plain); err != nil {
		return "", err
	}
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// VerifyPassword compares a bcrypt hash with a plaintext candidate.
func VerifyPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// validatePassword enforces minimum complexity.
func validatePassword(plain string) error {
	if len(plain) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	if len(plain) > 72 {
		// bcrypt truncates silently at 72 bytes; refuse longer inputs so the
		// user always knows exactly what is being protected.
		return errors.New("password must be at most 72 characters")
	}
	return nil
}

// RandomToken returns a URL-safe opaque token (base64-rawURL of N random bytes).
func RandomToken(n int) (string, error) {
	if n <= 0 {
		n = tokenEntropy
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken returns the hex-encoded SHA-256 of an opaque token. Only the hash
// is persisted; the raw token lives only in the verification email link.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	const hex = "0123456789abcdef"
	out := make([]byte, sha256.Size*2)
	for i, b := range sum {
		out[i*2] = hex[b>>4]
		out[i*2+1] = hex[b&0x0f]
	}
	return string(out)
}

// NormalizeEmail lower-cases and trims surrounding whitespace so that
// "Foo@Example.com" and "foo@example.com" are treated identically.
func NormalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return "", errors.New("email is required")
	}
	// Minimal sanity check; deeper validation happens at the handler layer.
	at := strings.IndexByte(email, '@')
	if at <= 0 || at == len(email)-1 || strings.IndexByte(email[at+1:], '.') < 0 {
		return "", errors.New("email format is invalid")
	}
	return email, nil
}
