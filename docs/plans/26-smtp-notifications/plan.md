# 26 — SMTP Email Notifications on Access Grant

> **Status:** ✅ Complete

## Overview

Send an HTML email to users when they are granted access to the service (via scheduler or admin). The email includes the user's name and the project name, rendered via Go's `html/template`. SMTP connection settings are global; sender address and subject line are configured per-project.

Email sending is best-effort — failures are logged but never block or roll back the access grant.

### Key Design Decisions

| Decision | Rationale |
|---|---|
| Stdlib `net/smtp` with STARTTLS | No new dependencies; sufficient for port 587 STARTTLS flows. |
| HTML template via `//go:embed` | Baked into the binary like the admin UI; no runtime file dependency. |
| Best-effort delivery | Access grant is the critical path; email is a nice-to-have notification. |
| Per-project `from` and `subject` | Different projects may have different branding/sender identities. |
| Global SMTP connection config | One mail server handles all projects. |
| Skip send when `from`/`subject` empty | Projects without email config simply don't send notifications. |

### Dependencies

- No new Go module dependencies (stdlib `net/smtp`, `crypto/tls`, `html/template`).
- Requires an SMTP server at runtime (e.g. Mailhog/MailPit for dev, real provider for prod).

---

## Requirements

### Configuration

1. New top-level `smtp` config section with global connection settings: `host`, `port`, `username`, `password`, `tls` (bool).
2. New per-project fields in `ProjectDefinition`: `emailFrom` (string), `emailSubject` (string).
3. If `smtp.host` is empty, the notifier is disabled globally (no emails sent).
4. If a project's `emailFrom` or `emailSubject` is empty, no email is sent for that project.

### Email Content

1. HTML email rendered from an embedded Go template.
2. Template data: `Firstname`, `Lastname`, `ProjectName`.
3. MIME headers: `Content-Type: text/html; charset=UTF-8`, `From`, `To`, `Subject`.

### Integration Points

1. **Scheduler** (`internal/waitlist/waitlist.go`) — after successful commit, send email to each granted user.
2. **Admin grant** (`internal/handler/admin.go`) — after successful commit, send email to the granted user.
3. Both call the notifier asynchronously or inline with error logging only (no failure propagation).

---

## Design

### Config Structs

```go
type SMTPConfig struct {
    Host     string `koanf:"host"`
    Port     int    `koanf:"port"`
    Username string `koanf:"username"`
    Password string `koanf:"password"`
    TLS      bool   `koanf:"tls"`
}
```

Added to `ProjectDefinition`:

```go
type ProjectDefinition struct {
    Name                string `koanf:"name"`
    HostMapping         string `koanf:"hostMapping"`
    EmailFrom           string `koanf:"emailFrom"`
    EmailSubject        string `koanf:"emailSubject"`
    EntryBatchSize      *int   `koanf:"entryBatchSize"`
    EntryWindowInterval string `koanf:"entryWindowInterval"`
    SchedulerDisabled   bool   `koanf:"schedulerDisabled"`
}
```

### Notifier Interface

```go
type Notifier interface {
    NotifyAccessGranted(ctx context.Context, user model.UserEntity, project model.Project) error
}
```

### Notifier Implementation

```go
// internal/notifier/notifier.go
type SMTPNotifier struct {
    host     string
    port     int
    username string
    password string
    tls      bool
    tmpl     *template.Template
    logger   *slog.Logger
}
```

- `NotifyAccessGranted` renders the template, composes the MIME message, dials the SMTP server with STARTTLS (if `tls=true`), authenticates, and sends.
- Returns an error (caller logs and ignores).

### Template

```html
<!-- internal/notifier/templates/access_granted.html -->
<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body>
  <h1>Welcome, {{.Firstname}} {{.Lastname}}!</h1>
  <p>You now have access to <strong>{{.ProjectName}}</strong>.</p>
</body>
</html>
```

### Wiring

The scheduler and admin handler receive a `Notifier` interface. When nil (SMTP not configured), they skip notification. Otherwise they call `NotifyAccessGranted` after commit and log any error at `Warn`.

---

## Implementation Steps

### Step 1: Add config structs
- **File:** `internal/config/config.go`
- Add `SMTPConfig` struct and `SMTP SMTPConfig` field to `Config`.
- Add `EmailFrom`, `EmailSubject` fields to `ProjectDefinition`.
- Propagate `EmailFrom`/`EmailSubject` in `ProjectsConfig.Projects()` → `model.Project`.

