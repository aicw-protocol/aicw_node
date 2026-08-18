package orchestrator

import (
	"context"
	"time"

	"github.com/fystack/mpcium/pkg/logger"
)

// StartInflightSweeper runs a background loop that deletes stale inflight locks
// when cfg.SweepEnabled is true. No-op when disabled (default).
func StartInflightSweeper(ctx context.Context, lock *LockManager, cfg Config) {
	if !cfg.SweepEnabled {
		return
	}

	maxAge := 2*cfg.ReshareResultTimeout + 5*time.Minute
	interval := cfg.SweepInterval
	if interval <= 0 {
		interval = 300 * time.Second
	}

	logger.Info("Inflight stale sweeper enabled",
		"interval", interval, "max_age", maxAge)

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				n, err := lock.SweepStale(maxAge)
				if err != nil {
					logger.Error("Inflight sweeper failed", err)
				} else if n > 0 {
					logger.Info("Inflight sweeper released stale locks", "count", n)
				}
			}
		}
	}()
}
