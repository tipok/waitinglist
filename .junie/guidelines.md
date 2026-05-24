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
| `github.com/knadh/koanf/v2` | Configuration loading (JSON file + env vars) |
| `github.com/knadh/koanf/providers/env/v2` | Environment variable provider for koanf |
| `github.com/knadh/koanf/providers/file` | File provider for koanf |
| `github.com/knadh/koanf/parsers/json` | JSON parser for koanf |
| `golang.org/x/crypto` | bcrypt for admin Basic Auth password hashing |

### Project Structure

```
waitinglist/
├── cmd/server/main.go              # Application entry point, health-check probe mode
├── internal/
│   ├── config/config.go            # Configuration loading (koanf, JSON file + env override)
│   ├── database/postgres.go        # DB connection setup + migration runner
│   ├── handler/
│   │   ├── health.go               # GET /healthz — DB-ping liveness/readiness probe
│   │   ├── ip.go                   # Client IP extraction (X-Forwarded-For → X-Real-Ip → RemoteAddr)
│   │   ├── middleware.go           # LoggingMiddleware, JSONContentType, BasicAuthMiddleware
│   │   ├── tenant.go              # Tenant resolution middleware (X-Project-ID / Host mapping)
│   │   ├── response.go             # WriteJSON / WriteError helpers
│   │   ├── admin.go                # /admin/* JSON endpoints (dashboard, lists, grant, revoke)
│   │   ├── adminui/                # Embedded HTML/CSS/JS admin SPA (//go:embed static)
│   │   │   ├── adminui.go          # embed.FS handler with Cache-Control: no-cache
│   │   │   └── static/             # HTML/CSS/JS assets baked into binary
│   │   └── waitinglist.go          # HTTP handlers for waiting list endpoints
│   ├── logger/logger.go            # slog logger construction
│   ├── model/model.go              # Data structures, sentinel errors, DB/Tx interfaces
│   ├── notifier/
│   │   ├── notifier.go             # SMTP notifier (sends access-granted emails)
│   │   └── templates/              # Embedded HTML email templates
│   │       └── access_granted.html # Access-granted notification template
│   ├── repository/
│   │   ├── project.go              # DB operations for project table
│   │   ├── scheduler.go            # DB operations for scheduler_state table
│   │   ├── user.go                 # DB operations for user_entity table (CRUD, grant, revoke)
│   │   └── waitinglist.go          # DB operations for waiting_list table
│   └── waitlist/waitlist.go        # Background scheduler goroutine that grants access
├── migrations/
│   ├── 001_init.sql                # Initial schema (user_entity, waiting_list)
│   ├── 002_schema_improvements.sql # weight column, weighted_created_at
│   ├── 003_scheduler_state.sql     # scheduler_state table for cooldown tracking
│   ├── 004_user_created_at.sql
│   ├── 005_user_entity_ip.sql      # ip_address column on user_entity
│   ├── 006_has_access_one_way.sql  # One-way has_access trigger (dropped by 007)
│   ├── 007_access_audit_and_drop_one_way.sql  # Access audit columns; drops 006's trigger
│   └── 008_multi_tenancy.sql       # Multi-tenancy: project table, project_id columns, composite indexes
├── conf/dev.json                   # Development configuration file
├── docs/plans/                     # Feature plans
├── Dockerfile                      # Multi-stage build → distroless runtime
├── .github/workflows/docker.yml    # CI: build + push to ghcr.io on main/tags
├── Makefile
├── go.mod
└── go.sum
```

### Configuration

The application loads configuration from a JSON file passed via `--config` flag, then overlays environment variables (env vars win):

```bash
./bin/waitinglist --config conf/dev.json
```

#### Full Configuration Schema

