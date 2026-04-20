# 10 — ETag Caching for `/waitinglist/users`

## Overview

Implement HTTP ETag-based caching for the `GET /waitinglist/users` endpoint. A new **generated column** (`row_hash`) is added to the `user_entity` table that stores a SHA-256 hash derived from `email` and `has_access`. When the endpoint is called, the individual row hashes are combined into a single composite ETag that is returned via the `ETag` response header. If the client sends an `If-None-Match` request header whose value matches the current ETag, the server responds with `304 Not Modified` instead of the full JSON body.

### Key Design Decisions

| Decision | Rationale |
|---|---|
| PostgreSQL generated column | Hash is always consistent with the data — no application-level cache invalidation needed. |
| Sort requested emails before hashing | Guarantees a deterministic composite ETag regardless of the order emails appear in the query string. |
| SHA-256 | Widely supported, collision-resistant, available natively in PostgreSQL (`pgcrypto` / `encode + digest`). |

### Dependencies

- Depends on: [02-database](../02-database/plan.md) (migration runner), [09-user-lookup-by-email](../09-user-lookup-by-email/plan.md) (existing endpoint).
- No new Go dependencies required — `crypto/sha256` and `encoding/hex` are in the standard library.

---

## Requirements

1. **Database** — Add a `row_hash` generated column to `user_entity` that automatically computes `SHA-256(email || ':' || has_access::text)`.
2. **Repository** — Extend `GetUserInfoByEmails` (or add a new method) to also return each user's `row_hash`.
3. **Handler / ETag logic** — On `GET /waitinglist/users`:
   a. Sort the requested emails alphabetically.
   b. Query users (including their `row_hash` values).
   c. Sort the returned rows by email (to match the sorted request order).
   d. Concatenate all `row_hash` values and compute a final SHA-256 → this is the composite ETag.
   e. Compare the composite ETag with the client's `If-None-Match` header.
   f. If they match → respond `304 Not Modified` (no body).
   g. Otherwise → respond `200 OK` with the JSON body **and** set the `ETag` response header.
4. **ETag format** — Use the quoted-string format required by HTTP: `"<hex-encoded-sha256>"`.
5. **Single-user request** — When only one email is requested the composite hash equals `SHA-256(row_hash)` (i.e. still hashed once more for consistency).

---

## Design

### Database Schema Change

New migration file: `migrations/005_user_row_hash.sql`

```sql
-- Requires pgcrypto for digest(); enable if not already present.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

ALTER TABLE user_entity
    ADD COLUMN IF NOT EXISTS row_hash TEXT
        GENERATED ALWAYS AS (
            encode(digest(email || ':' || has_access::text, 'sha256'), 'hex')
        ) STORED;
```

The column is `STORED` so the hash is computed once on insert/update and can be read efficiently.

### Model Changes (`internal/model/model.go`)

Add `RowHash` field to `UserInfo`:

```go
type UserInfo struct {
    Firstname string    `json:"firstname"`
    Lastname  string    `json:"lastname"`
    Email     string    `json:"email"`
    HasAccess bool      `json:"has_access"`
    CreatedAt time.Time `json:"created_at"`
    RowHash   string    `json:"-"` // excluded from JSON, used for ETag computation
}
```

### Repository Changes (`internal/repository/user.go`)

Update the `GetUserInfoByEmails` query to also select `row_hash`:

```sql
SELECT firstname, lastname, email, has_access, created_at, row_hash
FROM user_entity
WHERE email = ANY($1)
```

Scan `row_hash` into the new `RowHash` field.

### Handler Changes (`internal/handler/waitinglist.go`)

In `handleGetUsersByEmail`:

1. **Sort** the incoming `emails` slice with `slices.Sort(emails)`.
2. Call the repository as before.
3. **Sort** the returned `[]model.UserInfo` by `Email` using `slices.SortFunc`.
4. **Compute composite ETag**:
   ```go
   h := sha256.New()
   for _, u := range users {
       h.Write([]byte(u.RowHash))
   }
   etag := `"` + hex.EncodeToString(h.Sum(nil)) + `"`
   ```
5. **Compare** with `If-None-Match`:
   ```go
   if r.Header.Get("If-None-Match") == etag {
       w.WriteHeader(http.StatusNotModified)
       return
   }
   ```
