# 08 – Execution Tracking

## Overview

Track the timestamp of the last successful waitlist processing execution in the database. Before processing entries in `checkEntries`, verify that the configured `entryWindowInterval` has elapsed since the last successful execution. If the interval has not yet passed, skip processing and wait for the next scheduler tick.

This prevents the scheduler from granting access to new batches of users too frequently — ensuring a minimum gap of `entryWindowInterval` between successive grants, regardless of how often the ticker fires.

**Dependency:** Relies on the existing schema from plans 01–02 and 07. No changes to existing tables are required.

## Requirements

| # | Requirement | Rationale |
|---|-------------|-----------|
| R1 | Persist the timestamp of the last successful execution in the database | Survives application restarts; multiple instances can share state. |
| R2 | On each `checkEntries` invocation, check whether `entryWindowInterval` has elapsed since the last successful execution | Prevents granting access more frequently than the configured window. |
| R3 | If the interval has **not** elapsed, skip processing and return early | The next ticker tick will re-check. |
| R4 | After a successful grant (users granted access + waiting list entries deleted), update the last execution timestamp | Only successful completions advance the timestamp. |
| R5 | On first-ever run (no row in the DB), processing should proceed immediately | There is no previous execution to wait for. |

## Design

### New table: `scheduler_state`

A simple single-row key-value table to store scheduler metadata:

```sql
CREATE TABLE IF NOT EXISTS scheduler_state (
    key        VARCHAR(100) PRIMARY KEY,
    value      TIMESTAMP NOT NULL DEFAULT NOW()
);
```

The key `waitlist_last_success` will hold the timestamp of the last successful waitlist processing.

**Why a table instead of in-memory state?**
- Survives application restarts.
- Allows future multi-instance deployments to coordinate.
- Trivial to inspect and debug via SQL.

### Repository: `SchedulerRepository`

A new repository (`internal/repository/scheduler.go`) with two methods:

1. **`GetLastSuccess(ctx, key) (time.Time, error)`** — Returns the stored timestamp for the given key. Returns a zero `time.Time` and `nil` error if no row exists (R5).
2. **`UpdateLastSuccess(ctx, tx, key) error`** — Upserts the row for the given key to `NOW()`. Uses `INSERT ... ON CONFLICT ... DO UPDATE` for atomicity.

### Changes to `checkEntries` in `waitlist.go`

```
func checkEntries():
    1. Call schedulerRepo.GetLastSuccess(ctx, "waitlist_last_success")
    2. If time.Since(lastSuccess) < cfg.Waitlist.EntryWindowInterval → return (skip)
    3. Fetch entries (existing logic)
    4. Filter by WeightedCreatedAt (existing logic)
    5. Grant access + delete entries (existing logic)
    6. On success → call schedulerRepo.UpdateLastSuccess(ctx, tx, "waitlist_last_success")
```

The `UpdateLastSuccess` call should happen **after** the successful grant and delete, within the same success path. If the grant or delete fails, the timestamp is not advanced — the next tick will retry.

### Migration: `003_scheduler_state.sql`

```sql
CREATE TABLE IF NOT EXISTS scheduler_state (
    key   VARCHAR(100) PRIMARY KEY,
    value TIMESTAMP NOT NULL DEFAULT NOW()
);
```

Idempotent via `IF NOT EXISTS`.

## Implementation Steps

- [ ] **Step 1** — Create migration file `migrations/003_scheduler_state.sql` with the `scheduler_state` table.
- [ ] **Step 2** — Create `internal/repository/scheduler.go` with `SchedulerRepository`:
  - `GetLastSuccess(ctx context.Context, key string) (time.Time, error)`
  - `UpdateLastSuccess(ctx context.Context, tx model.DBTX, key string) error`
- [ ] **Step 3** — Update `internal/waitlist/waitlist.go`:
  - Accept `*repository.SchedulerRepository` as a new parameter to `Start`.
  - In `checkEntries`, call `GetLastSuccess` and skip if the interval has not elapsed.
  - After successful grant+delete, call `UpdateLastSuccess`.
- [ ] **Step 4** — Update `cmd/server/main.go` to create `SchedulerRepository` and pass it to `waitlist.Start`.
- [ ] **Step 5** — Write unit tests for `SchedulerRepository` (`internal/repository/scheduler_test.go`):
  - `GetLastSuccess` returns zero time when no row exists.
  - `GetLastSuccess` returns the stored timestamp after `UpdateLastSuccess`.
  - `UpdateLastSuccess` is idempotent (upsert).
- [ ] **Step 6** — Write unit tests for the gating logic in `waitlist.go` (`internal/waitlist/waitlist_test.go`):
  - When last success is zero (first run), processing proceeds.
  - When interval has elapsed, processing proceeds.
  - When interval has **not** elapsed, processing is skipped.
  - After successful processing, last success timestamp is updated.
- [ ] **Step 7** — Add integration tests (`internal/database/postgres_test.go`):
  - Migration creates the `scheduler_state` table.
  - Migration is idempotent.
- [ ] **Step 8** — Run `make format`, `make lint`, `make test` to verify everything passes.

## Testing

### Unit Tests

**`internal/repository/scheduler_test.go`** (requires `TEST_DATABASE_URL`):
- `GetLastSuccess` with no existing row returns zero `time.Time` and `nil` error.
- `UpdateLastSuccess` inserts a row, then `GetLastSuccess` returns a recent timestamp.
- Calling `UpdateLastSuccess` twice updates the timestamp (upsert behavior).
- `UpdateLastSuccess` with a transaction rolls back correctly on failure.

**`internal/waitlist/waitlist_test.go`** (mock-based or with test helpers):
- `checkEntries` skips processing when `entryWindowInterval` has not elapsed since last success.
- `checkEntries` proceeds when `entryWindowInterval` has elapsed.
- `checkEntries` proceeds on first run (no previous execution — zero time).
- `checkEntries` updates last success timestamp only on successful grant+delete.
- `checkEntries` does **not** update last success timestamp when grant or delete fails.

### Integration Tests

**`internal/database/postgres_test.go`**:
- Migration `003_scheduler_state.sql` creates the `scheduler_state` table.
- Migration is idempotent (running twice produces no errors).
- Full flow: insert scheduler state, read it back, verify timestamp is recent.

### Edge Cases
- First-ever application start (no `scheduler_state` row) — should process immediately.
- Application restart — should read the persisted timestamp and respect the interval.
- Multiple rapid ticker fires — only the first one within the window should process.
- Empty waiting list — no update to last success (no users were granted access).

## Acceptance Criteria

- [ ] Migration `003_scheduler_state.sql` exists and is idempotent.
- [ ] `scheduler_state` table is created with `key` (PK) and `value` (timestamp) columns.
- [ ] `SchedulerRepository` has `GetLastSuccess` and `UpdateLastSuccess` methods.
- [ ] `checkEntries` checks elapsed time since last successful execution before processing.
- [ ] If `entryWindowInterval` has not elapsed, `checkEntries` skips and returns early.
- [ ] On successful processing, the last success timestamp is updated in the database.
- [ ] First run (no row) processes immediately without error.
- [ ] All existing tests continue to pass.
- [ ] New unit and integration tests cover the core logic, edge cases, and error scenarios.
