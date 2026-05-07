package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/tipok/waitinglist/internal/config"
	"github.com/tipok/waitinglist/internal/database"
	"github.com/tipok/waitinglist/internal/handler"
	"github.com/tipok/waitinglist/internal/handler/adminui"
	lg "github.com/tipok/waitinglist/internal/logger"
	"github.com/tipok/waitinglist/internal/repository"
	"github.com/tipok/waitinglist/internal/waitlist"
)

func main() {
	logger := lg.NewLogger()
	flags, err := config.ParseFlags(os.Args[1:])
	if err != nil {
		logger.Error("Error parsing flags", "error", err)
		os.Exit(1)
	}

	if flags.HealthCheck {
		runHealthCheck(logger, resolveHealthCheckPort(flags.Port))
	}

	cfg, err := config.Load(flags.ConfigPath)
	if err != nil {
		logger.Error("Error loading config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	dbUrl, err := url.Parse(cfg.Database.URL)
	if err != nil {
		logger.Error("Error parsing database URL", "error", err)
		os.Exit(1)
	}

	if dbUrl.User == nil {
		dbUrl.User = url.UserPassword(cfg.Database.Username, cfg.Database.Password)
	}
	db, err := database.NewPostgresDB(dbUrl.String())
	if err != nil {
		logger.Error("Error connecting to database", "error", err)
		os.Exit(1)
	}
	defer func(db *sql.DB) {
		err := db.Close()
		if err != nil {
			logger.Error("Error closing database connection", "error", err)
			os.Exit(1)
		}
	}(db)

	if err := database.RunMigrations(db, cfg.Database.MigrationsDir, logger); err != nil {
		logger.Error("Error running migrations", "error", err)
		os.Exit(1)
	}

	userRepo := repository.NewUserRepository(db)
	waitListRepo := repository.NewWaitingListRepository(db)
	schedulerRepo := repository.NewSchedulerRepository(db)
	waitListHandler := handler.NewWaitingListHandler(userRepo, waitListRepo, logger)
	healthHandler := handler.NewHealthHandler(db, logger)
	err = waitlist.Start(ctx, cfg, waitListRepo, userRepo, schedulerRepo)
	if err != nil {
		logger.Error("Error starting waitlist", "error", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	waitListHandler.RegisterRoutes(mux)
	healthHandler.RegisterRoutes(mux)

	adminUser := cfg.Admin.BasicAuth.Username
	adminHash := []byte(cfg.Admin.BasicAuth.PasswordHash)
	if adminUser == "" || len(adminHash) == 0 {
		logger.Warn("admin basic auth not configured; /admin routes disabled")
	} else {
		adminHandler := handler.NewAdminHandler(userRepo, waitListRepo, logger)
		auth := handler.BasicAuthMiddleware(adminUser, adminHash, "waitinglist-admin", logger)

		adminMux := http.NewServeMux()
		adminHandler.RegisterRoutes(adminMux)
		// File-server fallback for the embedded admin UI. The JSON routes
		// register more specific patterns (e.g. "GET /admin/dashboard"),
		// so ServeMux dispatches them first, and only requests for
		// /admin/, /admin/admin.css, /admin/admin.js fall through here.
		adminMux.Handle("/admin/", http.StripPrefix("/admin/", adminui.Handler()))
		mux.Handle("/admin/", auth(adminMux))
	}

	wrapped := handler.LoggingMiddleware(handler.JSONContentType(mux), logger)

	addr := fmt.Sprintf("0.0.0.0:%d", cfg.Port)
	logger.Info("Starting server", "addr", addr)

	srv := &http.Server{
		Addr:    addr,
		Handler: wrapped,
		BaseContext: func(l net.Listener) context.Context {
			return ctx
		},
	}

	go func() {
		logger.Info("Server listening on", "address", addr)
		if err := srv.ListenAndServe(); err != nil {
			if !errors.Is(err, http.ErrServerClosed) {
				logger.Error("Server forced to shutdown: ", "error", err)
			}
		}
	}()

	<-ctx.Done()

	stop()
	logger.Info("shutting down gracefully, press Ctrl+C again to force")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("Server forced to shutdown:", "error", err)
		panic(err)
	}
}

// resolveHealthCheckPort determines the port for the /healthz probe without
// reading a config file. Precedence: --port flag > WL_PORT env > DefaultPort.
func resolveHealthCheckPort(flagPort int) int {
	if flagPort > 0 {
		return flagPort
	}
	if v := os.Getenv("WL_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return config.DefaultPort
}

// probeHealth performs an HTTP GET to /healthz on the given port.
// Returns nil on HTTP 200, an error otherwise.
func probeHealth(port int) error {
	target := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(target) //nolint:noctx
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

// runHealthCheck calls probeHealth and exits with the appropriate code.
func runHealthCheck(logger *slog.Logger, port int) {
	if err := probeHealth(port); err != nil {
		logger.Error("health check failed", "error", err)
		os.Exit(1)
	}
	os.Exit(0)
}
