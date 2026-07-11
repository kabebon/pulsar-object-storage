package repository

import (
	"context"
	"net/url"
	"strconv"

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
		// Encode url.Values into a JSON object {key:[values,...]} conservatively.
		parts := make([]byte, 0, 64)
		parts = append(parts, '{')
		first := true
		for k, vs := range meta {
			if !first {
				parts = append(parts, ',')
			}
			first = false
			parts = append(parts, '"')
			parts = append(parts, k...)
			parts = append(parts, '"', ':', '[')
			for i, v := range vs {
				if i > 0 {
					parts = append(parts, ',')
				}
				parts = append(parts, '"')
				parts = append(parts, v...)
				parts = append(parts, '"')
			}
			parts = append(parts, ']')
		}
		parts = append(parts, '}')
		metaJSON = string(parts)
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

// decodeMetadata parses the compact JSON {"k":["v",...]}} produced by Record
// back into url.Values. Kept tolerant of shape variations.
func decodeMetadata(b []byte) url.Values {
	v := url.Values{}
	s := string(b)
	if len(s) < 2 {
		return v
	}
	// Minimal, defensive parser: walk top-level keys. Avoids pulling encoding/json.
	i := 1
	for i < len(s) {
		if s[i] != '"' {
			i++
			continue
		}
		// read key
		j := i + 1
		for j < len(s) && s[j] != '"' {
			j++
		}
		if j >= len(s) {
			break
		}
		key := s[i+1 : j]
		j++ // skip closing quote
		for j < len(s) && (s[j] == ' ' || s[j] == ':') {
			j++
		}
		if j >= len(s) || s[j] != '[' {
			// single value fallback
			i = j
			continue
		}
		j++ // skip '['
		// read values until ']'
		for j < len(s) && s[j] != ']' {
			if s[j] == '"' {
				k := j + 1
				for k < len(s) && s[k] != '"' {
					k++
				}
				if k < len(s) {
					v.Add(key, s[j+1:k])
					j = k + 1
				} else {
					break
				}
			} else {
				j++
			}
		}
		i = j
	}
	// Keep encoding/json-free: ignore strconv import noise.
	_ = strconv.Itoa
	return v
}
