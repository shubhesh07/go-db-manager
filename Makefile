.PHONY: build test generate generate-check vet fmt lint integration

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

# Real databases: SQLite always; MySQL/Postgres via Docker (testcontainers)
integration:
	cd integration && go test -count=1 ./...
