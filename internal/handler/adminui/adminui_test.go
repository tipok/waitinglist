package adminui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler_ServesIndexAtRoot(t *testing.T) {
	h := Handler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "<title>Waiting List Admin</title>") {
		t.Errorf("expected index.html in body, got %.120q", w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("expected text/html content type, got %q", ct)
	}
}

func TestHandler_ServesCSS(t *testing.T) {
	h := Handler()
	req := httptest.NewRequest(http.MethodGet, "/admin.css", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/css") {
		t.Errorf("expected text/css, got %q", ct)
	}
}

func TestHandler_ServesJS(t *testing.T) {
	h := Handler()
	req := httptest.NewRequest(http.MethodGet, "/admin.js", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	// Different Go versions report this as text/javascript or application/javascript;
	// any of the standard JS MIME types is acceptable.
	if !strings.HasPrefix(ct, "text/javascript") && !strings.HasPrefix(ct, "application/javascript") {
		t.Errorf("expected JavaScript content type, got %q", ct)
	}
}

func TestHandler_404OnMissingAsset(t *testing.T) {
	h := Handler()
	req := httptest.NewRequest(http.MethodGet, "/does-not-exist.png", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// TestHandler_ClearsContentTypeFromUpstream verifies the handler resets the
// Content-Type header before delegating, so JSONContentType middleware
// (which sets application/json on every response) does not poison
// HTML/CSS/JS responses.
func TestHandler_ClearsContentTypeFromUpstream(t *testing.T) {
	h := Handler()
	req := httptest.NewRequest(http.MethodGet, "/admin.css", nil)
	w := httptest.NewRecorder()
	w.Header().Set("Content-Type", "application/json") // simulate upstream middleware

	h.ServeHTTP(w, req)

	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/css") {
		t.Errorf("expected file server to set text/css after Content-Type clear, got %q", ct)
	}
}

func TestHandler_SetsCacheControlNoCache(t *testing.T) {
	h := Handler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if got := w.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("expected Cache-Control: no-cache, got %q", got)
	}
}
