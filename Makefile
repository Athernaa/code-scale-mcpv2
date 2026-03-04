.PHONY: build test lint fmt clean release

BINARY=code-scale-mcp
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS=-ldflags "-X main.version=$(VERSION)"

build:
	go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/code-scale-mcp/

test:
	go test ./... -count=1

test-verbose:
	go test ./... -v -count=1

fmt:
	gofmt -w .

lint:
	@which golangci-lint > /dev/null 2>&1 || echo "Install golangci-lint: https://golangci-lint.run/usage/install/"
	golangci-lint run ./...

clean:
	rm -rf bin/ dist/

release:
	goreleaser release --snapshot --clean
