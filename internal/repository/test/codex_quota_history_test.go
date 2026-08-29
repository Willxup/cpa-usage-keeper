package test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repositorydto "cpa-usage-keeper/internal/repository/dto"

	"gorm.io/gorm"
)

func TestWriteCodexMainQuotaObservationsPreservesMonotonicSegments(t *testing.T) {
	// 准备：固定一个五小时 Primary 周期，构造重复、下降和非法回升序列。
	db := openCodexQuotaHistoryRepositoryDatabase(t, "monotonic.db")
	resetAt := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	firstObservedAt := resetAt.Add(-4 * time.Hour)
	observations := []repositorydto.CodexMainQuotaObservation{
		codexQuotaHistoryObservation("codex-auth", "primary", 18_000, resetAt, 90, firstObservedAt),
		codexQuotaHistoryObservation("codex-auth", "primary", 18_000, resetAt, 90, firstObservedAt.Add(time.Minute)),
		codexQuotaHistoryObservation("codex-auth", "primary", 18_000, resetAt, 89, firstObservedAt.Add(2*time.Minute)),
		codexQuotaHistoryObservation("codex-auth", "primary", 18_000, resetAt, 89, firstObservedAt.Add(3*time.Minute)),
		codexQuotaHistoryObservation("codex-auth", "primary", 18_000, resetAt, 90, firstObservedAt.Add(4*time.Minute)),
	}

	// 执行：repository 在 writer 短事务中逐条复核，runner 的内存判断不能成为唯一防线。
	if err := repository.WriteCodexMainQuotaObservations(context.Background(), db, observations); err != nil {
		t.Fatalf("write monotonic codex quota observations: %v", err)
	}

	// 断言：90 和 89 各保留一行，最后回升到 90 的 observation 被忽略。
	cycle, segments := loadCodexQuotaHistoryRows(t, db, "codex-auth", "primary")
	if len(segments) != 2 {
		t.Fatalf("expected two monotonic percent segments, got %+v", segments)
	}
	if segments[0].RemainingPercent != 90 || segments[0].ObservationCount != 2 {
		t.Fatalf("unexpected 90 percent segment: %+v", segments[0])
	}
	if segments[1].RemainingPercent != 89 || segments[1].ObservationCount != 2 {
		t.Fatalf("unexpected 89 percent segment: %+v", segments[1])
	}
	if !cycle.LastObservedAt.Equal(firstObservedAt.Add(3 * time.Minute)) {
		t.Fatalf("expected ignored recovery not to advance parent time, got %s", cycle.LastObservedAt)
	}

	// 断言：runner 重启恢复入口必须返回当前周期和最低尾段的完整状态。
	state, err := repository.LoadLatestCodexQuotaHistoryState(context.Background(), db, "codex-auth", "primary")
	if err != nil {
		t.Fatalf("load latest codex quota history state: %v", err)
	}
	if !state.Found || !state.HasTail || state.TailRemainingPercent != 89 || !state.TailLastObservedAt.Equal(firstObservedAt.Add(3*time.Minute)) {
		t.Fatalf("unexpected recovered codex quota history state: %+v", state)
	}
}

