BINARY    := bin/kittypaw
PKG       := ./cli
BUILDTIME := $(shell TZ=Asia/Seoul date +%Y-%m-%d\ %H:%M\ KST)
LDFLAGS   := -X 'main.version=dev ($(BUILDTIME))'

.PHONY: build test test-unit lint fmt run clean

build:
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

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
	./$(BINARY)

clean:
	rm -f $(BINARY)
