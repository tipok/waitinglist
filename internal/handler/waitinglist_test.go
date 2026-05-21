package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tipok/waitinglist/internal/model"
)

// fakeTx implements model.Tx for testing without a real database.
type fakeTx struct {
	committed  bool
	rolledBack bool
}

func (f *fakeTx) QueryRowContext(_ context.Context, _ string, _ ...any) *sql.Row {
	return nil
}

func (f *fakeTx) QueryContext(_ context.Context, _ string, _ ...any) (*sql.Rows, error) {
	return nil, nil
}

func (f *fakeTx) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	return nil, nil
}

func (f *fakeTx) Commit() error {
	f.committed = true
	return nil
}

func (f *fakeTx) Rollback() error {
	f.rolledBack = true
	return nil
}

// mockWaitingListUserStore is a test double for WaitingListUserStore.
type mockWaitingListUserStore struct {
	createTxFn            func(ctx context.Context, tx model.DBTX, user *model.UserEntity) error
	getByEmailTxFn        func(ctx context.Context, tx model.DBTX, projectID, email string) (*model.UserEntity, error)
	getUserInfoByEmailsFn func(ctx context.Context, projectID string, emails []string) ([]model.UserInfo, error)
}

func (m *mockWaitingListUserStore) CreateTx(ctx context.Context, tx model.DBTX, user *model.UserEntity) error {
	return m.createTxFn(ctx, tx, user)
}

func (m *mockWaitingListUserStore) GetByEmailTx(ctx context.Context, tx model.DBTX, projectID, email string) (*model.UserEntity, error) {
	return m.getByEmailTxFn(ctx, tx, projectID, email)
}

func (m *mockWaitingListUserStore) GetUserInfoByEmails(ctx context.Context, projectID string, emails []string) ([]model.UserInfo, error) {
	if m.getUserInfoByEmailsFn != nil {
		return m.getUserInfoByEmailsFn(ctx, projectID, emails)
	}
	return []model.UserInfo{}, nil
}

// mockWaitingListStore is a test double for WaitingListStore.
type mockWaitingListStore struct {
	addFn     func(ctx context.Context, tx model.DBTX, projectID, userID string) (*model.WaitingListEntry, error)
	getAllFn  func(ctx context.Context, projectID string) ([]model.WaitingListEntry, error)
	beginTxFn func(ctx context.Context) (model.Tx, error)
}

func (m *mockWaitingListStore) Add(ctx context.Context, tx model.DBTX, projectID, userID string) (*model.WaitingListEntry, error) {
	return m.addFn(ctx, tx, projectID, userID)
}

func (m *mockWaitingListStore) GetAll(ctx context.Context, projectID string) ([]model.WaitingListEntry, error) {
	return m.getAllFn(ctx, projectID)
}

func (m *mockWaitingListStore) BeginTx(ctx context.Context) (model.Tx, error) {
	return m.beginTxFn(ctx)
}

var testProject = &model.Project{ID: "test-project-id", Slug: "test", Name: "Test"}

func newWaitingListTestHandler(userStore WaitingListUserStore, wlStore WaitingListStore) *http.ServeMux {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewWaitingListHandler(userStore, wlStore, logger)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return mux
}

func withProjectCtx(r *http.Request) *http.Request {
	ctx := context.WithValue(r.Context(), ctxKeyProject, testProject)
	return r.WithContext(ctx)
}

