# SQLite Support (Dual Database Backend)

## Overview

Add SQLite as an alternative database backend alongside PostgreSQL. The driver is auto-detected from the `database.url` scheme: `postgres://...` uses PostgreSQL (github.com/lib/pq), `sqlite://path/to/file.db` uses SQLite (modernc.org/sqlite). This enables both lightweight self-hosted deployments (single binary + SQLite file) and local development without a PostgreSQL instance, while preserving full PostgreSQL support for production.

## Context (from discovery)

- **Files/components involved:**
  - `internal/database/postgres.go` — connection + migration runner
  - `internal/repository/user.go` — 12+ queries with PG-specific syntax ($N placeholders, pq.Array, ILIKE, to_char/date_trunc, interval arithmetic, NULLS LAST)
  - `internal/repository/waitinglist.go` — 10+ queries using pq.Array, ANY(), RETURNING, pq.Error codes
  - `internal/repository/scheduler.go` — ON CONFLICT ... DO UPDATE, NOW()
  - `internal/model/model.go` — DBTX/Tx interfaces (driver-agnostic, no changes needed)
  - `migrations/001_init.sql` — PG-specific DDL (gen_random_uuid, inet, generated columns with interval, storage parameters, CHECK constraints with ARRAY literals)
  - `cmd/server/main.go` — DB connection initialization, repo construction
  - `Dockerfile` — no changes needed (modernc.org/sqlite is pure Go, no CGO)

- **PostgreSQL-specific features requiring SQLite translation:**
  - `$1, $2, ...` → `?` placeholders
  - `pq.Array(ids)` + `ANY($1)` → `IN (?, ?, ...)` with expanded args
  - `ILIKE '%' || $1 || '%'` → `LIKE '%' || ? || '%'` (SQLite LIKE is case-insensitive for ASCII)
  - `gen_random_uuid()` → Go-generated UUID (github.com/google/uuid)
  - `NOW()` / `NOW() AT TIME ZONE 'UTC'` → `datetime('now')`
  - `to_char(date_trunc('day', created_at), 'YYYY-MM-DD')` → `strftime('%Y-%m-%d', created_at)`
  - `($2 || ' days')::interval` → `datetime('now', '-' || ? || ' days')`
  - `ON CONFLICT (cols) DO UPDATE SET ...` → same syntax (SQLite supports it)
  - `RETURNING id, ...` → same syntax (SQLite 3.35+, modernc supports it)
  - `generated always as (expr) stored` → SQLite generated columns (3.31+) with different expression syntax
  - `inet` type → `TEXT` (just store IP as string)
  - `timestamp with time zone` → `TEXT` (ISO 8601 strings)
  - `NULLS LAST` → `CASE WHEN col IS NULL THEN 1 ELSE 0 END, col`
  - Storage parameters (`WITH (fillfactor=...)`) → not applicable, omit
  - pq.Error code checking → sqlite error checking (constraint violation detection)

- **Dependencies to add:**
  - `modernc.org/sqlite` — pure-Go SQLite driver
  - `github.com/google/uuid` — UUID generation for SQLite (PG uses gen_random_uuid())

## Development Approach

- **Testing approach**: TDD (tests first)
- Complete each task fully before moving to the next
- Make small, focused changes
- **CRITICAL: every task MUST include new/updated tests** for code changes in that task
- **CRITICAL: all tests must pass before starting next task**
- **CRITICAL: update this plan file when scope changes during implementation**
- Run `make format`, `make lint`, and `make test` after each change

## Testing Strategy

- **Unit tests**: Required for every task — interface-based tests that verify both backends produce identical behavior
- **Integration tests**: SQLite tests run without external dependencies (in-memory `:memory:` or temp file). PostgreSQL tests gated by `TEST_DATABASE_URL` as before.
- **Shared test suites**: Write repository tests against the interfaces so the same test logic validates both implementations

## Progress Tracking

- Mark completed items with `[x]` immediately when done
- Add newly discovered tasks with ➕ prefix
- Document issues/blockers with ⚠️ prefix
- Update plan if implementation deviates from original scope

## Implementation Steps

### Task 1: Define repository interfaces and database factory

Extract explicit repository interfaces from the handler-defined interfaces into a shared location, and create the database factory that auto-detects the driver from the URL scheme.

