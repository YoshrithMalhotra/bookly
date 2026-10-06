-- 002_production.sql — everything the app needs on top of 001 to go live.

-- Decided: the post-visit message is called 'review', not 'follow-up'.
ALTER TABLE scheduled_messages DROP CONSTRAINT scheduled_messages_type_check;
UPDATE scheduled_messages SET type = 'review' WHERE type = 'follow-up';
ALTER TABLE scheduled_messages
    ADD CONSTRAINT scheduled_messages_type_check CHECK (type IN ('reminder', 'review'));

-- Services are retired, never deleted, so booking history keeps its service.
ALTER TABLE services ADD COLUMN active BOOLEAN NOT NULL DEFAULT TRUE;

-- Composite FK: an appointment's service must belong to the same business.
ALTER TABLE services ADD CONSTRAINT services_id_business_id_key UNIQUE (id, business_id);
ALTER TABLE appointments DROP CONSTRAINT appointments_service_id_fkey;
ALTER TABLE appointments
    ADD CONSTRAINT appointments_service_business_fkey
    FOREIGN KEY (service_id, business_id) REFERENCES services (id, business_id) ON DELETE RESTRICT;

-- Customers manage their booking with an unguessable token (no account needed).
ALTER TABLE appointments ADD COLUMN cancel_token TEXT UNIQUE;

-- Owner dashboard: appointments for a business on a given day.
CREATE INDEX appointments_business_starts_at_idx ON appointments (business_id, starts_at);

-- Owner login sessions. Only a SHA-256 of the cookie value is stored.
CREATE TABLE sessions (
    token_hash  BYTEA PRIMARY KEY,
    business_id INT NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);