func TestWaitingList_AddNewUser(t *testing.T) {
	userStore := &mockWaitingListUserStore{
		getByEmailTxFn: func(_ context.Context, _ model.DBTX, _, _ string) (*model.UserEntity, error) {
			return nil, model.ErrUserNotFound
		},
		createTxFn: func(_ context.Context, _ model.DBTX, user *model.UserEntity) error {
			user.ID = "user-uuid-1"
			user.HasAccess = false
			return nil
		},
	}
	wlStore := &mockWaitingListStore{
		beginTxFn: func(_ context.Context) (model.Tx, error) {
			return &fakeTx{}, nil
		},
		addFn: func(_ context.Context, _ model.DBTX, _, userID string) (*model.WaitingListEntry, error) {
			return &model.WaitingListEntry{
				ID:        "wl-uuid-1",
				UserID:    userID,
				CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			}, nil
		},
	}

	mux := newWaitingListTestHandler(userStore, wlStore)

	body := `{"firstname":"John","lastname":"Doe","email":"john@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/waitinglist", strings.NewReader(body))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, withProjectCtx(req))

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp addToWaitingListResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.User.ID != "user-uuid-1" {
		t.Errorf("expected user id user-uuid-1, got %s", resp.User.ID)
	}
	if resp.WaitingListEntry.ID != "wl-uuid-1" {
		t.Errorf("expected wl entry id wl-uuid-1, got %s", resp.WaitingListEntry.ID)
	}
}

func TestWaitingList_AddExistingUser(t *testing.T) {
	userStore := &mockWaitingListUserStore{
		getByEmailTxFn: func(_ context.Context, _ model.DBTX, _, _ string) (*model.UserEntity, error) {
			return &model.UserEntity{
				ID:        "existing-uuid",
				Firstname: "Jane",
				Lastname:  "Doe",
				Email:     "jane@example.com",
				HasAccess: false,
			}, nil
		},
	}
	wlStore := &mockWaitingListStore{
		beginTxFn: func(_ context.Context) (model.Tx, error) {
			return &fakeTx{}, nil
		},
		addFn: func(_ context.Context, _ model.DBTX, _, userID string) (*model.WaitingListEntry, error) {
			return &model.WaitingListEntry{
				ID:        "wl-uuid-2",
				UserID:    userID,
				CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
			}, nil
		},
	}

	mux := newWaitingListTestHandler(userStore, wlStore)

	body := `{"firstname":"Jane","lastname":"Doe","email":"jane@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/waitinglist", strings.NewReader(body))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, withProjectCtx(req))

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp addToWaitingListResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.User.ID != "existing-uuid" {
		t.Errorf("expected user id existing-uuid, got %s", resp.User.ID)
	}
}

func TestWaitingList_GetAll_Entries(t *testing.T) {
	wlStore := &mockWaitingListStore{
		getAllFn: func(_ context.Context, _ string) ([]model.WaitingListEntry, error) {
			return []model.WaitingListEntry{
				{ID: "wl-1", UserID: "u-1", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
				{ID: "wl-2", UserID: "u-2", CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
			}, nil
		},
	}

	mux := newWaitingListTestHandler(&mockWaitingListUserStore{}, wlStore)

	req := httptest.NewRequest(http.MethodGet, "/waitinglist", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, withProjectCtx(req))

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var entries []model.WaitingListEntry
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].ID != "wl-1" {
		t.Errorf("expected first entry id wl-1, got %s", entries[0].ID)
	}
}

func TestWaitingList_GetAll_Empty(t *testing.T) {
	wlStore := &mockWaitingListStore{
		getAllFn: func(_ context.Context, _ string) ([]model.WaitingListEntry, error) {
			return []model.WaitingListEntry{}, nil
		},
	}

	mux := newWaitingListTestHandler(&mockWaitingListUserStore{}, wlStore)

	req := httptest.NewRequest(http.MethodGet, "/waitinglist", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, withProjectCtx(req))

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var entries []model.WaitingListEntry
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty array, got %d entries", len(entries))
	}
}

