# Waitinglist

A self-hosted waiting list service written in Go. It lets you gate access to your product behind a queue — users sign up, land on a waiting list, and are granted access either automatically by a built-in scheduler or manually via an admin panel.

## Features

- **Waiting list API** — users sign up via a single POST endpoint; duplicates and already-granted users are handled gracefully.
- **Access check API** — look up one or more users by email to determine if they have been granted access.
- **Automatic scheduler** — periodically promotes users from the waiting list based on configurable batch size and time window.
- **Admin web UI** — embedded single-page dashboard with user search, grant/revoke actions, and enlistment charts (protected by HTTP Basic Auth).
- **Health endpoint** — `/healthz` pings the database and returns a structured JSON response (suitable for Kubernetes or Docker `HEALTHCHECK`).
- **Minimal dependencies** — Go standard library `net/http`, PostgreSQL via `lib/pq`, configuration via `koanf`.

## Requirements

- Go 1.25+
- PostgreSQL 14+
- Docker (for linting and container builds)

## Quick Start

```bash
# Build
make build

# Run with a config file
./bin/waitinglist --config conf/dev.json
```

The server listens on `0.0.0.0:<port>` (default `8080`).

## Configuration

Configuration is loaded from a JSON file specified with the `--config` flag. Environment variables can override any value in the file.

### JSON Configuration File

```json
{
  "port": 8080,
  "database": {
    "url": "postgres://localhost:5432/waitinglist?sslmode=disable",
    "username": "myuser",
    "password": "mypassword",
    "migrationsDir": "migrations"
  },
  "waitlist": {
    "entryBatchSize": 25,
    "entryWindowInterval": "30h"
  },
  "schedulerInterval": {
    "disabled": false,
    "waitlistCheckInterval": "1h"
  },
  "admin": {
    "basicAuth": {
      "username": "admin",
      "passwordHash": "$2y$10$..."
    }
  }
}
```

### Configuration Reference

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `port` | int | `8080` | TCP port the HTTP server binds to. |
| `database.url` | string | `postgres://localhost:5432/waitinglist?sslmode=disable` | PostgreSQL connection URL. |
| `database.username` | string | — | Database username (appended to URL if `url` has no userinfo). |
| `database.password` | string | — | Database password (appended to URL if `url` has no userinfo). |
| `database.migrationsDir` | string | `migrations` | Path to the directory containing `.sql` migration files. |
| `waitlist.entryBatchSize` | int | `25` | Maximum number of users promoted per scheduler run. |
| `waitlist.entryWindowInterval` | duration | `30h` | Minimum age of a waiting list entry before it becomes eligible for promotion. Also the cooldown between scheduler batches. |
| `schedulerInterval.disabled` | bool | `false` | Set to `true` to disable the automatic scheduler entirely. |
| `schedulerInterval.waitlistCheckInterval` | duration | `1h` | How often the scheduler wakes up to check for eligible entries. |
| `admin.basicAuth.username` | string | — | Username for the admin panel. If empty, admin routes are disabled. |
| `admin.basicAuth.passwordHash` | string | — | Bcrypt hash of the admin password. If empty, admin routes are disabled. |

Duration values accept Go duration strings: `"30m"`, `"1h"`, `"24h"`, `"720h"` etc.

### Environment Variables

Every configuration field can be overridden with an environment variable. The mapping rule is:

1. Prefix with `WL_`
2. Replace dots with underscores
3. Uppercase everything