| JSON Path | Type | Default | Description |
|---|---|---|---|
| `port` | int | `8080` | HTTP server port (binds to `0.0.0.0`) |
| `database.url` | string | `postgres://localhost:5432/waitinglist?sslmode=disable` | PostgreSQL connection URL |
| `database.username` | string | — | DB username (appended to URL if URL has no userinfo) |
| `database.password` | string | — | DB password (appended to URL if URL has no userinfo) |
| `database.migrationsDir` | string | `migrations` | Path to `.sql` migration files |
| `waitlist.entryBatchSize` | int | `25` | Max users promoted per scheduler batch |
| `waitlist.entryWindowInterval` | duration | `30h` | Min entry age for promotion + cooldown between batches |
| `schedulerInterval.disabled` | bool | `false` | Disable automatic access granting entirely |
| `schedulerInterval.waitlistCheckInterval` | duration | `1h` | How often the scheduler wakes up |
| `admin.basicAuth.username` | string | — | Admin panel username (empty = admin routes disabled) |
| `admin.basicAuth.passwordHash` | string | — | Bcrypt hash of admin password (empty = admin routes disabled) |
| `projects.headerName` | string | `X-Project-ID` | Header name for project identification |
| `projects.defaultSlug` | string | `default` | Fallback project slug when no header/host match |
| `projects.definitions.<slug>.name` | string | — | Human-readable project name |
| `projects.definitions.<slug>.hostMapping` | string | — | Hostname that resolves to this project (one per project) |
| `projects.definitions.<slug>.entryBatchSize` | int | — | Per-project override for scheduler batch size |
| `projects.definitions.<slug>.entryWindowInterval` | duration | — | Per-project override for entry window |
| `projects.definitions.<slug>.emailFrom` | string | — | Sender address for access-granted email (skip if empty) |
| `projects.definitions.<slug>.emailSubject` | string | — | Subject line for access-granted email (skip if empty) |
| `projects.definitions.<slug>.schedulerDisabled` | bool | `false` | Disable scheduler for this project |
| `smtp.host` | string | — | SMTP server hostname (empty = notifications disabled) |
| `smtp.port` | int | — | SMTP server port |
| `smtp.username` | string | — | SMTP auth username |
| `smtp.password` | string | — | SMTP auth password |
| `smtp.tls` | bool | `false` | Use implicit TLS (port 465); otherwise STARTTLS is attempted |

#### Environment Variable Override

Env vars use prefix `WL_`, flatten nested keys with `_`, uppercase everything. The mapping is implemented in `internal/config/config.go:loadEnvConfig` using koanf's env provider with a `WL_` prefix strip + `_` → `.` transform.

- `port` → `WL_PORT`
- `database.url` → `WL_DATABASE_URL`
- `database.username` → `WL_DATABASE_USERNAME`
- `database.password` → `WL_DATABASE_PASSWORD`
- `database.migrationsDir` → `WL_DATABASE_MIGRATIONSDIR`
- `waitlist.entryBatchSize` → `WL_WAITLIST_ENTRYBATCHSIZE`
- `waitlist.entryWindowInterval` → `WL_WAITLIST_ENTRYWINDOWINTERVAL`
- `schedulerInterval.disabled` → `WL_SCHEDULERINTERVAL_DISABLED`
- `schedulerInterval.waitlistCheckInterval` → `WL_SCHEDULERINTERVAL_WAITLISTCHECKINTERVAL`
- `admin.basicAuth.username` → `WL_ADMIN_BASICAUTH_USERNAME`
- `admin.basicAuth.passwordHash` → `WL_ADMIN_BASICAUTH_PASSWORDHASH`
- `projects.headerName` → `WL_PROJECTS_HEADERNAME`
- `projects.defaultSlug` → `WL_PROJECTS_DEFAULTSLUG`
- `smtp.host` → `WL_SMTP_HOST`
- `smtp.port` → `WL_SMTP_PORT`
- `smtp.username` → `WL_SMTP_USERNAME`
- `smtp.password` → `WL_SMTP_PASSWORD`
- `smtp.tls` → `WL_SMTP_TLS`

### Database

- PostgreSQL with four tables: `project`, `user_entity`, `waiting_list`, and `scheduler_state`.
- Migrations are plain `.sql` files in `migrations/`, executed in alphabetical order on startup.
- Schema uses `IF NOT EXISTS` for idempotent migrations.
- All migrations run on every startup (no migration state table) — they must be written to be re-runnable.
- Integration tests requiring a real database are gated by the `TEST_DATABASE_URL` environment variable.

### Startup Flow

