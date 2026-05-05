# 17 — Admin API & Basic Auth

## Overview

Expose a set of authenticated `/admin/*` HTTP endpoints that back the new admin page (UI in [plan 18](../18-admin-web-ui/plan.md)). The endpoints surface dashboard metrics, two filterable user lists (with-access / waiting-list), and three mutating actions (admin-grant, admin-revoke, waitlist-delete). All `/admin/*` routes are protected by HTTP Basic Auth whose credentials are configured in `config.json`.

This plan **depends on** [plan 16](../16-access-audit-and-revocation/plan.md) — the audit columns, `GrantAccessTx`/`RevokeAccessTx`, and the dropped one-way trigger must be in place first.

### Key Design Decisions

| Decision | Rationale |
|---|---|
| Path prefix `/admin/` | Clean, conventional, easy to match in middleware. |
| One basic-auth middleware applied to a sub-mux | All `/admin/*` routes share the auth check; the sub-mux pattern lets us keep `/healthz` and `/waitinglist` outside auth without per-route guards. |
| Credentials configurable via `config.json` under `admin.basic_auth` | Matches the project's existing config style (koanf JSON file). Env-var override comes for free if/when koanf env layer is added. |
| Password stored as **bcrypt hash** in config (`password_hash`) | Plain-text passwords on disk are a pointless risk when `bcrypt.CompareHashAndPassword` is one stdlib-adjacent dep (`golang.org/x/crypto/bcrypt`). See **Open Question 1**. |
| Single admin user (one username/hash pair) | Matches "configurable in the configuration file" — sufficient for the current scope. Multi-admin can be a follow-up. |
| Constant-time username comparison via `subtle.ConstantTimeCompare` | Prevents timing oracle on the username. The bcrypt compare is already constant-time. |
| `WWW-Authenticate: Basic realm="waitinglist-admin"` on 401 | Browsers will prompt for credentials; curl users get the standard handshake. |
| New `AdminHandler` in `internal/handler/admin.go` | Mirrors the existing handler-per-feature layout. |
| New `AdminUserStore` / `AdminWaitingListStore` interfaces local to the handler | Matches the small-interface pattern already established (`WaitingListUserStore`, `Pinger`). Easy to fake in tests. |
| Mutations use HTTP `POST` (not `PATCH`/`DELETE` for grant/revoke) | Idempotency isn't a clean fit (revoking twice with two different reasons should be two distinct events), and `POST` is universally supported by basic HTML forms used by the admin UI. The single exception is `DELETE /admin/waitlist/{id}` for removing a waiting-list row, which is genuinely idempotent. |
| Dashboard chart returns last 90 days of per-day enlistment counts | Bounded payload; UI can visualize without pagination. Window is configurable via query parameter (`?days=30`). |

### Dependencies

- Builds on: [16-access-audit-and-revocation](../16-access-audit-and-revocation/plan.md) (audit columns, repository helpers).
- Required by: [18-admin-web-ui](../18-admin-web-ui/plan.md).
- New Go module dependency: `golang.org/x/crypto/bcrypt` for password hashing (~1 MB, no transitive deps in our use).

---

## Requirements

### Configuration

1. Extend `config.json` schema with an `admin` section:

   ```json
   {
     "port": 8080,
     "database": { "url": "..." },
     "admin": {
       "basic_auth": {
         "username": "admin",
         "password_hash": "$2a$10$..."
       }
     }
   }
   ```

2. `internal/config/config.go` gains an `Admin.BasicAuth.Username` and `Admin.BasicAuth.PasswordHash` struct. Both are required when **any** `/admin/*` route is registered. If either is empty, the server logs a warning and **disables** the admin routes (it does not crash) — see **Open Question 2**.

### Middleware

3. New `BasicAuthMiddleware(username string, passwordHash []byte, realm string, logger *slog.Logger) func(http.Handler) http.Handler` in `internal/handler/middleware.go`.
   - Reads `Authorization: Basic ...` header.
   - Constant-time username compare; bcrypt password compare.
   - On failure: `401 Unauthorized` with `WWW-Authenticate` header; logs at `Info` (not `Warn` — failed auth is normal probe traffic).
   - On success: chains to the next handler.