func TestWriteCodexMainQuotaObservationsCorrectsFalseLowTailFromAuthoritativePercent(t *testing.T) {
	// Header 错误低值可以连续出现；同周期可信刷新回到 90% 时必须删除全部不可能的低值尾段。
	db := openCodexQuotaHistoryRepositoryDatabase(t, "authoritative-percent-correction.db")
	resetAt := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	firstObservedAt := resetAt.Add(-4 * time.Hour)
	observations := []repositorydto.CodexMainQuotaObservation{
		codexQuotaHistoryObservation("codex-auth", "primary", 18_000, resetAt, 100, firstObservedAt),
		codexQuotaHistoryObservation("codex-auth", "primary", 18_000, resetAt, 95, firstObservedAt.Add(time.Minute)),
		codexQuotaHistoryObservation("codex-auth", "primary", 18_000, resetAt, 90, firstObservedAt.Add(2*time.Minute)),
		codexQuotaHistoryObservation("codex-auth", "primary", 18_000, resetAt, 80, firstObservedAt.Add(3*time.Minute)),
		codexQuotaHistoryObservation("codex-auth", "primary", 18_000, resetAt, 75, firstObservedAt.Add(4*time.Minute)),
	}
	correction := codexQuotaHistoryObservation("codex-auth", "primary", 18_000, resetAt, 90, firstObservedAt.Add(5*time.Minute))
	correction.Authoritative = true
	observations = append(observations, correction)

	if err := repository.WriteCodexMainQuotaObservations(context.Background(), db, observations); err != nil {
		t.Fatalf("write authoritative percent correction: %v", err)
	}

	cycle, segments := loadCodexQuotaHistoryRows(t, db, "codex-auth", "primary")
	if len(segments) != 3 || segments[0].RemainingPercent != 100 || segments[1].RemainingPercent != 95 || segments[2].RemainingPercent != 90 {
		t.Fatalf("expected false 80 and 75 tails to be replaced by 90, got %+v", segments)
	}
	if segments[2].ObservationCount != 2 || !segments[2].LastObservedAt.Equal(correction.LastObservedAt) {
		t.Fatalf("expected existing 90 percent segment to absorb the trusted correction, got %+v", segments[2])
	}
	if !cycle.LastObservedAt.Equal(correction.LastObservedAt) {
		t.Fatalf("expected correction to advance parent observation time, got %s", cycle.LastObservedAt)
	}
}

func TestWriteCodexMainQuotaObservationsStartsNewCycleAndIgnoresOldLateData(t *testing.T) {
	// 准备：同一账号 Primary 先观察旧周期 20%，再进入重置后的新周期 99%。
	db := openCodexQuotaHistoryRepositoryDatabase(t, "cycle-switch.db")
	oldResetAt := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	newResetAt := oldResetAt.Add(5 * time.Hour)
	oldObservedAt := oldResetAt.Add(-time.Hour)
	newObservedAt := oldResetAt.Add(time.Minute)
	observations := []repositorydto.CodexMainQuotaObservation{
		codexQuotaHistoryObservation("codex-auth", "primary", 18_000, oldResetAt, 20, oldObservedAt),
		codexQuotaHistoryObservation("codex-auth", "primary", 18_000, newResetAt, 99, newObservedAt),
		codexQuotaHistoryObservation("codex-auth", "primary", 18_000, oldResetAt, 19, newObservedAt.Add(time.Minute)),
	}

	// 执行：新 reset 周期允许百分比回到高值，切换后的旧周期迟到数据必须忽略。
	if err := repository.WriteCodexMainQuotaObservations(context.Background(), db, observations); err != nil {
		t.Fatalf("write switched codex quota observations: %v", err)
	}

	// 断言：两个周期各有一个真实段，旧周期没有被迟到的 19% 继续推进。
	var cycles []entities.QuotaCycle
	if err := db.Where("provider = ? AND auth_index = ? AND quota_key = ?", "codex", "codex-auth", "rate_limit.primary_window").Order("reset_at asc").Find(&cycles).Error; err != nil {
		t.Fatalf("load switched codex quota cycles: %v", err)
	}
	if len(cycles) != 2 {
		t.Fatalf("expected two codex quota cycles, got %+v", cycles)
	}
	var segments []entities.QuotaPercentSegment
	if err := db.Where("cycle_id IN ?", []int64{cycles[0].ID, cycles[1].ID}).Order("cycle_id asc").Find(&segments).Error; err != nil {
		t.Fatalf("load switched codex quota percent segments: %v", err)
	}
	if len(segments) != 2 || segments[0].RemainingPercent != 20 || segments[1].RemainingPercent != 99 {
		t.Fatalf("unexpected switched codex quota percent segments: %+v", segments)
	}
	if !cycles[0].LastObservedAt.Equal(oldObservedAt) {
		t.Fatalf("expected old cycle late observation to be ignored, got %+v", cycles[0])
	}
}

