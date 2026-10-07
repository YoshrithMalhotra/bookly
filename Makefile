-include .env
export

.PHONY: db db-reset psql web api worker test test-unit up

db:        ## start Postgres
	docker compose up -d postgres

db-reset:  ## wipe the database (migrations re-run when the api/worker starts)
	docker compose down -v && docker compose up -d postgres

psql:      ## open a SQL shell
	docker compose exec postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB)

web:       ## build the Kotlin/JS frontend into web/build/dist/js/productionExecutable
	cd web && ./gradlew --no-daemon -q jsBrowserDistribution

api:       ## run the API (applies migrations on start)
	go run ./cmd/api

worker:
	go run ./cmd/worker

test-unit: ## tests that need no database
	go test -race ./internal/booking/ ./internal/notify/

test:      ## all tests; DB tests use TEST_DATABASE_URL (a user that may CREATE DATABASE)
	TEST_DATABASE_URL=$${TEST_DATABASE_URL:-postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:$(POSTGRES_PORT)/postgres?sslmode=disable} \
		go test -race ./...

up:        ## run everything in Docker: postgres + api + worker
	docker compose --profile app up --build
