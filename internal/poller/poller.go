package poller

import (
	"context"
	"errors"
	"time"
)

var ErrSyncAlreadyRunning = errors.New("sync already running")
var ErrSyncCompletedWithWarnings = errors.New("sync completed with warnings")

type Status struct {
	Running     bool
	LastRunAt   time.Time
	LastError   string
	LastWarning string
	LastStatus  string
	SyncRunning bool
}

func shouldLogSyncError(err error) bool {
	return err != nil && !errors.Is(err, ErrSyncCompletedWithWarnings) && !errors.Is(err, ErrSyncAlreadyRunning) && !errors.Is(err, context.Canceled)
}
