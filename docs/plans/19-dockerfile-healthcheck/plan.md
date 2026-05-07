# Plan 19: Docker HEALTHCHECK for Distroless Image

## Overview

Add a `HEALTHCHECK` instruction to the Dockerfile so that Docker/Kubernetes can probe container liveness. The final image is `gcr.io/distroless/base-debian13:nonroot`, which has no shell, `curl`, or `wget`. The health check command must therefore be a binary already present in the image. This plan extends the existing `waitinglist` binary with a `--health-check` flag that performs an HTTP GET to `/healthz` and exits `0`/`1`, then wires it into the Dockerfile.

## Requirements

1. **No new binary** — reuse the existing `waitinglist` binary; add a `--health-check` flag to it.
2. **Port-aware** — the health check must target the configured port, not a hardcoded value.
3. **Config-driven** — the flag is used alongside `--config` so the binary reads the port from the same config file the server uses.
4. **Minimal timeout** — the HTTP client in health-check mode must have a short, bounded timeout (≤ 5 s) so Docker doesn't hang waiting.
5. **Exit code semantics** — exit `0` on HTTP 200, exit `1` on any error (connection failure, non-200 status).
6. **HEALTHCHECK parameters** — reasonable defaults: `--interval=30s`, `--timeout=5s`, `--start-period=15s` (migration time buffer), `--retries=3`.

## Design

### Why not `curl`/`wget`?

The `distroless/base-debian13:nonroot` image contains only the C runtime and some CA certificates — no shell, no network utilities. Copying `curl` from a builder stage is fragile (shared library dependencies) and bloats the image. The cleanest solution is a built-in mode of the existing binary.

### `ParseFlags` refactor

`config.ParseFlags` currently returns `(configPath string, err error)`. Extend it to return a `Flags` struct so callers get all parsed CLI state in one place without an expanding return-value list:

```go
// Flags holds the parsed CLI arguments.
type Flags struct {
    ConfigPath  string
    HealthCheck bool
}

func ParseFlags(args []string) (Flags, error)
```

The flag set gains one new flag:

```go
healthCheck := fs.Bool("health-check", false, "probe /healthz and exit 0/1 (for Docker HEALTHCHECK)")
```

### `main.go` health-check path

After parsing flags and loading config, but **before** opening the database:

```go
if flags.HealthCheck {
    runHealthCheck(cfg.Port)
    // runHealthCheck always calls os.Exit — control never returns here.
}
```

`runHealthCheck` is a package-level function in `main`:

```go
func runHealthCheck(port int) {
    url := fmt.Sprintf("http://localhost:%d/healthz", port)
    client := &http.Client{Timeout: 5 * time.Second}
    resp, err := client.Get(url)
    if err != nil {
        fmt.Fprintf(os.Stderr, "health check failed: %v\n", err)
        os.Exit(1)
    }
    _ = resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        fmt.Fprintf(os.Stderr, "health check failed: status %d\n", resp.StatusCode)
        os.Exit(1)
    }
    os.Exit(0)
}
```

### Dockerfile change

Add the `HEALTHCHECK` instruction to the runtime stage, just before `ENTRYPOINT`:

```dockerfile
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD ["/waitinglist", "--config", "/config.json", "--health-check"]
```

The `--start-period=15s` ensures Docker does not count probe failures against the retry budget while the server is running DB migrations on startup.

## Implementation Steps

1. **Refactor `config.ParseFlags`** (`internal/config/config.go`)
   - Define `Flags` struct with `ConfigPath string` and `HealthCheck bool`.
   - Update `ParseFlags` to register `--health-check` flag and return `Flags`.
   - Update all callers (only `cmd/server/main.go` and any tests).

2. **Add `runHealthCheck` to `main.go`** (`cmd/server/main.go`)
   - After `config.Load`, before the database open: check `flags.HealthCheck` and call `runHealthCheck`.
   - Implement `runHealthCheck(port int)` as described above.

3. **Add `HEALTHCHECK` to Dockerfile**
   - Insert the instruction in the runtime stage, just above `ENTRYPOINT`.

4. **Update CLAUDE.md**
   - Mark plan `19-dockerfile-healthcheck` as Complete in the Plans table.

5. **Verify** — `make format`, `make lint`, `make test`, then build an image and inspect with `docker inspect <image> | grep -A5 Healthcheck`.

## Testing

### Unit tests for `ParseFlags` (`internal/config/config_test.go`)

- `--health-check` flag sets `Flags.HealthCheck = true`.
- Without the flag, `HealthCheck` is `false`.
- Existing tests for `--config` parsing still pass after the signature change.

### Unit test for `runHealthCheck` (`cmd/server/main_test.go` or new file)

Because `runHealthCheck` calls `os.Exit`, test it via a subprocess pattern (`exec.Command(os.Args[0], "-test.run=TestHelperHealthCheck", ...)`) or by extracting the HTTP logic into a testable helper that returns an error instead of exiting. The latter is preferred:

```go
// probeHealth is the testable core; main calls it and exits on error.
func probeHealth(port int) error {
    url := fmt.Sprintf("http://localhost:%d/healthz", port)
    client := &http.Client{Timeout: 5 * time.Second}
    resp, err := client.Get(url)
    if err != nil {
        return err
    }
    _ = resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("status %d", resp.StatusCode)
    }
    return nil
}
```

Tests cover:
- `probeHealth` returns `nil` when server responds 200.
- `probeHealth` returns an error when server responds 503.
- `probeHealth` returns an error when the server is not reachable.

All three use `httptest.NewServer` (or simply close the server before calling) to exercise each path without spawning a real process.

## Acceptance Criteria

- [ ] `config.ParseFlags` returns a `Flags` struct; all existing call sites updated.
- [ ] `--health-check` flag is parsed and respected in `main.go`.
- [ ] `probeHealth` is unit-tested for success, unhealthy, and unreachable cases.
- [ ] `Dockerfile` contains a `HEALTHCHECK` instruction using the binary itself.
- [ ] `make format`, `make lint`, and `make test` all pass.
- [ ] `docker inspect <image>` shows a configured `Healthcheck` with the correct `CMD`.