6. **Set header** and write response:
   ```go
   w.Header().Set("ETag", etag)
   WriteJSON(w, http.StatusOK, model.UserInfoList{Users: users}, h.logger)
   ```

### ETag Computation — Worked Example

Given two users:

| email | has_access | row_hash (SHA-256 of `email:has_access`) |
|---|---|---|
| `bob@example.com` | `false` | `a1b2c3...` |
| `alice@example.com` | `true` | `d4e5f6...` |

Request: `GET /waitinglist/users?email=bob@example.com&email=alice@example.com`

1. Sort emails → `[alice@example.com, bob@example.com]`
2. Query returns rows; sort by email → `[{alice, d4e5f6...}, {bob, a1b2c3...}]`
3. Composite = `SHA-256("d4e5f6..." + "a1b2c3...")` → `"<final_hex>"`
4. Return `ETag: "<final_hex>"` header.

---

## Implementation Steps

### Step 1 — Database Migration

- [ ] Create `migrations/005_user_row_hash.sql` with the `pgcrypto` extension and `ALTER TABLE` adding the `row_hash` generated column.
- [ ] Verify the migration is idempotent (`IF NOT EXISTS`).

### Step 2 — Model Update

- [ ] Add `RowHash string \`json:"-"\`` to `model.UserInfo`.

### Step 3 — Repository Update

- [ ] Update `GetUserInfoByEmails` SQL query to select `row_hash`.
- [ ] Update the `Scan` call to include `RowHash`.
- [ ] Update the `WaitingListUserStore` interface if the method signature changes (it should not — only the returned data changes).

### Step 4 — Handler ETag Logic

- [ ] Sort incoming emails before querying.
- [ ] Sort returned users by email after querying.
- [ ] Compute composite SHA-256 ETag from concatenated `row_hash` values.
- [ ] Check `If-None-Match` header; return `304` on match.
- [ ] Set `ETag` header on `200` responses.

### Step 5 — Formatting, Linting, Testing

- [ ] `make format`
- [ ] `make lint`
- [ ] `make test`

---

## Testing

### Unit Tests

#### Handler (`internal/handler/waitinglist_test.go`)

1. **ETag returned on 200** — Request users by email; verify the `ETag` header is present and correctly formatted (`"<hex>"`).
2. **304 Not Modified** — First request to get the ETag, then repeat with `If-None-Match` set to that ETag; verify `304` status and empty body.
3. **ETag changes when data changes** — Mock different `RowHash` values and verify the ETag differs.
4. **Order independence** — Request `?email=b&email=a` and `?email=a&email=b`; verify both produce the same ETag.
5. **Single email** — Verify ETag is still computed (SHA-256 of the single row hash).
6. **No match** — Send `If-None-Match` with a stale ETag; verify `200` with body.
7. **Empty result** — No users found; verify a deterministic ETag is still returned (SHA-256 of empty input).

#### Repository (`internal/repository/user_test.go`)

8. **RowHash populated** — (Integration test, gated by `TEST_DATABASE_URL`) Insert a user and call `GetUserInfoByEmails`; verify `RowHash` is a 64-character hex string.
9. **RowHash changes on update** — Update `has_access` and verify the hash changes.
10. **RowHash deterministic** — Same `email`/`has_access` always produces the same hash.

### Edge Cases & Negative Scenarios

11. **No `If-None-Match` header** — Normal `200` response with ETag.
12. **Malformed `If-None-Match`** — Non-matching value; returns `200`.
13. **Empty email list** — Returns `400` (existing validation); no ETag logic executed.
14. **Subset of emails exist** — Only existing users contribute to the ETag; missing emails are simply absent.

---

## Acceptance Criteria

- [ ] `user_entity` table has a `row_hash` generated column using SHA-256 of `email || ':' || has_access::text`.
- [ ] `GET /waitinglist/users` returns an `ETag` header on every `200` response.
- [ ] Sending `If-None-Match` with a matching ETag returns `304 Not Modified` with no body.
- [ ] The ETag is order-independent: requesting the same set of emails in any order produces the same ETag.
- [ ] `RowHash` is excluded from the JSON response (`json:"-"`).
- [ ] All existing tests continue to pass.
- [ ] New unit tests cover ETag generation, `304` responses, order independence, and edge cases.
- [ ] `make format`, `make lint`, and `make test` all pass.
