package repository

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"pulsar/internal/models"
)

// AuditLogRepo persists append-only security events.
type AuditLogRepo struct {
	db *DB
}

func NewAuditLogRepo(db *DB) *AuditLogRepo { return &AuditLogRepo{db: db} }

// Record writes a single audit entry. userID may be nil for pre-auth events.
func (r *AuditLogRepo) Record(ctx context.Context, userID *uuid.UUID, action models.AuditAction, ip, userAgent string, meta url.Values) error {
	metaJSON := "{}"
	if meta != nil && len(meta) > 0 {
		if b, err := json.Marshal(meta); err == nil {
			metaJSON = string(b)
		}
	}
	const q = `
        INSERT INTO audit_log (user_id, action, ip, user_agent, metadata)
        VALUES ($1, $2, NULLIF($3,'')::inet, $4, $5::jsonb)`
	_, err := r.db.Pool.Exec(ctx, q, uuidToText(userID), string(action), ip, userAgent, metaJSON)
	return err
}

// List returns audit entries for a user, most recent first, paginated.
func (r *AuditLogRepo) List(ctx context.Context, userID uuid.UUID, limit, offset int) ([]models.AuditLogEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	const q = `
        SELECT id, user_id, action, host(ip) AS ip, user_agent, metadata, created_at
        FROM audit_log WHERE user_id = $1
        ORDER BY created_at DESC
        LIMIT $2 OFFSET $3`
	rows, err := r.db.Pool.Query(ctx, q, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.AuditLogEntry
	for rows.Next() {
		var e models.AuditLogEntry
		var uid pgtype.UUID
		var meta []byte
		if err := rows.Scan(&e.ID, &uid, &e.Action, &e.IP, &e.UserAgent, &meta, &e.CreatedAt); err != nil {
			return nil, err
		}
		if uid.Valid {
			u, _ := uuid.FromBytes(uid.Bytes[:])
			uidCopy := u
			e.UserID = &uidCopy
		}
		if len(meta) > 0 {
			e.Metadata = decodeMetadata(meta)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Count returns total audit entries for a user (for pagination metadata).
func (r *AuditLogRepo) Count(ctx context.Context, userID uuid.UUID) (int64, error) {
	const q = `SELECT count(*) FROM audit_log WHERE user_id = $1`
	var n int64
	err := r.db.Pool.QueryRow(ctx, q, userID).Scan(&n)
	return n, err
}

func uuidToText(u *uuid.UUID) any {
	if u == nil {
		return nil
	}
	return *u
}

// decodeMetadata parses JSON back into url.Values.
func decodeMetadata(b []byte) url.Values {
	v := url.Values{}
	if len(b) > 0 {
		_ = json.Unmarshal(b, &v)
	}
	return v
}
