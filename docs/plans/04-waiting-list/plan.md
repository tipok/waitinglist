# Waiting List Plan

## Overview

Implement the waiting list feature — allows adding users to the waiting list and tracking when they signed up. When a new user is added to the waiting list, a user entity is created (or looked up if the email already exists) and a corresponding entry is inserted into the `waiting_list` table with a timestamp.

## Requirements

- Add a user to the waiting list (creates the user entity if needed, then adds a waiting list entry)
- Store the user ID and the datetime of sign-up in the `waiting_list` table
- A user can only be on the waiting list once
- Provide an endpoint to list all waiting list entries (optional, useful for admin)

## Design

### Model

```go
type WaitingListEntry struct {
    ID        string    `json:"id"`
    UserID    string    `json:"user_id"`
    CreatedAt time.Time `json:"created_at"`
}
```

### Repository

```go
type WaitingListRepository struct {
    db *sql.DB
}

func (r *WaitingListRepository) Add(ctx context.Context, userID string) (*WaitingListEntry, error)
func (r *WaitingListRepository) GetAll(ctx context.Context) ([]WaitingListEntry, error)
```

### Repository Details

#### `Add`
- Inserts a new row into `waiting_list` with the given `user_id`
- `created_at` defaults to `NOW()` in the database
- Returns a conflict error if the user is already on the waiting list (unique constraint on `user_id`)

#### `GetAll`
- Returns all waiting list entries ordered by `created_at ASC`
- Used for admin/debugging purposes

### Workflow: Adding to the Waiting List

The handler orchestrates the full flow:

1. Parse the incoming JSON (`firstname`, `lastname`, `email`)
2. Check if a user entity with that email already exists
   - If yes, use the existing user's ID
   - If no, create a new user entity and use its ID
3. Add the user ID to the `waiting_list` table
4. Return the combined response (user entity + waiting list entry)

This keeps the operation atomic from the caller's perspective. Use a database transaction to ensure consistency.

## Implementation Steps

- [ ] Define `WaitingListEntry` struct in `internal/model/model.go`
- [ ] Implement `WaitingListRepository` in `internal/repository/waitinglist.go`
  - [ ] `Add` method with unique constraint handling
  - [ ] `GetAll` method
- [ ] Implement HTTP handlers in `internal/handler/waitinglist.go`
  - [ ] `POST /waitinglist` — add a user to the waiting list (full flow)
  - [ ] `GET /waitinglist` — list all entries (optional)
- [ ] Wrap the create-user + add-to-list flow in a transaction
- [ ] Write unit tests

## Testing

### Unit Tests — Waiting List Repository (`internal/repository/waitinglist.go`)

- **Core logic**:
  - Test `Add` inserts a waiting list entry and returns a populated `WaitingListEntry` with a generated UUID and timestamp.
  - Test `GetAll` returns all entries ordered by `created_at ASC`.
- **Edge cases**:
  - Test `GetAll` on an empty table returns an empty slice (not nil).
  - Test `Add` populates `created_at` automatically via database default.
- **Error/negative scenarios**:
  - Test `Add` with a duplicate `user_id` returns a conflict/unique-violation error.
  - Test `Add` with a non-existent `user_id` returns a foreign key violation error.

### Unit Tests — Waiting List Handler (`internal/handler/waitinglist.go`)

- **Core logic**:
  - Test `POST /waitinglist` with valid JSON (new user) creates the user entity and waiting list entry, returns `201 Created`.
  - Test `POST /waitinglist` with an existing user email (not yet on list) reuses the user and adds them to the list, returns `201 Created`.
  - Test `GET /waitinglist` returns all entries sorted by sign-up time.
- **Edge cases**:
  - Test `GET /waitinglist` with no entries returns an empty JSON array.
  - Test `POST /waitinglist` with extra/unknown JSON fields — they should be ignored.
- **Error/negative scenarios**:
  - Test `POST /waitinglist` with an email already on the waiting list returns `409 Conflict`.
  - Test `POST /waitinglist` with missing or invalid fields returns `400 Bad Request`.
  - Test `POST /waitinglist` with empty JSON body returns `400 Bad Request`.
  - Test that the transaction rolls back if the waiting list insert fails after the user entity was created.
  - Test unsupported HTTP methods (e.g., `DELETE /waitinglist`) return `405 Method Not Allowed`.

## Acceptance Criteria

- `POST /waitinglist` with valid JSON creates the user entity and waiting list entry, returns `201 Created`
- `POST /waitinglist` with an email already on the list returns `409 Conflict`
- `POST /waitinglist` with an existing user (same email) who is not yet on the list adds them successfully
- `GET /waitinglist` returns all entries sorted by sign-up time
- `created_at` is automatically populated by the database
- The user entity + waiting list insert is transactional

## Dependencies

- [Database](../02-database/plan.md) — `waiting_list` table must exist
- [User Entity](../03-user-entity/plan.md) — user entity repository is used within the waiting list handler
- [Project Setup](../01-project-setup/plan.md) — project structure and DB connection
