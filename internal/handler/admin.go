package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tipok/waitinglist/internal/model"
)

var validSlugRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

const (
	defaultAdminListLimit = 50
	maxAdminListLimit     = 200

	defaultDashboardDays = 90
	maxDashboardDays     = 365

	maxRevokeReasonLen = 500
)

// AdminUserStore defines the user-side persistence operations needed by the
// admin handler. Kept narrow so handler tests can use a fake.
type AdminUserStore interface {
	CountByAccess(ctx context.Context, projectID string) (waitListCount int, withAccessCount int, err error)
	EnlistmentsByDay(ctx context.Context, projectID string, days int) ([]model.DayCount, error)
	ListWithAccess(ctx context.Context, projectID, emailLike string, limit, offset int) ([]model.UserEntity, error)
	GetByID(ctx context.Context, id string) (*model.UserEntity, error)
	// GrantAccessTx is called inside the grant transaction (alongside
	// AdminWaitingListStore.DeleteByUserIDTx) so the two writes are atomic.
	GrantAccessTx(ctx context.Context, tx model.DBTX, ids []string, source string) error
	// RevokeAccess is a single UPDATE; it does not need a transaction.
	RevokeAccess(ctx context.Context, id, reason, by string) error
}

// AdminWaitingListStore defines the waiting-list operations needed by the
// admin handler.
type AdminWaitingListStore interface {
	ListJoined(ctx context.Context, projectID, emailLike string, limit, offset int) ([]model.WaitingListAdminRow, error)
	DeleteByID(ctx context.Context, id string) error
	DeleteByUserIDTx(ctx context.Context, tx model.DBTX, userID string) error
	BeginTx(ctx context.Context) (model.Tx, error)
}

// AdminProjectStore defines project persistence operations needed by the admin handler.
type AdminProjectStore interface {
	GetAll(ctx context.Context) ([]model.Project, error)
	GetBySlug(ctx context.Context, slug string) (*model.Project, error)
	GetByID(ctx context.Context, id string) (*model.Project, error)
	Create(ctx context.Context, p *model.Project) error
	Update(ctx context.Context, p *model.Project) error
}

// ProjectCacheReloader allows the admin handler to refresh the tenant
// middleware's project cache after mutations.
type ProjectCacheReloader interface {
	Reload(projects []model.Project)
}

// AdminHandler serves the /admin/* JSON endpoints. The caller is responsible
// for wrapping the registered routes in BasicAuthMiddleware.
type AdminHandler struct {
	userStore     AdminUserStore
	wlStore       AdminWaitingListStore
	projectStore  AdminProjectStore
	cacheReloader ProjectCacheReloader
	logger        *slog.Logger
}

// NewAdminHandler creates a new AdminHandler.
func NewAdminHandler(userStore AdminUserStore, wlStore AdminWaitingListStore, projectStore AdminProjectStore, cacheReloader ProjectCacheReloader, logger *slog.Logger) *AdminHandler {
	return &AdminHandler{userStore: userStore, wlStore: wlStore, projectStore: projectStore, cacheReloader: cacheReloader, logger: logger}
}

// RegisterRoutes registers the admin routes on the given mux. Wrap the mux
// in BasicAuthMiddleware before mounting it on the public handler tree.
func (h *AdminHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/dashboard", h.handleDashboard)
	mux.HandleFunc("GET /admin/users/access", h.handleListWithAccess)
	mux.HandleFunc("GET /admin/users/waitlist", h.handleListWaitlist)
	mux.HandleFunc("POST /admin/users/{id}/grant-access", h.handleGrant)
	mux.HandleFunc("POST /admin/users/{id}/revoke-access", h.handleRevoke)
	mux.HandleFunc("DELETE /admin/waitlist/{id}", h.handleDeleteWaitlist)
	mux.HandleFunc("GET /admin/projects", h.handleListProjects)
	mux.HandleFunc("POST /admin/projects", h.handleCreateProject)
	mux.HandleFunc("PUT /admin/projects/{id}", h.handleUpdateProject)
}

type dashboardResponse struct {
	WaitingList      int              `json:"waiting_list"`
	WithAccess       int              `json:"with_access"`
	Total            int              `json:"total"`
	EnlistmentsByDay []model.DayCount `json:"enlistments_by_day"`
	WindowDays       int              `json:"window_days"`
}