### Routes

All routes return JSON.

| Method | Path | Body | Description |
|---|---|---|---|
| `GET` | `/admin/dashboard` | — | Returns counts (`waiting_list`, `with_access`, `total`) plus `enlistments_by_day` for the last `?days=N` (default 90, max 365). |
| `GET` | `/admin/users/access` | — | Lists users with `has_access=true`. Supports `?email=` substring filter (case-insensitive), `?limit=` (default 50, max 200), `?offset=`. Each row includes the audit fields from plan 16. |
| `GET` | `/admin/users/waitlist` | — | Lists current waiting-list rows joined to their users. Includes `weight` and `weighted_created_at`. Same `?email=`/`?limit=`/`?offset=` semantics. |
| `POST` | `/admin/users/{id}/grant-access` | `{}` (empty) | Admin-grants access immediately. Removes the user's waiting-list row (if any). Calls `GrantAccessTx([id], "admin")`. Returns the updated `UserEntity`. |
| `POST` | `/admin/users/{id}/revoke-access` | `{"reason":"…"}` | Admin-revokes access. `reason` required, ≤ 500 chars. Calls `RevokeAccessTx(id, reason, <username>)`. Returns the updated `UserEntity`. |
| `DELETE` | `/admin/waitlist/{id}` | — | Removes a single waiting-list row by its `id` (not user id). Does not touch `user_entity`. |

4. **All `/admin/*` routes require basic auth.** Failed auth → `401`. Other errors map per existing convention: `400` for validation, `404` for missing entities, `500` for internal.
5. **All admin actions log at `Info`** with `admin_user`, `action`, target IDs, and outcome. Revocations also log the (truncated) reason.

### Repository

6. `UserRepository` gains:
   - `CountByAccess(ctx) (waitlistCount, accessCount int, err error)` — single query using `COUNT(*) FILTER (WHERE has_access)`.
   - `EnlistmentsByDay(ctx, days int) ([]DayCount, error)` — groups `user_entity.created_at::date`; returns sorted ascending; zero-fills missing days client-side in the handler.
   - `ListWithAccess(ctx, emailLike string, limit, offset int) ([]UserEntity, error)`.
7. `WaitingListRepository` gains:
   - `ListJoined(ctx, emailLike string, limit, offset int) ([]WaitingListAdminRow, error)` where `WaitingListAdminRow` includes user fields, `weight`, and timestamps.
   - `DeleteByID(ctx, id string) error` — returns `ErrWaitingListEntryNotFound` when no row matches.
   - `DeleteByUserIDTx(ctx, tx, userID string) error` — used by `grant-access` to drop the waitlist row inside the same transaction.

### Tests

8. Handler unit tests with fake stores cover happy paths and all 4xx branches. Middleware tests cover auth pass/fail and timing safety (just check we use the constant-time helpers — no statistical timing test).
9. Integration tests gated by `TEST_DATABASE_URL` cover the new repository methods.

---

## Design

### Config struct (`internal/config/config.go`)

```go
type Config struct {
    Port     int          `koanf:"port"`
    Database DatabaseCfg  `koanf:"database"`
    Admin    AdminCfg     `koanf:"admin"`
}

type AdminCfg struct {
    BasicAuth BasicAuthCfg `koanf:"basic_auth"`
}

type BasicAuthCfg struct {
    Username     string `koanf:"username"`
    PasswordHash string `koanf:"password_hash"`
}
```

`config.json` (example, hash for password "changeme"):

```json
{
  "port": 8080,
  "database": { "url": "..." },
  "admin": {
    "basic_auth": {
      "username": "admin",
      "password_hash": "$2a$10$Yk4Z…replace…"
    }
  }
}
```

### Middleware (`internal/handler/middleware.go`)

