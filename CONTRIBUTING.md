# Contributing to Vocdoni Passport Prover

Thank you for your interest in contributing! This document provides guidelines for contributing to the Vocdoni Passport Prover.

## Code of Conduct

By participating in this project, you agree to abide by our code of conduct: be respectful, inclusive, and constructive in all interactions.

## How to Contribute

### Reporting Bugs

1. Check if the bug has already been reported in [Issues](https://github.com/vocdoni/vocdoni-passport-prover/issues)
2. If not, create a new issue with:
   - A clear, descriptive title
   - Steps to reproduce the bug
   - Expected vs actual behavior
   - Environment information (OS, Docker version, etc.)
   - Relevant logs or error messages

### Suggesting Features

1. Check existing issues and discussions for similar suggestions
2. Create a new issue with the "enhancement" label
3. Describe the feature and its use case
4. Explain why it would benefit users

### Pull Requests

1. Fork the repository
2. Create a feature branch from `main`:
   ```bash
   git checkout -b feature/your-feature-name
   ```
3. Make your changes following our coding standards
4. Write or update tests as needed
5. Ensure all tests pass:
   ```bash
   # Rust
   cargo test
   cargo fmt --check
   cargo clippy

   # Go
   cd server-go
   go test ./...
   gofmt -l .
   ```
6. Commit with clear, descriptive messages
7. Push to your fork and create a Pull Request

## Development Setup

### Prerequisites

- Rust 1.89+
- Go 1.24+
- Docker & Docker Compose
- (Optional) MongoDB for local testing

### Building

```bash
# Build Rust components
cargo build --release

# Build Go server
cd server-go && go build -o server ./cmd

# Build Docker images
docker build -t vocdoni-passport-prover .
docker build -f server-go/Dockerfile -t vocdoni-passport-server .
```

### Running Tests

```bash
# Rust tests
cargo test

# Go tests
cd server-go && go test -v ./...

# Integration tests (requires Docker)
docker compose -f server-go/docker-compose.yml up -d
curl http://localhost:8080/api/health
```

## Coding Standards

### Rust

- Follow the [Rust API Guidelines](https://rust-lang.github.io/api-guidelines/)
- Use `cargo fmt` for formatting
- Address all `cargo clippy` warnings
- Write documentation for public APIs
- Include unit tests for new functionality

### Go

- Follow [Effective Go](https://go.dev/doc/effective_go)
- Use `gofmt` for formatting
- Address all `go vet` warnings
- Write table-driven tests where appropriate

### Commit Messages

Use clear, descriptive commit messages:

```
feat: Add support for new circuit version
fix: Resolve memory leak in proof aggregation
docs: Update deployment instructions
refactor: Simplify witness solving pipeline
test: Add integration tests for API endpoints
```

### Code Review

All submissions require review:

1. Ensure CI checks pass
2. Address reviewer feedback
3. Keep PRs focused and reasonably sized
4. Update documentation as needed

## Architecture Notes

### Repository Structure

- **crates/**: Rust workspace with proving logic
- **server-go/**: HTTP server (orchestration only)
- **artifacts/**: Circuit artifacts (compatibility-sensitive)
- **config/**: Compatibility metadata

### Key Principles

1. **Proving logic belongs in Rust**: The Go server is orchestration only
2. **Version changes are compatibility work**: Don't treat them as simple bumps
3. **Test with real fixtures**: Synthetic tests aren't sufficient for ZK proofs

### Upgrading Dependencies

When upgrading zkPassport circuits, Barretenberg, or Noir:

1. Update this repository first
2. Validate with real fixtures
3. Update downstream consumers only after validation

See the README for detailed upgrade procedures.

## Security

If you discover a security vulnerability:

1. **Do NOT** open a public issue
2. Email security@vocdoni.io with details
3. Allow time for the issue to be addressed before disclosure

## License

By contributing, you agree that your contributions will be licensed under the AGPL-3.0 license.

## Questions?

- Join our [Discord](https://discord.gg/vocdoni)
- Check the [Documentation](https://docs.vocdoni.io)
- Open a [Discussion](https://github.com/vocdoni/vocdoni-passport-prover/discussions)

Thank you for contributing! 🙏