func TestWriteCodexMainQuotaObservationsSwitchesWindowBeforeComparingReset(t *testing.T) {
	// Weekly reset 虽然更远，但后续 observation 改为 5h 时，窗口变化必须优先建立新周期。
	db := openCodexQuotaHistoryRepositoryDatabase(t, "window-switch-priority.db")
	base := time.Date(2026, 8, 22, 8, 0, 0, 0, time.UTC)
	weeklyReset := base.Add(7 * 24 * time.Hour)
	fiveHourReset := base.Add(5 * time.Hour)
	weekly := codexQuotaHistoryObservation("codex-auth", "primary", 604_800, weeklyReset, 90, base)
	fiveHour := codexQuotaHistoryObservation("codex-auth", "primary", 18_000, fiveHourReset, 90, base.Add(time.Minute))
	fiveHourLower := codexQuotaHistoryObservation("codex-auth", "primary", 18_000, fiveHourReset, 89, base.Add(2*time.Minute))
	lateWeekly := codexQuotaHistoryObservation("codex-auth", "primary", 604_800, weeklyReset, 89, base.Add(30*time.Second))

	if err := repository.WriteCodexMainQuotaObservations(context.Background(), db, []repositorydto.CodexMainQuotaObservation{weekly, fiveHour, fiveHourLower, lateWeekly}); err != nil {
		t.Fatalf("write window-switch observations: %v", err)
	}

	var cycles []entities.QuotaCycle
	if err := db.Where("provider = ? AND auth_index = ? AND quota_key = ?", "codex", "codex-auth", "rate_limit.primary_window").Order("last_observed_at asc, id asc").Find(&cycles).Error; err != nil {
		t.Fatalf("load window-switch cycles: %v", err)
	}
	if len(cycles) != 2 || cycles[0].WindowSeconds != 604_800 || cycles[1].WindowSeconds != 18_000 {
		t.Fatalf("expected one ended Weekly cycle followed by one 5h cycle, got %+v", cycles)
	}
	weeklySegments := loadCodexQuotaHistorySegments(t, db, cycles[0].ID)
	if len(weeklySegments) != 1 || weeklySegments[0].RemainingPercent != 90 {
		t.Fatalf("expected late old Weekly observation to be ignored, got %+v", weeklySegments)
	}
	fiveHourSegments := loadCodexQuotaHistorySegments(t, db, cycles[1].ID)
	if len(fiveHourSegments) != 2 || fiveHourSegments[0].RemainingPercent != 90 || fiveHourSegments[1].RemainingPercent != 89 {
		t.Fatalf("expected 5h observations to continue in the new cycle, got %+v", fiveHourSegments)
	}
	state, err := repository.LoadLatestCodexQuotaHistoryState(context.Background(), db, "codex-auth", "primary")
	if err != nil {
		t.Fatalf("load latest window-switch state: %v", err)
	}
	if !state.Found || state.WindowSeconds != 18_000 || !state.HasTail || state.TailRemainingPercent != 89 {
		t.Fatalf("expected state recovery to select the latest observed 5h cycle, got %+v", state)
	}
}

