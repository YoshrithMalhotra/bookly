-- 004_operations.sql — day-to-day running: time off, owner-made bookings,
-- notes, currency and email verification.

-- Holidays, breaks, days off. Online booking skips these periods.
CREATE TABLE time_off (
    id          SERIAL PRIMARY KEY,
    business_id INT NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    starts_at   TIMESTAMPTZ NOT NULL,
    ends_at     TIMESTAMPTZ NOT NULL,
    reason      VARCHAR(200) NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (ends_at > starts_at)
);
CREATE INDEX time_off_business_idx ON time_off (business_id, starts_at);

-- 'online' = booked by the customer, 'owner' = added from the dashboard
-- (phone bookings, walk-ins).
ALTER TABLE appointments
    ADD COLUMN source TEXT NOT NULL DEFAULT 'online' CHECK (source IN ('online', 'owner')),
    ADD COLUMN notes  VARCHAR(500) NOT NULL DEFAULT '';

-- ISO 4217 code used to display prices.
ALTER TABLE businesses
    ADD COLUMN currency          CHAR(3) NOT NULL DEFAULT 'GBP' CHECK (currency ~ '^[A-Z]{3}$'),
    ADD COLUMN email_verified_at TIMESTAMPTZ;
-- Accounts that existed before verification was added count as verified.
UPDATE businesses SET email_verified_at = created_at;

CREATE TABLE email_verifications (
    token_hash  BYTEA PRIMARY KEY,
    business_id INT NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    email       VARCHAR(255) NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX email_verifications_business_idx ON email_verifications (business_id);
