# Plan 21: Health-Check Probe — Use IPv4 Loopback Instead of `localhost`

## Overview

The Docker `HEALTHCHECK` introduced in plan 19 (and decoupled from config in
plan 20) probes `http://localhost:<port>/healthz`. In the distroless runtime
container, `localhost` resolves to **IPv6** (`::1`) before IPv4 (`127.0.0.1`),
but the Go server bound on `":<port>"` ends up listening on the IPv4 wildcard
only in this environment. The probe therefore connects to `[::1]:8080` and
gets a TCP RST:

```json
{"level":"ERROR","msg":"health check failed",
 "error":"Get \"http://localhost:8080/healthz\":
          dial tcp [::1]:8080: connect: connection refused"}
```

The container stays in `Status: starting` (the failures are within
`--start-period`), but every probe fails because of the address-family
mismatch — once the start window elapses, Docker marks it `unhealthy`.

This plan replaces `localhost` with `127.0.0.1` in the probe so the probe
talks directly to the IPv4 stack the server is actually listening on.

## Root Cause

Three facts combine:

1. **Server bind address.** `cmd/server/main.go` builds the listener address
   as `addr := fmt.Sprintf(":%d", cfg.Port)` and passes it to
   `http.Server.ListenAndServe()`. In the `gcr.io/distroless/base-debian13`
   runtime image (and on the Docker Desktop / containerd Linux VM that runs
   it), this binds **only** to the IPv4 wildcard `0.0.0.0:<port>`. There is
   no IPv6 socket.

2. **Probe target hostname.** `probeHealth` does
   `http.Client.Get("http://localhost:<port>/healthz")`. Go's DNS resolver
   reads `/etc/hosts`, which in distroless lists `::1 localhost` alongside
   `127.0.0.1 localhost`, and the Go resolver returns IPv6 candidates first
   when an IPv6 stack is present.

3. **Connection refused, not a timeout.** Because no socket is listening on
   `[::1]:<port>`, the kernel returns RST immediately. Go's `http.Client`
   does **not** transparently fall through to the next address from the
   resolver in this scenario for the duration of a single dial attempt — it
   surfaces the first connection error.

Net effect: every probe fails despite the server being healthy.

## Why `127.0.0.1` is the right fix

- It eliminates DNS / `/etc/hosts` resolution entirely; the URL is parsed as
  an IP literal and dialed directly over IPv4.
- It matches the address family the server is actually bound to in this
  image, so the connect succeeds.
- It does **not** change how the server binds, so external traffic
  (`docker run -p 8080:8080`, Kubernetes service routing, etc.) keeps
  working exactly as before. Only the in-container probe path changes.
- The unit tests already exercise `probeHealth` against
  `httptest.NewServer`, which itself listens on `127.0.0.1` — so the change
  brings the production path in line with what the tests cover.

Alternatives considered and rejected:

- **Bind the server explicitly to `[::]:<port>` (IPv6 dual-stack).** Works
  on Linux with `IPV6_V6ONLY=0` (default), but is sensitive to the host
  kernel/container settings. Riskier than a one-line probe change for no
  user-visible benefit.
- **Try IPv4 then IPv6 in `probeHealth`.** Adds branching for a problem the
  IPv4 literal already solves.
- **Set `GODEBUG=netdns=...` to force IPv4 resolution.** Hidden behavior
  change; harder to discover than a literal IP in the URL.

## Requirements

1. `probeHealth` must target `http://127.0.0.1:<port>/healthz`.
2. The change must be limited to the probe path. Server bind, config
   loading, flag parsing, and Dockerfile HEALTHCHECK CMD remain unchanged.
3. Existing `probeHealth` tests (success / unhealthy / unreachable) must
   continue to pass without modification — they already assume IPv4
   loopback via `httptest`.
4. `make format`, `make lint`, and `make test` must pass.

## Design

One-line change in `cmd/server/main.go`:

```go
func probeHealth(port int) error {
    target := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
    // ...rest unchanged
}
```

No new dependencies, no new flags, no new env vars. The fix is intentionally
minimal because the failure mode is precisely localized.

## Implementation Steps

1. **Edit `cmd/server/main.go`** — replace `localhost` with `127.0.0.1` in
   the `target` URL inside `probeHealth`.
2. **Verify tests still pass** — run `make test`. The existing
   `probeHealth` tests use `httptest.NewServer.Listener.Addr().(*net.TCPAddr).Port`,
   and `httptest` listens on `127.0.0.1`, so the new probe URL resolves to
   the same listener.
3. **Run `make format` and `make lint`.**
4. **Update `CLAUDE.md`** — add plan `21-healthcheck-ipv4-loopback` as
   ✅ Complete.
5. **Smoke verification (optional but recommended)** — rebuild the image,
   start the container with the same env that previously failed, and
   confirm `docker inspect <id>` reports `Status: healthy` once the
   start-period elapses.

## Testing

The existing tests already cover the three meaningful states:

| Existing test | Covers |
|---|---|
| `TestProbeHealth_Success` | 200 from a `httptest` server on 127.0.0.1 |
| `TestProbeHealth_Unhealthy` | 503 from a `httptest` server on 127.0.0.1 |
| `TestProbeHealth_Unreachable` | RST when no server is listening |

No new tests are added because the change is purely a hostname-to-IP
substitution; the existing tests prove the IPv4 loopback path works end to
end. Adding a hand-rolled IPv6 listener test would not increase confidence
in the production fix and would couple the suite to host network details.

## Acceptance Criteria

- [x] `probeHealth` issues its GET against `http://127.0.0.1:<port>/healthz`.
- [x] All existing `probeHealth` tests pass without modification.
- [x] Server bind logic and Dockerfile HEALTHCHECK CMD are unchanged.
- [x] `make format`, `make lint`, and `make test` all pass.
- [x] In a deployment that previously logged `dial tcp [::1]:8080: connect:
      connection refused`, the container reports `healthy` after
      `--start-period` elapses.
