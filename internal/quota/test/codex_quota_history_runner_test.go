package test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	. "cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"

	"github.com/sirupsen/logrus"
	logrustest "github.com/sirupsen/logrus/hooks/test"
	"gorm.io/gorm"
)

func TestCodexQuotaHistoryRunnerMergesDuplicatesAndIgnoresSameCycleRecovery(t *testing.T) {
	// 同一绝对周期观察 90 -> 90 -> 89 -> 89 -> 90，最后回升值违反单调不增规则。
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, codexHistoryUsageIdentity("history-auth"))
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour,
		CodexQuotaHistoryFlushInterval:   time.Hour,
		PricingCatalog:                   emptyPricingCatalogForTest(),
	})

	base := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	resetAt := base.Add(5 * time.Hour)
	snapshots := []UsageHeaderSnapshot{
		codexHistoryPrimarySnapshot("history-auth", base, 90, resetAt),
		codexHistoryPrimarySnapshot("history-auth", base.Add(time.Second), 90, resetAt),
		codexHistoryPrimarySnapshot("history-auth", base.Add(2*time.Second), 89, resetAt),
		codexHistoryPrimarySnapshot("history-auth", base.Add(3*time.Second), 89, resetAt),
		codexHistoryPrimarySnapshot("history-auth", base.Add(4*time.Second), 90, resetAt),
	}
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(snapshots...)) {
		t.Fatal("expected cache/history fan-out to accept immutable snapshot pointers")
	}
	// shutdown 必须 drain 已接收队列并在两秒预算内完成最后一次历史写入。
	service.StopRefreshTasks()

	cycles := loadCodexQuotaCycles(t, db, "history-auth")
	if len(cycles) != 1 {
		t.Fatalf("expected one stable quota cycle, got %+v", cycles)
	}
	segments := loadCodexQuotaSegments(t, db, cycles[0].ID)
	if len(segments) != 2 {
		t.Fatalf("expected only observed 90 and 89 segments, got %+v", segments)
	}
	if segments[0].RemainingPercent != 90 || segments[0].ObservationCount != 2 {
		t.Fatalf("expected 90 percent duplicate count 2, got %+v", segments[0])
	}
	if segments[1].RemainingPercent != 89 || segments[1].ObservationCount != 2 {
		t.Fatalf("expected 89 percent duplicate count 2 and recovered 90 ignored, got %+v", segments[1])
	}
}

func TestCodexQuotaHistoryRunnerSortsSameBatchByObservationTime(t *testing.T) {
	// Redis inbox ID/主动刷新完成顺序可能与事件时间相反；同批先到 89、后到更早的 90 仍应保留完整下降线。
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, codexHistoryUsageIdentity("out-of-order-auth"))
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour,
		CodexQuotaHistoryFlushInterval:   time.Hour,
		PricingCatalog:                   emptyPricingCatalogForTest(),
	})

	base := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	resetAt := base.Add(5 * time.Hour)
	older := codexHistoryPrimarySnapshot("out-of-order-auth", base.Add(time.Minute), 90, resetAt)
	newer := codexHistoryPrimarySnapshot("out-of-order-auth", base.Add(2*time.Minute), 89, resetAt)
	// 故意按较新 observation 在前的队列顺序投递；shutdown drain 与正常十秒批次共享同一处理入口。
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(newer, older)) {
		service.StopRefreshTasks()
		t.Fatal("expected out-of-order history observations to enter one batch")
	}
	service.StopRefreshTasks()

	cycles := loadCodexQuotaCycles(t, db, "out-of-order-auth")
	if len(cycles) != 1 {
		t.Fatalf("expected one out-of-order quota cycle, got %+v", cycles)
	}
	segments := loadCodexQuotaSegments(t, db, cycles[0].ID)
	if len(segments) != 2 || segments[0].RemainingPercent != 90 || segments[1].RemainingPercent != 89 {
		t.Fatalf("expected chronological 90 to 89 segments despite reverse arrival, got %+v", segments)
	}
	if !segments[0].FirstObservedAt.Equal(older.ObservedAt) || !segments[1].FirstObservedAt.Equal(newer.ObservedAt) {
		t.Fatalf("expected persisted segment times to follow observation order, got %+v", segments)
	}
}

func TestCodexQuotaHistoryRunnerAllowsHigherPercentOnlyAfterNewCycleAndKeepsOldPending(t *testing.T) {
	// 旧周期先下降到 40，新周期恢复 95；新周期生效后的旧 reset 迟到值必须忽略。
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, codexHistoryUsageIdentity("cycle-auth"))
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour,
		CodexQuotaHistoryFlushInterval:   time.Hour,
		PricingCatalog:                   emptyPricingCatalogForTest(),
	})
	base := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	oldReset := base.Add(5 * time.Hour)
	newReset := oldReset.Add(5 * time.Hour)
	snapshots := []UsageHeaderSnapshot{
		codexHistoryPrimarySnapshot("cycle-auth", base, 40, oldReset),
		codexHistoryPrimarySnapshot("cycle-auth", base.Add(time.Second), 95, newReset),
		codexHistoryPrimarySnapshot("cycle-auth", base.Add(2*time.Second), 39, oldReset),
	}
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(snapshots...)) {
		t.Fatal("expected cycle transition batch to be accepted")
	}
	service.StopRefreshTasks()

	cycles := loadCodexQuotaCycles(t, db, "cycle-auth")
	if len(cycles) != 2 {
		t.Fatalf("expected old and new cycle parents, got %+v", cycles)
	}
	segmentsByReset := make(map[int64][]entities.QuotaPercentSegment, len(cycles))
	for _, cycle := range cycles {
		segmentsByReset[cycle.ResetAt.Unix()] = loadCodexQuotaSegments(t, db, cycle.ID)
	}
	oldSegments := segmentsByReset[oldReset.Unix()]
	if len(oldSegments) != 1 || oldSegments[0].RemainingPercent != 40 {
		t.Fatalf("expected old pending segment preserved and late 39 ignored, got %+v", oldSegments)
	}
	newSegments := segmentsByReset[newReset.Unix()]
	if len(newSegments) != 1 || newSegments[0].RemainingPercent != 95 {
		t.Fatalf("expected new cycle to accept higher starting percent, got %+v", newSegments)
	}
}

func TestCodexQuotaHistoryRunnerDebouncesDirectResetJitterWithoutLosingPercentages(t *testing.T) {
	// 上游连续返回相差一秒的直接重置时刻时，仍必须保留同一周期内真实观察到的 77、76、75。
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, codexHistoryUsageIdentity("jitter-auth"))
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour,
		CodexQuotaHistoryFlushInterval:   time.Hour,
		PricingCatalog:                   emptyPricingCatalogForTest(),
	})

	base := time.Date(2026, 8, 22, 8, 0, 0, 0, time.UTC)
	canonicalReset := time.Date(2026, 8, 27, 3, 35, 8, 0, time.UTC)
	snapshots := []UsageHeaderSnapshot{
		codexHistoryPrimarySnapshot("jitter-auth", base, 77, canonicalReset),
		codexHistoryPrimarySnapshot("jitter-auth", base.Add(time.Second), 76, canonicalReset.Add(2*time.Second)),
		codexHistoryPrimarySnapshot("jitter-auth", base.Add(2*time.Second), 75, canonicalReset.Add(time.Second)),
	}
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(snapshots...)) {
		t.Fatal("expected jitter observations to enter history runner")
	}
	service.StopRefreshTasks()

	cycles := loadCodexQuotaCycles(t, db, "jitter-auth")
	if len(cycles) != 1 {
		t.Fatalf("expected one stable cycle for direct reset jitter, got %+v", cycles)
	}
	if !cycles[0].ResetAt.Equal(canonicalReset) {
		t.Fatalf("expected first direct reset to remain canonical, got %s", cycles[0].ResetAt)
	}
	segments := loadCodexQuotaSegments(t, db, cycles[0].ID)
	if len(segments) != 3 || segments[0].RemainingPercent != 77 || segments[1].RemainingPercent != 76 || segments[2].RemainingPercent != 75 {
		t.Fatalf("expected 77, 76 and 75 in one cycle, got %+v", segments)
	}
}

