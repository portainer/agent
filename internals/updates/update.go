package updates

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
)

type GhostUpdaterCleaner interface {
	Clean(ctx context.Context) error
}

func Remove(ctx context.Context, updateID int, fn GhostUpdaterCleaner) error {
	// Make three attempts to allow the updater container to exit on its own.
	// This is important because if the updater container is forcibly removed
	// before it exits naturally, it may skip removing the previous agent's
	// container by the updater container, resulting in a container name conflict
	// during the next remote update.

	err := retry(ctx, 3, 30*time.Second, fn.Clean)
	if err != nil {
		log.Warn().Int("Update ID", updateID).Err(err).Msg("unable to clean up ghost updater stack")
		return err
	}
	return nil
}

// retry executes the given function f up to maxRetries times with a delay of delayBetweenRetries
func retry(ctx context.Context, maxRetries int, delayBetweenRetries time.Duration, f func(ctx context.Context) error) error {
	var err error
	for i := 0; i < maxRetries; i++ {
		err = f(ctx)
		if err == nil {
			return nil
		}
		time.Sleep(delayBetweenRetries)
	}
	return err
}
