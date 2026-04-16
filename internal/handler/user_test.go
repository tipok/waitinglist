package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tipok/waitinglist/internal/model"
)

// mockUserStore is a test double for UserStore.
type mockUserStore struct {
	createFn     func(ctx context.Context, user *model.UserEntity) error
	getByEmailFn func(ctx context.Context, email string) (*model.UserEntity, error)
}

func (m *mockUserStore) Create(ctx context.Context, user *model.UserEntity) error {
	return m.createFn(ctx, user)
}

func (m *mockUserStore) GetByEmail(ctx context.Context, email string) (*model.UserEntity, error) {
	return m.getByEmailFn(ctx, email)
}

func newTestHandler(store UserStore) (*UserHandler, *http.ServeMux) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewUserHandler(store, logger)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return h, mux
}

func TestHandleCreate_ValidUser(t *testing.T) {
	store := &mockUserStore{
		createFn: func(_ context.Context, user *model.UserEntity) error {
			user.ID = "test-uuid"
			user.HasAccess = false
			return nil
		},
	}
	_, mux := newTestHandler(store)

	body := `{"firstname":"John","lastname":"Doe","email":"john@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", w.Code)
	}

	var resp model.UserEntity
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.ID != "test-uuid" {
		t.Errorf("expected id test-uuid, got %s", resp.ID)
	}
	if resp.Firstname != "John" {
		t.Errorf("expected firstname John, got %s", resp.Firstname)
	}
	if resp.Email != "john@example.com" {
		t.Errorf("expected email john@example.com, got %s", resp.Email)
	}
}

func TestHandleCreate_ExtraFieldsIgnored(t *testing.T) {
	store := &mockUserStore{
		createFn: func(_ context.Context, user *model.UserEntity) error {
			user.ID = "uuid-1"
			return nil
		},
	}
	_, mux := newTestHandler(store)

	body := `{"firstname":"Jane","lastname":"Doe","email":"jane@example.com","unknown_field":"ignored"}`
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(body))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", w.Code)
	}
}

func TestHandleCreate_MissingFirstname(t *testing.T) {
	_, mux := newTestHandler(&mockUserStore{})

	body := `{"lastname":"Doe","email":"test@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(body))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestHandleCreate_MissingLastname(t *testing.T) {
	_, mux := newTestHandler(&mockUserStore{})

	body := `{"firstname":"John","email":"test@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(body))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestHandleCreate_MissingEmail(t *testing.T) {
	_, mux := newTestHandler(&mockUserStore{})

	body := `{"firstname":"John","lastname":"Doe"}`
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(body))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestHandleCreate_InvalidEmail(t *testing.T) {
	_, mux := newTestHandler(&mockUserStore{})

	body := `{"firstname":"John","lastname":"Doe","email":"not-an-email"}`
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(body))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestHandleCreate_EmptyBody(t *testing.T) {
	_, mux := newTestHandler(&mockUserStore{})

	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(""))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestHandleCreate_DuplicateEmail(t *testing.T) {
	store := &mockUserStore{
		createFn: func(_ context.Context, _ *model.UserEntity) error {
			return model.ErrDuplicateEmail
		},
	}
	_, mux := newTestHandler(store)

	body := `{"firstname":"John","lastname":"Doe","email":"dup@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(body))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", w.Code)
	}
}

func TestHandleCreate_InternalError(t *testing.T) {
	store := &mockUserStore{
		createFn: func(_ context.Context, _ *model.UserEntity) error {
			return fmt.Errorf("db connection lost")
		},
	}
	_, mux := newTestHandler(store)

	body := `{"firstname":"John","lastname":"Doe","email":"test@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(body))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", w.Code)
	}
}

func TestHandleGetByEmail_Found(t *testing.T) {
	store := &mockUserStore{
		getByEmailFn: func(_ context.Context, email string) (*model.UserEntity, error) {
			return &model.UserEntity{
				ID:        "uuid-1",
				Firstname: "John",
				Lastname:  "Doe",
				Email:     email,
				HasAccess: false,
			}, nil
		},
	}
	_, mux := newTestHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/users?email=john@example.com", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp model.UserEntity
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Email != "john@example.com" {
		t.Errorf("expected email john@example.com, got %s", resp.Email)
	}
}

func TestHandleGetByEmail_MissingParam(t *testing.T) {
	_, mux := newTestHandler(&mockUserStore{})

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestHandleGetByEmail_NotFound(t *testing.T) {
	store := &mockUserStore{
		getByEmailFn: func(_ context.Context, _ string) (*model.UserEntity, error) {
			return nil, model.ErrUserNotFound
		},
	}
	_, mux := newTestHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/users?email=notfound@example.com", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", w.Code)
	}
}

func TestHandleGetByEmail_InternalError(t *testing.T) {
	store := &mockUserStore{
		getByEmailFn: func(_ context.Context, _ string) (*model.UserEntity, error) {
			return nil, fmt.Errorf("db error")
		},
	}
	_, mux := newTestHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/users?email=test@example.com", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", w.Code)
	}
}

func TestUnsupportedMethod_Delete(t *testing.T) {
	_, mux := newTestHandler(&mockUserStore{})

	req := httptest.NewRequest(http.MethodDelete, "/users", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", w.Code)
	}
}

func TestUnsupportedMethod_Put(t *testing.T) {
	_, mux := newTestHandler(&mockUserStore{})

	req := httptest.NewRequest(http.MethodPut, "/users", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", w.Code)
	}
}

func TestHandleCreate_InvalidJSON(t *testing.T) {
	_, mux := newTestHandler(&mockUserStore{})

	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader("{invalid"))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestHandleCreate_WhitespaceOnlyFields(t *testing.T) {
	_, mux := newTestHandler(&mockUserStore{})

	body := `{"firstname":"  ","lastname":"Doe","email":"test@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(body))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}
