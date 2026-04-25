package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"time"

	"github.com/tipok/waitinglist/internal/config"
	"github.com/tipok/waitinglist/internal/database"
	"github.com/tipok/waitinglist/internal/handler"
	lg "github.com/tipok/waitinglist/internal/logger"
	"github.com/tipok/waitinglist/internal/repository"
	"github.com/tipok/waitinglist/internal/waitlist"
)

func main() {
	logger := lg.NewLogger()
	configPath, err := config.ParseFlags(os.Args[1:])
	if err != nil {
		logger.Error("Error parsing flags", "error", err)
		os.Exit(1)
	}

	cfg, err := config.Load(configPath)
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

	if err := database.RunMigrations(db, "migrations", logger); err != nil {
		logger.Error("Error running migrations", "error", err)
		os.Exit(1)
	}

	userRepo := repository.NewUserRepository(db)
	waitListRepo := repository.NewWaitingListRepository(db)
	schedulerRepo := repository.NewSchedulerRepository(db)
	waitListHandler := handler.NewWaitingListHandler(userRepo, waitListRepo, logger)
	err = waitlist.Start(ctx, cfg, waitListRepo, userRepo, schedulerRepo)
	if err != nil {
		logger.Error("Error starting waitlist", "error", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	waitListHandler.RegisterRoutes(mux)

	wrapped := handler.LoggingMiddleware(handler.JSONContentType(mux), logger)

	addr := fmt.Sprintf(":%d", cfg.Port)
	logger.Info("Starting server", "addr", addr)

	srv := &http.Server{
		Addr:    addr,
		Handler: wrapped,
		BaseContext: func(l net.Listener) context.Context {
			return ctx
		},
	}

	go func() {
		logger.Info("Server listening on ", "address", addr)
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
