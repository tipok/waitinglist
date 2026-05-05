# 15 — Health Check Endpoint

## Overview

Expose a lightweight HTTP health check at `GET /healthz` that verifies the service is up **and** that its PostgreSQL connection is reachable. The endpoint is intended for orchestrator probes (Docker / Kubernetes liveness & readiness, load-balancer health checks, uptime monitoring) and should be cheap, unauthenticated, and predictable.

### Key Design Decisions

| Decision | Rationale |
|---|---|
| Path is `/healthz` | Convention popularized by Kubernetes; reserved for health probes and unlikely to collide with future business endpoints. |
| Single endpoint covering liveness + readiness | The service is small and the DB is its only critical dependency. A single `/healthz` that pings the DB is sufficient; we can split into `/livez` and `/readyz` later if requirements diverge. |
| Use `db.PingContext` with a short timeout | `Ping` is the canonical lightweight DB liveness check in `database/sql`. A timeout (default `2s`) prevents a hung DB from blocking probes and consuming server goroutines. |
| Status codes: `200 OK` healthy / `503 Service Unavailable` unhealthy | `503` is the standard response for "the service exists but a dependency is failing" and is what most orchestrators expect. |
| JSON body with `status` + `checks` map | Consistent with the rest of the API (which uses JSON), and gives operators a quick readable diagnostic without needing to scrape logs. The body is tiny (~50 bytes) so it does not bloat probe traffic. |
| Skip the `LoggingMiddleware` for `/healthz` | Probes hit this endpoint every few seconds; logging each one floods the log stream and adds noise. We exclude `/healthz` from the request-logging middleware while keeping the `JSONContentType` middleware (the response is JSON). |
| New `HealthHandler` in `internal/handler/health.go` | Mirrors the existing handler package layout (`waitinglist.go`, `response.go`, `middleware.go`). Keeps DB ping logic out of `cmd/server/main.go`. |
| New `Pinger` interface (`PingContext(ctx) error`) | The handler depends on a small interface, not on `*sql.DB`. Lets us unit-test the handler with a fake pinger and matches the repository-pattern style already used in this codebase. |

### Dependencies

- Builds on: [01-project-setup](../01-project-setup/plan.md), [02-database](../02-database/plan.md).
- No new Go module dependencies.

---

## Requirements

1. **Endpoint** — `GET /healthz` returns:
   - `200 OK` with `{"status":"ok","checks":{"database":"ok"}}` when the DB ping succeeds.
   - `503 Service Unavailable` with `{"status":"unhealthy","checks":{"database":"<error message>"}}` when the DB ping fails or times out.
2. **Method scope** — Only `GET` is allowed. `ServeMux`'s method-prefix routing (`GET /healthz`) automatically rejects other methods with `405 Method Not Allowed`.
3. **No auth** — The endpoint is open. It does not expose secrets — only a boolean-style health summary plus an error string when unhealthy.
4. **Timeout** — DB ping is bounded by a context timeout (default `2s`, configurable via constant). Exceeding the timeout yields `503`.
5. **Logging exclusion** — `/healthz` requests are not logged by `LoggingMiddleware`. Failures (DB ping errors) are logged at `Warn` level by the handler itself so operators still see real problems.
6. **Tests** — Unit tests cover the healthy path, the unhealthy path (DB ping error), the timeout path, and the method-not-allowed path. Middleware test covers the logging-skip behavior.

---

## Design

### New handler (`internal/handler/health.go`)