func (h *AdminHandler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	days, err := parseIntQuery(r, "days", defaultDashboardDays, 1, maxDashboardDays)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error(), h.logger)
		return
	}

	ctx := r.Context()
	projectID, err := h.resolveProjectFilter(r)
	if err != nil {
		if errors.Is(err, model.ErrProjectNotFound) {
			WriteError(w, http.StatusBadRequest, "unknown project", h.logger)
		} else {
			h.logger.Error("admin: project filter resolution failed", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
		}
		return
	}

	waitList, withAccess, err := h.userStore.CountByAccess(ctx, projectID)
	if err != nil {
		h.logger.Error("admin dashboard: count failed", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
		return
	}

	series, err := h.userStore.EnlistmentsByDay(ctx, projectID, days)
	if err != nil {
		h.logger.Error("admin dashboard: enlistments query failed", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
		return
	}

	WriteJSON(w, http.StatusOK, dashboardResponse{
		WaitingList:      waitList,
		WithAccess:       withAccess,
		Total:            waitList + withAccess,
		EnlistmentsByDay: series,
		WindowDays:       days,
	}, h.logger)
}

type listWithAccessResponse struct {
	Users  []model.UserEntity `json:"users"`
	Limit  int                `json:"limit"`
	Offset int                `json:"offset"`
}

func (h *AdminHandler) handleListWithAccess(w http.ResponseWriter, r *http.Request) {
	emailLike := strings.TrimSpace(r.URL.Query().Get("email"))
	projectID, err := h.resolveProjectFilter(r)
	if err != nil {
		if errors.Is(err, model.ErrProjectNotFound) {
			WriteError(w, http.StatusBadRequest, "unknown project", h.logger)
		} else {
			h.logger.Error("admin: project filter resolution failed", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
		}
		return
	}

	limit, err := parseIntQuery(r, "limit", defaultAdminListLimit, 1, maxAdminListLimit)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error(), h.logger)
		return
	}
	offset, err := parseIntQuery(r, "offset", 0, 0, 1<<30)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error(), h.logger)
		return
	}

	users, err := h.userStore.ListWithAccess(r.Context(), projectID, emailLike, limit, offset)
	if err != nil {
		h.logger.Error("admin list-with-access failed", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
		return
	}

	WriteJSON(w, http.StatusOK, listWithAccessResponse{Users: users, Limit: limit, Offset: offset}, h.logger)
}

type listWaitlistResponse struct {
	Entries []model.WaitingListAdminRow `json:"entries"`
	Limit   int                         `json:"limit"`
	Offset  int                         `json:"offset"`
}

func (h *AdminHandler) handleListWaitlist(w http.ResponseWriter, r *http.Request) {
	emailLike := strings.TrimSpace(r.URL.Query().Get("email"))
	projectID, err := h.resolveProjectFilter(r)
	if err != nil {
		if errors.Is(err, model.ErrProjectNotFound) {
			WriteError(w, http.StatusBadRequest, "unknown project", h.logger)
		} else {
			h.logger.Error("admin: project filter resolution failed", "error", err)
			WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
		}
		return
	}

	limit, err := parseIntQuery(r, "limit", defaultAdminListLimit, 1, maxAdminListLimit)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error(), h.logger)
		return
	}
	offset, err := parseIntQuery(r, "offset", 0, 0, 1<<30)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error(), h.logger)
		return
	}

	entries, err := h.wlStore.ListJoined(r.Context(), projectID, emailLike, limit, offset)
	if err != nil {
		h.logger.Error("admin list-waitlist failed", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
		return
	}

	WriteJSON(w, http.StatusOK, listWaitlistResponse{Entries: entries, Limit: limit, Offset: offset}, h.logger)
}

