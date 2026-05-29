# 28 — Admin "Send Digest Now" Button (Full-State Digest)

> **Status:** ✅ Complete

## Overview

Add a "Send Digest Now" button to the admin UI that triggers an immediate digest email to all configured recipients. Unlike the scheduled digest (which reports only changes since the last run), this manual trigger sends the **full current state**: all users currently on the waiting list and all users who currently have access.

### Key Design Decisions

| Decision | Rationale |
|---|---|
| New API endpoint `POST /admin/digest/send` | Follows existing admin endpoint patterns; POST because it has side effects. |
| Full-state payload (not delta) | Explicit requirement — admin wants a snapshot, not incremental changes. |
| Does NOT update `scheduler_state` | The manual send is out-of-band; the scheduled digest should continue its own cadence unaffected. |
| Per-project scoping via `?project=<slug>` | Consistent with other admin endpoints; sends digest for a single project. |
| Reuses existing `SendDigest` method | Same rendering pipeline; only the data query differs (all rows vs. since-timestamp). |
| Button on the Dashboard tab | Dashboard is the overview page; the button lives alongside the counters. |
| Same Basic Auth as other admin routes | No new auth mechanism needed. |
| Success/error feedback via banner | Reuses the existing `showError`/banner pattern in the admin SPA. |

### Dependencies

- Existing `SMTPNotifier.SendDigest` method (plan 27).
- Existing repository methods: `ListWithAccess`, `ListJoined` (already used by admin list endpoints).
- Existing admin UI banner for feedback.

---

## Open Questions

- None. Requirements are clear from the user's request.

---

## Requirements

### Backend

1. New endpoint: `POST /admin/digest/send?project=<slug>` (project param required).
2. Queries **all** waitlist entries and **all** users with access for the given project.
3. Renders the digest template with the full dataset (period start = "all time", period end = now).
4. Sends the digest to all `digest.recipients` configured for the project.
5. Returns `200 OK` with a JSON body `{"sent_to": N}` on success.
6. Returns `400` if the project has no digest recipients configured or if SMTP is not configured.
7. Does NOT update `scheduler_state.digest_last_success`.
8. Protected by the same Basic Auth as all `/admin/*` routes.

### Frontend

1. A "Send Digest Now" button on the Dashboard tab, visible when a project is selected.
2. Button is disabled/hidden when "All projects" is selected (digest is per-project).
3. Clicking the button shows a confirmation modal ("Send full-state digest to N recipients?").
4. On success: show a success banner ("Digest sent to N recipients").
5. On failure: show the error banner with the server error message.

---

## Design

### API Endpoint

```
POST /admin/digest/send?project=<slug>
Authorization: Basic <credentials>

Response 200:
{ "sent_to": 2 }

Response 400:
{ "error": "digest not configured for project" }
```

### Handler Interface Extension

The `AdminHandler` needs access to the digest sender and the project list (already has both). New dependencies needed:

```go
// AdminDigestUserStore extends AdminUserStore with a method to get ALL users with access.
// ListWithAccess already exists with limit/offset — we can call it with a large limit or
// add a dedicated method. Since we already have ListWithAccess with limit=0 meaning "all",
// we'll use a high limit or add ListAllWithAccess.
```

Preferred approach: add `ListAllWithAccess(ctx, projectSlug)` and `ListAllWaitlist(ctx, projectSlug)` to avoid abusing pagination for a bulk export.

### Data Flow

1. Admin clicks button → `POST /admin/digest/send?project=beta-app`
2. Handler resolves project, validates digest config exists.
3. Queries all waitlist entries + all users with access (no time filter).
4. Builds `notifier.DigestData` with `PeriodStart = "Full state"` and `PeriodEnd = now`.
5. Calls `sender.SendDigest(recipients, from, subject, data)`.
6. Returns `{"sent_to": len(recipients)}`.

### UI Changes

Dashboard tab gets a new control next to the existing "Refresh" button:

```html
<button id="send-digest-btn" class="btn-primary" disabled>Send Digest Now</button>
```

The button enables when a project is selected in the project filter dropdown.

---

## Implementation Steps

### Step 1: Add repository methods for full-state queries ✅
- **File:** `internal/repository/user.go`
- Add `ListAllWithAccess(ctx context.Context, projectSlug string) ([]model.UserEntity, error)` — SELECT all users where `has_access = true AND project_slug = $slug`, no limit.
- **File:** `internal/repository/waitinglist.go`
- Add `ListAllJoined(ctx context.Context, projectSlug string) ([]model.WaitingListAdminRow, error)` — SELECT all waitlist entries joined with user data for the project, no limit.

