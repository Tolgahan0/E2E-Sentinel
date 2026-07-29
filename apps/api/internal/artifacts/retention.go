package artifacts

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

// DefaultRetentionSweepInterval bounds how often RunRetentionLoop checks
// for expired artifacts.
const DefaultRetentionSweepInterval = 1 * time.Hour

// RunRetentionLoop periodically deletes expired artifacts (spec §9
// "Retention jobs") until ctx is cancelled. It's meant to run in its own
// goroutine for the lifetime of the process; a failed sweep is logged
// and retried on the next tick rather than stopping the loop.
func RunRetentionLoop(ctx context.Context, store Store, interval time.Duration, logger zerolog.Logger) {
	if interval <= 0 {
		interval = DefaultRetentionSweepInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			deleted, err := store.DeleteExpired(ctx, time.Now())
			if err != nil {
				logger.Warn().Err(err).Msg("artifact retention sweep failed")
				continue
			}
			if deleted > 0 {
				logger.Info().Int("deleted", deleted).Msg("artifact retention sweep removed expired artifacts")
			}
		}
	}
}
