.PHONY: build test generate vet fmt

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
