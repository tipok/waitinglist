# 25 — Inline Host Mapping into Project Definitions

> **Status:** ✅ Complete

## Overview

Move the `hostMapping` configuration from a top-level `map[string]string` under `projects` into each project's `definitions` entry as a single `string` field. The host-to-slug lookup map is derived at config load time by iterating definitions, eliminating the redundant top-level map.

### Before

```json
{
  "projects": {
    "hostMapping": {
      "beta.localhost": "beta-app",
      "tools.localhost": "internal-tools"
    },
    "definitions": {
      "beta-app": {
        "name": "Beta App",
        "entryBatchSize": 10,
        "entryWindowInterval": "24h"
      }
    }
  }
}
```

### After

```json
{
  "projects": {
    "definitions": {
      "beta-app": {
        "name": "Beta App",
        "hostMapping": "beta.localhost",
        "entryBatchSize": 10,
        "entryWindowInterval": "24h"
      }
    }
  }
}
```

### Key Design Decisions

| Decision | Rationale |
|---|---|
| Remove top-level `hostMapping` map | Eliminates redundancy — host binding is a property of a project, not a separate cross-reference. |
| `hostMapping` is a single `string` per definition | Each project maps to at most one hostname. Simple and sufficient for current use cases. |
| Derive the reverse map at load time | `BuildHostMapping()` iterates definitions to produce `map[string]string` for the resolver. No runtime overhead beyond startup. |
| Validate duplicate hosts at startup | Two definitions claiming the same host is a config error — fail fast. |
| No backward compatibility shim | Service is pre-release; old configs must be updated. |

### Dependencies

- Builds on plan 24 (config-only projects) which introduced `definitions`.
- No new Go module dependencies.
- No database changes.

---

## Requirements

### Configuration

1. Remove `HostMapping map[string]string` field from `ProjectsConfig`.
2. Add `HostMapping string` field to `ProjectDefinition` (optional, empty = no host binding).
3. Provide a `BuildHostMapping() map[string]string` method on `ProjectsConfig` that iterates definitions and returns `host → slug`.
4. `Validate()` must reject configs where two definitions specify the same `hostMapping` value.
5. Remove the old validation that cross-checked hostMapping values against definition keys.

### Application Wiring

1. `cmd/server/main.go` must call `cfg.Projects.BuildHostMapping()` and pass the result to `NewProjectResolver`.
2. The `ProjectResolver` and its `Middleware` are unchanged — they already accept `map[string]string`.

### Dev Config

1. `conf/dev.json` must be updated to the new shape (remove top-level `hostMapping`, ensure definitions have the field).

---

## Design

### Config Structs

```go
type ProjectsConfig struct {
    HeaderName  string                       `koanf:"headerName"`
    DefaultSlug string                       `koanf:"defaultSlug"`
    Definitions map[string]ProjectDefinition `koanf:"definitions"`
}

type ProjectDefinition struct {
    Name                string `koanf:"name"`
    HostMapping         string `koanf:"hostMapping"`
    EntryBatchSize      *int   `koanf:"entryBatchSize"`
    EntryWindowInterval string `koanf:"entryWindowInterval"`
    SchedulerDisabled   bool   `koanf:"schedulerDisabled"`
}
```

### BuildHostMapping

```go
func (p ProjectsConfig) BuildHostMapping() map[string]string {
    m := make(map[string]string)
    for slug, def := range p.Definitions {
        if def.HostMapping != "" {
            m[def.HostMapping] = slug
        }
    }
    return m
}
```

### Validation

```go
func (p ProjectsConfig) Validate() error {
    if len(p.Definitions) == 0 {
        return fmt.Errorf("projects.definitions must not be empty")
    }
    if _, ok := p.Definitions[p.DefaultSlug]; !ok {
        return fmt.Errorf("projects.defaultSlug %q not found in definitions", p.DefaultSlug)
    }
    seen := make(map[string]string) // host → slug
    for slug, def := range p.Definitions {
        if def.HostMapping != "" {
            if other, exists := seen[def.HostMapping]; exists {
                return fmt.Errorf("duplicate hostMapping %q in definitions %q and %q", def.HostMapping, other, slug)
            }
            seen[def.HostMapping] = slug
        }
    }
    return nil
}
```

---

## Implementation Steps

### Step 1: Update `ProjectDefinition` struct
- **File:** `internal/config/config.go`
- Add `HostMapping string` field with `koanf:"hostMapping"` tag.
- Remove `HostMapping map[string]string` from `ProjectsConfig`.

### Step 2: Add `BuildHostMapping()` method
- **File:** `internal/config/config.go`
- Iterate `Definitions`, collect non-empty `HostMapping` values into `map[string]string`.

### Step 3: Update `Validate()`
- **File:** `internal/config/config.go`
- Remove old loop over `p.HostMapping`.
- Add duplicate-host detection across definitions.

### Step 4: Update `main.go`
- **File:** `cmd/server/main.go`
- Replace `cfg.Projects.HostMapping` with `cfg.Projects.BuildHostMapping()`.

### Step 5: Update dev config
- **File:** `conf/dev.json`
- Remove top-level `hostMapping` block. Definitions already have `hostMapping` field.

### Step 6: Update tests
- **File:** `internal/config/config_test.go`
- Add test for `BuildHostMapping()` output.
- Add test for duplicate-host validation error.
- Ensure existing tests still pass with the new config shape.

### Step 7: Update documentation
- **File:** `CLAUDE.md`
- Remove `projects.hostMapping` from the config schema table.
- Document `hostMapping` field in `ProjectDefinition`.

### Step 8: Verify
- Run `make format && make lint && make test` — all must pass.

---

## Testing

### Unit Tests (`internal/config/config_test.go`)

| Test | Description |
|---|---|
| `TestBuildHostMapping_CollectsFromDefinitions` | Definitions with `hostMapping` set produce correct reverse map. |
| `TestBuildHostMapping_SkipsEmpty` | Definitions without `hostMapping` are omitted from the map. |
| `TestValidate_DuplicateHostMapping_ReturnsError` | Two definitions with the same host value fail validation. |
| `TestValidate_NoDefinitions_ReturnsError` | Existing test — still passes. |
| `TestValidate_DefaultSlugMissing_ReturnsError` | Existing test — still passes. |
| `TestLoad_WithHostMappingInDefinitions` | Full load round-trip with new config shape. |

### Existing Tests (must still pass)

- `internal/handler/tenant_test.go` — these construct `hostMapping` directly via argument, unaffected by config changes.
- All existing `config_test.go` tests — update config JSON snippets if they reference old `hostMapping`.

---

## Acceptance Criteria

- [x] `ProjectsConfig` no longer has a `HostMapping` field.
- [x] `ProjectDefinition` has a `HostMapping string` field.
- [x] `BuildHostMapping()` correctly derives `map[string]string` from definitions.
- [x] `Validate()` rejects duplicate host mappings across definitions.
- [x] `conf/dev.json` uses the new config shape.
- [x] `cmd/server/main.go` uses `BuildHostMapping()`.
- [x] `make format`, `make lint`, `make test` all pass.
- [x] `CLAUDE.md` configuration table is updated.
