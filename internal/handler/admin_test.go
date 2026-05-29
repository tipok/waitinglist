package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tipok/waitinglist/internal/model"
	"github.com/tipok/waitinglist/internal/notifier"
)

type fakeAdminUserStore struct {
	countByAccessFn     func(ctx context.Context, projectID string) (int, int, error)
	enlistmentsByDayFn  func(ctx context.Context, projectID string, days int) ([]model.DayCount, error)
	listWithAccessFn    func(ctx context.Context, projectID, emailLike string, limit, offset int) ([]model.UserEntity, error)
	listAllWithAccessFn func(ctx context.Context, projectSlug string) ([]model.UserEntity, error)
	getByIDFn           func(ctx context.Context, id string) (*model.UserEntity, error)
	grantAccessTxFn     func(ctx context.Context, tx model.DBTX, ids []string, source string) error
	revokeAccessFn      func(ctx context.Context, id, reason, by string) error
}

func (f *fakeAdminUserStore) CountByAccess(ctx context.Context, projectID string) (int, int, error) {
	return f.countByAccessFn(ctx, projectID)
}
func (f *fakeAdminUserStore) EnlistmentsByDay(ctx context.Context, projectID string, days int) ([]model.DayCount, error) {
	return f.enlistmentsByDayFn(ctx, projectID, days)
}
func (f *fakeAdminUserStore) ListWithAccess(ctx context.Context, projectID, emailLike string, limit, offset int) ([]model.UserEntity, error) {
	return f.listWithAccessFn(ctx, projectID, emailLike, limit, offset)
}
func (f *fakeAdminUserStore) ListAllWithAccess(ctx context.Context, projectSlug string) ([]model.UserEntity, error) {
	if f.listAllWithAccessFn != nil {
		return f.listAllWithAccessFn(ctx, projectSlug)
	}
	return nil, nil
}
func (f *fakeAdminUserStore) GetByID(ctx context.Context, id string) (*model.UserEntity, error) {
	return f.getByIDFn(ctx, id)
}
func (f *fakeAdminUserStore) GrantAccessTx(ctx context.Context, tx model.DBTX, ids []string, source string) error {
	return f.grantAccessTxFn(ctx, tx, ids, source)
}
func (f *fakeAdminUserStore) RevokeAccess(ctx context.Context, id, reason, by string) error {
	return f.revokeAccessFn(ctx, id, reason, by)
}

type fakeAdminWaitlistStore struct {
	listJoinedFn       func(ctx context.Context, projectID, emailLike string, limit, offset int) ([]model.WaitingListAdminRow, error)
	listAllJoinedFn    func(ctx context.Context, projectSlug string) ([]model.WaitingListAdminRow, error)
	deleteByIDFn       func(ctx context.Context, id string) error
	deleteByUserIDTxFn func(ctx context.Context, tx model.DBTX, userID string) error
	beginTxFn          func(ctx context.Context) (model.Tx, error)
}

func (f *fakeAdminWaitlistStore) ListJoined(ctx context.Context, projectID, emailLike string, limit, offset int) ([]model.WaitingListAdminRow, error) {
	return f.listJoinedFn(ctx, projectID, emailLike, limit, offset)
}
func (f *fakeAdminWaitlistStore) ListAllJoined(ctx context.Context, projectSlug string) ([]model.WaitingListAdminRow, error) {
	if f.listAllJoinedFn != nil {
		return f.listAllJoinedFn(ctx, projectSlug)
	}
	return nil, nil
}
func (f *fakeAdminWaitlistStore) DeleteByID(ctx context.Context, id string) error {
	return f.deleteByIDFn(ctx, id)
}
func (f *fakeAdminWaitlistStore) DeleteByUserIDTx(ctx context.Context, tx model.DBTX, userID string) error {
	return f.deleteByUserIDTxFn(ctx, tx, userID)
}
func (f *fakeAdminWaitlistStore) BeginTx(ctx context.Context) (model.Tx, error) {
	return f.beginTxFn(ctx)
}

func newTestAdminHandler(us AdminUserStore, ws AdminWaitingListStore) (*AdminHandler, *http.ServeMux) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	projects := []model.Project{{Slug: "default", Name: "Default"}}
	h := NewAdminHandler(us, ws, projects, logger, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return h, mux
}

