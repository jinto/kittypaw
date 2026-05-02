BINARY := kittyportal
PKG    := ./cmd/server

.PHONY: build test run clean smoke

build:
	go build -o $(BINARY) $(PKG)

test:
	go test ./... -v -count=1

run: build
	@if [ -f .env ]; then set -a && . ./.env && set +a; fi && ./$(BINARY)

clean:
	rm -f $(BINARY)

smoke:
	@bash deploy/smoke.sh
