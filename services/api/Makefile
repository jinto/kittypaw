BINARY := kittypaw-api
PKG    := ./cmd/server

.PHONY: build test test-integration test-integration-down lint run clean seed-wikidata seed-seoul-metro benchmark-resolve

build:
	go build -o $(BINARY) $(PKG)

test:
	go test ./... -v -count=1

test-unit:
	go test ./... -v -count=1 -short

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .
	goimports -w .

run: build
	@if [ -f .env ]; then set -a && . ./.env && set +a; fi && ./$(BINARY)

clean:
	rm -f $(BINARY)

# --- Geo Resolve (Plan 6) -----------------------------------------------------
# Wikidata SPARQL → places (CC0). Idempotent transactional swap, safe to re-run.
seed-wikidata:
	@if [ -f .env ]; then set -a && . ./.env && set +a; fi && go run ./cmd/seed-wikidata

# Seoul Metro 1-8호선 CSV → places. Manual download required (see docs/maintenance.md).
seed-seoul-metro:
	@if [ -f .env ]; then set -a && . ./.env && set +a; fi && go run ./cmd/seed-seoul-metro --csv=testdata/seoul_metro.csv

# Benchmark resolve corpus against a running server. Exits 1 if precision < gate.
benchmark-resolve:
	go run ./cmd/benchmark-resolve --base=$${KITTYAPI_BASE:-http://localhost:8080}

# --- Integration test (docker-compose isolated PostgreSQL) ----------------------
# Spins up postgres:17-alpine on port 15432 with pg_trgm pre-installed,
# applies all migrations, then runs `go test -tags=integration ./...`.
# Container persists between runs for fast iteration; tear down with
# `make test-integration-down`.
test-integration:
	@docker-compose -f docker-compose.test.yml up -d
	@echo "waiting for postgres-test to be healthy..."
	@for i in $$(seq 1 30); do \
		if docker exec kittypaw-postgres-test pg_isready -U kittypaw -d kittypaw_api_test >/dev/null 2>&1; then \
			echo "postgres-test ready"; break; \
		fi; \
		sleep 1; \
	done
	DATABASE_URL="postgres://kittypaw:kittypaw@localhost:15432/kittypaw_api_test?sslmode=disable" \
		go test -tags=integration -count=1 ./...

test-integration-down:
	@docker-compose -f docker-compose.test.yml down -v
