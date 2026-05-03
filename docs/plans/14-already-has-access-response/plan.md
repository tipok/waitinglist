# 14 — Short-circuit Sign-up When User Already Has Access

## Overview

When a client re-submits `POST /waitinglist` for a user whose `has_access` is already `true`, the service should not attempt to (re-)insert the user into the `waiting_list` table. Instead, it should respond with **HTTP 205 Reset Content** and a descriptive message so the client can clear its sign-up form and route the user to the protected page.

In addition, the invariant *"`has_access` only ever transitions `false → true`, never the reverse"* must be enforced at the database level so that no future code path (or stray UPDATE) can revoke access by mistake.

### Key Design Decisions

| Decision | Rationale |
|---|---|
| Use HTTP **205 Reset Content** for the "already has access" case | The user explicitly requested this status. Semantically it tells the client "the action is complete on the server side; reset your local form / view". It is distinct from `409 Conflict` (which already covers "still on the waiting list, not yet granted access"). |
| Detect `HasAccess == true` in the handler before calling `waitListStore.Add` | Earliest point at which we have the existing `UserEntity` loaded. Avoids a wasted INSERT and a confusing `409` for a user who has already moved past the waiting list. |
| New sentinel error `model.ErrAlreadyHasAccess` | Lets the handler distinguish this case from `ErrAlreadyOnWaitingList` and from generic errors, mirroring the existing sentinel pattern. |
| DB-level guard via `CHECK` constraint on a trigger, OR a `BEFORE UPDATE` trigger | A plain `CHECK` cannot reference the previous row value; a `BEFORE UPDATE` trigger that raises an exception when `OLD.has_access = true AND NEW.has_access = false` is the simplest correct enforcement. |
| Repository `SetHasAccess` only ever sets `true` | Already true today; we additionally remove any (currently non-existent) helpers that could set it to false, and document the invariant in the repository godoc. |
| Empty body for the 205 response, with the message in the JSON envelope | RFC 7231 says a 205 response *should not* include content; in practice many clients accept a small JSON payload, and the existing `WriteJSON` / `WriteError` helpers already write structured bodies. We keep a tiny JSON body `{ "message": "..." }` for client UX consistency, matching the rest of the API. If the user prefers a strict empty body we can switch to a header-only response — see open question. |

### Dependencies

- Builds on: [04-waiting-list](../04-waiting-list/plan.md), [05-api](../05-api/plan.md).
- Relies on the existing `user_entity.has_access` column (introduced in [02-database](../02-database/plan.md)).
- No new Go module dependencies.

---

## Requirements

1. **Handler** — In `POST /waitinglist`, after loading an existing user by email, if `user.HasAccess == true`, return HTTP 205 with a JSON body `{"message": "user already has access"}` and **do not** insert into `waiting_list`.
2. **No regression for the 409 path** — Users that exist but do not yet have access continue to receive `409 Conflict` when they are still on the waiting list.
3. **Invariant in code** — `UserRepository.SetHasAccess` / `SetHasAccessTx` must continue to set only `true`. Add a godoc note documenting the one-way invariant. Reject any other code path that attempts to write `has_access = false` via a database-side guard.
4. **Invariant in DB** — Add a migration that installs a `BEFORE UPDATE` trigger on `user_entity` raising an exception when an UPDATE would change `has_access` from `true` to `false`. INSERTs with `has_access = false` (the default) remain allowed; the trigger only fires on the `true → false` transition.
5. **Helper** — Extend `internal/handler/response.go` (or wherever `WriteJSON` lives) so HTTP 205 with a JSON body is convenient to emit; reuse existing helpers if no new helper is warranted.
6. **Tests** — Cover the new handler branch, the unchanged 409 branch, the unchanged 201 branch, and the DB trigger.

---

## Design

### Handler change (`internal/handler/waitinglist.go`)

Insert a check after the `GetByEmailTx` lookup, before calling `waitListStore.Add`:

```go
user, err := h.userStore.GetByEmailTx(ctx, tx, req.Email)
if err != nil {
    if !errors.Is(err, model.ErrUserNotFound) {
        // ... existing error handling
    }
    // ... existing CreateTx path for new users
} else if user.HasAccess {
    // Existing user already has access — short-circuit.
    // Roll back is implicit via deferred tx.Rollback().
    WriteJSON(w, http.StatusResetContent, map[string]string{
        "message": "user already has access",
    }, h.logger)
    return
}
```

