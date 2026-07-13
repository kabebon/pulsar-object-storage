package cache

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// SessionData is the per-user payload stored under each session id. We keep it
// small and self-describing so it can be looked up without a DB hit.
type SessionData struct {
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	PlanSlug  string    `json:"plan_slug"`
	CreatedAt time.Time `json:"created_at"`
}

// SessionStore manages opaque session ids backed by Redis.
type SessionStore struct {
	c   *Client
	ttl time.Duration
}

// NewSessionStore returns a store with the configured session TTL.
func NewSessionStore(c *Client, ttl time.Duration) *SessionStore {
	return &SessionStore{c: c, ttl: ttl}
}

// Create issues a new random session id and stores data under it. The id is
// URL-safe and 32 bytes of entropy (256 bits), base64-encoded.
func (s *SessionStore) Create(ctx context.Context, data SessionData) (string, error) {
	id, err := generateToken(32)
	if err != nil {
		return "", err
	}
	data.CreatedAt = time.Now().UTC()
	if err := s.c.Set(ctx, sessionKey(id), data, s.ttl); err != nil {
		return "", err
	}
	setKey := s.c.key(userSessionsKey(data.UserID))
	_ = s.c.rdb.SAdd(ctx, setKey, id)
	_ = s.c.rdb.Expire(ctx, setKey, s.ttl)
	return id, nil
}

// Get retrieves session data by id. Returns ErrNotFound when missing/expired.
func (s *SessionStore) Get(ctx context.Context, id string) (*SessionData, error) {
	if id == "" {
		return nil, ErrNotFound
	}
	v, err := s.c.rdb.Get(ctx, s.c.key(sessionKey(id))).Result()
	if errors.Is(err, redis.Nil) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var data SessionData
	if err := decodeJSON([]byte(v), &data); err != nil {
		return nil, err
	}
	return &data, nil
}

// Touch extends the session TTL on activity (sliding expiration).
func (s *SessionStore) Touch(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	return s.c.rdb.Expire(ctx, s.c.key(sessionKey(id)), s.ttl).Err()
}

// Destroy deletes a session, effectively logging the user out.
func (s *SessionStore) Destroy(ctx context.Context, id string) error {
	return s.c.Del(ctx, sessionKey(id))
}

// DestroyAllForUser removes all sessions matching a user id. Sessions are
// keyed by id only; we additionally maintain an index set per user.
func (s *SessionStore) DestroyAllForUser(ctx context.Context, userID string) error {
	members, err := s.c.rdb.SMembers(ctx, s.c.key(userSessionsKey(userID))).Result()
	if err != nil {
		return err
	}
	for _, sid := range members {
		_ = s.Destroy(ctx, sid)
	}
	_ = s.c.Del(ctx, userSessionsKey(userID))
	return nil
}

func sessionKey(id string) string      { return "session:" + id }
func userSessionsKey(uid string) string { return "user-sessions:" + uid }

// generateToken returns n random bytes, base64-URL encoded (no padding).
func generateToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
