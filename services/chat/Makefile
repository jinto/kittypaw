BINARY := kittyrelay
PKG    := ./cmd/kittyrelay

.PHONY: build test lint fmt run clean

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

clean:
	rm -f $(BINARY)
