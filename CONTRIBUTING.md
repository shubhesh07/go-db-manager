# Contributing to gojpa

Thank you for your interest in contributing to gojpa! This document provides guidelines and instructions for contributing.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Setup](#development-setup)
- [Running Tests](#running-tests)
- [Submitting Changes](#submitting-changes)
- [Coding Standards](#coding-standards)
- [Reporting Bugs](#reporting-bugs)
- [Requesting Features](#requesting-features)

## Code of Conduct

By participating in this project, you agree to abide by our [Code of Conduct](CODE_OF_CONDUCT.md).

## Getting Started

1. **Fork** the repository on GitHub
2. **Clone** your fork locally:
   ```bash
   git clone https://github.com/YOUR-USERNAME/gojpa.git
   cd gojpa
   ```
3. **Add the upstream remote**:
   ```bash
   git remote add upstream https://github.com/shubhesh07/gojpa.git
   ```

## Development Setup

### Prerequisites

- Go 1.21 or higher
- Docker and Docker Compose (for running integration tests)
- golangci-lint (for linting)

### Install golangci-lint

```bash
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin latest
```

### Start local databases

```bash
docker-compose up -d
```

### Download dependencies

```bash
go mod download
```

## Running Tests

### Run all tests

```bash
go test ./...
```

### Run tests with coverage

```bash
go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
go tool cover -html=coverage.out -o coverage.html
```

### Run tests for a specific package

```bash
go test ./query/...
go test ./repository/...
```

### Run linter

```bash
golangci-lint run
```

### Run go vet

```bash
go vet ./...
```

## Submitting Changes

1. **Create a new branch** from `main`:
   ```bash
   git checkout -b feat/your-feature-name
   ```

2. **Make your changes** following the coding standards below

3. **Write or update tests** for your changes

4. **Run the test suite** to make sure everything passes:
   ```bash
   go test ./...
   golangci-lint run
   ```

5. **Commit your changes** using [Conventional Commits](https://www.conventionalcommits.org/):
   - `feat: add support for LIKE operator`
   - `fix: handle nil pointer in FindByID`
   - `docs: update README with pagination examples`
   - `test: add unit tests for query builder`
   - `chore: update dependencies`

6. **Push to your fork** and open a Pull Request

## Coding Standards

### General

- Follow standard Go conventions and idioms
- Use `gofmt` for formatting (enforced by CI)
- Use `goimports` for import organization
- Write clear, descriptive variable and function names
- Add package-level and exported function doc comments (Go doc style)

### Error Handling

- Always check and handle errors
- Wrap errors with context: `fmt.Errorf("parsing method %q: %w", name, err)`
- Never use `panic` in library code

### Testing

- Write unit tests for all new exported functions
- Use table-driven tests where appropriate
- Test both success and error cases
- Integration tests should use the `TestMain` pattern to set up/tear down databases

### Commit Messages

We use [Conventional Commits](https://www.conventionalcommits.org/). Valid prefixes:
- `feat:` - New feature
- `fix:` - Bug fix
- `docs:` - Documentation only changes
- `test:` - Adding or modifying tests
- `refactor:` - Code change that neither fixes a bug nor adds a feature
- `chore:` - Changes to build process, dependencies, CI config
- `perf:` - Performance improvement
- `ci:` - Changes to CI configuration

## Reporting Bugs

Please use the [Bug Report template](.github/ISSUE_TEMPLATE/bug_report.md) and include:

- Your Go version (`go version`)
- Your OS and architecture
- Database type and version (PostgreSQL/MySQL)
- A minimal code example that reproduces the issue
- Expected vs actual behavior
- Full error message and stack trace if applicable

## Requesting Features

Please use the [Feature Request template](.github/ISSUE_TEMPLATE/feature_request.md).

Describe the problem you're trying to solve, not just the solution you have in mind.

## Questions?

Open a [GitHub Discussion](https://github.com/shubhesh07/gojpa/discussions) for questions and general discussion.
