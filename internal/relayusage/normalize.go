package relayusage

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

func floatPtr(v float64) *float64 { return &v }

// percent 返回 used/limit*100（0-100），limit<=0 时返回 nil。
func percent(used, limit float64) *float64 {
	if limit <= 0 {
		return nil
	}
	v := used / limit * 100
	if v < 0 {
		v = 0
	} else if v > 100 {
		v = 100
	}
	return &v
}

// remainingFraction 返回 remaining/limit（0-1），limit<=0 时返回 nil。
func remainingFraction(remaining, limit float64) *float64 {
	if limit <= 0 {
		return nil
	}
	v := remaining / limit
	if v < 0 {
		v = 0
	} else if v > 1 {
		v = 1
	}
	return &v
}

// bearerToken 规范化鉴权头值：已带 Bearer 前缀则原样返回，否则补上。
// DeepSeek/Kimi 等平台的 key 既可能裸填也可能带 Bearer，统一处理。
func bearerToken(apiKey string) string {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(apiKey), "bearer ") {
		return apiKey
	}
	return "Bearer " + apiKey
}

// parseFloatString 解析中转商返回的字符串型数值（如 "12345"），失败返回 0。
func parseFloatString(raw string) float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return v
}

// parseResetTime 把中转商返回的重置时间统一成 RFC3339 字符串。
// 支持：毫秒时间戳（number）、Unix 秒、ISO 字符串。
func parseResetTime(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// 纯数字：按毫秒或秒解析
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		var t time.Time
		if n > 1e12 {
			t = time.UnixMilli(n)
		} else {
			t = time.Unix(n, 0)
		}
		return t.UTC().Format(time.RFC3339)
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC().Format(time.RFC3339)
	}
	// 兜底尝试常见格式
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05Z", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return raw
}

// clampPercent 把百分比限制在 [0,100]。
func clampPercent(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// millisToRFC3339 把毫秒时间戳转成 RFC3339 字符串。
func millisToRFC3339(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

// resetTimeFromAny 处理中转商返回的重置时间字段（可能是 number、string 或 null）。
// GLM 的 nextResetTime 在 percentage=0 时为空字符串，正常时为毫秒时间戳。
func resetTimeFromAny(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return parseResetTime(t)
	case float64:
		n := int64(t)
		if t > 1e12 {
			return time.UnixMilli(n).UTC().Format(time.RFC3339)
		}
		return time.Unix(n, 0).UTC().Format(time.RFC3339)
	case json.Number:
		return parseResetTime(t.String())
	}
	return ""
}