func TestCodexQuotaHistoryRunnerUsesFixedTwoMinuteResetTolerance(t *testing.T) {
	tests := []struct {
		name       string
		difference time.Duration
		wantCycles int
	}{
		{name: "two minutes merges", difference: 120 * time.Second, wantCycles: 1},
		{name: "over two minutes starts new cycle", difference: 121 * time.Second, wantCycles: 2},
	}
	for testIndex, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			db := openQuotaTestDatabase(t)
			authIndex := fmt.Sprintf("tolerance-auth-%d", testIndex)
			seedUsageIdentity(t, db, codexHistoryUsageIdentity(authIndex))
			service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{
				UsageHeaderSnapshotFlushInterval: time.Hour,
				CodexQuotaHistoryFlushInterval:   time.Hour,
				PricingCatalog:                   emptyPricingCatalogForTest(),
			})
			base := time.Date(2026, 8, 22, 8, 0, 0, 0, time.UTC)
			resetAt := base.Add(7 * 24 * time.Hour)
			first := codexHistoryPrimarySnapshot(authIndex, base, 77, resetAt)
			second := codexHistoryPrimarySnapshot(authIndex, base.Add(time.Second), 76, resetAt.Add(testCase.difference))
			if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(first, second)) {
				t.Fatal("expected tolerance observations to enter history runner")
			}
			service.StopRefreshTasks()

			cycles := loadCodexQuotaCycles(t, db, authIndex)
			if len(cycles) != testCase.wantCycles {
				t.Fatalf("expected %d cycles at %s difference, got %+v", testCase.wantCycles, testCase.difference, cycles)
			}
		})
	}
}

func TestCodexQuotaHistoryRunnerSwitchesWeeklyToFiveHourBeforeComparingReset(t *testing.T) {
	// Weekly 的重置时刻更远；窗口改为 5h 后必须先结束 Weekly，再让后续 5h 百分比继续下降。
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, codexHistoryUsageIdentity("window-switch-auth"))
	base := time.Date(2026, 8, 22, 8, 0, 0, 0, time.UTC)
	weeklyReset := base.Add(7 * 24 * time.Hour)
	fiveHourReset := base.Add(5 * time.Hour)
	weekly := codexUsageHeaderSnapshotWithHeaders("window-switch-auth", base, http.Header{
		"X-Codex-Primary-Used-Percent":   []string{"10"},
		"X-Codex-Primary-Window-Minutes": []string{"10080"},
		"X-Codex-Primary-Reset-At":       []string{strconv.FormatInt(weeklyReset.Unix(), 10)},
	})
	fiveHour := codexHistoryPrimarySnapshot("window-switch-auth", base.Add(time.Minute), 90, fiveHourReset)
	fiveHourLower := codexHistoryPrimarySnapshot("window-switch-auth", base.Add(2*time.Minute), 89, fiveHourReset)

	firstService := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour,
		CodexQuotaHistoryFlushInterval:   time.Hour,
		PricingCatalog:                   emptyPricingCatalogForTest(),
	})
	if !firstService.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(weekly, fiveHour)) {
		firstService.StopRefreshTasks()
		t.Fatal("expected Weekly and 5h observations to enter history runner")
	}
	firstService.StopRefreshTasks()

	// 重启后恢复也必须选择观察时间最新的 5h，而不是 reset 更远的旧 Weekly。
	secondService := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour,
		CodexQuotaHistoryFlushInterval:   time.Hour,
		PricingCatalog:                   emptyPricingCatalogForTest(),
	})
	if !secondService.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(fiveHourLower)) {
		secondService.StopRefreshTasks()
		t.Fatal("expected follow-up 5h observation to enter history runner")
	}
	secondService.StopRefreshTasks()

	cycles := loadCodexQuotaCycles(t, db, "window-switch-auth")
	if len(cycles) != 2 {
		t.Fatalf("expected Weekly and 5h cycles, got %+v", cycles)
	}
	segmentsByWindow := make(map[int64][]entities.QuotaPercentSegment, len(cycles))
	for _, cycle := range cycles {
		segmentsByWindow[cycle.WindowSeconds] = loadCodexQuotaSegments(t, db, cycle.ID)
	}
	if weeklySegments := segmentsByWindow[604_800]; len(weeklySegments) != 1 || weeklySegments[0].RemainingPercent != 90 {
		t.Fatalf("expected the old Weekly cycle to end at 90, got %+v", weeklySegments)
	}
	if fiveHourSegments := segmentsByWindow[18_000]; len(fiveHourSegments) != 2 || fiveHourSegments[0].RemainingPercent != 90 || fiveHourSegments[1].RemainingPercent != 89 {
		t.Fatalf("expected the new 5h cycle to continue from 90 to 89, got %+v", fiveHourSegments)
	}
}

func TestCodexQuotaHistoryRunnerRestoresDatabaseTailBeforeComparing(t *testing.T) {
	// 第一届 service 落库 90；第二届 service 必须先恢复它，不能接受同周期回升 91。
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, codexHistoryUsageIdentity("restart-auth"))
	base := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	resetAt := base.Add(5 * time.Hour)

	firstService := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour,
		CodexQuotaHistoryFlushInterval:   time.Hour,
		PricingCatalog:                   emptyPricingCatalogForTest(),
	})
	first := codexHistoryPrimarySnapshot("restart-auth", base, 90, resetAt)
	if !firstService.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(first)) {
		t.Fatal("expected first service observation to be accepted")
	}
	firstService.StopRefreshTasks()

	secondService := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour,
		CodexQuotaHistoryFlushInterval:   time.Hour,
		PricingCatalog:                   emptyPricingCatalogForTest(),
	})
	recoveredHigher := codexHistoryPrimarySnapshot("restart-auth", base.Add(time.Minute), 91, resetAt)
	lower := codexHistoryPrimarySnapshot("restart-auth", base.Add(2*time.Minute), 89, resetAt)
	if !secondService.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(recoveredHigher, lower)) {
		t.Fatal("expected restarted service observations to be accepted for async comparison")
	}
	secondService.StopRefreshTasks()

	cycles := loadCodexQuotaCycles(t, db, "restart-auth")
	if len(cycles) != 1 {
		t.Fatalf("expected restart to reuse one cycle, got %+v", cycles)
	}
	segments := loadCodexQuotaSegments(t, db, cycles[0].ID)
	if len(segments) != 2 || segments[0].RemainingPercent != 90 || segments[1].RemainingPercent != 89 {
		t.Fatalf("expected recovered 90 tail, ignored 91, then accepted 89, got %+v", segments)
	}
}

func TestCodexQuotaHistoryRunnerCarriesAbsoluteUpgradeIntoMergedPendingSegment(t *testing.T) {
	// relative 与 absolute 落在同一两分钟容差内且百分比相同，最终父周期仍必须升级到绝对边界。
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, codexHistoryUsageIdentity("upgrade-auth"))
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour,
		CodexQuotaHistoryFlushInterval:   time.Hour,
		PricingCatalog:                   emptyPricingCatalogForTest(),
	})
	base := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	relative := codexUsageHeaderSnapshotWithHeaders("upgrade-auth", base, http.Header{
		"X-Codex-Primary-Used-Percent":        []string{"10"},
		"X-Codex-Primary-Window-Minutes":      []string{"300"},
		"X-Codex-Primary-Reset-After-Seconds": []string{"3600"},
	})
	absoluteReset := base.Add(time.Hour + 30*time.Second)
	absolute := codexHistoryPrimarySnapshot("upgrade-auth", base.Add(time.Minute), 90, absoluteReset)
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(relative, absolute)) {
		t.Fatal("expected relative/absolute upgrade observations to be accepted")
	}
	service.StopRefreshTasks()

	cycles := loadCodexQuotaCycles(t, db, "upgrade-auth")
	if len(cycles) != 1 || cycles[0].ResetAtSource != entities.QuotaResetAtSourceAbsolute || !cycles[0].ResetAt.Equal(absoluteReset) {
		t.Fatalf("expected one cycle upgraded to the absolute reset, got %+v", cycles)
	}
	segments := loadCodexQuotaSegments(t, db, cycles[0].ID)
	if len(segments) != 1 || segments[0].RemainingPercent != 90 || segments[0].ObservationCount != 2 {
		t.Fatalf("expected same pending percent to merge count while upgrading boundary, got %+v", segments)
	}
}

