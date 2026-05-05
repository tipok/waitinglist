# 16 — Access Audit Columns & Revocation Support

## Overview

The upcoming admin page (plans [17](../17-admin-api-and-auth/plan.md) and [18](../18-admin-web-ui/plan.md)) needs to:

- **Show** when each user enlisted on the waiting list, when they were granted access, and **how** the grant happened (scheduler vs. admin override).
- **Allow an admin to revoke access** with a textual reason that is later surfaced both in the admin list view and through the public lookup endpoint (`GET /waitinglist/users`).
- **Allow an admin to grant access immediately**, bypassing the scheduler, and remove the corresponding waiting-list row.

This plan introduces the schema, repository, and audit plumbing that the API/UI plans depend on. **Most importantly, it resolves a hard conflict between the existing one-way `has_access` invariant ([plan 14](../14-already-has-access-response/plan.md)) and the new revocation requirement.**

### ⚠️ Direct conflict with plan 14

Plan 14 (committed in `90dbec8`, `155b11f`) installed `migration 006_has_access_one_way.sql`, a `BEFORE UPDATE` trigger that **rejects any UPDATE flipping `has_access` from `TRUE` to `FALSE`**. Admin-driven revocation requires exactly that transition, so the trigger and the new feature are mutually exclusive.

**Recommended resolution (default in this plan):** drop the trigger in a new migration `007_drop_has_access_one_way.sql`. The audit columns introduced here (`access_revoked_at`, `access_revoke_reason`, `access_revoked_by`) describe the lifecycle properly and replace the value the trigger was protecting (a bare boolean cannot capture "user lost access *and why*"). Anchor the original invariant at the application layer instead: only the dedicated repository method `RevokeAccessTx` may set `has_access = false`, and it requires a non-empty reason.

**Alternative considered:** keep `has_access` as a one-way flag and add a separate `access_revoked` flag, where the API treats a user as "currently has access" iff `has_access AND NOT access_revoked`. Cleaner from an "audit-trail-never-loses-data" standpoint, but it complicates every existing query that reads `has_access` and surprises future readers. Flagged as **Open Question 1** for review.

### Key Design Decisions

| Decision | Rationale |
|---|---|
| Drop the one-way trigger (migration 007) | Required for revocation. The application enforces "only `RevokeAccessTx` may set false" instead. |
| Add `access_granted_at`, `access_granted_by`, `access_revoked_at`, `access_revoked_by`, `access_revoke_reason` columns to `user_entity` | Single source of truth co-located with the boolean; avoids cross-table joins on every admin row render. |
| `access_granted_by` is a `TEXT` column with a `CHECK (access_granted_by IN ('scheduler','admin'))` constraint | Two known sources today; constraint is cheap to extend. Avoids a Postgres ENUM (which is painful to evolve). |
| Backfill `access_granted_at = created_at` and `access_granted_by = 'scheduler'` for users where `has_access = TRUE` at migration time | Best-effort retrofit. We do not have an audit trail for past grants, so attributing them to the scheduler matches the only path that existed. Flagged as **Open Question 2**. |
| Surface `access_revoke_reason` (and `access_revoked_at`) in `UserInfo` returned by `GET /waitinglist/users` | The user explicitly requested that the revoke reason be returned by the "check endpoint". `GET /waitinglist/users` is the only existing endpoint whose purpose is to check access by email. |
| Repository: split into `GrantAccessTx(ids, source string)` and `RevokeAccessTx(id, reason, by string)` | The current `SetHasAccessTx(ids)` becomes a thin wrapper around `GrantAccessTx(ids, "scheduler")` for backward compatibility, until the scheduler is migrated to call `GrantAccessTx` directly. |
| Index on `created_at` for the dashboard chart | The "users enlisted per day" chart in plan 18 groups by `DATE(created_at)`; an index keeps it cheap as the table grows. |

### Dependencies

- Builds on: [02-database](../02-database/plan.md), [14-already-has-access-response](../14-already-has-access-response/plan.md).
- Required by: [17-admin-api-and-auth](../17-admin-api-and-auth/plan.md), [18-admin-web-ui](../18-admin-web-ui/plan.md).
- No new Go module dependencies.

