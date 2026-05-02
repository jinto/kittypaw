# Contributing

## Prerequisites

- Go 1.25+
- [golangci-lint](https://golangci-lint.run/welcome/install/) v2+
- [lefthook](https://github.com/evilmartians/lefthook) (git hooks)

## Quick Start

```bash
# Clone and build
git clone https://github.com/kittypaw-app/kitty.git
cd kittypaw
make build

# Install git hooks
lefthook install

# Run tests
make test
```

## Development Workflow

```bash
make build       # Build binary
make test        # All tests (verbose, no cache)
make test-unit   # Short tests only
make lint        # Lint (golangci-lint)
make fmt         # Format (gofmt + goimports)
make run         # Build and run
make clean       # Remove binary
```

## Commit Conventions

This project uses [Conventional Commits](https://www.conventionalcommits.org/). Lefthook enforces the format on every commit:

```
type(scope): description
```

**Types**: `feat`, `fix`, `refactor`, `perf`, `docs`, `chore`, `test`, `ci`, `build`, `merge`

**Examples**:
```
feat(engine): add workspace indexing with FTS search
fix(store): prevent cleanup from deleting recent execution records
refactor: rename cmd/kittypaw to cli for role-based naming
docs: update CLAUDE.md with development workflow
```

## Code Style

- Go standard formatting (`gofmt`)
- Import groups: stdlib, external, `github.com/jinto/kittypaw` (enforced by `goimports`)
- American English spelling in comments and strings
- Error strings: lowercase, no punctuation (per Go conventions)
- Unchecked errors: use `_ =` prefix when intentionally ignoring

## Testing

- Co-located tests (`*_test.go` in same package)
- Table-driven tests preferred
- Use `t.TempDir()` for filesystem tests (auto-cleanup)
- Use `t.Helper()` in test helpers

```bash
make test                        # All tests
make test-unit                   # Short tests only
go test ./engine/... -run TestX  # Single test
go test ./... -coverprofile=c.out && go tool cover -html=c.out  # Coverage
```

## Linting

golangci-lint v2 with 7 linters enabled. See `.golangci.yml` for details.

```bash
make lint                  # Run all linters
golangci-lint fmt ./...    # Auto-fix formatting
```

Pre-commit hooks run `gofmt` and `golangci-lint` automatically.

## Project Structure

See `CLAUDE.md` for architecture overview and design decisions.
