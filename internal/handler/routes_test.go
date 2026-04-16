package handler

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tipok/waitinglist/internal/model"
)

func newFullMux() *http.ServeMux {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	userStore := &mockUserStore{
		createFn: func(_ context.Context, user *model.UserEntity) error {
			user.ID = "uuid-1"
			return nil
		},
		getByEmailFn: func(_ context.Context, email string) (*model.UserEntity, error) {
			return &model.UserEntity{ID: "uuid-1", Firstname: "John", Lastname: "Doe", Email: email}, nil
		},
	}
	userHandler := NewUserHandler(userStore, logger)

	wlUserStore := &mockWaitingListUserStore{
		getByEmailTxFn: func(_ context.Context, _ model.DBTX, _ string) (*model.UserEntity, error) {
			return nil, model.ErrUserNotFound
		},
		createTxFn: func(_ context.Context, _ model.DBTX, user *model.UserEntity) error {
			user.ID = "uuid-1"
			return nil
		},
	}
	wlStore := &mockWaitingListStore{
		beginTxFn: func(_ context.Context) (model.Tx, error) {
			return &fakeTx{}, nil
		},
		addFn: func(_ context.Context, _ model.DBTX, userID string) (*model.WaitingListEntry, error) {
			return &model.WaitingListEntry{ID: "wl-1", UserID: userID}, nil
		},
		getAllFn: func(_ context.Context) ([]model.WaitingListEntry, error) {
			return []model.WaitingListEntry{}, nil
		},
	}
	wlHandler := NewWaitingListHandler(wlUserStore, wlStore, logger)

	mux := http.NewServeMux()
	userHandler.RegisterRoutes(mux)
	wlHandler.RegisterRoutes(mux)
	return mux
}

func TestRoutes_POST_Users_ReachesHandler(t *testing.T) {
	mux := newFullMux()

	body := `{"firstname":"John","lastname":"Doe","email":"john@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(body))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRoutes_GET_Users_ReachesHandler(t *testing.T) {
	mux := newFullMux()

	req := httptest.NewRequest(http.MethodGet, "/users?email=john@example.com", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRoutes_POST_WaitingList_ReachesHandler(t *testing.T) {
	mux := newFullMux()

	body := `{"firstname":"John","lastname":"Doe","email":"john@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/waitinglist", strings.NewReader(body))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRoutes_GET_WaitingList_ReachesHandler(t *testing.T) {
	mux := newFullMux()

	req := httptest.NewRequest(http.MethodGet, "/waitinglist", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRoutes_UndefinedRoute_Returns404(t *testing.T) {
	mux := newFullMux()

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", w.Code)
	}
}

func TestRoutes_PATCH_Users_Returns405(t *testing.T) {
	mux := newFullMux()

	req := httptest.NewRequest(http.MethodPatch, "/users", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", w.Code)
	}
}

func TestRoutes_PATCH_WaitingList_Returns405(t *testing.T) {
	mux := newFullMux()

	req := httptest.NewRequest(http.MethodPatch, "/waitinglist", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", w.Code)
	}
}
