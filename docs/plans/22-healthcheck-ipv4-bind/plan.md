# Plan 22 — Fix Health Check: Explicit IPv4 Bind Address

## Overview

The Docker HEALTHCHECK fails with:

```
"Get \"http://127.0.0.1:8080/healthz\": dial tcp 127.0.0.1:8080: connect: connection refused"
```

The probe binary correctly targets `127.0.0.1` (plan 21), but the server is unreachable at that address.

**Root cause:** `http.ListenAndServe` calls `net.Listen("tcp", ":8080")` internally. On Linux, Go resolves the unqualified `"tcp"` network by attempting an IPv6 socket first. In distroless containers where the kernel sysctl `net.ipv6.conf.all.bindv6only` (`IPV6_V6ONLY`) is set to `1`, the resulting socket is IPv6-only (`[::]:8080`). IPv4 clients (including `127.0.0.1`) are then rejected with "connection refused" even though the server process is running.

The fix is to explicitly bind to `0.0.0.0:port` (IPv4 wildcard), which is always reachable via `127.0.0.1` regardless of IPv6 configuration.

## Requirements

- Server MUST bind on the IPv4 wildcard address (`0.0.0.0`).
- `GET http://127.0.0.1:<port>/healthz` MUST succeed from within the container.
- External IPv4 clients MUST still be able to reach all endpoints.
- Configured port (config file or `WL_PORT`) MUST still be respected.
- No change to any other server behavior.

## Design

### Listen address change

In `cmd/server/main.go`, change:

```go
addr := fmt.Sprintf(":%d", cfg.Port)
```

to:

```go
addr := fmt.Sprintf("0.0.0.0:%d", cfg.Port)
```

This pins the bind to the IPv4 wildcard. `net.Listen` will create a `tcp4` socket, guaranteed to accept connections from `127.0.0.1`.

### Dockerfile HEALTHCHECK timing (defensive hardening)

The current `--start-period=15s` may be too short when PostgreSQL is slow to accept connections or migrations take time. Increase it to `30s` to give the server a longer initialization window before failures count toward `--retries`.

```dockerfile
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD ["/waitinglist", "--health-check"]
```

## Implementation Steps

- [x] **1.** In `cmd/server/main.go`, update the listen address:
  ```go
  // Before
  addr := fmt.Sprintf(":%d", cfg.Port)
  // After
  addr := fmt.Sprintf("0.0.0.0:%d", cfg.Port)
  ```
  Update the two `logger.Info` calls below it to log the full address so it is obvious in logs.

- [x] **2.** In `Dockerfile`, increase `--start-period` from `15s` to `30s`:
  ```dockerfile
  HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
      CMD ["/waitinglist", "--health-check"]
  ```

- [x] **3.** Run `make format && make lint && make test` and confirm all pass.

- [x] **4.** Build the Docker image locally and start a container; verify:
  - The server log shows `addr=0.0.0.0:8080`.
  - `docker inspect --format='{{json .State.Health}}' <container>` reports `"Status":"healthy"`.

## Testing

### Unit tests

The listen-address is a one-liner change with no branching logic; dedicated unit tests for it are not meaningful. The existing `TestParseFlags` and `TestResolveHealthCheckPort` tests in `cmd/server/` should still pass unchanged.

### Integration / smoke test

```bash
# Build image
make docker-build:arm64   # or amd64

# Run container (requires a reachable Postgres and config file)
docker run --rm \
  -e WL_PORT=8080 \
  -v $(pwd)/config.json:/config.json \
  waitinglist:latest

# In a second shell, invoke the probe binary directly
docker exec <container> /waitinglist --health-check
# Expected exit code: 0
```

## Acceptance Criteria

- [x] `docker inspect` shows `"Status":"healthy"` (not `"unhealthy"` or `"starting"` indefinitely).
- [x] Server logs contain `addr=0.0.0.0:<port>` on startup.
- [x] `make format`, `make lint`, and `make test` all pass.
- [x] No change to the public API or admin endpoints.
