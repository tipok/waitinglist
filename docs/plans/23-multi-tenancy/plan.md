# 23 — Multi-Tenancy (Project-Scoped API)

> **Status:** 📋 Planned

## Overview

Restructure the waiting list service so a single deployment can serve multiple SaaS projects. Each project gets an isolated set of users, waiting lists, and scheduler configuration. This enables re-use across different products without separate deployments.

Project identification uses two mechanisms:
- **Frontend requests** (served on different domains): resolved via Host header mapping in config, OR via a custom request header set by a reverse proxy/API gateway.
- **Service-to-service requests**: use the custom header directly (e.g. `X-Project-ID`) since domain routing doesn't apply.

Precedence: custom header → Host mapping → default project (backwards-compatible) → reject with 400.

### Key Design Decisions

| Decision | Rationale |
|---|---|
| Shared tables with `project_id` column | Simplest to implement; one DB, one connection pool, queries filter by project. Physical isolation adds operational complexity that isn't justified yet. |
| Custom header (`X-Project-ID`) for service-to-service | Domain-based routing doesn't apply for inter-service calls. A dedicated header is explicit and doesn't overload `X-Forwarded-For` (which carries proxy semantics). |
| Host mapping **and** header (both supported) | Frontend traffic arrives on different domains — a proxy sets the header OR the service maps Host → project directly. Supporting both covers gateway-less dev setups and proper prod infrastructure. |
| Per-project scheduler config stored in DB | Projects are dynamic (created via admin API). Storing scheduler parameters alongside the project row avoids config-file changes when onboarding a new SaaS. |
| Global admin with project filter column | Simplest admin change. Operator sees everything; can narrow by project. Full project-scoped admin can follow later. |
| `defaultSlug` config for backwards-compatibility | Requests without a project header from an unmapped host resolve to the default project — existing integrations keep working without changes. |
| Email uniqueness scoped to `(project_id, email)` | The same person can sign up for different products' waiting lists independently. |

### Dependencies

- Builds on: all previous plans (entire existing schema and API surface).
- Required by: future per-project frontend deployments.
- No new Go module dependencies.

---

## Requirements

### Database

1. New `project` table with UUID PK, unique slug, display name, per-project scheduler config fields, and timestamps.
2. `project_id` column added to `user_entity`, `waiting_list`, and `scheduler_state` tables (NOT NULL after backfill).
3. A default project (`slug = 'default'`) is inserted; existing rows are backfilled with its ID.
4. Unique constraint on `user_entity(email)` becomes `(project_id, email)`.
5. Composite indexes: `waiting_list(project_id, weighted_created_at)`, `user_entity(project_id, has_access)`.

### Configuration

6. New config section `projects`:

   ```json
   {
     "projects": {
       "headerName": "X-Project-ID",
       "defaultSlug": "default",
       "hostMapping": {
         "waitlist.product-a.com": "product-a",
         "waitlist.product-b.com": "product-b"
       }
     }
   }
   ```

7. Env var mapping:
   - `projects.headerName` → `WL_PROJECTS_HEADERNAME`
   - `projects.defaultSlug` → `WL_PROJECTS_DEFAULTSLUG`
   - `projects.hostMapping` → not overridable via env (map type); must use config file.

8. Global `waitlist.*` and `schedulerInterval.*` remain as defaults when a project doesn't specify overrides.

### Tenant Resolution Middleware

9. New middleware in `internal/handler/tenant.go`.
10. Resolution precedence: configured header → Host lookup in `hostMapping` → `defaultSlug` (if set) → 400 error.
11. Resolved `*model.Project` is stored in request context.
12. Applied to `/waitinglist` and `/waitinglist/users` endpoints. **Not** applied to `/healthz` or `/admin/*` (admin uses an explicit `?project=` query parameter).

### Handler Changes

13. `WaitingListHandler` extracts project from context; passes `projectID` to repository calls.
14. `AdminHandler` gains optional `?project=<slug>` query parameter on list/dashboard endpoints. Empty = all projects.
15. New admin endpoints for project management:
    - `GET /admin/projects` — list all projects.
    - `POST /admin/projects` — create a project.
    - `PUT /admin/projects/{id}` — update project name/scheduler config.

### Repository Changes

16. All repository methods add `project_id` filtering. Methods operating within a tenant receive `projectID string` as a parameter.
17. Admin-facing queries (counts, lists) accept an optional `projectID` — empty string means all projects.
18. New `internal/repository/project.go` with CRUD for the `project` table.

### Scheduler Changes