---

## Requirements

1. **Schema additions** on `user_entity`:
   - `access_granted_at TIMESTAMPTZ` — nullable; set when access is granted.
   - `access_granted_by TEXT CHECK (access_granted_by IN ('scheduler','admin'))` — nullable; set when access is granted.
   - `access_revoked_at TIMESTAMPTZ` — nullable; set when access is revoked.
   - `access_revoked_by TEXT` — nullable; admin identifier (today: the basic-auth username from plan 17).
   - `access_revoke_reason TEXT` — nullable; required when `access_revoked_at IS NOT NULL` (`CHECK ((access_revoked_at IS NULL) = (access_revoke_reason IS NULL))`).
2. **Drop the one-way trigger** installed by migration 006. Migration must be idempotent (`DROP TRIGGER IF EXISTS … DROP FUNCTION IF EXISTS …`).
3. **Backfill** `access_granted_at = created_at` and `access_granted_by = 'scheduler'` for rows where `has_access = TRUE`. INSERT/UPDATE order matters; do this in the same migration that adds the columns, before the `CHECK` constraints are validated.
4. **Repository helpers**:
   - `GrantAccessTx(ctx, tx, ids []string, source string) error` — sets `has_access = TRUE`, `access_granted_at = NOW()`, `access_granted_by = source`. Validates `source` against the allowed set.
   - `RevokeAccessTx(ctx, tx, id string, reason string, by string) error` — sets `has_access = FALSE`, `access_revoked_at = NOW()`, `access_revoke_reason = reason`, `access_revoked_by = by`. Returns `ErrUserNotFound` when no row matches; rejects empty `reason` with `ErrRevokeReasonRequired`.
   - `SetHasAccessTx(ids)` retained for now as a wrapper calling `GrantAccessTx(ids, "scheduler")`. Mark as deprecated in godoc.
5. **Model updates**:
   - Extend `UserEntity` with the new fields (all `*time.Time`/`*string`).
   - Extend `UserInfo` with `AccessGrantedAt`, `AccessGrantedBy`, `AccessRevokedAt`, `AccessRevokeReason` (all pointers / `omitzero`). The lookup endpoint already returns `UserInfo`, so this propagates automatically.
   - New sentinel: `ErrRevokeReasonRequired = errors.New("access revoke reason is required")`.
6. **Scheduler integration**: the existing scheduler grant path (`internal/waitlist/waitlist.go`) must call `GrantAccessTx(..., "scheduler")` so the audit columns are populated. (Ideally remove the deprecated wrapper in a follow-up.)
7. **Tests**: unit tests for the new repository methods (constraint enforcement, empty reason, unknown source); integration tests for the migration (gated by `TEST_DATABASE_URL`); model marshalling tests for the new optional fields.

---

## Design

### Migration `migrations/007_access_audit_and_drop_one_way.sql`

```sql
-- 007_access_audit_and_drop_one_way.sql
-- Add audit columns for access grants/revocations and remove the one-way
-- has_access trigger introduced in migration 006. Application code is now
-- the source of truth for the false→true→false transitions.

ALTER TABLE user_entity
    ADD COLUMN IF NOT EXISTS access_granted_at    TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS access_granted_by    TEXT,
    ADD COLUMN IF NOT EXISTS access_revoked_at    TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS access_revoked_by    TEXT,
    ADD COLUMN IF NOT EXISTS access_revoke_reason TEXT;

-- Backfill: any user that already has access was granted by the scheduler at
-- some point. The exact timestamp is unrecoverable; created_at is the best
-- proxy and is conservative for any "granted in the last N days" query.
UPDATE user_entity
SET    access_granted_at = COALESCE(access_granted_at, created_at),
       access_granted_by = COALESCE(access_granted_by, 'scheduler')
WHERE  has_access = TRUE
   AND (access_granted_at IS NULL OR access_granted_by IS NULL);

-- Constraints (added after backfill so existing data validates).
ALTER TABLE user_entity
    DROP CONSTRAINT IF EXISTS user_entity_access_granted_by_check,
    ADD  CONSTRAINT user_entity_access_granted_by_check
         CHECK (access_granted_by IS NULL OR access_granted_by IN ('scheduler','admin'));

ALTER TABLE user_entity
    DROP CONSTRAINT IF EXISTS user_entity_revoke_pair_check,
    ADD  CONSTRAINT user_entity_revoke_pair_check
         CHECK ((access_revoked_at IS NULL) = (access_revoke_reason IS NULL));

-- Drop the one-way trigger from migration 006.
DROP TRIGGER IF EXISTS trg_user_entity_has_access_one_way ON user_entity;
DROP FUNCTION IF EXISTS user_entity_has_access_one_way();

-- Index supporting the per-day enlistment chart (plan 18).
CREATE INDEX IF NOT EXISTS idx_user_entity_created_at
    ON user_entity (created_at);
```

