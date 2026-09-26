package test

import (
	"reflect"
	"time"
	"unsafe"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	. "cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/repository/dto"
	"gorm.io/gorm"
)

// 以下入口保留原统计 oracle 和固定边界测试，不向生产包增加导出接口。
//
//go:linkname computeWindowMinutes cpa-usage-keeper/internal/repository.computeWindowMinutes
func computeWindowMinutes(filter dto.UsageQueryFilter) int64

//go:linkname shouldBucketUsageOverviewByDay cpa-usage-keeper/internal/repository.shouldBucketUsageOverviewByDay
func shouldBucketUsageOverviewByDay(filter dto.UsageQueryFilter, windowMinutes int64) bool

//go:linkname newUsageOverviewRecord cpa-usage-keeper/internal/repository.newUsageOverviewRecord
func newUsageOverviewRecord(windowMinutes int64) *dto.UsageOverviewRecord

//go:linkname applyUsageEventToOverviewSnapshot cpa-usage-keeper/internal/repository.applyUsageEventToOverviewSnapshot
func applyUsageEventToOverviewSnapshot(snapshot *dto.StatisticsSnapshot, event entities.UsageEvent)

// 生产辅助函数增加了可选的身份查询切片，此处保持可变参数声明以匹配调用约定。
//
//go:linkname applyUsageEventToOverview cpa-usage-keeper/internal/repository.applyUsageEventToOverview
func applyUsageEventToOverview(overview *dto.UsageOverviewRecord, event entities.UsageEvent, bucketByDay bool, costResolver pricing.Resolver, identityLookups ...any)

//go:linkname finalizeUsageOverview cpa-usage-keeper/internal/repository.finalizeUsageOverview
func finalizeUsageOverview(overview *dto.UsageOverviewRecord)

//go:linkname applyUsageOverviewQuery cpa-usage-keeper/internal/repository.applyUsageOverviewQuery
func applyUsageOverviewQuery(query *gorm.DB, filter dto.UsageQueryFilter) *gorm.DB

//go:linkname usageOverviewBucket cpa-usage-keeper/internal/repository.usageOverviewBucket
func usageOverviewBucket(timestamp time.Time, byDay bool) (string, int64)

//go:linkname usageOverviewRealtimeWindow cpa-usage-keeper/internal/repository.usageOverviewRealtimeWindow
func usageOverviewRealtimeWindow(value string) (time.Duration, time.Duration)

//go:linkname usageOverviewRealtimeWindowLabel cpa-usage-keeper/internal/repository.usageOverviewRealtimeWindowLabel
func usageOverviewRealtimeWindowLabel(window time.Duration) string

//go:linkname usageOverviewRealtimeAggregationWindow cpa-usage-keeper/internal/repository.usageOverviewRealtimeAggregationWindow
func usageOverviewRealtimeAggregationWindow(window time.Duration) time.Duration

//go:linkname usageOverviewRealtimeAggregationBucketCount cpa-usage-keeper/internal/repository.usageOverviewRealtimeAggregationBucketCount
func usageOverviewRealtimeAggregationBucketCount(span, aggregationWindow time.Duration) int

//go:linkname usageOverviewRealtimeScatterPointRange cpa-usage-keeper/internal/repository.usageOverviewRealtimeScatterPointRange
func usageOverviewRealtimeScatterPointRange(index, pointCount, maxPoints int) (int, int)

//go:linkname newEmptyUsageRecentEventCache cpa-usage-keeper/internal/repository.newEmptyUsageRecentEventCache
func newEmptyUsageRecentEventCache(opts UsageRecentEventCacheOptions) *UsageRecentEventCache

//go:linkname appendRecentCacheEvents cpa-usage-keeper/internal/repository.(*UsageRecentEventCache).appendEvents
func appendRecentCacheEvents(cache *UsageRecentEventCache, events []entities.UsageEvent)

func recentCacheField[T any](cache *UsageRecentEventCache, name string) *T {
	field := reflect.ValueOf(cache).Elem().FieldByName(name)
	return (*T)(unsafe.Pointer(field.UnsafeAddr()))
}

func hasCachedCredentialHealthKey(cache *UsageRecentEventCache, authType, authIndex string) bool {
	buckets := reflect.ValueOf(cache).Elem().FieldByName("credentialHealth").FieldByName("bucketsByCredential")
	for _, key := range buckets.MapKeys() {
		if key.FieldByName("authType").String() == authType && key.FieldByName("authIndex").String() == authIndex {
			return true
		}
	}
	return false
}

// 只传递切片头，不读写、遍历或重新分配元素，避免复制生产私有行结构的内存布局。
// 窗口行交回原生产聚合函数；健康行仅统计批次长度，字段值经公开健康快照验证。
type opaqueWindowTokenRows []struct{}
type opaqueCredentialHealthRows []struct{}

//go:linkname sumLongUsageWindowTokenStats cpa-usage-keeper/internal/repository.sumLongUsageWindowTokenStats
func sumLongUsageWindowTokenStats(db *gorm.DB, authIndex string, start, end time.Time, activeFields pricing.ActiveFields) (opaqueWindowTokenRows, error)

//go:linkname usageWindowStatsFromTokenStats cpa-usage-keeper/internal/repository.usageWindowStatsFromTokenStats
func usageWindowStatsFromTokenStats(rows opaqueWindowTokenRows, costResolver pricing.Resolver) UsageWindowStats

//go:linkname loadCredentialHealthCacheRowsBatched cpa-usage-keeper/internal/repository.loadCredentialHealthCacheRowsBatched
func loadCredentialHealthCacheRowsBatched(db *gorm.DB, start time.Time, batchSize int, handle func(opaqueCredentialHealthRows) error) error
