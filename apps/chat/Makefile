BINARY := kittychat
PKG    := ./cmd/kittychat

.PHONY: build test lint fmt run smoke-local clean

build:
	go build -o $(BINARY) $(PKG)

test:
	go test ./... -count=1

lint:
	golangci-lint run ./...

fmt:
	gofmt -w cmd internal

run:
	go run $(PKG)

smoke-local:
	go run ./cmd/kittychat-smoke

clean:
	rm -f $(BINARY)
