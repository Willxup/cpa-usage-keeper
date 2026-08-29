package test

import (
	"testing"
	"time"

	keeperapp "cpa-usage-keeper/internal/app"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
)

func TestNewWithConfigKeepsMemoryDatabaseOnOriginalSinglePool(t *testing.T) {
	// 三种受支持内存 DSN 都必须保持旧单池语义，不能依赖 SQLite 官方不推荐的 shared-cache 拆池。
	for name, databasePath := range map[string]string{
		"anonymous":   ":memory:",
		"memory_uri":  "file:keeper-memory-uri?mode=memory&cache=shared",
		"special_uri": "file::memory:?cache=shared",
	} {
		t.Run(name, func(t *testing.T) {
			// 准备：每个子用例构造独立 App，writer 负责建表，所有读取继续复用同一底层池。
			cfg := databasePoolTestConfig(databasePath)
			application, err := keeperapp.NewWithConfig(cfg)
			if err != nil {
				t.Fatalf("NewWithConfig returned error: %v", err)
			}
			closed := false
			t.Cleanup(func() {
				if !closed {
					_ = application.Close()
				}
			})

			// 断言：内存库不能打开第二个 reader，避免产生独立空库或引入 shared-cache 锁语义。
			if application.ReadDB == nil || application.ReadDB != application.DB {
				t.Fatalf("expected memory database to reuse writer pool, write=%p read=%p", application.DB, application.ReadDB)
			}
			// 执行：通过 writer 写入一条事件，再从 App 暴露的 reader 查询同一条记录。
			event := entities.UsageEvent{EventKey: "memory-read-pool-" + name, Model: "model-a", Timestamp: time.Now()}
			if err := application.DB.Create(&event).Error; err != nil {
				t.Fatalf("write memory usage event: %v", err)
			}
			var count int64
			if err := application.ReadDB.Model(&entities.UsageEvent{}).Where("event_key = ?", event.EventKey).Count(&count).Error; err != nil {
				t.Fatalf("read memory usage event: %v", err)
			}
			if count != 1 {
				t.Fatalf("expected memory reader to observe writer data, got %d rows", count)
			}

			// 执行：同一池被两个 App 字段引用时，统一关闭入口必须只关闭一次并保持幂等。
			if err := application.Close(); err != nil {
				t.Fatalf("first Close returned error: %v", err)
			}
			closed = true
			if err := application.Close(); err != nil {
				t.Fatalf("second Close returned error: %v", err)
			}
		})
	}
}

func databasePoolTestConfig(databasePath string) config.Config {
	// App 测试只构造本地资源，不启动 HTTP listener 或任何远端后台任务。
	return config.Config{
		AppPort:                "invalid-port",
		CPABaseURL:             "https://cpa.example.com",
		CPAManagementKey:       "secret",
		RedisQueueIdleInterval: time.Second,
		MetadataSyncInterval:   30 * time.Second,
		SQLitePath:             databasePath,
		RequestTimeout:         5 * time.Second,
		LogLevel:               "info",
		LogFileEnabled:         false,
		LogRetentionDays:       7,
	}
}
