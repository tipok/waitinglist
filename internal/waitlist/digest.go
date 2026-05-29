package waitlist

import (
	"context"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/tipok/waitinglist/internal/model"
	"github.com/tipok/waitinglist/internal/notifier"
)

const schedulerKeyDigestLastSuccess = "digest_last_success"

type digestUserStore interface {
	GetGrantedSince(ctx context.Context, projectSlug string, since time.Time) ([]model.UserEntity, error)
}

type digestWaitlistStore interface {
	GetEnlistedSince(ctx context.Context, projectSlug string, since time.Time) ([]model.WaitingListAdminRow, error)
}

type digestSchedulerStore interface {
	GetLastSuccess(ctx context.Context, projectSlug, key string) (time.Time, error)
	UpdateLastSuccess(ctx context.Context, tx model.DBTX, projectSlug, key string) error
}

// DigestSender abstracts the SendDigest call for testing.
type DigestSender interface {
	SendDigest(recipients []string, from, subject string, data notifier.DigestData) error
}

// StartDigest launches a cron-based scheduler that sends digest emails for
// projects with configured recipients and a valid cron schedule.
func StartDigest(
	ctx context.Context,
	logger *slog.Logger,
	projects []model.Project,
	userRepo digestUserStore,
	waitlistRepo digestWaitlistStore,
	schedulerRepo digestSchedulerStore,
	db model.DBTX,
	sender DigestSender,
) {
	if sender == nil {
		logger.Info("digest: smtp not configured, digest disabled")
		return
	}

	c := cron.New()
	jobCount := 0

	for _, p := range projects {
		if len(p.Digest.Recipients) == 0 || p.Digest.Schedule == "" {
			continue
		}

		proj := p
		_, err := c.AddFunc(proj.Digest.Schedule, func() {
			processDigestProject(ctx, logger, proj, userRepo, waitlistRepo, schedulerRepo, db, sender)
		})
		if err != nil {
			logger.Warn("digest: failed to register cron job", "error", err, "project", proj.Slug, "schedule", proj.Digest.Schedule)
			continue
		}
		jobCount++
		logger.Info("digest: registered", "project", proj.Slug, "schedule", proj.Digest.Schedule)
	}

	if jobCount == 0 {
		logger.Info("digest: no projects have valid digest schedule configured, digest disabled")
		return
	}

	c.Start()

	go func() {
		<-ctx.Done()
		stopCtx := c.Stop()
		<-stopCtx.Done()
		logger.Info("digest: cron scheduler stopped")
	}()
}

func processDigestProject(
	ctx context.Context,
	logger *slog.Logger,
	p model.Project,
	userRepo digestUserStore,
	waitlistRepo digestWaitlistStore,
	schedulerRepo digestSchedulerStore,
	db model.DBTX,
	sender DigestSender,
) {
	lastSuccess, err := schedulerRepo.GetLastSuccess(ctx, p.Slug, schedulerKeyDigestLastSuccess)
	if err != nil {
		logger.Warn("digest: failed to get last success", "error", err, "project", p.Slug)
		return
	}

	since := lastSuccess
	if since.IsZero() {
		since = time.Now().Add(-24 * time.Hour)
	}
	now := time.Now()

	enlisted, err := waitlistRepo.GetEnlistedSince(ctx, p.Slug, since)
	if err != nil {
		logger.Warn("digest: failed to query enlisted", "error", err, "project", p.Slug)
		return
	}

	granted, err := userRepo.GetGrantedSince(ctx, p.Slug, since)
	if err != nil {
		logger.Warn("digest: failed to query granted", "error", err, "project", p.Slug)
		return
	}

	if len(enlisted) == 0 && len(granted) == 0 {
		logger.Info("digest: no activity, skipping", "project", p.Slug, "since", since)
		return
	}

	newEnlisted := make([]notifier.EnlistedEntry, 0, len(enlisted))
	for _, e := range enlisted {
		newEnlisted = append(newEnlisted, notifier.EnlistedEntry{
			Firstname: e.Firstname,
			Lastname:  e.Lastname,
			Email:     e.Email,
			JoinedAt:  notifier.FormatDigestTime(e.CreatedAt),
		})
	}

	newGranted := make([]notifier.GrantedEntry, 0, len(granted))
	for _, u := range granted {
		grantedBy := ""
		if u.AccessGrantedBy != nil {
			grantedBy = *u.AccessGrantedBy
		}
		grantedAt := ""
		if u.AccessGrantedAt != nil {
			grantedAt = notifier.FormatDigestTime(*u.AccessGrantedAt)
		}
		newGranted = append(newGranted, notifier.GrantedEntry{
			Firstname: u.Firstname,
			Lastname:  u.Lastname,
			Email:     u.Email,
			GrantedAt: grantedAt,
			GrantedBy: grantedBy,
		})
	}

	from := p.Digest.From
	if from == "" {
		from = p.Email.From
	}
	subject := p.Digest.Subject
	if subject == "" {
		subject = p.Name + " — Activity Digest"
	}

	data := notifier.DigestData{
		ProjectName:   p.Name,
		PeriodStart:   notifier.FormatDigestTime(since),
		PeriodEnd:     notifier.FormatDigestTime(now),
		NewEnlisted:   newEnlisted,
		NewGranted:    newGranted,
		EnlistedCount: len(newEnlisted),
		GrantedCount:  len(newGranted),
	}

	if err := sender.SendDigest(p.Digest.Recipients, from, subject, data); err != nil {
		logger.Warn("digest: failed to send digest", "error", err, "project", p.Slug)
		return
	}

	if err := schedulerRepo.UpdateLastSuccess(ctx, db, p.Slug, schedulerKeyDigestLastSuccess); err != nil {
		logger.Warn("digest: failed to update scheduler state", "error", err, "project", p.Slug)
		return
	}

	logger.Info("digest: sent",
		"project", p.Slug,
		"enlisted", len(newEnlisted),
		"granted", len(newGranted),
		"recipients", len(p.Digest.Recipients))
}