### Step 2: Add handler interface and endpoint ✅
- **File:** `internal/handler/admin.go`
- Extend `AdminHandler` to accept a `DigestSender` (the `waitlist.DigestSender` interface, or define a local equivalent).
- Add `handleSendDigest(w, r)` method.
- Register route: `POST /admin/digest/send`.
- Validate: project param required, project has `Digest.Recipients` configured, sender is non-nil.
- Query full state, build `DigestData`, call `SendDigest`, return `{"sent_to": N}`.

### Step 3: Wire digest sender into AdminHandler ✅
- **File:** `cmd/server/main.go`
- Pass the `*notifier.SMTPNotifier` (or the `DigestSender` interface) to `NewAdminHandler`.

### Step 4: Add UI button and logic ✅
- **File:** `internal/handler/adminui/static/index.html`
- Add "Send Digest Now" button in the dashboard controls section.
- **File:** `internal/handler/adminui/static/admin.js`
- Add `sendDigest()` function that POSTs to `/admin/digest/send?project=<slug>`.
- Wire button enable/disable to project filter state.
- Show confirmation modal before sending.
- Show success/error via banner.

### Step 5: Add tests ✅
- **File:** `internal/handler/admin_test.go`
  - Test `POST /admin/digest/send?project=default` succeeds with valid config.
  - Test returns 400 when no project is specified.
  - Test returns 400 when project has no digest recipients.
  - Test returns 400 when SMTP (sender) is nil.
  - Test does NOT update `scheduler_state`.
- **File:** `internal/repository/user_test.go`
  - Test `ListAllWithAccess` returns all users with access for the project.
- **File:** `internal/repository/waitinglist_test.go`
  - Test `ListAllJoined` returns all entries for the project.

### Step 6: Update documentation ✅
- **File:** `CLAUDE.md`
- Add `POST /admin/digest/send` to the HTTP Endpoints table.
- Update plan 28 status.

### Step 7: Verify ✅
- Run `make format && make lint && make test` — all must pass.
- Manual test: run dev server, open admin UI, select a project, click "Send Digest Now", verify email arrives in MailPit.

---

## Testing

### Unit Tests

| Test | File | Description |
|---|---|---|
| `TestHandleSendDigest_Success` | `handler/admin_test.go` | Returns 200 with `sent_to` count when project has digest config. |
| `TestHandleSendDigest_NoProject` | `handler/admin_test.go` | Returns 400 when project query param is missing. |
| `TestHandleSendDigest_NoDigestConfig` | `handler/admin_test.go` | Returns 400 when project has no digest recipients. |
| `TestHandleSendDigest_NoSender` | `handler/admin_test.go` | Returns 400 when SMTP notifier is nil. |
| `TestHandleSendDigest_DoesNotUpdateState` | `handler/admin_test.go` | Verifies scheduler_state is not modified. |
| `TestListAllWithAccess` | `repository/user_test.go` | Returns all users with access for the given project. |
| `TestListAllJoined` | `repository/waitinglist_test.go` | Returns all waitlist entries for the given project. |

### Integration/Manual Testing

- Run MailPit (`docker run -p 1025:1025 -p 8025:8025 axllent/mailpit`).
- Start dev server with `conf/dev.json` (ensure digest recipients are configured for "default" project).
- Add users to waitlist and grant some access.
- Open admin UI → Dashboard → select "Default" project → click "Send Digest Now".
- Verify email arrives in MailPit containing ALL waitlist entries and ALL access-granted users (not just recent ones).
- Verify the scheduled digest continues to fire at its configured interval, unaffected by the manual send.

---

## Acceptance Criteria

- [x] `POST /admin/digest/send?project=<slug>` endpoint exists and is protected by Basic Auth.
- [x] Endpoint sends a digest containing the full current state (all waitlist + all access users).
- [x] Endpoint does NOT update `scheduler_state.digest_last_success`.
- [x] Endpoint returns `{"sent_to": N}` on success.
- [x] Endpoint returns 400 with clear error when project has no digest config or SMTP is unconfigured.
- [x] Admin UI has a "Send Digest Now" button on the Dashboard tab.
- [x] Button is disabled when no project is selected.
- [x] Button shows a confirmation modal before sending.
- [x] Success/error feedback is shown via the existing banner.
- [x] `ListAllWithAccess` and `ListAllJoined` repository methods exist and are tested.
- [x] Handler tests cover success, missing project, missing config, and nil sender cases.
- [x] `make format`, `make lint`, `make test` all pass.
