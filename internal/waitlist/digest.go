package waitlist

import (
	"context"
	"log/slog"
	"time"

	"github.com/tipok/waitinglist/internal/model"
	"github.com/tipok/waitinglist/internal/notifier"
)

const (
	schedulerKeyDigestLastSuccess = "digest_last_success"
	defaultDigestInterval         = 24 * time.Hour
	digestCheckInterval           = 1 * time.Hour
)

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

// StartDigest launches a background goroutine that periodically sends digest
// emails for projects with configured recipients.
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

	hasAnyRecipients := false
	for _, p := range projects {
		if len(p.Digest.Recipients) > 0 {
			hasAnyRecipients = true
			break
		}
	}
	if !hasAnyRecipients {
		logger.Info("digest: no projects have digestRecipients configured, digest disabled")
		return
	}

	processProject := func(p model.Project) {
		if len(p.Digest.Recipients) == 0 {
			return
		}

		interval := defaultDigestInterval
		if p.Digest.Interval != nil {
			interval = time.Duration(*p.Digest.Interval)
		}

		lastSuccess, err := schedulerRepo.GetLastSuccess(ctx, p.Slug, schedulerKeyDigestLastSuccess)
		if err != nil {
			logger.Warn("digest: failed to get last success", "error", err, "project", p.Slug)
			return
		}
		if !lastSuccess.IsZero() && time.Since(lastSuccess) < interval {
			return
		}

		since := lastSuccess
		if since.IsZero() {
			since = time.Now().Add(-interval)
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

	checkAllProjects := func() {
		for _, p := range projects {
			processProject(p)
		}
	}

	ticker := time.NewTicker(digestCheckInterval)
	go func() {
		immediately := make(chan struct{}, 1)
		immediately <- struct{}{}
		for {
			select {
			case <-immediately:
				checkAllProjects()
			case <-ticker.C:
				checkAllProjects()
			case <-ctx.Done():
				ticker.Stop()
				return
			}
		}
	}()
}
