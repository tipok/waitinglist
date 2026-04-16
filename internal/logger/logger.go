package logger

import (
	"log/slog"
	"os"
)

func NewLogger() *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}
