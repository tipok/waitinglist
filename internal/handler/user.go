package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/tipok/waitinglist/internal/model"
)

// UserStore defines the interface for user persistence operations.
type UserStore interface {
	Create(ctx context.Context, user *model.UserEntity) error
	GetByEmail(ctx context.Context, email string) (*model.UserEntity, error)
}

// UserHandler handles HTTP requests for user entity operations.
type UserHandler struct {
	store  UserStore
	logger *slog.Logger
}

// NewUserHandler creates a new UserHandler.
func NewUserHandler(store UserStore, logger *slog.Logger) *UserHandler {
	return &UserHandler{store: store, logger: logger}
}

// RegisterRoutes registers user routes on the given mux.
func (h *UserHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /users", h.handleCreate)
	mux.HandleFunc("GET /users", h.handleGetByEmail)
}

type createUserRequest struct {
	Firstname string `json:"firstname"`
	Lastname  string `json:"lastname"`
	Email     string `json:"email"`
}

func (h *UserHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if err := validateCreateRequest(req); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	user := &model.UserEntity{
		Firstname: req.Firstname,
		Lastname:  req.Lastname,
		Email:     req.Email,
	}

	if err := h.store.Create(r.Context(), user); err != nil {
		if errors.Is(err, model.ErrDuplicateEmail) {
			h.writeError(w, http.StatusConflict, "email already exists")
			return
		}
		h.logger.Error("Failed to create user", "error", err)
		h.writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.writeJSON(w, http.StatusCreated, user)
}

func (h *UserHandler) handleGetByEmail(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")
	if email == "" {
		h.writeError(w, http.StatusBadRequest, "email query parameter is required")
		return
	}

	user, err := h.store.GetByEmail(r.Context(), email)
	if err != nil {
		if errors.Is(err, model.ErrUserNotFound) {
			h.writeError(w, http.StatusNotFound, "user not found")
			return
		}
		h.logger.Error("Failed to get user by email", "error", err)
		h.writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.writeJSON(w, http.StatusOK, user)
}

func validateCreateRequest(req createUserRequest) error {
	if strings.TrimSpace(req.Firstname) == "" {
		return errors.New("firstname is required")
	}
	if strings.TrimSpace(req.Lastname) == "" {
		return errors.New("lastname is required")
	}
	if strings.TrimSpace(req.Email) == "" {
		return errors.New("email is required")
	}
	if !strings.Contains(req.Email, "@") {
		return errors.New("email must contain @")
	}
	return nil
}

func (h *UserHandler) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		h.logger.Error("Failed to encode response", "error", err)
	}
}

func (h *UserHandler) writeError(w http.ResponseWriter, status int, message string) {
	h.writeJSON(w, status, map[string]string{"error": message})
}
