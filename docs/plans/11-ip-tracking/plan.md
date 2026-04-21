# 11 — Track IP Address on User Entity

## Overview

Track the IP address of the client when a user entity is created via the waiting list sign-up flow. The IP is stored on the `user_entity` row because user entities represent permanent data, whereas `waiting_list` entries are intermediate and may be purged. When the service runs behind a reverse proxy or load balancer, the `X-Forwarded-For` header is used to determine the original client IP; otherwise the direct connection address (`http.Request.RemoteAddr`) is used as a fallback.

### Key Design Decisions

| Decision | Rationale |
|---|---|
| Store IP on `user_entity`, not `waiting_list` | `user_entity` holds permanent data; `waiting_list` is intermediate and may be purged — the IP should survive purges. |
| `X-Forwarded-For` first entry | Standard convention — the first address in the comma-separated list is the original client IP. |
| `INET` column type | PostgreSQL's `INET` type validates and efficiently stores IPv4/IPv6 addresses. |
| Nullable column | Existing rows predate IP tracking; a `NULL` value means "unknown". |
| Helper function in handler package | IP extraction logic is reusable and independently testable. |
| IP set only on creation | The IP is captured once when the user entity is first created. Existing users looked up by email are not updated. |

### Dependencies

- Depends on: [02-database](../02-database/plan.md) (migration runner), [03-user-entity](../03-user-entity/plan.md) (user entity model and repository).
- No new Go dependencies required — `net`, `net/http`, and `strings` are in the standard library.

---

## Requirements

1. **Database** — Add a nullable `ip_address` column (`INET`) to the `user_entity` table.
2. **IP extraction** — Implement a helper function that resolves the client IP from an `*http.Request`:
   a. Check `X-Forwarded-For` header; if present, use the **first** (left-most) IP in the comma-separated list.
   b. If `X-Forwarded-For` is absent or empty, fall back to `X-Real-Ip` header.
   c. If neither proxy header is present, use `r.RemoteAddr` (stripping the port if present).
3. **Model** — Add `IPAddress` field to `UserEntity`.
4. **Repository** — Update the `CreateTx` method to accept and persist an IP address string.
5. **Handler** — Extract the client IP from the request and set it on the `UserEntity` before calling `CreateTx` in the `POST /waitinglist` flow.
6. **API response** — Include `ip_address` in the JSON representation of user entities.

---

## Design

### Database Schema Change

New migration file: `migrations/005_user_entity_ip.sql`

```sql
ALTER TABLE user_entity
    ADD COLUMN IF NOT EXISTS ip_address INET;
```

The column is nullable so existing rows remain valid without a backfill.

### Model Changes (`internal/model/model.go`)

Add `IPAddress` to `UserEntity`:

```go
type UserEntity struct {
    ID        string    `json:"id"`
    Firstname string    `json:"firstname"`
    Lastname  string    `json:"lastname"`
    Email     string    `json:"email"`
    HasAccess bool      `json:"has_access"`
    CreatedAt time.Time `json:"created_at"`
    IPAddress *string   `json:"ip_address,omitzero"`
}
```

A pointer-to-string (`*string`) maps naturally to a nullable database column. `omitzero` omits the field when nil.

### IP Extraction Helper (`internal/handler/ip.go`)

```go
// ClientIP extracts the client's IP address from the request.
// It checks X-Forwarded-For (first entry), then X-Real-Ip, then
// falls back to r.RemoteAddr with port stripped.
func ClientIP(r *http.Request) string { ... }
```

Logic:
1. Read `X-Forwarded-For` header → split on `,` → trim and return the first non-empty entry.
2. If empty, read `X-Real-Ip` header → trim and return if non-empty.
3. Otherwise, use `r.RemoteAddr` and strip the port via `net.SplitHostPort` (handle the case where `SplitHostPort` fails, e.g. bare IPv4 without port).

### Repository Changes (`internal/repository/user.go`)

Update `CreateTx` to include `ip_address` in the INSERT:

```go
func (r *UserRepository) CreateTx(ctx context.Context, tx model.DBTX, user *model.UserEntity) error {
    query := `INSERT INTO user_entity (firstname, lastname, email, ip_address)
        VALUES ($1, $2, $3, $4)
        RETURNING id, has_access, created_at`

    err := tx.QueryRowContext(ctx, query, user.Firstname, user.Lastname, user.Email, user.IPAddress).
        Scan(&user.ID, &user.HasAccess, &user.CreatedAt)
    ...
}
```

Update all `SELECT` queries in user repository (`GetByEmailTx`, `GetUserInfoByEmails`) to also select and scan `ip_address` where `UserEntity` is returned.

Note: `GetUserInfoByEmails` returns `UserInfo` (not `UserEntity`), so it does not need to include `ip_address` unless we also add it to `UserInfo`. For now, `ip_address` is only on `UserEntity`.

### Handler Changes (`internal/handler/waitinglist.go`)

