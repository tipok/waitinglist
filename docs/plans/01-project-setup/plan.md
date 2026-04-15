# Project Setup Plan

## Overview

Set up the Go project structure for the waiting list service. The service uses PostgreSQL for storage and relies only on Go's standard library (including the built-in `net/http` mux) to minimize dependencies, with the exception of the PostgreSQL driver and the configuration library.

## Requirements

- Go module initialized (`go mod init`)
- External dependencies:
  - `github.com/lib/pq` — PostgreSQL driver (required since Go's `database/sql` needs a driver)
  - `github.com/knadh/koanf` — configuration loading from JSON file
- No third-party HTTP routers — use `net/http.ServeMux`
- Configuration stored in a JSON file; the path to the config file is passed as a CLI argument
- Clean, idiomatic Go project layout

## Design

### Directory Structure

```
waitinglist/
├── cmd/
│   └── server/
│       └── main.go            # Application entry point
├── internal/
│   ├── config/
│   │   └── config.go          # Configuration loading (koanf, JSON file)
│   ├── database/
│   │   └── postgres.go        # DB connection setup
│   ├── handler/
│   │   ├── user.go            # HTTP handlers for user entity endpoints
│   │   └── waitinglist.go     # HTTP handlers for waiting list endpoints
│   ├── model/
│   │   └── model.go           # Data structures (UserEntity, WaitingListEntry)
│   └── repository/
│       ├── user.go            # DB operations for user_entity table
│       └── waitinglist.go     # DB operations for waiting_list table
├── migrations/
│   └── 001_init.sql           # SQL migration for initial schema
├── config.json                # Default configuration file
├── docs/
│   └── plans/                 # Feature plans (this directory)
├── go.mod
├── go.sum
├── README.md
└── LICENSE
```

### Configuration

The application loads configuration from a JSON file. The path to the file is passed as a command-line argument:

```bash
./server --config /path/to/config.json
```

#### JSON Config File Format

```json
{
  "port": 8080,
  "database": {
    "url": "postgres://localhost:5432/waitinglist?sslmode=disable"
  }
}
```

#### Config Fields

| Field          | Description                | Default                                                  |
|----------------|----------------------------|----------------------------------------------------------|
| `port`         | HTTP server listen port    | `8080`                                                   |
| `database.url` | PostgreSQL connection URL  | `postgres://localhost:5432/waitinglist?sslmode=disable`   |

#### Config Loading (koanf)

`internal/config/config.go` uses `github.com/knadh/koanf` with the JSON file provider:

```go
import (
    "github.com/knadh/koanf/v2"
    "github.com/knadh/koanf/parsers/json"
    "github.com/knadh/koanf/providers/file"
)
```

1. Parse the `--config` flag from `os.Args` (using Go's `flag` package).
2. Load the JSON file via `koanf`'s `file.Provider` and `json.Parser`.
3. Unmarshal into a typed `Config` struct.
4. Apply defaults for any missing fields.

#### Config Struct

```go
type Config struct {
    Port     int            `koanf:"port"`
    Database DatabaseConfig `koanf:"database"`
}

type DatabaseConfig struct {
    URL string `koanf:"url"`
}
```

## Implementation Steps

- [x] Initialize Go module: `go mod init github.com/tipok/waitinglist`
- [x] Create directory structure as outlined above
- [x] Add `github.com/lib/pq` as an external dependency
- [x] Add `github.com/knadh/koanf/v2` and its JSON file provider/parser as external dependencies
- [x] Implement `internal/config/config.go` to load configuration from a JSON file using koanf
  - [x] Parse `--config` CLI flag for the config file path
  - [x] Load and parse the JSON config file
  - [x] Apply defaults for missing values
- [x] Create a default `config.json` at the project root
- [x] Implement `internal/database/postgres.go` to establish DB connection
- [x] Implement `cmd/server/main.go` to wire everything together and start the HTTP server

## Testing

### Unit Tests — Config Loading (`internal/config/`)

- **Core logic**:
  - Test that a valid JSON config file is parsed correctly into the `Config` struct.
  - Test that default values are applied when fields are missing from the JSON file.
  - Test that all config fields (`port`, `database.url`) are correctly mapped.
- **Edge cases**:
  - Test loading a config file with only partial fields (e.g., only `port` set, `database` missing).
  - Test loading an empty JSON object `{}` — all defaults should apply.
- **Error/negative scenarios**:
  - Test that a non-existent config file path returns a descriptive error.
  - Test that an invalid JSON file (malformed syntax) returns a parse error.
  - Test that a missing `--config` flag falls back to a default path or returns an error (depending on chosen behavior).

### Unit Tests — Database Connection (`internal/database/`)

- **Core logic**:
  - Test that `NewPostgresDB` returns a valid `*sql.DB` when given a correct connection URL (use a test helper or mock if needed).
- **Error/negative scenarios**:
  - Test that an invalid connection URL returns an error.

## Acceptance Criteria

- `go build ./...` succeeds with no errors
- The server accepts a `--config` flag pointing to a JSON configuration file
- Configuration is loaded from the JSON file using `koanf`
- Missing config values fall back to sensible defaults
- The server starts and listens on the configured port
- The project has exactly two external dependencies (`lib/pq` and `knadh/koanf`)
