-- Pulsar initial schema
-- Sets up the foundational tables for users and email verification tokens.
-- Subsequent migrations add buckets, objects, api_keys, billing, domains.
--
-- NOTE: We avoid the `citext` extension deliberately because it requires
-- superuser privileges that the application DB role may not have on managed
-- PostgreSQL offerings. Case-insensitive email matching is achieved with a
-- LOWER(email) unique index instead.

CREATE EXTENSION IF NOT EXISTS "pgcrypto"; -- provides gen_random_uuid()

-- ----------------------------------------------------------------------------
-- users
-- ----------------------------------------------------------------------------
CREATE TABLE users (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email            TEXT NOT NULL,
    name             TEXT NOT NULL DEFAULT '',
    password_hash    TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'unverified'
                     CHECK (status IN ('active','unverified','suspended')),
    email_verified_at TIMESTAMPTZ,
    last_login_at    TIMESTAMPTZ,
    totp_enabled     BOOLEAN NOT NULL DEFAULT FALSE,
    totp_secret      TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Case-insensitive uniqueness + lookup on email (citext replacement).
CREATE UNIQUE INDEX users_email_lower_uniq ON users (LOWER(email));
CREATE INDEX users_status_idx ON users (status);
CREATE INDEX users_created_at_idx ON users (created_at DESC);

-- ----------------------------------------------------------------------------
-- email_verifications
-- ----------------------------------------------------------------------------
CREATE TABLE email_verifications (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE,
    type         TEXT NOT NULL CHECK (type IN ('signup','reset')),
    expires_at   TIMESTAMPTZ NOT NULL,
    consumed_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX email_verifications_user_idx ON email_verifications (user_id);
CREATE INDEX email_verifications_token_hash_idx ON email_verifications (token_hash);
CREATE INDEX email_verifications_expires_idx ON email_verifications (expires_at);

-- ----------------------------------------------------------------------------
-- audit_log
-- ----------------------------------------------------------------------------
CREATE TABLE audit_log (
    id          BIGSERIAL PRIMARY KEY,
    user_id     UUID REFERENCES users(id) ON DELETE SET NULL,
    action      TEXT NOT NULL,
    ip          INET,
    user_agent  TEXT,
    metadata    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX audit_log_user_idx ON audit_log (user_id, created_at DESC);
CREATE INDEX audit_log_action_idx ON audit_log (action, created_at DESC);

-- ----------------------------------------------------------------------------
-- updated_at trigger helper
-- ----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER users_set_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
