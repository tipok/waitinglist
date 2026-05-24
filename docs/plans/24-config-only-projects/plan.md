# 24 — Config-Only Projects (Eliminate `project` Table)

> **Status:** ✅ Complete

## Overview

Remove the `project` database table entirely. Projects are defined solely in the JSON config file (or env vars). The `project_id` UUID columns on `user_entity`, `waiting_list`, and `scheduler_state` are replaced with `project_slug TEXT` columns. No foreign key constraints, no `ProjectRepository`.

This simplifies project onboarding to a single config change — no SQL needed.

### Key Design Decisions

| Decision | Rationale |
|---|---|
| Eliminate `project` table | Projects are static, config-defined entities. The DB table adds operational overhead (manual INSERT required to onboard) without providing meaningful runtime value. |
| `project_slug TEXT` replaces `project_id UUID` | Slug is the natural key used everywhere in config and API; storing it directly avoids a join/lookup to resolve UUID → slug. |
| No FK constraint on slug | Projects are validated at startup from config. Runtime integrity is enforced by the application (resolver rejects unknown slugs before queries execute). |
| Per-project scheduler config in `definitions` | Replaces the DB-stored per-project overrides. Same capability, config-driven. |
| `hostMapping` stays `map[string]string` | Minimal breaking change. Values reference slugs defined in `definitions`. |
| Startup validation | All slugs referenced in `hostMapping` values and `defaultSlug` must exist as keys in `definitions`. Fail-fast on misconfiguration. |

### Dependencies

- Supersedes plan 23 (multi-tenancy) — same feature, different storage strategy.
- Requires migration 009 to transform existing data.
- No new Go module dependencies.

---

## Requirements

### Configuration

1. New `definitions` map in `ProjectsConfig`: keys are slugs, values are project metadata (name + optional scheduler overrides).
2. `hostMapping` remains `map[string]string` (host → slug).
3. `defaultSlug` must reference a key in `definitions`.
4. Every value in `hostMapping` must reference a key in `definitions`.
5. Application must fail to start if validation fails (missing slug references).

### Database

1. New migration `009_drop_project_table.sql`:
   - Add `project_slug TEXT` columns to `user_entity`, `waiting_list`, `scheduler_state`.
   - Backfill from `project.slug` via JOIN.
   - Set `NOT NULL`.
   - Drop old `project_id` columns (and their FK constraints).
   - Recreate composite unique constraint `(project_slug, email)` on `user_entity`.
   - Recreate composite PK `(project_slug, key)` on `scheduler_state`.
   - Recreate indexes using `project_slug`.
   - Drop `project` table.
2. Migration must be idempotent (use `IF EXISTS` / `IF NOT EXISTS` guards).

### Application

1. `model.Project` becomes config-derived: `Slug`, `Name`, scheduler fields. No `ID` or `CreatedAt`.
2. `model.UserEntity`, `WaitingListEntry`, etc.: `ProjectID string` → `ProjectSlug string`.
3. `ProjectRepository` is deleted.
4. `ProjectResolver` is built from config `definitions` (no DB fetch at startup).
5. All repository queries use `project_slug` TEXT instead of `project_id` UUID.
6. Scheduler iterates config-defined projects, respects per-project overrides.
7. Admin endpoints filter by `?project=<slug>` against the text column.

---

## Design

### Config Schema

```go
type ProjectsConfig struct {
    HeaderName  string                       `koanf:"headerName"`
    DefaultSlug string                       `koanf:"defaultSlug"`
    HostMapping map[string]string            `koanf:"hostMapping"`
    Definitions map[string]ProjectDefinition `koanf:"definitions"`
}

type ProjectDefinition struct {
    Name                string `koanf:"name"`
    EntryBatchSize      *int   `koanf:"entryBatchSize"`
    EntryWindowInterval string `koanf:"entryWindowInterval"`
    SchedulerDisabled   bool   `koanf:"schedulerDisabled"`
}
```

### Example Config

```json
{
  "projects": {
    "headerName": "X-Project-ID",
    "defaultSlug": "default",
    "hostMapping": {
      "beta.localhost": "beta-app",
      "tools.localhost": "internal-tools"
    },
    "definitions": {
      "default": {
        "name": "Default"
      },
      "beta-app": {
        "name": "Beta App",
        "entryBatchSize": 10,
        "entryWindowInterval": "24h"
      },
      "internal-tools": {
        "name": "Internal Tools",
        "schedulerDisabled": true
      }
    }
  }
}
```

