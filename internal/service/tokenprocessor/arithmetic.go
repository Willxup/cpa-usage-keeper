package tokenprocessor

import "math"

func safeAddTokenCounts(values ...int64) (int64, bool) {
	// 所有协议 fold 和 Total 对账共用这一条算术路径，避免不同 handler 对溢出的处理漂移。
	total := int64(0)
	for _, value := range values {
		// 正数加法必须先检查 MaxInt64 剩余空间，不能依赖 Go 的回绕结果。
		if value > 0 && total > math.MaxInt64-value {
			return 0, false
		}
		// 工具保持通用安全性；虽然正常 Token 已 clamp 非负，负数也不能下溢。
		if value < 0 && total < math.MinInt64-value {
			return 0, false
		}
		total += value
	}
	return total, true
}
