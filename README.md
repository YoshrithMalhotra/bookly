# Bookly

Appointment booking for salons, tutors and clinics, with WhatsApp reminders
(fewer no-shows) and a Google review request after each visit.

- **Customers** open the business's link (`/b/your-salon`), pick a service and a
  free time, and get a WhatsApp reminder 24h before with a link to cancel.
- **Owners** sign up, add services and opening hours, see their day, and mark
  visits done / no-show. Marking *done* sends the Google review request.

Backend in Go + Postgres. Frontend in Kotlin/JS, served by the Go API from the
same origin (no CORS, cookie sessions).

## Run locally

Needs Go 1.26, JDK 21 and Docker.

```bash
cp .env.example .env    # then set a real password
make db                 # Postgres in Docker
make web                # build the Kotlin frontend (first run downloads Gradle/Node)
make api                # http://localhost:8080 — applies migrations on start
make worker             # sends due reminders / review requests (logged until Twilio is set)
make test               # unit + integration tests against the Docker Postgres
```

Or run everything in containers: `make up`.

## Layout

```
cmd/api/            HTTP server (API + frontend), runs migrations on start
cmd/worker/         sends due reminders and review requests
internal/booking/   rules: slots, validation, phone numbers, message timing (pure, unit tested)
internal/store/     all SQL, migrations runner, sessions
internal/api/       routes, JSON, auth, CSRF, rate limits
internal/worker/    the send loop (FOR UPDATE SKIP LOCKED, retries)
internal/notify/    Sender interface, fake sender, Twilio WhatsApp sender
migrations/         schema, embedded into the binaries
web/                Kotlin/JS frontend (Gradle)
```

## API

All errors are `{"error": "..."}`. Writes must be `Content-Type: application/json`.

| Method | Path | Who |
|---|---|---|
| GET | `/health` | load balancer (pings the DB) |
| GET | `/api/businesses/{slug}` | public: name, services, hours |
| GET | `/api/businesses/{slug}/slots?service_id=&date=YYYY-MM-DD` | public |
| POST | `/api/businesses/{slug}/appointments` | public → 201 / 400 / 404 / 409 |
| GET, POST | `/api/manage/{token}`, `/api/manage/{token}/cancel` | customer with their link |
| POST | `/api/signup`, `/api/login`, `/api/logout` | owner |
| GET, PUT | `/api/owner/business` | owner settings |
| GET, POST, PUT | `/api/owner/services`, `/api/owner/services/{id}` | owner |
| GET, PUT | `/api/owner/hours` | owner (weekday 0 = Sunday) |
| GET | `/api/owner/appointments?date=&days=` | owner |
| POST | `/api/owner/appointments/{id}/status` | owner: confirmed, cancelled, done, no_show |
| GET | `/api/owner/appointments/{id}/messages` | owner |
| GET | `/api/owner/stats` | owner, last 90 days |

## Deploy

One Docker image runs both processes: the default entrypoint is the API,
`/app/worker` is the worker. Any host that runs a Dockerfile with managed
Postgres works (Fly.io, Render, Railway…):

1. Create a managed Postgres (17 recommended) and note its `DATABASE_URL`.
2. Deploy the image twice — a **web** service (port 8080, health check `/health`)
   and a **worker** service with command `/app/worker`. Run 1+ of each; several
   workers are safe.
3. Set env vars on both (see `.env.example`): `APP_ENV=prod`, `DATABASE_URL`,
   `PUBLIC_URL=https://your-domain`, `TRUST_PROXY=true`, `DEFAULT_COUNTRY_CODE`,
   and the `TWILIO_*` values.
4. Put it behind HTTPS (the session cookie is `Secure` in prod).

Migrations run automatically when either process starts, under a Postgres
advisory lock, so parallel starts are safe.

## WhatsApp (Twilio)

1. Sandbox for testing: set `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`,
   `TWILIO_WHATSAPP_FROM=+14155238886`, and join the sandbox from your phone.
2. Production: register a WhatsApp sender in Twilio, then create and get
   approved two **Content templates** (utility category). Variables must be in
   this order:
   - Reminder: `{{1}}` name, `{{2}}` service, `{{3}}` business, `{{4}}` date & time, `{{5}}` cancel link
   - Review: `{{1}}` name, `{{2}}` business, `{{3}}` review link

   Put their `HX…` SIDs in `TWILIO_REMINDER_CONTENT_SID` / `TWILIO_REVIEW_CONTENT_SID`.
   Without approved templates WhatsApp rejects these messages outside the sandbox.

Messages are only sent to customers who ticked the WhatsApp box. Failed sends
retry with backoff (1m, 5m, 25m, 2h) and are marked `failed` after 5 tries or
straight away for permanent errors (bad number, rejected template); owners see
the status under **Messages** on each appointment.

## Before you take money

The app is feature-complete for the core flow, but these are business/legal
items code can't do for you:

- **Billing**: there's no subscription/payment system. Invoice manually at first,
  or add Stripe Checkout + a `plan` column on `businesses`.
- **Privacy policy & terms**: you store customers' names and phone numbers and
  message them; you need a privacy policy, a data processing agreement for
  business customers, and (UK/EU) GDPR basics like deletion on request.
- **WhatsApp Business approval** and templates (above) take days, not hours.
- **Backups**: turn on automated daily backups / PITR on your managed Postgres.
- **Password reset**: not built yet. Until it is, reset by hand:
  generate a bcrypt hash and `UPDATE businesses SET password_hash = ... WHERE owner_email = ...`.
- **Email**: no transactional email (signup confirmation, reset links) yet.