func TestCodexQuotaHistoryRunnerCalibratesTrustedActiveResetWithoutPercentChange(t *testing.T) {
	// 专门额度接口在百分比不变时仍可校准 Header 已经明确返回、但存在抖动的重置时间。
	trustedSources := []struct {
		name   string
		source RefreshSource
	}{
		{name: "manual", source: RefreshSourceManual},
		{name: "scheduled", source: RefreshSourceScheduled},
		{name: "inspection", source: RefreshSourceInspection},
	}
	for _, testCase := range trustedSources {
		t.Run(testCase.name, func(t *testing.T) {
			db := openQuotaTestDatabase(t)
			authIndex := "trusted-reset-" + testCase.name
			seedUsageIdentity(t, db, codexHistoryUsageIdentity(authIndex))
			base := time.Now().Add(-time.Minute).Truncate(time.Second)
			headerReset := base.Add(5 * time.Hour)
			trustedReset := headerReset.Add(118 * time.Second)
			handler := &recordingProviderHandler{output: ProviderOutput{Provider: "codex", Result: CodexResult{Usage: &CodexUsagePayload{
				RateLimit: &CodexRateLimitInfo{PrimaryWindow: codexHistoryUsageWindow(10, 18_000, trustedReset)},
			}}}}
			headerService := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{
				UsageHeaderSnapshotFlushInterval: time.Hour,
				CodexQuotaHistoryFlushInterval:   time.Hour,
				PricingCatalog:                   emptyPricingCatalogForTest(),
			})

			header := codexHistoryPrimarySnapshot(authIndex, base, 90, headerReset)
			if !headerService.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(header)) {
				headerService.StopRefreshTasks()
				t.Fatal("expected Header baseline to enter history queue")
			}
			headerService.StopRefreshTasks()

			trustedService := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(map[string]ProviderHandler{"codex": handler}), ServiceOptions{
				UsageHeaderSnapshotFlushInterval: time.Hour,
				CodexQuotaHistoryFlushInterval:   time.Hour,
				PricingCatalog:                   emptyPricingCatalogForTest(),
			})
			if _, err := trustedService.Check(context.Background(), CheckRequest{AuthIndex: authIndex, Source: testCase.source}); err != nil {
				trustedService.StopRefreshTasks()
				t.Fatalf("trusted active quota check failed: %v", err)
			}
			trustedService.StopRefreshTasks()
			calibratedCycles := loadCodexQuotaCycles(t, db, authIndex)
			if len(calibratedCycles) != 1 || !calibratedCycles[0].ResetAt.Equal(trustedReset) {
				t.Fatalf("expected trusted reset to calibrate the persisted Header cycle, got %+v", calibratedCycles)
			}

			// 再次重启后，普通 Header 必须恢复可信边界并继续记录同周期下降。
			followUpService := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{
				UsageHeaderSnapshotFlushInterval: time.Hour,
				CodexQuotaHistoryFlushInterval:   time.Hour,
				PricingCatalog:                   emptyPricingCatalogForTest(),
			})
			validAfterCalibration := codexHistoryPrimarySnapshot(authIndex, time.Now().Add(time.Second), 89, trustedReset.Add(3*time.Second))
			if !followUpService.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(validAfterCalibration)) {
				followUpService.StopRefreshTasks()
				t.Fatal("expected valid follow-up to enter history queue")
			}
			followUpService.StopRefreshTasks()

			cycles := loadCodexQuotaCycles(t, db, authIndex)
			if len(cycles) != 1 || !cycles[0].ResetAt.Equal(trustedReset) {
				t.Fatalf("expected trusted reset to calibrate one stable cycle, got %+v", cycles)
			}
			segments := loadCodexQuotaSegments(t, db, cycles[0].ID)
			if len(segments) != 2 || segments[0].RemainingPercent != 90 || segments[1].RemainingPercent != 89 {
				t.Fatalf("expected persisted 90 to 89 history after boundary calibration, got %+v", segments)
			}
		})
	}
}

func TestCodexQuotaHistoryRunnerDiscardsPendingHeaderAndFlushesTrustedImmediately(t *testing.T) {
	trustedSources := []struct {
		name   string
		source RefreshSource
	}{
		{name: "manual", source: RefreshSourceManual},
		{name: "scheduled", source: RefreshSourceScheduled},
		{name: "inspection", source: RefreshSourceInspection},
	}
	for _, testCase := range trustedSources {
		t.Run(testCase.name, func(t *testing.T) {
			db := openQuotaTestDatabase(t)
			authIndex := "trusted-percent-" + testCase.name
			seedUsageIdentity(t, db, codexHistoryUsageIdentity(authIndex))
			base := time.Now().Add(-time.Minute).Truncate(time.Second)
			resetAt := base.Add(5 * time.Hour)
			handler := &recordingProviderHandler{output: ProviderOutput{Provider: "codex", Result: CodexResult{Usage: &CodexUsagePayload{
				RateLimit: &CodexRateLimitInfo{PrimaryWindow: codexHistoryUsageWindow(10, 18_000, resetAt)},
			}}}}
			service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(map[string]ProviderHandler{"codex": handler}), ServiceOptions{
				UsageHeaderSnapshotFlushInterval: time.Hour,
				CodexQuotaHistoryFlushInterval:   time.Hour,
				PricingCatalog:                   emptyPricingCatalogForTest(),
			})
			defer service.StopRefreshTasks()

			timers := make(chan usageHeaderManualTimer, 1)
			setCodexQuotaHistoryTimerFactory(service, func(delay time.Duration) (<-chan time.Time, func()) {
				timer := usageHeaderManualTimer{delay: delay, fire: make(chan time.Time, 1)}
				timers <- timer
				return timer.fire, func() {}
			})
			writerKinds := make(chan bool, 2)
			setCodexQuotaHistoryWriter(service, func(ctx context.Context, writerDB *gorm.DB, observations []repositorydto.CodexMainQuotaObservation) error {
				if len(observations) == 0 {
					t.Fatal("expected non-empty source-specific history batch")
				}
				authoritative := observations[0].Authoritative
				for _, observation := range observations[1:] {
					if observation.Authoritative != authoritative {
						t.Fatalf("expected Header and trusted observations in separate writer batches, got %+v", observations)
					}
				}
				err := repository.WriteCodexMainQuotaObservations(ctx, writerDB, observations)
				writerKinds <- authoritative
				return err
			})

			badHeader := codexHistoryPrimarySnapshot(authIndex, base, 80, resetAt)
			if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(badHeader)) {
				t.Fatal("expected Header observation to enter delayed history queue")
			}
			timer := waitForCodexQuotaHistoryManualTimer(t, timers)
			if timer.delay != time.Hour {
				t.Fatalf("expected configured Header history delay, got %s", timer.delay)
			}
			if _, err := service.Check(context.Background(), CheckRequest{AuthIndex: authIndex, Source: testCase.source}); err != nil {
				t.Fatalf("trusted active quota check failed: %v", err)
			}

			select {
			case gotAuthoritative := <-writerKinds:
				if !gotAuthoritative {
					t.Fatal("expected only the trusted source to reach the writer")
				}
			case <-time.After(time.Second):
				t.Fatal("expected trusted source to flush without firing the Header timer")
			}
			select {
			case gotAuthoritative := <-writerKinds:
				t.Fatalf("expected pending Header to be discarded, got another writer batch authoritative=%v", gotAuthoritative)
			case <-time.After(20 * time.Millisecond):
			}
			cycles := loadCodexQuotaCycles(t, db, authIndex)
			if len(cycles) != 1 {
				t.Fatalf("expected one corrected cycle, got %+v", cycles)
			}
			segments := loadCodexQuotaSegments(t, db, cycles[0].ID)
			if len(segments) != 1 || segments[0].RemainingPercent != 90 {
				t.Fatalf("expected trusted 90 percent to remove false Header 80, got %+v", segments)
			}
			if segments[0].ObservationCount != 1 {
				t.Fatalf("expected the discarded Header not to increase observation count, got %+v", segments[0])
			}
		})
	}
}

