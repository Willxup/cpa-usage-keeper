package repository

import (
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/pricing"
)

// UsageEventCostSubject 将待入库或已入库的归一化事件的九维和四类 Token 映射到统一计价输入，
// 并保留 CPA 事件时间供时段分支使用；此处不再执行 Token 归一化。
func UsageEventCostSubject(event entities.UsageEvent) pricing.CostSubject {
	modelAlias := ""
	if event.ModelAlias != nil {
		modelAlias = *event.ModelAlias
	}
	subject := pricing.NewCostSubject(pricing.UsageDimensions{
		APIGroupKey:         event.APIGroupKey,
		Model:               event.Model,
		AuthIndex:           event.AuthIndex,
		ModelAlias:          modelAlias,
		ServiceTier:         event.ServiceTier,
		ResponseServiceTier: event.ResponseServiceTier,
		ReasoningEffort:     event.ReasoningEffort,
		Endpoint:            event.Endpoint,
		ExecutorType:        event.ExecutorType,
	}, helper.UsageTokenCostInput{
		InputTokens:         event.InputTokens,
		OutputTokens:        event.OutputTokens,
		CacheReadTokens:     event.CacheReadTokens,
		CacheCreationTokens: event.CacheCreationTokens,
	})
	// 分支时段使用 CPA 已存事件时间，不能使用处理时钟或本地接收时间。
	subject.Timestamp = event.Timestamp
	return subject
}
