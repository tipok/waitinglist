package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// Pinger is the minimal interface needed to verify connectivity to a backing
// store. *sql.DB satisfies this via its PingContext method.
type Pinger interface {
	PingContext(ctx context.Context) error
}

// healthCheckTimeout bounds the database ping during /healthz probes.
const healthCheckTimeout = 2 * time.Second

// HealthHandler serves the /healthz endpoint.
type HealthHandler struct {
	db     Pinger
	logger *slog.Logger
}

// NewHealthHandler creates a new HealthHandler.
func NewHealthHandler(db Pinger, logger *slog.Logger) *HealthHandler {
	return &HealthHandler{db: db, logger: logger}
}

// RegisterRoutes registers /healthz on the given mux.
func (h *HealthHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", h.handle)
}

type healthResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

func (h *HealthHandler) handle(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), healthCheckTimeout)
	defer cancel()

	if err := h.db.PingContext(ctx); err != nil {
		h.logger.Warn("health check: database ping failed", "error", err)
		WriteJSON(w, http.StatusServiceUnavailable, healthResponse{
			Status: "unhealthy",
			Checks: map[string]string{"database": err.Error()},
		}, h.logger)
		return
	}

	WriteJSON(w, http.StatusOK, healthResponse{
		Status: "ok",
		Checks: map[string]string{"database": "ok"},
	}, h.logger)
}