func TestWaitingList_Add_AlreadyOnList(t *testing.T) {
	userStore := &mockWaitingListUserStore{
		getByEmailTxFn: func(_ context.Context, _ model.DBTX, _, _ string) (*model.UserEntity, error) {
			return &model.UserEntity{ID: "existing-uuid", Email: "dup@example.com"}, nil
		},
	}
	wlStore := &mockWaitingListStore{
		beginTxFn: func(_ context.Context) (model.Tx, error) {
			return &fakeTx{}, nil
		},
		addFn: func(_ context.Context, _ model.DBTX, _, _ string) (*model.WaitingListEntry, error) {
			return nil, model.ErrAlreadyOnWaitingList
		},
	}

	mux := newWaitingListTestHandler(userStore, wlStore)

	body := `{"firstname":"John","lastname":"Doe","email":"dup@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/waitinglist", strings.NewReader(body))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, withProjectCtx(req))

	if w.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWaitingList_Add_MissingFirstname(t *testing.T) {
	mux := newWaitingListTestHandler(&mockWaitingListUserStore{}, &mockWaitingListStore{})

	body := `{"lastname":"Doe","email":"test@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/waitinglist", strings.NewReader(body))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, withProjectCtx(req))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestWaitingList_Add_MissingLastname(t *testing.T) {
	mux := newWaitingListTestHandler(&mockWaitingListUserStore{}, &mockWaitingListStore{})

	body := `{"firstname":"John","email":"test@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/waitinglist", strings.NewReader(body))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, withProjectCtx(req))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestWaitingList_Add_MissingEmail(t *testing.T) {
	mux := newWaitingListTestHandler(&mockWaitingListUserStore{}, &mockWaitingListStore{})

	body := `{"firstname":"John","lastname":"Doe"}`
	req := httptest.NewRequest(http.MethodPost, "/waitinglist", strings.NewReader(body))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, withProjectCtx(req))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestWaitingList_Add_InvalidEmail(t *testing.T) {
	mux := newWaitingListTestHandler(&mockWaitingListUserStore{}, &mockWaitingListStore{})

	body := `{"firstname":"John","lastname":"Doe","email":"not-an-email"}`
	req := httptest.NewRequest(http.MethodPost, "/waitinglist", strings.NewReader(body))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, withProjectCtx(req))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestWaitingList_Add_EmptyBody(t *testing.T) {
	mux := newWaitingListTestHandler(&mockWaitingListUserStore{}, &mockWaitingListStore{})

	req := httptest.NewRequest(http.MethodPost, "/waitinglist", strings.NewReader(""))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, withProjectCtx(req))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestWaitingList_Add_ExtraFieldsIgnored(t *testing.T) {
	userStore := &mockWaitingListUserStore{
		getByEmailTxFn: func(_ context.Context, _ model.DBTX, _, _ string) (*model.UserEntity, error) {
			return nil, model.ErrUserNotFound
		},
		createTxFn: func(_ context.Context, _ model.DBTX, user *model.UserEntity) error {
			user.ID = "uuid-extra"
			return nil
		},
	}
	wlStore := &mockWaitingListStore{
		beginTxFn: func(_ context.Context) (model.Tx, error) {
			return &fakeTx{}, nil
		},
		addFn: func(_ context.Context, _ model.DBTX, _, userID string) (*model.WaitingListEntry, error) {
			return &model.WaitingListEntry{
				ID:        "wl-extra",
				UserID:    userID,
				CreatedAt: time.Now(),
			}, nil
		},
	}

	mux := newWaitingListTestHandler(userStore, wlStore)

	body := `{"firstname":"John","lastname":"Doe","email":"extra@example.com","unknown":"ignored"}`
	req := httptest.NewRequest(http.MethodPost, "/waitinglist", strings.NewReader(body))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, withProjectCtx(req))

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", w.Code)
	}
}

func TestWaitingList_Add_TransactionRollbackOnInsertFailure(t *testing.T) {
	userCreated := false
	tx := &fakeTx{}
	userStore := &mockWaitingListUserStore{
		getByEmailTxFn: func(_ context.Context, _ model.DBTX, _, _ string) (*model.UserEntity, error) {
			return nil, model.ErrUserNotFound
		},
		createTxFn: func(_ context.Context, _ model.DBTX, user *model.UserEntity) error {
			userCreated = true
			user.ID = "new-user-uuid"
			return nil
		},
	}
	wlStore := &mockWaitingListStore{
		beginTxFn: func(_ context.Context) (model.Tx, error) {
			return tx, nil
		},
		addFn: func(_ context.Context, _ model.DBTX, _, _ string) (*model.WaitingListEntry, error) {
			return nil, fmt.Errorf("database error")
		},
	}

	mux := newWaitingListTestHandler(userStore, wlStore)

	body := `{"firstname":"John","lastname":"Doe","email":"rollback@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/waitinglist", strings.NewReader(body))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, withProjectCtx(req))

	if !userCreated {
		t.Error("expected user creation to be attempted")
	}
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", w.Code)
	}
	if !tx.rolledBack {
		t.Error("expected transaction to be rolled back")
	}
	if tx.committed {
		t.Error("expected transaction not to be committed")
	}
}