func TestAdmin_Dashboard_HappyPath(t *testing.T) {
	us := &fakeAdminUserStore{
		countByAccessFn: func(_ context.Context, _ string) (int, int, error) { return 7, 3, nil },
		enlistmentsByDayFn: func(_ context.Context, _ string, days int) ([]model.DayCount, error) {
			if days != defaultDashboardDays {
				t.Fatalf("expected default days, got %d", days)
			}
			return []model.DayCount{{Day: "2026-05-01", Count: 1}}, nil
		},
	}
	_, mux := newTestAdminHandler(us, &fakeAdminWaitlistStore{})

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var got dashboardResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.WaitingList != 7 || got.WithAccess != 3 || got.Total != 10 {
		t.Errorf("counts mismatch: %+v", got)
	}
	if got.WindowDays != defaultDashboardDays {
		t.Errorf("window_days: %d", got.WindowDays)
	}
	if len(got.EnlistmentsByDay) != 1 {
		t.Errorf("series len: %d", len(got.EnlistmentsByDay))
	}
}

func TestAdmin_Dashboard_DaysOutOfRange(t *testing.T) {
	_, mux := newTestAdminHandler(
		&fakeAdminUserStore{},
		&fakeAdminWaitlistStore{},
	)

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard?days=400", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "365") {
		t.Errorf("expected error to mention max 365, got %s", w.Body.String())
	}
}

