package test

import (
	"context"
	"sync"
	"testing"
	"time"

	keeperapp "cpa-usage-keeper/internal/app"
)

type metadataConnectionSyncer struct {
	mu     sync.Mutex
	calls  int
	called chan struct{}
}

func newMetadataConnectionSyncer() *metadataConnectionSyncer {
	return &metadataConnectionSyncer{called: make(chan struct{}, 8)}
}

func (s *metadataConnectionSyncer) SyncMetadata(context.Context) error {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	select {
	case s.called <- struct{}{}:
	default:
	}
	return nil
}

func (s *metadataConnectionSyncer) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func TestMetadataSyncRunnerWaitsForConnectionAndKeepsPolling(t *testing.T) {
	syncer := newMetadataConnectionSyncer()
	runner := keeperapp.NewMetadataSyncRunner(syncer, 20*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runner.Run(ctx)
	}()

	select {
	case <-syncer.called:
		t.Fatal("metadata sync ran before ingest connected")
	case <-time.After(10 * time.Millisecond):
	}

	runner.NotifyIngestConnected()
	select {
	case <-syncer.called:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for connection-triggered metadata sync")
	}
	select {
	case <-syncer.called:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for periodic metadata sync")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for metadata runner to stop")
	}
	if got := syncer.callCount(); got < 2 {
		t.Fatalf("expected connection-triggered sync plus periodic sync, got %d calls", got)
	}
}

func TestMetadataSyncRunnerIgnoresRefreshBeforeConnectionActivation(t *testing.T) {
	syncer := newMetadataConnectionSyncer()
	runner := keeperapp.NewMetadataSyncRunner(syncer, time.Hour)
	runner.RequestMetadataRefresh()
	runner.NotifyIngestConnected()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runner.Run(ctx)
	}()

	select {
	case <-syncer.called:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for connection-triggered metadata sync")
	}
	select {
	case <-syncer.called:
		t.Fatal("refresh received before activation triggered an extra metadata sync")
	case <-time.After(1100 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for metadata runner to stop")
	}
	if got := syncer.callCount(); got != 1 {
		t.Fatalf("expected only connection-triggered sync, got %d calls", got)
	}
}
