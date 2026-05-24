# 27 — Admin Digest Email (Scheduled Activity Summary)

> **Status:** ✅ Complete

## Overview

Add a periodic background job that sends a digest email to configured admin recipients summarizing activity since the last digest: newly enlisted waiting-list users and users who were granted access. The digest runs on a configurable schedule (per-project), reuses the existing global SMTP config, and is best-effort (failures logged, never blocking).

### Key Design Decisions

| Decision | Rationale |
|---|---|
| Separate background goroutine alongside existing scheduler | Digest cadence is independent of the access-grant scheduler; they shouldn't block each other. |
| Per-project recipient list + schedule | Different projects may have different stakeholders and desired cadence. |
| Reuse global `smtp.*` config | One SMTP server for all outbound mail; consistent with plan 26. |
| Track "last digest sent" in `scheduler_state` table | Existing pattern; no new tables needed. |
| Skip send when no activity | Avoids noisy "nothing happened" emails. |
| Embedded HTML template via `//go:embed` | Consistent with existing notifier; no runtime file dependency. |
| Query DB for "since timestamp" data | Simple, reliable; avoids event-sourcing complexity. |

### Dependencies

- No new Go module dependencies (reuses `net/smtp`, `html/template`, existing SMTP infra).
- Requires repository queries for "users enlisted since X" and "users granted access since X".

---

## Open Questions

> These must be answered before implementation begins.

1. **Recipients**: Should each project define a list of admin email addresses (e.g. `digestRecipients: ["admin@example.com"]`), or should it go to the configured Basic Auth admin user's email (which doesn't exist today)?
2. **Schedule**: What default interval? (e.g. `24h` for daily, `6h` for frequent). Should it be configurable per project?
3. **Scope**: One email per project (only projects with activity get a digest), or one combined email across all projects?
4. **Empty digests**: Skip entirely when no activity, or send anyway?
5. **SMTP from/subject**: Reuse per-project `emailFrom`/`emailSubject`, or add separate `digestFrom`/`digestSubject` fields?

---

## Requirements

### Configuration (Proposed)

1. New per-project fields in `ProjectDefinition`:
   - `digestRecipients` (`[]string`) — email addresses to receive the digest.
   - `digestInterval` (`string`, duration) — how often to send (default: `24h`).
   - `digestFrom` (`string`) — sender address for digest emails (falls back to `emailFrom` if empty).
   - `digestSubject` (`string`) — subject line (falls back to a sensible default like `"[ProjectName] Activity Digest"`).
2. Digest is disabled for a project when `digestRecipients` is empty.
3. Global SMTP config (`smtp.*`) is reused — if SMTP is not configured, digest is disabled globally.

### Data Queries

1. **New waitlist entries since timestamp**: query `waiting_list` joined with `user_entity` where `waiting_list.created_at > $lastDigest` and `project_slug = $slug`.
2. **Users granted access since timestamp**: query `user_entity` where `access_granted_at > $lastDigest` and `project_slug = $slug`.
3. Timestamp of last successful digest is stored in `scheduler_state` with key `digest_last_success` per project.

### Email Content

1. HTML email rendered from an embedded template.
2. Template data:
   - Project name
   - Period (from → to timestamps)
   - List of newly enlisted users (name, email, date)
   - List of newly granted users (name, email, date, granted by)
   - Counts/summary line
3. MIME headers: `Content-Type: text/html; charset=UTF-8`, `From`, `To`, `Subject`.

### Behavior

1. Runs as a background goroutine (like the access-grant scheduler).
2. On each tick: iterate all projects, check if `digestInterval` has elapsed since last digest.
3. Query for activity; if none, skip (no email sent).
4. Send one email per project per tick (to all `digestRecipients` as `To` or `Bcc`).
5. Update `scheduler_state` with new "last digest sent" timestamp on successful send.
6. Errors are logged at `Warn`, never propagated or retried.

---

## Design

### Config Structs

Added to `ProjectDefinition`:

```go
type ProjectDefinition struct {
    // ... existing fields ...
    DigestRecipients []string `koanf:"digestRecipients"`
    DigestInterval   string   `koanf:"digestInterval"`
    DigestFrom       string   `koanf:"digestFrom"`
    DigestSubject    string   `koanf:"digestSubject"`
}
```

Propagated to `model.Project`:

```go
type Project struct {
    // ... existing fields ...
    DigestRecipients []string `json:"digest_recipients,omitempty"`
    DigestInterval   *Duration `json:"digest_interval,omitempty"`
    DigestFrom       string   `json:"digest_from,omitempty"`
    DigestSubject    string   `json:"digest_subject,omitempty"`
}
```

### Repository Queries

```go
// internal/repository/user.go
func (r *UserRepository) GetGrantedSince(ctx context.Context, projectSlug string, since time.Time) ([]model.UserEntity, error)

// internal/repository/waitinglist.go
func (r *WaitingListRepository) GetEnlistedSince(ctx context.Context, projectSlug string, since time.Time) ([]model.WaitingListAdminRow, error)
```

### Digest Notifier

```go
// internal/notifier/digest.go
type DigestData struct {
    ProjectName    string
    PeriodStart    time.Time
    PeriodEnd      time.Time
    NewEnlisted    []EnlistedEntry
    NewGranted     []GrantedEntry
    EnlistedCount  int
    GrantedCount   int
}

func (n *SMTPNotifier) SendDigest(recipients []string, from, subject string, data DigestData) error
```

### Digest Scheduler

```go
// internal/waitlist/digest.go
func StartDigest(
    ctx context.Context,
    cfg *config.Config,
    logger *slog.Logger,
    projects []model.Project,
    userRepo digestUserStore,
    waitlistRepo digestWaitlistStore,
    schedulerRepo schedulerStore,
    notifier *notifier.SMTPNotifier,
) error
```

- Iterates projects, checks elapsed time via `scheduler_state` (key: `digest_last_success`).
- Queries for activity, renders template, sends email, updates state.
- Tick interval: minimum of all project `digestInterval` values (or a global default like `1h` check interval, actual send gated by per-project interval).

### Template

```html
<!-- internal/notifier/templates/digest.html -->
<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"></head>
<body>
  <h1>{{.ProjectName}} — Activity Digest</h1>
  <p>Period: {{.PeriodStart.Format "2006-01-02 15:04"}} → {{.PeriodEnd.Format "2006-01-02 15:04"}}</p>

  {{if .NewEnlisted}}
  <h2>New Waiting List Entries ({{.EnlistedCount}})</h2>
  <table>
    <tr><th>Name</th><th>Email</th><th>Joined</th></tr>
    {{range .NewEnlisted}}
    <tr><td>{{.Firstname}} {{.Lastname}}</td><td>{{.Email}}</td><td>{{.JoinedAt.Format "2006-01-02 15:04"}}</td></tr>
    {{end}}
  </table>
  {{end}}

  {{if .NewGranted}}
  <h2>Access Granted ({{.GrantedCount}})</h2>
  <table>
    <tr><th>Name</th><th>Email</th><th>Granted At</th><th>By</th></tr>
    {{range .NewGranted}}
    <tr><td>{{.Firstname}} {{.Lastname}}</td><td>{{.Email}}</td><td>{{.GrantedAt.Format "2006-01-02 15:04"}}</td><td>{{.GrantedBy}}</td></tr>
    {{end}}
  </table>
  {{end}}

  {{if and (not .NewEnlisted) (not .NewGranted)}}
  <p>No activity during this period.</p>
  {{end}}
</body>
</html>
```

---

## Implementation Steps

### Step 1: Add config fields
- **File:** `internal/config/config.go`
- Add `DigestRecipients`, `DigestInterval`, `DigestFrom`, `DigestSubject` to `ProjectDefinition`.
- Propagate in `ProjectsConfig.Projects()` → `model.Project`.

### Step 2: Update model
- **File:** `internal/model/model.go`
- Add `DigestRecipients`, `DigestInterval`, `DigestFrom`, `DigestSubject` to `model.Project`.

### Step 3: Add repository queries
- **File:** `internal/repository/user.go`
- Add `GetGrantedSince(ctx, projectSlug, since)` — SELECT users where `access_granted_at > $since AND project_slug = $slug`.
- **File:** `internal/repository/waitinglist.go`
- Add `GetEnlistedSince(ctx, projectSlug, since)` — SELECT waiting_list JOIN user_entity where `waiting_list.created_at > $since AND waiting_list.project_slug = $slug`.

### Step 4: Create digest HTML template
- **File:** `internal/notifier/templates/digest.html`
- HTML table layout showing enlisted and granted users with timestamps.