func TestWriteCodexMainQuotaObservationsMergesRelativeResetAndUpgradesAbsoluteBoundary(t *testing.T) {
	// 准备：relative-only 候选相差 30 秒，随后官方 absolute 边界在两分钟容差内到达。
	db := openCodexQuotaHistoryRepositoryDatabase(t, "relative-upgrade.db")
	firstResetAt := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	first := codexQuotaHistoryObservation("codex-auth", "secondary", 604_800, firstResetAt, 90, firstResetAt.Add(-time.Hour))
	first.ResetAtSource = "relative"
	second := codexQuotaHistoryObservation("codex-auth", "secondary", 604_800, firstResetAt.Add(30*time.Second), 89, first.FirstObservedAt.Add(time.Minute))
	second.ResetAtSource = "relative"
	absolute := codexQuotaHistoryObservation("codex-auth", "secondary", 604_800, firstResetAt.Add(40*time.Second), 88, first.FirstObservedAt.Add(2*time.Minute))
	absolute.ResetAtSource = "absolute"

	// 执行：relative 候选先合并到一行，absolute 到达后原子校正同一父周期边界。
	if err := repository.WriteCodexMainQuotaObservations(context.Background(), db, []repositorydto.CodexMainQuotaObservation{first, second, absolute}); err != nil {
		t.Fatalf("write relative and absolute codex quota observations: %v", err)
	}

	// 断言：最终只有一个父周期，reset 来源和起点都升级为 absolute 边界。
	cycle, segments := loadCodexQuotaHistoryRows(t, db, "codex-auth", "secondary")
	if cycle.ResetAtSource != entities.QuotaResetAtSourceAbsolute || !cycle.ResetAt.Equal(absolute.ResetAt) || !cycle.WindowStartedAt.Equal(absolute.ResetAt.Add(-604_800*time.Second)) {
		t.Fatalf("unexpected upgraded codex quota cycle: %+v", cycle)
	}
	if len(segments) != 3 || segments[0].RemainingPercent != 90 || segments[1].RemainingPercent != 89 || segments[2].RemainingPercent != 88 {
		t.Fatalf("unexpected relative-upgrade segments: %+v", segments)
	}
}

func TestWriteCodexMainQuotaObservationsLetsAuthoritativeSourceCalibrateAbsoluteReset(t *testing.T) {
	// Header 和可信接口都返回明确时刻时，可信结果仍应在两分钟容差内替换 Header 抖动边界。
	db := openCodexQuotaHistoryRepositoryDatabase(t, "authoritative-absolute-reset.db")
	base := time.Date(2026, 8, 22, 8, 0, 0, 0, time.UTC)
	headerReset := base.Add(5 * time.Hour)
	trustedReset := headerReset.Add(118 * time.Second)
	header := codexQuotaHistoryObservation("codex-auth", "primary", 18_000, headerReset, 90, base)
	trusted := codexQuotaHistoryObservation("codex-auth", "primary", 18_000, trustedReset, 90, base.Add(time.Minute))
	trusted.Authoritative = true
	followUp := codexQuotaHistoryObservation("codex-auth", "primary", 18_000, trustedReset.Add(3*time.Second), 89, base.Add(2*time.Minute))

	if err := repository.WriteCodexMainQuotaObservations(context.Background(), db, []repositorydto.CodexMainQuotaObservation{header, trusted, followUp}); err != nil {
		t.Fatalf("write authoritative absolute reset calibration: %v", err)
	}

	cycle, segments := loadCodexQuotaHistoryRows(t, db, "codex-auth", "primary")
	if !cycle.ResetAt.Equal(trustedReset) || !cycle.WindowStartedAt.Equal(trustedReset.Add(-18_000*time.Second)) {
		t.Fatalf("expected trusted source to calibrate absolute reset and window start, got %+v", cycle)
	}
	if len(segments) != 2 || segments[0].RemainingPercent != 90 || segments[0].ObservationCount != 2 || segments[1].RemainingPercent != 89 {
		t.Fatalf("expected reset calibration to preserve 90 then 89 history, got %+v", segments)
	}
}