func TestCodexQuotaHistoryRunnerPreservesHeaderOutsideTrustedAccountRole(t *testing.T) {
	// 账号 A 的可信刷新只能替代 A 的同角色 Header；账号 B 的待写 Header 必须继续独立落库。
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, codexHistoryUsageIdentity("trusted-account"))
	seedUsageIdentity(t, db, codexHistoryUsageIdentity("header-account"))
	base := time.Now().Add(-time.Minute).Truncate(time.Second)
	primaryReset := base.Add(5 * time.Hour)
	handler := &recordingProviderHandler{output: ProviderOutput{Provider: "codex", Result: CodexResult{Usage: &CodexUsagePayload{
		RateLimit: &CodexRateLimitInfo{PrimaryWindow: codexHistoryUsageWindow(10, 18_000, primaryReset)},
	}}}}
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(map[string]ProviderHandler{"codex": handler}), ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour,
		CodexQuotaHistoryFlushInterval:   time.Hour,
		PricingCatalog:                   emptyPricingCatalogForTest(),
	})
	defer service.StopRefreshTasks()

	trustedAccountHeader := codexHistoryPrimarySnapshot("trusted-account", base, 80, primaryReset)
	unrelatedHeader := codexHistoryPrimarySnapshot("header-account", base, 76, primaryReset)
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(trustedAccountHeader, unrelatedHeader)) {
		t.Fatal("expected both accounts' Header observations to enter delayed history queue")
	}
	if _, err := service.Check(context.Background(), CheckRequest{AuthIndex: "trusted-account", Source: RefreshSourceManual}); err != nil {
		t.Fatalf("trusted active quota check failed: %v", err)
	}

	// 两个账号都落库后分别检查尾段，证明 A 的 80% 被替代而 B 的 76% 没有被跨账号清空。
	waitForCodexQuotaCycleCount(t, db, 2)
	trustedCycles := loadCodexQuotaCycles(t, db, "trusted-account")
	if len(trustedCycles) != 1 {
		t.Fatalf("expected one trusted account cycle, got %+v", trustedCycles)
	}
	trustedSegments := loadCodexQuotaSegments(t, db, trustedCycles[0].ID)
	if len(trustedSegments) != 1 || trustedSegments[0].RemainingPercent != 90 {
		t.Fatalf("expected trusted account Header to be replaced by 90 percent, got %+v", trustedSegments)
	}
	unrelatedCycles := loadCodexQuotaCycles(t, db, "header-account")
	if len(unrelatedCycles) != 1 {
		t.Fatalf("expected one unrelated Header cycle, got %+v", unrelatedCycles)
	}
	unrelatedSegments := loadCodexQuotaSegments(t, db, unrelatedCycles[0].ID)
	if len(unrelatedSegments) != 1 || unrelatedSegments[0].RemainingPercent != 76 {
		t.Fatalf("expected unrelated Header 76 percent to persist, got %+v", unrelatedSegments)
	}
}

func TestCodexQuotaHistoryRunnerPrefersTrustedQueueAtTimerBoundary(t *testing.T) {
	// 生产者先写可信队列、再发布通知；timer 到点时必须以两条队列的固定边界为准，不能只探测通知。
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, codexHistoryUsageIdentity("trusted-boundary-auth"))
	base := time.Now().Add(-time.Minute).Truncate(time.Second)
	resetAt := base.Add(5 * time.Hour)
	handler := &recordingProviderHandler{output: ProviderOutput{Provider: "codex", Result: CodexResult{Usage: &CodexUsagePayload{
		RateLimit: &CodexRateLimitInfo{PrimaryWindow: codexHistoryUsageWindow(10, 18_000, resetAt)},
	}}}}
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(map[string]ProviderHandler{"codex": handler}), ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour,
		CodexQuotaHistoryFlushInterval:   time.Hour,
		PricingCatalog:                   emptyPricingCatalogForTest(),
	})

	timers := make(chan usageHeaderManualTimer, 1)
	timerBoundaryReached := make(chan struct{})
	releaseTimerBoundary := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(releaseTimerBoundary) })
	}
	defer func() {
		release()
		service.StopRefreshTasks()
	}()
	setCodexQuotaHistoryTimerFactory(service, func(delay time.Duration) (<-chan time.Time, func()) {
		timer := usageHeaderManualTimer{delay: delay, fire: make(chan time.Time, 1)}
		timers <- timer
		return timer.fire, func() {
			close(timerBoundaryReached)
			<-releaseTimerBoundary
		}
	})

	writes := make(chan []repositorydto.CodexMainQuotaObservation, 2)
	setCodexQuotaHistoryWriter(service, func(ctx context.Context, writerDB *gorm.DB, observations []repositorydto.CodexMainQuotaObservation) error {
		copied := append([]repositorydto.CodexMainQuotaObservation(nil), observations...)
		writes <- copied
		return repository.WriteCodexMainQuotaObservations(ctx, writerDB, observations)
	})

	badHeader := codexHistoryPrimarySnapshot("trusted-boundary-auth", base, 100, resetAt)
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(badHeader)) {
		t.Fatal("expected Header observation to enter delayed history queue")
	}
	timer := waitForCodexQuotaHistoryManualTimer(t, timers)
	timer.fire <- time.Now()
	select {
	case <-timerBoundaryReached:
	case <-time.After(time.Second):
		t.Fatal("expected runner to reach the timer boundary")
	}

	// 主动查询在 runner 暂停期间完整入队；取走通知模拟生产者持锁时队列发送已完成、通知尚未发布的瞬间。
	if _, err := service.Check(context.Background(), CheckRequest{AuthIndex: "trusted-boundary-auth", Source: RefreshSourceManual}); err != nil {
		t.Fatalf("trusted active quota check failed: %v", err)
	}
	if !consumeCodexQuotaHistoryTrustedWake(service) {
		t.Fatal("expected trusted queue notification to be pending at the timer boundary")
	}
	release()

	select {
	case observations := <-writes:
		if len(observations) != 1 || !observations[0].Authoritative || observations[0].RemainingPercent != 90 {
			t.Fatalf("expected timer boundary to discard false Header 100 and write trusted 90, got %+v", observations)
		}
	case <-time.After(time.Second):
		t.Fatal("expected timer boundary history write")
	}
	select {
	case observations := <-writes:
		t.Fatalf("expected one source-specific writer batch, got another %+v", observations)
	case <-time.After(20 * time.Millisecond):
	}

	cycles := loadCodexQuotaCycles(t, db, "trusted-boundary-auth")
	if len(cycles) != 1 {
		t.Fatalf("expected one trusted cycle at the timer boundary, got %+v", cycles)
	}
	segments := loadCodexQuotaSegments(t, db, cycles[0].ID)
	if len(segments) != 1 || segments[0].RemainingPercent != 90 || segments[0].ObservationCount != 1 {
		t.Fatalf("expected only the trusted 90 percent observation, got %+v", segments)
	}
}

func TestCodexQuotaHistoryRunnerDoesNotUseHeaderResetWhenPercentIsRejected(t *testing.T) {
	// Header 百分比异常回升时整份候选不具备校准权限；后续与最后可信边界超过两分钟仍建立新周期。
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, codexHistoryUsageIdentity("untrusted-header-reset"))
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour,
		CodexQuotaHistoryFlushInterval:   time.Hour,
		PricingCatalog:                   emptyPricingCatalogForTest(),
	})
	base := time.Date(2026, 8, 22, 8, 0, 0, 0, time.UTC)
	inferredReset := base.Add(5 * time.Hour)
	directReset := inferredReset.Add(118 * time.Second)
	inferred := codexUsageHeaderSnapshotWithHeaders("untrusted-header-reset", base, http.Header{
		"X-Codex-Primary-Used-Percent":        []string{"10"},
		"X-Codex-Primary-Window-Minutes":      []string{"300"},
		"X-Codex-Primary-Reset-After-Seconds": []string{strconv.FormatInt(int64(5*time.Hour/time.Second), 10)},
	})
	badPercent := codexHistoryPrimarySnapshot("untrusted-header-reset", base.Add(time.Second), 100, directReset)
	validFollowUp := codexHistoryPrimarySnapshot("untrusted-header-reset", base.Add(2*time.Second), 89, directReset.Add(3*time.Second))
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(inferred, badPercent, validFollowUp)) {
		service.StopRefreshTasks()
		t.Fatal("expected Header observations to enter history queue")
	}
	service.StopRefreshTasks()

	cycles := loadCodexQuotaCycles(t, db, "untrusted-header-reset")
	if len(cycles) != 2 || !cycles[0].ResetAt.Equal(inferredReset) || !cycles[1].ResetAt.Equal(directReset.Add(3*time.Second)) {
		t.Fatalf("expected rejected Header reset not to bridge the 121-second boundary, got %+v", cycles)
	}
	for _, cycle := range cycles {
		for _, segment := range loadCodexQuotaSegments(t, db, cycle.ID) {
			if segment.RemainingPercent == 100 {
				t.Fatalf("expected invalid 100 percent Header observation to stay rejected, got %+v", segment)
			}
		}
	}
}