- [x] Create `internal/database/database.go` with a `New(databaseURL string) (*sql.DB, Driver, error)` factory function that parses the URL scheme (`postgres://` → postgres driver, `sqlite://` → sqlite driver) and returns the opened connection plus a `Driver` enum (`DriverPostgres`, `DriverSQLite`)
- [x] Define `Driver` type (string or int) in `internal/database/database.go`
- [x] For SQLite URLs, parse `sqlite:///path/to/file.db` to extract the file path and open with modernc.org/sqlite driver
- [x] For SQLite, enable WAL mode and foreign keys (`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON`) on connection open
- [x] Keep `internal/database/postgres.go` working as-is (the factory calls into it for postgres URLs)
- [x] Create `internal/database/sqlite.go` with `NewSQLiteDB(path string) (*sql.DB, error)`
- [x] Write tests for factory URL parsing (postgres://, sqlite://, invalid schemes)
- [x] Write tests for SQLite connection open (temp file, :memory:)
- [x] Run `make format && make lint && make test` — must pass before next task

### Task 2: Create SQLite migration schema

Write the equivalent DDL for SQLite that produces the same logical schema as `migrations/001_init.sql`.

- [x] Create `migrations/sqlite/001_init.sql` with SQLite-compatible DDL
- [x] Move existing PostgreSQL migrations to `migrations/postgres/001_init.sql`
- [x] UUID primary keys: use `TEXT PRIMARY KEY` with no server-side default (UUID generated in Go)
- [x] Replace `gen_random_uuid()` defaults — SQLite won't auto-generate; handled in repository layer
- [x] Replace `timestamp default now()` → `TEXT DEFAULT (datetime('now'))`
- [x] Replace `inet` → `TEXT`
- [x] Replace `timestamp with time zone` → `TEXT`
- [x] Replace generated column `weighted_created_at` with SQLite expression: `(datetime(created_at, '-' || (weight * 3600) || ' seconds'))`
- [x] Remove `WITH (fillfactor=...)` storage parameters
- [x] Replace `ARRAY['scheduler'::text, 'admin'::text]` CHECK constraint with `CHECK (access_granted_by IN ('scheduler', 'admin'))`
- [x] Preserve all indexes (CREATE INDEX IF NOT EXISTS syntax works in SQLite)
- [x] Preserve foreign key with ON DELETE CASCADE
- [x] Update `internal/database/database.go` or migration runner to select the right migrations directory based on driver
- [x] Write test: run SQLite migrations on a fresh :memory: DB and verify all tables exist with correct columns
- [x] Write test: verify PostgreSQL migrations still work (gated by TEST_DATABASE_URL)
- [x] Run `make format && make lint && make test` — must pass before next task

### Task 3: Implement SQLite UserRepository

Create the SQLite implementation of user repository operations with SQLite-compatible SQL.

- [x] Create `internal/repository/sqlite/` package directory
- [x] Create `internal/repository/sqlite/user.go` implementing all UserRepository methods
- [x] Replace `$1, $2` placeholders with `?` throughout
- [x] Replace `pq.Array(ids)` + `ANY($2)` with dynamically built `IN (?, ?, ...)` clauses
- [x] Replace `ILIKE '%' || $1 || '%'` with `LIKE '%' || ? || '%'`
- [x] Replace `to_char(date_trunc('day', created_at), 'YYYY-MM-DD')` with `strftime('%Y-%m-%d', created_at)`
- [x] Replace `(NOW() AT TIME ZONE 'UTC') - ($1 || ' days')::interval` with `strftime('%Y-%m-%dT%H:%M:%SZ', 'now', '-N days')`
- [x] Replace `NULLS LAST` with `CASE WHEN col IS NULL THEN 1 ELSE 0 END, col DESC`
- [x] Generate UUID in Go (github.com/google/uuid) for Create method instead of relying on RETURNING gen_random_uuid()
- [x] Handle constraint violation errors using SQLite error codes instead of pq.Error (unique constraint = SQLITE_CONSTRAINT_UNIQUE)
- [x] Replace `NOW()` in UPDATE queries with `strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`
- [x] Write interface-conformance tests: Create, GetByEmail, GetByID, GetByIDs, GetUserInfoByEmails
- [x] Write interface-conformance tests: GrantAccessTx, RevokeAccessTx, CountByAccess
- [x] Write interface-conformance tests: EnlistmentsByDay, ListWithAccess, ListAllWithAccess, GetGrantedSince
- [x] Write error-case tests: duplicate email, user not found, invalid grant source
- [x] Run `make format && make lint && make test` — must pass before next task

### Task 4: Implement SQLite WaitingListRepository

Create the SQLite implementation of waiting list repository operations.

- [x] Create `internal/repository/sqlite/waitinglist.go` implementing all WaitingListRepository methods
- [x] Replace `$1, $2` placeholders with `?`
- [x] Replace `pq.Array(ids)` + `ANY($1)` in DeleteByIDs with dynamically built `IN (?, ?, ...)`
- [x] Handle constraint violation errors (23505 → UNIQUE, 23503 → FOREIGN KEY) using SQLite error types
- [x] Verify `RETURNING` works with modernc.org/sqlite (SQLite 3.35+)
- [x] Replace `ILIKE` with `LIKE` in ListJoined
- [x] Write interface-conformance tests: Add, GetAll, GetWithOffsetLimit, GetEnlistedSince
- [x] Write interface-conformance tests: ListAllJoined, ListJoined, DeleteByIDs, DeleteByID, DeleteByUserID
- [x] Write error-case tests: already on waitlist, foreign key violation, entry not found
- [x] Write BeginTx test verifying transaction commit/rollback semantics
- [x] Run `make format && make lint && make test` — must pass before next task

### Task 5: Implement SQLite SchedulerRepository

Create the SQLite implementation of scheduler state operations.

- [x] Create `internal/repository/sqlite/scheduler.go` implementing SchedulerRepository methods
- [x] Replace `$1, $2` with `?` placeholders
- [x] Replace `NOW()` with `datetime('now')` in the upsert query
- [x] Verify `ON CONFLICT ... DO UPDATE` works identically in SQLite
- [x] Write interface-conformance tests: GetLastSuccess (no rows, existing row)
- [x] Write interface-conformance tests: UpdateLastSuccess (insert, upsert)
- [x] Run `make format && make lint && make test` — must pass before next task

### Task 6: Rename PostgreSQL repository to namespaced package

Move the existing PostgreSQL repository implementations into `internal/repository/postgres/` to mirror the sqlite package structure.

- [ ] Create `internal/repository/postgres/` package directory
- [ ] Move user.go → `internal/repository/postgres/user.go` (update package declaration)
- [ ] Move waitinglist.go → `internal/repository/postgres/waitinglist.go`
- [ ] Move scheduler.go → `internal/repository/postgres/scheduler.go`
- [ ] Update all imports in `cmd/server/main.go` and any other files referencing the old `repository` package
- [ ] Keep `internal/repository/` as a package that holds shared interfaces or can re-export for backward compatibility (if needed by handler interfaces)
- [ ] Write no new tests — just verify existing tests pass with the relocated code
- [ ] Run `make format && make lint && make test` — must pass before next task

### Task 7: Wire up dual-backend in main.go

Update `cmd/server/main.go` to use the database factory and construct the appropriate repository implementations based on the detected driver.

- [ ] Replace direct `database.NewPostgresDB(...)` call with `database.New(databaseURL)` factory
- [ ] Based on returned `Driver`, construct either `postgres.NewUserRepository(db)` or `sqlite.NewUserRepository(db)` (same for waitlist and scheduler repos)
- [ ] For SQLite URLs, skip the `url.UserPassword` handling (no username/password in SQLite URLs)
- [ ] Select migrations directory based on driver: `cfg.Database.MigrationsDir + "/postgres"` or `+ "/sqlite"`
- [ ] Update health handler: SQLite health check should use `db.Ping()` (same as postgres, already works)
- [ ] Write integration test: start with SQLite URL, verify the full startup flow works (migrations + repos)
- [ ] Run `make format && make lint && make test` — must pass before next task

### Task 8: Shared repository interface tests (TDD parity validation)

Write a shared test suite that runs the same behavioral tests against both backends to ensure feature parity.

- [ ] Create `internal/repository/repotest/` package with shared test helpers
- [ ] Define test setup functions that create a fresh DB (SQLite :memory: or PG with TEST_DATABASE_URL) and run migrations
- [ ] Write shared test functions that accept interfaces (not concrete types) and verify identical behavior
- [ ] Create `internal/repository/postgres/user_test.go` that calls shared tests with PG backend (gated by TEST_DATABASE_URL)
- [ ] Create `internal/repository/sqlite/user_test.go` that calls shared tests with SQLite backend
- [ ] Do the same for waitinglist and scheduler repos
- [ ] Verify all parity: same inputs → same outputs/errors for both backends
- [ ] Run `make format && make lint && make test` — must pass before next task

### Task 9: Update configuration and documentation

Update config handling to document the URL-based detection and ensure SQLite paths work with the configuration system.

- [ ] Update `conf/dev.json` to show both PostgreSQL and SQLite examples (commented out or as a note)
- [ ] Verify `WL_DATABASE_URL=sqlite:///tmp/waitinglist.db` works with env override
- [ ] Update README.md: add SQLite section to Configuration Reference
- [ ] Update README.md: document the URL scheme detection (`postgres://` vs `sqlite://`)
- [ ] Update README.md: add note about SQLite limitations (single-writer, no concurrent access across processes)
- [ ] Add new dependencies to the External Dependencies table in CLAUDE.md
- [ ] Run `make format && make lint && make test` — must pass before next task

### Task 10: Verify acceptance criteria

- [ ] Verify: `database.url = "postgres://..."` uses PostgreSQL with lib/pq (no behavior change)
- [ ] Verify: `database.url = "sqlite:///path/to/db.sqlite"` uses SQLite with modernc.org/sqlite
- [ ] Verify: migrations run correctly on both backends (fresh DB)
- [ ] Verify: all CRUD operations produce identical results on both backends
- [ ] Verify: scheduler works correctly with SQLite (upsert, timestamp comparison)
- [ ] Verify: constraint violations map to the same sentinel errors on both backends
- [ ] Verify: UUID generation works on SQLite (Go-generated UUIDs stored as TEXT)
- [ ] Verify: weighted_created_at ordering works correctly on SQLite
- [ ] Run full test suite (`make test`)
- [ ] Run linter (`make lint`) — all issues must be fixed
- [ ] Run format check (`make format`)

### Task 11: [Final] Update documentation

- [ ] Update CLAUDE.md project structure section to reflect new directory layout
- [ ] Update CLAUDE.md External Dependencies table with new deps
- [ ] Update CLAUDE.md Configuration section with sqlite URL format
- [ ] Verify README.md is complete and accurate

## Technical Details

### URL Scheme Detection

```
postgres://user:pass@host:5432/dbname?sslmode=disable  → PostgreSQL (lib/pq)
sqlite:///absolute/path/to/file.db                     → SQLite (modernc.org/sqlite)
sqlite://relative/path.db                              → SQLite (relative path)
sqlite://:memory:                                      → SQLite (in-memory, for testing)
```

### SQLite Connection Setup

```go
db, err := sql.Open("sqlite", filepath)
db.Exec("PRAGMA journal_mode=WAL")
db.Exec("PRAGMA foreign_keys=ON")
db.Exec("PRAGMA busy_timeout=5000")
db.SetMaxOpenConns(1) // SQLite doesn't support concurrent writes
```

### UUID Generation Strategy

- PostgreSQL: `gen_random_uuid()` in DEFAULT clause (server-side)
- SQLite: Generate in Go using `github.com/google/uuid` before INSERT, pass as parameter

### Timestamp Handling

- PostgreSQL: `timestamp with time zone` stored natively, `NOW()` for current time
- SQLite: `TEXT` columns storing ISO 8601 UTC strings (`2024-01-15T10:30:00Z`), `datetime('now')` for current time
- Go scanning: both produce `time.Time` values when scanned with database/sql

### Error Code Mapping

| PostgreSQL (pq.Error.Code) | SQLite Equivalent | Sentinel Error |
|---|---|---|
| 23505 (unique_violation) | SQLITE_CONSTRAINT_UNIQUE (2067) | ErrDuplicateEmail / ErrAlreadyOnWaitingList |
| 23503 (foreign_key_violation) | SQLITE_CONSTRAINT_FOREIGNKEY (787) | ErrWaitingListForeignKey |

### Directory Structure After Implementation

```
internal/
├── database/
│   ├── database.go          # Factory: New() → *sql.DB + Driver
│   ├── postgres.go          # NewPostgresDB()
│   └── sqlite.go            # NewSQLiteDB()
├── repository/
│   ├── postgres/
│   │   ├── user.go
│   │   ├── waitinglist.go
│   │   └── scheduler.go
│   ├── sqlite/
│   │   ├── user.go
│   │   ├── waitinglist.go
│   │   └── scheduler.go
│   └── repotest/            # Shared interface tests
│       └── ...
migrations/
├── postgres/
│   └── 001_init.sql
└── sqlite/
    └── 001_init.sql
```

## Post-Completion

**Manual verification:**
- Test with a real SQLite file: start server with `sqlite:///tmp/test.db`, exercise all endpoints via curl/admin UI
- Verify WAL mode enables concurrent readers during writes
- Verify generated column ordering matches PostgreSQL behavior
- Test Docker image still builds and runs with SQLite (no CGO needed with modernc.org/sqlite)

**Performance considerations:**
- SQLite max concurrent connections set to 1 for writes (WAL allows concurrent reads)
- For high-traffic deployments, PostgreSQL remains recommended
- SQLite is best for single-instance deployments, development, and testing
