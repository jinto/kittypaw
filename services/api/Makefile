BINARY := kittypaw-api
PKG    := ./cmd/server

.PHONY: build test lint run clean

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
