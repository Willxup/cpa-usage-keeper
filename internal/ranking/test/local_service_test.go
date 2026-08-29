package test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/ranking"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type localRankingQueryCounter struct {
	usageEventReads int
}

type localProfileUpdateNameContextKey struct{}

func (l *localRankingQueryCounter) LogMode(logger.LogLevel) logger.Interface { return l }
func (l *localRankingQueryCounter) Info(context.Context, string, ...any)     {}
func (l *localRankingQueryCounter) Warn(context.Context, string, ...any)     {}
func (l *localRankingQueryCounter) Error(context.Context, string, ...any)    {}
func (l *localRankingQueryCounter) Trace(_ context.Context, _ time.Time, sql func() (string, int64), _ error) {
	statement, _ := sql()
	if strings.Contains(statement, "FROM usage_events") {
		l.usageEventReads++
	}
}

func TestLocalRankingServiceBuildsTodayWithoutBackfillingOlderPeriods(t *testing.T) {
	location := localRankingLocation(t)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, location)
	db := openLocalRankingDatabase(t, "today-only.db")
	keys := seedLocalRankingAPIKeys(t, db)
	ttftA := int64(100)
	ttftB := int64(600)
	events := []entities.UsageEvent{
		{EventKey: "a-today-1", APIGroupKey: keys[0].APIKey, Model: "gpt-5", Timestamp: now.Add(-4 * time.Minute), LatencyMS: 900, TTFTMS: &ttftA, InputTokens: 100, CacheReadTokens: 80, TotalTokens: 300},
		{EventKey: "a-today-2", APIGroupKey: keys[0].APIKey, Model: "gpt-5", Timestamp: now.Add(-3 * time.Minute), LatencyMS: 1_000, TTFTMS: &ttftA, InputTokens: 200, CacheReadTokens: 160, TotalTokens: 500},
		{EventKey: "b-today-ok", APIGroupKey: keys[1].APIKey, Model: "gpt-5", Timestamp: now.Add(-2 * time.Minute), LatencyMS: 3_000, TTFTMS: &ttftB, InputTokens: 100, CacheReadTokens: 10, TotalTokens: 100},
		{EventKey: "b-today-fail", APIGroupKey: keys[1].APIKey, Model: "gpt-5", Timestamp: now.Add(-time.Minute), Failed: true, InputTokens: 20, TotalTokens: 20},
		{EventKey: "a-yesterday", APIGroupKey: keys[0].APIKey, Model: "gpt-5", Timestamp: now.AddDate(0, 0, -1), TotalTokens: 150},
		{EventKey: "a-previous-month", APIGroupKey: keys[0].APIKey, Model: "gpt-5", Timestamp: time.Date(2026, 6, 20, 9, 0, 0, 0, location), TotalTokens: 120},
	}
	insertLocalRankingEvents(t, db, events)
	service := newLocalRankingService(t, db, func() time.Time { return now })
	if err := service.AggregateOnce(context.Background()); err != nil {
		t.Fatalf("aggregate local ranking today: %v", err)
	}

	today := loadLocalBoard(t, service, ranking.LeaderboardToday, ranking.MetricTotalTokens)
	if today.Stale || len(today.Entries) != 2 || today.Entries[0].ParticipantID != "1" || today.Entries[0].DisplayName != "Alpha" || today.Entries[0].Value != 800 {
		t.Fatalf("unexpected today total tokens board: %+v", today)
	}
	if strings.Contains(today.Entries[1].DisplayName, keys[1].APIKey) || today.Entries[1].DisplayName == keys[1].APIKey {
		t.Fatalf("local leaderboard exposed a full API Key: %+v", today.Entries[1])
	}
	cacheRate := loadLocalBoard(t, service, ranking.LeaderboardToday, ranking.MetricCacheReadRate)
	if len(cacheRate.Entries) != 2 || cacheRate.Entries[1].Value != 83_333 {
		t.Fatalf("local cache rate did not use Community fixed-point semantics: %+v", cacheRate)
	}
	currentMonth := loadLocalBoard(t, service, ranking.LeaderboardCurrentMonth, ranking.MetricRequestCount)
	if len(currentMonth.Entries) != 2 || currentMonth.Entries[0].Value != 2 || currentMonth.Entries[1].Value != 2 {
		t.Fatalf("current month should initially equal today: %+v", currentMonth)
	}
	if board := loadLocalBoard(t, service, ranking.LeaderboardYesterday, ranking.MetricRequestCount); len(board.Entries) != 0 {
		t.Fatalf("first activation backfilled yesterday: %+v", board)
	}
	if board := loadLocalBoard(t, service, ranking.LeaderboardPreviousMonth, ranking.MetricRequestCount); len(board.Entries) != 0 {
		t.Fatalf("first activation backfilled previous month: %+v", board)
	}

	var alphaRows int64
	if err := db.Model(&entities.LocalRankingPeriodStat{}).Where("api_key_id = ?", keys[0].ID).Count(&alphaRows).Error; err != nil {
		t.Fatalf("count local ranking rows: %v", err)
	}
	if alphaRows != 2 {
		t.Fatalf("first activation should create only today and current month, got %d rows", alphaRows)
	}

	overall := loadLocalBoard(t, service, ranking.LeaderboardToday, ranking.MetricOverall)
	if len(overall.Entries) != 2 || overall.Entries[0].ParticipantID != "1" || overall.ScoreExplanation == nil || overall.ScoreExplanation.Version != 2 {
		t.Fatalf("unexpected local overall board: %+v", overall)
	}
	for _, entry := range overall.Entries {
		if entry.Value < 0 || entry.Value > 100 || len(entry.Metrics) != 7 {
			t.Fatalf("invalid local overall entry: %+v", entry)
		}
	}
}

