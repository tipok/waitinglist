package main

import (
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/tipok/waitinglist/internal/database"
)

func TestProbeHealth_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	port := srv.Listener.Addr().(*net.TCPAddr).Port
	if err := probeHealth(port); err != nil {
		t.Errorf("expected nil error on 200, got: %v", err)
	}
}

func TestProbeHealth_Unhealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	port := srv.Listener.Addr().(*net.TCPAddr).Port
	if err := probeHealth(port); err == nil {
		t.Error("expected error on 503, got nil")
	}
}

func TestProbeHealth_Unreachable(t *testing.T) {
	// Port 1 is reserved and never listening in practice.
	if err := probeHealth(1); err == nil {
		t.Error("expected error when server is unreachable, got nil")
	}
}

func TestResolveHealthCheckPort_FlagWins(t *testing.T) {
	t.Setenv("WL_PORT", "9999")
	if got := resolveHealthCheckPort(); got != 9999 {
		t.Errorf("expected 1234 (flag), got %d", got)
	}
}

func TestResolveHealthCheckPort_EnvUsedWhenFlagZero(t *testing.T) {
	t.Setenv("WL_PORT", "9999")
	if got := resolveHealthCheckPort(); got != 9999 {
		t.Errorf("expected 9999 (env), got %d", got)
	}
}

func TestResolveHealthCheckPort_DefaultWhenNothingSet(t *testing.T) {
	t.Setenv("WL_PORT", "")
	if got := resolveHealthCheckPort(); got != 8080 {
		t.Errorf("expected 8080 (default), got %d", got)
	}
}

func TestResolveHealthCheckPort_InvalidEnvFallsBack(t *testing.T) {
	t.Setenv("WL_PORT", "abc")
	if got := resolveHealthCheckPort(); got != 8080 {
		t.Errorf("expected 8080 (default) on invalid env, got %d", got)
	}
}

func TestBuildRepositories_SQLite(t *testing.T) {
	db, driver, err := database.New("sqlite://:memory:")
	if err != nil {
		t.Fatalf("opening sqlite db: %v", err)
	}
	defer db.Close()

	migrationsDir := findMigrationsDir(t, "sqlite")
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := database.RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("running migrations: %v", err)
	}

	userRepo, wlRepo, schedRepo := buildRepositories(db, driver)
	if userRepo == nil {
		t.Fatal("userRepo is nil")
	}
	if wlRepo == nil {
		t.Fatal("waitingListRepo is nil")
	}
	if schedRepo == nil {
		t.Fatal("schedulerRepo is nil")
	}
}

func TestBuildRepositories_Postgres(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	db, driver, err := database.New(dbURL)
	if err != nil {
		t.Fatalf("opening postgres db: %v", err)
	}
	defer db.Close()

	migrationsDir := findMigrationsDir(t, "postgres")
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := database.RunMigrations(db, migrationsDir, logger); err != nil {
		t.Fatalf("running migrations: %v", err)
	}

	userRepo, wlRepo, schedRepo := buildRepositories(db, driver)
	if userRepo == nil {
		t.Fatal("userRepo is nil")
	}
	if wlRepo == nil {
		t.Fatal("waitingListRepo is nil")
	}
	if schedRepo == nil {
		t.Fatal("schedulerRepo is nil")
	}
}

// findMigrationsDir walks up from cwd to find the migrations/<driver> directory.
func findMigrationsDir(t *testing.T, driver string) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting working directory: %v", err)
	}
	for {
		candidate := dir + "/migrations/" + driver
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
		parent := dir[:len(dir)-len("/"+lastSegment(dir))]
		if parent == dir {
			t.Fatalf("could not find migrations/%s directory", driver)
		}
		dir = parent
	}
}

func lastSegment(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}