func TestWriteCodexMainQuotaObservationsToleranceMergesDirectResetJitter(t *testing.T) {
	// 上游直接返回的两个重置时刻相差 30 秒时仍属于同一稳定周期，后续下降必须保留。
	db := openCodexQuotaHistoryRepositoryDatabase(t, "absolute-nearby.db")
	firstResetAt := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	first := codexQuotaHistoryObservation("codex-auth", "primary", 18_000, firstResetAt, 77, firstResetAt.Add(-time.Hour))
	second := codexQuotaHistoryObservation("codex-auth", "primary", 18_000, firstResetAt.Add(30*time.Second), 76, first.FirstObservedAt.Add(time.Minute))
	if err := repository.WriteCodexMainQuotaObservations(context.Background(), db, []repositorydto.CodexMainQuotaObservation{first, second}); err != nil {
		t.Fatalf("write nearby absolute codex quota cycles: %v", err)
	}
	var cycleCount int64
	if err := db.Model(&entities.QuotaCycle{}).Where("provider = ? AND auth_index = ? AND quota_key = ?", "codex", "codex-auth", "rate_limit.primary_window").Count(&cycleCount).Error; err != nil {
		t.Fatalf("count nearby absolute cycles: %v", err)
	}
	if cycleCount != 1 {
		t.Fatalf("expected one debounced direct-reset cycle, got %d", cycleCount)
	}
	_, segments := loadCodexQuotaHistoryRows(t, db, "codex-auth", "primary")
	if len(segments) != 2 || segments[0].RemainingPercent != 77 || segments[1].RemainingPercent != 76 {
		t.Fatalf("expected direct reset jitter to retain 77 and 76, got %+v", segments)
	}
}

func TestWriteCodexMainQuotaObservationsKeepsResetToleranceBounded(t *testing.T) {
	// 所有重置时刻统一允许两分钟抖动；边界外必须建立新周期，避免跨周期静默合并。
	testCases := []struct {
		name               string
		resetOffset        time.Duration
		expectedCycleCount int64
	}{
		{name: "exactly_two_minutes_merges", resetOffset: 120 * time.Second, expectedCycleCount: 1},
		{name: "over_two_minutes_splits", resetOffset: 121 * time.Second, expectedCycleCount: 2},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			db := openCodexQuotaHistoryRepositoryDatabase(t, testCase.name+".db")
			firstResetAt := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
			first := codexQuotaHistoryObservation("codex-auth", "primary", 18_000, firstResetAt, 90, firstResetAt.Add(-time.Hour))
			second := codexQuotaHistoryObservation("codex-auth", "primary", 18_000, firstResetAt.Add(testCase.resetOffset), 89, first.FirstObservedAt.Add(time.Minute))

			if err := repository.WriteCodexMainQuotaObservations(context.Background(), db, []repositorydto.CodexMainQuotaObservation{first, second}); err != nil {
				t.Fatalf("write reset boundary observations: %v", err)
			}
			var cycles []entities.QuotaCycle
			if err := db.Where("provider = ? AND auth_index = ? AND quota_key = ?", "codex", "codex-auth", "rate_limit.primary_window").Order("reset_at asc").Find(&cycles).Error; err != nil {
				t.Fatalf("load reset boundary cycles: %v", err)
			}
			if int64(len(cycles)) != testCase.expectedCycleCount {
				t.Fatalf("expected %d cycles at offset %s, got %+v", testCase.expectedCycleCount, testCase.resetOffset, cycles)
			}
			if testCase.expectedCycleCount == 1 {
				segments := loadCodexQuotaHistorySegments(t, db, cycles[0].ID)
				if len(segments) != 2 || segments[0].RemainingPercent != 90 || segments[1].RemainingPercent != 89 {
					t.Fatalf("expected tolerance-bound observations to share one ordered cycle, got %+v", segments)
				}
			}
		})
	}
}

