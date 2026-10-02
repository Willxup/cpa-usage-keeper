package test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	keeperapp "cpa-usage-keeper/internal/app"
)

type bridgeInboxWriter func(context.Context, string, []string, time.Time) (int, error)

func (write bridgeInboxWriter) Insert(ctx context.Context, source string, messages []string, at time.Time) (int, error) {
	return write(ctx, source, messages, at)
}

type bridgeControlObserver struct {
	connected atomic.Int32
	refresh   atomic.Int32
}

func (o *bridgeControlObserver) NotifyIngestConnected()            { o.connected.Add(1) }
func (o *bridgeControlObserver) MarkRefreshSupported()             { o.refresh.Add(1) }
func (o *bridgeControlObserver) RequestMetadataRefresh()           { o.refresh.Add(1) }
func (o *bridgeControlObserver) MarkRefreshPollingRequired(string) { o.refresh.Add(1) }

// TestUsageIngestBridgeTransfersOnlyAfterAcceptedWrite 验证同一 runner 移交时已取出的旧批先落盘，新批才走正常写列。
func TestUsageIngestBridgeTransfersOnlyAfterAcceptedWrite(t *testing.T) {
	oldStarted := make(chan struct{})
	releaseOld := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseOld) }) })
	oldDone := make(chan error, 1)
	oldWrites, newWrites := atomic.Int32{}, atomic.Int32{}
	bridge := keeperapp.NewUsageIngestBridge(bridgeInboxWriter(func(_ context.Context, _ string, messages []string, _ time.Time) (int, error) {
		close(oldStarted)
		<-releaseOld
		oldWrites.Add(int32(len(messages)))
		return len(messages), nil
	}))
	go func() {
		_, err := bridge.Insert(context.Background(), "http_pull", []string{`{"request_id":"already-pulled"}`}, time.Now())
		oldDone <- err
	}()
	awaitAppSignal(t, oldStarted, "old inbox write did not begin")
	observer := &bridgeControlObserver{}
	bridge.MarkRefreshSupported() // 升级中的控制消息不会访问业务 metadata。
	switchStarted := make(chan struct{})
	switchDone := make(chan error, 1)
	go func() {
		close(switchStarted)
		switchDone <- bridge.ActivateNormalWriter(bridgeInboxWriter(func(_ context.Context, _ string, messages []string, _ time.Time) (int, error) {
			newWrites.Add(int32(len(messages)))
			return len(messages), nil
		}), observer)
	}()
	awaitAppSignal(t, switchStarted, "ingest transfer did not begin")
	select {
	case err := <-switchDone:
		t.Fatalf("ingest writer switched before an accepted batch committed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(releaseOld) })
	if err := awaitAppError(t, oldDone, "old accepted batch did not finish"); err != nil {
		t.Fatal(err)
	}
	if err := awaitAppError(t, switchDone, "normal writer did not activate"); err != nil {
		t.Fatal(err)
	}
	if _, err := bridge.Insert(context.Background(), "http_pull", []string{`{"request_id":"new"}`}, time.Now()); err != nil {
		t.Fatal(err)
	}
	bridge.RequestMetadataRefresh()
	if oldWrites.Load() != 1 || newWrites.Load() != 1 || observer.connected.Load() != 1 || observer.refresh.Load() != 1 {
		t.Fatalf("transfer lost writes or metadata boundary: old=%d new=%d connected=%d refresh=%d", oldWrites.Load(), newWrites.Load(), observer.connected.Load(), observer.refresh.Load())
	}
}

func awaitAppSignal(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal(message)
	}
}

func awaitAppError(t *testing.T, result <-chan error, message string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(time.Second):
		t.Fatal(message)
		return nil
	}
}
