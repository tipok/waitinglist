package waitlist

import (
	"context"
	"time"

	"github.com/tipok/waitinglist/internal/config"
	lg "github.com/tipok/waitinglist/internal/logger"
	"github.com/tipok/waitinglist/internal/model"
	"github.com/tipok/waitinglist/internal/repository"
)

const schedulerKeyWaitlistLastSuccess = "waitlist_last_success"

func Start(
	ctx context.Context,
	cfg *config.Config,
	projectRepo *repository.ProjectRepository,
	waitingListRepo *repository.WaitingListRepository,
	userRepo *repository.UserRepository,
	schedulerRepo *repository.SchedulerRepository,
) error {
	logger := lg.NewLogger()

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
			windowInterval = *p.EntryWindowInterval
		}

		lastSuccess, err := schedulerRepo.GetLastSuccess(ctx, p.ID, schedulerKeyWaitlistLastSuccess)
		if err != nil {
			logger.Error("failed to get last success", "error", err, "project", p.Slug)
			return
		}
		if !lastSuccess.IsZero() && time.Since(lastSuccess) < windowInterval {
			return
		}

		entries, err := waitingListRepo.GetWithOffsetLimit(ctx, p.ID, nil, &batchSize)
		if err != nil {
			logger.Error("failed to get waiting list", "error", err, "project", p.Slug)
			return
		}
		usersToAllow := make([]string, 0, len(entries))
		waitingListIds := make([]string, 0, len(entries))
		for _, entry := range entries {
			if time.Since(entry.WeightedCreatedAt) < windowInterval {
				continue
			}
			usersToAllow = append(usersToAllow, entry.UserID)
			waitingListIds = append(waitingListIds, entry.ID)
		}

		if len(usersToAllow) == 0 {
			return
		}

		err = userRepo.GrantAccess(ctx, usersToAllow, "scheduler")
		if err != nil {
			logger.Error("failed to grant access", "error", err, "project", p.Slug)
			return
		}

		err = waitingListRepo.DeleteByIDs(ctx, waitingListIds)
		if err != nil {
			logger.Error("failed to delete waiting list entries", "error", err, "project", p.Slug)
			return
		}

		err = schedulerRepo.UpdateLastSuccess(ctx, schedulerRepo.DB(), p.ID, schedulerKeyWaitlistLastSuccess)
		if err != nil {
			logger.Error("failed to update last success", "error", err, "project", p.Slug)
		}

		logger.Info("scheduler batch processed",
			"project", p.Slug, "granted", len(usersToAllow))
	}

	checkAllProjects := func() {
		projects, err := projectRepo.GetAll(ctx)
		if err != nil {
			logger.Error("scheduler: failed to load projects", "error", err)
			return
		}
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
