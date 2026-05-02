BINARY := kittyportal
PKG    := ./cmd/server
VERSION ?= $(shell git -C ../.. describe --tags --always 2>/dev/null || echo dev)
COMMIT  ?= $(shell git -C ../.. rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT)

.PHONY: build test run clean smoke

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

test:
	go test ./... -v -count=1

run: build
	@if [ -f .env ]; then set -a && . ./.env && set +a; fi && ./$(BINARY)

clean:
	rm -f $(BINARY)

smoke:
	@bash deploy/smoke.sh