```go
package handler

import (
    "context"
    "log/slog"
    "net/http"
    "time"
)

// Pinger is the minimal interface needed to verify connectivity to a backing
// store. *sql.DB satisfies this via its PingContext method.
type Pinger interface {
    PingContext(ctx context.Context) error
}

// healthCheckTimeout bounds the database ping during /healthz probes.
const healthCheckTimeout = 2 * time.Second

// HealthHandler serves the /healthz endpoint.
type HealthHandler struct {
    db     Pinger
    logger *slog.Logger
}

// NewHealthHandler creates a new HealthHandler.
func NewHealthHandler(db Pinger, logger *slog.Logger) *HealthHandler {
    return &HealthHandler{db: db, logger: logger}
}

// RegisterRoutes registers /healthz on the given mux.
func (h *HealthHandler) RegisterRoutes(mux *http.ServeMux) {
    mux.HandleFunc("GET /healthz", h.handle)
}

type healthResponse struct {
    Status string            `json:"status"`
    Checks map[string]string `json:"checks"`
}

func (h *HealthHandler) handle(w http.ResponseWriter, r *http.Request) {
    ctx, cancel := context.WithTimeout(r.Context(), healthCheckTimeout)
    defer cancel()

    if err := h.db.PingContext(ctx); err != nil {
        h.logger.Warn("health check: database ping failed", "error", err)
        WriteJSON(w, http.StatusServiceUnavailable, healthResponse{
            Status: "unhealthy",
            Checks: map[string]string{"database": err.Error()},
        }, h.logger)
        return
    }

    WriteJSON(w, http.StatusOK, healthResponse{
        Status: "ok",
        Checks: map[string]string{"database": "ok"},
    }, h.logger)
}
```

### Wiring in `cmd/server/main.go`

After the existing handler construction:

```go
healthHandler := handler.NewHealthHandler(db, logger)

mux := http.NewServeMux()
waitListHandler.RegisterRoutes(mux)
healthHandler.RegisterRoutes(mux)
```

`*sql.DB` already implements `PingContext`, so it satisfies `handler.Pinger` without an adapter.

### Middleware change (`internal/handler/middleware.go`)

Update `LoggingMiddleware` to skip `/healthz`:

```go
func LoggingMiddleware(next http.Handler, logger *slog.Logger) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path == "/healthz" {
            next.ServeHTTP(w, r)
            return
        }
        start := time.Now()
        rec := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
        next.ServeHTTP(rec, r)
        logger.Info("request",
            "method", r.Method,
            "path", r.URL.Path,
            "status", rec.statusCode,
            "duration", time.Since(start),
        )
    })
}
```

The `JSONContentType` middleware is left unchanged — `/healthz` legitimately returns JSON.

### Response shape

Healthy:

```json
{
  "status": "ok",
  "checks": {
    "database": "ok"
  }
}
```

Unhealthy (e.g. DB down):

```json
{
  "status": "unhealthy",
  "checks": {
    "database": "pq: connection refused"
  }
}
```

> The error string is the raw `err.Error()` from `PingContext`. This may include host/port information from the driver — acceptable since the connection target is configured by the operator and not user-supplied data, but flag for review if the deployment treats DB host as sensitive. See **Open Questions**.

---

## Implementation Steps

1. **Create handler** — Add `internal/handler/health.go` with the `Pinger` interface, `HealthHandler`, and `RegisterRoutes`.
2. **Update logging middleware** — Skip `/healthz` in `LoggingMiddleware` (`internal/handler/middleware.go`).
3. **Wire up in main** — Construct `HealthHandler` in `cmd/server/main.go` and call `RegisterRoutes` on the mux.
4. **Add handler tests** — `internal/handler/health_test.go` covering healthy, unhealthy, timeout, and method-not-allowed cases (using a fake `Pinger`).
5. **Update middleware tests** — Extend `internal/handler/middleware_test.go` to assert `/healthz` requests are not logged.
6. **Update routes test** — If `routes_test.go` enumerates registered routes, add `/healthz` there.
7. **Update `CLAUDE.md`** — Add `15-health-check` to the plans table with status `Not started`, then mark `✅ Complete` when the implementation lands.
8. **Run formatters/linters/tests** — `make format && make lint && make test`.

---

## Testing

### Handler unit tests (`internal/handler/health_test.go`)

