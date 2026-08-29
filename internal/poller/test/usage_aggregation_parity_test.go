package poller_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/repository"

	"gorm.io/gorm"
)

func TestUsageAggregationRunnerPreservesExistingOverviewAndIdentityFinalSnapshots(t *testing.T) {
	// 准备：两个独立数据库写入完全相同的多维度事件和超过一页的 active/deleted identities。
	previousLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previousLocal })
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	baselineDB := openUsageAggregationRunnerDatabase(t)
	runnerDB := openUsageAggregationRunnerDatabase(t)
	seedUsageAggregationParityDatabase(t, baselineDB, now)
	seedUsageAggregationParityDatabase(t, runnerDB, now)

	// 执行：基准库调用当前完整 catch-up，候选库按 Runner 的 rollups→Identity 有界 turn 轮转。
	if err := repository.AggregateUsageOverviewStats(context.Background(), baselineDB, now); err != nil {
		t.Fatalf("aggregate baseline overview: %v", err)
	}
	if err := repository.AggregateUsageIdentityStats(context.Background(), baselineDB, now); err != nil {
		t.Fatalf("aggregate baseline identities: %v", err)
	}
	runner := poller.NewUsageAggregationRunner(runnerDB)
	for transaction := 0; transaction < 6; transaction++ {
		if _, err := runner.RunOnce(context.Background()); err != nil {
			t.Fatalf("runner transaction %d: %v", transaction+1, err)
		}
	}

	// 断言：结合 repository 固定 main golden 的 characterization tests，逐字段证明 Runner 拆批不改变旧业务结果。
	baseline := loadUsageAggregationParitySnapshot(t, baselineDB)
	candidate := loadUsageAggregationParitySnapshot(t, runnerDB)
	assertUsageAggregationParitySnapshot(t, candidate, baseline)
}

func assertUsageAggregationParitySnapshot(t *testing.T, candidate, baseline usageAggregationParitySnapshot) {
	// 断言：先单独比较 Overview checkpoint，避免表行很多时淹没真正差异。
	t.Helper()
	if candidate.OverviewCursor != baseline.OverviewCursor || candidate.OverviewStatsUpdated != baseline.OverviewStatsUpdated {
		t.Fatalf("runner changed overview checkpoint: candidate cursor=%d stats_updated=%v; baseline cursor=%d stats_updated=%v", candidate.OverviewCursor, candidate.OverviewStatsUpdated, baseline.OverviewCursor, baseline.OverviewStatsUpdated)
	}
	// 断言：三个旧表的行数必须完全一致。
	if len(candidate.Hourly) != len(baseline.Hourly) || len(candidate.Daily) != len(baseline.Daily) || len(candidate.Identities) != len(baseline.Identities) {
		t.Fatalf("runner changed aggregation row counts: candidate hourly=%d daily=%d identities=%d; baseline hourly=%d daily=%d identities=%d", len(candidate.Hourly), len(candidate.Daily), len(candidate.Identities), len(baseline.Hourly), len(baseline.Daily), len(baseline.Identities))
	}
	// 断言：按稳定排序逐行比较 hourly 的全部旧业务字段。
	for index := range baseline.Hourly {
		if !reflect.DeepEqual(candidate.Hourly[index], baseline.Hourly[index]) {
			t.Fatalf("runner changed hourly row %d: candidate=%+v baseline=%+v", index, candidate.Hourly[index], baseline.Hourly[index])
		}
	}
	// 断言：按稳定排序逐行比较 daily 的全部旧业务字段。
	for index := range baseline.Daily {
		if !reflect.DeepEqual(candidate.Daily[index], baseline.Daily[index]) {
			t.Fatalf("runner changed daily row %d: candidate=%+v baseline=%+v", index, candidate.Daily[index], baseline.Daily[index])
		}
	}
	// 断言：按 identity 业务键逐行比较旧统计、cursor 和首尾时间。
	for index := range baseline.Identities {
		if !reflect.DeepEqual(candidate.Identities[index], baseline.Identities[index]) {
			t.Fatalf("runner changed identity row %d: candidate=%+v baseline=%+v", index, candidate.Identities[index], baseline.Identities[index])
		}
	}
}

type usageAggregationParitySnapshot struct {
	// Hourly 保存全部旧 Overview hourly 业务字段。
	Hourly []usageAggregationOverviewRow
	// Daily 保存全部旧 Overview daily 业务字段。
	Daily []usageAggregationOverviewRow
	// OverviewCursor 保存旧 checkpoint 最终 usage event ID。
	OverviewCursor int64
	// OverviewStatsUpdated 保存旧 checkpoint 已经记录业务更新时间的状态。
	OverviewStatsUpdated bool
	// Identities 保存 active/deleted 每行全部旧统计字段和 cursor。
	Identities []usageAggregationIdentityRow
}

