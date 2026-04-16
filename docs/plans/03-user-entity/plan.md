# User Entity Plan

## Overview

Implement the user entity feature — the core data model representing a user in the system. This includes the Go struct, repository layer for database operations, and HTTP handlers for creating and retrieving users.

## Requirements

- User entity fields: `id` (UUID), `firstname`, `lastname`, `email`
- Add a new user entity (create)
- Get a user entity by email (read)
- Users are stored in the `user_entity` PostgreSQL table
- Email must be unique — duplicate email submissions return an appropriate error

## Design

### Model

```go
type UserEntity struct {
    ID        string `json:"id"`
    Firstname string `json:"firstname"`
    Lastname  string `json:"lastname"`
    Email     string `json:"email"`
    HasAccess bool   `json:"has_access"`
}
```

### Repository Interface

```go
type UserRepository struct {
    db *sql.DB
}

func (r *UserRepository) Create(ctx context.Context, user *UserEntity) error
func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*UserEntity, error)
```

### Repository Details

#### `Create`
- Inserts a new row into `user_entity`
- Uses `RETURNING id` to capture the generated UUID
- Returns a conflict error if email already exists (catch PG unique violation `23505`)

#### `GetByEmail`
- Queries `user_entity` by `email`
- Returns the full `UserEntity` struct
- Returns a not-found error if no row matches

### Input Validation

Before persisting, validate:
- `firstname` is non-empty
- `lastname` is non-empty
- `email` is non-empty and contains `@`

Return `400 Bad Request` with a descriptive message on validation failure.

## Implementation Steps

- [x] Define `UserEntity` struct in `internal/model/model.go`
- [x] Implement `UserRepository` in `internal/repository/user.go`
  - [x] `Create` method with unique constraint error handling
  - [x] `GetByEmail` method with not-found handling
- [x] Implement HTTP handlers in `internal/handler/user.go`
  - [x] `POST /users` — create a new user entity
  - [x] `GET /users?email=<email>` — retrieve user by email
- [x] Add input validation for create requests
- [x] Write unit tests for repository and handler layers

## Testing

### Unit Tests — User Repository (`internal/repository/user.go`)

- **Core logic**:
  - Test `Create` inserts a user and returns a populated `UserEntity` with a generated UUID.
  - Test `GetByEmail` returns the correct user when the email exists.
- **Edge cases**:
  - Test `Create` with maximum-length `firstname`, `lastname`, and `email` values.
  - Test `GetByEmail` is case-sensitive (or document if case-insensitive).
- **Error/negative scenarios**:
  - Test `Create` with a duplicate email returns a conflict/unique-violation error.
  - Test `GetByEmail` with a non-existent email returns a not-found error.

### Unit Tests — User Handler (`internal/handler/user.go`)

- **Core logic**:
  - Test `POST /users` with valid JSON returns `201 Created` and the created user in the response body.
  - Test `GET /users?email=<email>` returns `200 OK` and the correct user.
- **Edge cases**:
  - Test `POST /users` with extra/unknown JSON fields — they should be ignored.
  - Test `GET /users` without the `email` query parameter returns `400 Bad Request`.
- **Error/negative scenarios**:
  - Test `POST /users` with missing `firstname` returns `400 Bad Request`.
  - Test `POST /users` with missing `lastname` returns `400 Bad Request`.
  - Test `POST /users` with missing or invalid `email` (no `@`) returns `400 Bad Request`.
  - Test `POST /users` with empty JSON body returns `400 Bad Request`.
  - Test `POST /users` with a duplicate email returns `409 Conflict`.
  - Test `GET /users?email=notfound@example.com` returns `404 Not Found`.
  - Test unsupported HTTP methods (e.g., `DELETE /users`) return `405 Method Not Allowed`.

## Acceptance Criteria

- `POST /users` with valid JSON creates a user and returns `201 Created` with the user data
- `POST /users` with a duplicate email returns `409 Conflict`
- `POST /users` with missing/invalid fields returns `400 Bad Request`
- `GET /users?email=test@example.com` returns the user if found, `404 Not Found` otherwise
- All fields (`id`, `firstname`, `lastname`, `email`, `has_access`) are correctly persisted and returned

## Dependencies

- [Database](../02-database/plan.md) — `user_entity` table must exist
- [Project Setup](../01-project-setup/plan.md) — project structure and DB connection
