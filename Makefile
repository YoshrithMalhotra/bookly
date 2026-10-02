include .env
export

.PHONY: db db-reset psql api worker test

db:        ## start Postgres
	docker compose up -d postgres

db-reset:  ## wipe the database and re-run migrations
	docker compose down -v && docker compose up -d postgres

psql:      ## open a SQL shell
	docker compose exec postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB)

api:
	go run ./cmd/api

worker:
	go run ./cmd/worker

test:
	go test -race ./...
