package waitlist

import (
	"context"
	"time"

	"github.com/tipok/waitinglist/internal/config"
	lg "github.com/tipok/waitinglist/internal/logger"
	"github.com/tipok/waitinglist/internal/repository"
)

func Start(
	ctx context.Context,
	cfg *config.Config,
	waitingListRepo *repository.WaitingListRepository,
	userRepo *repository.UserRepository,
) error {
	logger := lg.NewLogger()

	checkEntries := func() {
		entries, err := waitingListRepo.GetWithOffsetLimit(ctx, nil, &cfg.Waitlist.EntryBatchSize)
		if err != nil {
			logger.Error("failed to get waiting list", "error", err)
			return
		}
		usersToAllow := make([]string, 0, len(entries))
		waitingListIds := make([]string, 0, len(entries))
		for _, entry := range entries {
			if time.Since(entry.WeightedCreatedAt) < cfg.Waitlist.EntryWindowInterval {
				continue
			}
			usersToAllow = append(usersToAllow, entry.UserID)
			waitingListIds = append(waitingListIds, entry.ID)
		}

		if len(usersToAllow) == 0 {
			return
		}

		err = userRepo.SetHasAccess(ctx, usersToAllow)
		if err != nil {
			logger.Error("failed to set has_access", "error", err)
			return
		}

		err = waitingListRepo.DeleteByIDs(ctx, waitingListIds)
		if err != nil {
			logger.Error("failed to delete waiting list entries", "error", err)
			return
		}
	}

	ticker := time.NewTicker(cfg.SchedulerInterval.WaitlistCheckInterval)
	go func() {
		immediately := make(chan struct{}, 1)
		immediately <- struct{}{}
		for {
			select {
			case <-immediately:
				checkEntries()
			case <-ticker.C:
				checkEntries()
			case <-ctx.Done():
				ticker.Stop()
				return
			}
		}
	}()
	return nil
}