1. Parse flags (`--config`, `--health-check`)
2. If `--health-check`: probe `127.0.0.1:<port>/healthz` and `os.Exit(0|1)` — no config file needed, port from `WL_PORT` env or default
3. Load configuration from JSON file + env overlay (koanf)
4. Connect to PostgreSQL (URL composed from `database.url` + optional `username`/`password`)
5. Run migrations from `migrations/` directory
6. Start background scheduler goroutine (unless `schedulerInterval.disabled`)
7. Register routes: waitinglist, health, admin (if credentials configured)
8. Start HTTP server on `0.0.0.0:<port>` with graceful shutdown on SIGINT

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
| `GET`  | `/admin/projects` | **Admin · Basic Auth.** List all projects. |
| `POST` | `/admin/projects` | **Admin · Basic Auth.** Create a project (slug, name, scheduler config). |
| `PUT`  | `/admin/projects/{id}` | **Admin · Basic Auth.** Update project name/scheduler config. |
| `GET`  | `/admin/` (and `/admin/{asset}`) | **Admin · Basic Auth.** Embedded HTML/CSS/JS admin SPA (dashboard + lists + actions). Served from `embed.FS` in `internal/handler/adminui/`. |

> **Note:** `GET /admin/dashboard`, `GET /admin/users/access`, and `GET /admin/users/waitlist` accept an optional `?project=<slug>` query parameter to scope results to a specific project.

### Scheduler Internals

The scheduler iterates all non-disabled projects on each tick, processing each independently with per-project config (batch size, window interval).

The scheduler (`internal/waitlist/waitlist.go`) runs as a background goroutine:

1. Fires immediately on startup, then on every `waitlistCheckInterval` tick.
2. Reads `scheduler_state` table for the last-success timestamp; skips if less than `entryWindowInterval` has elapsed.
3. Fetches the oldest `entryBatchSize` entries ordered by `weighted_created_at`.
4. Filters to only those older than `entryWindowInterval`.
5. Calls `GrantAccess(ctx, userIDs, "scheduler")` → sets `has_access=true`, records `access_granted_at/by`.
6. Deletes promoted entries from `waiting_list`.
7. Updates `scheduler_state` last-success timestamp.

When `schedulerInterval.disabled = true`, the goroutine is never started.

### Admin Panel

The admin routes are only registered when both `admin.basicAuth.username` and `admin.basicAuth.passwordHash` are non-empty. The password hash must be a valid bcrypt hash. Authentication uses HTTP Basic Auth with timing-safe username comparison + bcrypt password verification. The authenticated username is stored in request context (`AdminUserFromContext`) for audit logging.

The embedded SPA lives in `internal/handler/adminui/static/` and is served with `Cache-Control: no-cache`. The `JSONContentType` middleware's `Content-Type: application/json` header is explicitly cleared for static file responses so the file server can set the correct MIME type.

## Architecture Patterns

### Repository Pattern

- Each DB table has its own repository in `internal/repository/`.
- Repositories accept `*sql.DB` in their constructor and expose both standalone methods and `*Tx` variants for transactional use.
- All repositories use the `model.DBTX` interface so queries work against both `*sql.DB` and `*sql.Tx`.
- Error mapping: PostgreSQL error codes (e.g., `23505` for unique violation) are translated into sentinel errors in `model/model.go`.

### Handler Pattern

- Handlers live in `internal/handler/` and are constructed with interface dependencies (not concrete repos) for testability.
- Each handler type has a `RegisterRoutes(mux *http.ServeMux)` method.
- Handler interfaces (`WaitingListUserStore`, `WaitingListStore`, `AdminUserStore`, `AdminWaitingListStore`) are defined in the handler files, not in the repository package — this keeps the dependency direction clean.
- Handlers use `WriteJSON`/`WriteError` helpers from `response.go` for consistent JSON output.

### Transaction Boundaries

- Multi-table writes (e.g., grant access = update user + delete waitlist row) use explicit `BeginTx()` → operations → `Commit()` with `defer tx.Rollback()`.
- The `model.Tx` interface extends `model.DBTX` with `Commit()` and `Rollback()`.

### Access Grant Sources

The `access_granted_by` column is constrained to known values. The `validGrantSources` map in `repository/user.go` must stay in sync with the `CHECK` constraint in migration 007. Currently allowed: `"scheduler"`, `"admin"`.

### Client IP Extraction

