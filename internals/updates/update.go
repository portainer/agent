package updates

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
)

type GhostUpdaterCleaner interface {
	Clean(ctx context.Context) error
	UpdateID() int
}

func Remove(ctx context.Context, cleaner GhostUpdaterCleaner) error {
	// Make three attempts to allow the updater container to exit on its own.
	// This is important because if the updater container is forcibly removed
	// before it exits naturally, it may skip removing the previous agent's
	// container by the updater container, resulting in a container name conflict
	// during the next remote update.

	log.Debug().Msg("start to clean updater")

	if err := retry(ctx, 3, 10*time.Second, cleaner.Clean); err != nil {
		log.Warn().Int("Update ID", cleaner.UpdateID()).Err(err).Msg("unable to clean up ghost updater stack")

		return err
	}

	log.Debug().Msg("finish to clean updater")

	return nil
}

// retry executes the given function f up to maxRetries times with a delay of delayBetweenRetries
func retry(ctx context.Context, maxRetries int, delayBetweenRetries time.Duration, f func(ctx context.Context) error) error {
	var err error

	for range maxRetries {
		err = f(ctx)
		if err == nil {
			return nil
		}

		time.Sleep(delayBetweenRetries)
	}

	return err
}