```go
import (
    "crypto/subtle"
    "golang.org/x/crypto/bcrypt"
)

func BasicAuthMiddleware(username string, passwordHash []byte, realm string, logger *slog.Logger) func(http.Handler) http.Handler {
    expectedUser := []byte(username)
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            user, pass, ok := r.BasicAuth()
            if !ok {
                unauthorized(w, realm)
                return
            }
            if subtle.ConstantTimeCompare([]byte(user), expectedUser) != 1 {
                unauthorized(w, realm)
                logger.Info("admin auth failed", "reason", "username mismatch", "remote_addr", r.RemoteAddr)
                return
            }
            if err := bcrypt.CompareHashAndPassword(passwordHash, []byte(pass)); err != nil {
                unauthorized(w, realm)
                logger.Info("admin auth failed", "reason", "password mismatch", "remote_addr", r.RemoteAddr)
                return
            }
            // Stash username in context for downstream handlers (used as access_revoked_by).
            ctx := context.WithValue(r.Context(), ctxKeyAdminUser, username)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

func unauthorized(w http.ResponseWriter, realm string) {
    w.Header().Set("WWW-Authenticate", `Basic realm="`+realm+`"`)
    w.WriteHeader(http.StatusUnauthorized)
}
```

### Handler (`internal/handler/admin.go`)

Sketch:

```go
type AdminUserStore interface {
    CountByAccess(ctx context.Context) (int, int, error)
    EnlistmentsByDay(ctx context.Context, days int) ([]model.DayCount, error)
    ListWithAccess(ctx context.Context, emailLike string, limit, offset int) ([]model.UserEntity, error)
    GetByID(ctx context.Context, id string) (*model.UserEntity, error)
    GrantAccessTx(ctx context.Context, tx model.DBTX, ids []string, source string) error
    RevokeAccessTx(ctx context.Context, tx model.DBTX, id, reason, by string) error
}

type AdminWaitingListStore interface {
    ListJoined(ctx context.Context, emailLike string, limit, offset int) ([]model.WaitingListAdminRow, error)
    DeleteByID(ctx context.Context, id string) error
    DeleteByUserIDTx(ctx context.Context, tx model.DBTX, userID string) error
    BeginTx(ctx context.Context) (model.Tx, error)
}

type AdminHandler struct { /* userStore, wlStore, logger */ }

func (h *AdminHandler) RegisterRoutes(mux *http.ServeMux) {
    // Caller wraps each route in BasicAuthMiddleware. Alternatively expose a
    // single Mount(parent *http.ServeMux, mw func(http.Handler) http.Handler).
    mux.HandleFunc("GET /admin/dashboard",                 h.handleDashboard)
    mux.HandleFunc("GET /admin/users/access",              h.handleListWithAccess)
    mux.HandleFunc("GET /admin/users/waitlist",            h.handleListWaitlist)
    mux.HandleFunc("POST /admin/users/{id}/grant-access",  h.handleGrant)
    mux.HandleFunc("POST /admin/users/{id}/revoke-access", h.handleRevoke)
    mux.HandleFunc("DELETE /admin/waitlist/{id}",          h.handleDeleteWaitlist)
}
```

`handleGrant` wraps `DeleteByUserIDTx` and `GrantAccessTx` in a transaction so the waiting-list row is removed atomically with the grant.

`handleRevoke` decodes `{"reason": "..."}`, validates length 1..500, pulls the admin username from `ctx`, and calls `RevokeAccessTx`.

### Wiring (`cmd/server/main.go`)

```go
adminUser := cfg.Admin.BasicAuth.Username
adminHash := []byte(cfg.Admin.BasicAuth.PasswordHash)
if adminUser == "" || len(adminHash) == 0 {
    logger.Warn("admin basic auth not configured; /admin routes disabled")
} else {
    adminHandler := handler.NewAdminHandler(userRepo, waitListRepo, logger)
    auth := handler.BasicAuthMiddleware(adminUser, adminHash, "waitinglist-admin", logger)

    adminMux := http.NewServeMux()
    adminHandler.RegisterRoutes(adminMux)
    mux.Handle("/admin/", auth(adminMux))
}
```

### New model types (`internal/model/model.go`)

