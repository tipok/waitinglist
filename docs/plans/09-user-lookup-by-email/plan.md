# User Lookup by Email Plan

## Overview

Add a new HTTP endpoint that accepts one or more email addresses and returns user information: first name, last name, email, whether the user has access, and the user creation date. The `user_entity` table currently lacks a `created_at` column, so a new migration is required to add it. The query is against `user_entity` only — no join with `waiting_list` is needed.

## Requirements

- Accept one or multiple email addresses as input via query parameters
- Return a list of user information: first name, last name, email, has_access, and user creation date
- Return an empty list when no user with the given email exists, return all users found
- Return `400` for missing or invalid email input
- Add `created_at` column to `user_entity` table via a new migration
- Follow existing project conventions (JSON responses, error format, middleware, `log/slog`)

## Design

### Database Migration

A new migration file `migrations/004_user_created_at.sql` adds a `created_at` column to `user_entity`:

```sql
ALTER TABLE user_entity ADD COLUMN IF NOT EXISTS created_at TIMESTAMP NOT NULL DEFAULT NOW();
```

> **Note:** Existing rows will receive the current timestamp as their `created_at` value. New rows will automatically get the insertion timestamp.

### Endpoint

| Method | Path                               | Description                        | Request Body | Success Response |
|--------|------------------------------------|------------------------------------|--------------|------------------|
| GET    | `/waitinglist/users?email=<email>` | Look up users info by email        | —            | `200 OK`         |

Multiple emails can be passed as repeated query parameters: `/waitinglist/users?email=a@example.com&email=b@example.com`

### Response Body

```json
{
  "users": [
    {
      "firstname": "Jane",
      "lastname": "Doe",
      "email": "jane@example.com",
      "has_access": false,
      "created_at": "2026-04-10T14:30:00Z"
    }
  ]
}
```

### Response Model

A new struct `UserInfo` in `internal/model/model.go`:

```go
type UserInfo struct {
    Firstname string    `json:"firstname"`
    Lastname  string    `json:"lastname"`
    Email     string    `json:"email"`
    HasAccess bool      `json:"has_access"`
    CreatedAt time.Time `json:"created_at"`
}
```

A wrapper struct for the list response:

```go
type UserInfoList struct {
    Users []UserInfo `json:"users"`
}
```

### Model Update

Update `UserEntity` to include `CreatedAt`:

```go
type UserEntity struct {
    ID        string    `json:"id"`
    Firstname string    `json:"firstname"`
    Lastname  string    `json:"lastname"`
    Email     string    `json:"email"`
    HasAccess bool      `json:"has_access"`
    CreatedAt time.Time `json:"created_at"`
}
```

### Error Responses

| Status | Condition                                          |
|--------|----------------------------------------------------|
| 400    | Missing `email` query parameter or invalid format  |
| 500    | Internal server error                              |

Error body follows the existing format:

```json
{
    "error": "human-readable error message"
}
```

### SQL Query

Query `user_entity` directly — no join required:

```sql
SELECT firstname, lastname, email, has_access, created_at
FROM user_entity
WHERE email = ANY($1)
```

### Repository Layer

Add a new method to `UserRepository`:

```go
func (r *UserRepository) GetUserInfoByEmails(ctx context.Context, emails []string) ([]model.UserInfo, error)
```

This method queries `user_entity` by email(s) and returns a slice of `UserInfo`. Returns an empty slice when no matching users are found.

### Handler Layer

Add a new handler method on `WaitingListHandler` and register the route:

```go
mux.HandleFunc("GET /waitinglist/users", h.handleGetUsersByEmail)
```

The handler:
1. Reads the `email` query parameter(s) from `r.URL.Query()["email"]`
2. Validates each email (non-empty, contains `@`)
3. Calls the repository method
4. Returns `200` with the user info list (empty list if no users found)

### Interface Update

Extend the `WaitingListUserStore` interface in `internal/handler/waitinglist.go`:

```go
type WaitingListUserStore interface {
    CreateTx(ctx context.Context, tx model.DBTX, user *model.UserEntity) error
    GetByEmailTx(ctx context.Context, tx model.DBTX, email string) (*model.UserEntity, error)
    GetUserInfoByEmails(ctx context.Context, emails []string) ([]model.UserInfo, error)
}
```

## Implementation Steps

- [ ] Create migration `migrations/004_user_created_at.sql` to add `created_at` to `user_entity`
- [ ] Update `UserEntity` struct in `internal/model/model.go` to include `CreatedAt`
- [ ] Add `UserInfo` and `UserInfoList` structs to `internal/model/model.go`
- [ ] Update existing repository queries that SELECT/INSERT on `user_entity` to include `created_at`
- [ ] Implement `GetUserInfoByEmails` in `internal/repository/user.go`
- [ ] Add unit tests for `GetUserInfoByEmails` in `internal/repository/user_test.go`
- [ ] Update `WaitingListUserStore` interface in `internal/handler/waitinglist.go`
- [ ] Implement `handleGetUsersByEmail` handler in `internal/handler/waitinglist.go`
- [ ] Register the `GET /waitinglist/users` route in `RegisterRoutes`
- [ ] Add unit tests for the handler in `internal/handler/waitinglist_test.go`
- [ ] Update route registration tests in `internal/handler/routes_test.go`
- [ ] Run `make format`, `make lint`, and `make test` to verify

## Testing

### Unit Tests — Repository (`internal/repository/user_test.go`)

- **Core logic**:
  - Test `GetUserInfoByEmails` returns correct user info (firstname, lastname, email, has_access, created_at) for existing users.
  - Test with multiple emails — returns all matching users.
- **Edge cases**:
  - Test with a mix of existing and non-existing emails — returns only the found users.
  - Test with a single email — returns a single-element list.
- **Error/negative scenarios**:
  - Test with non-existent emails — returns an empty slice.
  - Test with an empty email list — returns an empty slice.

> **Note:** Repository tests require a real PostgreSQL database and are gated by the `TEST_DATABASE_URL` environment variable.

### Unit Tests — Handler (`internal/handler/waitinglist_test.go`)

- **Core logic**:
  - Test `GET /waitinglist/users?email=valid@example.com` returns `200` with correct JSON body containing a `users` list.
  - Test with multiple email parameters returns all matching users.
- **Edge cases**:
  - Test with email that has leading/trailing whitespace — should still work after trimming.
  - Test with no matching emails — returns `200` with an empty `users` list.
- **Error/negative scenarios**:
  - Test `GET /waitinglist/users` without `email` parameter — returns `400`.
  - Test `GET /waitinglist/users?email=invalid` (no `@`) — returns `400`.
  - Test `GET /waitinglist/users?email=valid@example.com` when repository returns an internal error — returns `500`.

### Route Registration Tests (`internal/handler/routes_test.go`)

- Test that `GET /waitinglist/users` is routed correctly.
- Test that `POST /waitinglist/users` returns `405 Method Not Allowed`.

## Acceptance Criteria

- `GET /waitinglist/users?email=<email>` returns `200` with a `users` list containing firstname, lastname, email, has_access, and created_at
- Multiple email parameters return all matching users
- No matching emails returns `200` with an empty `users` list
- Missing or invalid email returns `400` with a descriptive error message
- Response uses the standard JSON error format for errors
- `user_entity` table has a `created_at` column populated automatically
- All existing tests continue to pass
- `make format`, `make lint`, and `make test` all pass

## Dependencies

- [Database](../02-database/plan.md) — schema with `user_entity` table
- [API](../05-api/plan.md) — route registration, middleware, error response helpers
