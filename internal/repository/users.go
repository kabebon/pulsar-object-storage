package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"pulsar/internal/models"
)

// UsersRepo exposes user-account persistence.
type UsersRepo struct {
	db *DB
}

func NewUsersRepo(db *DB) *UsersRepo { return &UsersRepo{db: db} }

// Create inserts a new user. Returns models.ErrConflict (via IsUniqueViolation)
// when the email is already taken.
func (r *UsersRepo) Create(ctx context.Context, email, name, passwordHash string) (*models.User, error) {
	const q = `
        INSERT INTO users (email, name, password_hash, status)
        VALUES ($1, $2, $3, 'unverified')
        RETURNING id, email, name, password_hash, status, email_verified_at,
                  last_login_at, totp_enabled, created_at, updated_at`
	var u models.User
	err := r.db.Pool.QueryRow(ctx, q, email, name, passwordHash).Scan(
		&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Status,
		&u.EmailVerifiedAt, &u.LastLoginAt, &u.TOTPEnabled, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if IsUniqueViolation(err) {
			return nil, Wrap(models.ErrConflict, "email already registered")
		}
		return nil, err
	}
	return &u, nil
}

// FindByEmail looks up a user by (case-insensitive) email.
func (r *UsersRepo) FindByEmail(ctx context.Context, email string) (*models.User, error) {
	const q = `
        SELECT id, email, name, password_hash, status, email_verified_at,
               last_login_at, totp_enabled, created_at, updated_at
        FROM users WHERE email = $1`
	var u models.User
	err := r.db.Pool.QueryRow(ctx, q, email).Scan(
		&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Status,
		&u.EmailVerifiedAt, &u.LastLoginAt, &u.TOTPEnabled, &u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// FindByID looks up a user by primary key.
func (r *UsersRepo) FindByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	const q = `
        SELECT id, email, name, password_hash, status, email_verified_at,
               last_login_at, totp_enabled, created_at, updated_at
        FROM users WHERE id = $1`
	var u models.User
	err := r.db.Pool.QueryRow(ctx, q, id).Scan(
		&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.Status,
		&u.EmailVerifiedAt, &u.LastLoginAt, &u.TOTPEnabled, &u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// MarkEmailVerified sets email_verified_at and flips status to active.
func (r *UsersRepo) MarkEmailVerified(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE users SET email_verified_at = now(), status = 'active' WHERE id = $1`
	tag, err := r.db.Pool.Exec(ctx, q, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return models.ErrNotFound
	}
	return nil
}

// UpdatePassword replaces the password hash.
func (r *UsersRepo) UpdatePassword(ctx context.Context, id uuid.UUID, passwordHash string) error {
	const q = `UPDATE users SET password_hash = $1 WHERE id = $2`
	tag, err := r.db.Pool.Exec(ctx, q, passwordHash, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return models.ErrNotFound
	}
	return nil
}

// UpdateProfile updates mutable profile fields.
func (r *UsersRepo) UpdateProfile(ctx context.Context, id uuid.UUID, name string) error {
	const q = `UPDATE users SET name = $1 WHERE id = $2`
	tag, err := r.db.Pool.Exec(ctx, q, name, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return models.ErrNotFound
	}
	return nil
}

// TouchLastLogin sets last_login_at to now.
func (r *UsersRepo) TouchLastLogin(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE users SET last_login_at = now() WHERE id = $1`
	_, err := r.db.Pool.Exec(ctx, q, id)
	return err
}

// _ = errors ensures the import stays referenced for future checks.
var _ = errors.Is