func TestWaitingList_UnsupportedMethod_Delete(t *testing.T) {
	mux := newWaitingListTestHandler(&mockWaitingListUserStore{}, &mockWaitingListStore{})

	req := httptest.NewRequest(http.MethodDelete, "/waitinglist", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, withProjectCtx(req))

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", w.Code)
	}
}

func TestWaitingList_UnsupportedMethod_Put(t *testing.T) {
	mux := newWaitingListTestHandler(&mockWaitingListUserStore{}, &mockWaitingListStore{})

	req := httptest.NewRequest(http.MethodPut, "/waitinglist", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, withProjectCtx(req))

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", w.Code)
	}
}

func TestWaitingList_GetAll_InternalError(t *testing.T) {
	wlStore := &mockWaitingListStore{
		getAllFn: func(_ context.Context, _ string) ([]model.WaitingListEntry, error) {
			return nil, fmt.Errorf("db error")
		},
	}

	mux := newWaitingListTestHandler(&mockWaitingListUserStore{}, wlStore)

	req := httptest.NewRequest(http.MethodGet, "/waitinglist", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, withProjectCtx(req))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", w.Code)
	}
}

func TestWaitingList_GetUsersByEmail_Success(t *testing.T) {
	userStore := &mockWaitingListUserStore{
		getUserInfoByEmailsFn: func(_ context.Context, _ string, emails []string) ([]model.UserInfo, error) {
			return []model.UserInfo{
				{
					Firstname: "Jane",
					Lastname:  "Doe",
					Email:     "jane@example.com",
					HasAccess: false,
					CreatedAt: time.Date(2026, 4, 10, 14, 30, 0, 0, time.UTC),
				},
			}, nil
		},
	}

	mux := newWaitingListTestHandler(userStore, &mockWaitingListStore{})

	req := httptest.NewRequest(http.MethodGet, "/waitinglist/users?email=jane@example.com", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, withProjectCtx(req))

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp model.UserInfoList
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(resp.Users))
	}
	if resp.Users[0].Email != "jane@example.com" {
		t.Errorf("expected email jane@example.com, got %s", resp.Users[0].Email)
	}
	if resp.Users[0].Firstname != "Jane" {
		t.Errorf("expected firstname Jane, got %s", resp.Users[0].Firstname)
	}
}

func TestWaitingList_GetUsersByEmail_MultipleEmails(t *testing.T) {
	userStore := &mockWaitingListUserStore{
		getUserInfoByEmailsFn: func(_ context.Context, _ string, emails []string) ([]model.UserInfo, error) {
			return []model.UserInfo{
				{Firstname: "Jane", Lastname: "Doe", Email: "jane@example.com", HasAccess: false, CreatedAt: time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)},
				{Firstname: "John", Lastname: "Smith", Email: "john@example.com", HasAccess: true, CreatedAt: time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)},
			}, nil
		},
	}

	mux := newWaitingListTestHandler(userStore, &mockWaitingListStore{})

	req := httptest.NewRequest(http.MethodGet, "/waitinglist/users?email=jane@example.com&email=john@example.com", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, withProjectCtx(req))

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp model.UserInfoList
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(resp.Users))
	}
}

