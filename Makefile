.PHONY: build test test-integration lint clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/jake-landersweb/previewctl/src/version.Version=$(VERSION)

build:
	go build -ldflags "$(LDFLAGS)" -o bin/previewctl ./src/cmd/previewctl

test:
	go test ./src/...

test-integration:
	go test -tags integration ./src/...

fmt:
	go fmt ./src/...

lint:
	golangci-lint run ./src/...

clean:
	rm -rf bin/
