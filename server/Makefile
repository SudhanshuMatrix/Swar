GO ?= go
export GOTOOLCHAIN ?= local
BIN := bin/swar-server

.PHONY: run build test race vet fmt check docker clean

run:
	$(GO) run ./cmd/server

build:
	CGO_ENABLED=0 $(GO) build -trimpath -o $(BIN) ./cmd/server

test:
	$(GO) test -timeout 180s ./...

race:
	$(GO) test -race -count=1 -timeout 180s ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w .

## Everything CI should run.
check: vet race build
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed:"; gofmt -l .; exit 1)

docker:
	docker build -t swar-server .

clean:
	rm -rf bin
