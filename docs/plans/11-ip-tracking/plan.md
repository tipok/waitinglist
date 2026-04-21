# 11 — Track IP Address of Waiting List Entry Creator

## Overview

Track the IP address of the user who creates a waiting list entry. The IP is stored on the `waiting_list` row at creation time and returned in API responses. When the service runs behind a reverse proxy or load balancer, the `X-Forwarded-For` header is used to determine the original client IP; otherwise the direct connection address (`http.Request.RemoteAddr`) is used as a fallback.

### Key Design Decisions

| Decision | Rationale |
|---|---|
| Store IP on `waiting_list`, not `user_entity` | The IP is a property of the sign-up action, not the user identity. A user could sign up from different IPs across services. |
| `X-Forwarded-For` first entry | Standard convention — the first address in the comma-separated list is the original client IP. |
| `INET` column type | PostgreSQL's `INET` type validates and efficiently stores IPv4/IPv6 addresses. Falls back to `TEXT` if bare IP extraction fails. |
| Nullable column | Existing rows predate IP tracking; a `NULL` value means "unknown". |
| Helper function in handler package | IP extraction logic is reusable and independently testable. |

### Dependencies

- Depends on: [02-database](../02-database/plan.md) (migration runner), [04-waiting-list](../04-waiting-list/plan.md) (existing waiting list flow).
- No new Go dependencies required — `net`, `net/http`, and `strings` are in the standard library.

---

## Requirements

1. **Database** — Add a nullable `ip_address` column (`INET`) to the `waiting_list` table.
2. **IP extraction** — Implement a helper function that resolves the client IP from an `*http.Request`:
   a. Check `X-Forwarded-For` header; if present, use the **first** (left-most) IP in the comma-separated list.
   b. If `X-Forwarded-For` is absent or empty, fall back to `X-Real-Ip` header.
   c. If neither proxy header is present, use `r.RemoteAddr` (stripping the port if present).
3. **Repository** — Update the `Add` method to accept and persist an IP address string.
4. **Model** — Add `IPAddress` field to `WaitingListEntry`.
5. **Handler** — Extract the client IP from the request and pass it through to the repository on `POST /waitinglist`.
6. **API response** — Include `ip_address` in the JSON representation of waiting list entries.

---

## Design

### Database Schema Change

New migration file: `migrations/005_waiting_list_ip.sql`

```sql
ALTER TABLE waiting_list
    ADD COLUMN IF NOT EXISTS ip_address INET;
```

The column is nullable so existing rows remain valid without a backfill.

### Model Changes (`internal/model/model.go`)

Add `IPAddress` to `WaitingListEntry`:

```go
type WaitingListEntry struct {
    ID                string    `json:"id"`
    UserID            string    `json:"user_id"`
    CreatedAt         time.Time `json:"created_at"`
    WeightedCreatedAt time.Time `json:"weighted_created_at"`
    IPAddress         *string   `json:"ip_address,omitzero"`
}
```

A pointer-to-string (`*string`) maps naturally to a nullable database column. `omitzero` omits the field when nil.

### IP Extraction Helper (`internal/handler/ip.go`)

```go
// ClientIP extracts the client's IP address from the request.
// It checks X-Forwarded-For (first entry), then X-Real-Ip, then
// falls back to r.RemoteAddr with the port stripped.
func ClientIP(r *http.Request) string {
    // 1. X-Forwarded-For: client, proxy1, proxy2
    if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
        if ip, _, _ := strings.Cut(xff, ","); ip != "" {
            return strings.TrimSpace(ip)
        }
    }

    // 2. X-Real-Ip
    if xri := r.Header.Get("X-Real-Ip"); xri != "" {
        return strings.TrimSpace(xri)
    }

    // 3. RemoteAddr (host:port)
    host, _, err := net.SplitHostPort(r.RemoteAddr)
    if err != nil {
        return r.RemoteAddr // already bare IP or unexpected format
    }
    return host
}
```

### Repository Changes (`internal/repository/waitinglist.go`)

Update the `Add` method signature and SQL:

```go
func (r *WaitingListRepository) Add(ctx context.Context, tx model.DBTX, userID string, ipAddress string) (*model.WaitingListEntry, error) {
    query := `INSERT INTO waiting_list (user_id, ip_address)
        VALUES ($1, $2::inet)
        RETURNING id, user_id, created_at, ip_address`
    // ...
}
```

Update `GetAll` / `GetWithOffsetLimit` queries to select `ip_address` and scan it into the new field.

### Handler Changes (`internal/handler/waitinglist.go`)

In `handleAdd`:

```go
clientIP := ClientIP(r)
entry, err := h.waitListStore.Add(ctx, tx, user.ID, clientIP)
```

### Interface Changes

Update `WaitingListStore.Add` signature:

```go
type WaitingListStore interface {
    Add(ctx context.Context, tx model.DBTX, userID string, ipAddress string) (*model.WaitingListEntry, error)
    // ...
}
```

---

## Implementation Steps

### Step 1 — Database Migration

- [x] Create `migrations/005_waiting_list_ip.sql` adding the `ip_address INET` nullable column.
- [x] Verify the migration is idempotent (`IF NOT EXISTS`).

### Step 2 — Model Update

- [x] Add `IPAddress *string \`json:"ip_address,omitzero"\`` to `model.WaitingListEntry`.

### Step 3 — IP Extraction Helper

- [x] Create `internal/handler/ip.go` with the `ClientIP(r *http.Request) string` function.

### Step 4 — Repository Update

- [x] Update `Add` to accept an `ipAddress string` parameter and include it in the `INSERT` statement.
- [x] Update the `RETURNING` clause to include `ip_address`.
- [x] Update `GetAll` / `GetWithOffsetLimit` to select and scan `ip_address`.

### Step 5 — Handler & Interface Update

- [x] Update the `WaitingListStore` interface's `Add` signature to include `ipAddress string`.
- [x] In `handleAdd`, call `ClientIP(r)` and pass the result to `Add`.

### Step 6 — Formatting, Linting, Testing

- [x] `make format`
- [x] `make lint`
- [x] `make test`

---

## Testing

### Unit Tests

#### IP Extraction (`internal/handler/ip_test.go`)

1. **X-Forwarded-For single IP** — Header `X-Forwarded-For: 203.0.113.50` → returns `203.0.113.50`.
2. **X-Forwarded-For multiple IPs** — Header `X-Forwarded-For: 203.0.113.50, 70.41.3.18, 150.172.238.178` → returns `203.0.113.50` (first entry).
3. **X-Forwarded-For with spaces** — Header `X-Forwarded-For:  203.0.113.50 , 70.41.3.18` → returns `203.0.113.50` (trimmed).
4. **X-Real-Ip fallback** — No `X-Forwarded-For`; `X-Real-Ip: 198.51.100.10` → returns `198.51.100.10`.
5. **RemoteAddr fallback** — No proxy headers; `RemoteAddr = "192.0.2.1:12345"` → returns `192.0.2.1`.
6. **RemoteAddr IPv6** — `RemoteAddr = "[::1]:8080"` → returns `::1`.
7. **RemoteAddr without port** — `RemoteAddr = "192.0.2.1"` → returns `192.0.2.1`.
8. **X-Forwarded-For takes precedence over X-Real-Ip** — Both headers set; `X-Forwarded-For` wins.
9. **Empty X-Forwarded-For falls through** — `X-Forwarded-For: ""` → falls through to `X-Real-Ip` or `RemoteAddr`.

#### Handler (`internal/handler/waitinglist_test.go`)

10. **IP passed to store** — Mock the store and verify that `Add` receives the extracted IP address.
11. **IP in response** — Verify the `ip_address` field appears in the JSON response for `POST /waitinglist`.
12. **IP in GET response** — Verify `ip_address` appears in `GET /waitinglist` entries.

#### Repository (`internal/repository/waitinglist_test.go`)

13. **IP stored and retrieved** — (Integration test, gated by `TEST_DATABASE_URL`) Insert an entry with an IP and verify it is returned correctly.
14. **NULL IP for legacy rows** — Rows without an IP return `nil` for `IPAddress`.

### Edge Cases & Negative Scenarios

15. **IPv6 address in X-Forwarded-For** — `X-Forwarded-For: 2001:db8::1` → stored correctly as INET.
16. **No headers at all** — Gracefully falls back to `RemoteAddr`.
17. **Invalid IP string** — If an unparseable value reaches the DB, the `::inet` cast will cause the insert to fail; the handler should return `500`. (Alternatively, validate before inserting.)

---

## Acceptance Criteria

- [ ] `waiting_list` table has a nullable `ip_address` column of type `INET`.
- [ ] `POST /waitinglist` extracts the client IP (respecting `X-Forwarded-For` and `X-Real-Ip` headers) and stores it.
- [ ] `GET /waitinglist` responses include the `ip_address` field for each entry.
- [ ] Existing rows with no IP are returned with `ip_address: null` (or omitted from JSON).
- [ ] All existing tests continue to pass.
- [ ] New unit tests cover IP extraction (all header combinations), handler integration, and repository persistence.
- [ ] `make format`, `make lint`, and `make test` all pass.