### Model (`internal/model/model.go`)

```go
type UserEntity struct {
    ID                 string     `json:"id"`
    Firstname          string     `json:"firstname"`
    Lastname           string     `json:"lastname"`
    Email              string     `json:"email"`
    HasAccess          bool       `json:"has_access"`
    CreatedAt          time.Time  `json:"created_at"`
    IPAddress          *string    `json:"ip_address,omitzero"`
    AccessGrantedAt    *time.Time `json:"access_granted_at,omitzero"`
    AccessGrantedBy    *string    `json:"access_granted_by,omitzero"`
    AccessRevokedAt    *time.Time `json:"access_revoked_at,omitzero"`
    AccessRevokedBy    *string    `json:"access_revoked_by,omitzero"`
    AccessRevokeReason *string    `json:"access_revoke_reason,omitzero"`
}

type UserInfo struct {
    Firstname          string     `json:"firstname"`
    Lastname           string     `json:"lastname"`
    Email              string     `json:"email"`
    HasAccess          bool       `json:"has_access"`
    CreatedAt          time.Time  `json:"created_at"`
    AccessGrantedAt    *time.Time `json:"access_granted_at,omitzero"`
    AccessGrantedBy    *string    `json:"access_granted_by,omitzero"`
    AccessRevokedAt    *time.Time `json:"access_revoked_at,omitzero"`
    AccessRevokeReason *string    `json:"access_revoke_reason,omitzero"`
}

var ErrRevokeReasonRequired = errors.New("access revoke reason is required")
```

> Note: `UserInfo` deliberately omits `AccessRevokedBy`. The public lookup endpoint should not leak admin identifiers; only `AccessRevokeReason` is user-facing.

### Repository (`internal/repository/user.go`)

Add:

```go
// validGrantSources is the set of allowed access_granted_by values. Update
// the migration 007 CHECK constraint in lockstep with this set.
var validGrantSources = map[string]struct{}{
    "scheduler": {},
    "admin":     {},
}

// GrantAccessTx flips has_access to true for the given user IDs, recording
// the grant timestamp and source ('scheduler' | 'admin').
func (r *UserRepository) GrantAccessTx(ctx context.Context, tx model.DBTX, ids []string, source string) error {
    if _, ok := validGrantSources[source]; !ok {
        return fmt.Errorf("invalid grant source %q", source)
    }
    if len(ids) == 0 {
        return nil
    }
    _, err := tx.ExecContext(ctx, `
        UPDATE user_entity
        SET    has_access           = TRUE,
               access_granted_at    = NOW(),
               access_granted_by    = $1,
               access_revoked_at    = NULL,
               access_revoked_by    = NULL,
               access_revoke_reason = NULL
        WHERE  id = ANY($2)`, source, pq.Array(ids))
    return err
}

// RevokeAccessTx flips has_access to false for one user, recording the
// revoke timestamp, admin identifier, and reason.
func (r *UserRepository) RevokeAccessTx(ctx context.Context, tx model.DBTX, id, reason, by string) error {
    if strings.TrimSpace(reason) == "" {
        return model.ErrRevokeReasonRequired
    }
    res, err := tx.ExecContext(ctx, `
        UPDATE user_entity
        SET    has_access           = FALSE,
               access_revoked_at    = NOW(),
               access_revoked_by    = $1,
               access_revoke_reason = $2
        WHERE  id = $3`, by, reason, id)
    if err != nil {
        return err
    }
    n, err := res.RowsAffected()
    if err != nil {
        return err
    }
    if n == 0 {
        return model.ErrUserNotFound
    }
    return nil
}

// SetHasAccessTx is deprecated in favor of GrantAccessTx. Retained for the
// existing scheduler call site until that is migrated.
//
// Deprecated: use GrantAccessTx(ctx, tx, ids, "scheduler") instead.
func (r *UserRepository) SetHasAccessTx(ctx context.Context, tx model.DBTX, ids []string) error {
    return r.GrantAccessTx(ctx, tx, ids, "scheduler")
}
```