func TestCodexQuotaHistoryRunnerRecordsActiveCheckMainWindowsOnly(t *testing.T) {
	// 主动 Check 已确认活跃 Codex Auth File；完整 Review/Additional 也不能生成第三个父周期。
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, codexHistoryUsageIdentity("check-auth"))
	now := time.Now().Truncate(time.Second)
	primary := codexHistoryUsageWindow(10, 18_000, now.Add(5*time.Hour))
	secondary := codexHistoryUsageWindow(20, 604_800, now.Add(7*24*time.Hour))
	handler := &recordingProviderHandler{output: ProviderOutput{Provider: "codex", Result: CodexResult{Usage: &CodexUsagePayload{
		RateLimit:           &CodexRateLimitInfo{PrimaryWindow: primary, SecondaryWindow: secondary},
		CodeReviewRateLimit: &CodexRateLimitInfo{PrimaryWindow: primary},
		AdditionalRateLimits: []CodexAdditionalRateLimit{{
			LimitName: "Spark",
			RateLimit: &CodexRateLimitInfo{PrimaryWindow: primary},
		}},
	}}}}
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(map[string]ProviderHandler{"codex": handler}), ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour,
		CodexQuotaHistoryFlushInterval:   time.Hour,
		PricingCatalog:                   emptyPricingCatalogForTest(),
	})
	if _, err := service.Check(context.Background(), CheckRequest{AuthIndex: "check-auth"}); err != nil {
		t.Fatalf("active Codex Check returned error: %v", err)
	}
	service.StopRefreshTasks()

	cycles := loadCodexQuotaCycles(t, db, "check-auth")
	if len(cycles) != 2 {
		t.Fatalf("expected only Primary and Secondary main cycles, got %+v", cycles)
	}
	quotaKeys := []string{cycles[0].QuotaKey, cycles[1].QuotaKey}
	sort.Strings(quotaKeys)
	if fmt.Sprint(quotaKeys) != "[rate_limit.primary_window rate_limit.secondary_window]" {
		t.Fatalf("expected primary/secondary quota keys only, got %v", quotaKeys)
	}
}

func TestCodexQuotaHistoryRunnerLogsActiveRefreshSourceOnWriteFailure(t *testing.T) {
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, codexHistoryUsageIdentity("source-log-auth"))
	now := time.Now().Truncate(time.Second)
	handler := &recordingProviderHandler{output: ProviderOutput{Provider: "codex", Result: CodexResult{Usage: &CodexUsagePayload{
		RateLimit: &CodexRateLimitInfo{PrimaryWindow: codexHistoryUsageWindow(24, 604_800, now.Add(7*24*time.Hour))},
	}}}}
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(map[string]ProviderHandler{"codex": handler}), ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour,
		CodexQuotaHistoryFlushInterval:   time.Hour,
		PricingCatalog:                   emptyPricingCatalogForTest(),
	})
	setRefreshCooldown(service, func(time.Duration) {})
	setCodexQuotaHistoryWriter(service, func(context.Context, *gorm.DB, []repositorydto.CodexMainQuotaObservation) error {
		return errors.New("injected history write failure")
	})
	hook := logrustest.NewGlobal()
	defer hook.Reset()
	previousLevel := logrus.GetLevel()
	logrus.SetLevel(logrus.WarnLevel)
	t.Cleanup(func() { logrus.SetLevel(previousLevel) })

	if _, err := service.Refresh(context.Background(), RefreshRequest{AuthIndexes: []string{"source-log-auth"}, Source: RefreshSourceScheduled}); err != nil {
		t.Fatalf("scheduled Codex refresh returned error: %v", err)
	}
	waitForRefreshTask(t, service, "source-log-auth", RefreshTaskStatusCompleted)
	service.StopRefreshTasks()

	for _, entry := range hook.AllEntries() {
		if entry.Message == "codex quota history flush failed" && entry.Data["sources"] == "scheduled" {
			return
		}
	}
	t.Fatalf("expected history failure log to retain scheduled source, got %#v", hook.AllEntries())
}

func TestCodexQuotaHistoryRunnerMergesOverlappingHeaderAndActiveCheck(t *testing.T) {
	// 主动刷新已经进入 provider 时让 Header 先入队，证明两条独立来源共享同一周期和百分比尾段。
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, codexHistoryUsageIdentity("overlap-auth"))
	resetAt := time.Now().Add(5 * time.Hour).Truncate(time.Second)
	providerEntered := make(chan struct{}, 1)
	releaseProvider := make(chan struct{})
	handler := &blockingCodexHistoryProviderHandler{
		entered: providerEntered,
		release: releaseProvider,
		output: ProviderOutput{Provider: "codex", Result: CodexResult{Usage: &CodexUsagePayload{
			RateLimit: &CodexRateLimitInfo{PrimaryWindow: codexHistoryUsageWindow(10, 18_000, resetAt)},
		}}},
	}
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(map[string]ProviderHandler{"codex": handler}), ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour,
		CodexQuotaHistoryFlushInterval:   time.Hour,
		PricingCatalog:                   emptyPricingCatalogForTest(),
	})
	checkResult := make(chan error, 1)
	go func() {
		_, err := service.Check(context.Background(), CheckRequest{AuthIndex: "overlap-auth"})
		checkResult <- err
	}()
	select {
	case <-providerEntered:
	case <-time.After(time.Second):
		service.StopRefreshTasks()
		t.Fatal("expected active check to enter provider")
	}

	// Header 观察时间早于主动刷新完成时间，固定同百分比两次观察的真实先后顺序。
	headerObservedAt := time.Now().Add(-time.Minute)
	headerSnapshot := codexHistoryPrimarySnapshot("overlap-auth", headerObservedAt, 90, resetAt)
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(headerSnapshot)) {
		close(releaseProvider)
		service.StopRefreshTasks()
		t.Fatal("expected overlapping Header snapshot to enter history queue")
	}
	close(releaseProvider)
	select {
	case err := <-checkResult:
		if err != nil {
			service.StopRefreshTasks()
			t.Fatalf("overlapping active check returned error: %v", err)
		}
	case <-time.After(time.Second):
		service.StopRefreshTasks()
		t.Fatal("overlapping active check did not complete")
	}
	service.StopRefreshTasks()

	cycles := loadCodexQuotaCycles(t, db, "overlap-auth")
	if len(cycles) != 1 {
		t.Fatalf("expected Header and active check to share one cycle, got %+v", cycles)
	}
	segments := loadCodexQuotaSegments(t, db, cycles[0].ID)
	if len(segments) != 1 || segments[0].RemainingPercent != 90 || segments[0].ObservationCount != 2 {
		t.Fatalf("expected overlapping sources to merge into one 90 percent segment counted twice, got %+v", segments)
	}
}

func TestCodexQuotaHistoryRunnerRejectsNonCodexHeaderIdentity(t *testing.T) {
	// provider 文本看似 Codex 也不能替代 usage_identities.type=codex 的真实身份判断。
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, entities.UsageIdentity{
		Identity: "claude-auth", Provider: "codex", Type: "claude", AuthType: entities.UsageIdentityAuthTypeAuthFile,
	})
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour,
		CodexQuotaHistoryFlushInterval:   time.Hour,
		PricingCatalog:                   emptyPricingCatalogForTest(),
	})
	snapshot := codexHistoryPrimarySnapshot("claude-auth", time.Now(), 90, time.Now().Add(5*time.Hour))
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(snapshot)) {
		t.Fatal("cache fan-out should accept snapshot even when history later rejects identity")
	}
	service.StopRefreshTasks()

	if cycles := loadCodexQuotaCycles(t, db, "claude-auth"); len(cycles) != 0 {
		t.Fatalf("expected non-Codex Auth File to have no history, got %+v", cycles)
	}
}

