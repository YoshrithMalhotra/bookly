-- 003_billing_and_accounts.sql — Stripe subscriptions and password resets.

-- 'trial' is our own free trial before the owner subscribes; every other
-- value mirrors the Stripe subscription status.
ALTER TABLE businesses
    ADD COLUMN subscription_status    TEXT NOT NULL DEFAULT 'trial',
    ADD COLUMN trial_ends_at          TIMESTAMPTZ NOT NULL DEFAULT now() + interval '14 days',
    ADD COLUMN stripe_customer_id     TEXT UNIQUE,
    ADD COLUMN stripe_subscription_id TEXT UNIQUE,
    ADD COLUMN current_period_end     TIMESTAMPTZ;

CREATE TABLE password_resets (
    token_hash  BYTEA PRIMARY KEY,
    business_id INT NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ
);
CREATE INDEX password_resets_business_idx ON password_resets (business_id);