func TestWriteCodexMainQuotaObservationsRejectsInvalidInputAndCanceledContext(t *testing.T) {
	// 准备：构造最小非法输入和已经取消的全局写入 context。
	db := openCodexQuotaHistoryRepositoryDatabase(t, "validation.db")
	invalid := codexQuotaHistoryObservation("", "primary", 18_000, time.Now(), 90, time.Now())
	if err := repository.WriteCodexMainQuotaObservations(context.Background(), db, []repositorydto.CodexMainQuotaObservation{invalid}); err == nil {
		t.Fatal("expected empty auth_index observation to be rejected")
	}
	// 超出 time.Duration 秒域的正窗口会让 reset_at-window_seconds 回绕，必须在事务前拒绝。
	overflow := codexQuotaHistoryObservation("codex-auth", "primary", math.MaxInt64/int64(time.Second)+1, time.Now().Add(time.Hour), 90, time.Now())
	if err := repository.WriteCodexMainQuotaObservations(context.Background(), db, []repositorydto.CodexMainQuotaObservation{overflow}); err == nil {
		t.Fatal("expected time.Duration-overflowing window seconds to be rejected")
	}
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	valid := codexQuotaHistoryObservation("codex-auth", "primary", 18_000, time.Now().Add(time.Hour), 90, time.Now())

	// 执行与断言：取消必须在取得 writer 事务前退出，不能继续创建父子行。
	if err := repository.WriteCodexMainQuotaObservations(canceledContext, db, []repositorydto.CodexMainQuotaObservation{valid}); err == nil {
		t.Fatal("expected canceled codex quota history write to fail")
	}
	var cycleCount int64
	if err := db.Model(&entities.QuotaCycle{}).Count(&cycleCount).Error; err != nil {
		t.Fatalf("count cycles after canceled write: %v", err)
	}
	if cycleCount != 0 {
		t.Fatalf("expected canceled write to create no cycles, got %d", cycleCount)
	}
}

func TestWriteCodexMainQuotaObservationsKeepsThirtyTwoItemTransactionBoundary(t *testing.T) {
	// 32 条仍在一个短事务中；第 33 条才进入下一事务，分别用最后一条触发失败证明提交边界。
	testCases := []struct {
		name               string
		observationCount   int
		expectedCycleCount int64
	}{
		{name: "thirty_two_roll_back_together", observationCount: 32, expectedCycleCount: 0},
		{name: "thirty_third_starts_next_transaction", observationCount: 33, expectedCycleCount: 32},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			db := openCodexQuotaHistoryRepositoryDatabase(t, testCase.name+".db")
			resetAt := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
			observedAt := resetAt.Add(-time.Hour)
			observations := make([]repositorydto.CodexMainQuotaObservation, 0, testCase.observationCount)
			for index := range testCase.observationCount {
				authIndex := fmt.Sprintf("codex-auth-%03d", index)
				observations = append(observations, codexQuotaHistoryObservation(authIndex, "primary", 18_000, resetAt, 90, observedAt.Add(time.Duration(index)*time.Second)))
			}
			// 受控测试身份不包含外部输入；trigger 只让本批最后一条父行 INSERT 失败。
			failingAuthIndex := fmt.Sprintf("codex-auth-%03d", testCase.observationCount-1)
			triggerSQL := fmt.Sprintf(`CREATE TRIGGER fail_last_codex_quota_cycle BEFORE INSERT ON quota_cycles WHEN NEW.auth_index = '%s' BEGIN SELECT RAISE(ABORT, 'expected transaction boundary failure'); END;`, failingAuthIndex)
			if err := db.Exec(triggerSQL).Error; err != nil {
				t.Fatalf("create transaction boundary trigger: %v", err)
			}

			if err := repository.WriteCodexMainQuotaObservations(context.Background(), db, observations); err == nil {
				t.Fatal("expected final observation to fail")
			}
			var cycleCount int64
			if err := db.Model(&entities.QuotaCycle{}).Count(&cycleCount).Error; err != nil {
				t.Fatalf("count cycles after boundary failure: %v", err)
			}
			if cycleCount != testCase.expectedCycleCount {
				t.Fatalf("expected %d committed cycles, got %d", testCase.expectedCycleCount, cycleCount)
			}
			var segmentCount int64
			if err := db.Model(&entities.QuotaPercentSegment{}).Count(&segmentCount).Error; err != nil {
				t.Fatalf("count segments after boundary failure: %v", err)
			}
			if segmentCount != testCase.expectedCycleCount {
				t.Fatalf("expected parent and child counts to match at %d, got %d children", testCase.expectedCycleCount, segmentCount)
			}
		})
	}
}

