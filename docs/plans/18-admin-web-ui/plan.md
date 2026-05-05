# 18 — Admin Web UI (Dashboard, Lists, Chart)

> **Status:** ✅ Complete. Resolved Open Questions:
> - **Q1 (chart):** hand-rolled inline SVG (no Chart.js, no CDN).
> - **Q3 (caching):** `Cache-Control: no-cache` on every asset response so the
>   browser revalidates after each binary deploy.
>
> **Implementation deviations:**
> - Static URLs are flat (`/admin/admin.css`, `/admin/admin.js`) instead of
>   the `/admin/static/...` shape the plan sketched. The natural output of
>   `embed.FS` + `fs.Sub("static")` + `http.StripPrefix("/admin/")` is flat
>   URLs; nesting `static/` in the URL would have required either an extra
>   strip layer or shipping the assets at the package root.
> - The `Handler()` returned by `internal/handler/adminui` deliberately
>   `Header().Del("Content-Type")` before delegating to the file server.
>   `JSONContentType` middleware sets `application/json` on every
>   response, and `http.FileServer` only sets the correct type when none
>   is present — without the `Del` we'd ship CSS as `application/json`.
> - A new test `TestAdminRoutes_JSONNotShadowedByFileServer` guards the mux
>   precedence rule that lets `GET /admin/dashboard` win over the
>   `/admin/` catch-all file server.
>
> **Out of scope (not implemented):**
> - Q2 (Revoked-users tab), Q4 (bulk select), Q5 (server-rendered/no-JS),
>   Q6 (i18n) — none of these were requested.
> - Playwright-style browser automation. The Go tests cover wiring and
>   asset content-type behavior; UI behavior is verified by hand.

## Overview

Ship a small, server-rendered HTML admin page that consumes the JSON endpoints from [plan 17](../17-admin-api-and-auth/plan.md). The page is **embedded in the Go binary** via `embed.FS`, served from `GET /admin/` (and `GET /admin/index.html`) behind the same Basic Auth that protects the API. The UI provides:

- A **dashboard** showing the waiting-list count, with-access count, total, and a per-day enlistment chart for the last 90 days (configurable).
- A **users-with-access** view: searchable by email, listing each user with grant timestamp, grant source (`scheduler` or `admin`), and (if revoked) revoke reason; each row has a **Revoke** action that prompts for a reason.
- A **waiting-list** view: searchable by email, listing each waiting-list row with the user's `weight` and timestamps; each row has a **Grant access now** action and a **Remove from waiting list** action.

**This plan depends on plan 17.** All data flows through the `/admin/*` JSON endpoints — the UI never talks to the database directly.

### Key Design Decisions

| Decision | Rationale |
|---|---|
| Server-rendered HTML embedded in the Go binary (`//go:embed`) | Matches the project's stdlib-first ethos. No build step, no separate frontend repo, no node_modules. Distribution is one binary + `config.json`. |
| Single HTML page with three tabs (dashboard / access / waitlist) | Simpler than three pages; one set of CSS, one navigation pattern. State is owned in the URL hash (`#tab=waitlist`) so refresh and bookmark behavior is sane. |
| Vanilla JS (`fetch`), no framework | The interactive surface area is tiny: render a table, run a search, fire a POST/DELETE. React/Vue/Svelte would dwarf the actual code. |
| Chart: hand-rolled inline **SVG** bar chart | Adding Chart.js (or any CDN) means either bundling ~200 KB or pulling JS at runtime, both of which bite on locked-down deployments. A 60-line SVG renderer covers a daily bar chart trivially. **Open Question 1** flags this if the team would rather take the dependency. |
| Styling: a single embedded `admin.css`, no Tailwind / utility framework | Same dependency-discipline argument as the JS. The page has maybe 200 lines of CSS at most. |
| Forms post via `fetch` with JSON; the page never reloads on action | Better UX than form-encoded POSTs that 302 back. Keeps the URL stable while users browse. |
| Confirmation modal for revoke + delete actions | Both are destructive; a one-click slip should not silently revoke a user's access. The modal collects the **reason** for revoke. |
| Static assets served by `http.FileServer(http.FS(embedFS))` behind the existing admin auth middleware | Reuses the auth wiring from plan 17 verbatim. |
| Page polls `/admin/dashboard` only on tab activation, not on a timer | Dashboards that auto-refresh waste cycles and confuse users mid-action. Manual refresh button instead. |

