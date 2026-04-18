# 07 – Schema Improvements

## Overview

Improve the database schema for correctness, performance, and long-term maintainability:

1. **Cascade user deletion** — deleting a row from `user_entity` must automatically remove the corresponding `waiting_list` entry.
2. **No cascade from waiting list to users** — deleting a `waiting_list` row must never affect `user_entity`. The foreign key should use `ON DELETE RESTRICT` (or no action) in the reverse direction; since the FK lives on `waiting_list.user_id`, this is already structurally guaranteed (there is no FK from `user_entity` pointing to `waiting_list`).
3. **Mitigate table bloat on `waiting_list`** — the table experiences frequent INSERT/DELETE cycles (users are added then removed after being granted access). Tune storage parameters to reclaim space efficiently.
4. **Add indexes** — create indexes that support the queries executed by the application.

## Requirements

| # | Requirement | Rationale |
|---|-------------|-----------|
| R1 | `ON DELETE CASCADE` on `waiting_list.user_id → user_entity.id` | Automatically clean up waiting list when a user is deleted. |
| R2 | No cascading effect from `waiting_list` deletion to `user_entity` | Deleting a waiting list entry must never remove the user. |
| R3 | Reduce table bloat on `waiting_list` | The table has a high churn rate (frequent inserts and deletes). |
| R4 | Index columns used in application queries | Improve lookup and ordering performance. |

## Design

### R1 – Cascade user deletion (already satisfied)

The current migration (`001_init.sql`) already defines:

```sql
user_id UUID NOT NULL REFERENCES user_entity(id) ON DELETE CASCADE
```

**No change required.** This is documented here for completeness.

### R2 – No cascade from waiting list to users (already satisfied)

There is no foreign key from `user_entity` pointing to `waiting_list`, so deleting a waiting list row cannot cascade to users. This is inherent in the current schema design.

**No change required.**

### R3 – Mitigate table bloat on `waiting_list`

The `waiting_list` table follows a queue-like pattern: rows are inserted and later deleted in batches. This causes dead tuples to accumulate between autovacuum runs, leading to table bloat over time.

Two complementary strategies:

1. **Lower `FILLFACTOR`** (e.g. 70) — leaves free space on each page so that HOT (Heap-Only Tuple) updates and subsequent inserts can reuse space without requiring new pages.
2. **Aggressive autovacuum settings** — trigger vacuum more frequently and allow it to do more work per cycle so dead tuples are reclaimed promptly.

```sql
ALTER TABLE waiting_list SET (
    fillfactor = 70,
    autovacuum_vacuum_scale_factor = 0.05,
    autovacuum_vacuum_threshold = 50,
    autovacuum_analyze_scale_factor = 0.05,
    autovacuum_analyze_threshold = 50
);
```

**Explanation of chosen values:**

| Parameter | Value | Default | Why |
|-----------|-------|---------|-----|
| `fillfactor` | 70 | 100 | Reserves 30% free space per page for reuse after deletes, reducing physical table growth. |
| `autovacuum_vacuum_scale_factor` | 0.05 | 0.20 | Triggers vacuum after only 5% of the table changes (vs 20%), keeping dead tuples low. |
| `autovacuum_vacuum_threshold` | 50 | 50 | Minimum number of dead tuples before vacuum considers running (kept at default). |
| `autovacuum_analyze_scale_factor` | 0.05 | 0.10 | Triggers re-analyze sooner so the query planner has up-to-date statistics. |
| `autovacuum_analyze_threshold` | 50 | 50 | Minimum tuple changes before analyze considers running (kept at default). |

### R4 – Add indexes

Analysis of application queries (from repository layer):

| Query pattern | Source | Columns involved |
|---------------|--------|------------------|
| `SELECT … FROM user_entity WHERE email = $1` | `user.go – GetByEmail` | `email` (already has UNIQUE constraint → implicit index) |
| `UPDATE user_entity SET has_access = true WHERE id = ANY($1)` | `user.go – SetHasAccess` | `id` (primary key → implicit index) |
| `INSERT INTO waiting_list … RETURNING …` | `waitinglist.go – Add` | `user_id` (UNIQUE constraint → implicit index) |
| `SELECT … FROM waiting_list ORDER BY weighted_created_at ASC` | `waitinglist.go – GetWithOffsetLimit` | `weighted_created_at` (**no index — needs one**) |
| `DELETE FROM waiting_list WHERE id = ANY($1)` | `waitinglist.go – DeleteByIDs` | `id` (primary key → implicit index) |

**Required new index:**

```sql
CREATE INDEX IF NOT EXISTS idx_waiting_list_weighted_created_at
    ON waiting_list (weighted_created_at ASC);
```

This index supports the `ORDER BY weighted_created_at ASC` query used when listing/polling the waiting list, and also benefits `LIMIT`/`OFFSET` pagination.

No additional indexes are needed — all other query columns are already covered by primary keys or unique constraints.

## Implementation Steps

All changes go into a new idempotent migration file.

- [x] **Step 1** — Analyse current schema and confirm R1/R2 are already satisfied.
- [ ] **Step 2** — Create migration file `migrations/002_schema_improvements.sql` containing:
  - `ALTER TABLE waiting_list SET (…)` for bloat mitigation (R3).
  - `CREATE INDEX IF NOT EXISTS` for the weighted_created_at column (R4).
- [ ] **Step 3** — Verify the migration is idempotent (uses `IF NOT EXISTS`, `ALTER TABLE SET` is inherently idempotent).
- [ ] **Step 4** — Run existing tests (`make test`) to ensure nothing breaks.
- [ ] **Step 5** — Integration-test the migration against a real PostgreSQL instance (gated by `TEST_DATABASE_URL`).

## Testing

### Unit / Build Tests
- Run `make test` to confirm all existing tests still pass after adding the new migration file.
- The migration runner loads `.sql` files alphabetically, so `002_schema_improvements.sql` will execute after `001_init.sql`.

### Integration Tests (require `TEST_DATABASE_URL`)
- Verify that the migration applies cleanly on a fresh database (after `001_init.sql`).
- Verify that the migration is idempotent — running it twice produces no errors.
- Verify that the index `idx_waiting_list_weighted_created_at` exists after migration.
- Verify that `ON DELETE CASCADE` still works: insert a user + waiting list entry, delete the user, confirm the waiting list entry is gone.
- Verify that deleting a waiting list entry does **not** delete the corresponding user.
- Verify that `waiting_list` storage parameters (`fillfactor`, `autovacuum_*`) are set correctly by querying `pg_class.reloptions`.

### Negative / Edge Cases
- Applying migration on an empty database (tables exist but no data) — should succeed.
- Applying migration on a database with existing data — should succeed without data loss.
- Creating the index when it already exists — should be a no-op (`IF NOT EXISTS`).

## Acceptance Criteria

- [ ] Migration file `002_schema_improvements.sql` exists and is idempotent.
- [ ] `waiting_list` table has `fillfactor = 70` and aggressive autovacuum settings.
- [ ] Index `idx_waiting_list_weighted_created_at` exists on `waiting_list.weighted_created_at`.
- [ ] Cascade delete from `user_entity` to `waiting_list` works (was already working — confirmed).
- [ ] Deleting from `waiting_list` does not affect `user_entity` (was already the case — confirmed).
- [ ] All existing tests pass.