func TestWriteCodexMainQuotaObservationsMergesOneThousandEqualPercentages(t *testing.T) {
	// 高频 Header 在同一周期反复返回相同整数百分比时，只更新尾段时间和累计次数。
	db := openCodexQuotaHistoryRepositoryDatabase(t, "thousand-duplicates.db")
	resetAt := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	firstObservedAt := resetAt.Add(-time.Hour)
	observations := make([]repositorydto.CodexMainQuotaObservation, 0, 1000)
	for index := range 1000 {
		observations = append(observations, codexQuotaHistoryObservation(
			"codex-auth",
			"primary",
			18_000,
			resetAt,
			90,
			firstObservedAt.Add(time.Duration(index)*time.Millisecond),
		))
	}

	if err := repository.WriteCodexMainQuotaObservations(context.Background(), db, observations); err != nil {
		t.Fatalf("write one thousand duplicate percentages: %v", err)
	}
	cycle, segments := loadCodexQuotaHistoryRows(t, db, "codex-auth", "primary")
	if len(segments) != 1 {
		t.Fatalf("expected one merged percent segment, got %+v", segments)
	}
	segment := segments[0]
	if segment.RemainingPercent != 90 || segment.ObservationCount != 1000 {
		t.Fatalf("expected one 90 percent segment counted 1000 times, got %+v", segment)
	}
	if !segment.FirstObservedAt.Equal(firstObservedAt) || !segment.LastObservedAt.Equal(firstObservedAt.Add(999*time.Millisecond)) {
		t.Fatalf("unexpected merged observation range: %+v", segment)
	}
	if !cycle.FirstObservedAt.Equal(firstObservedAt) || !cycle.LastObservedAt.Equal(segment.LastObservedAt) {
		t.Fatalf("expected parent range to match merged segment, got parent %+v child %+v", cycle, segment)
	}
}

func TestWriteCodexMainQuotaObservationsHonorsCancellationWhileWriterIsOccupied(t *testing.T) {
	// 生产只有一个 SQLite writer；先占住该连接，证明 history 在池外等待时能响应调用方取消。
	db := openCodexQuotaHistoryRepositoryDatabase(t, "writer-cancel.db")
	heldTransaction := db.Begin()
	if heldTransaction.Error != nil {
		t.Fatalf("begin held writer transaction: %v", heldTransaction.Error)
	}
	t.Cleanup(func() { _ = heldTransaction.Rollback().Error })
	observation := codexQuotaHistoryObservation(
		"codex-auth",
		"primary",
		18_000,
		time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC),
		90,
		time.Date(2026, 8, 20, 23, 0, 0, 0, time.UTC),
	)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- repository.WriteCodexMainQuotaObservations(ctx, db, []repositorydto.CodexMainQuotaObservation{observation})
	}()
	// 写连接仍由 heldTransaction 持有；取消必须唤醒等待中的 history 调用。
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context cancellation while waiting for writer, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("history write did not stop after context cancellation")
	}
	if err := heldTransaction.Rollback().Error; err != nil {
		t.Fatalf("release held writer transaction: %v", err)
	}
	var cycleCount int64
	if err := db.Model(&entities.QuotaCycle{}).Count(&cycleCount).Error; err != nil {
		t.Fatalf("count cycles after canceled writer wait: %v", err)
	}
	if cycleCount != 0 {
		t.Fatalf("expected canceled writer wait to persist no cycles, got %d", cycleCount)
	}
}

