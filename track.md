# Bookly — Learning Track

Build Bookly week by week. Each week has a **goal**, the **concepts** you'll learn,
a **build checklist**, and a **done when** check you can verify yourself.

How to use this:
- Work top to bottom. Tick boxes as you go (`- [x]`).
- Write the code yourself first. Ask for a review after, not a solution before.
- Every "done when" should be something you can *run*: a curl, a test, a psql query.
- When stuck for more than 30 minutes, write down what you tried, then ask.

---

## Week 0 — Schema ✅

**Goal:** a database that refuses bad data on its own.

**Concepts:** constraints as the last line of defence, `ON DELETE` behaviour,
exclusion constraints, partial indexes, `TIMESTAMPTZ` vs `TIMESTAMP`.

- [x] Write `migrations/001_init.sql`
- [x] `CHECK`, `NOT NULL`, `UNIQUE` on every column that needs them
- [x] Double booking blocked with `EXCLUDE USING gist` (cancelled ones ignored)
- [x] Duplicate messages blocked with `UNIQUE (appointment_id, type)`
- [x] Partial index for the worker's pending-messages query
- [ ] `cp .env.example .env`, `make db`, check tables with `make psql` → `\dt`
- [ ] Commit the migration

**Done when:** inserting an overlapping appointment by hand in psql fails with
an exclusion-constraint error.

**Open question to decide:** rename message type `'follow-up'` to `'review'`?

---

## Week 1 — Store, booking rules, first API

**Goal:** a customer can see free slots and book one over HTTP.

**Concepts:**
- Connection pools (`pgxpool`) and why you pass `*Store` instead of a global
- Transactions: all-or-nothing writes
- Mapping Postgres errors (`23P01` exclusion, `23505` unique) to HTTP status codes
- Time zones: storing UTC, computing in the business's local time
- Go 1.22+ routing (`mux.HandleFunc("GET /x/{slug}", ...)`, `r.PathValue`)
- Table-driven tests

### 1.1 Store
- [ ] `go get github.com/jackc/pgx/v5`
- [ ] `store.New(ctx, url) (*Store, error)` with a `pgxpool.Pool`, plus `Close()`
- [ ] Connect in `cmd/api/main.go`; make `/health` also ping the DB
- [ ] Seed script (`migrations/seed.sql` or a `make seed` target): one business,
      two services, opening hours Mon–Fri

**Done when:** `curl localhost:8080/health` returns OK, and returns an error
when you `docker compose stop postgres`.

### 1.2 Booking: Create
- [ ] Validate input (name, phone, start in the future, service belongs to business)
- [ ] Compute `ends_at` from the service's `duration_min`; never trust the client for it
- [ ] One transaction: insert appointment + its `scheduled_messages`
      (reminder 24h before, review request after `ends_at`)
- [ ] Return a typed error `ErrSlotTaken` on `23P01`

**Done when:** a test proves two overlapping bookings → the second gets `ErrSlotTaken`,
and that a failed booking leaves no orphan `scheduled_messages` rows.

### 1.3 Booking: AvailableSlots
- [ ] Load the business timezone with `time.LoadLocation`
- [ ] Opening hours for that weekday → candidate slots every `duration_min`
- [ ] Remove slots overlapping non-cancelled appointments
- [ ] Remove slots in the past

**Done when:** table-driven tests pass for: closed day, fully booked day,
a booking in the middle, and a DST-change date (e.g. Europe/London, last Sunday of October).

