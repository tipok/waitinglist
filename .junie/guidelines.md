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
│   │   ├── health.go               # GET /healthz — DB-ping liveness/readiness probe
│   │   ├── ip.go                   # Client IP extraction helpers
│   │   ├── middleware.go           # LoggingMiddleware, JSONContentType
│   │   ├── response.go             # WriteJSON / WriteError helpers
│   │   └── waitinglist.go          # HTTP handlers for waiting list endpoints
│   ├── logger/logger.go            # slog logger construction
│   ├── model/model.go              # Data structures, sentinel errors, DB/Tx interfaces
│   ├── repository/
│   │   ├── scheduler.go            # DB operations for scheduler_state table
│   │   ├── user.go                 # DB operations for user_entity table
│   │   └── waitinglist.go          # DB operations for waiting_list table
│   └── waitlist/waitlist.go        # Background scheduler that grants access
├── migrations/
│   ├── 001_init.sql                # Initial schema (user_entity, waiting_list)
│   ├── 002_schema_improvements.sql
│   ├── 003_scheduler_state.sql
│   ├── 004_user_created_at.sql
│   ├── 005_user_entity_ip.sql
│   ├── 006_has_access_one_way.sql  # One-way has_access trigger (dropped by 007)
│   └── 007_access_audit_and_drop_one_way.sql  # Access audit columns; drops 006's trigger
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

### HTTP Endpoints

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/waitinglist` | Add a user to the waiting list (creates the user if needed). `201` on success, `409` if already on the list, `205` if the user already has access. |
| `GET`  | `/waitinglist` | List all waiting list entries. |
| `GET`  | `/waitinglist/users` | Look up users by email (`?email=`). Supports ETag caching. |
| `GET`  | `/healthz` | Health probe. Pings the database with a 2 s timeout. `200` healthy, `503` unhealthy. Excluded from `LoggingMiddleware` to avoid probe-spam. |
| `GET`  | `/admin/dashboard` | **Admin · Basic Auth.** Counters + per-day enlistment series (`?days=N`, default 90, max 365). |
| `GET`  | `/admin/users/access` | **Admin · Basic Auth.** Users with access; supports `?email=` substring filter, `?limit=` (max 200), `?offset=`. |
| `GET`  | `/admin/users/waitlist` | **Admin · Basic Auth.** Joined waitlist view including `weight`; same filter/pagination semantics. |
| `POST` | `/admin/users/{id}/grant-access` | **Admin · Basic Auth.** Admin-grants access (atomic with waitlist removal); returns the updated user. |
| `POST` | `/admin/users/{id}/revoke-access` | **Admin · Basic Auth.** Body `{"reason":"…"}` (1..500 chars). Calls `RevokeAccess` with the authenticated admin as `revoked_by`. |
| `DELETE` | `/admin/waitlist/{id}` | **Admin · Basic Auth.** Removes a single waiting-list row by entry id. |

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
| `12-docker-build` | ✅ Complete | Multi-stage Dockerfile with distroless image and arm64/amd64 Make targets |
| `13-github-docker-workflow` | ✅ Complete | GitHub Actions workflow building and pushing Docker images to ghcr.io |
| `14-already-has-access-response` | ✅ Complete | Return HTTP 205 on re-signup when user already has access; enforce one-way `has_access` invariant in DB |
| `15-health-check` | ✅ Complete | `GET /healthz` endpoint that pings the database and returns 200/503 with a JSON status body |
| `16-access-audit-and-revocation` | ✅ Complete | Audit columns (`access_granted_at/by`, `access_revoked_at/by/reason`); drop one-way trigger; `GrantAccessTx`/`RevokeAccessTx` |
| `17-admin-api-and-auth` | ✅ Complete | `/admin/*` JSON endpoints (dashboard, list, grant, revoke, delete) protected by configurable Basic Auth |
| `18-admin-web-ui` | Not started | Embedded HTML/JS admin page with dashboard, searchable lists, and revoke/grant/delete actions |

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
| `make docker-build:amd64` | container/docker build | Build Docker image for `linux/amd64`.                |
| `make docker-build:arm64` | container/docker build | Build Docker image for `linux/arm64`.                |
| `make docker-build` | both arch builds   | Build Docker images for both architectures.                        |

### Prerequisites

- **Docker** must be installed and running for `make lint` (golangci-lint runs as a container).
- **goimports** must be installed for `make format` / `make format-check` (`go install golang.org/x/tools/cmd/goimports@latest`).

## Logging

- Always use `log/slog` for logging. Do not use the standard `log` package or third-party logging libraries.
- Create a logger instance with `slog.New(slog.NewTextHandler(os.Stderr, nil))` and pass it where needed.
- Use structured key-value pairs for log arguments: `logger.Info("message", "key", value)` — never use `fmt.Sprintf`-style formatting.
- Request access logging is performed by `LoggingMiddleware` (`internal/handler/middleware.go`). `/healthz` requests are deliberately excluded so orchestrator probes do not flood the log stream; failures inside the health handler are still logged at `Warn`.

## Testing

- Every implementation change must include unit tests.
- Tests should cover the core logic, edge cases, and error/negative scenarios for the changed code.
- Do not merge or consider a feature complete without accompanying unit tests.
- Integration tests that require external services (e.g., PostgreSQL) should be gated by environment variables (e.g., `TEST_DATABASE_URL`) and skip gracefully when not set.