func TestWriteCodexMainQuotaObservationsRollsBackParentWhenChildInsertFails(t *testing.T) {
	// 父周期先创建、百分比子行后创建；子表失败必须由同一个事务连父行一起回滚。
	db := openCodexQuotaHistoryRepositoryDatabase(t, "child-rollback.db")
	if err := db.Exec(`CREATE TRIGGER fail_codex_quota_segment BEFORE INSERT ON quota_percent_segments BEGIN SELECT RAISE(ABORT, 'expected child insert failure'); END;`).Error; err != nil {
		t.Fatalf("create child failure trigger: %v", err)
	}
	observation := codexQuotaHistoryObservation(
		"codex-auth",
		"primary",
		18_000,
		time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC),
		90,
		time.Date(2026, 8, 20, 23, 0, 0, 0, time.UTC),
	)
	if err := repository.WriteCodexMainQuotaObservations(context.Background(), db, []repositorydto.CodexMainQuotaObservation{observation}); err == nil {
		t.Fatal("expected child segment insert to fail")
	}
	var cycleCount int64
	if err := db.Model(&entities.QuotaCycle{}).Count(&cycleCount).Error; err != nil {
		t.Fatalf("count cycles after child failure: %v", err)
	}
	var segmentCount int64
	if err := db.Model(&entities.QuotaPercentSegment{}).Count(&segmentCount).Error; err != nil {
		t.Fatalf("count segments after child failure: %v", err)
	}
	if cycleCount != 0 || segmentCount != 0 {
		t.Fatalf("expected parent and child rollback, got %d cycles and %d segments", cycleCount, segmentCount)
	}
}

func codexQuotaHistoryObservation(authIndex string, role string, windowSeconds int64, resetAt time.Time, remaining int, observedAt time.Time) repositorydto.CodexMainQuotaObservation {
	// 测试 helper 默认构造单份 absolute observation；单项测试再覆盖 relative 或其它字段。
	return repositorydto.CodexMainQuotaObservation{
		AuthIndex:        authIndex,
		WindowRole:       role,
		WindowSeconds:    windowSeconds,
		ResetAtSource:    "absolute",
		ResetAt:          resetAt,
		RemainingPercent: remaining,
		FirstObservedAt:  observedAt,
		LastObservedAt:   observedAt,
		ObservationCount: 1,
	}
}

func openCodexQuotaHistoryRepositoryDatabase(t *testing.T, name string) *gorm.DB {
	t.Helper()
	// 每个用例使用真实文件 SQLite，确保 writer 路由、WAL、foreign_keys 和 migration 与生产一致。
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), name)})
	if err != nil {
		t.Fatalf("open codex quota history repository database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get codex quota history repository sql database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func loadCodexQuotaHistoryRows(t *testing.T, db *gorm.DB, authIndex string, role string) (entities.QuotaCycle, []entities.QuotaPercentSegment) {
	t.Helper()
	// 当前测试账号与角色只应存在一个目标周期；Take 让缺行直接成为测试失败。
	var cycle entities.QuotaCycle
	quotaKey := "rate_limit.primary_window"
	if role == "secondary" {
		quotaKey = "rate_limit.secondary_window"
	}
	if err := db.Where("provider = ? AND auth_index = ? AND quota_key = ?", "codex", authIndex, quotaKey).Order("reset_at desc, id desc").Take(&cycle).Error; err != nil {
		t.Fatalf("load codex quota cycle: %v", err)
	}
	// 百分比段按首次观察时间读取，固定真实状态变化顺序而不是按整数值排序。
	return cycle, loadCodexQuotaHistorySegments(t, db, cycle.ID)
}

func loadCodexQuotaHistorySegments(t *testing.T, db *gorm.DB, cycleID int64) []entities.QuotaPercentSegment {
	t.Helper()
	// 子表始终按首次观察时间读取，供单周期与容差边界测试复用真实状态变化顺序。
	var segments []entities.QuotaPercentSegment
	if err := db.Where("cycle_id = ?", cycleID).Order("first_observed_at asc, id asc").Find(&segments).Error; err != nil {
		t.Fatalf("load codex quota percent segments: %v", err)
	}
	return segments
}