func TestLocalRankingProfileKeepsDefaultAvatarUntilAnOverrideIsSaved(t *testing.T) {
	location := localRankingLocation(t)
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, location)
	db := openLocalRankingDatabase(t, "local-profile.db")
	keys := seedLocalRankingAPIKeys(t, db)
	if err := db.Create(&entities.LocalRankingPeriodStat{
		PeriodKind: entities.LocalRankingPeriodDay, PeriodKey: "2026-08-03", APIKeyID: keys[0].ID,
		RequestCount: 1, TotalTokens: 100, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed local ranking profile stats: %v", err)
	}
	service := newLocalRankingService(t, db, func() time.Time { return now })

	before := loadLocalBoard(t, service, ranking.LeaderboardToday, ranking.MetricTotalTokens)
	if len(before.Entries) != 1 || before.Entries[0].AvatarID != 1 || before.Entries[0].KeyAlias != "Alpha" {
		t.Fatalf("default local profile did not use the stable ID mapping: %+v", before.Entries)
	}

	profile, err := service.UpdateProfile(context.Background(), keys[0].ID, "  Renamed Key  ", 42)
	if err != nil {
		t.Fatalf("update local ranking profile: %v", err)
	}
	if profile.ParticipantID != "1" || profile.KeyAlias != "Renamed Key" || profile.DisplayName != "Renamed Key" || profile.AvatarID != 42 {
		t.Fatalf("unexpected updated local profile: %+v", profile)
	}
	after := loadLocalBoard(t, service, ranking.LeaderboardToday, ranking.MetricTotalTokens)
	if len(after.Entries) != 1 || after.Entries[0].DisplayName != "Renamed Key" || after.Entries[0].KeyAlias != "Renamed Key" || after.Entries[0].AvatarID != 42 {
		t.Fatalf("saved local profile was not projected into the board: %+v", after.Entries)
	}
	var stored entities.CPAAPIKey
	if err := db.First(&stored, keys[0].ID).Error; err != nil {
		t.Fatalf("reload local ranking profile: %v", err)
	}
	if stored.LocalRankingAvatarID == nil || *stored.LocalRankingAvatarID != 42 || stored.KeyAlias != "Renamed Key" {
		t.Fatalf("local ranking profile was not bound to the key: %+v", stored)
	}

	for _, invalid := range []struct {
		name     string
		alias    string
		avatarID uint8
	}{
		{name: "missing avatar", alias: "Valid", avatarID: 0},
		{name: "avatar outside catalog", alias: "Valid", avatarID: 67},
		{name: "control character", alias: "Bad\u0001Alias", avatarID: 1},
		{name: "long alias", alias: strings.Repeat("a", 129), avatarID: 1},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			if _, err := service.UpdateProfile(context.Background(), keys[0].ID, invalid.alias, invalid.avatarID); !errors.Is(err, ranking.ErrInvalidLocalProfile) {
				t.Fatalf("UpdateProfile error = %v, want ErrInvalidLocalProfile", err)
			}
		})
	}
}