19. Single global ticker iterates over all non-disabled projects per tick.
20. Per-project config (batch size, window interval) read from the `project` table.
21. `scheduler_state.key` becomes project-scoped: `"waitlist_last_success:<project_id>"`.

### Admin UI Changes

22. Project column added to user and waitlist tables.
23. Project filter dropdown on dashboard and list views.
24. Project management page (create/edit).

---

## Design

### Migration `008_multi_tenancy.sql`

```sql
-- Project table
CREATE TABLE IF NOT EXISTS project (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug                    TEXT UNIQUE NOT NULL,
    name                    TEXT NOT NULL,
    entry_batch_size        INT,
    entry_window_interval   INTERVAL,
    waitlist_check_interval INTERVAL,
    scheduler_disabled      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Insert default project
INSERT INTO project (slug, name) VALUES ('default', 'Default')
ON CONFLICT (slug) DO NOTHING;

-- Add project_id to user_entity
ALTER TABLE user_entity ADD COLUMN IF NOT EXISTS project_id UUID;
UPDATE user_entity SET project_id = (SELECT id FROM project WHERE slug = 'default') WHERE project_id IS NULL;
ALTER TABLE user_entity ALTER COLUMN project_id SET NOT NULL;
ALTER TABLE user_entity ADD CONSTRAINT fk_user_entity_project FOREIGN KEY (project_id) REFERENCES project(id);

-- Rebuild unique constraint: email is unique per project
ALTER TABLE user_entity DROP CONSTRAINT IF EXISTS user_entity_email_key;
CREATE UNIQUE INDEX IF NOT EXISTS uq_user_entity_project_email ON user_entity (project_id, email);

-- Add project_id to waiting_list
ALTER TABLE waiting_list ADD COLUMN IF NOT EXISTS project_id UUID;
UPDATE waiting_list SET project_id = (SELECT id FROM project WHERE slug = 'default') WHERE project_id IS NULL;
ALTER TABLE waiting_list ALTER COLUMN project_id SET NOT NULL;
ALTER TABLE waiting_list ADD CONSTRAINT fk_waiting_list_project FOREIGN KEY (project_id) REFERENCES project(id);

-- Add project_id to scheduler_state
ALTER TABLE scheduler_state ADD COLUMN IF NOT EXISTS project_id UUID;
UPDATE scheduler_state SET project_id = (SELECT id FROM project WHERE slug = 'default') WHERE project_id IS NULL;
ALTER TABLE scheduler_state ALTER COLUMN project_id SET NOT NULL;
ALTER TABLE scheduler_state ADD CONSTRAINT fk_scheduler_state_project FOREIGN KEY (project_id) REFERENCES project(id);

-- Performance indexes
CREATE INDEX IF NOT EXISTS idx_waiting_list_project_weighted ON waiting_list (project_id, weighted_created_at);
CREATE INDEX IF NOT EXISTS idx_user_entity_project_access ON user_entity (project_id, has_access);
```

### Config struct additions (`internal/config/config.go`)

```go
type ProjectsConfig struct {
    HeaderName  string            `koanf:"headerName"`
    DefaultSlug string            `koanf:"defaultSlug"`
    HostMapping map[string]string `koanf:"hostMapping"`
}
```

Default: `HeaderName = "X-Project-ID"`, `DefaultSlug = "default"`, `HostMapping = nil`.

### Model (`internal/model/model.go`)

```go
type Project struct {
    ID                     string         `json:"id"`
    Slug                   string         `json:"slug"`
    Name                   string         `json:"name"`
    EntryBatchSize         *int           `json:"entry_batch_size,omitzero"`
    EntryWindowInterval    *time.Duration `json:"entry_window_interval,omitzero"`
    WaitlistCheckInterval  *time.Duration `json:"waitlist_check_interval,omitzero"`
    SchedulerDisabled      bool           `json:"scheduler_disabled"`
    CreatedAt              time.Time      `json:"created_at"`
}
```

Add `ProjectID string` field to `UserEntity`, `WaitingListEntry`, `WaitingListAdminRow`.

### Tenant middleware (`internal/handler/tenant.go`)

