# Database Plan

## Overview

Design and implement the PostgreSQL schema for the waiting list service. The database consists of two tables: `user_entity` (stores user data) and `waiting_list` (tracks when users joined the waiting list).

## Requirements

- Two tables: `user_entity` and `waiting_list`
- `user_entity` stores: `id`, `firstname`, `lastname`, `email`, and a boolean `has_access`
- `waiting_list` stores: `user_id` (foreign key to `user_entity`) and `created_at` (datetime)
- Email must be unique in `user_entity`
- Referential integrity between `waiting_list` and `user_entity`

## Design

### Schema

```sql
CREATE TABLE IF NOT EXISTS user_entity (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    firstname  VARCHAR(255) NOT NULL,
    lastname   VARCHAR(255) NOT NULL,
    email      VARCHAR(255) NOT NULL UNIQUE,
    has_access BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS waiting_list (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES user_entity(id) ON DELETE CASCADE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(user_id)
);
```

### Key Decisions

- **UUID primary keys**: Avoids sequential ID exposure and is suitable for distributed systems.
- **`gen_random_uuid()`**: Uses PostgreSQL's built-in UUID generation (available in PG 13+).
- **`has_access` default `FALSE`**: New users start without access; access is granted later.
- **`waiting_list.user_id` is UNIQUE**: A user can only appear once on the waiting list.
- **`ON DELETE CASCADE`**: Removing a user entity automatically removes their waiting list entry.
- **`TIMESTAMP WITH TIME ZONE`**: Stores the exact moment a user was added to the waiting list.

### Indexes

The `UNIQUE` constraint on `email` and `user_id` automatically creates indexes. No additional indexes are needed initially.

## Implementation Steps

- [ ] Create `migrations/001_init.sql` with the schema above
- [ ] Implement migration runner in `internal/database/postgres.go` that reads and executes SQL files on startup
- [ ] Verify schema creation against a local PostgreSQL instance

## Testing

### Unit Tests — Migration Runner (`internal/database/`)

- **Core logic**:
  - Test that the migration runner executes SQL files and creates both `user_entity` and `waiting_list` tables.
  - Test that the `user_entity` table has the expected columns (`id`, `firstname`, `lastname`, `email`, `has_access`) with correct types.
  - Test that the `waiting_list` table has the expected columns (`id`, `user_id`, `created_at`) with correct types.
- **Edge cases**:
  - Test that running migrations multiple times is idempotent (no errors on re-run due to `IF NOT EXISTS`).
  - Test that `has_access` defaults to `FALSE` when not specified.
  - Test that `created_at` defaults to the current timestamp when not specified.
- **Error/negative scenarios**:
  - Test that inserting a duplicate `email` into `user_entity` fails with a unique constraint violation.
  - Test that inserting a `waiting_list` entry with a non-existent `user_id` fails with a foreign key violation.
  - Test that inserting a duplicate `user_id` into `waiting_list` fails with a unique constraint violation.
  - Test that `ON DELETE CASCADE` removes the `waiting_list` entry when the referenced `user_entity` is deleted.

## Acceptance Criteria

- Both tables are created successfully when the migration runs
- `user_entity.email` has a unique constraint
- `waiting_list.user_id` references `user_entity.id` with cascade delete
- `waiting_list.user_id` has a unique constraint (one entry per user)
- Running migrations multiple times is idempotent (`IF NOT EXISTS`)

## Dependencies

- [Project Setup](../01-project-setup/plan.md) — database connection must be established first
