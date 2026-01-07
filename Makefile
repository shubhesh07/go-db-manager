.PHONY: build test clean run-example

# Build the project
build:
	go build ./...

# Run tests
test:
	go test -v ./...

# Run example
run-example:
	go run cmd/example/main.go

# Clean build artifacts
clean:
	go clean
	rm -f *.test *.out

# Install dependencies
deps:
	go mod download
	go mod tidy

# Format code
fmt:
	go fmt ./...

# Lint code
lint:
	golangci-lint run

# Run migrations (example)
migrate:
	@echo "Run migrations using your migration tool"