Notes:
- The transaction is rolled back via the existing `defer` — no commit happens for the 205 response.
- We do not need to log this case at error level; an `Info` log with `"email"` and `"user_id"` is sufficient and consistent with the rest of the file.
- The `Add` call's existing `ErrAlreadyOnWaitingList` → `409` branch is untouched; it now applies only to users without access.

#### Status code constant

Use the standard library: `http.StatusResetContent` (= `205`).

### Model change (`internal/model/model.go`)

Add a new sentinel for repository-level use (not strictly required for the handler change above, but useful if any future code path wants to surface the invariant violation explicitly):

```go
var (
    // existing sentinels...
    ErrAlreadyHasAccess = errors.New("user already has access")
)
```

This sentinel is **not** returned today; it is reserved for future repository helpers (e.g. an "ensure on waiting list or report access" method). The handler change uses an inline `user.HasAccess` check.

> Decision: keep the sentinel, but do not wire it through the repository in this plan to keep the change minimal. If the codebase prefers strict YAGNI, drop this sentinel — the handler does not need it.

### Repository invariant note (`internal/repository/user.go`)

Add a single-line godoc above `SetHasAccessTx`:

```go
// SetHasAccessTx grants access to the given user IDs. has_access is a one-way
// flag — once true it must never be set back to false. The 003-style migration
// installs a database trigger that enforces this invariant.
```

No code change to the function body.

### Database migration

New file: `migrations/006_has_access_one_way.sql`

```sql
-- 006_has_access_one_way.sql
-- Enforce that user_entity.has_access can only transition false → true.
-- Once a user is granted access, the flag may never be revoked.

CREATE OR REPLACE FUNCTION user_entity_has_access_one_way()
RETURNS trigger AS $$
BEGIN
    IF OLD.has_access = TRUE AND NEW.has_access = FALSE THEN
        RAISE EXCEPTION
            'has_access is one-way: cannot set false on user % whose has_access is already true', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_user_entity_has_access_one_way ON user_entity;
CREATE TRIGGER trg_user_entity_has_access_one_way
    BEFORE UPDATE ON user_entity
    FOR EACH ROW
    EXECUTE FUNCTION user_entity_has_access_one_way();
```

