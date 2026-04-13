# Contributing to Duq Gateway

Thank you for your interest in contributing! This document provides guidelines for contributing to this project.

## Getting Started

1. Fork the repository
2. Clone your fork locally
3. Ensure Go 1.21+ is installed
4. Create a feature branch from `main`

## Development Workflow

### 1. Create a Branch

```bash
git checkout -b feature/your-feature-name
# or
git checkout -b fix/bug-description
```

### 2. Make Changes

- Write clean, idiomatic Go code
- Follow existing code patterns
- Add tests for new functionality
- Update documentation if needed

### 3. Test Your Changes

```bash
# Run tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run linting
golangci-lint run
```

### 4. Build and Verify

```bash
# Build
go build -o duq-gateway .

# Verify binary works
./duq-gateway --help
```

### 5. Commit Your Changes

Use conventional commit messages:

```
feat: add rate limiting middleware
fix: handle connection timeout properly
docs: update API endpoint documentation
refactor: simplify webhook handler
test: add tests for voice endpoint
```

### 6. Submit a Pull Request

1. Push your branch to your fork
2. Open a Pull Request against `main`
3. Fill out the PR template
4. Wait for review

## Code Style

### Go

- Follow [Effective Go](https://go.dev/doc/effective_go)
- Use `gofmt` for formatting
- Use `golangci-lint` for linting
- Keep functions focused and small
- Document exported functions

### Formatting

```bash
# Format code
go fmt ./...

# Run linter
golangci-lint run
```

## Testing

- Write tests for all new features
- Use table-driven tests where appropriate
- Test error conditions
- Aim for high coverage on critical paths

```bash
# Run tests with verbose output
go test -v ./...

# Run specific test
go test -run TestFunctionName ./pkg/...
```

## Project Structure

```
.
├── main.go              # Application entry point
├── config.go            # Configuration loading
├── internal/
│   ├── middleware/      # HTTP middleware
│   ├── handlers/        # Request handlers
│   └── session/         # Session management
├── pkg/
│   └── tracing/         # Tracing utilities
└── docs/                # Documentation
```

## Pull Request Guidelines

### PR Title

Use conventional commit format:

- `feat: ...` - New feature
- `fix: ...` - Bug fix
- `docs: ...` - Documentation only
- `refactor: ...` - Code restructuring
- `test: ...` - Test additions/changes
- `chore: ...` - Maintenance tasks

### PR Description

Include:
- Summary of changes
- Related issue number (if any)
- Testing performed
- Breaking changes (if any)

## Reporting Issues

### Bug Reports

Include:
- Clear description of the bug
- Steps to reproduce
- Expected vs actual behavior
- Environment details (Go version, OS)
- Relevant logs or error messages

### Feature Requests

Include:
- Clear description of the feature
- Use case / motivation
- Proposed implementation (optional)

## Security

For security vulnerabilities, please see [SECURITY.md](SECURITY.md) for reporting instructions.

## Questions?

Open a GitHub Discussion or Issue for questions.

## License

By contributing, you agree that your contributions will be licensed under the project's MIT License.