type usageAggregationOverviewRow struct {
	// BucketStart 是旧聚合行的时间维度。
	BucketStart time.Time
	// APIGroupKey 是旧聚合行 API group 维度。
	APIGroupKey string
	// Model 是旧聚合行 model 维度。
	Model string
	// AuthIndex 是旧聚合行 auth 维度。
	AuthIndex string
	// ModelAlias 是旧聚合行 alias 维度。
	ModelAlias string
	// RequestCount 是旧请求总数。
	RequestCount int64
	// SuccessCount 是旧成功数。
	SuccessCount int64
	// FailureCount 是旧失败数。
	FailureCount int64
	// InputTokens 是旧 input token 累计。
	InputTokens int64
	// OutputTokens 是旧 output token 累计。
	OutputTokens int64
	// ReasoningTokens 是旧 reasoning token 累计。
	ReasoningTokens int64
	// CachedTokens 是必须继续保留的旧兼容字段累计。
	CachedTokens int64
	// CacheReadTokens 是旧 cache read 累计。
	CacheReadTokens int64
	// CacheCreationTokens 是旧 cache creation 累计。
	CacheCreationTokens int64
	// TotalTokens 是旧 total token 累计。
	TotalTokens int64
}

type usageAggregationIdentityRow struct {
	// Identity 是每行稳定业务键。
	Identity string
	// IsDeleted 保留 deleted identity 的原聚合语义。
	IsDeleted bool
	// TotalRequests 是旧 identity 请求总数。
	TotalRequests int64
	// SuccessCount 是旧 identity 成功数。
	SuccessCount int64
	// FailureCount 是旧 identity 失败数。
	FailureCount int64
	// InputTokens 是旧 identity input token 累计。
	InputTokens int64
	// OutputTokens 是旧 identity output token 累计。
	OutputTokens int64
	// ReasoningTokens 是旧 identity reasoning token 累计。
	ReasoningTokens int64
	// CachedTokens 是旧 identity 必须继续维护的兼容字段。
	CachedTokens int64
	// CacheReadTokens 是旧 identity cache read 累计。
	CacheReadTokens int64
	// TotalTokens 是旧 identity total token 累计。
	TotalTokens int64
	// Cursor 是该 identity 独立 usage event checkpoint。
	Cursor int64
	// FirstUsedAt 是该 identity 最早事件时间。
	FirstUsedAt *time.Time
	// LastUsedAt 是该 identity 最晚事件时间。
	LastUsedAt *time.Time
	// StatsUpdated 表示旧 identity 已记录本轮真正推进时间。
	StatsUpdated bool
}

func seedUsageAggregationParityDatabase(t *testing.T, db *gorm.DB, now time.Time) {
	// 准备：27 行强制 Identity runner 分两页提交，1001 条事件强制 Overview/Activity 各分两批。
	t.Helper()
	identities := make([]entities.UsageIdentity, 0, 27)
	events := make([]entities.UsageEvent, 0, 1001)
	for index := 1; index <= 27; index++ {
		identity := fmt.Sprintf("parity-auth-%02d", index)
		identities = append(identities, entities.UsageIdentity{
			Name: identity, AuthType: entities.UsageIdentityAuthTypeAuthFile, Identity: identity, Type: "codex", IsDeleted: index%2 == 0,
		})
	}
	// 准备：事件循环复用 27 个 identity，并覆盖多 API group、model、alias、成功失败和全部旧 Token 字段。
	for index := 1; index <= 1001; index++ {
		identityIndex := (index-1)%27 + 1
		identity := fmt.Sprintf("parity-auth-%02d", identityIndex)
		alias := fmt.Sprintf("alias-%d", index%3)
		events = append(events, entities.UsageEvent{
			EventKey: fmt.Sprintf("parity-event-%04d", index), APIGroupKey: fmt.Sprintf("provider-%d", index%2),
			Model: fmt.Sprintf("model-%d", index%4), ModelAlias: &alias, AuthType: "oauth", AuthIndex: identity,
			Timestamp: now.Add(-time.Duration(index%27+1) * time.Minute), Failed: index%5 == 0,
			InputTokens: int64(index), OutputTokens: int64(index * 2), ReasoningTokens: int64(index * 3),
			CachedTokens: int64(index * 11), CacheReadTokens: int64(index * 5), CacheCreationTokens: int64(index * 7), TotalTokens: int64(index * 13),
		})
	}
	// 准备：先建 identity，确保两条执行路径读取相同 ID 页边界。
	if err := db.Create(&identities).Error; err != nil {
		t.Fatalf("seed parity identities: %v", err)
	}
	// 准备：再写 usage events，确保两个数据库自增 ID 完全一致。
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("seed parity usage events: %v", err)
	}
}

