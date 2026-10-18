package test

import (
	"cpa-usage-keeper/internal/service"
	"testing"
	"time"
)

// 相同双 ID 原文也用于未修改基线，避免混用失败路径与成功路径比较吞吐。
func BenchmarkDecodeRedisUsageExecutionIdentity(b *testing.B) {
	for _, tc := range []struct{ name, request string }{{"both-ids", `"request_id":"request-1",`}, {"execution-only", ""}} {
		b.Run(tc.name, func(b *testing.B) {
			message := `{` + tc.request + `"execution_id":"16bc515a-b13a-49c6-8d9b-2c617cd3dc07","timestamp":"2026-10-18T01:02:03+08:00","provider":"codex","executor_type":"CodexExecutor","model":"gpt-5.1-codex","auth_type":"apikey","tokens":{"input_tokens":100,"output_tokens":200,"total_tokens":300}}`
			fetchedAt := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, _, err := service.DecodeRedisUsageMessage(message, fetchedAt); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