```go
type ProjectResolver struct {
    headerName  string
    defaultSlug string
    hostMapping map[string]string // host → slug
    projects    map[string]*model.Project // slug → project (cached)
}

func (pr *ProjectResolver) Middleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        slug := r.Header.Get(pr.headerName)
        if slug == "" {
            host := r.Host
            if mapped, ok := pr.hostMapping[host]; ok {
                slug = mapped
            }
        }
        if slug == "" {
            slug = pr.defaultSlug
        }
        if slug == "" {
            WriteError(w, http.StatusBadRequest, "project identification required", nil)
            return
        }

        project, ok := pr.projects[slug]
        if !ok {
            WriteError(w, http.StatusBadRequest, "unknown project: "+slug, nil)
            return
        }

        ctx := context.WithValue(r.Context(), ctxKeyProject, project)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

### Scheduler redesign (`internal/waitlist/waitlist.go`)

```go
func Start(ctx context.Context, cfg *config.Config, projectRepo *repository.ProjectRepository, ...) error {
    ticker := time.NewTicker(cfg.SchedulerInterval.WaitlistCheckInterval)
    go func() {
        for {
            select {
            case <-ticker.C:
                projects, _ := projectRepo.GetAll(ctx)
                for _, p := range projects {
                    if p.SchedulerDisabled {
                        continue
                    }
                    processProject(ctx, p, cfg, waitingListRepo, userRepo, schedulerRepo)
                }
            case <-ctx.Done():
                ticker.Stop()
                return
            }
        }
    }()
    return nil
}
```

Each project uses its own config (falling back to the global config for nil fields).

### Wiring (`cmd/server/main.go`)

```go
projectRepo := repository.NewProjectRepository(db)
projects, _ := projectRepo.GetAll(ctx)
resolver := handler.NewProjectResolver(cfg.Projects.HeaderName, cfg.Projects.DefaultSlug, cfg.Projects.HostMapping, projects)

mux := http.NewServeMux()
// Tenant-scoped routes
tenantMux := http.NewServeMux()
waitListHandler.RegisterRoutes(tenantMux)
mux.Handle("/waitinglist", resolver.Middleware(tenantMux))
mux.Handle("/waitinglist/", resolver.Middleware(tenantMux))

// Non-tenant routes
healthHandler.RegisterRoutes(mux)
// Admin routes (project passed via ?project= query param, not middleware)
```

---

## Implementation Steps

1. **Write migration `008_multi_tenancy.sql`** — create `project` table, add `project_id` columns to existing tables, insert default project, backfill, rebuild indexes/constraints.
2. **Add `Project` model** — extend `internal/model/model.go` with `Project` struct, `ProjectID` field on existing types, context helper `ProjectFromContext`.
3. **Add project repository** — `internal/repository/project.go` with `GetAll`, `GetBySlug`, `Create`, `Update`.
4. **Extend config** — add `ProjectsConfig` to `internal/config/config.go` with defaults and env mapping.
5. **Implement tenant middleware** — `internal/handler/tenant.go` with `ProjectResolver`, context key, tests.
6. **Update `UserRepository`** — add `project_id` to INSERT/SELECT queries, scope uniqueness by project.
7. **Update `WaitingListRepository`** — add `project_id` to INSERT/SELECT/DELETE queries.
8. **Update `SchedulerRepository`** — scope `scheduler_state` queries by project.
9. **Update `WaitingListHandler`** — extract project from context, pass `ProjectID` to store calls.
10. **Rewrite scheduler** — iterate projects, use per-project config with global fallback.
11. **Update `AdminHandler`** — add `?project=` query filter, add project management endpoints (`GET/POST /admin/projects`, `PUT /admin/projects/{id}`).
12. **Update admin UI** — project column, filter dropdown, project management page.
13. **Wire in `main.go`** — init project cache, wrap tenant-scoped routes in middleware, pass project repo to scheduler.
14. **Verify** — `make format && make lint && make test`.

---

## Testing

### Tenant Middleware (`internal/handler/tenant_test.go`)

| # | Case | Expected |
|---|---|---|
| 1 | Request with `X-Project-ID: product-a` | Context carries `product-a` project |
| 2 | Request with Host matching `hostMapping` entry | Context carries mapped project |
| 3 | Header takes precedence over Host mapping | Header value wins |
| 4 | No header, no Host match, `defaultSlug` configured | Resolves to default project |
| 5 | No header, no Host match, no `defaultSlug` | 400 error |
| 6 | Header has unknown slug | 400 error |

### Project Repository (`internal/repository/project_test.go`, gated by `TEST_DATABASE_URL`)

| # | Case | Expected |
|---|---|---|
| 7 | Create project with valid slug | Returns populated struct with UUID |
| 8 | Create project with duplicate slug | Returns error (23505) |
| 9 | GetBySlug for existing project | Returns matching project |
| 10 | GetBySlug for non-existent slug | Returns not-found error |
| 11 | GetAll returns all projects | Returns ≥ 1 (default project from migration) |
| 12 | Update changes name and scheduler config | Re-read confirms changes |

### User Repository (project-scoped)

| # | Case | Expected |
|---|---|---|
| 13 | Create user with project A email `x@y.com`; create same email in project B | Both succeed |
| 14 | Create duplicate email within same project | Returns `ErrDuplicateEmail` |
| 15 | GetByEmailTx scoped to project A | Does not find project B's user |
| 16 | ListWithAccess with project filter | Only returns users from that project |
| 17 | CountByAccess with project filter | Counts scoped to project |

### Waiting List Repository (project-scoped)

| # | Case | Expected |
|---|---|---|
| 18 | Add entry for project A; query project B | Entry not visible |
| 19 | GetWithOffsetLimit filtered by project | Returns only that project's entries |
| 20 | ListJoined with project filter | Join respects project scope |

### Scheduler

| # | Case | Expected |
|---|---|---|
| 21 | Project with `scheduler_disabled = true` | Skipped |
| 22 | Project A config (batch=5); Project B config (batch=10) | Each processes its own batch size |
| 23 | Scheduler state key is project-scoped | Processing project A doesn't affect project B's cooldown |

### Handler Integration

| # | Case | Expected |
|---|---|---|
| 24 | POST `/waitinglist` with `X-Project-ID: product-a` | Creates user with `project_id` of product-a |
| 25 | GET `/waitinglist/users?email=x` scoped to project | Only returns users from that project |
| 26 | Admin `GET /admin/dashboard?project=product-a` | Stats scoped to product-a |
| 27 | Admin `GET /admin/dashboard` (no project filter) | Stats across all projects |
| 28 | Admin `POST /admin/projects` | Creates project in DB |

### End-to-end (manual smoke test)

```bash
# Create a project via admin
curl -u admin:changeme -X POST -d '{"slug":"acme","name":"Acme Corp"}' \
    http://localhost:8080/admin/projects