`handler/ip.go:ClientIP(r)` checks headers in order: `X-Forwarded-For` (first entry) → `X-Real-Ip` → `r.RemoteAddr` (port stripped). The IP is stored in `user_entity.ip_address` at user creation time.

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
| `03-user-entity` | ✅ Complete | User entity CRUD operations |
| `04-waiting-list` | ✅ Complete | Waiting list operations |
| `05-api` | ✅ Complete | HTTP API endpoints |
| `11-ip-tracking` | ✅ Complete | Track client IP address on waiting list entry creation |
| `12-docker-build` | ✅ Complete | Multi-stage Dockerfile with distroless image and arm64/amd64 Make targets |
| `13-github-docker-workflow` | ✅ Complete | GitHub Actions workflow building and pushing Docker images to ghcr.io |
| `14-already-has-access-response` | ✅ Complete | Return HTTP 205 on re-signup when user already has access; enforce one-way `has_access` invariant in DB |
| `15-health-check` | ✅ Complete | `GET /healthz` endpoint that pings the database and returns 200/503 with a JSON status body |
| `16-access-audit-and-revocation` | ✅ Complete | Audit columns (`access_granted_at/by`, `access_revoked_at/by/reason`); drop one-way trigger; `GrantAccessTx`/`RevokeAccessTx` |
| `17-admin-api-and-auth` | ✅ Complete | `/admin/*` JSON endpoints (dashboard, list, grant, revoke, delete) protected by configurable Basic Auth |
| `18-admin-web-ui` | ✅ Complete | Embedded HTML/JS admin page with dashboard, searchable lists, and revoke/grant/delete actions |
| `19-dockerfile-healthcheck` | ✅ Complete | `HEALTHCHECK` in Dockerfile using a `--health-check` flag on the main binary (distroless-compatible) |
| `20-healthcheck-config-decouple` | ✅ Complete | Stop requiring a config file in `--health-check` mode; resolve port via `WL_PORT` env → default |
| `21-healthcheck-ipv4-loopback` | ✅ Complete | Probe `127.0.0.1` instead of `localhost` so the IPv4-bound server is reachable in distroless containers |
| `22-healthcheck-ipv4-bind` | ✅ Complete | Bind server to `0.0.0.0:port` explicitly so `127.0.0.1` health probe succeeds when `IPV6_V6ONLY=1` in distroless containers |
| `23-multi-tenancy` | ✅ Complete | Multi-tenancy: project-scoped users, waiting lists, and scheduler with tenant resolution middleware |
| `25-inline-host-mapping` | ✅ Complete | Move `hostMapping` from top-level map into per-project definitions |
| `26-smtp-notifications` | ✅ Complete | SMTP email notifications on access grant (per-project from/subject, embedded HTML template) |

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
- Handler tests use narrow interface fakes (not mocking the full repository) — define only the methods the handler actually calls.
- Repository tests that need a real DB connection use `testMain` + env gate pattern; unit tests for query building or error mapping can use interface stubs.

## Code Conventions

- **No new dependencies** without strong justification. The project deliberately avoids frameworks (no gorilla/mux, no gin, no echo). Use `net/http.ServeMux` for routing.
- **Error handling**: wrap with `fmt.Errorf("context: %w", err)` for repository/database errors; map to sentinel errors at the boundary; handlers return structured JSON error responses via `WriteError`.
- **PostgreSQL error codes**: use `github.com/lib/pq` error type assertion (`*pq.Error`) and check `.Code` — e.g., `23505` (unique_violation), `23503` (foreign_key_violation).
- **GoLand noinspection directives**: existing code uses `//goland:noinspection ALL` on raw SQL queries. Preserve these when editing repository methods.
- **Duration config fields**: stored as `time.Duration` in Go structs, represented as Go duration strings in JSON (`"30h"`, `"5m"`).
- **UUID primary keys**: all entities use UUID primary keys generated by PostgreSQL (`gen_random_uuid()`). IDs are `string` in Go.
- **Graceful shutdown**: server listens for `SIGINT`, drains with a 5-second timeout.
- **Container runtime detection**: Makefile auto-detects `container` (Podman) vs `docker` for lint and builds.

## Deployment Notes

- **Docker image**: multi-stage build from `golang:1.25` → `gcr.io/distroless/base-debian13:nonroot`. No shell in runtime image.
- **Health check in distroless**: uses `--health-check` flag on the same binary (no curl/wget available). Probes `127.0.0.1` on IPv4. Server binds `0.0.0.0` to avoid `IPV6_V6ONLY` issues.
- **CI/CD**: GitHub Actions builds and pushes to `ghcr.io/<org>/waitinglist` with `-amd64`/`-arm64` suffixes on tags and main branch pushes.
- **Migrations in container**: `migrations/` directory is `COPY`'d into the image at `/migrations/`. Config must set `database.migrationsDir` to `/migrations` (or rely on the volume mount).
- **Default Dockerfile `CMD`**: `["--config", "/config.json"]` — mount your config file at `/config.json` or override the CMD.