func TestLocalRankingProfileConcurrentUpdatesReturnCoherentRows(t *testing.T) {
	db := openLocalRankingDatabase(t, "local-profile-concurrent.db")
	keys := seedLocalRankingAPIKeys(t, db)
	firstQueryReached := make(chan struct{})
	secondUpdateDone := make(chan struct{})
	if err := db.Callback().Query().Before("gorm:query").Register("test:block_non_transactional_profile_read", func(tx *gorm.DB) {
		if tx.Statement.Context.Value(localProfileUpdateNameContextKey{}) != "first" {
			return
		}
		close(firstQueryReached)
		// 旧实现的回读已经离开写事务，可以让第二次保存插入其间；事务内回读则无需阻塞。
		if _, inTransaction := tx.Statement.ConnPool.(*sql.Tx); !inTransaction {
			<-secondUpdateDone
		}
	}); err != nil {
		t.Fatalf("register local profile concurrency callback: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove("test:block_non_transactional_profile_read")
	})
	service := newLocalRankingService(t, db, time.Now)
	type updateResult struct {
		profile ranking.LocalProfile
		err     error
	}
	firstResult := make(chan updateResult, 1)
	go func() {
		profile, err := service.UpdateProfile(
			context.WithValue(context.Background(), localProfileUpdateNameContextKey{}, "first"),
			keys[0].ID,
			"First",
			42,
		)
		firstResult <- updateResult{profile: profile, err: err}
	}()
	<-firstQueryReached
	secondProfile, secondErr := service.UpdateProfile(context.Background(), keys[0].ID, "Second", 43)
	close(secondUpdateDone)
	first := <-firstResult
	if first.err != nil || secondErr != nil {
		t.Fatalf("concurrent local profile updates failed: first=%v second=%v", first.err, secondErr)
	}
	for name, profile := range map[string]ranking.LocalProfile{
		"first":  first.profile,
		"second": secondProfile,
	} {
		coherent := (profile.KeyAlias == "First" && profile.DisplayName == "First" && profile.AvatarID == 42) ||
			(profile.KeyAlias == "Second" && profile.DisplayName == "Second" && profile.AvatarID == 43)
		if !coherent {
			t.Fatalf("%s update returned a mixed profile: %+v", name, profile)
		}
	}
}