| # | Test Case | Setup | Expected Result |
|---|---|---|---|
| 1 | Healthy — DB ping succeeds | `Pinger.PingContext` returns `nil` | `200 OK`, body `{"status":"ok","checks":{"database":"ok"}}`, `Content-Type: application/json` |
| 2 | Unhealthy — DB ping returns error | `Pinger.PingContext` returns `errors.New("connection refused")` | `503`, body `{"status":"unhealthy","checks":{"database":"connection refused"}}`, warn log emitted |
| 3 | Unhealthy — DB ping times out | `Pinger.PingContext` blocks until ctx is cancelled, then returns `ctx.Err()` | `503`, body's `database` field starts with `"context deadline exceeded"`; total handler duration ≤ ~`healthCheckTimeout + small slack` |
| 4 | Method not allowed | `POST /healthz` | `405 Method Not Allowed` (handled by `ServeMux`'s method routing; no body assertion required) |
| 5 | Path not registered without handler | Removing the handler from the mux returns `404` for `/healthz` | Sanity check that the route is not auto-registered elsewhere |

### Middleware unit tests (`internal/handler/middleware_test.go`)

| # | Test Case | Setup | Expected Result |
|---|---|---|---|
| 6 | `LoggingMiddleware` skips `/healthz` | Wrap a no-op handler, capture log output via a `slog.Handler` that records records, send `GET /healthz` | No log record is emitted for the request |
| 7 | `LoggingMiddleware` still logs other paths | Send `GET /waitinglist` through the same wrapped handler | One `request` log record is emitted with `"path"="/waitinglist"` |

### Fake `Pinger` for tests

```go
type fakePinger struct {
    err   error
    block bool
}

func (f *fakePinger) PingContext(ctx context.Context) error {
    if f.block {
        <-ctx.Done()
        return ctx.Err()
    }
    return f.err
}
```

For test #3 (timeout), override `healthCheckTimeout` via a small package-level variable or thread the timeout through `NewHealthHandler` — see **Open Questions**.

### Edge cases

| # | Test Case | Description |
|---|---|---|
| 8 | DB temporarily unhealthy then recovers | Two sequential requests, first returns `503`, second returns `200`; verifies handler is stateless |
| 9 | Probe under load | Optional benchmark / load test: `/healthz` should respond in ≤ ~5 ms when the DB is healthy. Not required for merge, but useful for tuning the orchestrator probe period. |

---

## Acceptance Criteria

- [ ] `GET /healthz` returns `200 OK` with `{"status":"ok","checks":{"database":"ok"}}` when the DB is reachable.
- [ ] `GET /healthz` returns `503 Service Unavailable` with `{"status":"unhealthy","checks":{"database":"<err>"}}` when `db.PingContext` returns an error.
- [ ] DB ping is bounded by a `2s` timeout — the handler responds within that window even if the DB hangs.
- [ ] `/healthz` requests are **not** emitted by `LoggingMiddleware`; failures are still logged at `Warn` from the handler.
- [ ] `POST /healthz` (or any non-GET method) returns `405 Method Not Allowed`.
- [ ] `cmd/server/main.go` registers the health handler on the same mux as the waiting list handler.
- [ ] All new and existing handler / middleware tests pass.
- [ ] `make format`, `make lint`, and `make test` all pass.
- [ ] `CLAUDE.md` plans table references plan `15`.

---

## Open Questions

1. **Configurable timeout?** — The plan hard-codes `healthCheckTimeout = 2 * time.Second` as a package constant. If operators need to tune it without a recompile (e.g. very slow staging DBs), expose it via `config.json` (e.g. `health.db_timeout_seconds`). Default in this plan: hard-coded constant; trivial to lift later.
2. **Expose DB error verbatim?** — The unhealthy body includes `err.Error()`. If the DB driver leaks host/port and the deployment considers that sensitive, replace with a static `"unreachable"` string and log the full error server-side only.
3. **Split into `/livez` + `/readyz`?** — Some orchestrators differentiate liveness (process is up) from readiness (dependencies are up). For now `/healthz` answers both; revisit if Kubernetes deployment requires the split.
4. **Include build/version info?** — A common addition is `{"version":"<git-sha>"}` in the body. Out of scope for this plan; would be a follow-up once a build-info injection mechanism exists.