# Add user to acme project via header
curl -H "X-Project-ID: acme" -X POST \
    -d '{"firstname":"Alice","lastname":"Smith","email":"alice@example.com"}' \
    http://localhost:8080/waitinglist

# Same email in default project (should succeed)
curl -H "X-Project-ID: default" -X POST \
    -d '{"firstname":"Alice","lastname":"Smith","email":"alice@example.com"}' \
    http://localhost:8080/waitinglist

# Check isolation: lookup in acme only
curl -H "X-Project-ID: acme" \
    "http://localhost:8080/waitinglist/users?email=alice@example.com"
```

---

## Acceptance Criteria

- [ ] Migration `008_multi_tenancy.sql` creates `project` table, backfills default project, adds `project_id` columns with FK constraints and indexes.
- [ ] Existing data is assigned to the `default` project without downtime (idempotent migration).
- [ ] Requests with `X-Project-ID` header are resolved to the correct project.
- [ ] Requests with a mapped Host are resolved to the correct project.
- [ ] Header takes precedence over Host mapping when both are present.
- [ ] Requests without identification resolve to `defaultSlug` when configured; return 400 otherwise.
- [ ] Same email can be registered in different projects independently.
- [ ] `POST /waitinglist` creates user and entry scoped to the resolved project.
- [ ] `GET /waitinglist/users?email=` returns only users from the resolved project.
- [ ] Scheduler processes each project independently with per-project config.
- [ ] Admin endpoints support `?project=<slug>` filter; omitting it returns cross-project data.
- [ ] Admin project CRUD endpoints work (`GET/POST /admin/projects`, `PUT /admin/projects/{id}`).
- [ ] Admin UI shows project column and filter dropdown.
- [ ] `make format`, `make lint`, and `make test` all pass.

---

## Open Questions

1. **Project deletion** — Should projects be deletable? If so, what happens to their users and waiting-list entries? Recommendation: soft-delete (add `deleted_at`) or disallow deletion entirely (only disable via `scheduler_disabled`). Confirm preference.
2. **Project cache refresh** — When a project is created/updated via the admin API, should the in-memory resolver cache refresh immediately (invalidation) or on a timer (poll every N seconds)? Recommendation: immediate invalidation via a `Reload()` method called after admin project mutations.
3. **Rate limiting per project** — Should per-project rate limits be part of this plan or deferred? Recommendation: defer to a follow-up plan.
4. **Scheduler tick interval** — The global `schedulerInterval.waitlistCheckInterval` controls how often the scheduler wakes up. Should per-project `waitlist_check_interval` be respected as "don't process this project more frequently than X", or should each project get its own goroutine? Recommendation: single ticker at the fastest interval, per-project cooldown via `scheduler_state` timestamps.
5. **Host stripping of port** — Should Host mapping ignore the port portion (e.g. `product-a.com:8080` matches `product-a.com`)? Recommendation: yes, strip port before lookup.
