# Project Guidelines

## Plan Management

- All feature plans are stored in `docs/plans/`, organized by feature in their own directories.
- Each feature directory contains a `plan.md` file describing the design, requirements, and implementation steps for that feature.
- Plan directories must be prefixed with numbers in the correct implementation order (e.g., `01-project-setup`, `02-database`, `03-user-entity`). When adding a new plan, assign the next sequential number.
- When creating or updating plans:
  1. Identify the feature scope and create/update the corresponding directory under `docs/plans/<NN-feature-name>/` (where `NN` is the sequence number).
  2. Each plan should include: **Overview**, **Requirements**, **Design**, **Implementation Steps**, **Testing**, and **Acceptance Criteria**.
  3. Every plan must include a **Testing** section that describes the unit tests to be written, covering core logic, edge cases, and error/negative scenarios.
  4. Plans should be kept up to date as implementation progresses — mark completed steps and note any deviations.
- Cross-cutting concerns (e.g., database schema shared across features) get their own plan directory.
- Reference related plans from within a plan when there are dependencies between features.

## Development Workflow

The project includes a `Makefile` with standard targets. After making any code changes, always run formatting, linting, and tests before considering the work complete.

### After Every Code Change

1. **Format code** — run `make format` to auto-fix formatting with `goimports`.
2. **Lint code** — run `make lint` to check for issues using `golangci-lint` (runs via Docker).
3. **Run tests** — run `make test` to execute the full test suite (`go test ./...`).

### Available Makefile Targets

| Target          | Command            | Description                                                        |
|-----------------|--------------------|--------------------------------------------------------------------|
| `make build`    | `go build`         | Build the binary to `bin/waitinglist`.                             |
| `make test`     | `go test ./...`    | Run all tests.                                                     |
| `make lint`     | `golangci-lint`    | Lint the codebase (runs in Docker).                                |
| `make format`   | `goimports -w .`   | Auto-format all Go files.                                          |
| `make format-check` | `goimports -l .` | Check formatting without modifying files (CI-friendly).           |
| `make deps`     | `go mod tidy/download` | Tidy and download module dependencies.                        |
| `make clean`    | `rm -rf bin/`      | Remove build artifacts.                                            |
| `make run`      | build + execute    | Build and run the server binary.                                   |
| `make release`  | cross-compile      | Build release binaries for all supported platforms.                |

### Prerequisites

- **Docker** must be installed and running for `make lint` (golangci-lint runs as a container).
- **goimports** must be installed for `make format` / `make format-check` (`go install golang.org/x/tools/cmd/goimports@latest`).

## Logging

- Always use `log/slog` for logging. Do not use the standard `log` package or third-party logging libraries.
- Create a logger instance with `slog.New(slog.NewTextHandler(os.Stderr, nil))` and pass it where needed.
- Use structured key-value pairs for log arguments: `logger.Info("message", "key", value)` — never use `fmt.Sprintf`-style formatting.

## Testing

- Every implementation change must include unit tests.
- Tests should cover the core logic, edge cases, and error/negative scenarios for the changed code.
- Do not merge or consider a feature complete without accompanying unit tests.
