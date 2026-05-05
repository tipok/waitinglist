package handler

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// ctxKey is a private context key type to avoid collisions with other packages.
type ctxKey int

const (
	// ctxKeyAdminUser carries the authenticated admin username on the request
	// context after BasicAuthMiddleware succeeds.
	ctxKeyAdminUser ctxKey = iota
)

// AdminUserFromContext returns the admin username stashed by
// BasicAuthMiddleware, or "" if none is present.
func AdminUserFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyAdminUser).(string)
	return v
}

// statusRecorder wraps http.ResponseWriter to capture the status code.
type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

// LoggingMiddleware logs the method, path, status code, and duration for every request.
// /healthz is excluded to avoid flooding the log stream with orchestrator probe traffic.
func LoggingMiddleware(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rec, r)
		logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.statusCode,
			"duration", time.Since(start),
		)
	})
}

// JSONContentType sets Content-Type: application/json on all responses.
func JSONContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

// BasicAuthMiddleware guards the wrapped handler behind HTTP Basic Auth.
// `passwordHash` is a bcrypt hash; generate it with `htpasswd -nbBC 10 …` or
// `bcrypt.GenerateFromPassword`. On success the authenticated username is
// added to the request context (retrievable via AdminUserFromContext).
//
// On failure the response is 401 with `WWW-Authenticate: Basic realm="…"`,
// no body, and the failure is logged at Info — failed admin auth is a
// normal probe pattern and should not look like an error to operators.
func BasicAuthMiddleware(username string, passwordHash []byte, realm string, logger *slog.Logger) func(http.Handler) http.Handler {
	expectedUser := []byte(username)
	wwwAuth := `Basic realm="` + realm + `"`

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, pass, ok := r.BasicAuth()
			if !ok {
				w.Header().Set("WWW-Authenticate", wwwAuth)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			if subtle.ConstantTimeCompare([]byte(user), expectedUser) != 1 {
				logger.Info("admin auth failed", "reason", "username mismatch", "remote_addr", r.RemoteAddr)
				w.Header().Set("WWW-Authenticate", wwwAuth)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			if err := bcrypt.CompareHashAndPassword(passwordHash, []byte(pass)); err != nil {
				logger.Info("admin auth failed", "reason", "password mismatch", "remote_addr", r.RemoteAddr)
				w.Header().Set("WWW-Authenticate", wwwAuth)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), ctxKeyAdminUser, username)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