func TestAdmin_Dashboard_DaysNonInt(t *testing.T) {
	_, mux := newTestAdminHandler(&fakeAdminUserStore{}, &fakeAdminWaitlistStore{})

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard?days=abc", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAdmin_ListWithAccess_PassesFilters(t *testing.T) {
	var gotEmail string
	var gotLimit, gotOffset int
	us := &fakeAdminUserStore{
		listWithAccessFn: func(_ context.Context, _, emailLike string, limit, offset int) ([]model.UserEntity, error) {
			gotEmail, gotLimit, gotOffset = emailLike, limit, offset
			return []model.UserEntity{{ID: "u1", Email: "alice@example.com", HasAccess: true}}, nil
		},
	}
	_, mux := newTestAdminHandler(us, &fakeAdminWaitlistStore{})

	req := httptest.NewRequest(http.MethodGet, "/admin/users/access?email=alice&limit=25&offset=50", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if gotEmail != "alice" || gotLimit != 25 || gotOffset != 50 {
		t.Errorf("filters: email=%q limit=%d offset=%d", gotEmail, gotLimit, gotOffset)
	}
}

func TestAdmin_ListWithAccess_LimitTooHigh(t *testing.T) {
	_, mux := newTestAdminHandler(&fakeAdminUserStore{}, &fakeAdminWaitlistStore{})

	req := httptest.NewRequest(http.MethodGet, "/admin/users/access?limit=1000", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (over max), got %d", w.Code)
	}
}

func TestAdmin_ListWaitlist_NegativeOffset(t *testing.T) {
	_, mux := newTestAdminHandler(&fakeAdminUserStore{}, &fakeAdminWaitlistStore{})

	req := httptest.NewRequest(http.MethodGet, "/admin/users/waitlist?offset=-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAdmin_ListWaitlist_HappyPath(t *testing.T) {
	ws := &fakeAdminWaitlistStore{
		listJoinedFn: func(_ context.Context, _, emailLike string, limit, offset int) ([]model.WaitingListAdminRow, error) {
			return []model.WaitingListAdminRow{{
				EntryID: "wl1", UserID: "u1", Email: "bob@example.com",
				Firstname: "Bob", Lastname: "S", Weight: 3,
				CreatedAt: time.Now(), WeightedCreatedAt: time.Now(),
			}}, nil
		},
	}
	_, mux := newTestAdminHandler(&fakeAdminUserStore{}, ws)

	req := httptest.NewRequest(http.MethodGet, "/admin/users/waitlist", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var got listWaitlistResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Entries) != 1 || got.Entries[0].Weight != 3 {
		t.Errorf("entries: %+v", got.Entries)
	}
}

func TestAdmin_Grant_HappyPath(t *testing.T) {
	tx := &fakeTx{}
	deleteCalled, grantCalled := false, false
	var grantSource string
	var grantIDs []string
	getCalled := false

	us := &fakeAdminUserStore{
		grantAccessTxFn: func(_ context.Context, _ model.DBTX, ids []string, source string) error {
			grantCalled = true
			grantSource = source
			grantIDs = ids
			return nil
		},
		getByIDFn: func(_ context.Context, id string) (*model.UserEntity, error) {
			getCalled = true
			return &model.UserEntity{ID: id, HasAccess: true}, nil
		},
	}
	ws := &fakeAdminWaitlistStore{
		beginTxFn: func(_ context.Context) (model.Tx, error) { return tx, nil },
		deleteByUserIDTxFn: func(_ context.Context, _ model.DBTX, _ string) error {
			deleteCalled = true
			return nil
		},
	}
	_, mux := newTestAdminHandler(us, ws)

	req := httptest.NewRequest(http.MethodPost, "/admin/users/u123/grant-access", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !deleteCalled {
		t.Error("expected DeleteByUserIDTx called")
	}
	if !grantCalled {
		t.Error("expected GrantAccessTx called")
	}
	if grantSource != "admin" {
		t.Errorf("expected admin source, got %q", grantSource)
	}
	if len(grantIDs) != 1 || grantIDs[0] != "u123" {
		t.Errorf("expected ids=[u123], got %v", grantIDs)
	}
	if !tx.committed {
		t.Error("expected tx commit")
	}
	if !getCalled {
		t.Error("expected GetByID called for response payload")
	}
}

func TestAdmin_Grant_UserNotFoundRollsBack(t *testing.T) {
	tx := &fakeTx{}
	us := &fakeAdminUserStore{
		grantAccessTxFn: func(_ context.Context, _ model.DBTX, _ []string, _ string) error {
			return model.ErrUserNotFound
		},
	}
	ws := &fakeAdminWaitlistStore{
		beginTxFn:          func(_ context.Context) (model.Tx, error) { return tx, nil },
		deleteByUserIDTxFn: func(_ context.Context, _ model.DBTX, _ string) error { return nil },
	}
	_, mux := newTestAdminHandler(us, ws)

	req := httptest.NewRequest(http.MethodPost, "/admin/users/missing/grant-access", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if tx.committed {
		t.Error("expected tx NOT committed on user-not-found")
	}
	if !tx.rolledBack {
		t.Error("expected tx rolled back")
	}
}

func TestAdmin_Revoke_HappyPath(t *testing.T) {
	revokeCalled := false
	var gotID, gotReason, gotBy string

	us := &fakeAdminUserStore{
		revokeAccessFn: func(_ context.Context, id, reason, by string) error {
			revokeCalled = true
			gotID, gotReason, gotBy = id, reason, by
			return nil
		},
		getByIDFn: func(_ context.Context, id string) (*model.UserEntity, error) {
			return &model.UserEntity{ID: id, HasAccess: false}, nil
		},
	}
	_, mux := newTestAdminHandler(us, &fakeAdminWaitlistStore{})

	body := strings.NewReader(`{"reason":"policy violation"}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/users/u9/revoke-access", body)
	// Simulate the BasicAuthMiddleware having stashed the username.
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyAdminUser, "admin"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !revokeCalled {
		t.Error("expected RevokeAccess called")
	}
	if gotID != "u9" || gotReason != "policy violation" || gotBy != "admin" {
		t.Errorf("revoke args: id=%q reason=%q by=%q", gotID, gotReason, gotBy)
	}
}

func TestAdmin_Revoke_EmptyReason(t *testing.T) {
	_, mux := newTestAdminHandler(
		&fakeAdminUserStore{revokeAccessFn: func(_ context.Context, _, _, _ string) error {
			t.Fatal("RevokeAccess must not be called when reason is empty")
			return nil
		}},
		&fakeAdminWaitlistStore{},
	)

	body := strings.NewReader(`{"reason":"   "}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/users/u1/revoke-access", body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAdmin_Revoke_ReasonTooLong(t *testing.T) {
	_, mux := newTestAdminHandler(
		&fakeAdminUserStore{revokeAccessFn: func(_ context.Context, _, _, _ string) error {
			t.Fatal("RevokeAccess must not be called when reason is too long")
			return nil
		}},
		&fakeAdminWaitlistStore{},
	)

	long := strings.Repeat("a", maxRevokeReasonLen+1)
	body := strings.NewReader(`{"reason":"` + long + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/users/u1/revoke-access", body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAdmin_Revoke_UserNotFound(t *testing.T) {
	us := &fakeAdminUserStore{
		revokeAccessFn: func(_ context.Context, _, _, _ string) error {
			return model.ErrUserNotFound
		},
	}
	_, mux := newTestAdminHandler(us, &fakeAdminWaitlistStore{})

	body := strings.NewReader(`{"reason":"x"}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/users/missing/revoke-access", body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestAdmin_Revoke_InvalidJSON(t *testing.T) {
	_, mux := newTestAdminHandler(&fakeAdminUserStore{}, &fakeAdminWaitlistStore{})

	body := strings.NewReader(`{`)
	req := httptest.NewRequest(http.MethodPost, "/admin/users/u1/revoke-access", body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAdmin_DeleteWaitlist_HappyPath(t *testing.T) {
	called := false
	ws := &fakeAdminWaitlistStore{
		deleteByIDFn: func(_ context.Context, id string) error {
			called = true
			if id != "wl42" {
				t.Errorf("expected id wl42, got %q", id)
			}
			return nil
		},
	}
	_, mux := newTestAdminHandler(&fakeAdminUserStore{}, ws)

	req := httptest.NewRequest(http.MethodDelete, "/admin/waitlist/wl42", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if !called {
		t.Error("expected DeleteByID called")
	}
}

func TestAdmin_DeleteWaitlist_NotFound(t *testing.T) {
	ws := &fakeAdminWaitlistStore{
		deleteByIDFn: func(_ context.Context, _ string) error {
			return model.ErrWaitingListEntryNotFound
		},
	}
	_, mux := newTestAdminHandler(&fakeAdminUserStore{}, ws)

	req := httptest.NewRequest(http.MethodDelete, "/admin/waitlist/missing", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestAdmin_DeleteWaitlist_InternalError(t *testing.T) {
	ws := &fakeAdminWaitlistStore{
		deleteByIDFn: func(_ context.Context, _ string) error {
			return errors.New("db went away")
		},
	}
	_, mux := newTestAdminHandler(&fakeAdminUserStore{}, ws)

	req := httptest.NewRequest(http.MethodDelete, "/admin/waitlist/wl1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestAdmin_MethodNotAllowed(t *testing.T) {
	_, mux := newTestAdminHandler(&fakeAdminUserStore{}, &fakeAdminWaitlistStore{})

	req := httptest.NewRequest(http.MethodPost, "/admin/dashboard", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestAdmin_ListProjects_HappyPath(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	projects := []model.Project{
		{Slug: "alpha", Name: "Alpha Project"},
		{Slug: "beta", Name: "Beta Project"},
	}
	h := NewAdminHandler(&fakeAdminUserStore{}, &fakeAdminWaitlistStore{}, projects, logger, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/projects", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var got []model.Project
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(got))
	}
}

// --- Send Digest tests -------------------------------------------------------

type fakeDigestSender struct {
	called     bool
	recipients []string
	data       notifier.DigestData
	err        error
}

func (f *fakeDigestSender) SendDigest(recipients []string, _, _ string, data notifier.DigestData) error {
	f.called = true
	f.recipients = recipients
	f.data = data
	return f.err
}

func newTestAdminHandlerWithDigest(us AdminUserStore, ws AdminWaitingListStore, projects []model.Project, sender AdminDigestSender) (*AdminHandler, *http.ServeMux) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewAdminHandler(us, ws, projects, logger, nil, sender)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return h, mux
}

func TestAdmin_SendDigest_Success(t *testing.T) {
	sender := &fakeDigestSender{}
	projects := []model.Project{{
		Slug: "default",
		Name: "Default",
		Digest: model.ProjectDigest{
			Recipients: []string{"admin@test.com", "ops@test.com"},
			From:       "digest@test.com",
			Subject:    "Test Digest",
		},
	}}
	us := &fakeAdminUserStore{
		listAllWithAccessFn: func(_ context.Context, slug string) ([]model.UserEntity, error) {
			return []model.UserEntity{{ID: "u1", Email: "alice@test.com", HasAccess: true}}, nil
		},
	}
	ws := &fakeAdminWaitlistStore{
		listAllJoinedFn: func(_ context.Context, slug string) ([]model.WaitingListAdminRow, error) {
			return []model.WaitingListAdminRow{{EntryID: "wl1", Email: "bob@test.com", CreatedAt: time.Now()}}, nil
		},
	}
	_, mux := newTestAdminHandlerWithDigest(us, ws, projects, sender)

	req := httptest.NewRequest(http.MethodPost, "/admin/digest/send?project=default", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !sender.called {
		t.Fatal("expected SendDigest called")
	}
	if len(sender.recipients) != 2 {
		t.Errorf("expected 2 recipients, got %d", len(sender.recipients))
	}
	if sender.data.EnlistedCount != 1 || sender.data.GrantedCount != 1 {
		t.Errorf("unexpected data: enlisted=%d granted=%d", sender.data.EnlistedCount, sender.data.GrantedCount)
	}
	if sender.data.PeriodStart != "Full state" {
		t.Errorf("expected PeriodStart='Full state', got %q", sender.data.PeriodStart)
	}

	var resp sendDigestResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SentTo != 2 {
		t.Errorf("expected sent_to=2, got %d", resp.SentTo)
	}
}

func TestAdmin_SendDigest_NoProject(t *testing.T) {
	sender := &fakeDigestSender{}
	projects := []model.Project{{Slug: "default", Name: "Default", Digest: model.ProjectDigest{Recipients: []string{"a@b.c"}}}}
	_, mux := newTestAdminHandlerWithDigest(&fakeAdminUserStore{}, &fakeAdminWaitlistStore{}, projects, sender)

	req := httptest.NewRequest(http.MethodPost, "/admin/digest/send", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if sender.called {
		t.Error("SendDigest should not be called when project is missing")
	}
}

func TestAdmin_SendDigest_NoDigestConfig(t *testing.T) {
	sender := &fakeDigestSender{}
	projects := []model.Project{{Slug: "default", Name: "Default"}}
	_, mux := newTestAdminHandlerWithDigest(&fakeAdminUserStore{}, &fakeAdminWaitlistStore{}, projects, sender)

	req := httptest.NewRequest(http.MethodPost, "/admin/digest/send?project=default", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if sender.called {
		t.Error("SendDigest should not be called when no recipients configured")
	}
}

func TestAdmin_SendDigest_NoSender(t *testing.T) {
	projects := []model.Project{{Slug: "default", Name: "Default", Digest: model.ProjectDigest{Recipients: []string{"a@b.c"}}}}
	_, mux := newTestAdminHandlerWithDigest(&fakeAdminUserStore{}, &fakeAdminWaitlistStore{}, projects, nil)

	req := httptest.NewRequest(http.MethodPost, "/admin/digest/send?project=default", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdmin_SendDigest_UnknownProject(t *testing.T) {
	sender := &fakeDigestSender{}
	projects := []model.Project{{Slug: "default", Name: "Default"}}
	_, mux := newTestAdminHandlerWithDigest(&fakeAdminUserStore{}, &fakeAdminWaitlistStore{}, projects, sender)

	req := httptest.NewRequest(http.MethodPost, "/admin/digest/send?project=nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdmin_SendDigest_SendError(t *testing.T) {
	sender := &fakeDigestSender{err: errors.New("smtp down")}
	projects := []model.Project{{
		Slug:   "default",
		Name:   "Default",
		Digest: model.ProjectDigest{Recipients: []string{"a@b.c"}},
	}}
	us := &fakeAdminUserStore{
		listAllWithAccessFn: func(_ context.Context, _ string) ([]model.UserEntity, error) {
			return nil, nil
		},
	}
	ws := &fakeAdminWaitlistStore{
		listAllJoinedFn: func(_ context.Context, _ string) ([]model.WaitingListAdminRow, error) {
			return nil, nil
		},
	}
	_, mux := newTestAdminHandlerWithDigest(us, ws, projects, sender)

	req := httptest.NewRequest(http.MethodPost, "/admin/digest/send?project=default", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}
