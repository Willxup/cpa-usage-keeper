package test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"cpa-usage-keeper/internal/quota"
)

func BenchmarkBuildUsageHeaderSnapshot(b *testing.B) {
	// 基准 Header 同时包含主额度、Additional 和应被过滤的敏感/无关字段，覆盖生产解析形状。
	headers := http.Header{
		"Date":                                            []string{"Thu, 20 Aug 2026 00:00:00 GMT"},
		"Set-Cookie":                                      []string{"session=secret"},
		"X-Codex-Plan-Type":                               []string{"pro"},
		"X-Codex-Primary-Used-Percent":                    []string{"10.49"},
		"X-Codex-Primary-Window-Minutes":                  []string{"300"},
		"X-Codex-Primary-Reset-After-Seconds":             []string{"3600"},
		"X-Codex-Primary-Reset-At":                        []string{"1787270400"},
		"X-Codex-Secondary-Used-Percent":                  []string{"25.2"},
		"X-Codex-Secondary-Window-Minutes":                []string{"10080"},
		"X-Codex-Secondary-Reset-After-Seconds":           []string{"604800"},
		"X-Codex-Secondary-Reset-At":                      []string{"1787875200"},
		"X-Codex-Bengalfox-Limit-Name":                    []string{"GPT-5.3-Codex-Spark"},
		"X-Codex-Bengalfox-Primary-Used-Percent":          []string{"1"},
		"X-Codex-Bengalfox-Primary-Window-Minutes":        []string{"300"},
		"X-Codex-Bengalfox-Primary-Reset-After-Seconds":   []string{"18000"},
		"X-Codex-Bengalfox-Secondary-Used-Percent":        []string{"2"},
		"X-Codex-Bengalfox-Secondary-Window-Minutes":      []string{"10080"},
		"X-Codex-Bengalfox-Secondary-Reset-After-Seconds": []string{"604800"},
	}
	// 输入对象在所有迭代中保持只读；基准只统计一次快照构造自身的 CPU 和分配。
	input := quota.UsageHeaderSnapshotInput{
		AuthType:   "oauth",
		AuthIndex:  "codex-auth",
		Provider:   "codex",
		ObservedAt: time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC),
		Headers:    headers,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		// 每轮必须成功，避免编译器把无观察结果的构造路径消除。
		if snapshot, ok := quota.BuildUsageHeaderSnapshot(input); !ok || snapshot == nil {
			b.Fatal("expected codex usage header snapshot")
		}
	}
}

func BenchmarkTryAppendUsageHeaderSnapshotPointers32(b *testing.B) {
	// 该基准只测一次锁内批量指针合并与非阻塞 wake，不把 Header 解码或数据库工作混入结果。
	service := quota.NewServiceWithRegistryAndOptions(nil, quota.NewProviderRegistry(nil), quota.ServiceOptions{
		UsageHeaderSnapshotFlushInterval: time.Hour,
		CodexQuotaHistoryFlushInterval:   time.Hour,
		PricingCatalog:                   emptyPricingCatalogForTest(),
	})
	defer service.StopRefreshTasks()
	// 使用非 OAuth 测试身份避免 shutdown 时执行数据库 identity 查询；append 的 map/指针成本保持相同。
	snapshots := make([]*quota.UsageHeaderSnapshot, 32)
	for index := range snapshots {
		snapshots[index] = &quota.UsageHeaderSnapshot{
			AuthType:   "benchmark",
			AuthIndex:  fmt.Sprintf("benchmark-auth-%02d", index),
			Provider:   "codex",
			ObservedAt: time.Date(2026, 8, 20, 8, 0, index, 0, time.UTC),
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if !service.TryAppendUsageHeaderSnapshots(snapshots) {
			b.Fatal("expected pointer batch append")
		}
	}
}