func loadUsageAggregationParitySnapshot(t *testing.T, db *gorm.DB) usageAggregationParitySnapshot {
	// 执行：按业务维度稳定排序读取两个旧 Overview 表。
	t.Helper()
	var hourly []entities.UsageOverviewHourlyStat
	if err := db.Order("bucket_start asc, api_group_key asc, model asc, auth_index asc, model_alias asc").Find(&hourly).Error; err != nil {
		t.Fatalf("load parity hourly rows: %v", err)
	}
	var daily []entities.UsageOverviewDailyStat
	if err := db.Order("bucket_start asc, api_group_key asc, model asc, auth_index asc, model_alias asc").Find(&daily).Error; err != nil {
		t.Fatalf("load parity daily rows: %v", err)
	}
	// 执行：读取唯一 Overview checkpoint。
	var checkpoint entities.UsageAggregationCheckpoint
	if err := db.Where("name = ?", entities.UsageAggregationCheckpointOverview).Take(&checkpoint).Error; err != nil {
		t.Fatalf("load parity overview checkpoint: %v", err)
	}
	// 执行：按稳定 identity 业务键读取 active/deleted 行。
	var identities []entities.UsageIdentity
	if err := db.Order("identity asc").Find(&identities).Error; err != nil {
		t.Fatalf("load parity identities: %v", err)
	}
	// 断言准备：机械转换为不含自增 ID 和调度时间戳的业务快照。
	// 调度改为异步后实际执行时间可以不同，跨路径只比较旧 timestamp 字段是否被正确推进。
	snapshot := usageAggregationParitySnapshot{OverviewCursor: checkpoint.LastAggregatedUsageEventID, OverviewStatsUpdated: checkpoint.StatsUpdatedAt != nil}
	for _, row := range hourly {
		snapshot.Hourly = append(snapshot.Hourly, usageAggregationOverviewRowFromHourly(row))
	}
	for _, row := range daily {
		snapshot.Daily = append(snapshot.Daily, usageAggregationOverviewRowFromDaily(row))
	}
	for _, row := range identities {
		snapshot.Identities = append(snapshot.Identities, usageAggregationIdentityRow{
			Identity: row.Identity, IsDeleted: row.IsDeleted, TotalRequests: row.TotalRequests,
			SuccessCount: row.SuccessCount, FailureCount: row.FailureCount, InputTokens: row.InputTokens,
			OutputTokens: row.OutputTokens, ReasoningTokens: row.ReasoningTokens, CachedTokens: row.CachedTokens,
			CacheReadTokens: row.CacheReadTokens, TotalTokens: row.TotalTokens, Cursor: row.LastAggregatedUsageEventID,
			FirstUsedAt: row.FirstUsedAt, LastUsedAt: row.LastUsedAt,
			StatsUpdated: row.StatsUpdatedAt != nil,
		})
	}
	// 返回可直接 DeepEqual 的完整旧聚合业务快照。
	return snapshot
}

func usageAggregationOverviewRowFromHourly(row entities.UsageOverviewHourlyStat) usageAggregationOverviewRow {
	// hourly 到统一快照只做旧字段机械复制。
	return usageAggregationOverviewRow{
		BucketStart: row.BucketStart, APIGroupKey: row.APIGroupKey, Model: row.Model, AuthIndex: row.AuthIndex, ModelAlias: row.ModelAlias,
		RequestCount: row.RequestCount, SuccessCount: row.SuccessCount, FailureCount: row.FailureCount,
		InputTokens: row.InputTokens, OutputTokens: row.OutputTokens, ReasoningTokens: row.ReasoningTokens,
		CachedTokens: row.CachedTokens, CacheReadTokens: row.CacheReadTokens, CacheCreationTokens: row.CacheCreationTokens, TotalTokens: row.TotalTokens,
	}
}

func usageAggregationOverviewRowFromDaily(row entities.UsageOverviewDailyStat) usageAggregationOverviewRow {
	// daily 到统一快照只做旧字段机械复制。
	return usageAggregationOverviewRow{
		BucketStart: row.BucketStart, APIGroupKey: row.APIGroupKey, Model: row.Model, AuthIndex: row.AuthIndex, ModelAlias: row.ModelAlias,
		RequestCount: row.RequestCount, SuccessCount: row.SuccessCount, FailureCount: row.FailureCount,
		InputTokens: row.InputTokens, OutputTokens: row.OutputTokens, ReasoningTokens: row.ReasoningTokens,
		CachedTokens: row.CachedTokens, CacheReadTokens: row.CacheReadTokens, CacheCreationTokens: row.CacheCreationTokens, TotalTokens: row.TotalTokens,
	}
}
