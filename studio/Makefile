# Crew Studio. Run `make help` for the targets.
SHELL := /bin/bash
GO := GOTOOLCHAIN=local go
PYTHON := $(abspath ../.venv/bin/python)
# Exports STUDIO_* (database URLs etc.) from studio/.env for the targets that need them
LOAD_ENV := set -a && . ./.env && set +a

.PHONY: help setup db db-stop test-db build web-build dev-api dev-web start migrate-status migrate-down reset-admin-password test test-go test-runner test-web test-e2e clean

help:
	@echo "setup      first-time setup: .env, Postgres, migrations, UI dependencies"
	@echo "db         start Postgres (Docker, loopback port 55432)"
	@echo "dev-api    run the Go API on 127.0.0.1:8088 (rebuilds first)"
	@echo "dev-web    run the Vite dev UI on 127.0.0.1:5174"
	@echo "start      build everything and serve UI + API from 127.0.0.1:8088"
	@echo "test       run every test suite (Go, Python runner, web)"
	@echo "test-e2e   run the Playwright browser tests against a throwaway server and database (e2e/)"
	@echo "migrate-status        list database migrations and whether each is applied"
	@echo "migrate-down VERSION=N list what rolling back to version N would undo; add YES=1 to do it (data is lost)"
	@echo "reset-admin-password [EMAIL=a@b] set a new administrator password (asked twice, hidden), clear lockouts, sign them out"

setup:
	@test -f .env || (cp .env.example .env && PW=$$(openssl rand -hex 16) && sed -i "s/change-me/$$PW/g" .env && chmod 600 .env && echo "created studio/.env")
	$(MAKE) db test-db
	cd web && npm install
	$(MAKE) build
	cd server && ./bin/crewstudio -studio-dir .. -migrate

db:
	docker compose --env-file .env up -d --wait

db-stop:
	docker compose --env-file .env stop

test-db: db
	@docker exec crewstudio-postgres-1 psql -U crewstudio -tc "SELECT 1 FROM pg_database WHERE datname = 'crewstudio_test'" | grep -q 1 \
		|| docker exec crewstudio-postgres-1 psql -U crewstudio -c "CREATE DATABASE crewstudio_test"

build:
	cd server && $(GO) build -o bin/crewstudio ./cmd/crewstudio

web-build:
	cd web && npm run build

dev-api: build
	cd server && ./bin/crewstudio -studio-dir ..

dev-web:
	cd web && npm run dev

start: build web-build
	cd server && ./bin/crewstudio -studio-dir ..

migrate-status: build
	cd server && ./bin/crewstudio -studio-dir .. -migrate-status

migrate-down: build
	@test -n "$(VERSION)" || (echo "usage: make migrate-down VERSION=6 [YES=1]   (rolls back every migration newer than 6)" && exit 1)
	cd server && ./bin/crewstudio -studio-dir .. -migrate-down-to "$(VERSION)" $(if $(YES),-yes)

reset-admin-password: build
	cd server && ./bin/crewstudio -studio-dir .. -reset-admin-password $(if $(EMAIL),-email "$(EMAIL)")

test: test-go test-runner test-web

test-go: test-db
	$(LOAD_ENV) && cd server && $(GO) test -race -cover -timeout 30m ./...

test-runner:
	cd runner && $(PYTHON) -m pytest -q --cov=studio_runner --cov=run_crew --cov-branch

test-web:
	cd web && npm run test:coverage

clean:
	rm -rf server/bin web/dist

# Browser end-to-end tests: own Postgres database (crewstudio_e2e), own server on 127.0.0.1:8091, fake LLM
test-e2e:
	cd e2e && (test -d node_modules || npm install) && npx playwright test
