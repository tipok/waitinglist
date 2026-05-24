package waitlist

import (
	"context"
	"log/slog"
	"time"

	"github.com/tipok/waitinglist/internal/config"
	"github.com/tipok/waitinglist/internal/model"
	"github.com/tipok/waitinglist/internal/notifier"
)

const schedulerKeyWaitlistLastSuccess = "waitlist_last_success"

type waitingListStore interface {
	GetWithOffsetLimit(ctx context.Context, projectSlug string, offset, limit *int) ([]model.WaitingListEntry, error)
	DeleteByIDsTx(ctx context.Context, tx model.DBTX, ids []string) error
	BeginTx(ctx context.Context) (model.Tx, error)
}

type userStore interface {
	GrantAccessTx(ctx context.Context, tx model.DBTX, ids []string, source string) error
	GetByIDs(ctx context.Context, ids []string) ([]model.UserEntity, error)
}

type schedulerStore interface {
	GetLastSuccess(ctx context.Context, projectSlug, key string) (time.Time, error)
	UpdateLastSuccess(ctx context.Context, tx model.DBTX, projectSlug, key string) error
}

func Start(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	projects []model.Project,
	waitingListRepo waitingListStore,
	userRepo userStore,
	schedulerRepo schedulerStore,
	emailNotifier notifier.Notifier,
) error {
	if cfg.SchedulerInterval.Disabled {
		logger.Info("scheduler disabled globally, skipping")
		return nil
	}

	processProject := func(p model.Project) {
		if p.SchedulerDisabled {
			return
		}

		batchSize := cfg.Waitlist.EntryBatchSize
		if p.EntryBatchSize != nil {
			batchSize = *p.EntryBatchSize
		}

		windowInterval := cfg.Waitlist.EntryWindowInterval
		if p.EntryWindowInterval != nil {
			windowInterval = time.Duration(*p.EntryWindowInterval)
		}

		lastSuccess, err := schedulerRepo.GetLastSuccess(ctx, p.Slug, schedulerKeyWaitlistLastSuccess)
		if err != nil {
			logger.Error("failed to get last success", "error", err, "project", p.Slug)
			return
		}
		if !lastSuccess.IsZero() && time.Since(lastSuccess) < windowInterval {
			return
		}

		entries, err := waitingListRepo.GetWithOffsetLimit(ctx, p.Slug, nil, &batchSize)
		if err != nil {
			logger.Error("failed to get waiting list", "error", err, "project", p.Slug)
			return
		}
		usersToAllow := make([]string, 0, len(entries))
		waitingListIDs := make([]string, 0, len(entries))
		for _, entry := range entries {
			if time.Since(entry.WeightedCreatedAt) < windowInterval {
				continue
			}
			usersToAllow = append(usersToAllow, entry.UserID)
			waitingListIDs = append(waitingListIDs, entry.ID)
		}

		if len(usersToAllow) == 0 {
			return
		}

		tx, err := waitingListRepo.BeginTx(ctx)
		if err != nil {
			logger.Error("failed to begin tx", "error", err, "project", p.Slug)
			return
		}
		defer func() { _ = tx.Rollback() }()

		err = userRepo.GrantAccessTx(ctx, tx, usersToAllow, "scheduler")
		if err != nil {
			logger.Error("failed to grant access", "error", err, "project", p.Slug)
			return
		}

		err = waitingListRepo.DeleteByIDsTx(ctx, tx, waitingListIDs)
		if err != nil {
			logger.Error("failed to delete waiting list entries", "error", err, "project", p.Slug)
			return
		}

		err = schedulerRepo.UpdateLastSuccess(ctx, tx, p.Slug, schedulerKeyWaitlistLastSuccess)
		if err != nil {
			logger.Error("failed to update last success", "error", err, "project", p.Slug)
			return
		}

		if err = tx.Commit(); err != nil {
			logger.Error("failed to commit tx", "error", err, "project", p.Slug)
			return
		}

		logger.Info("scheduler batch processed",
			"project", p.Slug, "granted", len(usersToAllow))

		if emailNotifier != nil {
			users, fetchErr := userRepo.GetByIDs(ctx, usersToAllow)
			if fetchErr != nil {
				logger.Warn("scheduler: failed to fetch users for notification", "error", fetchErr, "project", p.Slug)
			} else {
				for _, u := range users {
					emailNotifier.NotifyAccessGranted(u, p)
				}
			}
		}
	}

	checkAllProjects := func() {
		for _, p := range projects {
			processProject(p)
		}
	}

	ticker := time.NewTicker(cfg.SchedulerInterval.WaitlistCheckInterval)
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
	return nil
}
