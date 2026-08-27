.PHONY: build test generate generate-check vet fmt lint integration quickstart

build:
	go build ./...

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

# Regenerate every *_gen.go (needs the //go:generate line in each repository package)
generate:
	go generate ./...

lint:
	golangci-lint run ./...

# Fail if generated files are stale
generate-check:
	go run ./cmd/jpagen -dir example/warehouse -type Repository -mock -check
	cd integration && go run github.com/shubhesh07/gojpa/cmd/jpagen -dir cmd/quickstart -type ProductRepository -mock -check

# Real databases: SQLite always; MySQL/Postgres via Docker (testcontainers)
integration:
	cd integration && go test -count=1 ./...

# Runnable end-to-end example on in-memory SQLite (no database to install)
quickstart:
	cd integration && go run ./cmd/quickstart
