package app

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"cpa-usage-keeper/internal/poller"
)

type manualRedisSyncStub struct {
	status poller.Status
	err    error
	calls  int
	order  *[]string
}

func (s *manualRedisSyncStub) Run(context.Context) error {
	return nil
}

func (s *manualRedisSyncStub) Status() poller.Status {
	return s.status
}

func (s *manualRedisSyncStub) SyncNow(context.Context) error {
	s.calls++
	if s.order != nil {
		*s.order = append(*s.order, "redis")
	}
	return s.err
}

type manualMetadataSyncStub struct {
	err   error
	calls int
	order *[]string
}

func (s *manualMetadataSyncStub) SyncMetadata(context.Context) error {
	s.calls++
	if s.order != nil {
		*s.order = append(*s.order, "metadata")
	}
	return s.err
}

func TestManualSyncRunnerRunsRedisThenMetadata(t *testing.T) {
	var order []string
	redis := &manualRedisSyncStub{status: poller.Status{LastStatus: "completed"}, order: &order}
	metadata := &manualMetadataSyncStub{order: &order}
	runner := newManualSyncRunner(redis, metadata)

	if err := runner.SyncNow(context.Background()); err != nil {
		t.Fatalf("SyncNow returned error: %v", err)
	}
	if !reflect.DeepEqual(order, []string{"redis", "metadata"}) {
		t.Fatalf("expected redis then metadata sync, got %v", order)
	}
	if runner.Status().LastStatus != "completed" {
		t.Fatalf("expected status to delegate to redis runner, got %+v", runner.Status())
	}
}

func TestManualSyncRunnerReturnsRedisErrorWithoutMetadata(t *testing.T) {
	redisErr := errors.New("redis failed")
	redis := &manualRedisSyncStub{err: redisErr}
	metadata := &manualMetadataSyncStub{}
	runner := newManualSyncRunner(redis, metadata)

	err := runner.SyncNow(context.Background())
	if !errors.Is(err, redisErr) {
		t.Fatalf("expected redis error, got %v", err)
	}
	if err == nil || err.Error() != "redis sync failed: redis failed" {
		t.Fatalf("expected redis-specific sync error, got %v", err)
	}
	if metadata.calls != 0 {
		t.Fatalf("expected metadata sync not to run after redis failure, got %d calls", metadata.calls)
	}
}

func TestManualSyncRunnerReturnsMetadataError(t *testing.T) {
	metadataErr := errors.New("metadata failed")
	redis := &manualRedisSyncStub{}
	metadata := &manualMetadataSyncStub{err: metadataErr}
	runner := newManualSyncRunner(redis, metadata)

	err := runner.SyncNow(context.Background())
	if !errors.Is(err, metadataErr) {
		t.Fatalf("expected metadata error, got %v", err)
	}
	if err == nil || err.Error() != "metadata sync failed: metadata failed" {
		t.Fatalf("expected metadata-specific sync error, got %v", err)
	}
	if redis.calls != 1 || metadata.calls != 1 {
		t.Fatalf("expected redis and metadata to run once, got redis=%d metadata=%d", redis.calls, metadata.calls)
	}
}