### Migration 009

```sql
-- Add project_slug columns
ALTER TABLE user_entity ADD COLUMN IF NOT EXISTS project_slug TEXT;
ALTER TABLE waiting_list ADD COLUMN IF NOT EXISTS project_slug TEXT;
ALTER TABLE scheduler_state ADD COLUMN IF NOT EXISTS project_slug TEXT;

-- Backfill from project table
UPDATE user_entity SET project_slug = p.slug FROM project p WHERE user_entity.project_id = p.id;
UPDATE waiting_list SET project_slug = p.slug FROM project p WHERE waiting_list.project_id = p.id;
UPDATE scheduler_state SET project_slug = p.slug FROM project p WHERE scheduler_state.project_id = p.id;

-- Set NOT NULL
ALTER TABLE user_entity ALTER COLUMN project_slug SET NOT NULL;
ALTER TABLE waiting_list ALTER COLUMN project_slug SET NOT NULL;
ALTER TABLE scheduler_state ALTER COLUMN project_slug SET NOT NULL;

-- Drop old FK columns
ALTER TABLE user_entity DROP COLUMN IF EXISTS project_id;
ALTER TABLE waiting_list DROP COLUMN IF EXISTS project_id;
ALTER TABLE scheduler_state DROP COLUMN IF EXISTS project_id;

-- Recreate constraints and indexes
-- user_entity: unique (project_slug, email)
DROP INDEX IF EXISTS idx_user_entity_project_email;
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_entity_project_slug_email ON user_entity (project_slug, email);

-- scheduler_state: composite PK on (project_slug, key)
-- (Requires rebuilding the PK)
ALTER TABLE scheduler_state DROP CONSTRAINT IF EXISTS scheduler_state_pkey;
ALTER TABLE scheduler_state ADD PRIMARY KEY (project_slug, key);

-- Performance indexes
DROP INDEX IF EXISTS idx_waiting_list_project_weighted;
CREATE INDEX IF NOT EXISTS idx_waiting_list_project_slug_weighted ON waiting_list (project_slug, weighted_created_at);

DROP INDEX IF EXISTS idx_user_entity_project_access;
CREATE INDEX IF NOT EXISTS idx_user_entity_project_slug_access ON user_entity (project_slug, has_access);

-- Drop project table
DROP TABLE IF EXISTS project;
```

### Model Changes

```go
// Project is derived from config, not DB.
type Project struct {
    Slug                string
    Name                string
    EntryBatchSize      *int
    EntryWindowInterval *Duration
    SchedulerDisabled   bool
}

// UserEntity — ProjectID becomes ProjectSlug
type UserEntity struct {
    ID          string  `json:"id"`
    ProjectSlug string  `json:"project_slug"`
    // ... rest unchanged
}
```

### Startup Flow (Revised)

1. Parse flags (`--config`, `--health-check`)
2. If `--health-check`: probe and exit (unchanged)
3. Load config from JSON + env overlay
4. **Validate projects config** (all slug references resolve to `definitions` keys)
5. Build `[]model.Project` from `definitions` map
6. Connect to PostgreSQL, run migrations
7. Build `ProjectResolver` from config (no DB fetch)
8. Start scheduler (iterates config-defined projects)
9. Register routes, start HTTP server

---

## Implementation Steps

### Phase 1: Config and Model (no DB change yet)

- [x] Add `ProjectDefinition` struct to `config.go`.
- [x] Add `Definitions map[string]ProjectDefinition` to `ProjectsConfig`.
- [x] Add startup validation function: check `defaultSlug` ∈ `definitions`, all `hostMapping` values ∈ `definitions`.
- [x] Update `model.Project`: remove `ID`, `CreatedAt`; keep `Slug`, `Name`, scheduler fields.
- [x] Update `model.UserEntity`: `ProjectID` → `ProjectSlug`.
- [x] Update `model.WaitingListEntry`: `ProjectID` → `ProjectSlug`.
- [x] Update `model.WaitingListAdminRow`: `ProjectID` → `ProjectSlug`.
- [x] Verify: `make format && make lint` (tests will break — expected).

