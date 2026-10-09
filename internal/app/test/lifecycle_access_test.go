package test

import (
	"reflect"
	"time"
	"unsafe"

	_ "cpa-usage-keeper/internal/app"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

// 只在启动 runner 前设置原有时钟、等待和启动信号，保持生命周期测试的同步边界。
func appTestField[T any](target any, name string) *T {
	field := reflect.ValueOf(target).Elem().FieldByName(name)
	return (*T)(unsafe.Pointer(field.UnsafeAddr()))
}

//go:linkname nextDailyBackupAt cpa-usage-keeper/internal/app.nextDailyBackupAt
func nextDailyBackupAt(now time.Time) time.Time

//go:linkname nextDailyCleanupAt cpa-usage-keeper/internal/app.nextDailyCleanupAt
func nextDailyCleanupAt(now time.Time) time.Time

//go:linkname newUsageRecentEventCache cpa-usage-keeper/internal/app.newUsageRecentEventCache
var newUsageRecentEventCache func(*gorm.DB, repository.UsageRecentEventCacheOptions) (*repository.UsageRecentEventCache, error)
