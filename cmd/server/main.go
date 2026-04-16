package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/tipok/waitinglist/internal/config"
	"github.com/tipok/waitinglist/internal/database"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
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

	db, err := database.NewPostgresDB(cfg.Database.URL)
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

	mux := http.NewServeMux()

	addr := fmt.Sprintf(":%d", cfg.Port)
	logger.Info("Starting server", "addr", addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		logger.Error("Error starting server", "error", err)
		os.Exit(1)
	}
}