func TestCodexQuotaHistoryQueueFullDoesNotBlockOrDiscardCacheSnapshot(t *testing.T) {
	// 第一批到点后的 identity 查询被挂起、第二份占满容量一队列，第三份 history 必须被丢弃但 cache 仍接收。
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, codexHistoryUsageIdentity("queue-auth"))
	timers := make(chan usageHeaderManualTimer, 1)
	queryEntered := make(chan struct{}, 1)
	releaseQuery := make(chan struct{})
	var blocked atomic.Bool
	callbackName := "test:block_codex_history_identity_query"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if queryMentionsTable(tx.Statement.SQL.String(), "usage_identities") && blocked.CompareAndSwap(false, true) {
			queryEntered <- struct{}{}
			<-releaseQuery
		}
	}); err != nil {
		t.Fatalf("register history identity blocker: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour,
		CodexQuotaHistoryFlushInterval:   time.Hour,
		CodexQuotaHistoryQueueSize:       1,
		PricingCatalog:                   emptyPricingCatalogForTest(),
	})
	setCodexQuotaHistoryTimerFactory(service, func(delay time.Duration) (<-chan time.Time, func()) {
		manualTimer := usageHeaderManualTimer{delay: delay, fire: make(chan time.Time, 1)}
		timers <- manualTimer
		return manualTimer.fire, func() {}
	})
	base := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	resetAt := base.Add(5 * time.Hour)
	first := codexHistoryPrimarySnapshot("queue-auth", base, 90, resetAt)
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(first)) {
		t.Fatal("expected first snapshot to enter history runner")
	}
	// 新批次模型在 timer 到点前不取队列；手动到点后才进入第一批身份查询。
	firstTimer := waitForCodexQuotaHistoryManualTimer(t, timers)
	firstTimer.fire <- time.Now()
	select {
	case <-queryEntered:
	case <-time.After(time.Second):
		close(releaseQuery)
		service.StopRefreshTasks()
		t.Fatal("expected history runner to enter identity query")
	}

	second := codexHistoryPrimarySnapshot("queue-auth", base.Add(time.Second), 89, resetAt)
	third := codexHistoryPrimarySnapshot("queue-auth", base.Add(2*time.Second), 87, resetAt)
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(second)) {
		t.Fatal("expected second snapshot to occupy one-slot history queue")
	}
	appendDone := make(chan bool, 1)
	go func() {
		appendDone <- service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(third))
	}()
	select {
	case accepted := <-appendDone:
		if !accepted {
			t.Fatal("history queue rejection must not reject cache append")
		}
	case <-time.After(100 * time.Millisecond):
		close(releaseQuery)
		service.StopRefreshTasks()
		t.Fatal("queue-full history fan-out blocked usage/cache producer")
	}
	close(releaseQuery)
	service.StopRefreshTasks()

	// 分钟 cache shutdown flush 使用同身份最新快照，证明 history 丢弃边界与 cache pending map 独立。
	task, err := service.GetRefreshTaskByAuthIndex(context.Background(), "queue-auth")
	if err != nil {
		t.Fatalf("load queue-auth cache task: %v", err)
	}
	if task.Quota == nil || len(task.Quota.Quota) != 1 || task.Quota.Quota[0].UsedPercent == nil || *task.Quota.Quota[0].UsedPercent != 13 {
		t.Fatalf("expected latest third snapshot to reach cache despite full history queue, got %+v", task)
	}
}

func TestCodexQuotaHistoryRunnerSnapshotsQueueOnlyWhenTimerExpires(t *testing.T) {
	// 第一条只启动十秒窗口；timer 到期时固定当时两条，落库期间到达的第三条必须留给下一轮。
	db := openQuotaTestDatabase(t)
	for _, authIndex := range []string{"batch-auth-1", "batch-auth-2", "batch-auth-3"} {
		seedUsageIdentity(t, db, codexHistoryUsageIdentity(authIndex))
	}
	// 手动 timer 让测试精确控制两个十秒窗口，而不依赖 CI 的真实调度时间。
	timers := make(chan usageHeaderManualTimer, 3)
	// 第一批在批量 identity 查询处暂停，提供一个确定的“落库期间”并发入队窗口。
	queryEntered := make(chan struct{}, 1)
	releaseQuery := make(chan struct{})
	var releaseQueryOnce sync.Once
	release := func() {
		releaseQueryOnce.Do(func() { close(releaseQuery) })
	}
	var blockOnce sync.Once
	callbackName := "test:block_codex_history_snapshot_batch"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if queryMentionsTable(tx.Statement.SQL.String(), "usage_identities") {
			blockOnce.Do(func() {
				queryEntered <- struct{}{}
				<-releaseQuery
			})
		}
	}); err != nil {
		t.Fatalf("register history batch blocker: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour,
		PricingCatalog:                   emptyPricingCatalogForTest(),
	})
	// 任一断言提前结束测试时先解除查询，再让 shutdown drain 等待 runner 完成。
	defer func() {
		release()
		service.StopRefreshTasks()
	}()
	setCodexQuotaHistoryTimerFactory(service, func(delay time.Duration) (<-chan time.Time, func()) {
		manualTimer := usageHeaderManualTimer{delay: delay, fire: make(chan time.Time, 1)}
		timers <- manualTimer
		return manualTimer.fire, func() {}
	})

	base := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	first := codexHistoryPrimarySnapshot("batch-auth-1", base, 90, base.Add(5*time.Hour))
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(first)) {
		t.Fatal("expected first batch snapshot to enter history queue")
	}
	firstTimer := waitForCodexQuotaHistoryManualTimer(t, timers)
	if firstTimer.delay != 10*time.Second {
		t.Fatalf("expected first observation to start a ten-second window, got %s", firstTimer.delay)
	}
	if queueLength := codexQuotaHistoryHeaderQueueLength(service); queueLength != 1 {
		t.Fatalf("expected first observation to remain queued before timer expiry, got queue length %d", queueLength)
	}

	second := codexHistoryPrimarySnapshot("batch-auth-2", base.Add(time.Second), 89, base.Add(5*time.Hour))
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(second)) {
		t.Fatal("expected second observation to join the active ten-second queue window")
	}
	if queueLength := codexQuotaHistoryHeaderQueueLength(service); queueLength != 2 {
		t.Fatalf("expected two observations queued at timer expiry boundary, got %d", queueLength)
	}
	firstTimer.fire <- time.Now()
	select {
	case <-queryEntered:
	case <-time.After(time.Second):
		release()
		t.Fatal("expected first frozen batch to enter identity verification")
	}

	// 第一批已经按 timer 到期时的数量取出；此后到达的数据必须留在 channel 中等待第二轮。
	third := codexHistoryPrimarySnapshot("batch-auth-3", base.Add(2*time.Second), 88, base.Add(5*time.Hour))
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(third)) {
		release()
		t.Fatal("expected observation during first batch persistence to enter next queue window")
	}
	if queueLength := codexQuotaHistoryHeaderQueueLength(service); queueLength != 1 {
		release()
		t.Fatalf("expected only the next-window observation to remain queued, got %d", queueLength)
	}
	release()

	// 第一轮必须只写前两条；runner 随后从残留 wake 启动第二个完整十秒窗口。
	secondTimer := waitForCodexQuotaHistoryManualTimer(t, timers)
	waitForCodexQuotaCycleCount(t, db, 2)
	if queueLength := codexQuotaHistoryHeaderQueueLength(service); queueLength != 1 {
		t.Fatalf("expected third observation to remain queued before second timer, got %d", queueLength)
	}
	secondTimer.fire <- time.Now()
	waitForCodexQuotaCycleCount(t, db, 3)
}

