BINARY := kittypaw-api
PKG    := ./cmd/server

.PHONY: build test lint run clean seed-wikidata seed-seoul-metro benchmark-resolve

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
