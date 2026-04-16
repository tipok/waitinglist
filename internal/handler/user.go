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
		WriteError(w, http.StatusBadRequest, "invalid JSON body", h.logger)
		return
	}

	if err := validateCreateRequest(req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error(), h.logger)
		return
	}

	user := &model.UserEntity{
		Firstname: req.Firstname,
		Lastname:  req.Lastname,
		Email:     req.Email,
	}

	if err := h.store.Create(r.Context(), user); err != nil {
		if errors.Is(err, model.ErrDuplicateEmail) {
			WriteError(w, http.StatusConflict, "email already exists", h.logger)
			return
		}
		h.logger.Error("Failed to create user", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
		return
	}

	WriteJSON(w, http.StatusCreated, user, h.logger)
}

func (h *UserHandler) handleGetByEmail(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")
	if email == "" {
		WriteError(w, http.StatusBadRequest, "email query parameter is required", h.logger)
		return
	}

	user, err := h.store.GetByEmail(r.Context(), email)
	if err != nil {
		if errors.Is(err, model.ErrUserNotFound) {
			WriteError(w, http.StatusNotFound, "user not found", h.logger)
			return
		}
		h.logger.Error("Failed to get user by email", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
		return
	}

	WriteJSON(w, http.StatusOK, user, h.logger)
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