func TestCodexQuotaHistoryRunnerUsesDefaultWindowWithoutCountBasedEarlyFlush(t *testing.T) {
	// 默认生产配置必须等待完整十秒；即使队列超过旧的 256 条阈值也不能提前查询或落库。
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, codexHistoryUsageIdentity("volume-auth"))
	timers := make(chan usageHeaderManualTimer, 2)
	var identityQueries atomic.Int64
	callbackName := "test:count_codex_history_default_window_queries"
	if err := db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if queryMentionsTable(tx.Statement.SQL.String(), "usage_identities") {
			identityQueries.Add(1)
		}
	}); err != nil {
		t.Fatalf("register history identity query counter: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour,
		PricingCatalog:                   emptyPricingCatalogForTest(),
	})
	defer service.StopRefreshTasks()
	setCodexQuotaHistoryTimerFactory(service, func(delay time.Duration) (<-chan time.Time, func()) {
		manualTimer := usageHeaderManualTimer{delay: delay, fire: make(chan time.Time, 1)}
		timers <- manualTimer
		return manualTimer.fire, func() {}
	})

	base := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	resetAt := base.Add(5 * time.Hour)
	snapshots := make([]UsageHeaderSnapshot, 0, 257)
	for index := range 257 {
		// 同一身份同一百分比模拟高频 Header；每条仍代表一次需要累计的真实观察。
		snapshots = append(snapshots, codexHistoryPrimarySnapshot("volume-auth", base.Add(time.Duration(index)*time.Millisecond), 90, resetAt))
	}
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(snapshots...)) {
		t.Fatal("expected all high-volume snapshots to enter cache/history fan-out")
	}
	timer := waitForCodexQuotaHistoryManualTimer(t, timers)
	if timer.delay != 10*time.Second {
		t.Fatalf("expected production default history window of ten seconds, got %s", timer.delay)
	}
	if queueLength := codexQuotaHistoryHeaderQueueLength(service); queueLength != 257 {
		t.Fatalf("expected all 257 observations to remain queued before timer expiry, got %d", queueLength)
	}
	if got := identityQueries.Load(); got != 0 {
		t.Fatalf("expected no identity query before timer expiry, got %d", got)
	}
	select {
	case unexpected := <-timers:
		t.Fatalf("expected one active timer regardless of queue size, got another delay %s", unexpected.delay)
	case <-time.After(30 * time.Millisecond):
	}

	// 手动到点后一次处理全部现有数据，证明测试观察的是延迟边界而不是丢弃路径。
	timer.fire <- time.Now()
	waitForCodexQuotaCycleCount(t, db, 1)
	cycles := loadCodexQuotaCycles(t, db, "volume-auth")
	segments := loadCodexQuotaSegments(t, db, cycles[0].ID)
	if len(segments) != 1 || segments[0].RemainingPercent != 90 || segments[0].ObservationCount != 257 {
		t.Fatalf("expected one post-window segment containing all 257 observations, got %+v", segments)
	}
}

func TestCodexQuotaHistoryRunnerSkipsRepeatedRecoveryAfterSameBatchFailure(t *testing.T) {
	// 同一账号窗口本批首次恢复失败后必须立即放弃余下候选；下一批新数据仍要重新尝试恢复。
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, codexHistoryUsageIdentity("recovery-failure-auth"))
	timers := make(chan usageHeaderManualTimer, 2)
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour,
		PricingCatalog:                   emptyPricingCatalogForTest(),
	})
	defer service.StopRefreshTasks()
	setCodexQuotaHistoryTimerFactory(service, func(delay time.Duration) (<-chan time.Time, func()) {
		manualTimer := usageHeaderManualTimer{delay: delay, fire: make(chan time.Time, 1)}
		timers <- manualTimer
		return manualTimer.fire, func() {}
	})

	var recoveryCalls atomic.Int64
	var failRecovery atomic.Bool
	failRecovery.Store(true)
	recoveryEntered := make(chan struct{}, 1)
	setCodexQuotaHistoryLoader(service, func(ctx context.Context, writerDB *gorm.DB, authIndex string, windowRole string) (repositorydto.CodexQuotaHistoryState, error) {
		recoveryCalls.Add(1)
		select {
		case recoveryEntered <- struct{}{}:
		default:
		}
		if failRecovery.Load() {
			return repositorydto.CodexQuotaHistoryState{}, errors.New("injected state recovery failure")
		}
		return repository.LoadLatestCodexQuotaHistoryState(ctx, writerDB, authIndex, windowRole)
	})

	base := time.Date(2026, 8, 22, 8, 0, 0, 0, time.UTC)
	resetAt := base.Add(5 * time.Hour)
	firstBatch := []UsageHeaderSnapshot{
		codexHistoryPrimarySnapshot("recovery-failure-auth", base, 90, resetAt),
		codexHistoryPrimarySnapshot("recovery-failure-auth", base.Add(time.Second), 89, resetAt),
		codexHistoryPrimarySnapshot("recovery-failure-auth", base.Add(2*time.Second), 88, resetAt),
	}
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(firstBatch...)) {
		t.Fatal("expected failing recovery batch to enter history queue")
	}
	firstTimer := waitForCodexQuotaHistoryManualTimer(t, timers)
	firstTimer.fire <- time.Now()
	select {
	case <-recoveryEntered:
	case <-time.After(time.Second):
		t.Fatal("expected first batch state recovery attempt")
	}

	// 首批数量已经固定后再入队下一份，让第二个 timer 成为首批处理完成的确定同步点。
	followUp := codexHistoryPrimarySnapshot("recovery-failure-auth", base.Add(time.Minute), 87, resetAt)
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(followUp)) {
		t.Fatal("expected follow-up observation to enter next history batch")
	}
	secondTimer := waitForCodexQuotaHistoryManualTimer(t, timers)
	if got := recoveryCalls.Load(); got != 1 {
		t.Fatalf("expected one recovery attempt for the failed state key in one batch, got %d", got)
	}

	// 新批次解除注入失败后必须再次恢复并只保存本批的新鲜基线，不重放失败批次。
	failRecovery.Store(false)
	secondTimer.fire <- time.Now()
	waitForCodexQuotaCycleCount(t, db, 1)
	if got := recoveryCalls.Load(); got != 2 {
		t.Fatalf("expected next batch to retry state recovery once, got %d total calls", got)
	}
	cycles := loadCodexQuotaCycles(t, db, "recovery-failure-auth")
	segments := loadCodexQuotaSegments(t, db, cycles[0].ID)
	if len(segments) != 1 || segments[0].RemainingPercent != 87 || segments[0].ObservationCount != 1 {
		t.Fatalf("expected only the successful follow-up baseline to persist, got %+v", segments)
	}
}

func TestCodexQuotaHistoryWriteFailureInvalidatesStateBeforeNextObservation(t *testing.T) {
	// 首次父行 INSERT 被触发器拒绝；第二份 observation 必须从空数据库恢复并独立落库。
	db := openQuotaTestDatabase(t)
	seedUsageIdentity(t, db, codexHistoryUsageIdentity("failure-auth"))
	if err := db.Exec(`CREATE TRIGGER fail_codex_history_once BEFORE INSERT ON quota_cycles BEGIN SELECT RAISE(ABORT, 'history write failed'); END;`).Error; err != nil {
		t.Fatalf("create history failure trigger: %v", err)
	}
	writeAttempted := make(chan struct{}, 1)
	var signalOnce sync.Once
	callbackName := "test:observe_codex_history_write_failure"
	if err := db.Callback().Create().After("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "quota_cycles" {
			signalOnce.Do(func() { writeAttempted <- struct{}{} })
		}
	}); err != nil {
		t.Fatalf("register history create callback: %v", err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callbackName) })

	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour,
		CodexQuotaHistoryFlushInterval:   10 * time.Millisecond,
		PricingCatalog:                   emptyPricingCatalogForTest(),
	})
	base := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	resetAt := base.Add(5 * time.Hour)
	first := codexHistoryPrimarySnapshot("failure-auth", base, 90, resetAt)
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(first)) {
		t.Fatal("expected first failing history observation to be accepted asynchronously")
	}
	select {
	case <-writeAttempted:
	case <-time.After(time.Second):
		service.StopRefreshTasks()
		t.Fatal("expected first history write attempt")
	}
	if err := db.Exec(`DROP TRIGGER fail_codex_history_once`).Error; err != nil {
		service.StopRefreshTasks()
		t.Fatalf("drop history failure trigger: %v", err)
	}
	// 给失败 flush 返回并清空/失效内存状态的机会，再提交下一份真实下降 observation。
	time.Sleep(30 * time.Millisecond)
	second := codexHistoryPrimarySnapshot("failure-auth", base.Add(time.Minute), 89, resetAt)
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(second)) {
		t.Fatal("expected observation after failed write to be accepted")
	}
	service.StopRefreshTasks()

	cycles := loadCodexQuotaCycles(t, db, "failure-auth")
	if len(cycles) != 1 {
		t.Fatalf("expected recovered second observation to create one cycle, got %+v", cycles)
	}
	segments := loadCodexQuotaSegments(t, db, cycles[0].ID)
	if len(segments) != 1 || segments[0].RemainingPercent != 89 || segments[0].ObservationCount != 1 {
		t.Fatalf("expected failed 90 batch dropped and reloaded 89 saved once, got %+v", segments)
	}
}

