# Project Guidelines

## Project Overview

This is a **waiting list service** written in Go. It uses PostgreSQL for storage and relies on Go's standard library (including `net/http.ServeMux`) to minimize dependencies, with the exception of the PostgreSQL driver and the configuration library.

- **Module**: `github.com/tipok/waitinglist`
- **Go version**: 1.25.1
- **Entry point**: `cmd/server/main.go`
- **Binary name**: `waitinglist` (built to `bin/`)

### External Dependencies

| Dependency | Purpose |
|---|---|
| `github.com/lib/pq` | PostgreSQL driver for `database/sql` |
| `github.com/knadh/koanf/v2` | Configuration loading from JSON file |

### Project Structure

```
waitinglist/
├── cmd/server/main.go              # Application entry point
├── internal/
│   ├── config/config.go            # Configuration loading (koanf, JSON file)
│   ├── database/postgres.go        # DB connection setup + migration runner
│   ├── handler/
│   │   ├── user.go                 # HTTP handlers for user entity endpoints
│   │   └── waitinglist.go          # HTTP handlers for waiting list endpoints
│   ├── model/model.go              # Data structures (UserEntity, WaitingListEntry)
│   └── repository/
│       ├── user.go                 # DB operations for user_entity table
│       └── waitinglist.go          # DB operations for waiting_list table
├── migrations/
│   └── 001_init.sql                # SQL migration for initial schema
├── config.json                     # Default configuration file
├── docs/plans/                     # Feature plans
├── Makefile
├── go.mod
└── go.sum
```

### Configuration

The application loads configuration from a JSON file passed via `--config` flag:

```bash
./bin/waitinglist --config config.json
```

| Field | Default |
|---|---|
| `port` | `8080` |
| `database.url` | `postgres://localhost:5432/waitinglist?sslmode=disable` |

### Database

- PostgreSQL with two tables: `user_entity` and `waiting_list`.
- Migrations are plain `.sql` files in `migrations/`, executed in alphabetical order on startup.
- Schema uses `IF NOT EXISTS` for idempotent migrations.
- Integration tests requiring a real database are gated by the `TEST_DATABASE_URL` environment variable.

### Startup Flow

1. Parse `--config` flag
2. Load configuration from JSON file (koanf)
3. Connect to PostgreSQL
4. Run migrations from `migrations/` directory
5. Start HTTP server on configured port

## Plan Management

- All feature plans are stored in `docs/plans/`, organized by feature in their own directories.
- Each feature directory contains a `plan.md` file describing the design, requirements, and implementation steps for that feature.
- Plan directories must be prefixed with numbers in the correct implementation order (e.g., `01-project-setup`, `02-database`, `03-user-entity`). When adding a new plan, assign the next sequential number.
- When creating or updating plans:
  1. Identify the feature scope and create/update the corresponding directory under `docs/plans/<NN-feature-name>/` (where `NN` is the sequence number).
  2. Each plan should include: **Overview**, **Requirements**, **Design**, **Implementation Steps**, **Testing**, and **Acceptance Criteria**.
  3. Every plan must include a **Testing** section that describes the unit tests to be written, covering core logic, edge cases, and error/negative scenarios.
  4. Plans should be kept up to date as implementation progresses — mark completed steps and note any deviations.
- Cross-cutting concerns (e.g., database schema shared across features) get their own plan directory.
- Reference related plans from within a plan when there are dependencies between features.

### Current Plans

| Plan | Status | Description |
|---|---|---|
| `01-project-setup` | ✅ Complete | Go module, config loading, DB connection, HTTP server entry point |
| `02-database` | ✅ Complete | PostgreSQL schema migration (user_entity, waiting_list tables) and migration runner |
| `03-user-entity` | Not started | User entity CRUD operations |
| `04-waiting-list` | Not started | Waiting list operations |
| `05-api` | Not started | HTTP API endpoints |
| `11-ip-tracking` | Not started | Track client IP address on waiting list entry creation |
| `12-docker-build` | Not started | Multi-stage Dockerfile with distroless image and arm64/amd64 Make targets |

## Development Workflow

The project includes a `Makefile` with standard targets. After making any code changes, always run formatting, linting, and tests before considering the work complete.

### After Every Code Change

**All three steps are mandatory** — never skip any of them. Every plan's implementation and verification steps must include all three:

1. **Format code** — run `make format` to auto-fix formatting with `goimports`.
2. **Lint code** — run `make lint` to check for issues using `golangci-lint` (runs via Docker or containers).
3. **Run tests** — run `make test` to execute the full test suite (`go test ./...`).

> ⚠️ A change is not considered complete until `make format`, `make lint`, and `make test` all pass. Plans must always reference all three commands in their final verification step.

### Available Makefile Targets

| Target          | Command            | Description                                                        |
|-----------------|--------------------|--------------------------------------------------------------------|
| `make build`    | `go build`         | Build the binary to `bin/waitinglist`.                             |
| `make test`     | `go test ./...`    | Run all tests.                                                     |
| `make lint`     | `golangci-lint`    | Lint the codebase (runs in Docker).                                |
| `make format`   | `goimports -w .`   | Auto-format all Go files.                                          |
| `make format-check` | `goimports -l .` | Check formatting without modifying files (CI-friendly).           |
| `make deps`     | `go mod tidy/download` | Tidy and download module dependencies.                        |
| `make clean`    | `rm -rf bin/`      | Remove build artifacts.                                            |
| `make run`      | build + execute    | Build and run the server binary.                                   |
| `make release`  | cross-compile      | Build release binaries for all supported platforms.                |

### Prerequisites

- **Docker** must be installed and running for `make lint` (golangci-lint runs as a container).
- **goimports** must be installed for `make format` / `make format-check` (`go install golang.org/x/tools/cmd/goimports@latest`).

## Logging

- Always use `log/slog` for logging. Do not use the standard `log` package or third-party logging libraries.
- Create a logger instance with `slog.New(slog.NewTextHandler(os.Stderr, nil))` and pass it where needed.
- Use structured key-value pairs for log arguments: `logger.Info("message", "key", value)` — never use `fmt.Sprintf`-style formatting.

## Testing

- Every implementation change must include unit tests.
- Tests should cover the core logic, edge cases, and error/negative scenarios for the changed code.
- Do not merge or consider a feature complete without accompanying unit tests.
- Integration tests that require external services (e.g., PostgreSQL) should be gated by environment variables (e.g., `TEST_DATABASE_URL`) and skip gracefully when not set.
