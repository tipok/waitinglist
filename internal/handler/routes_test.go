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
	wlHandler.RegisterRoutes(mux)
	return mux
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

func TestRoutes_Users_Returns404(t *testing.T) {
	mux := newFullMux()

	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"firstname":"John","lastname":"Doe","email":"john@example.com"}`))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for removed /users route, got %d", w.Code)
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