(The repository's `GetByIDTx` / `GetByEmailTx` / `GetUserInfoByEmails` SELECTs must be extended to read the new columns. This is mechanical but required for the new fields to actually surface.)

### Scheduler (`internal/waitlist/waitlist.go`)

Replace the existing `SetHasAccessTx(ids)` call with `GrantAccessTx(ctx, tx, ids, "scheduler")`. A two-line change. Drop the wrapper in a follow-up commit once the scheduler is migrated and tests are green.

### Test data

Repository tests should cover:

- `GrantAccessTx` with `source="admin"` populates the audit columns.
- `GrantAccessTx` with an unknown source returns an error and does not mutate.
- `GrantAccessTx` clears any prior revocation columns (re-granting a previously revoked user).
- `RevokeAccessTx` with empty reason returns `ErrRevokeReasonRequired`.
- `RevokeAccessTx` on a missing user returns `ErrUserNotFound`.
- `RevokeAccessTx` populates the audit columns and the `CHECK` constraint passes.
- `GetUserInfoByEmails` returns the new fields for both granted and revoked users.

### Backwards-compatible wire format

Adding optional, `omitzero` fields to `UserInfo` does not break existing clients — they simply ignore unknown keys. No version bump needed.

---

## Implementation Steps

1. **Migration** — add `migrations/007_access_audit_and_drop_one_way.sql` per Design.
2. **Model** — extend `UserEntity` and `UserInfo`; add `ErrRevokeReasonRequired`.
3. **Repository** — implement `GrantAccessTx`, `RevokeAccessTx`; deprecate `SetHasAccessTx`; extend SELECT statements (`GetByIDTx`, `GetByEmailTx`, `GetUserInfoByEmails`) to read the new columns.
4. **Scheduler** — switch `internal/waitlist/waitlist.go` from `SetHasAccessTx` to `GrantAccessTx(..., "scheduler")`.
5. **Tests** — repository unit tests + integration tests gated by `TEST_DATABASE_URL`.
6. **Docs** — update CLAUDE.md project structure / endpoint table if needed (the lookup endpoint shape changes).
7. **Verify** — `make format && make lint && make test`. Run integration suite locally with `TEST_DATABASE_URL` to confirm migration 007 applies cleanly on top of migration 006.

---

## Testing

### Repository unit / integration tests (`internal/repository/user_test.go` — gated by `TEST_DATABASE_URL`)

| # | Test Case | Expected Result |
|---|---|---|
| 1 | `GrantAccessTx(ids, "scheduler")` on fresh users | `has_access=true`, `access_granted_at` set to ~`NOW()`, `access_granted_by="scheduler"` |
| 2 | `GrantAccessTx(ids, "admin")` on fresh users | same, with `access_granted_by="admin"` |
| 3 | `GrantAccessTx(ids, "bogus")` | returns `error`; row unchanged |
| 4 | `RevokeAccessTx(id, "policy violation", "admin1")` on a granted user | `has_access=false`, `access_revoked_at` set, `access_revoke_reason="policy violation"`, `access_revoked_by="admin1"` |
| 5 | `RevokeAccessTx(id, "", "admin1")` | returns `ErrRevokeReasonRequired`; row unchanged |
| 6 | `RevokeAccessTx("nonexistent-uuid", "x", "admin1")` | returns `ErrUserNotFound` |
| 7 | Re-grant after revoke (`Grant → Revoke → Grant`) | revoke columns cleared on the second grant; `has_access=true` |
| 8 | `GetUserInfoByEmails` on a revoked user | response includes `access_revoke_reason`, omits `access_revoked_by` |
| 9 | Migration idempotency | apply migration 007 twice; second run is a no-op (no error, no schema drift) |
| 10 | Migration backfill | a row with `has_access=true` and NULL audit columns gets `access_granted_at=created_at` and `access_granted_by="scheduler"` |
| 11 | `CHECK` constraint enforces revoke pair | raw `UPDATE` setting `access_revoked_at` without `access_revoke_reason` (or vice versa) is rejected |
| 12 | Trigger from migration 006 is gone | raw `UPDATE … SET has_access=false` succeeds at the SQL level (the application-layer guard is the only check now) |

### Model tests (`internal/model/model_test.go` — pure unit)

| # | Test Case | Expected Result |
|---|---|---|
| 13 | `UserInfo` JSON omits zero-valued audit fields | `omitzero` produces no key when pointer is nil |
| 14 | `UserInfo` JSON includes audit fields when set | keys present with correct ISO-8601 timestamps |

### Scheduler tests

| # | Test Case | Expected Result |
|---|---|---|
| 15 | Scheduler grant populates `access_granted_by="scheduler"` | observable in `user_entity` after a scheduler tick |

### Edge cases

- **Concurrent grant + revoke**: a revoke landing between the scheduler's `SELECT` and `GrantAccessTx` should not silently re-grant access. Acceptable today because both happen inside the scheduler's outer transaction; document but no extra defense required for now.
- **Existing data with `has_access = false` and revoke columns half-set**: not possible — the migration runs the backfill before adding the `CHECK` constraint, so no row can be in that state at migration time, and the application never sets revoke columns on a never-granted user.

---

## Acceptance Criteria

- [ ] Migration `007_access_audit_and_drop_one_way.sql` adds the five audit columns, backfills existing granted users, drops the one-way trigger and function, and creates the `created_at` index. Idempotent on re-run.
- [ ] `UserEntity` and `UserInfo` expose the audit fields via JSON with `omitzero` semantics.
- [ ] `UserRepository.GrantAccessTx` and `RevokeAccessTx` exist, are covered by tests, and enforce the `source` whitelist and non-empty `reason`.
- [ ] `SetHasAccessTx` is a deprecated wrapper calling `GrantAccessTx(..., "scheduler")`.
- [ ] The waitlist scheduler grants via `GrantAccessTx(..., "scheduler")` (verified by an integration test).
- [ ] `GET /waitinglist/users` includes `access_revoke_reason` (and the other audit timestamps) when applicable.
- [ ] `make format`, `make lint`, and `make test` all pass.
- [ ] CLAUDE.md plans table references plan `16`.

---

## Open Questions

1. **Conflict resolution with plan 14** — Drop the trigger (default in this plan) or layer revocation on top of a separate `access_revoked` flag while keeping `has_access` one-way? Default: drop. Confirm before merging.
2. **Backfill attribution** — `access_granted_at = created_at, access_granted_by = 'scheduler'` for existing granted users is a best guess. If precise audit history matters for compliance, leave the columns NULL for legacy rows and document the cutover date instead. Default: backfill.
3. **`access_revoked_by` shape** — Today the value is the basic-auth username from plan 17. If the team plans multiple admins or external SSO later, we may want a foreign key to a future `admin_user` table. Default: `TEXT`, no FK.
4. **`UserInfo` should expose `access_revoked_by`?** — The user said "the reason for access revoke should be shown ... and sent in the check endpoint". Does "shown" include the admin's identifier, or is the reason alone sufficient for end users? Default: reason only, admin identity stays admin-side.
5. **Soft-delete waiting list rows on grant?** — When the admin grants access in plan 17, we delete the corresponding waiting_list row. Should that row be soft-deleted for audit instead? Default: hard delete (matches the existing scheduler behavior).
