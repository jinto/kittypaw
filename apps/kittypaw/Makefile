BINARY    := bin/kittypaw
PKG       := ./cli
BUILDTIME := $(shell TZ=Asia/Seoul date +%Y-%m-%d\ %H:%M\ KST)
LDFLAGS   := -X 'main.version=dev ($(BUILDTIME))'

.PHONY: build test test-unit test-integration test-e2e test-ci lint fmt run clean eval-secretary eval-user-flows eval-local smoke

build:
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

test: test-ci

test-ci:
	go test ./... -v -count=1
	go test -tags integration ./... -v -count=1
	go test -tags e2e ./... -v -count=1
	golangci-lint run ./...
	$(MAKE) build

test-unit:
	go test ./... -v -count=1 -short

test-integration:
	go test -tags integration ./... -v -count=1

test-e2e:
	go test -tags e2e ./... -v -count=1

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .
	goimports -w .

run: build
	./$(BINARY)

eval-secretary:
	eval/secretary_smoke/run.sh

eval-user-flows:
	eval/user_vision_flows/run.sh

eval-local: eval-secretary eval-user-flows

smoke: eval-local

clean:
	rm -f $(BINARY)