### Step 5: Add digest send method to notifier
- **File:** `internal/notifier/digest.go`
- `DigestData` struct, `SendDigest(recipients, from, subject, data)` method on `SMTPNotifier`.
- Renders the digest template, builds MIME message (multiple recipients), sends via existing `send()` infra.

### Step 6: Create digest scheduler
- **File:** `internal/waitlist/digest.go`
- `StartDigest(...)` function: background goroutine, iterates projects, checks cooldown, queries data, calls `SendDigest`.
- Uses `scheduler_state` key `"digest_last_success"` per project.

### Step 7: Wire in main.go
- **File:** `cmd/server/main.go`
- Call `waitlist.StartDigest(...)` after constructing notifier and repos (similar to `waitlist.Start`).

### Step 8: Update dev config
- **File:** `conf/dev.json`
- Add `digestRecipients`, `digestInterval`, `digestFrom`, `digestSubject` to project definitions.

### Step 9: Add tests
- **File:** `internal/notifier/digest_test.go`
  - Test template rendering with both sections populated.
  - Test template rendering with only enlisted (no grants).
  - Test template rendering with only grants (no enlisted).
  - Test `SendDigest` skips when recipients list is empty.
- **File:** `internal/waitlist/digest_test.go`
  - Test digest skips project when interval hasn't elapsed.
  - Test digest skips when no activity (no email sent).
  - Test digest calls `SendDigest` when activity exists.
  - Test digest updates `scheduler_state` on success.

### Step 10: Update documentation
- **Files:** `CLAUDE.md`
- Document new per-project config fields and env var mappings.
- Add plan 27 to the plans table.

### Step 11: Verify
- Run `make format && make lint && make test` — all must pass.

---

## Testing

### Unit Tests

| Test | File | Description |
|---|---|---|
| `TestDigestTemplate_BothSections` | `notifier/digest_test.go` | Template renders with both enlisted and granted data. |
| `TestDigestTemplate_OnlyEnlisted` | `notifier/digest_test.go` | Template renders correctly with no grants. |
| `TestDigestTemplate_OnlyGranted` | `notifier/digest_test.go` | Template renders correctly with no enlisted. |
| `TestSendDigest_SkipsEmptyRecipients` | `notifier/digest_test.go` | No send attempt when recipients is empty. |
| `TestDigestScheduler_SkipsBeforeInterval` | `waitlist/digest_test.go` | No query/send when interval hasn't elapsed. |
| `TestDigestScheduler_SkipsNoActivity` | `waitlist/digest_test.go` | No email when both queries return empty. |
| `TestDigestScheduler_SendsOnActivity` | `waitlist/digest_test.go` | Calls SendDigest with correct data when activity exists. |
| `TestDigestScheduler_UpdatesState` | `waitlist/digest_test.go` | scheduler_state updated after successful send. |
| `TestGetGrantedSince` | `repository/user_test.go` | Returns users granted after the given timestamp. |
| `TestGetEnlistedSince` | `repository/waitinglist_test.go` | Returns entries created after the given timestamp. |

### Integration/Manual Testing

- Run MailPit (`docker run -p 1025:1025 -p 8025:8025 axllent/mailpit`).
- Configure `conf/dev.json` with short `digestInterval` (e.g. `"1m"`) and `digestRecipients`.
- Add users to waitlist and grant access.
- Verify digest email arrives in MailPit with correct content.

---

## Acceptance Criteria

- [x] Per-project `digestRecipients`, `digestInterval`, `digestFrom`, `digestSubject` config fields added.
- [x] `model.Project` carries digest config fields.
- [x] Repository query `GetGrantedSince` returns users granted access after a timestamp.
- [x] Repository query `GetEnlistedSince` returns waitlist entries created after a timestamp.
- [x] HTML digest template embedded via `//go:embed`, renders both sections conditionally.
- [x] `SMTPNotifier.SendDigest` sends HTML email to multiple recipients.
- [x] Digest scheduler runs as a background goroutine alongside the access-grant scheduler.
- [x] Digest skips projects with empty `digestRecipients`.
- [x] Digest skips sending when no activity occurred since last digest.
- [x] `scheduler_state` tracks last digest timestamp per project (key: `digest_last_success`).
- [x] Errors logged at `Warn`, never propagated or block other projects.
- [x] `conf/dev.json` includes digest config for local testing.
- [x] `CLAUDE.md` documents new config fields.
- [x] `make format`, `make lint`, `make test` all pass.