func TestCodexQuotaHistoryRunnerRecoversAfterPartialRepositoryCommit(t *testing.T) {
	// 一次 flush 的前 32 条提交后第 33 条失败；runner 必须丢弃内存批次并从已提交尾段继续。
	db := openQuotaTestDatabase(t)
	const observationCount = 33
	for index := range observationCount {
		seedUsageIdentity(t, db, codexHistoryUsageIdentity(fmt.Sprintf("partial-auth-%03d", index)))
	}
	if err := db.Exec(`CREATE TRIGGER fail_partial_codex_history BEFORE INSERT ON quota_cycles WHEN NEW.auth_index = 'partial-auth-032' BEGIN SELECT RAISE(ABORT, 'expected second transaction failure'); END;`).Error; err != nil {
		t.Fatalf("create partial history failure trigger: %v", err)
	}

	writeResults := make(chan error, 2)
	service := NewServiceWithRegistryAndOptions(db, NewProviderRegistry(nil), ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour,
		CodexQuotaHistoryFlushInterval:   10 * time.Millisecond,
		PricingCatalog:                   emptyPricingCatalogForTest(),
	})
	defer service.StopRefreshTasks()
	setCodexQuotaHistoryWriter(service, func(ctx context.Context, writerDB *gorm.DB, observations []repositorydto.CodexMainQuotaObservation) error {
		err := repository.WriteCodexMainQuotaObservations(ctx, writerDB, observations)
		writeResults <- err
		return err
	})

	base := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	resetAt := base.Add(5 * time.Hour)
	firstBatch := make([]UsageHeaderSnapshot, 0, observationCount)
	for index := range observationCount {
		authIndex := fmt.Sprintf("partial-auth-%03d", index)
		firstBatch = append(firstBatch, codexHistoryPrimarySnapshot(authIndex, base.Add(time.Duration(index)*time.Millisecond), 90, resetAt))
	}
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(firstBatch...)) {
		t.Fatal("expected partial-failure batch to enter history queue")
	}
	select {
	case err := <-writeResults:
		if err == nil {
			t.Fatal("expected second repository transaction to fail")
		}
	case <-time.After(time.Second):
		t.Fatal("expected partial repository write result")
	}

	// 第一事务已经提交 32 个父子状态；失败不能让 runner 自动重放并重复累计它们。
	var committedCycles int64
	if err := db.Model(&entities.QuotaCycle{}).Count(&committedCycles).Error; err != nil {
		t.Fatalf("count cycles after partial history failure: %v", err)
	}
	if committedCycles != 32 {
		t.Fatalf("expected first 32 cycles to stay committed, got %d", committedCycles)
	}
	committed := loadCodexQuotaCycles(t, db, "partial-auth-000")
	committedSegments := loadCodexQuotaSegments(t, db, committed[0].ID)
	if len(committedSegments) != 1 || committedSegments[0].ObservationCount != 1 {
		t.Fatalf("expected committed tail to remain counted once before recovery, got %+v", committedSegments)
	}
	if failed := loadCodexQuotaCycles(t, db, "partial-auth-032"); len(failed) != 0 {
		t.Fatalf("expected failed second transaction to leave no cycle, got %+v", failed)
	}

	if err := db.Exec(`DROP TRIGGER fail_partial_codex_history`).Error; err != nil {
		t.Fatalf("drop partial history failure trigger: %v", err)
	}
	followUp := []UsageHeaderSnapshot{
		codexHistoryPrimarySnapshot("partial-auth-000", base.Add(time.Minute), 90, resetAt),
		codexHistoryPrimarySnapshot("partial-auth-000", base.Add(2*time.Minute), 89, resetAt),
	}
	if !service.TryAppendUsageHeaderSnapshots(usageHeaderSnapshotPointers(followUp...)) {
		t.Fatal("expected observations after partial failure to enter history queue")
	}
	select {
	case err := <-writeResults:
		if err != nil {
			t.Fatalf("expected recovery write to succeed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("expected recovery repository write result")
	}

	// 相同百分比只为新观察增加一次，随后下降形成新段，证明比较状态来自数据库尾段。
	committed = loadCodexQuotaCycles(t, db, "partial-auth-000")
	committedSegments = loadCodexQuotaSegments(t, db, committed[0].ID)
	if len(committedSegments) != 2 || committedSegments[0].RemainingPercent != 90 || committedSegments[0].ObservationCount != 2 || committedSegments[1].RemainingPercent != 89 || committedSegments[1].ObservationCount != 1 {
		t.Fatalf("expected database-tail recovery without duplicate retry count, got %+v", committedSegments)
	}
	untouched := loadCodexQuotaCycles(t, db, "partial-auth-001")
	untouchedSegments := loadCodexQuotaSegments(t, db, untouched[0].ID)
	if len(untouchedSegments) != 1 || untouchedSegments[0].ObservationCount != 1 {
		t.Fatalf("expected unrelated committed state to remain counted once, got %+v", untouchedSegments)
	}
}

func codexHistoryUsageIdentity(authIndex string) entities.UsageIdentity {
	// 历史只接受活跃 OAuth Auth File；Type=codex 是 Header identity 验证的真实依据。
	return entities.UsageIdentity{
		Identity: authIndex,
		Provider: "codex",
		Type:     "codex",
		AuthType: entities.UsageIdentityAuthTypeAuthFile,
	}
}

func codexHistoryPrimarySnapshot(authIndex string, observedAt time.Time, remainingPercent int, resetAt time.Time) UsageHeaderSnapshot {
	// Header 原始值是已用百分比，测试 helper 显式转换页面口径的整数剩余百分比。
	usedPercent := 100 - remainingPercent
	return codexUsageHeaderSnapshotWithHeaders(authIndex, observedAt, http.Header{
		"X-Codex-Primary-Used-Percent":   []string{strconv.Itoa(usedPercent)},
		"X-Codex-Primary-Window-Minutes": []string{"300"},
		"X-Codex-Primary-Reset-At":       []string{strconv.FormatInt(resetAt.Unix(), 10)},
	})
}

func codexHistoryUsageWindow(usedPercent float64, windowSeconds int64, resetAt time.Time) *CodexUsageWindow {
	// 主动查询必须通过 presence 标记区分明确零值和字段缺失。
	return &CodexUsageWindow{
		UsedPercent:           usedPercent,
		LimitWindowSeconds:    windowSeconds,
		ResetAt:               resetAt.Unix(),
		HasUsedPercent:        true,
		HasLimitWindowSeconds: true,
		HasResetAt:            true,
	}
}

func loadCodexQuotaCycles(t *testing.T, db *gorm.DB, authIndex string) []entities.QuotaCycle {
	t.Helper()
	var cycles []entities.QuotaCycle
	if err := db.Where("provider = ? AND auth_index = ?", "codex", authIndex).Order("reset_at ASC, id ASC").Find(&cycles).Error; err != nil {
		t.Fatalf("load Codex quota cycles for %s: %v", authIndex, err)
	}
	return cycles
}

func loadCodexQuotaSegments(t *testing.T, db *gorm.DB, cycleID int64) []entities.QuotaPercentSegment {
	t.Helper()
	var segments []entities.QuotaPercentSegment
	if err := db.Where("cycle_id = ?", cycleID).Order("first_observed_at ASC, id ASC").Find(&segments).Error; err != nil {
		t.Fatalf("load Codex quota percent segments for cycle %d: %v", cycleID, err)
	}
	return segments
}

func waitForCodexQuotaHistoryManualTimer(t *testing.T, timers <-chan usageHeaderManualTimer) usageHeaderManualTimer {
	t.Helper()
	select {
	case timer := <-timers:
		return timer
	case <-time.After(time.Second):
		t.Fatal("expected Codex quota history batch timer")
		return usageHeaderManualTimer{}
	}
}

func waitForCodexQuotaCycleCount(t *testing.T, db *gorm.DB, expected int64) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var count int64
		if err := db.Model(&entities.QuotaCycle{}).Count(&count).Error; err != nil {
			t.Fatalf("count Codex quota cycles: %v", err)
		}
		if count == expected {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected %d Codex quota cycles before deadline", expected)
}

type blockingCodexHistoryProviderHandler struct {
	// entered/release 只用于把主动刷新暂停在 observation 入队之前，构造确定的 Header 并发窗口。
	entered chan<- struct{}
	release <-chan struct{}
	output  ProviderOutput
}

func (h *blockingCodexHistoryProviderHandler) Check(_ context.Context, _ ProviderInput) (ProviderOutput, error) {
	h.entered <- struct{}{}
	<-h.release
	return h.output, nil
}