### 1.4 HTTP
- [ ] `GET  /businesses/{slug}/slots?date=YYYY-MM-DD`
- [ ] `POST /businesses/{slug}/appointments` → 201, 400, 404, 409
- [ ] JSON errors in one consistent shape: `{"error": "..."}`
- [ ] Server timeouts + graceful shutdown (pattern from Governor's `main.go`)

**Done when:** you can book a slot with curl, the slot disappears from `/slots`,
and booking it again returns 409.

---

## Week 2 — The worker (the heart of the product)

**Goal:** reminders and review requests go out exactly once, even with crashes
and multiple workers running.

**Concepts:**
- `SELECT ... FOR UPDATE SKIP LOCKED` as a job queue
- At-least-once vs exactly-once delivery, and idempotency
- Retries with backoff, a max-attempts cap, recording `last_error`
- Graceful shutdown mid-batch (`context` cancellation)

### 2.1 sendDue
- [ ] Claim a batch: due + pending, `FOR UPDATE SKIP LOCKED LIMIT n`, inside a transaction
- [ ] Skip & mark `cancelled` if the appointment is cancelled;
      review requests only if the appointment is `done`
- [ ] `sender.Send`, then mark `sent` with `sent_at`
- [ ] On error: `attempts++`, save `last_error`, push `send_at` back (backoff);
      mark `failed` after N attempts
- [ ] Message bodies built from templates (business name, time in *local* timezone)

### 2.2 Booking: Cancel
- [ ] Mark appointment `cancelled` and its pending messages `cancelled` in one transaction
- [ ] `POST /appointments/{id}/cancel` (think: who is allowed to call this?)

### 2.3 Owner actions
- [ ] Mark an appointment `done` or `no_show` (this is what triggers the review request)

**Done when:**
- Running **two** `make worker` processes at once never sends a message twice
  (check `FakeSender` logs).
- A `Sender` that fails twice then succeeds ends with `attempts = 2`, status `sent`.
- Killing the worker with Ctrl-C mid-batch loses nothing.

---

## Week 3 — Owner auth + React frontend

**Goal:** a business owner can log in and see their day; customers can book from a web page.

**Concepts:** password hashing (`bcrypt`), sessions vs JWT, auth middleware,
CORS, React state + data fetching, forms.

### 3.1 Owner auth (Go)
- [ ] `POST /signup`, `POST /login` with `bcrypt` (`golang.org/x/crypto/bcrypt`)
- [ ] Session cookie (`HttpOnly`, `Secure`, `SameSite`) or a signed token, pick one and justify it
- [ ] Middleware that loads the owner's business; every owner query is scoped by `business_id`
- [ ] Owner endpoints: list appointments for a date, mark done / no-show, manage services & hours

### 3.2 Frontend (`web/`)
- [ ] Vite + React + TypeScript
- [ ] Public page `/b/{slug}`: pick service → date → slot → name/phone/opt-in → confirm
- [ ] Owner dashboard: login, today's appointments, buttons for done / no-show / cancel
- [ ] Handle 409 nicely ("someone just took that slot, pick another")

**Done when:** owner A can never see or change owner B's appointments (write a test for it),
and a full booking works end to end in the browser.

---

## Week 4 — Real WhatsApp, Docker, deploy

**Goal:** a real message lands on a real phone, from a deployed app.

**Concepts:** third-party APIs, secrets management, webhooks, containers, deploys.

### 4.1 WhatsApp
- [ ] `TwilioSender` implementing `notify.Sender` (WhatsApp sandbox)
- [ ] Pick the sender from config: fake locally, Twilio when credentials are set
- [ ] Only send to customers with `whatsapp_opt_in = true`
- [ ] Phone numbers normalised to E.164 at booking time

### 4.2 Docker for api + worker
- [ ] Multi-stage `Dockerfile` (build with `golang`, run on a small base image)
- [ ] `api` and `worker` services in `docker-compose.yml`,
      `depends_on: postgres: condition: service_healthy`
- [ ] Container `DATABASE_URL` uses host `postgres`, not `localhost`

### 4.3 Deploy
- [ ] Pick a host (Fly.io, Railway, Render…) with managed Postgres
- [ ] Migrations run as a deploy step, not via `docker-entrypoint-initdb.d`
      (look at `golang-migrate` or `goose`)
- [ ] Logs you can read in production

**Done when:** you book an appointment on the deployed site and get the
WhatsApp reminder on your phone.

---

## Stretch goals

- [ ] Split shifts: allow several opening periods per weekday (schema change + migration `002`)
- [ ] Composite FK so an appointment's service always belongs to the same business
- [ ] Customer confirms via WhatsApp reply → status `confirmed` (Twilio webhook)
- [ ] No-show rate per business on the owner dashboard
- [ ] Rate limiting on the public booking endpoint
- [ ] CI: `go test -race ./...` + migration check on every push (GitHub Actions)

---

## Log

Write one line per session: date, what you built, what you learned, what confused you.

| Date | Built | Learned | Confused by |
|------|-------|---------|-------------|
| 2026-10-03 | Schema `001_init.sql` | Exclusion constraints, partial indexes, `RESTRICT` vs `CASCADE` | |
