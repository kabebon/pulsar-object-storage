-- Storage: buckets, objects, api_keys, usage_events, plans, subscriptions, custom_domains.
-- Adds the core data model for the object storage product.

-- ----------------------------------------------------------------------------
-- buckets
-- ----------------------------------------------------------------------------
CREATE TABLE buckets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    region      TEXT NOT NULL DEFAULT 'us-east-1',
    visibility  TEXT NOT NULL DEFAULT 'private'
                CHECK (visibility IN ('private','public')),
    cdn_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, name)
);

CREATE INDEX buckets_user_idx ON buckets (user_id, created_at DESC);

CREATE TRIGGER buckets_set_updated_at
    BEFORE UPDATE ON buckets
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ----------------------------------------------------------------------------
-- objects
-- ----------------------------------------------------------------------------
CREATE TABLE objects (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bucket_id     UUID NOT NULL REFERENCES buckets(id) ON DELETE CASCADE,
    key           TEXT NOT NULL,
    size          BIGINT NOT NULL DEFAULT 0,
    content_type  TEXT NOT NULL DEFAULT 'application/octet-stream',
    etag          TEXT,
    sha256        TEXT,
    version       INT NOT NULL DEFAULT 1,
    storage_class TEXT NOT NULL DEFAULT 'STANDARD',
    uploaded_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (bucket_id, key)
);

CREATE INDEX objects_bucket_idx ON objects (bucket_id, key);
CREATE INDEX objects_uploaded_idx ON objects (uploaded_at DESC);

CREATE TRIGGER objects_set_updated_at
    BEFORE UPDATE ON objects
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ----------------------------------------------------------------------------
-- api_keys
-- ----------------------------------------------------------------------------
CREATE TABLE api_keys (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    key_prefix   TEXT NOT NULL,
    key_hash     TEXT NOT NULL UNIQUE,
    scopes       TEXT[] NOT NULL DEFAULT '{}',
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX api_keys_user_idx ON api_keys (user_id, created_at DESC);
CREATE INDEX api_keys_key_hash_idx ON api_keys (key_hash);

-- ----------------------------------------------------------------------------
-- plans
-- ----------------------------------------------------------------------------
CREATE TABLE plans (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug                     TEXT UNIQUE NOT NULL,
    name                     TEXT NOT NULL,
    storage_gb               BIGINT NOT NULL DEFAULT 0,
    bandwidth_gb_month       BIGINT NOT NULL DEFAULT 0,
    price_monthly_cents      BIGINT NOT NULL DEFAULT 0,
    price_yearly_cents       BIGINT NOT NULL DEFAULT 0,
    max_buckets              INT NOT NULL DEFAULT 1,
    custom_domains_allowed   INT NOT NULL DEFAULT 0,
    sort_order               INT NOT NULL DEFAULT 0,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ----------------------------------------------------------------------------
-- subscriptions
-- ----------------------------------------------------------------------------
CREATE TABLE subscriptions (
    user_id                 UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    plan_id                 UUID NOT NULL REFERENCES plans(id),
    status                  TEXT NOT NULL DEFAULT 'active'
                            CHECK (status IN ('trialing','active','past_due','canceled','incomplete')),
    stripe_customer_id      TEXT,
    stripe_subscription_id  TEXT,
    current_period_end      TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX subscriptions_stripe_customer_idx ON subscriptions (stripe_customer_id);

CREATE TRIGGER subscriptions_set_updated_at
    BEFORE UPDATE ON subscriptions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ----------------------------------------------------------------------------
-- usage_events (append-only counters feeding billing & quota)
-- ----------------------------------------------------------------------------
CREATE TABLE usage_events (
    id          BIGSERIAL PRIMARY KEY,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type        TEXT NOT NULL CHECK (type IN ('storage_bytes','bandwidth_bytes','api_calls')),
    amount      BIGINT NOT NULL,
    bucket_id   UUID REFERENCES buckets(id) ON DELETE SET NULL,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX usage_events_user_idx ON usage_events (user_id, type, recorded_at DESC);
CREATE INDEX usage_events_bucket_idx ON usage_events (bucket_id);

-- Materialized current-storage view (sum of object sizes per user).
CREATE MATERIALIZED VIEW storage_usage AS
SELECT o.id AS object_id, b.user_id, o.bucket_id, o.size, o.uploaded_at
FROM objects o JOIN buckets b ON b.id = o.bucket_id;
CREATE UNIQUE INDEX storage_usage_object_idx ON storage_usage (object_id);

-- ----------------------------------------------------------------------------
-- custom_domains
-- ----------------------------------------------------------------------------
CREATE TABLE custom_domains (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bucket_id  UUID NOT NULL REFERENCES buckets(id) ON DELETE CASCADE,
    domain     TEXT NOT NULL,
    dns_status TEXT NOT NULL DEFAULT 'pending'
               CHECK (dns_status IN ('pending','verified','failed')),
    ssl_status TEXT NOT NULL DEFAULT 'pending'
               CHECK (ssl_status IN ('pending','issued','failed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (domain)
);

CREATE INDEX custom_domains_user_idx ON custom_domains (user_id);

CREATE TRIGGER custom_domains_set_updated_at
    BEFORE UPDATE ON custom_domains
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ----------------------------------------------------------------------------
-- Seed default plans
-- ----------------------------------------------------------------------------
INSERT INTO plans (slug, name, storage_gb, bandwidth_gb_month, price_monthly_cents, price_yearly_cents, max_buckets, custom_domains_allowed, sort_order) VALUES
    ('free',     'Free',     5,    50,    0,      0,      3,  0, 1),
    ('pro',      'Pro',      100,  1000,  9900,   99000,  50, 5, 2),
    ('business', 'Business', 1024, 10240, 49900,  499000, 0,  0, 3)
ON CONFLICT (slug) DO NOTHING;

-- Give every newly registered user a free subscription by default.
CREATE OR REPLACE FUNCTION assign_default_plan() RETURNS TRIGGER AS $$
DECLARE
    free_plan_id UUID;
BEGIN
    SELECT id INTO free_plan_id FROM plans WHERE slug = 'free' LIMIT 1;
    IF free_plan_id IS NOT NULL THEN
        INSERT INTO subscriptions (user_id, plan_id, status)
        VALUES (NEW.id, free_plan_id, 'active')
        ON CONFLICT (user_id) DO NOTHING;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER users_assign_default_plan
    AFTER INSERT ON users
    FOR EACH ROW EXECUTE FUNCTION assign_default_plan();
