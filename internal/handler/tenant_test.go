package handler

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tipok/waitinglist/internal/model"
)

func newTestResolver(projects []model.Project, headerName, defaultSlug string, hostMapping map[string]string) *ProjectResolver {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewProjectResolver(headerName, defaultSlug, hostMapping, projects, logger)
}

func TestTenantMiddleware_HeaderResolution(t *testing.T) {
	projects := []model.Project{{ID: "id-a", Slug: "product-a", Name: "Product A"}}
	resolver := newTestResolver(projects, "X-Project-ID", "", nil)

	handler := resolver.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := ProjectFromContext(r.Context())
		if p == nil {
			t.Fatal("expected project in context")
		}
		if p.Slug != "product-a" {
			t.Errorf("expected slug product-a, got %s", p.Slug)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/waitinglist", nil)
	req.Header.Set("X-Project-ID", "product-a")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTenantMiddleware_HostMapping(t *testing.T) {
	projects := []model.Project{{ID: "id-b", Slug: "product-b", Name: "Product B"}}
	hostMapping := map[string]string{"waitlist.product-b.com": "product-b"}
	resolver := newTestResolver(projects, "X-Project-ID", "", hostMapping)

	handler := resolver.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := ProjectFromContext(r.Context())
		if p == nil {
			t.Fatal("expected project in context")
		}
		if p.ID != "id-b" {
			t.Errorf("expected id id-b, got %s", p.ID)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/waitinglist", nil)
	req.Host = "waitlist.product-b.com"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTenantMiddleware_HeaderPrecedenceOverHost(t *testing.T) {
	projects := []model.Project{
		{ID: "id-a", Slug: "product-a", Name: "Product A"},
		{ID: "id-b", Slug: "product-b", Name: "Product B"},
	}
	hostMapping := map[string]string{"waitlist.product-b.com": "product-b"}
	resolver := newTestResolver(projects, "X-Project-ID", "", hostMapping)

	handler := resolver.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := ProjectFromContext(r.Context())
		if p.Slug != "product-a" {
			t.Errorf("header should take precedence; expected product-a, got %s", p.Slug)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/waitinglist", nil)
	req.Host = "waitlist.product-b.com"
	req.Header.Set("X-Project-ID", "product-a")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestTenantMiddleware_DefaultSlugFallback(t *testing.T) {
	projects := []model.Project{{ID: "id-def", Slug: "default", Name: "Default"}}
	resolver := newTestResolver(projects, "X-Project-ID", "default", nil)

	handler := resolver.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := ProjectFromContext(r.Context())
		if p.Slug != "default" {
			t.Errorf("expected default, got %s", p.Slug)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/waitinglist", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTenantMiddleware_NoIdentification_Returns400(t *testing.T) {
	projects := []model.Project{{ID: "id-a", Slug: "product-a", Name: "Product A"}}
	resolver := newTestResolver(projects, "X-Project-ID", "", nil)

	handler := resolver.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("handler must not be called when project cannot be resolved")
		//goland:noinspection GoUnreachableCode
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/waitinglist", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestTenantMiddleware_UnknownSlug_Returns400(t *testing.T) {
	projects := []model.Project{{ID: "id-a", Slug: "product-a", Name: "Product A"}}
	resolver := newTestResolver(projects, "X-Project-ID", "", nil)

	handler := resolver.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("handler must not be called for unknown slug")
		//goland:noinspection GoUnreachableCode
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/waitinglist", nil)
	req.Header.Set("X-Project-ID", "unknown-project")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestTenantMiddleware_HostWithPort_StripsPort(t *testing.T) {
	projects := []model.Project{{ID: "id-c", Slug: "product-c", Name: "Product C"}}
	hostMapping := map[string]string{"product-c.com": "product-c"}
	resolver := newTestResolver(projects, "X-Project-ID", "", hostMapping)

	handler := resolver.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := ProjectFromContext(r.Context())
		if p.Slug != "product-c" {
			t.Errorf("expected product-c, got %s", p.Slug)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/waitinglist", nil)
	req.Host = "product-c.com:8080"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