func TestWaitingList_GetUsersByEmail_TrimWhitespace(t *testing.T) {
	var capturedEmails []string
	userStore := &mockWaitingListUserStore{
		getUserInfoByEmailsFn: func(_ context.Context, _ string, emails []string) ([]model.UserInfo, error) {
			capturedEmails = emails
			return []model.UserInfo{}, nil
		},
	}

	mux := newWaitingListTestHandler(userStore, &mockWaitingListStore{})

	req := httptest.NewRequest(http.MethodGet, "/waitinglist/users?email=+jane@example.com+", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, withProjectCtx(req))

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(capturedEmails) != 1 || capturedEmails[0] != "jane@example.com" {
		t.Errorf("expected trimmed email, got %v", capturedEmails)
	}
}

func TestWaitingList_GetUsersByEmail_NoMatchReturnsEmptyList(t *testing.T) {
	userStore := &mockWaitingListUserStore{
		getUserInfoByEmailsFn: func(_ context.Context, _ string, _ []string) ([]model.UserInfo, error) {
			return []model.UserInfo{}, nil
		},
	}

	mux := newWaitingListTestHandler(userStore, &mockWaitingListStore{})

	req := httptest.NewRequest(http.MethodGet, "/waitinglist/users?email=unknown@example.com", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, withProjectCtx(req))

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp model.UserInfoList
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Users) != 0 {
		t.Errorf("expected empty users list, got %d", len(resp.Users))
	}
}

