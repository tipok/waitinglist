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

	ticker := time.NewTicker(cfg.SchedulerInterval.WaitlistCheckInterval)
	go func() {
		for {
			select {
			case <-ticker.C:
				entries, err := waitingListRepo.GetWithOffsetLimit(ctx, nil, nil)
				if err != nil {
					logger.Error("failed to get waiting list", "error", err)
					continue
				}
				usersToAllow := make([]string, 0, len(entries))
				waitingListIds := make([]string, 0, len(entries))
				for _, entry := range entries {
					usersToAllow = append(usersToAllow, entry.UserID)
					waitingListIds = append(waitingListIds, entry.ID)
				}
				err = userRepo.SetHasAccess(ctx, usersToAllow)
				if err != nil {
					logger.Error("failed to set has_access", "error", err)
					continue
				}

				err = waitingListRepo.DeleteByIDs(ctx, waitingListIds)
				if err != nil {
					logger.Error("failed to delete waiting list entries", "error", err)
					continue
				}
			case <-ctx.Done():
				ticker.Stop()
				return
			}
		}
	}()
	return nil
}
