package app

import (
	"context"
	"fmt"

	"cpa-usage-keeper/internal/poller"
)

type manualSyncRunner struct {
	redis    Runner
	metadata MetadataSyncer
}

type manualSyncStageError struct {
	message string
	err     error
}

func (e manualSyncStageError) Error() string {
	return fmt.Sprintf("%s: %v", e.message, e.err)
}

func (e manualSyncStageError) Unwrap() error {
	return e.err
}

func (e manualSyncStageError) UserMessage() string {
	return e.message
}

func newManualSyncRunner(redis Runner, metadata MetadataSyncer) *manualSyncRunner {
	return &manualSyncRunner{redis: redis, metadata: metadata}
}

func (r *manualSyncRunner) Status() poller.Status {
	if r == nil || r.redis == nil {
		return poller.Status{}
	}
	return r.redis.Status()
}

func (r *manualSyncRunner) SyncNow(ctx context.Context) error {
	if r == nil {
		return fmt.Errorf("manual sync runner is nil")
	}
	if r.redis == nil {
		return fmt.Errorf("manual redis syncer is nil")
	}
	if err := r.redis.SyncNow(ctx); err != nil {
		return manualSyncStageError{message: "redis sync failed", err: err}
	}
	if r.metadata == nil {
		return fmt.Errorf("manual metadata syncer is nil")
	}
	if err := r.metadata.SyncMetadata(ctx); err != nil {
		return manualSyncStageError{message: "metadata sync failed", err: err}
	}
	return nil
}