| JSON Path | Environment Variable | Example |
|-----------|---------------------|---------|
| `port` | `WL_PORT` | `WL_PORT=9090` |
| `database.url` | `WL_DATABASE_URL` | `WL_DATABASE_URL=postgres://db:5432/prod` |
| `database.username` | `WL_DATABASE_USERNAME` | `WL_DATABASE_USERNAME=app` |
| `database.password` | `WL_DATABASE_PASSWORD` | `WL_DATABASE_PASSWORD=secret` |
| `database.migrationsDir` | `WL_DATABASE_MIGRATIONSDIR` | `WL_DATABASE_MIGRATIONSDIR=/app/migrations` |
| `waitlist.entryBatchSize` | `WL_WAITLIST_ENTRYBATCHSIZE` | `WL_WAITLIST_ENTRYBATCHSIZE=50` |
| `waitlist.entryWindowInterval` | `WL_WAITLIST_ENTRYWINDOWINTERVAL` | `WL_WAITLIST_ENTRYWINDOWINTERVAL=48h` |
| `schedulerInterval.disabled` | `WL_SCHEDULERINTERVAL_DISABLED` | `WL_SCHEDULERINTERVAL_DISABLED=true` |
| `schedulerInterval.waitlistCheckInterval` | `WL_SCHEDULERINTERVAL_WAITLISTCHECKINTERVAL` | `WL_SCHEDULERINTERVAL_WAITLISTCHECKINTERVAL=30m` |
| `admin.basicAuth.username` | `WL_ADMIN_BASICAUTH_USERNAME` | `WL_ADMIN_BASICAUTH_USERNAME=admin` |
| `admin.basicAuth.passwordHash` | `WL_ADMIN_BASICAUTH_PASSWORDHASH` | `WL_ADMIN_BASICAUTH_PASSWORDHASH='$2y$10$...'` |

Environment variables take precedence over values in the JSON file.

## Enabling the Admin Web UI

The admin panel is served at `/admin/` and provides a dashboard with enlistment charts, searchable user lists, and actions to grant or revoke access.

To enable it, configure both `admin.basicAuth.username` and `admin.basicAuth.passwordHash` in your config file or via environment variables. If either is missing or empty, the `/admin/` routes are not registered.

### Generating a Password Hash

Use `htpasswd` (from Apache utils) or any bcrypt tool:

```bash
# Using htpasswd
htpasswd -nbBC 10 admin 'your-password' | cut -d: -f2

# Using Python
python3 -c "import bcrypt; print(bcrypt.hashpw(b'your-password', bcrypt.gensalt(10)).decode())"
```

Place the resulting hash in the `admin.basicAuth.passwordHash` field.

### Example: Admin Enabled

```json
{
  "admin": {
    "basicAuth": {
      "username": "admin",
      "passwordHash": "$2y$10$naPXKlz5deUJSvCZ9WFyD.CUoPAX0oJRxLdAdzFymrdzP4mxPsc.G"
    }
  }
}
```

Then open `http://localhost:8080/admin/` in your browser and authenticate with the configured credentials.

## Scheduler (Automatic Access Granting)

The built-in scheduler periodically checks the waiting list and automatically grants access to eligible users.

### How It Works

1. The scheduler runs on the interval defined by `schedulerInterval.waitlistCheckInterval` (default: every hour).
2. On each tick it fetches up to `waitlist.entryBatchSize` entries from the waiting list (ordered by weighted priority).
3. Entries whose `weightedCreatedAt` is older than `waitlist.entryWindowInterval` are promoted — their users are granted access and removed from the waiting list.
4. A cooldown is enforced: if the last successful batch was less than `entryWindowInterval` ago, the scheduler skips the run.

### Disabling the Scheduler

If you want to manage access purely through the admin panel (manual grants only), disable the scheduler:

```json
{
  "schedulerInterval": {
    "disabled": true
  }
}
```

Or via environment variable:

```bash
export WL_SCHEDULERINTERVAL_DISABLED=true
```

When disabled, the server logs `"scheduler disabled, skipping"` on startup. Users on the waiting list will remain there until an admin grants access manually.

### Tuning the Scheduler

| Goal | Configuration |
|------|---------------|
| Promote users faster | Decrease `entryWindowInterval` (e.g. `"1h"`) |
| Promote more users per batch | Increase `entryBatchSize` (e.g. `100`) |
| Check more frequently | Decrease `waitlistCheckInterval` (e.g. `"10m"`) |
| Slow drip (exclusive feel) | `entryBatchSize: 5`, `entryWindowInterval: "168h"` (one week) |

## API Usage

### Adding a User to the Waiting List

```bash
curl -X POST http://localhost:8080/waitinglist \
  -H "Content-Type: application/json" \
  -d '{
    "firstname": "Jane",
    "lastname": "Doe",
    "email": "jane@example.com"
  }'
```

**Responses:**

| Status | Meaning |
|--------|---------|
| `201 Created` | User was created (if new) and added to the waiting list. |
| `409 Conflict` | User is already on the waiting list. |
| `205 Reset Content` | User already has access — no action needed. The client should redirect to the protected area. |
| `400 Bad Request` | Missing or invalid fields (firstname, lastname, email). |

