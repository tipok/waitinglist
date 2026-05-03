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

// WaitingListUserStore defines user persistence operations needed by the waiting list handler.
type WaitingListUserStore interface {
	CreateTx(ctx context.Context, tx model.DBTX, user *model.UserEntity) error
	GetByEmailTx(ctx context.Context, tx model.DBTX, email string) (*model.UserEntity, error)
	GetUserInfoByEmails(ctx context.Context, emails []string) ([]model.UserInfo, error)
}

// WaitingListStore defines waiting list persistence operations.
type WaitingListStore interface {
	Add(ctx context.Context, tx model.DBTX, userID string) (*model.WaitingListEntry, error)
	GetAll(ctx context.Context) ([]model.WaitingListEntry, error)
	BeginTx(ctx context.Context) (model.Tx, error)
}

// WaitingListHandler handles HTTP requests for waiting list operations.
type WaitingListHandler struct {
	userStore     WaitingListUserStore
	waitListStore WaitingListStore
	logger        *slog.Logger
}

// NewWaitingListHandler creates a new WaitingListHandler.
func NewWaitingListHandler(userStore WaitingListUserStore, waitListStore WaitingListStore, logger *slog.Logger) *WaitingListHandler {
	return &WaitingListHandler{
		userStore:     userStore,
		waitListStore: waitListStore,
		logger:        logger,
	}
}

// RegisterRoutes registers waiting list routes on the given mux.
func (h *WaitingListHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /waitinglist", h.handleAdd)
	mux.HandleFunc("GET /waitinglist", h.handleGetAll)
	mux.HandleFunc("GET /waitinglist/users", h.handleGetUsersByEmail)
}

type addToWaitingListRequest struct {
	Firstname string `json:"firstname"`
	Lastname  string `json:"lastname"`
	Email     string `json:"email"`
}

type addToWaitingListResponse struct {
	User             *model.UserEntity       `json:"user"`
	WaitingListEntry *model.WaitingListEntry `json:"waiting_list_entry"`
}

func (h *WaitingListHandler) handleAdd(w http.ResponseWriter, r *http.Request) {
	var req addToWaitingListRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid JSON body", h.logger)
		return
	}

	if err := validateWaitingListRequest(req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error(), h.logger)
		return
	}

	ctx := r.Context()

	tx, err := h.waitListStore.BeginTx(ctx)
	if err != nil {
		h.logger.Error("Failed to begin transaction", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
		return
	}
	defer func() {
		_ = tx.Rollback()
	}()

	// Look up or create user entity within the transaction.
	user, err := h.userStore.GetByEmailTx(ctx, tx, req.Email)
	if err != nil {
		if !errors.Is(err, model.ErrUserNotFound) {
			h.logger.Error("Failed to look up user by email", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
			return
		}

		// User does not exist — create a new one.
		ip := ClientIP(r)
		user = &model.UserEntity{
			Firstname: req.Firstname,
			Lastname:  req.Lastname,
			Email:     req.Email,
			IPAddress: &ip,
		}
		if err := h.userStore.CreateTx(ctx, tx, user); err != nil {
			h.logger.Error("Failed to create user", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
			return
		}
	} else if user.HasAccess {
		// User already has access — no need to (re-)enqueue. Tell the client to
		// reset its sign-up form and route the user to the protected page.
		h.logger.Info("user already has access; skipping waiting list add", "user_id", user.ID)
		WriteJSON(w, http.StatusResetContent, map[string]string{
			"message": model.ErrAlreadyHasAccess.Error(),
		}, h.logger)
		return
	}

	// Add user to the waiting list.
	entry, err := h.waitListStore.Add(ctx, tx, user.ID)
	if err != nil {
		if errors.Is(err, model.ErrAlreadyOnWaitingList) {
			WriteError(w, http.StatusConflict, "user is already on the waiting list", h.logger)
			return
		}
		h.logger.Error("Failed to add to waiting list", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
		return
	}

	if err := tx.Commit(); err != nil {
		h.logger.Error("Failed to commit transaction", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
		return
	}

	WriteJSON(w, http.StatusCreated, addToWaitingListResponse{
		User:             user,
		WaitingListEntry: entry,
	}, h.logger)
}

func (h *WaitingListHandler) handleGetAll(w http.ResponseWriter, r *http.Request) {
	entries, err := h.waitListStore.GetAll(r.Context())
	if err != nil {
		h.logger.Error("Failed to get waiting list entries", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
		return
	}

	WriteJSON(w, http.StatusOK, entries, h.logger)
}

func (h *WaitingListHandler) handleGetUsersByEmail(w http.ResponseWriter, r *http.Request) {
	emails := r.URL.Query()["email"]
	if len(emails) == 0 {
		WriteError(w, http.StatusBadRequest, "email query parameter is required", h.logger)
		return
	}

	// Validate and trim each email.
	for i, email := range emails {
		emails[i] = strings.TrimSpace(email)
		if emails[i] == "" {
			WriteError(w, http.StatusBadRequest, "email must not be empty", h.logger)
			return
		}
		if !strings.Contains(emails[i], "@") {
			WriteError(w, http.StatusBadRequest, "email must contain @", h.logger)
			return
		}
	}

	users, err := h.userStore.GetUserInfoByEmails(r.Context(), emails)
	if err != nil {
		h.logger.Error("Failed to get users by email", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
		return
	}

	WriteJSON(w, http.StatusOK, model.UserInfoList{Users: users}, h.logger)
}

func validateWaitingListRequest(req addToWaitingListRequest) error {
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