### Dependencies

- Builds on: [17-admin-api-and-auth](../17-admin-api-and-auth/plan.md) (every action goes through those endpoints).
- Required by: nothing — this is a leaf feature.
- No new Go module dependencies.

---

## Requirements

1. **Page route**: `GET /admin/` returns `index.html` from the embedded filesystem. `GET /admin/static/*` serves CSS, JS, and any future assets. Both are protected by the same Basic Auth middleware.
2. **Dashboard tab**:
   - Shows three counters: **Waiting list**, **With access**, **Total** (waitlist + with-access).
   - Shows a per-day bar chart of enlistments for the last `N` days (default 90, user can change to 7 / 30 / 90 / 365 via a select).
   - Has a **Refresh** button.
3. **Users-with-access tab**:
   - Search box filters by email substring (debounced 250 ms; submits to `/admin/users/access?email=…`).
   - Table columns: Email, Name, Enlisted at, Granted at, Granted by, Revoke status (badge), Actions.
   - **Revoke** button per row → opens confirmation modal with a required `reason` textarea (max 500 chars). Submits `POST /admin/users/{id}/revoke-access`.
   - If a user is currently revoked (granted then revoked, not re-granted), the row is shown with the badge **Revoked** and the revoke reason inline. (These users still appear here only if `has_access=true`. Revoked users have `has_access=false` and therefore appear in **neither** list — consistent with the data model from plan 16. This is expected: once revoked the user can re-sign-up via `POST /waitinglist`. **Open Question 2** asks whether revoked users should have their own tab.)
   - Pagination: Prev / Next buttons (`?offset=` shifts by `limit`); page size selector (25 / 50 / 100).
4. **Waiting-list tab**:
   - Search box filters by email substring (same debounce). Hits `/admin/users/waitlist?email=…`.
   - Table columns: Email, Name, Weight, Enlisted at, Effective queue time (`weighted_created_at`), Actions.
   - **Grant access now** button per row → confirmation modal → `POST /admin/users/{user_id}/grant-access`. On success, the row is removed from the table without a full reload.
   - **Remove** button per row → confirmation modal → `DELETE /admin/waitlist/{entry_id}`.
   - Same pagination scheme as the access tab.
5. **Auth UX**: When the basic-auth prompt is cancelled, the browser shows the standard 401 page. We do not customize this.
6. **No JS framework, no CDN**: assets are local and embedded. The page must work with JS enabled; we do not provide a no-JS fallback (all actions are mutations and require JS in any case).
7. **Tests**: Go tests verify the asset routes are served correctly under auth; behavior of the page itself is tested by hand (and by a small Playwright/curl smoke test described below — automation is out of scope unless the team wants to invest).

---

## Design

### Directory layout

```
internal/handler/adminui/
├── adminui.go              # http.Handler + embed.FS
├── static/
│   ├── index.html
│   ├── admin.css
│   └── admin.js
```

`adminui.go`:

```go
package adminui

import (
    "embed"
    "io/fs"
    "net/http"
)

//go:embed static
var staticFS embed.FS

// Handler returns an http.Handler that serves the embedded admin SPA. Strip
// the /admin/ prefix before invoking; the caller is responsible for auth.
func Handler() http.Handler {
    sub, err := fs.Sub(staticFS, "static")
    if err != nil {
        panic(err) // embed paths are compile-time validated
    }
    return http.FileServer(http.FS(sub))
}
```

### Wire-up (`cmd/server/main.go`)

Inside the same `if adminUser != ""` block introduced in plan 17:

```go
adminMux := http.NewServeMux()
adminHandler.RegisterRoutes(adminMux)
adminMux.Handle("/admin/", http.StripPrefix("/admin/", adminui.Handler()))

mux.Handle("/admin/", auth(adminMux))
```

`http.ServeMux` routes `/admin/foo.css` to the file-server fallback because the JSON routes register more specific patterns (`/admin/dashboard`, `/admin/users/...`).

### `index.html` (sketch)

A single HTML file with three sections gated by the `data-tab` attribute and a tiny `admin.js` that:

1. On load, reads `location.hash` (`#tab=dashboard|access|waitlist`); defaults to `dashboard`.
2. Provides three render functions, one per tab. Each fetches from the relevant `/admin/*` endpoint and re-renders the table.
3. Provides modal helpers (`confirmAction(title, body, onConfirm)`).
4. Provides an SVG bar chart helper given an array of `{day, count}` objects.

### SVG chart helper (~50 lines)

```js
function renderBarChart(svg, data) {
  const W = 720, H = 220, P = 24;
  const max = Math.max(1, ...data.map(d => d.count));
  const bw = (W - 2 * P) / data.length;
  svg.setAttribute("viewBox", `0 0 ${W} ${H}`);
  svg.innerHTML = data.map((d, i) => {
    const h = ((H - 2 * P) * d.count) / max;
    const x = P + i * bw, y = H - P - h;
    return `<rect x="${x}" y="${y}" width="${bw - 1}" height="${h}"
      fill="#4f46e5"><title>${d.day}: ${d.count}</title></rect>`;
  }).join("");
  // Y-axis label, x-axis ticks every Nth day
}
```

### CSS

A neutral dark-on-light style at `~150 lines`. Tabs, table styling, modal, and a sticky header. No external fonts (system stack).

### Error handling

Every fetch wraps the response in a `try/catch`; on failure, render an inline banner at the top of the active tab (`"Failed to load dashboard: 503 Service Unavailable"`). Modal forms keep the modal open and show the inline error (so the operator can retry without re-typing the reason).

### Server-side validation echoes

When the API returns `400` with `{"error": "…"}`, the modal displays that message verbatim. Plan 17 already produces structured errors via `WriteError`.

---

## Implementation Steps

1. **Create directory** `internal/handler/adminui/static/` with `index.html`, `admin.css`, `admin.js`.
2. **Implement `adminui.Handler()`** with `//go:embed`.
3. **Wire** the file-server route into the admin sub-mux in `cmd/server/main.go`.
4. **Implement `index.html`** with the three-tab skeleton, search inputs, tables, and the modal element.
5. **Implement `admin.js`** with: tab routing, fetchers for each endpoint, table renderers, debounced search, action handlers (grant / revoke / delete), inline error banner, SVG chart renderer.
6. **Implement `admin.css`** for tabs, table, modal, and chart axes.
7. **Tests**:
   - Go test verifying `GET /admin/` returns the embedded HTML behind auth.
   - Go test verifying `GET /admin/static/admin.js` returns the embedded JS behind auth.
   - Go test verifying `GET /admin/` without auth returns `401`.
   - Manual / smoke checklist below.
8. **Docs**: extend the HTTP Endpoints table in CLAUDE.md to mention `/admin/` (UI) alongside the API routes.
9. **Verify**: `make format && make lint && make test`. Boot the server with a hashed admin password, point a browser at `http://localhost:8080/admin/`, exercise every action.

---

## Testing

### Go unit tests (`internal/handler/adminui/adminui_test.go`)

| # | Case | Expected |
|---|---|---|
| 1 | `Handler()` serves `index.html` at `/` | `200`, body contains a known marker (`<title>Waiting List Admin</title>`) |
| 2 | `Handler()` serves `admin.js` at `/static/admin.js` | `200`, `Content-Type: text/javascript` (or `application/javascript`) |
| 3 | `Handler()` 404s on a missing asset | `404` |

### Integration with auth (`cmd/server/...` or a new `internal/handler/admin_routes_test.go`)