```go
type DayCount struct {
    Day   string `json:"day"`   // "2026-05-04"
    Count int    `json:"count"`
}

type WaitingListAdminRow struct {
    EntryID            string    `json:"entry_id"`
    UserID             string    `json:"user_id"`
    Email              string    `json:"email"`
    Firstname          string    `json:"firstname"`
    Lastname           string    `json:"lastname"`
    Weight             int       `json:"weight"`
    CreatedAt          time.Time `json:"created_at"`
    WeightedCreatedAt  time.Time `json:"weighted_created_at"`
}

var ErrWaitingListEntryNotFound = errors.New("waiting list entry not found")
```

### Logging exclusion

Per the convention established in plan 15, do **not** add `/admin/` to the logging exclusion list — admin actions are exactly what we want in the access log.

---

## Implementation Steps

1. **Add bcrypt dep** — `go get golang.org/x/crypto/bcrypt`.
2. **Config** — extend `internal/config/config.go` with `Admin.BasicAuth.{Username, PasswordHash}`. Update `config.json` with placeholder values; document hash generation in a comment (`go run ./cmd/admintool hash 'my-password'` or `htpasswd -nbBC 10 admin pw | cut -d: -f2`).
3. **Middleware** — implement `BasicAuthMiddleware` in `internal/handler/middleware.go` and the `unauthorized` helper. Stash the admin username on the request context with a typed key.
4. **Repository** — implement `CountByAccess`, `EnlistmentsByDay`, `ListWithAccess` on `UserRepository`; `ListJoined`, `DeleteByID`, `DeleteByUserIDTx` on `WaitingListRepository`.
5. **Model** — add `DayCount`, `WaitingListAdminRow`, `ErrWaitingListEntryNotFound`.
6. **Handler** — implement `internal/handler/admin.go` with the six routes per Design.
7. **Wire-up** — in `cmd/server/main.go`, build the admin sub-mux behind the auth middleware. Skip when credentials are missing.
8. **Tests** — handler unit tests (auth bypass via direct call), middleware tests (auth pass/fail), repository integration tests gated by `TEST_DATABASE_URL`.
9. **Docs** — extend the HTTP Endpoints table in CLAUDE.md to include the `/admin/*` routes; note the basic-auth requirement.
10. **Verify** — `make format && make lint && make test`; manual smoke test with curl using basic auth.

---

## Testing

### Middleware (`internal/handler/middleware_test.go`)

| # | Case | Expected |
|---|---|---|
| 1 | No `Authorization` header | `401`, `WWW-Authenticate` set |
| 2 | Wrong username | `401`, `WWW-Authenticate` set |
| 3 | Right username, wrong password | `401` |
| 4 | Right username + right password | `200`, inner handler called, `ctx` carries username |
| 5 | Malformed Authorization header | `401` |

### Handler (`internal/handler/admin_test.go` — fakes for both stores)

| # | Case | Expected |
|---|---|---|
| 6  | `GET /admin/dashboard` happy path | `200`, body has `waiting_list`, `with_access`, `total`, `enlistments_by_day` |
| 7  | `GET /admin/dashboard?days=400` | `400`, body cites max 365 |
| 8  | `GET /admin/users/access?email=foo` | calls `ListWithAccess("foo", 50, 0)`; returns rows |
| 9  | `GET /admin/users/access?limit=1000` | clamps to max 200 |
| 10 | `GET /admin/users/waitlist?offset=-1` | `400` |
| 11 | `POST /admin/users/{id}/grant-access` happy path | tx commits; `DeleteByUserIDTx` then `GrantAccessTx` called in order; `200` with updated user |
| 12 | `POST /admin/users/{id}/grant-access` user has access already | `200` no-op-ish (repository upsert is fine) — explicitly tested to confirm idempotency |
| 13 | `POST /admin/users/{id}/grant-access` user not found | `404` |
| 14 | `POST /admin/users/{id}/revoke-access` empty reason | `400` |
| 15 | `POST /admin/users/{id}/revoke-access` reason 501 chars | `400` |
| 16 | `POST /admin/users/{id}/revoke-access` happy path | `RevokeAccessTx(id, reason, "admin")` called; `200` |
| 17 | `POST /admin/users/{id}/revoke-access` user not found | `404` |
| 18 | `DELETE /admin/waitlist/{id}` happy path | `204` |
| 19 | `DELETE /admin/waitlist/{id}` missing entry | `404` |