func TestLocalRankingServiceReplacesCompleteTodaySnapshotWithoutDoubleCounting(t *testing.T) {
	location := localRankingLocation(t)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, location)
	db := openLocalRankingDatabase(t, "replace-today.db")
	keys := seedLocalRankingAPIKeys(t, db)
	queryCounter := &localRankingQueryCounter{}
	serviceDB := db.Session(&gorm.Session{Logger: queryCounter})
	ttft := int64(120)
	insertLocalRankingEvents(t, db, []entities.UsageEvent{
		{EventKey: "base-1", APIGroupKey: keys[0].APIKey, Model: "gpt-5", Timestamp: now.Add(-4 * time.Minute), LatencyMS: 900, TTFTMS: &ttft, InputTokens: 100, CacheReadTokens: 50, TotalTokens: 100},
		{EventKey: "base-2", APIGroupKey: keys[0].APIKey, Model: "gpt-5", Timestamp: now.Add(-3 * time.Minute), LatencyMS: 900, TTFTMS: &ttft, InputTokens: 100, CacheReadTokens: 50, TotalTokens: 200},
	})
	service := newLocalRankingService(t, serviceDB, func() time.Time { return now })
	if err := service.AggregateOnce(context.Background()); err != nil {
		t.Fatalf("aggregate initial today snapshot: %v", err)
	}
	if queryCounter.usageEventReads != 1 {
		t.Fatalf("initial local ranking aggregation read usage_events %d times, want 1", queryCounter.usageEventReads)
	}

	now = now.Add(5 * time.Minute)
	insertLocalRankingEvents(t, db, []entities.UsageEvent{{
		EventKey: "base-3", APIGroupKey: keys[0].APIKey, Model: "gpt-5", Timestamp: now.Add(-7 * time.Minute),
		LatencyMS: 900, TTFTMS: &ttft, InputTokens: 100, CacheReadTokens: 50, TotalTokens: 400,
	}})
	if err := service.AggregateOnce(context.Background()); err != nil {
		t.Fatalf("replace today snapshot: %v", err)
	}
	if queryCounter.usageEventReads != 2 {
		t.Fatalf("changed local ranking aggregation read usage_events %d total times, want 2", queryCounter.usageEventReads)
	}
	for _, period := range []ranking.LeaderboardPeriod{ranking.LeaderboardToday, ranking.LeaderboardCurrentMonth} {
		board := loadLocalBoard(t, service, period, ranking.MetricTotalTokens)
		if len(board.Entries) != 1 || board.Entries[0].Value != 700 {
			t.Fatalf("%s snapshot was double counted: %+v", period, board)
		}
	}
	peak := loadLocalBoard(t, service, ranking.LeaderboardToday, ranking.MetricPeakTPM)
	if len(peak.Entries) != 1 || peak.Entries[0].Value != 700 {
		t.Fatalf("full-day aggregation did not recompute the complete five-minute bucket: %+v", peak)
	}

	var before entities.LocalRankingPeriodStat
	if err := db.First(&before, "period_kind = ? AND period_key = ? AND api_key_id = ?", entities.LocalRankingPeriodDay, "2026-07-31", keys[0].ID).Error; err != nil {
		t.Fatalf("load today row before unchanged run: %v", err)
	}
	now = now.Add(5 * time.Minute)
	if err := service.AggregateOnce(context.Background()); err != nil {
		t.Fatalf("repeat unchanged aggregation: %v", err)
	}
	if queryCounter.usageEventReads != 3 {
		t.Fatalf("unchanged local ranking aggregation read usage_events %d total times, want 3", queryCounter.usageEventReads)
	}
	var after entities.LocalRankingPeriodStat
	if err := db.First(&after, "period_kind = ? AND period_key = ? AND api_key_id = ?", entities.LocalRankingPeriodDay, "2026-07-31", keys[0].ID).Error; err != nil {
		t.Fatalf("load today row after unchanged run: %v", err)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("unchanged source rewrote the snapshot: before=%s after=%s", before.UpdatedAt, after.UpdatedAt)
	}
}

