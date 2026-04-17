# API Plan

## Overview

Define and implement the HTTP API layer using Go's built-in `net/http.ServeMux`. This plan covers route registration, request/response conventions, error handling, and middleware (logging, content-type enforcement).

## Requirements

- Use only `net/http.ServeMux` — no third-party routers
- JSON request and response bodies
- Consistent error response format
- Proper HTTP status codes
- Basic request logging

## Design

### Endpoints

| Method | Path              | Description                        | Request Body                                      | Success Response  |
|--------|-------------------|------------------------------------|---------------------------------------------------|-------------------|
| POST   | `/waitinglist`    | Add a user to the waiting list     | `{"firstname","lastname","email"}`                | `201 Created`     |
| GET    | `/waitinglist`    | List all waiting list entries      | —                                                 | `200 OK`          |

> **Note:** `/users` endpoints were removed in [API Refactor (Plan 06)](../06-api-refactor/plan.md). Users are created exclusively via `POST /waitinglist`.

### Error Response Format

All errors return a consistent JSON body:

```json
{
    "error": "human-readable error message"
}
```

### Status Code Conventions

| Status | Usage                                      |
|--------|--------------------------------------------|
| 200    | Successful retrieval                       |
| 201    | Successful creation                        |
| 400    | Invalid request (bad JSON, missing fields) |
| 404    | Resource not found                         |
| 405    | Method not allowed                         |
| 409    | Conflict (duplicate email / already on list)|
| 500    | Internal server error                      |

### Middleware

Since we use only the standard library, middleware is implemented as handler-wrapping functions:

```go
func loggingMiddleware(next http.Handler) http.Handler
func jsonContentType(next http.Handler) http.Handler
```

- **`loggingMiddleware`**: Logs method, path, status code, and duration for every request.
- **`jsonContentType`**: Sets `Content-Type: application/json` on all responses.

### Route Registration

```go
mux := http.NewServeMux()

mux.HandleFunc("/users", userHandler.ServeHTTP)
mux.HandleFunc("/waitinglist", waitingListHandler.ServeHTTP)
```

Each handler inspects `r.Method` to dispatch to the correct action (e.g., `GET` vs `POST`), returning `405 Method Not Allowed` for unsupported methods.

## Implementation Steps

- [x] Implement error response helper in `internal/handler/` (shared utility)
- [x] Implement `loggingMiddleware` and `jsonContentType` middleware
- [x] Register all routes in `cmd/server/main.go`
- [x] Implement method dispatching in each handler (`GET`/`POST` switch)
- [x] Add `405 Method Not Allowed` responses for unsupported methods
- [x] Test endpoints manually with `curl` or write integration tests

## Testing

### Unit Tests — Middleware

- **Core logic**:
  - Test `jsonContentType` sets `Content-Type: application/json` on the response.
  - Test `loggingMiddleware` logs the method, path, status code, and duration (capture log output).
- **Edge cases**:
  - Test that middleware correctly chains and calls the next handler.
  - Test `loggingMiddleware` with slow handlers — duration should reflect actual time.
- **Error/negative scenarios**:
  - Test that `jsonContentType` is set even when the handler returns an error status code.

### Unit Tests — Error Response Helper

- **Core logic**:
  - Test that the error helper returns the expected JSON format `{"error": "message"}`.
  - Test that the error helper sets the correct HTTP status code.
- **Edge cases**:
  - Test error messages with special characters (quotes, newlines) are properly JSON-encoded.

### Unit Tests — Route Registration & Method Dispatching

- **Core logic**:
  - Test that `POST` requests to `/waitinglist` reach the correct handler logic.
  - Test that `GET` requests to `/waitinglist` reach the correct handler logic.
- **Error/negative scenarios**:
  - Test that `DELETE`, `PUT`, `PATCH` on `/waitinglist` return `405 Method Not Allowed`.
  - Test that requests to undefined routes (including `/users`) return `404 Not Found`.

## Acceptance Criteria

- All endpoints respond with correct status codes and JSON bodies
- Unsupported HTTP methods return `405`
- Every request is logged with method, path, status, and duration
- All responses have `Content-Type: application/json`
- No third-party HTTP libraries are used

## Dependencies

- [Project Setup](../01-project-setup/plan.md) — server entry point and mux setup
- [Waiting List](../04-waiting-list/plan.md) — waiting list handlers
- [API Refactor](../06-api-refactor/plan.md) — removal of `/users` endpoints
