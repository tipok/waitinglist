# API Refactor Plan

## Overview

Refactor the HTTP API to remove all standalone user entity endpoints (`POST /users`, `GET /users`). The service should not expose a way to create or query users directly. Users are only created as a side effect of being added to the waiting list — the `POST /waitinglist` endpoint already handles user creation internally within a transaction.

This simplifies the API surface to only waiting list operations.

## Requirements

- Remove all user HTTP routes (`POST /users`, `GET /users`)
- Remove the `UserHandler` struct, its methods, and its route registration
- Remove the `UserStore` interface (used only by the user handler)
- Remove the standalone user handler tests
- Remove the `docs/requests/users.http` request file
- Keep the `UserRepository` and its transactional methods (`CreateTx`, `GetByEmailTx`) — these are still used by the `WaitingListHandler`
- Keep the non-transactional `Create` and `GetByEmail` methods on `UserRepository` only if they are referenced elsewhere; remove them if they are now unused
- Update `cmd/server/main.go` to stop creating and registering the `UserHandler`
- Update route registration tests to reflect the removal of user routes
- Ensure all existing waiting list tests continue to pass

## Design

### Endpoints After Refactor

| Method | Path           | Description                    | Request Body                       | Success Response |
|--------|----------------|--------------------------------|------------------------------------|------------------|
| POST   | `/waitinglist` | Add a user to the waiting list | `{"firstname","lastname","email"}` | `201 Created`    |
| GET    | `/waitinglist` | List all waiting list entries  | —                                  | `200 OK`         |

The `/users` endpoints are fully removed. User entity creation happens exclusively inside the `POST /waitinglist` transactional flow.

### Files to Modify

| File                              | Action                                                        |
|-----------------------------------|---------------------------------------------------------------|
| `internal/handler/user.go`        | Delete file                                                   |
| `internal/handler/user_test.go`   | Delete file                                                   |
| `internal/handler/routes_test.go` | Update to remove user route assertions                        |
| `cmd/server/main.go`              | Remove `UserHandler` creation and route registration          |
| `docs/requests/users.http`        | Delete file                                                   |
| `internal/repository/user.go`     | Remove unused non-transactional methods (`Create`, `GetByEmail`) if no longer referenced |
| `internal/repository/user_test.go`| Remove tests for deleted methods                              |
| `docs/plans/05-api/plan.md`       | Update endpoint table to reflect removal of user routes       |

## Implementation Steps

- [x] Delete `internal/handler/user.go`
- [x] Delete `internal/handler/user_test.go`
- [x] Delete `docs/requests/users.http`
- [x] Update `cmd/server/main.go`:
  - Remove the `userHandler` variable and `handler.NewUserHandler(...)` call
  - Remove `userHandler.RegisterRoutes(mux)`
  - Keep `userRepo` since it is still passed to `NewWaitingListHandler`
- [x] Update `internal/handler/routes_test.go`:
  - Remove test cases for `POST /users`, `GET /users`, and method-not-allowed tests on `/users`
  - Add test verifying `/users` now returns `404`
  - Verify remaining waiting list route tests still pass
- [x] Audit `internal/repository/user.go` for unused methods:
  - `Create` and `GetByEmail` are still used by integration tests in `waitinglist_test.go` — kept
  - `CreateTx` and `GetByEmailTx` used by `WaitingListHandler` — kept
- [x] Run `make format` and `make test` to verify everything passes
- [x] Update `docs/plans/05-api/plan.md` endpoint table to remove user routes

## Testing

Since this is a deletion/simplification refactor, the primary testing concern is ensuring nothing breaks:

### Existing Tests to Verify
- **Waiting list handler tests** (`internal/handler/waitinglist_test.go`) — must all continue to pass unchanged. These cover the full user-creation-within-waiting-list flow.
- **Waiting list repository tests** (`internal/repository/waitinglist_test.go`) — must all continue to pass unchanged.
- **Route registration tests** (`internal/handler/routes_test.go`) — must be updated to remove user route assertions and then pass.
- **Middleware tests** (`internal/handler/middleware_test.go`) — should be unaffected.
- **Response helper tests** (`internal/handler/response_test.go`) — should be unaffected.

### Tests to Remove
- `internal/handler/user_test.go` — all tests (file deleted with the handler).
- Tests for `Create` and `GetByEmail` in `internal/repository/user_test.go` if those methods are removed.

### Verification
- `make test` passes with no failures.
- `make lint` reports no issues.
- No compilation errors after removing the user handler and its references.

## Acceptance Criteria

- `POST /users` and `GET /users` endpoints no longer exist (requests return `404 Not Found`)
- `POST /waitinglist` continues to create user entities as part of the waiting list flow
- `GET /waitinglist` continues to list all waiting list entries
- All remaining tests pass
- No dead code (unused handler, interfaces, or repository methods) remains
- `cmd/server/main.go` no longer references the user handler

## Dependencies

- [User Entity](../03-user-entity/plan.md) — repository layer is partially retained (transactional methods only)
- [Waiting List](../04-waiting-list/plan.md) — unchanged, still the primary feature
- [API](../05-api/plan.md) — endpoint table must be updated to reflect the reduced API surface