func TestLocalRankingServiceSettlesYesterdayAndRollsItIntoCurrentMonth(t *testing.T) {
	location := localRankingLocation(t)
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, location)
	db := openLocalRankingDatabase(t, "day-settlement.db")
	keys := seedLocalRankingAPIKeys(t, db)
	insertLocalRankingEvents(t, db, []entities.UsageEvent{{EventKey: "day-base", APIGroupKey: keys[0].APIKey, Model: "gpt-5", Timestamp: now.Add(-time.Minute), TotalTokens: 100}})
	service := newLocalRankingService(t, db, func() time.Time { return now })
	if err := service.AggregateOnce(context.Background()); err != nil {
		t.Fatalf("aggregate first dynamic day: %v", err)
	}

	insertLocalRankingEvents(t, db, []entities.UsageEvent{{EventKey: "day-late", APIGroupKey: keys[0].APIKey, Model: "gpt-5", Timestamp: time.Date(2026, 7, 14, 23, 59, 0, 0, location), TotalTokens: 50}})
	now = time.Date(2026, 7, 15, 1, 0, 0, 0, location)
	insertLocalRankingEvents(t, db, []entities.UsageEvent{{EventKey: "next-day", APIGroupKey: keys[0].APIKey, Model: "gpt-5", Timestamp: now.Add(-time.Minute), TotalTokens: 10}})
	if err := service.AggregateOnce(context.Background()); err != nil {
		t.Fatalf("aggregate new day before settlement: %v", err)
	}
	if board := loadLocalBoard(t, service, ranking.LeaderboardYesterday, ranking.MetricTotalTokens); len(board.Entries) != 1 || board.Entries[0].Value != 100 {
		t.Fatalf("yesterday should remain the last dynamic snapshot before 02:00: %+v", board)
	}

	now = time.Date(2026, 7, 15, 2, 0, 0, 0, location)
	if err := service.AggregateOnce(context.Background()); err != nil {
		t.Fatalf("settle yesterday: %v", err)
	}
	yesterday := loadLocalBoard(t, service, ranking.LeaderboardYesterday, ranking.MetricTotalTokens)
	if len(yesterday.Entries) != 1 || yesterday.Entries[0].Value != 150 {
		t.Fatalf("daily settlement did not include the final interval: %+v", yesterday)
	}
	month := loadLocalBoard(t, service, ranking.LeaderboardCurrentMonth, ranking.MetricTotalTokens)
	if len(month.Entries) != 1 || month.Entries[0].Value != 160 {
		t.Fatalf("settled yesterday was not rolled into current month exactly once: %+v", month)
	}

	now = now.Add(5 * time.Minute)
	if err := service.AggregateOnce(context.Background()); err != nil {
		t.Fatalf("repeat settled day: %v", err)
	}
	repeated := loadLocalBoard(t, service, ranking.LeaderboardCurrentMonth, ranking.MetricTotalTokens)
	if len(repeated.Entries) != 1 || repeated.Entries[0].Value != 160 {
		t.Fatalf("repeated settlement changed current month: %+v", repeated)
	}
}

func TestLocalRankingServiceMonthSettlementNaturallyCreatesPreviousMonth(t *testing.T) {
	location := localRankingLocation(t)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, location)
	db := openLocalRankingDatabase(t, "month-settlement.db")
	keys := seedLocalRankingAPIKeys(t, db)
	insertLocalRankingEvents(t, db, []entities.UsageEvent{{EventKey: "july-base", APIGroupKey: keys[0].APIKey, Model: "gpt-5", Timestamp: now.Add(-time.Minute), TotalTokens: 100}})
	service := newLocalRankingService(t, db, func() time.Time { return now })
	if err := service.AggregateOnce(context.Background()); err != nil {
		t.Fatalf("aggregate July dynamic day: %v", err)
	}
	insertLocalRankingEvents(t, db, []entities.UsageEvent{{EventKey: "july-late", APIGroupKey: keys[0].APIKey, Model: "gpt-5", Timestamp: time.Date(2026, 7, 31, 23, 59, 0, 0, location), TotalTokens: 50}})

	now = time.Date(2026, 8, 1, 1, 0, 0, 0, location)
	insertLocalRankingEvents(t, db, []entities.UsageEvent{{EventKey: "august", APIGroupKey: keys[0].APIKey, Model: "gpt-5", Timestamp: now.Add(-time.Minute), TotalTokens: 10}})
	if err := service.AggregateOnce(context.Background()); err != nil {
		t.Fatalf("aggregate first August snapshot: %v", err)
	}
	now = time.Date(2026, 8, 1, 2, 0, 0, 0, location)
	if err := service.AggregateOnce(context.Background()); err != nil {
		t.Fatalf("settle previous month: %v", err)
	}

	previousMonth := loadLocalBoard(t, service, ranking.LeaderboardPreviousMonth, ranking.MetricTotalTokens)
	if len(previousMonth.Entries) != 1 || previousMonth.Entries[0].Value != 150 {
		t.Fatalf("previous month was not finalized from settled days: %+v", previousMonth)
	}
	currentMonth := loadLocalBoard(t, service, ranking.LeaderboardCurrentMonth, ranking.MetricTotalTokens)
	if len(currentMonth.Entries) != 1 || currentMonth.Entries[0].Value != 10 {
		t.Fatalf("new current month should contain only August today: %+v", currentMonth)
	}
}