func (h *AdminHandler) handleGrant(w http.ResponseWriter, r *http.Request) {
	// TODO(multi-tenancy): validate that the target resource belongs to the
	// project scope of the current admin session once per-admin project
	// scoping is implemented.
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		WriteError(w, http.StatusBadRequest, "id is required", h.logger)
		return
	}

	adminUser := AdminUserFromContext(r.Context())

	ctx := r.Context()
	tx, err := h.wlStore.BeginTx(ctx)
	if err != nil {
		h.logger.Error("admin grant: begin tx failed", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
		return
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := h.wlStore.DeleteByUserIDTx(ctx, tx, id); err != nil {
		h.logger.Error("admin grant: delete waitlist row failed", "error", err, "user_id", id)
		WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
		return
	}

	if err := h.userStore.GrantAccessTx(ctx, tx, []string{id}, "admin"); err != nil {
		if errors.Is(err, model.ErrUserNotFound) {
			WriteError(w, http.StatusNotFound, "user not found", h.logger)
			return
		}
		h.logger.Error("admin grant: grant access failed", "error", err, "user_id", id)
		WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
		return
	}

	if err := tx.Commit(); err != nil {
		h.logger.Error("admin grant: commit failed", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
		return
	}

	user, err := h.userStore.GetByID(ctx, id)
	if err != nil {
		h.logger.Error("admin grant: post-commit fetch failed", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
		return
	}

	h.logger.Info("admin grant: access granted", "admin_user", adminUser, "user_id", id)
	WriteJSON(w, http.StatusOK, user, h.logger)
}

type revokeRequest struct {
	Reason string `json:"reason"`
}

func (h *AdminHandler) handleRevoke(w http.ResponseWriter, r *http.Request) {
	// TODO(multi-tenancy): validate that the target resource belongs to the
	// project scope of the current admin session once per-admin project
	// scoping is implemented.
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		WriteError(w, http.StatusBadRequest, "id is required", h.logger)
		return
	}

	var req revokeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid JSON body", h.logger)
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		WriteError(w, http.StatusBadRequest, "reason is required", h.logger)
		return
	}
	if len(reason) > maxRevokeReasonLen {
		WriteError(w, http.StatusBadRequest, "reason exceeds 500 characters", h.logger)
		return
	}

	adminUser := AdminUserFromContext(r.Context())
	ctx := r.Context()

	if err := h.userStore.RevokeAccess(ctx, id, reason, adminUser); err != nil {
		switch {
		case errors.Is(err, model.ErrUserNotFound):
			WriteError(w, http.StatusNotFound, "user not found", h.logger)
		case errors.Is(err, model.ErrRevokeReasonRequired):
			WriteError(w, http.StatusBadRequest, "reason is required", h.logger)
		default:
			h.logger.Error("admin revoke: failed", "error", err, "user_id", id)
			WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
		}
		return
	}

	user, err := h.userStore.GetByID(ctx, id)
	if err != nil {
		h.logger.Error("admin revoke: post-commit fetch failed", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
		return
	}

	logReason := reason
	if runes := []rune(logReason); len(runes) > 80 {
		logReason = string(runes[:80]) + "..."
	}
	h.logger.Info("admin revoke: access revoked",
		"admin_user", adminUser, "user_id", id, "reason", logReason)
	WriteJSON(w, http.StatusOK, user, h.logger)
}

func (h *AdminHandler) handleDeleteWaitlist(w http.ResponseWriter, r *http.Request) {
	// TODO(multi-tenancy): validate that the target resource belongs to the
	// project scope of the current admin session once per-admin project
	// scoping is implemented.
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		WriteError(w, http.StatusBadRequest, "id is required", h.logger)
		return
	}

	if err := h.wlStore.DeleteByID(r.Context(), id); err != nil {
		if errors.Is(err, model.ErrWaitingListEntryNotFound) {
			WriteError(w, http.StatusNotFound, "waiting list entry not found", h.logger)
			return
		}
		h.logger.Error("admin delete waitlist: failed", "error", err, "entry_id", id)
		WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
		return
	}

	h.logger.Info("admin delete waitlist: removed",
		"admin_user", AdminUserFromContext(r.Context()), "entry_id", id)
	w.WriteHeader(http.StatusNoContent)
}

// resolveProjectFilter reads an optional ?project=<slug> query parameter and
// resolves it to a project ID. Returns "" (meaning all projects) when the
// parameter is absent. Returns an error when the slug is unknown or the DB
// lookup fails.
func (h *AdminHandler) resolveProjectFilter(r *http.Request) (string, error) {
	slug := strings.TrimSpace(r.URL.Query().Get("project"))
	if slug == "" {
		return "", nil
	}
	p, err := h.projectStore.GetBySlug(r.Context(), slug)
	if err != nil {
		return "", err
	}
	return p.ID, nil
}

func (h *AdminHandler) handleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.projectStore.GetAll(r.Context())
	if err != nil {
		h.logger.Error("admin list-projects failed", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
		return
	}
	WriteJSON(w, http.StatusOK, projects, h.logger)
}

type createProjectRequest struct {
	Slug                  string  `json:"slug"`
	Name                  string  `json:"name"`
	EntryBatchSize        *int    `json:"entry_batch_size,omitempty"`
	EntryWindowInterval   *string `json:"entry_window_interval,omitempty"`
	WaitlistCheckInterval *string `json:"waitlist_check_interval,omitempty"` // stored but not yet used by scheduler
	SchedulerDisabled     bool    `json:"scheduler_disabled"`
}

func (h *AdminHandler) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid JSON body", h.logger)
		return
	}

	slug := strings.TrimSpace(req.Slug)
	name := strings.TrimSpace(req.Name)
	if slug == "" {
		WriteError(w, http.StatusBadRequest, "slug is required", h.logger)
		return
	}
	if !validSlugRe.MatchString(slug) {
		WriteError(w, http.StatusBadRequest, "slug must be lowercase alphanumeric with hyphens", h.logger)
		return
	}
	if name == "" {
		WriteError(w, http.StatusBadRequest, "name is required", h.logger)
		return
	}

	if req.EntryBatchSize != nil && *req.EntryBatchSize < 1 {
		WriteError(w, http.StatusBadRequest, "entry_batch_size must be >= 1", h.logger)
		return
	}

	p := &model.Project{
		Slug:              slug,
		Name:              name,
		EntryBatchSize:    req.EntryBatchSize,
		SchedulerDisabled: req.SchedulerDisabled,
	}
	if req.EntryWindowInterval != nil {
		d, err := parseDurationString(*req.EntryWindowInterval)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid entry_window_interval", h.logger)
			return
		}
		p.EntryWindowInterval = &d
	}
	if req.WaitlistCheckInterval != nil {
		d, err := parseDurationString(*req.WaitlistCheckInterval)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid waitlist_check_interval", h.logger)
			return
		}
		p.WaitlistCheckInterval = &d
	}

	if err := h.projectStore.Create(r.Context(), p); err != nil {
		if errors.Is(err, model.ErrDuplicateProjectSlug) {
			WriteError(w, http.StatusConflict, "project slug already exists", h.logger)
			return
		}
		h.logger.Error("admin create-project failed", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
		return
	}

	h.reloadProjectCache(r.Context())

	h.logger.Info("admin project created",
		"admin_user", AdminUserFromContext(r.Context()), "project_slug", slug)
	WriteJSON(w, http.StatusCreated, p, h.logger)
}

