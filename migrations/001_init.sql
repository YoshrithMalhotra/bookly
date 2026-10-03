-- 001_init.sql — YOU write this one. Runs automatically the first time
-- Postgres starts (see docker-compose.yml). Use `make db-reset` to re-run.
--
-- Tables to create:
--   businesses          id, name, slug (unique), timezone, google_review_url,
--                       owner_email (unique), password_hash, created_at
--   services            id, business_id -> businesses, name, duration_min, price
--   opening_hours       business_id -> businesses, weekday, opens_at, closes_at
--   appointments        id, business_id, service_id, customer_name, customer_phone,
--                       starts_at, ends_at (TIMESTAMPTZ), status, whatsapp_opt_in, created_at
--   scheduled_messages  id, appointment_id -> appointments, type, send_at, status,
--                       attempts, last_error, sent_at
--
-- Think about:
--   - NOT NULL and CHECK constraints (duration_min > 0, ends_at > starts_at, valid statuses)
--   - ON DELETE behavior for each foreign key
--   - Double booking: EXCLUDE USING gist (needs CREATE EXTENSION btree_gist)
--   - Sending twice: UNIQUE (appointment_id, type)
--   - Indexes the worker needs: it queries pending messages by send_at

CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE businesses (
    id                SERIAL PRIMARY KEY,
    name              VARCHAR(100) NOT NULL,
    slug              VARCHAR(50) UNIQUE NOT NULL,
    timezone          VARCHAR(50) NOT NULL,
    google_review_url TEXT,
    owner_email       VARCHAR(255) UNIQUE NOT NULL,
    password_hash     TEXT NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE services (
    id           SERIAL PRIMARY KEY,
    business_id  INT NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    name         VARCHAR(100) NOT NULL,
    duration_min INT NOT NULL CHECK (duration_min > 0),
    price        NUMERIC(10, 2) NOT NULL CHECK (price >= 0)
);

CREATE TABLE opening_hours (
    business_id INT NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    weekday     INT NOT NULL CHECK (weekday BETWEEN 0 AND 6),
    opens_at    TIME NOT NULL,
    closes_at   TIME NOT NULL,
    PRIMARY KEY (business_id, weekday),
    CHECK (closes_at > opens_at)
);

CREATE TABLE appointments (
    id              SERIAL PRIMARY KEY,
    business_id     INT NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    -- RESTRICT: deleting a service must not erase booking history.
    service_id      INT NOT NULL REFERENCES services(id) ON DELETE RESTRICT,
    customer_name   VARCHAR(100) NOT NULL,
    customer_phone  VARCHAR(20) NOT NULL,
    starts_at       TIMESTAMPTZ NOT NULL,
    ends_at         TIMESTAMPTZ NOT NULL,
    -- Must match booking.Status in internal/booking/booking.go.
    status          VARCHAR(20) NOT NULL DEFAULT 'booked'
                    CHECK (status IN ('booked', 'confirmed', 'cancelled', 'done', 'no_show')),
    whatsapp_opt_in BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (ends_at > starts_at),
    -- No overlapping appointments per business; cancelled ones free the slot.
    EXCLUDE USING gist (
        business_id WITH =,
        tstzrange(starts_at, ends_at) WITH &&
    ) WHERE (status <> 'cancelled')
);

CREATE TABLE scheduled_messages (
    id             SERIAL PRIMARY KEY,
    appointment_id INT NOT NULL REFERENCES appointments(id) ON DELETE CASCADE,
    type           VARCHAR(20) NOT NULL CHECK (type IN ('reminder', 'follow-up')),
    send_at        TIMESTAMPTZ NOT NULL,
    status         VARCHAR(20) NOT NULL DEFAULT 'pending'
                   CHECK (status IN ('pending', 'sent', 'failed', 'cancelled')),
    attempts       INT NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error     TEXT,
    sent_at        TIMESTAMPTZ,
    UNIQUE (appointment_id, type)
);

-- Worker polls: WHERE status = 'pending' AND send_at <= now() ORDER BY send_at
CREATE INDEX scheduled_messages_pending_send_at_idx
    ON scheduled_messages (send_at)
    WHERE status = 'pending';