func TestLocalRankingServiceReaggregatesAllKeysWhenMetadataArrives(t *testing.T) {
	location := localRankingLocation(t)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, location)
	db := openLocalRankingDatabase(t, "metadata-delay.db")
	known := entities.CPAAPIKey{APIKey: "sk-known", DisplayKey: "sk-***known", KeyAlias: "Known"}
	if err := db.Create(&known).Error; err != nil {
		t.Fatalf("seed known API key: %v", err)
	}
	insertLocalRankingEvents(t, db, []entities.UsageEvent{
		{EventKey: "known", APIGroupKey: known.APIKey, Model: "gpt-5", Timestamp: now.Add(-2 * time.Minute), TotalTokens: 100},
		{EventKey: "pending", APIGroupKey: "sk-pending", Model: "gpt-5", Timestamp: now.Add(-time.Minute), TotalTokens: 200},
	})
	service := newLocalRankingService(t, db, func() time.Time { return now })
	if err := service.AggregateOnce(context.Background()); err != nil {
		t.Fatalf("aggregate before metadata sync: %v", err)
	}

	pending := entities.CPAAPIKey{APIKey: "sk-pending", DisplayKey: "sk-***pending", KeyAlias: "Pending"}
	if err := db.Create(&pending).Error; err != nil {
		t.Fatalf("sync pending API key: %v", err)
	}
	now = now.Add(5 * time.Minute)
	if err := service.AggregateOnce(context.Background()); err != nil {
		t.Fatalf("aggregate after metadata sync: %v", err)
	}
	board := loadLocalBoard(t, service, ranking.LeaderboardToday, ranking.MetricTotalTokens)
	if len(board.Entries) != 2 || board.Entries[0].DisplayName != "Pending" || board.Entries[0].Value != 200 || board.Entries[1].Value != 100 {
		t.Fatalf("metadata arrival did not refresh the complete all-Key snapshot: %+v", board)
	}
}

func TestLocalRankingServiceFutureEventNeverBlocksAnotherKey(t *testing.T) {
	location := localRankingLocation(t)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, location)
	db := openLocalRankingDatabase(t, "future-event.db")
	keys := seedLocalRankingAPIKeys(t, db)
	// 先插入未来事件，再插入可用事件，确保未来事件 ID 会被更大的正常 ID 覆盖。
	insertLocalRankingEvents(t, db, []entities.UsageEvent{
		{EventKey: "future", APIGroupKey: keys[1].APIKey, Model: "gpt-5", Timestamp: now.Add(time.Minute), TotalTokens: 200},
		{EventKey: "ready", APIGroupKey: keys[0].APIKey, Model: "gpt-5", Timestamp: now.Add(-time.Minute), TotalTokens: 100},
	})
	service := newLocalRankingService(t, db, func() time.Time { return now })
	if err := service.AggregateOnce(context.Background()); err != nil {
		t.Fatalf("aggregate before future event is due: %v", err)
	}
	first := loadLocalBoard(t, service, ranking.LeaderboardToday, ranking.MetricTotalTokens)
	if len(first.Entries) != 1 || first.Entries[0].ParticipantID != "1" || first.Entries[0].Value != 100 {
		t.Fatalf("future event blocked the ready API Key: %+v", first)
	}

	now = now.Add(2 * time.Minute)
	if err := service.AggregateOnce(context.Background()); err != nil {
		t.Fatalf("aggregate after future event is due: %v", err)
	}
	second := loadLocalBoard(t, service, ranking.LeaderboardToday, ranking.MetricTotalTokens)
	if len(second.Entries) != 2 || second.Entries[0].ParticipantID != "2" || second.Entries[0].Value != 200 || second.Entries[1].Value != 100 {
		t.Fatalf("due future event was not included in the next complete snapshot: %+v", second)
	}
}