### Phase 2: Migration

- [x] Write `migrations/009_drop_project_table.sql` as designed above.
- [x] Verify: migration is idempotent (can be re-run on a fresh DB after 008).

### Phase 3: Repository Layer

- [x] Delete `internal/repository/project.go` and its test file.
- [x] Update `internal/repository/user.go`: all queries use `project_slug` TEXT column.
- [x] Update `internal/repository/waitinglist.go`: all queries use `project_slug` TEXT column.
- [x] Update `internal/repository/scheduler.go`: all queries use `project_slug` TEXT column.
- [x] Verify: `make format && make lint`.

### Phase 4: Handler and Resolver

- [x] Update `internal/handler/tenant.go`: build from `[]model.Project` derived from config (remove `projectRepo` dependency).
- [x] Update `internal/handler/waitinglist.go`: pass `project.Slug` instead of `project.ID` to repo methods.
- [x] Update `internal/handler/admin.go`: remove `Reload()` calls; project filter uses slug directly.
- [x] Update `internal/waitlist/waitlist.go`: scheduler iterates config projects, uses slug, reads per-project overrides.
- [x] Verify: `make format && make lint`.

### Phase 5: Wiring (main.go)

- [x] Remove `ProjectRepository` instantiation and `GetAll()` call.
- [x] Build `[]model.Project` from `cfg.Projects.Definitions`.
- [x] Pass to `NewProjectResolver`.
- [x] Pass per-project config to scheduler.
- [x] Remove startup validation that checked DB projects against `defaultSlug`.
- [x] Add new startup validation from config (Phase 1 function).
- [x] Verify: `make format && make lint`.

### Phase 6: Tests

- [x] Update/delete `internal/repository/project_test.go`.
- [x] Update `internal/repository/user_test.go`: use `project_slug` in test setup.
- [x] Update `internal/repository/waitinglist_test.go`: use `project_slug` in test setup.
- [x] Update `internal/repository/scheduler_test.go`: use `project_slug` in test setup.
- [x] Update `internal/handler/*_test.go`: adapt fakes/mocks to new signatures.
- [x] Update `internal/waitlist/waitlist_test.go` if applicable.
- [x] Add config validation tests.
- [x] Verify: `make format && make lint && make test`.

### Phase 7: Documentation

- [x] Update `README.md`: document `projects.definitions` field, update config example.
- [x] Update `CLAUDE.md`: reflect new project structure (no `ProjectRepository`, slug-based).
- [x] Mark plan 23 as superseded.

---

## Testing

### Unit Tests

| Test | Description |
|------|-------------|
| Config validation: valid | All slugs resolve → no error |
| Config validation: missing default | `defaultSlug` not in `definitions` → error |
| Config validation: missing hostMapping target | `hostMapping` value not in `definitions` → error |
| Config validation: empty definitions | Error if no projects defined |
| Repository: user CRUD with project_slug | Create, get-by-email, get-by-id all scope by slug |
| Repository: email uniqueness per slug | Same email in different slugs → OK; same email in same slug → conflict |
| Repository: waitlist operations with slug | Add, list, delete all scope by slug |
| Repository: scheduler state with slug | Read/write last-success per project slug |
| Handler: resolver picks correct project | Header > host mapping > default slug > 400 |
| Handler: unknown slug returns 400 | Request for slug not in config → rejected |
| Scheduler: per-project overrides | Respects `schedulerDisabled`, `entryBatchSize`, `entryWindowInterval` from config |

### Integration Tests (gated by `TEST_DATABASE_URL`)

| Test | Description |
|------|-------------|
| Migration 009 on existing data | Rows with old `project_id` are correctly migrated to `project_slug` |
| Full flow: two projects | Create entries in separate projects, verify isolation end-to-end |

---

## Acceptance Criteria

- [x] No `project` table exists after migration 009.
- [x] Adding a new project requires only a config change + restart.
- [x] Existing data is preserved (migrated from UUID to slug).
- [x] Per-project scheduler overrides work from config.
- [x] Unknown slugs are rejected at request time with 400.
- [x] `make format && make lint && make test` all pass.
