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
- [x] `cp .env.example .env`, `make db`, check tables with `make psql` → `\dt`
- [x] Commit the migration

**Done when:** inserting an overlapping appointment by hand in psql fails with
an exclusion-constraint error.

**Decided:** message type `'follow-up'` renamed to `'review'` (migration 002).

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
- [x] `go get github.com/jackc/pgx/v5`
- [x] `store.New(ctx, url) (*Store, error)` with a `pgxpool.Pool`, plus `Close()`
- [x] Connect in `cmd/api/main.go`; make `/health` also ping the DB
- [x] Seed script (`migrations/seed.sql` or a `make seed` target): one business,
      two services, opening hours Mon–Fri

**Done when:** `curl localhost:8080/health` returns OK, and returns an error
when you `docker compose stop postgres`.

### 1.2 Booking: Create
- [x] Validate input (name, phone, start in the future, service belongs to business)
- [x] Compute `ends_at` from the service's `duration_min`; never trust the client for it
- [x] One transaction: insert appointment + its `scheduled_messages`
      (reminder 24h before, review request after `ends_at`)
- [x] Return a typed error `ErrSlotTaken` on `23P01`

**Done when:** a test proves two overlapping bookings → the second gets `ErrSlotTaken`,
and that a failed booking leaves no orphan `scheduled_messages` rows.

### 1.3 Booking: AvailableSlots
- [x] Load the business timezone with `time.LoadLocation`
- [x] Opening hours for that weekday → candidate slots every `duration_min`
- [x] Remove slots overlapping non-cancelled appointments
- [x] Remove slots in the past

**Done when:** table-driven tests pass for: closed day, fully booked day,
a booking in the middle, and a DST-change date (e.g. Europe/London, last Sunday of October).

### 1.4 HTTP
- [x] `GET  /businesses/{slug}/slots?date=YYYY-MM-DD`
- [x] `POST /businesses/{slug}/appointments` → 201, 400, 404, 409
- [x] JSON errors in one consistent shape: `{"error": "..."}`
- [x] Server timeouts + graceful shutdown (pattern from Governor's `main.go`)

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
- [x] Claim a batch: due + pending, `FOR UPDATE SKIP LOCKED LIMIT n`, inside a transaction
- [x] Skip & mark `cancelled` if the appointment is cancelled;
      review requests only if the appointment is `done`
- [x] `sender.Send`, then mark `sent` with `sent_at`
- [x] On error: `attempts++`, save `last_error`, push `send_at` back (backoff);
      mark `failed` after N attempts
- [x] Message bodies built from templates (business name, time in *local* timezone)

### 2.2 Booking: Cancel
- [x] Mark appointment `cancelled` and its pending messages `cancelled` in one transaction
- [x] `POST /appointments/{id}/cancel` (think: who is allowed to call this?)

### 2.3 Owner actions
- [x] Mark an appointment `done` or `no_show` (this is what triggers the review request)

**Done when:**
- Running **two** `make worker` processes at once never sends a message twice
  (check `FakeSender` logs).
- A `Sender` that fails twice then succeeds ends with `attempts = 2`, status `sent`.
- Killing the worker with Ctrl-C mid-batch loses nothing.

---

## Week 3 — Owner auth + Kotlin/JS frontend

**Goal:** a business owner can log in and see their day; customers can book from a web page.

**Concepts:** password hashing (`bcrypt`), sessions vs JWT, auth middleware,
CORS, React state + data fetching, forms.

### 3.1 Owner auth (Go)
- [x] `POST /signup`, `POST /login` with `bcrypt` (`golang.org/x/crypto/bcrypt`)
- [x] Session cookie (`HttpOnly`, `Secure`, `SameSite`) or a signed token, pick one and justify it
- [x] Middleware that loads the owner's business; every owner query is scoped by `business_id`
- [x] Owner endpoints: list appointments for a date, mark done / no-show, manage services & hours

### 3.2 Frontend (`web/`, Kotlin/JS)
- [x] Kotlin/JS + kotlinx.html, served by the Go API (replaced the React plan)
- [x] Public page `/b/{slug}`: pick service → date → slot → name/phone/opt-in → confirm
- [x] Owner dashboard: login, today's appointments, buttons for done / no-show / cancel
- [x] Handle 409 nicely ("someone just took that slot, pick another")

**Done when:** owner A can never see or change owner B's appointments (write a test for it),
and a full booking works end to end in the browser.

---

## Week 4 — Real WhatsApp, Docker, deploy

**Goal:** a real message lands on a real phone, from a deployed app.

**Concepts:** third-party APIs, secrets management, webhooks, containers, deploys.

### 4.1 WhatsApp
- [x] `TwilioSender` implementing `notify.Sender` (WhatsApp sandbox)
- [x] Pick the sender from config: fake locally, Twilio when credentials are set
- [x] Only send to customers with `whatsapp_opt_in = true`
- [x] Phone numbers normalised to E.164 at booking time

### 4.2 Docker for api + worker
- [x] Multi-stage `Dockerfile` (build with `golang`, run on a small base image)
- [x] `api` and `worker` services in `docker-compose.yml`,
      `depends_on: postgres: condition: service_healthy`
- [x] Container `DATABASE_URL` uses host `postgres`, not `localhost`

### 4.3 Deploy
- [ ] Pick a host (Fly.io, Railway, Render…) with managed Postgres — see README "Deploy"
- [x] Migrations run as a deploy step, not via `docker-entrypoint-initdb.d`
      (embedded runner in `internal/store/migrate.go`, advisory-locked)
- [x] Logs you can read in production

**Done when (still to do — needs your Twilio + hosting accounts):** you book an appointment on the deployed site and get the
WhatsApp reminder on your phone.

---

## Stretch goals

- [ ] Split shifts: allow several opening periods per weekday (schema change + migration `003`)
- [x] Composite FK so an appointment's service always belongs to the same business
- [ ] Customer confirms via WhatsApp reply → status `confirmed` (Twilio webhook)
- [x] No-show rate per business on the owner dashboard
- [x] Rate limiting on the public booking endpoint
- [x] CI: `go test -race ./...` + migration check on every push (GitHub Actions)

---

## Log

Write one line per session: date, what you built, what you learned, what confused you.

| Date | Built | Learned | Confused by |
|------|-------|---------|-------------|
| 2026-10-03 | Schema `001_init.sql` | Exclusion constraints, partial indexes, `RESTRICT` vs `CASCADE` | |
| 2026-10-06 | Weeks 1–4 code: store, booking rules, API, auth, worker, Twilio sender, Kotlin/JS frontend, Docker, CI | `SKIP LOCKED` queues, DST-safe slots, CSRF via JSON-only writes | |
