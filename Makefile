.PHONY: run test lint web-build build
run:
	go run ./cmd/server
test:
	go test ./...
lint:
	go vet ./...
web-build:
	pnpm --dir web build
build: web-build
	go build -o bin/ticket-hub ./cmd/server
