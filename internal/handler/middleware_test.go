package handler

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestJSONContentType_SetsHeader(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := JSONContentType(inner)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %s", ct)
	}
}

func TestJSONContentType_SetsHeaderOnErrorStatus(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	handler := JSONContentType(inner)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type application/json even on error, got %s", ct)
	}
}

func TestJSONContentType_ChainsNextHandler(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := JSONContentType(inner)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if !called {
		t.Fatal("expected inner handler to be called")
	}
}

func TestLoggingMiddleware_LogsRequestDetails(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := LoggingMiddleware(inner, logger)
	req := httptest.NewRequest(http.MethodGet, "/test-path", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	logOutput := buf.String()
	for _, expected := range []string{"method=GET", "path=/test-path", "status=200"} {
		if !strings.Contains(logOutput, expected) {
			t.Errorf("expected log to contain %q, got: %s", expected, logOutput)
		}
	}
}

func TestLoggingMiddleware_LogsDuration(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	handler := LoggingMiddleware(inner, logger)
	req := httptest.NewRequest(http.MethodGet, "/slow", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	logOutput := buf.String()
	if !strings.Contains(logOutput, "duration=") {
		t.Errorf("expected log to contain duration, got: %s", logOutput)
	}
}

func TestLoggingMiddleware_CapturesNonOKStatus(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	handler := LoggingMiddleware(inner, logger)
	req := httptest.NewRequest(http.MethodPost, "/missing", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	logOutput := buf.String()
	if !strings.Contains(logOutput, "status=404") {
		t.Errorf("expected log to contain status=404, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "method=POST") {
		t.Errorf("expected log to contain method=POST, got: %s", logOutput)
	}
}

func TestLoggingMiddleware_ChainsNextHandler(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := LoggingMiddleware(inner, logger)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if !called {
		t.Fatal("expected inner handler to be called")
	}
}

func TestLoggingMiddleware_SkipsHealthz(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := LoggingMiddleware(inner, logger)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if !called {
		t.Fatal("expected inner handler to be called for /healthz")
	}
	if buf.Len() != 0 {
		t.Errorf("expected no log output for /healthz, got: %s", buf.String())
	}
}

func TestLoggingMiddleware_LogsNonHealthzPath(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := LoggingMiddleware(inner, logger)
	req := httptest.NewRequest(http.MethodGet, "/waitinglist", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	logOutput := buf.String()
	if !strings.Contains(logOutput, "path=/waitinglist") {
		t.Errorf("expected log to contain path=/waitinglist, got: %s", logOutput)
	}
}

func TestBasicAuthMiddleware_NoAuthHeader_Returns401(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	hash, err := bcrypt.GenerateFromPassword([]byte("changeme"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	called := false
	inner := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	})

	handler := BasicAuthMiddleware("admin", hash, "test-realm", logger)(inner)
	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if got := w.Header().Get("WWW-Authenticate"); !strings.Contains(got, `realm="test-realm"`) {
		t.Errorf("expected WWW-Authenticate to include realm, got %q", got)
	}
	if called {
		t.Error("inner handler must not be called")
	}
}

func TestBasicAuthMiddleware_WrongUsername_Returns401(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	hash, _ := bcrypt.GenerateFromPassword([]byte("changeme"), bcrypt.MinCost)

	handler := BasicAuthMiddleware("admin", hash, "r", logger)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	req.SetBasicAuth("nope", "changeme")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestBasicAuthMiddleware_WrongPassword_Returns401(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	hash, _ := bcrypt.GenerateFromPassword([]byte("changeme"), bcrypt.MinCost)

	handler := BasicAuthMiddleware("admin", hash, "r", logger)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	req.SetBasicAuth("admin", "wrong")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestBasicAuthMiddleware_ValidCreds_AllowsRequestAndSetsCtx(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	hash, _ := bcrypt.GenerateFromPassword([]byte("changeme"), bcrypt.MinCost)

	var seenUser string
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seenUser = AdminUserFromContext(r.Context())
	})

	handler := BasicAuthMiddleware("admin", hash, "r", logger)(inner)
	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	req.SetBasicAuth("admin", "changeme")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if seenUser != "admin" {
		t.Errorf("expected admin user in ctx, got %q", seenUser)
	}
}

func TestAdminUserFromContext_Empty(t *testing.T) {
	if got := AdminUserFromContext(t.Context()); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}
