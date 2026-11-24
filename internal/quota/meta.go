package quota

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/timeutil"
)

const metaQuotaURL = "https://api.meta.ai/muse-code/key"

type metaCredentialReader interface {
	FetchMetaDCAToken(context.Context, string) (string, error)
}

type metaProvider struct {
	caller ManagementAPICaller
}

func NewMetaProvider(caller ManagementAPICaller) ProviderHandler {
	return metaProvider{caller: caller}
}

func (p metaProvider) Check(ctx context.Context, input ProviderInput) (ProviderOutput, error) {
	identity := input.Identity
	if identity.FileName == nil || strings.TrimSpace(*identity.FileName) == "" || identity.FilePath == nil || strings.TrimSpace(*identity.FilePath) == "" {
		return ProviderOutput{}, fmt.Errorf("Meta credential is not downloadable")
	}
	reader, ok := p.caller.(metaCredentialReader)
	if !ok {
		return ProviderOutput{}, fmt.Errorf("Meta credential reader is unavailable")
	}
	token, err := reader.FetchMetaDCAToken(ctx, *identity.FileName)
	if err != nil {
		return ProviderOutput{}, err
	}
	// DCA 是独立的账号授权，不是 LLM API key；只用于固定的官方 quota endpoint。
	response, err := p.caller.CallManagementAPI(ctx, apicall.Request{
		AuthIndex: identity.Identity,
		Method:    "POST",
		URL:       metaQuotaURL,
		Header: map[string]string{
			"Accept": "application/json", "Content-Type": "application/json",
			"Authorization": "Bearer " + token, "x-api-version": "1.0.0",
		},
		Data: "{}",
	})
	if err != nil {
		if ctx.Err() != nil {
			return ProviderOutput{}, ctx.Err()
		}
		return ProviderOutput{}, fmt.Errorf("Meta quota request failed")
	}
	// Meta 错误正文也可能含 api_key 或 PII；仅状态码进入普通错误，原文仍可由既有 opt-in 查看器采集。
	if response != nil && (response.StatusCode < 200 || response.StatusCode >= 300) {
		return ProviderOutput{}, ProviderHTTPError{StatusCode: response.StatusCode}
	}
	object, err := parseResponseObject(response)
	if err != nil {
		return ProviderOutput{}, err
	}
	usage := objectField(object, "subs_usage")
	plan := metaStringField(object, "subs_tier_name")
	if plan == "" {
		plan = metaStringField(usage, "tier")
	}
	return ProviderOutput{Provider: "meta", Result: metaResult{
		Plan: plan, Window: parseMetaWindow(objectField(usage, "window"), true), Weekly: parseMetaWindow(objectField(usage, "weekly"), false),
	}}, nil
}

// metaResult 只保留额度展示所需数据，不保留可能包含 api_key/PII 的完整响应。
type metaResult struct {
	Plan   string
	Window *metaWindow
	Weekly *metaWindow
}

type metaWindow struct {
	UsedPercent     *float64
	DurationMinutes *float64
	ResetAt         string
}

func resolveMetaSubscription(result any) *SubscriptionInfo {
	var plan string
	switch value := result.(type) {
	case metaResult:
		plan = value.Plan
	case *metaResult:
		if value != nil {
			plan = value.Plan
		}
	}
	if strings.TrimSpace(plan) == "" {
		return nil
	}
	return &SubscriptionInfo{Provider: "meta", Plan: plan}
}

func metaStringField(object map[string]json.RawMessage, key string) string {
	var value string
	if json.Unmarshal(object[key], &value) != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func parseMetaWindow(object map[string]json.RawMessage, includeDuration bool) *metaWindow {
	if object == nil {
		return nil
	}
	window := &metaWindow{}
	if percent, ok := floatValue(object, "used_percent"); ok && !math.IsNaN(percent) && !math.IsInf(percent, 0) {
		percent = math.Min(100, math.Max(0, percent))
		window.UsedPercent = &percent
	}
	if includeDuration {
		if minutes, ok := metaPositiveNumber(object, "window_duration_mins"); ok {
			window.DurationMinutes = &minutes
		}
	}
	// 官方时间是 Unix 秒；限制在 RFC3339 支持范围内，避免无效外部数值溢出。
	if seconds, ok := metaPositiveNumber(object, "resets_at"); ok && seconds < 253402300800 {
		whole, fraction := math.Modf(seconds)
		window.ResetAt = timeutil.FormatStorageTime(time.Unix(int64(whole), int64(fraction*1e9)))
	}
	return window
}

func metaPositiveNumber(object map[string]json.RawMessage, key string) (float64, bool) {
	value, ok := floatValue(object, key)
	return value, ok && value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}