Notes:
- `CREATE OR REPLACE FUNCTION` and `DROP TRIGGER IF EXISTS` keep the migration idempotent (consistent with the project's `IF NOT EXISTS` style).
- Trigger only fires on `UPDATE`, so the default `false` value on INSERT is unaffected.
- An UPDATE that does not change `has_access` (e.g. `UPDATE user_entity SET firstname = ...`) passes through because `OLD.has_access = NEW.has_access`.

### Response shape

For the new 205 case the response body is:

```json
{ "message": "user already has access" }
```

Content-Type: `application/json`. The `WriteJSON` helper already sets this header.

> **Open question:** RFC 7231 §6.3.6 says a 205 response *MUST NOT* include a payload body. Strict clients may reject a body. If the team prefers strict compliance, replace `WriteJSON(...)` with a header-only write:
> ```go
> w.Header().Set("X-Status-Reason", "user already has access")
> w.WriteHeader(http.StatusResetContent)
> ```
> The user's request mentions "with the corresponding message", which is ambiguous between body-message and header-message. **Default in this plan: JSON body** (matches the rest of the API). Flag this for review during implementation.

---

## Implementation Steps

1. **Add migration** — Create `migrations/006_has_access_one_way.sql` with the trigger from the Design section.
2. **Update model** — Add `ErrAlreadyHasAccess` sentinel to `internal/model/model.go` (optional — see Design note).
3. **Document repository invariant** — Add the godoc note above `SetHasAccessTx` in `internal/repository/user.go`.
4. **Update handler** — In `internal/handler/waitinglist.go::handleAdd`, add the `user.HasAccess` short-circuit returning HTTP 205.
5. **Update handler tests** — Add a test for the 205 branch and adjust existing tests if any mock returned `HasAccess=true` for a non-creation path.
6. **Update repository tests** — Add an integration test (gated by `TEST_DATABASE_URL`) that asserts the trigger rejects `has_access = false` updates.
7. **Run formatters/linters/tests** — `make format && make lint && make test`.
8. **Update `CLAUDE.md` plans table** — Mark `14-already-has-access-response` as Not started → ✅ Complete on merge.

---

## Testing

### Handler unit tests (`internal/handler/waitinglist_test.go`)

| # | Test Case | Setup | Expected Result |
|---|---|---|---|
| 1 | New user → 201 Created | `GetByEmailTx` returns `ErrUserNotFound`; `CreateTx` succeeds; `Add` succeeds | `201`, body contains `user` and `waiting_list_entry` |
| 2 | Existing user without access, not on list → 201 Created | `GetByEmailTx` returns user with `HasAccess=false`; `Add` succeeds | `201`, body contains existing `user` and new `waiting_list_entry` |
| 3 | Existing user without access, already on list → 409 Conflict | `GetByEmailTx` returns user with `HasAccess=false`; `Add` returns `ErrAlreadyOnWaitingList` | `409`, message `"user is already on the waiting list"` |
| 4 | **(NEW)** Existing user with `HasAccess=true` → 205 Reset Content | `GetByEmailTx` returns user with `HasAccess=true` | `205`, body `{"message": "user already has access"}`; **`Add` is never called** |
| 5 | **(NEW)** 205 path commits no transaction | Same as #4 | The mock transaction's `Commit` is **not** invoked; `Rollback` is invoked via defer |
| 6 | Invalid JSON / missing fields still return 400 | unchanged | unchanged |

### Repository tests (`internal/repository/user_test.go` — integration, gated by `TEST_DATABASE_URL`)

| # | Test Case | Description |
|---|---|---|
| 7 | Trigger blocks `true → false` | INSERT user, `SetHasAccess`, then attempt raw `UPDATE user_entity SET has_access=false WHERE id=$1`; expect a Postgres error containing `"has_access is one-way"` |
| 8 | Trigger allows `false → true` | INSERT user (default `false`), `SetHasAccess(ids)` succeeds |
| 9 | Trigger allows unrelated UPDATE | INSERT user with `has_access=true`, run `UPDATE user_entity SET firstname='x' WHERE id=$1`; expect success (no `has_access` change) |
| 10 | Trigger allows `true → true` no-op | UPDATE that re-sets `has_access=true` succeeds |

### Edge cases

| # | Test Case | Description |
|---|---|---|
| 11 | Concurrent re-submit by the same email | Two concurrent `POST /waitinglist` for an `has_access=true` user both return 205; neither inserts into `waiting_list`. Verifiable via integration or manual test. |
| 12 | User has access but somehow still on `waiting_list` | If a stale row exists (e.g. between scheduler runs), the new branch still returns 205 *without* attempting the INSERT, leaving the stale row to be cleaned up by the scheduler. Documented behavior; covered by handler test #4. |

---

## Acceptance Criteria

- [ ] `POST /waitinglist` with an email matching an existing user where `has_access = true` returns `HTTP 205 Reset Content` with body `{"message": "user already has access"}`.
- [ ] No `waiting_list` row is inserted in the 205 case.
- [ ] The transaction opened in `handleAdd` is rolled back (not committed) in the 205 case.
- [ ] Existing 201 (new user) and 409 (already on list, no access) responses are unchanged.
- [ ] Migration `006_has_access_one_way.sql` installs a trigger that rejects any `UPDATE` flipping `has_access` from `true` to `false`.
- [ ] Migration is idempotent (re-running the migration suite does not error).
- [ ] All new and existing handler tests pass.
- [ ] Integration tests for the trigger pass when `TEST_DATABASE_URL` is set, and skip cleanly when it is not.
- [ ] `make format`, `make lint`, and `make test` all pass.
- [ ] `CLAUDE.md` plans table updated to reference plan `14`.

---

## Open Questions

1. **Body vs. header for 205** — Strictly, RFC 7231 forbids a body on 205 responses. Confirm with the API consumer whether `{"message": "..."}` is acceptable (default in this plan) or whether a header-only response is required.
2. **Should the sentinel `ErrAlreadyHasAccess` be wired through the repository?** — Currently the handler does an inline `user.HasAccess` check. If a future endpoint needs to raise the same condition from a repository call, threading a sentinel through `Add` (or a new method) would be cleaner. Out of scope for this plan unless requested.