func TestWaitingList_GetUsersByEmail_MissingParam(t *testing.T) {
	mux := newWaitingListTestHandler(&mockWaitingListUserStore{}, &mockWaitingListStore{})

	req := httptest.NewRequest(http.MethodGet, "/waitinglist/users", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, withProjectCtx(req))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWaitingList_GetUsersByEmail_InvalidEmail(t *testing.T) {
	mux := newWaitingListTestHandler(&mockWaitingListUserStore{}, &mockWaitingListStore{})

	req := httptest.NewRequest(http.MethodGet, "/waitinglist/users?email=invalid", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, withProjectCtx(req))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWaitingList_GetUsersByEmail_InternalError(t *testing.T) {
	userStore := &mockWaitingListUserStore{
		getUserInfoByEmailsFn: func(_ context.Context, _ string, _ []string) ([]model.UserInfo, error) {
			return nil, fmt.Errorf("database connection lost")
		},
	}

	mux := newWaitingListTestHandler(userStore, &mockWaitingListStore{})

	req := httptest.NewRequest(http.MethodGet, "/waitinglist/users?email=jane@example.com", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, withProjectCtx(req))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWaitingList_Add_IPSetOnNewUser(t *testing.T) {
	var capturedIP *string
	userStore := &mockWaitingListUserStore{
		getByEmailTxFn: func(_ context.Context, _ model.DBTX, _, _ string) (*model.UserEntity, error) {
			return nil, model.ErrUserNotFound
		},
		createTxFn: func(_ context.Context, _ model.DBTX, user *model.UserEntity) error {
			capturedIP = user.IPAddress
			user.ID = "new-uuid"
			return nil
		},
	}
	wlStore := &mockWaitingListStore{
		beginTxFn: func(_ context.Context) (model.Tx, error) {
			return &fakeTx{}, nil
		},
		addFn: func(_ context.Context, _ model.DBTX, _, userID string) (*model.WaitingListEntry, error) {
			return &model.WaitingListEntry{ID: "wl-1", UserID: userID, CreatedAt: time.Now()}, nil
		},
	}

	mux := newWaitingListTestHandler(userStore, wlStore)

	body := `{"firstname":"John","lastname":"Doe","email":"ip@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/waitinglist", strings.NewReader(body))
	req.Header.Set("X-Forwarded-For", "203.0.113.50, 70.41.3.18")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, withProjectCtx(req))

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}
	if capturedIP == nil {
		t.Fatal("expected IPAddress to be set, got nil")
	}
	if *capturedIP != "203.0.113.50" {
		t.Errorf("expected IP 203.0.113.50, got %s", *capturedIP)
	}
}

func TestWaitingList_Add_IPNotSetOnExistingUser(t *testing.T) {
	userStore := &mockWaitingListUserStore{
		getByEmailTxFn: func(_ context.Context, _ model.DBTX, _, _ string) (*model.UserEntity, error) {
			return &model.UserEntity{
				ID:        "existing-uuid",
				Firstname: "Jane",
				Lastname:  "Doe",
				Email:     "jane@example.com",
				HasAccess: false,
				IPAddress: nil,
			}, nil
		},
	}
	wlStore := &mockWaitingListStore{
		beginTxFn: func(_ context.Context) (model.Tx, error) {
			return &fakeTx{}, nil
		},
		addFn: func(_ context.Context, _ model.DBTX, _, userID string) (*model.WaitingListEntry, error) {
			return &model.WaitingListEntry{ID: "wl-2", UserID: userID, CreatedAt: time.Now()}, nil
		},
	}

	mux := newWaitingListTestHandler(userStore, wlStore)

	body := `{"firstname":"Jane","lastname":"Doe","email":"jane@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/waitinglist", strings.NewReader(body))
	req.Header.Set("X-Forwarded-For", "203.0.113.99")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, withProjectCtx(req))

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp addToWaitingListResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.User.IPAddress != nil {
		t.Errorf("expected nil IPAddress for existing user, got %v", *resp.User.IPAddress)
	}
}

func TestWaitingList_Add_AlreadyHasAccessReturns205(t *testing.T) {
	addCalled := false
	tx := &fakeTx{}
	userStore := &mockWaitingListUserStore{
		getByEmailTxFn: func(_ context.Context, _ model.DBTX, _, _ string) (*model.UserEntity, error) {
			return &model.UserEntity{
				ID:        "granted-uuid",
				Firstname: "Jane",
				Lastname:  "Doe",
				Email:     "granted@example.com",
				HasAccess: true,
			}, nil
		},
		createTxFn: func(_ context.Context, _ model.DBTX, _ *model.UserEntity) error {
			t.Fatal("CreateTx must not be called when user already has access")
			return nil
		},
	}
	wlStore := &mockWaitingListStore{
		beginTxFn: func(_ context.Context) (model.Tx, error) {
			return tx, nil
		},
		addFn: func(_ context.Context, _ model.DBTX, _, _ string) (*model.WaitingListEntry, error) {
			addCalled = true
			return nil, nil
		},
	}

	mux := newWaitingListTestHandler(userStore, wlStore)

	body := `{"firstname":"Jane","lastname":"Doe","email":"granted@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/waitinglist", strings.NewReader(body))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, withProjectCtx(req))

	if w.Code != http.StatusResetContent {
		t.Fatalf("expected status 205, got %d: %s", w.Code, w.Body.String())
	}
	if addCalled {
		t.Error("waiting list Add must not be called when user already has access")
	}
	if tx.committed {
		t.Error("transaction must not be committed on the 205 short-circuit path")
	}
	if !tx.rolledBack {
		t.Error("transaction must be rolled back via defer on the 205 short-circuit path")
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["message"] != model.ErrAlreadyHasAccess.Error() {
		t.Errorf("expected message %q, got %q", model.ErrAlreadyHasAccess.Error(), resp["message"])
	}
}

func TestWaitingList_Add_BeginTxError(t *testing.T) {
	wlStore := &mockWaitingListStore{
		beginTxFn: func(_ context.Context) (model.Tx, error) {
			return nil, fmt.Errorf("connection lost")
		},
	}

	mux := newWaitingListTestHandler(&mockWaitingListUserStore{}, wlStore)

	body := `{"firstname":"John","lastname":"Doe","email":"test@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/waitinglist", strings.NewReader(body))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, withProjectCtx(req))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", w.Code)
	}
}
