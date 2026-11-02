package poller_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

func pluginRunnerIdentity(key string) entities.PluginAPIKeyIdentity {
	return entities.PluginAPIKeyIdentity{APIGroupKey: helper.PluginAPIKeyGroupKey("plugin:test:auth", key), AccessProvider: "plugin:test:auth"}
}

func startPluginKeyRunner(t *testing.T, runner *poller.PluginAPIKeyRunner) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(time.Second):
			t.Error("plugin runner did not stop")
		}
	})
}

func requirePluginKeyCount(t *testing.T, db *gorm.DB, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&entities.CPAAPIKey{}).Count(&count).Error; err != nil || count != want {
		t.Fatalf("registered key count = %d, want %d: %v", count, want, err)
	}
}

func waitPluginKeys(t *testing.T, runner *poller.PluginAPIKeyRunner, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if count, _ := runner.PendingStats(); count == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	count, _ := runner.PendingStats()
	t.Fatalf("pending identities = %d, want %d", count, want)
}

func TestPluginAPIKeyRunnerUsesFixedWindowAndRemainsDatabaseSilentWhenIdle(t *testing.T) {
	db := openUsageAggregationRunnerDatabase(t)
	var queries atomic.Int64
	var eventScans atomic.Int64
	if err := db.Callback().Query().Before("gorm:query").Register("test:plugin-query-counter", func(tx *gorm.DB) {
		queries.Add(1)
		if tx.Statement.Table == "usage_events" || tx.Statement.Table == "usage_events_archive" {
			eventScans.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	runner := poller.NewPluginAPIKeyRunnerWithOptions(db, poller.PluginAPIKeyRunnerOptions{DebounceInterval: 300 * time.Millisecond})
	startPluginKeyRunner(t, runner)
	time.Sleep(40 * time.Millisecond)
	if queries.Load() != 0 {
		t.Fatal("startup queried database without notifications")
	}
	runner.NotifyPluginAPIKeys([]entities.PluginAPIKeyIdentity{pluginRunnerIdentity("first")})
	time.Sleep(220 * time.Millisecond)
	runner.NotifyPluginAPIKeys([]entities.PluginAPIKeyIdentity{pluginRunnerIdentity("first"), pluginRunnerIdentity("second")})
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if count, _ := runner.PendingStats(); count == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if count, _ := runner.PendingStats(); count != 0 {
		t.Fatal("later notification reset the fixed window")
	}
	if eventScans.Load() != 0 {
		t.Fatal("registration scanned usage events")
	}
	before := queries.Load()
	time.Sleep(60 * time.Millisecond)
	if queries.Load() != before {
		t.Fatal("idle worker polled database")
	}
}

func TestPluginAPIKeyRunnerRetriesErrorsAndYieldsToUsageInbox(t *testing.T) {
	db := openUsageAggregationRunnerDatabase(t)
	inbox := entities.RedisUsageInbox{Source: "test", RawMessage: "{}", Status: repository.RedisUsageInboxStatusPending, PoppedAt: time.Now()}
	if err := db.Create(&inbox).Error; err != nil {
		t.Fatal(err)
	}
	var failOnce atomic.Bool
	failed := make(chan struct{}, 1)
	if err := db.Callback().Create().Before("gorm:create").Register("test:fail-plugin-registration-once", func(tx *gorm.DB) {
		if tx.Statement.Table == "cpa_api_keys" && failOnce.CompareAndSwap(false, true) {
			tx.AddError(errors.New("injected transient plugin error"))
			failed <- struct{}{}
		}
	}); err != nil {
		t.Fatal(err)
	}
	runner := poller.NewPluginAPIKeyRunnerWithOptions(db, poller.PluginAPIKeyRunnerOptions{DebounceInterval: 10 * time.Millisecond})
	startPluginKeyRunner(t, runner)
	runner.NotifyPluginAPIKeys([]entities.PluginAPIKeyIdentity{pluginRunnerIdentity("pending")})
	time.Sleep(50 * time.Millisecond)
	if failOnce.Load() {
		t.Fatal("registration wrote while usage inbox was pending")
	}
	if count, _ := runner.PendingStats(); count != 1 {
		t.Fatal("deferred identity was lost")
	}
	if err := db.Model(&entities.RedisUsageInbox{}).Where("id = ?", inbox.ID).Update("status", repository.RedisUsageInboxStatusProcessed).Error; err != nil {
		t.Fatal(err)
	}
	select {
	case <-failed:
	case <-time.After(time.Second):
		t.Fatal("registration did not resume after inbox processing")
	}
	if count, _ := runner.PendingStats(); count != 1 {
		t.Fatal("failed registration was forgotten")
	}
	waitPluginKeys(t, runner, 0)
	requirePluginKeyCount(t, db, 1)
}

func TestPluginAPIKeyRunnerBoundsPendingCountBytesAndInFlightIdentities(t *testing.T) {
	db := openUsageAggregationRunnerDatabase(t)
	first := pluginRunnerIdentity("first")
	size := len(first.APIGroupKey) + len(first.AccessProvider)
	bounded := poller.NewPluginAPIKeyRunnerWithOptions(db, poller.PluginAPIKeyRunnerOptions{MaxPendingIdentities: 2, MaxPendingBytes: size})
	bounded.NotifyPluginAPIKeys([]entities.PluginAPIKeyIdentity{first, first, pluginRunnerIdentity("second"), pluginRunnerIdentity(strings.Repeat("oversized", 100))})
	if count, bytes := bounded.PendingStats(); count != 1 || bytes != size {
		t.Fatalf("byte bound or dedup failed: count=%d bytes=%d", count, bytes)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var blockOnce atomic.Bool
	if err := db.Callback().Create().Before("gorm:create").Register("test:block-plugin-registration", func(tx *gorm.DB) {
		if tx.Statement.Table == "cpa_api_keys" && blockOnce.CompareAndSwap(false, true) {
			close(started)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	runner := poller.NewPluginAPIKeyRunnerWithOptions(db, poller.PluginAPIKeyRunnerOptions{DebounceInterval: 10 * time.Millisecond, MaxPendingIdentities: 2})
	startPluginKeyRunner(t, runner)
	runner.NotifyPluginAPIKeys([]entities.PluginAPIKeyIdentity{first})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("registration did not start")
	}
	runner.NotifyPluginAPIKeys([]entities.PluginAPIKeyIdentity{pluginRunnerIdentity("second"), pluginRunnerIdentity("third")})
	count, _ := runner.PendingStats()
	close(release)
	if count != 2 {
		t.Fatalf("in-flight identity did not count toward memory cap: %d", count)
	}
	waitPluginKeys(t, runner, 0)
	var stored []entities.CPAAPIKey
	if err := db.Order("id").Find(&stored).Error; err != nil || len(stored) != 2 {
		t.Fatalf("new pending identity was lost: %+v %v", stored, err)
	}
	// 只拒收超限通知；内存腾出后，同一身份的后续用量可再次登记。
	runner.NotifyPluginAPIKeys([]entities.PluginAPIKeyIdentity{pluginRunnerIdentity("third")})
	waitPluginKeys(t, runner, 0)
	requirePluginKeyCount(t, db, 3)
}

func TestPluginAPIKeyRunnerBacksOffWhenInboxCheckFails(t *testing.T) {
	db := openUsageAggregationRunnerDatabase(t)
	var failCheck atomic.Bool
	failCheck.Store(true)
	var checks atomic.Int64
	if err := db.Callback().Row().Before("gorm:row").Register("test:fail-plugin-inbox-check", func(tx *gorm.DB) {
		if tx.Statement.Table == "redis_usage_inboxes" {
			checks.Add(1)
			if failCheck.Load() {
				tx.AddError(errors.New("injected inbox query error"))
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	runner := poller.NewPluginAPIKeyRunnerWithOptions(db, poller.PluginAPIKeyRunnerOptions{DebounceInterval: 10 * time.Millisecond})
	startPluginKeyRunner(t, runner)
	runner.NotifyPluginAPIKeys([]entities.PluginAPIKeyIdentity{pluginRunnerIdentity("query-failed")})
	time.Sleep(100 * time.Millisecond)
	if count, _ := runner.PendingStats(); count != 1 || checks.Load() != 1 {
		t.Fatalf("inbox error lost identity or busy-looped: count=%d checks=%d", count, checks.Load())
	}
	requirePluginKeyCount(t, db, 0)
	failCheck.Store(false)
	waitPluginKeys(t, runner, 0)
}

func TestPluginAPIKeyRunnerHasNoRestartRecoveryAndCancelsWaiting(t *testing.T) {
	db := openUsageAggregationRunnerDatabase(t)
	// 旧实例尚未进入登记就退出；新实例只会在后续用量再次通知时登记。
	old := poller.NewPluginAPIKeyRunner(db)
	old.NotifyPluginAPIKeys([]entities.PluginAPIKeyIdentity{pluginRunnerIdentity("after-restart")})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- old.Run(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("debounce did not cancel")
	}
	for i := 0; i < 2; i++ {
		runner := poller.NewPluginAPIKeyRunnerWithOptions(db, poller.PluginAPIKeyRunnerOptions{DebounceInterval: 10 * time.Millisecond})
		startPluginKeyRunner(t, runner)
		if i == 0 {
			time.Sleep(30 * time.Millisecond)
			requirePluginKeyCount(t, db, 0)
		} else {
			runner.NotifyPluginAPIKeys([]entities.PluginAPIKeyIdentity{pluginRunnerIdentity("after-restart")})
			waitPluginKeys(t, runner, 0)
		}
	}
}

func TestPluginAPIKeyRunnerDrainsMoreThanOneMemoryBatchWithoutAnotherWindow(t *testing.T) {
	db := openUsageAggregationRunnerDatabase(t)
	runner := poller.NewPluginAPIKeyRunnerWithOptions(db, poller.PluginAPIKeyRunnerOptions{DebounceInterval: 100 * time.Millisecond})
	identities := make([]entities.PluginAPIKeyIdentity, 0, 1005)
	for i := 0; i < 1005; i++ {
		identities = append(identities, pluginRunnerIdentity(fmt.Sprintf("team-%d", i)))
	}
	runner.NotifyPluginAPIKeys(identities)
	startPluginKeyRunner(t, runner)
	waitPluginKeys(t, runner, 0)
	requirePluginKeyCount(t, db, 1005)
}