| # | Case | Expected |
|---|---|---|
| 4 | `GET /admin/` without credentials | `401`, `WWW-Authenticate` set |
| 5 | `GET /admin/` with valid credentials | `200`, HTML body |
| 6 | `GET /admin/static/admin.css` with valid credentials | `200`, CSS body |
| 7 | `GET /admin/dashboard` (the API route) is **not** shadowed by the file server | API JSON returned, not an HTML 404 — verifies the mux precedence |

### Manual / browser smoke test (checklist)

| # | Action | Expected |
|---|---|---|
| 8  | Browser hits `/admin/`, prompts for auth | basic auth dialog |
| 9  | Wrong password → re-prompt | yes |
| 10 | Dashboard tab loads counts and chart | yes |
| 11 | Switch to "Users with access", search "alice" | rows filter live |
| 12 | Click Revoke, leave reason blank, submit | inline error "reason is required" |
| 13 | Click Revoke, enter 1-char reason, submit | row disappears (user no longer has access); toast "revoked" |
| 14 | Switch to "Waiting list", search "bob" | filter works |
| 15 | Click "Grant access now", confirm | row disappears; if you switch to "Users with access", new row visible |
| 16 | Click "Remove" on a waitlist row, confirm | row disappears |
| 17 | Refresh during a search | URL retains the active tab; search box re-populates from URL params |
| 18 | Server returns `503` (e.g. DB down) | banner shows the error; UI does not crash |
| 19 | Tab through interactive elements with keyboard | focus order is sensible; modals trap focus |

### Optional automation (out of scope by default)

A Playwright smoke test could automate items #8–#18. Not required for this plan; flagged for a follow-up if the UI churns.

---

## Acceptance Criteria

- [ ] `GET /admin/` (and `/admin/static/*`) is served from `embed.FS` behind Basic Auth.
- [ ] Dashboard shows three counters and an SVG bar chart with a configurable window.
- [ ] Users-with-access list is searchable by email substring and supports paginated browsing.
- [ ] Revoke action opens a modal, requires a non-empty reason, calls `POST /admin/users/{id}/revoke-access`, and removes the row on success.
- [ ] Waiting-list view shows `weight` and timestamps and supports paginated browsing.
- [ ] "Grant access now" calls `POST /admin/users/{id}/grant-access` and removes the row on success.
- [ ] "Remove from waiting list" calls `DELETE /admin/waitlist/{entry_id}`.
- [ ] No external network requests are made by the page (no CDN fonts, no CDN JS).
- [ ] `GET /admin/dashboard` (API) is not shadowed by the file-server fallback.
- [ ] CLAUDE.md HTTP Endpoints table mentions `/admin/` (UI) alongside the API routes.
- [ ] `make format`, `make lint`, and `make test` all pass.

---

## Open Questions

1. **Hand-rolled SVG vs. Chart.js** — Default is a tiny SVG renderer (no deps). Chart.js gives polish (tooltips, animations) but adds ~200 KB of JS. Confirm preference; flipping is mostly a `<script>` swap.
2. **Should "revoked" users have their own tab?** — Plan 16 leaves a revoked user with `has_access=false` and no waitlist row, so they are invisible to both current lists. If support / debugging needs to find them, a third tab "Revoked" backed by a new `/admin/users/revoked` endpoint would be cheap. Not included by default.
3. **Embedded static-asset versioning** — On change, browsers may serve stale cached JS/CSS. Add a build-time hash query (`admin.js?v=<sha>`) or a `Cache-Control: no-cache` header? Default in this plan: `Cache-Control: no-cache` on the file server, since changes are rare and the asset budget is tiny.
4. **Bulk operations** — Should the lists support multi-select for bulk grant/revoke/delete? Out of scope; plan 17 endpoints take a single ID. Easy follow-up if the operator volume justifies it.
5. **Server-rendered vs. embedded SPA** — Default is embedded SPA. If accessibility/SEO/no-JS concerns ever surface (unlikely behind admin auth), we could switch to `html/template`-rendered pages with form posts.
6. **Internationalization** — Strings are hard-coded English. Out of scope.
