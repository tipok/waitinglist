package handler

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"sync"

	"github.com/tipok/waitinglist/internal/model"
)

const (
	// +10 to avoid collision with ctxKeyAdminUser defined in middleware.go
	ctxKeyProject ctxKey = iota + 10
)

// ProjectFromContext returns the project stashed by the tenant middleware, or
// nil if none is present.
func ProjectFromContext(ctx context.Context) *model.Project {
	v, _ := ctx.Value(ctxKeyProject).(*model.Project)
	return v
}

// ProjectResolver resolves incoming requests to a project using header or Host
// mapping. It maintains an in-memory cache of projects keyed by slug.
type ProjectResolver struct {
	headerName  string
	defaultSlug string
	hostMapping map[string]string

	mu       sync.RWMutex
	projects map[string]*model.Project
	logger   *slog.Logger
}

// NewProjectResolver creates a ProjectResolver with the given configuration and
// initial project set.
func NewProjectResolver(headerName, defaultSlug string, hostMapping map[string]string, projects []model.Project, logger *slog.Logger) *ProjectResolver {
	pr := &ProjectResolver{
		headerName:  headerName,
		defaultSlug: defaultSlug,
		hostMapping: hostMapping,
		projects:    make(map[string]*model.Project, len(projects)),
		logger:      logger,
	}
	for i := range projects {
		pr.projects[projects[i].Slug] = &projects[i]
	}
	return pr
}

// Reload replaces the project cache with a fresh set of projects.
func (pr *ProjectResolver) Reload(projects []model.Project) {
	m := make(map[string]*model.Project, len(projects))
	for i := range projects {
		m[projects[i].Slug] = &projects[i]
	}
	pr.mu.Lock()
	pr.projects = m
	pr.mu.Unlock()
}

// Middleware returns an HTTP middleware that resolves the project for the
// request and stores it in the context.
func (pr *ProjectResolver) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slug := r.Header.Get(pr.headerName)

		if slug == "" {
			// hostMapping is immutable after construction; no lock needed.
			originalHost := r.Header.Get("X-Forwarded-Host")
			if originalHost == "" {
				originalHost = r.Host // fallback if no proxy
			}

			host := stripPort(originalHost)
			if mapped, ok := pr.hostMapping[host]; ok {
				slug = mapped
			}
		}

		if slug == "" {
			slug = pr.defaultSlug
		}

		if slug == "" {
			WriteError(w, http.StatusBadRequest, "project identification required", pr.logger)
			return
		}

		pr.mu.RLock()
		project, ok := pr.projects[slug]
		pr.mu.RUnlock()

		if !ok {
			WriteError(w, http.StatusBadRequest, "unknown project: "+slug, pr.logger)
			return
		}

		ctx := context.WithValue(r.Context(), ctxKeyProject, project)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func stripPort(hostPort string) string {
	host, _, err := net.SplitHostPort(hostPort)
	if err != nil {
		return hostPort
	}
	return host
}
