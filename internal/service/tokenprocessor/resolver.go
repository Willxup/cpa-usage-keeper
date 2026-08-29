package tokenprocessor

import (
	"fmt"
	"strings"
)

// ResolveExecutor 只使用事件实际上报的 CPA executor；未知结果明确要求 sync 再查询 identity。
func ResolveExecutor(executorType string) (HandlerResolution, error) {
	// 静态 registry 校验失败属于部署代码错误，不能悄悄降级成 strict 并掩盖遗漏映射。
	if defaultRegistryErr != nil {
		return nil, fmt.Errorf("validate token processor registry: %w", defaultRegistryErr)
	}
	// 内部 resolver 返回不可伪造的具体值，公开接口只暴露带私有标记的只读接口。
	return resolveExecutor(defaultRegistry, executorType), nil
}

// ResolveIdentity 保持 executor 优先；只有 executor 缺失或未知时才把 identity 当作 fallback hint。
func ResolveIdentity(executorType, identityType string) (HandlerResolution, error) {
	// 与 ResolveExecutor 共用同一静态 registry，避免 sync 预解析和 identity fallback 形成两套路由表。
	if defaultRegistryErr != nil {
		return nil, fmt.Errorf("validate token processor registry: %w", defaultRegistryErr)
	}

	// 先按 executor 解析，因为它描述本次 CPA parser，而 identity 只描述凭证的历史类型。
	resolution := resolveExecutor(defaultRegistry, executorType)
	if !resolution.needsIdentity {
		// 调用方即使同时传入冲突 identity，也不能推翻已知 executor 合同。
		resolution.identityType = strings.TrimSpace(identityType)
		return resolution, nil
	}

	// executor 无法证明协议时才读取 identity exact alias，保持旧事件处理能力。
	normalizedIdentity := normalizeRegistryAlias(identityType)
	if definition, exists := defaultRegistry.identities[normalizedIdentity]; exists {
		resolution.handlerID = definition.handlerID
		resolution.evidenceSource = EvidenceIdentity
		resolution.evidenceStrength = EvidenceIdentityHint
		resolution.identityType = strings.TrimSpace(identityType)
		resolution.needsIdentity = false
		return resolution, nil
	}

	// exact alias 永远优先，随后只尝试文档允许的 openai-compatible-* prefix matcher。
	for _, definition := range defaultRegistry.identityPrefixes {
		if strings.HasPrefix(normalizedIdentity, definition.prefix) {
			resolution.handlerID = definition.handlerID
			resolution.evidenceSource = EvidenceIdentity
			resolution.evidenceStrength = EvidenceIdentityHint
			resolution.identityType = strings.TrimSpace(identityType)
			resolution.needsIdentity = false
			return resolution, nil
		}
	}

	// identity 也未知时固定走 strict default，不能根据 provider/model 名称猜测 Token 口径。
	resolution.handlerID = HandlerStrictPassThrough
	resolution.evidenceSource = EvidenceDefault
	resolution.evidenceStrength = EvidenceUnknown
	resolution.identityType = strings.TrimSpace(identityType)
	resolution.needsIdentity = false
	return resolution, nil
}

func resolveExecutor(registry registryIndexes, executorType string) handlerResolution {
	// 只对 exact CPA Go 类型名做大小写/空白归一化，未来新名称必须显式加入独立 definition。
	normalizedExecutor := normalizeRegistryAlias(executorType)
	if definition, exists := registry.executors[normalizedExecutor]; exists {
		return handlerResolution{
			handlerID:        definition.handlerID,
			evidenceSource:   EvidenceExecutor,
			evidenceStrength: EvidenceParserContract,
			executorType:     strings.TrimSpace(executorType),
			needsIdentity:    false,
		}
	}

	// 空、显式 unknown 和未来未注册类型都先保持 strict，等待 sync 的联合键 identity 查询。
	return handlerResolution{
		handlerID:        HandlerStrictPassThrough,
		evidenceSource:   EvidenceDefault,
		evidenceStrength: EvidenceUnknown,
		executorType:     strings.TrimSpace(executorType),
		needsIdentity:    true,
		// 只有非空且不是 CPA 固定占位值 unknown 的未注册名称，才需要进入新执行器维护日志。
		unknownExecutor: normalizedExecutor != "" && normalizedExecutor != "unknown",
	}
}