func TestLocalRankingOverallUsesCommunityDimensionQuantization(t *testing.T) {
	location := localRankingLocation(t)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, location)
	db := openLocalRankingDatabase(t, "overall-quantization.db")
	keys := seedLocalRankingAPIKeys(t, db)
	statsUpdatedAt := now.Add(-time.Minute)
	rows := []entities.LocalRankingPeriodStat{
		{
			PeriodKind: entities.LocalRankingPeriodDay, PeriodKey: "2026-07-31", APIKeyID: keys[0].ID,
			RequestCount: 236, SuccessCount: 226, FailureCount: 10, InputTokens: 2353, CacheReadTokens: 459, TotalTokens: 7911,
			TTFTSumMS: 1713, TTFTSampleCount: 223, LatencySumMS: 8193, LatencySampleCount: 223,
			Peak5MTotalTokens: 5328, Peak5MRequestCount: 228, UpdatedAt: statsUpdatedAt,
		},
		{
			PeriodKind: entities.LocalRankingPeriodDay, PeriodKey: "2026-07-31", APIKeyID: keys[1].ID,
			RequestCount: 355, SuccessCount: 175, FailureCount: 180, InputTokens: 4226, CacheReadTokens: 4205, TotalTokens: 9660,
			TTFTSumMS: 2167, TTFTSampleCount: 143, LatencySumMS: 14381, LatencySampleCount: 143,
			Peak5MTotalTokens: 7794, Peak5MRequestCount: 3, UpdatedAt: statsUpdatedAt,
		},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed overall quantization rows: %v", err)
	}
	service := newLocalRankingService(t, db, func() time.Time { return now })
	board := loadLocalBoard(t, service, ranking.LeaderboardToday, ranking.MetricOverall)
	for _, entry := range board.Entries {
		if entry.ParticipantID == "1" {
			if entry.Value != 52 {
				t.Fatalf("expected Community V2 quantized score 52, got %+v", entry)
			}
			return
		}
	}
	t.Fatalf("missing first API key from overall board: %+v", board.Entries)
}

func openLocalRankingDatabase(t *testing.T, name string) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), name)})
	if err != nil {
		t.Fatalf("open local ranking database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("load local ranking sql database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func seedLocalRankingAPIKeys(t *testing.T, db *gorm.DB) []entities.CPAAPIKey {
	t.Helper()
	rows := []entities.CPAAPIKey{
		{APIKey: "sk-local-alpha", DisplayKey: "sk-*********alpha", KeyAlias: "Alpha"},
		{APIKey: "sk-local-beta", DisplayKey: "sk-**********beta", IsDeleted: true},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("seed local ranking API keys: %v", err)
	}
	return rows
}

func insertLocalRankingEvents(t *testing.T, db *gorm.DB, events []entities.UsageEvent) {
	t.Helper()
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("insert local ranking events: %v", err)
	}
}

func newLocalRankingService(t *testing.T, db *gorm.DB, now func() time.Time) *ranking.LocalRankingService {
	t.Helper()
	service, err := ranking.NewLocalRankingService(db, ranking.LocalRankingServiceOptions{Now: now})
	if err != nil {
		t.Fatalf("create local ranking service: %v", err)
	}
	return service
}

func loadLocalBoard(t *testing.T, service *ranking.LocalRankingService, period ranking.LeaderboardPeriod, metric ranking.LeaderboardMetric) ranking.Leaderboard {
	t.Helper()
	board, err := service.Leaderboard(context.Background(), period, metric)
	if err != nil {
		t.Fatalf("load local leaderboard %s/%s: %v", period, metric, err)
	}
	return board
}

func localRankingLocation(t *testing.T) *time.Location {
	t.Helper()
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load Asia/Shanghai: %v", err)
	}
	return location
}