### Repository integration (`internal/repository/*_test.go` gated by `TEST_DATABASE_URL`)

| # | Case | Expected |
|---|---|---|
| 20 | `CountByAccess` returns expected counts after seeding | exact match |
| 21 | `EnlistmentsByDay(7)` produces 7 rows with zero-fill | sorted ascending, zeros for empty days |
| 22 | `ListWithAccess("alice")` is case-insensitive | matches `Alice@example.com` |
| 23 | `ListJoined` includes `weight` correctly | round-trips |
| 24 | `DeleteByID` removes a row; second call returns `ErrWaitingListEntryNotFound` | both branches |
| 25 | `DeleteByUserIDTx` inside a tx is atomic with `GrantAccessTx` | rolling back the tx leaves the waitlist row in place |

### End-to-end (manual / curl smoke test)

```bash
HASH=$(htpasswd -nbBC 10 admin changeme | cut -d: -f2)
# put HASH into config.json
curl -u admin:wrong  http://localhost:8080/admin/dashboard           # 401
curl -u admin:changeme http://localhost:8080/admin/dashboard         # 200
curl -u admin:changeme -X POST http://localhost:8080/admin/users/$ID/grant-access
curl -u admin:changeme -X POST -d '{"reason":"abuse"}' \
    http://localhost:8080/admin/users/$ID/revoke-access
```

---

## Acceptance Criteria

- [ ] `config.json` accepts `admin.basic_auth.{username, password_hash}`. Empty values disable `/admin/*` (with a startup warning) instead of crashing.
- [ ] `BasicAuthMiddleware` rejects missing/bad credentials with `401` and `WWW-Authenticate`.
- [ ] All six admin routes are reachable behind auth and return JSON per the spec.
- [ ] `grant-access` runs `DeleteByUserIDTx` and `GrantAccessTx` inside a single transaction; rolling back leaves both untouched.
- [ ] `revoke-access` requires a non-empty reason ≤ 500 chars and stores it via `RevokeAccessTx`.
- [ ] Dashboard `enlistments_by_day` zero-fills days with no signups within the requested window.
- [ ] CLAUDE.md HTTP Endpoints table lists the new routes and their auth requirement.
- [ ] `make format`, `make lint`, and `make test` all pass.

---

## Open Questions

1. **Bcrypt vs. plain-text** — Default in this plan is bcrypt (`password_hash` field). If the team would rather keep the config truly stdlib-only, we can store a plain `password` and document the operator responsibility. Recommendation: stick with bcrypt; the dep is small and the security upside is meaningful when `config.json` ends up in backups, container images, or CI artifacts.
2. **Disable on missing creds vs. crash** — Default: warn + disable. Alternative: require credentials and refuse to start. The "warn + disable" path lets dev environments run without admin config; the strict path catches misconfigurations early in prod. Confirm preference.
3. **Single admin or list?** — Plan covers a single username/hash pair. If multi-admin is anticipated, switch the config to `[]BasicAuthCfg` and the middleware to lookup by username. Default: single admin.
4. **Audit log** — Should we persist admin actions (who, when, what, target) to a new `admin_audit_log` table for forensics? Out of scope here; structured `Info` logs are the only audit trail today.
5. **Rate limiting / brute-force protection** — `bcrypt.CompareHashAndPassword` at cost 10 already throttles attackers to ~10/sec/core. If `/admin` is exposed to the internet, we may want a small in-memory lockout. Not included; flag for review based on deployment shape.
6. **CSRF on POST/DELETE** — Basic auth + same-origin admin UI (plan 18) is sufficient for now (basic auth is sent on every request, no cookie-based session is established server-side). If we ever introduce cookie sessions, revisit.