In `handleAdd`, extract the IP and set it on the user entity before creation:

```go
user = &model.UserEntity{
    Firstname: req.Firstname,
    Lastname:  req.Lastname,
    Email:     req.Email,
    IPAddress: ptrTo(ClientIP(r)),
}
```

Where `ptrTo` is a small generic helper (or inline `&ip`).

### Interface Changes

The `WaitingListUserStore` interface method signatures remain unchanged — `CreateTx` still takes `*model.UserEntity`. The IP is passed as a field on the struct.

The `WaitingListStore` interface is **not** modified.

---

## Implementation Steps

1. **Create migration** — Add `migrations/005_user_entity_ip.sql` with the `ALTER TABLE` statement.
2. **Update model** — Add `IPAddress *string` field with `json:"ip_address,omitzero"` tag to `UserEntity`.
3. **Create IP helper** — Add `internal/handler/ip.go` with the `ClientIP` function.
4. **Write IP helper tests** — Add `internal/handler/ip_test.go` covering all extraction paths.
5. **Update user repository** — Modify `CreateTx` INSERT to include `ip_address`, update `GetByEmailTx` SELECT to scan `ip_address`.
6. **Update user repository tests** — Adjust existing mocks and add tests for IP persistence.
7. **Update handler** — In `handleAdd`, call `ClientIP(r)` and set it on the `UserEntity` before creation.
8. **Update handler tests** — Verify IP is extracted and passed through in handler tests.
9. **Update existing tests** — Fix any test mocks/assertions affected by the new field.
10. **Verify** — Run `make format`, `make lint`, `make test`.

---

## Testing

### IP Extraction (`internal/handler/ip_test.go`)

| # | Test Case | Input | Expected Output |
|---|---|---|---|
| 1 | Single X-Forwarded-For | `X-Forwarded-For: 203.0.113.50` | `203.0.113.50` |
| 2 | Multiple X-Forwarded-For | `X-Forwarded-For: 203.0.113.50, 70.41.3.18, 150.172.238.178` | `203.0.113.50` |
| 3 | X-Forwarded-For with spaces | `X-Forwarded-For:  203.0.113.50 , 70.41.3.18 ` | `203.0.113.50` |
| 4 | Empty X-Forwarded-For, X-Real-Ip present | `X-Real-Ip: 198.51.100.10` | `198.51.100.10` |
| 5 | No proxy headers, RemoteAddr with port | `RemoteAddr: 192.0.2.1:12345` | `192.0.2.1` |
| 6 | No proxy headers, IPv6 RemoteAddr with port | `RemoteAddr: [::1]:12345` | `::1` |
| 7 | No proxy headers, bare IP (no port) | `RemoteAddr: 192.0.2.1` | `192.0.2.1` |
| 8 | X-Forwarded-For takes precedence over X-Real-Ip | Both set | First X-Forwarded-For IP |
| 9 | Empty X-Forwarded-For value (only commas/spaces) | `X-Forwarded-For: , ,` | Falls back to X-Real-Ip or RemoteAddr |

### Handler Tests (`internal/handler/waitinglist_test.go`)

| # | Test Case | Description |
|---|---|---|
| 10 | IP set on new user creation | Verify that `CreateTx` receives a `UserEntity` with `IPAddress` populated from request headers. |
| 11 | IP not set on existing user lookup | When user already exists (looked up by email), `IPAddress` on the returned entity should remain as stored. |

### Repository Tests (`internal/repository/user_test.go`)

| # | Test Case | Description |
|---|---|---|
| 12 | Create user with IP | `CreateTx` persists user with non-nil IP; verify it round-trips through `GetByEmailTx`. |
| 13 | Create user without IP | `CreateTx` persists user with nil IP; verify `GetByEmailTx` returns nil `IPAddress`. |
| 14 | Existing users have nil IP | Pre-existing rows (without IP) return nil `IPAddress` from `GetByEmailTx`. |

### Edge Cases

| # | Test Case | Description |
|---|---|---|
| 15 | X-Forwarded-For with IPv6 | `X-Forwarded-For: 2001:db8::1` → `2001:db8::1` |
| 16 | RemoteAddr is empty string | Returns `""` (graceful degradation). |
| 17 | X-Forwarded-For single empty entry | `X-Forwarded-For: ` → falls back to next source. |

---

## Acceptance Criteria

- [ ] `user_entity` table has a nullable `ip_address INET` column.
- [ ] Creating a user via `POST /waitinglist` captures the client IP on the `UserEntity`.
- [ ] `X-Forwarded-For` (first entry) is preferred, then `X-Real-Ip`, then `RemoteAddr`.
- [ ] Existing users (looked up by email) are not modified — their stored IP (or NULL) is preserved.
- [ ] The `ip_address` field appears in JSON responses containing `UserEntity` when non-nil.
- [ ] All IP extraction tests pass (9 cases).
- [ ] All handler and repository tests pass.
- [ ] `make format`, `make lint`, and `make test` all pass.
