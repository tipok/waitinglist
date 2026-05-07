# Plan 20: Decouple Docker HEALTHCHECK from Config File

## Overview

The Dockerfile `HEALTHCHECK` introduced in plan 19 invokes
`/waitinglist --config /config.json --health-check`. The image, however, does
**not** ship a `/config.json` — that path is reserved for a user-mounted
config at runtime, and many deployments mount their config elsewhere (or rely
purely on `WL_*` env vars). When `/config.json` is absent, the health-check
binary exits `1` before it can probe `/healthz`:

```json
{"level":"ERROR","msg":"Error loading config",
 "error":"loading config file: open /config.json: no such file or directory"}
```

Docker therefore reports the container as `unhealthy` even though the server
itself is running and serving traffic. This plan removes the dependency on a
config file from the health-check path.

## Root Cause

`main.go` calls `config.Load(flags.ConfigPath)` **before** branching on
`flags.HealthCheck`. `config.Load` requires the file to exist (the koanf file
provider returns an error otherwise), so any environment without a config
file at the path baked into the `HEALTHCHECK` instruction will fail the probe
regardless of whether the server is healthy.

The probe only needs **one** value from config: the port to hit. The fix is
to short-circuit the health-check path so it never opens the config file.

## Requirements

1. `--health-check` mode must **not** read or require a config file.
2. The probe must determine the port from inputs available in any deployment:
   - explicit `--port N` flag, or
   - `WL_PORT` env var (matches the existing koanf env convention), or
   - the default port (`config.DefaultPort` = 8080).
3. The Dockerfile `HEALTHCHECK` must work in the default image without any
   mounted file.
4. Server (non-health-check) startup behavior is unchanged: `config.Load`
   still runs and still requires `--config` to point to a real file.
5. All existing tests continue to pass; new tests cover the port-resolution
   precedence.

## Design

### Flag set additions

Extend `config.Flags` with a `Port` field and register a `--port` flag:

```go
type Flags struct {
    ConfigPath  string
    HealthCheck bool
    Port        int // 0 means "not set"
}
```

`--port` defaults to `0`; the value is only consulted in health-check mode.
We do not change the server-startup port resolution — the server always uses
`cfg.Port` as today.

### Port resolution helper

Add a small helper (in `cmd/server/main.go` or `internal/config`) that
applies the precedence chain:

```go
// resolveHealthCheckPort picks the port for the /healthz probe without
// requiring a config file. Precedence: --port flag > WL_PORT env > default.
func resolveHealthCheckPort(flagPort int) int {
    if flagPort > 0 {
        return flagPort
    }
    if v := os.Getenv("WL_PORT"); v != "" {
        if n, err := strconv.Atoi(v); err == nil && n > 0 {
            return n
        }
    }
    return config.DefaultPort
}
```

This deliberately reads `WL_PORT` directly rather than going through koanf,
because koanf's env provider is intertwined with the file provider in
`config.Load`. A direct env lookup keeps the health-check path zero-deps.

### `main.go` reorder

Move the health-check branch **above** `config.Load`:

```go
flags, err := config.ParseFlags(os.Args[1:])
if err != nil { ... }

if flags.HealthCheck {
    runHealthCheck(logger, resolveHealthCheckPort(flags.Port))
    // os.Exit inside; never returns.
}

cfg, err := config.Load(flags.ConfigPath)
// ... unchanged from here
```

### Dockerfile change

The HEALTHCHECK no longer references the config file:

```dockerfile
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD ["/waitinglist", "--health-check"]
```

Deployments that run on a non-default port can either set `WL_PORT` (which
the server already honors) or override the HEALTHCHECK via `docker run
--health-cmd` / Kubernetes `livenessProbe`.

## Implementation Steps

1. **Extend `config.Flags`** (`internal/config/config.go`)
   - Add `Port int` field.
   - Register `--port` flag (default `0`); store result in `Flags.Port`.
   - Existing `--config` / `--health-check` parsing untouched.

2. **Reorder `main.go`** (`cmd/server/main.go`)
   - Move `if flags.HealthCheck { runHealthCheck(...) }` to immediately
     after `ParseFlags`, **before** `config.Load`.
   - Pass `resolveHealthCheckPort(flags.Port)` instead of `cfg.Port`.
   - Add `resolveHealthCheckPort` helper.

3. **Update Dockerfile**
   - Drop `--config /config.json` from the HEALTHCHECK CMD.

4. **Tests**
   - Add `TestParseFlags_PortFlag` (sets `Port` correctly).
   - Add `TestParseFlags_PortDefaultsToZero` (no flag → `Port == 0`).
   - Add `TestResolveHealthCheckPort_FlagWins` — flag > env > default.
   - Add `TestResolveHealthCheckPort_EnvUsedWhenFlagZero` (use
     `t.Setenv("WL_PORT", "9999")`).
   - Add `TestResolveHealthCheckPort_DefaultWhenNothingSet`
     (use `t.Setenv("WL_PORT", "")` to neutralize ambient env).
   - Add `TestResolveHealthCheckPort_InvalidEnvFallsBack`
     (e.g. `WL_PORT=abc` → returns default).

5. **Update `CLAUDE.md`**
   - Add plan `20-healthcheck-config-decouple` to the table as ✅ Complete
     once implemented.

6. **Verify** — run `make format`, `make lint`, `make test`. Optionally
   rebuild the image and confirm `docker inspect` shows the new HEALTHCHECK
   command, then run a smoke test where the container is started **without**
   any mounted `/config.json` (using `WL_*` env vars) and confirm the
   container reports `healthy`.

## Testing

All new behavior is covered by unit tests in
`internal/config/config_test.go` and `cmd/server/main_test.go`:

| Test | What it proves |
|---|---|
| `TestParseFlags_PortFlag` | `--port 1234` populates `Flags.Port`. |
| `TestParseFlags_PortDefaultsToZero` | Flag absent → `Port == 0`. |
| `TestResolveHealthCheckPort_FlagWins` | Non-zero flag overrides env. |
| `TestResolveHealthCheckPort_EnvUsedWhenFlagZero` | `WL_PORT` honored. |
| `TestResolveHealthCheckPort_DefaultWhenNothingSet` | Falls back to 8080. |
| `TestResolveHealthCheckPort_InvalidEnvFallsBack` | Junk env → default. |

Existing `probeHealth` tests are unaffected; existing `ParseFlags` /
`Load` tests must continue to pass without modification (the `--port` flag
is purely additive).

## Acceptance Criteria

- [ ] `--health-check` mode no longer calls `config.Load` and no longer
      requires `/config.json` (or any config file) to exist.
- [ ] Port resolution precedence is `--port` → `WL_PORT` → `DefaultPort`,
      with unit tests covering each branch.
- [ ] Dockerfile HEALTHCHECK CMD is `["/waitinglist", "--health-check"]`
      (no `--config` argument).
- [ ] Running the container without a mounted config (env-only configuration)
      reports `healthy` once the server is up.
- [ ] `make format`, `make lint`, and `make test` all pass.
