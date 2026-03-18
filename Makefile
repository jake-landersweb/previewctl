.PHONY: build test test-integration lint clean

build:
	go build -o bin/previewctl ./src/cmd/previewctl

test:
	go test ./src/...

test-integration:
	go test -tags integration ./src/...

lint:
	golangci-lint run ./src/...

clean:
	rm -rf bin/
