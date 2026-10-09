package test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"cpa-usage-keeper/internal/app"
)

func TestLocalMetadataRefreshPreservesSchedulingMode(t *testing.T) {
	for _, notificationMode := range []bool{false, true} {
		name := "polling"
		if notificationMode {
			name = "notification"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				syncer := &metadataSyncStub{}
				runner := app.NewMetadataSyncRunner(syncer, 10*time.Second)
				if notificationMode {
					runner.MarkRefreshSupported()
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				runner.NotifyIngestConnected()
				go func() {
					if err := runner.Run(ctx); err != nil {
						t.Errorf("Run: %v", err)
					}
				}()
				synctest.Wait()
				if got := syncer.CallCount(); got != 1 {
					t.Fatalf("connection syncs = %d, want 1", got)
				}
				// 连续本地操作共用 trailing debounce，但不能改变原同步模式。
				for range 3 {
					runner.RequestLocalMetadataRefresh()
					time.Sleep(200 * time.Millisecond)
				}
				time.Sleep(time.Second)
				synctest.Wait()
				if got := syncer.CallCount(); got != 2 {
					t.Fatalf("syncs after local burst = %d, want 2", got)
				}
				time.Sleep(10 * time.Second)
				synctest.Wait()
				want := 3
				if notificationMode {
					want = 2
				}
				if got := syncer.CallCount(); got != want {
					t.Fatalf("syncs after periodic tick = %d, want %d", got, want)
				}
			})
		})
	}
}

func TestLocalMetadataRefreshBeforeConnectionPreservesPolling(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		syncer := &metadataSyncStub{}
		runner := app.NewMetadataSyncRunner(syncer, 3*time.Second)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		runner.RequestLocalMetadataRefresh()
		go func() {
			if err := runner.Run(ctx); err != nil {
				t.Errorf("Run: %v", err)
			}
		}()
		time.Sleep(4 * time.Second)
		synctest.Wait()
		if got := syncer.CallCount(); got != 0 {
			t.Fatalf("syncs before connection = %d, want 0", got)
		}
		runner.NotifyIngestConnected()
		synctest.Wait()
		time.Sleep(4 * time.Second)
		synctest.Wait()
		if got := syncer.CallCount(); got != 2 {
			t.Fatalf("connection and periodic syncs = %d, want 2", got)
		}
	})
}
