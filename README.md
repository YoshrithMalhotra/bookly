# Bookly

Appointment booking for salons, tutors and clinics, with WhatsApp reminders
(fewer no-shows) and a Google review request after each visit.

- **Customers** open the business's link (`/b/your-salon`), pick a service and a
  free time, and get a WhatsApp reminder 24h before with a link to cancel.
- **Owners** sign up, add services and opening hours, see their day, and mark
  visits done / no-show. Marking *done* sends the Google review request.
- **You** get paid by Stripe subscription after a free trial. When a trial ends
  without a subscription (or a subscription is cancelled/unpaid) the owner's
  booking page pauses until they subscribe.
- **Running the day**: owners add phone bookings and walk-ins, move
  appointments (the reminder follows), block holidays and breaks as time off,
  see customers' notes, and download everything as CSV.
- **Accounts**: login, email confirmation, change login email, password reset
  by email, change password, delete account
  (cancels the subscription and deletes all data), Terms and Privacy pages.

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
internal/billing/   Stripe client, webhook signature check, "has this business paid?"
internal/email/     SMTP email (password resets, owner notifications)
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
| PUT | `/api/owner/password` | owner: change password |
| DELETE | `/api/owner/account` | owner: delete everything (needs password) |
| GET | `/api/owner/billing` | owner: plan status |
| POST | `/api/owner/billing/checkout`, `/api/owner/billing/portal` | owner → returns a Stripe URL |
| POST | `/api/password/forgot`, `/api/password/reset` | owner, from the emailed link |
| POST | `/api/stripe/webhook` | Stripe (signature checked) |
| GET | `/api/config` | public: company name, price label, trial length |
| POST | `/api/owner/appointments` | owner: phone booking / walk-in |
| POST | `/api/owner/appointments/{id}/reschedule` | owner: move an upcoming appointment |
| GET, POST, DELETE | `/api/owner/time-off`, `/api/owner/time-off/{id}` | owner: holidays and breaks |
| PUT | `/api/owner/email` | owner: change login email (needs password) |
| POST | `/api/owner/email/resend`, `/api/email/verify` | email confirmation |
| GET | `/api/owner/export.csv` | owner: all appointments |

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

## Payments (Stripe)

1. In Stripe, create a Product with a recurring Price (e.g. £19/month) and copy
   its `price_…` id into `STRIPE_PRICE_ID`.
2. Copy your secret key into `STRIPE_SECRET_KEY` (test key first).
3. Add a webhook endpoint `https://YOUR_DOMAIN/api/stripe/webhook` with events
   `checkout.session.completed` and `customer.subscription.created`, `.updated`,
   `.deleted`, `.paused`, `.resumed`; put its signing secret in `STRIPE_WEBHOOK_SECRET`.
4. Turn on the **Customer portal** (Settings → Billing → Customer portal) so
   owners can change card, see invoices and cancel.
5. Test with card `4242 4242 4242 4242`, then switch to live keys.

New businesses get `TRIAL_DAYS` free (default 14, no card needed). The owner
subscribes from the **Billing** tab; the dashboard warns 7 days before the
trial ends and when a payment fails. `past_due` keeps the page open while
Stripe retries the card. Existing appointments and reminders keep working even
when a page is paused.

## Email

Set `SMTP_*` and `EMAIL_FROM` for any provider (Resend, Postmark, SES, Mailgun…)
and verify your sending domain (SPF/DKIM) with them. Bookly sends: welcome,
password reset links (1 hour, single use), and a notice to the owner for each
new or cancelled booking.

## Before you go live

Code can't do these for you:

- **Legal review**: `/terms` and `/privacy` are a sensible starting template
  filled from `COMPANY_NAME` / `SUPPORT_EMAIL`, not legal advice. Have them
  checked for your country (UK/EU GDPR: you're a processor for the businesses'
  customer data, so offer a DPA).
- **WhatsApp Business approval** and templates (above) take days.
- **Stripe account activation** (business details, bank account) before live keys work.
- **Domain, HTTPS and email domain verification** (SPF/DKIM).
- **Backups**: turn on daily backups / point-in-time recovery for Postgres.
- **Monitoring**: point an uptime checker at `/health` and keep an eye on logs
  for `level=ERROR`.

Deliberately not built yet (each is a sizeable product decision, not a gap
in what's here): taking deposits from customers at booking (needs Stripe
Connect so money goes to each business), several staff members with their
own calendars, several logins per business, and two-way WhatsApp
(customers confirming by replying).