### Step 2: Update `model.Project`
- **File:** `internal/model/model.go`
- Add `EmailFrom` and `EmailSubject` fields to `model.Project`.

### Step 3: Create HTML template
- **File:** `internal/notifier/templates/access_granted.html`
- HTML email template with `{{.Firstname}}`, `{{.Lastname}}`, `{{.ProjectName}}`.

### Step 4: Create notifier package
- **File:** `internal/notifier/notifier.go`
- `//go:embed templates/access_granted.html`
- `SMTPNotifier` struct, `New(cfg config.SMTPConfig, logger)` constructor.
- `NotifyAccessGranted(ctx, user, project)` method: render template, compose MIME message, send via `net/smtp`.

### Step 5: Update scheduler
- **File:** `internal/waitlist/waitlist.go`
- Add a `Notifier` interface (or accept the one from the notifier package).
- After commit, look up users by ID and call `NotifyAccessGranted` for each. Log errors at `Warn`.
- Requires adding a `GetByIDs` method or using existing `GetByID` in a loop.

### Step 6: Update admin handler
- **File:** `internal/handler/admin.go`
- Accept a `Notifier` interface in `NewAdminHandler`.
- After successful grant commit, call `NotifyAccessGranted`. Log errors at `Warn`.

### Step 7: Wire in `main.go`
- **File:** `cmd/server/main.go`
- Construct `notifier.SMTPNotifier` if `cfg.SMTP.Host` is non-empty (else nil).
- Pass to `waitlist.Start` and `NewAdminHandler`.

### Step 8: Update dev config
- **File:** `conf/dev.json`
- Add `smtp` section (pointing to localhost:1025 for MailPit/Mailhog).
- Add `emailFrom` and `emailSubject` to project definitions.

### Step 9: Update documentation
- **Files:** `CLAUDE.md`, `README.md`
- Document `smtp.*` config fields and per-project `emailFrom`/`emailSubject`.

### Step 10: Add tests
- **File:** `internal/notifier/notifier_test.go`
- Test template rendering produces valid HTML with correct substitution.
- Test that `NotifyAccessGranted` skips send when project `EmailFrom` is empty.
- Test MIME message format (headers, body).

### Step 11: Verify
- Run `make format && make lint && make test` — all must pass.

---

## Testing

### Unit Tests (`internal/notifier/notifier_test.go`)

| Test | Description |
|---|---|
| `TestRenderTemplate_ValidData` | Template renders with firstname, lastname, project name. |
| `TestRenderTemplate_EmptyFields` | Gracefully handles empty firstname/lastname. |
| `TestNotifyAccessGranted_SkipsWhenNoFrom` | Returns nil without sending when project has no `EmailFrom`. |
| `TestNotifyAccessGranted_SkipsWhenNoSubject` | Returns nil without sending when project has no `EmailSubject`. |
| `TestComposeMIMEMessage` | Verifies correct headers and body in the composed message. |

### Integration/Manual Testing

- Run MailPit (`docker run -p 1025:1025 -p 8025:8025 axllent/mailpit`).
- Configure `conf/dev.json` with `smtp.host: "localhost"`, `smtp.port: 1025`, `smtp.tls: false`.
- Grant access via admin UI or trigger scheduler.
- Verify email arrives in MailPit web UI at `localhost:8025`.

---

## Acceptance Criteria

- [x] `SMTPConfig` struct added with `host`, `port`, `username`, `password`, `tls`.
- [x] `ProjectDefinition` has `emailFrom` and `emailSubject` fields.
- [x] HTML template embedded in the binary via `//go:embed`.
- [x] `SMTPNotifier.NotifyAccessGranted` sends HTML email with user name and project name.
- [x] Scheduler calls notifier after granting access (best-effort).
- [x] Admin handler calls notifier after granting access (best-effort).
- [x] No email sent when SMTP not configured or project lacks `emailFrom`/`emailSubject`.
- [x] Errors are logged at `Warn` level, never propagated to the caller.
- [x] `conf/dev.json` includes SMTP config for local development.
- [x] `CLAUDE.md` and `README.md` document new config fields.
- [x] `make format`, `make lint`, `make test` all pass.