**201 response body:**

```json
{
  "user": {
    "id": "uuid",
    "firstname": "Jane",
    "lastname": "Doe",
    "email": "jane@example.com",
    "has_access": false,
    "created_at": "2026-05-16T10:00:00Z"
  },
  "waiting_list_entry": {
    "id": "uuid",
    "user_id": "uuid",
    "created_at": "2026-05-16T10:00:00Z",
    "weighted_created_at": "2026-05-16T10:00:00Z"
  }
}
```

### Checking if a User Has Access

Query one or more users by email:

```bash
# Single user
curl "http://localhost:8080/waitinglist/users?email=jane@example.com"

# Multiple users
curl "http://localhost:8080/waitinglist/users?email=jane@example.com&email=john@example.com"
```

**Response:**

```json
{
  "users": [
    {
      "firstname": "Jane",
      "lastname": "Doe",
      "email": "jane@example.com",
      "has_access": true,
      "created_at": "2026-05-16T10:00:00Z",
      "access_granted_at": "2026-05-16T12:00:00Z",
      "access_granted_by": "scheduler"
    }
  ]
}
```

The `has_access` field is the key indicator:
- `true` — the user has been granted access and may use the service.
- `false` — the user is still on the waiting list (or had access revoked).

The `access_granted_by` field indicates how access was granted (`"scheduler"` for automatic, `"admin"` for manual).

If access was revoked, the response includes `access_revoked_at` and `access_revoke_reason`.

### Integration Pattern

A typical integration checks access on each request to your protected service:

```
GET /waitinglist/users?email=<user-email>
  -> if users[0].has_access == true  -> allow
  -> if users array is empty         -> user not registered, redirect to sign-up
  -> if has_access == false           -> show "you're on the waiting list" page
```

This endpoint supports ETag caching — repeated polls will return `304 Not Modified` when nothing has changed.

## Admin API Endpoints

All `/admin/*` endpoints require HTTP Basic Auth (configured as described above).

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/admin/dashboard` | Counters and enlistment-per-day chart data. Query: `?days=N` (default 90, max 365). |
| `GET` | `/admin/users/access` | List users with access. Query: `?email=`, `?limit=`, `?offset=`. |
| `GET` | `/admin/users/waitlist` | List users on the waiting list. Query: `?email=`, `?limit=`, `?offset=`. |
| `POST` | `/admin/users/{id}/grant-access` | Grant access to a user (removes them from the waiting list atomically). |
| `POST` | `/admin/users/{id}/revoke-access` | Revoke access. Body: `{"reason": "..."}` (1-500 chars, required). |
| `DELETE` | `/admin/waitlist/{id}` | Remove a waiting list entry by its entry ID. |

## Health Check

```bash
curl http://localhost:8080/healthz
```

Returns `200` with `{"status":"ok","checks":{"database":"ok"}}` when healthy, or `503` when the database is unreachable.

### Docker Health Check

The binary supports a `--health-check` flag for use in container `HEALTHCHECK` instructions:

```dockerfile
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD ["/waitinglist", "--health-check"]
```

In health-check mode, the binary probes `http://127.0.0.1:<port>/healthz` and exits with code 0 (healthy) or 1 (unhealthy). The port is resolved from the `WL_PORT` environment variable or defaults to `8080` — no config file is needed.

## Docker

```bash
# Build for your architecture
make docker-build:arm64
# or
make docker-build:amd64

# Run
docker run -p 8080:8080 \
  -e WL_DATABASE_URL=postgres://user:pass@db:5432/waitinglist \
  -e WL_ADMIN_BASICAUTH_USERNAME=admin \
  -e WL_ADMIN_BASICAUTH_PASSWORDHASH='$2y$10$...' \
  -v /path/to/config.json:/config.json \
  waitinglist:latest-arm64
```

The image uses `gcr.io/distroless/base-debian13:nonroot` — no shell, minimal attack surface.

## Development

```bash
make build          # Compile to bin/waitinglist
make test           # Run all tests
make lint           # Lint via golangci-lint (Docker)
make format         # Auto-format with goimports
make deps           # go mod tidy + download
make run            # Build and run
make release        # Cross-compile for all platforms
```

## License

See [LICENSE](LICENSE) for details.
