package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/google/uuid"

	"pulsar/internal/models"
	"pulsar/internal/repository"
)

// APIKeyDeps bundles collaborators for the API key service.
type APIKeyDeps struct {
	Keys  *repository.APIKeysRepo
	Audit *repository.AuditLogRepo
}

// APIKeyService issues and resolves long-lived bearer tokens.
type APIKeyService struct {
	APIKeyDeps
}

// NewAPIKeyService wires dependencies.
func NewAPIKeyService(deps APIKeyDeps) *APIKeyService {
	return &APIKeyService{APIKeyDeps: deps}
}

// CreateResult is returned once after creation; Secret is shown only here.
type CreateResult struct {
	ID     uuid.UUID
	Secret string // full key, shown once (e.g. "pk_live_<secret>")
	Prefix string
	Name   string
}

// Create issues a new API key for a user. The secret is returned once and
// never persisted; only its hash is stored.
func (s *APIKeyService) Create(ctx context.Context, userID uuid.UUID, name string, scopes []string) (*CreateResult, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, repository.Wrap(models.ErrValidation, "name is required")
	}
	if len(name) > 64 {
		return nil, repository.Wrap(models.ErrValidation, "name too long")
	}
	raw, err := RandomToken(32)
	if err != nil {
		return nil, err
	}
	secret := "pk_live_" + raw
	prefix := secret[:14] // "pk_live_XXXXXX"
	hash := hashSecret(secret)
	if len(scopes) == 0 {
		scopes = []string{"*"}
	}
	k, err := s.Keys.Create(ctx, userID, name, hash, prefix, scopes)
	if err != nil {
		return nil, err
	}
	return &CreateResult{ID: k.ID, Secret: secret, Prefix: prefix, Name: k.Name}, nil
}

// Resolve looks up a bearer token by hashing it and returns the owning user id, name, and scopes.
// It also touches last_used_at. Returns ErrNotFound on any miss.
func (s *APIKeyService) Resolve(ctx context.Context, secret string) (uuid.UUID, string, []string, error) {
	secret = strings.TrimSpace(secret)
	if !strings.HasPrefix(secret, "pk_live_") {
		return uuid.Nil, "", nil, models.ErrNotFound
	}
	k, err := s.Keys.FindByHash(ctx, hashSecret(secret))
	if err != nil {
		return uuid.Nil, "", nil, err
	}
	// Best-effort touch.
	_ = s.Keys.TouchLastUsed(ctx, k.ID)
	return k.UserID, k.Name, k.Scopes, nil
}

// hashSecret returns the hex-encoded SHA-256 of the full secret.
func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