type updateProjectRequest struct {
	Name                  *string `json:"name,omitempty"`
	EntryBatchSize        *int    `json:"entry_batch_size,omitempty"`
	EntryWindowInterval   *string `json:"entry_window_interval,omitempty"`
	WaitlistCheckInterval *string `json:"waitlist_check_interval,omitempty"` // stored but not yet used by scheduler
	SchedulerDisabled     *bool   `json:"scheduler_disabled,omitempty"`
}

func (h *AdminHandler) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		WriteError(w, http.StatusBadRequest, "id is required", h.logger)
		return
	}

	existing, err := h.projectStore.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, model.ErrProjectNotFound) {
			WriteError(w, http.StatusNotFound, "project not found", h.logger)
			return
		}
		h.logger.Error("admin update-project: fetch failed", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
		return
	}

	var req updateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid JSON body", h.logger)
		return
	}

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			WriteError(w, http.StatusBadRequest, "name must not be empty", h.logger)
			return
		}
		existing.Name = name
	}
	if req.EntryBatchSize != nil {
		if *req.EntryBatchSize < 1 {
			WriteError(w, http.StatusBadRequest, "entry_batch_size must be >= 1", h.logger)
			return
		}
		existing.EntryBatchSize = req.EntryBatchSize
	}
	if req.EntryWindowInterval != nil {
		d, err := parseDurationString(*req.EntryWindowInterval)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid entry_window_interval", h.logger)
			return
		}
		existing.EntryWindowInterval = &d
	}
	if req.WaitlistCheckInterval != nil {
		d, err := parseDurationString(*req.WaitlistCheckInterval)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid waitlist_check_interval", h.logger)
			return
		}
		existing.WaitlistCheckInterval = &d
	}
	if req.SchedulerDisabled != nil {
		existing.SchedulerDisabled = *req.SchedulerDisabled
	}

	if err := h.projectStore.Update(r.Context(), existing); err != nil {
		if errors.Is(err, model.ErrProjectNotFound) {
			WriteError(w, http.StatusNotFound, "project not found", h.logger)
			return
		}
		h.logger.Error("admin update-project failed", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal server error", h.logger)
		return
	}

	h.reloadProjectCache(r.Context())

	h.logger.Info("admin project updated",
		"admin_user", AdminUserFromContext(r.Context()), "project_id", id)
	WriteJSON(w, http.StatusOK, existing, h.logger)
}

func (h *AdminHandler) reloadProjectCache(ctx context.Context) {
	if h.cacheReloader == nil {
		return
	}
	projects, err := h.projectStore.GetAll(ctx)
	if err != nil {
		h.logger.Error("failed to reload project cache", "error", err)
		return
	}
	h.cacheReloader.Reload(projects)
}

func parseDurationString(s string) (model.Duration, error) {
	d, err := time.ParseDuration(s)
	return model.Duration(d), err
}

// parseIntQuery parses a query parameter as an int, applying a default when
// missing/empty and clamping the value into [min, max].
func parseIntQuery(r *http.Request, key string, def, minV, maxV int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New(key + " must be an integer")
	}
	if n < minV {
		return 0, errors.New(key + " must be >= " + strconv.Itoa(minV))
	}
	if n > maxV {
		return 0, errors.New(key + " must be <= " + strconv.Itoa(maxV))
	}
	return n, nil
}
