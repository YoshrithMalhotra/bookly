# Bookly

Appointment booking for salons, tutors and clinics, with WhatsApp reminders
(fewer no-shows) and a Google review request after each visit.

## Run locally

```bash
cp .env.example .env    # then set a real password
make db                 # Postgres in Docker, runs migrations/ on first start
make api                # http://localhost:8080/health
make worker             # message scheduler
make test
```

## Layout

```
cmd/api/          HTTP API
cmd/worker/       sends due reminders and review requests
internal/booking/ appointment rules
internal/notify/  Sender interface (fake now, WhatsApp later)
internal/store/   all SQL
migrations/       schema
web/              React frontend (week 3)
```
