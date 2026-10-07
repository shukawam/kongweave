.PHONY: build test check

build:
	go build -trimpath -o bin/kongweave ./cmd/kongweave

test:
	go test ./...

check:
	go vet ./...
	go test -race ./...
