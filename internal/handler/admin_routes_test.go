package handler

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/tipok/waitinglist/internal/handler/adminui"
	"github.com/tipok/waitinglist/internal/model"
)

// buildAdminServer wires the full chain that lives in cmd/server/main.go for
// the /admin/* routes: JSONContentType -> BasicAuth -> {AdminHandler routes,
// adminui file server fallback}. The fakes return harmless values for the
// JSON routes because the asset/auth tests only care about wiring.
func buildAdminServer(t *testing.T, username, password string) http.Handler {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hashing password: %v", err)
	}

	us := &fakeAdminUserStore{
		countByAccessFn: func(_ context.Context, _ string) (int, int, error) { return 1, 2, nil },
		enlistmentsByDayFn: func(_ context.Context, _ string, _ int) ([]model.DayCount, error) {
			return []model.DayCount{}, nil
		},
	}
	ws := &fakeAdminWaitlistStore{}

	projects := []model.Project{{Slug: "default", Name: "Default"}}
	adminHandler := NewAdminHandler(us, ws, projects, logger, nil, nil)
	auth := BasicAuthMiddleware(username, hash, "test", logger)

	adminMux := http.NewServeMux()
	adminHandler.RegisterRoutes(adminMux)
	adminMux.Handle("/admin/", http.StripPrefix("/admin/", adminui.Handler()))

	mux := http.NewServeMux()
	mux.Handle("/admin/", auth(adminMux))

	return JSONContentType(mux)
}

func TestAdminRoutes_HTML_Unauthenticated(t *testing.T) {
	srv := buildAdminServer(t, "admin", "changeme")

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if got := w.Header().Get("WWW-Authenticate"); !strings.Contains(got, "Basic") {
		t.Errorf("expected WWW-Authenticate header, got %q", got)
	}
}

func TestAdminRoutes_HTML_Authenticated(t *testing.T) {
	srv := buildAdminServer(t, "admin", "changeme")

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	req.SetBasicAuth("admin", "changeme")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "<title>Waiting List Admin</title>") {
		t.Errorf("expected index.html title, got %.120q", w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("expected text/html (JSONContentType must not poison assets), got %q", ct)
	}
}

func TestAdminRoutes_CSS_Authenticated(t *testing.T) {
	srv := buildAdminServer(t, "admin", "changeme")

	req := httptest.NewRequest(http.MethodGet, "/admin/admin.css", nil)
	req.SetBasicAuth("admin", "changeme")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("expected text/css, got %q", ct)
	}
}

// TestAdminRoutes_JSONNotShadowedByFileServer guards the mux precedence
// rule we rely on: the registered "GET /admin/dashboard" pattern must
// dispatch before the catch-all file-server handler at "/admin/".
func TestAdminRoutes_JSONNotShadowedByFileServer(t *testing.T) {
	srv := buildAdminServer(t, "admin", "changeme")

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	req.SetBasicAuth("admin", "changeme")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("expected application/json (the JSON handler should win), got %q", ct)
	}
	if !strings.Contains(w.Body.String(), `"waiting_list"`) {
		t.Errorf("expected dashboard JSON, got %.120q", w.Body.String())
	}
}
