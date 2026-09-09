package quota

import (
	"net/http"
	"strings"
	"time"

	repositorydto "cpa-usage-keeper/internal/repository/dto"
)

const codexHeaderSnapshotValueMaxLength = 4096

type UsageHeaderSnapshotInput struct {
	// AuthType 是 UsageEvent 的标准化身份来源；只有 oauth 才能对应 Codex Auth File。
	AuthType string
	// AuthIndex 是 UsageEvent 携带的 CPA auth-index，空值不能建立 cache 或历史归属。
	AuthIndex string
	// Provider 是 UsageEvent 的上游提示，仅用于诊断，不参与真实 Codex 身份判定。
	Provider string
	// ObservedAt 是 Header 对应 UsageEvent 的发生 instant，不得替换为解码或入队时间。
	ObservedAt time.Time
	// Headers 是同步构造阶段借用的响应 Header；返回后不会被快照或 runner 持有。
	Headers http.Header
}

type UsageHeaderSnapshot struct {
	// AuthType 是标准化身份来源；Header 快照只接受 oauth。
	AuthType string
	// AuthIndex 是 CPA Auth File 稳定账号键，供 cache 身份匹配和历史回溯使用。
	AuthIndex string
	// Provider 是上游诊断提示，不替代 usage_identities.type 的 Codex 身份校验。
	Provider string
	// ObservedAt 是对应 UsageEvent 的真实观察时间，不是 runner 入队或 flush 时间。
	ObservedAt time.Time
	// CacheOutput 是现有 quota cache 合同的完整只读解析结果，worker 不再解析 Header。
	CacheOutput ProviderOutput
	// MainQuotaObservations 只包含无 group Primary/Secondary，供 history runner 在一分钟批次到期后消费。
	MainQuotaObservations []repositorydto.CodexMainQuotaObservation
	// pendingMainObservedAt 只供一分钟 cache 合并记录主额度自身的新鲜度；零值表示回退 ObservedAt。
	pendingMainObservedAt time.Time
	// pendingAdditionalObservedAt 按 LimitName 记录 Additional 自身的新鲜度，不进入 history 或对外响应。
	pendingAdditionalObservedAt map[string]time.Time
}

type UsageHeaderSnapshotAppender interface {
	// TryAppendUsageHeaderSnapshots 接收不可变快照；实现可以保留指针或创建派生快照，但不能修改输入及其嵌套对象。
	TryAppendUsageHeaderSnapshots([]*UsageHeaderSnapshot) bool
}

type usageHeaderSnapshotProcessor interface {
	TryBuildUsageHeaderSnapshot(UsageHeaderSnapshotInput) (*UsageHeaderSnapshot, bool)
}

var usageHeaderSnapshotProcessors = []usageHeaderSnapshotProcessor{
	codexUsageHeaderSnapshotProcessor{},
}

func BuildUsageHeaderSnapshot(input UsageHeaderSnapshotInput) (*UsageHeaderSnapshot, bool) {
	for _, processor := range usageHeaderSnapshotProcessors {
		if snapshot, ok := processor.TryBuildUsageHeaderSnapshot(input); ok {
			return snapshot, true
		}
	}
	return nil, false
}

type codexUsageHeaderSnapshotProcessor struct{}

func (codexUsageHeaderSnapshotProcessor) TryBuildUsageHeaderSnapshot(input UsageHeaderSnapshotInput) (*UsageHeaderSnapshot, bool) {
	authType := strings.ToLower(strings.TrimSpace(input.AuthType))
	authIndex := strings.TrimSpace(input.AuthIndex)
	if authType != "oauth" || authIndex == "" || len(input.Headers) == 0 {
		return nil, false
	}
	// Header 只在构造阶段过滤和数值解析一次，同时生成 cache 与 history 两个独立投影。
	decoded, ok := decodeCodexHeaderQuota(input.Headers)
	if !ok {
		return nil, false
	}
	cacheOutput, cacheOK := decoded.cacheOutput()
	// history 只在来源可信时复用同一 decoded 主窗口；cache 始终保持原有投影。
	var observations []repositorydto.CodexMainQuotaObservation
	if decoded.mainQuotaHistoryAllowed() {
		historyOutput := decoded.mainQuotaOutput()
		observations = BuildCodexMainQuotaObservations(authIndex, historyOutput, input.ObservedAt)
	}
	// 只有 cache 或 history 至少一个消费者可处理时才创建异步快照。
	if !cacheOK && len(observations) == 0 {
		return nil, false
	}
	return &UsageHeaderSnapshot{
		AuthType:              authType,
		AuthIndex:             authIndex,
		Provider:              strings.TrimSpace(input.Provider),
		ObservedAt:            input.ObservedAt,
		CacheOutput:           cacheOutput,
		MainQuotaObservations: observations,
	}, true
}

func codexQuotaSnapshotHeaders(headers http.Header) http.Header {
	// 空输入不分配 map，保持无快照的快速路径。
	if len(headers) == 0 {
		return nil
	}
	// 只为 quota 白名单字段分配目标 map，避免先复制全部 Cookie/Date 再二次过滤。
	filtered := make(http.Header)
	for key, values := range headers {
		// Header key 先 trim/canonical，兼容 Redis JSON 中的全小写字段名。
		canonicalKey := http.CanonicalHeaderKey(strings.TrimSpace(key))
		if canonicalKey == "" || !isCodexQuotaHeaderKey(canonicalKey) {
			continue
		}
		if value, ok := firstBoundedHeaderValue(values); ok {
			// 每个 quota key 只保留第一个非空有界值，快照构造后不再持有调用方 value slice。
			filtered.Set(canonicalKey, value)
		}
	}
	return filtered
}

func isCodexQuotaHeaderKey(key string) bool {
	if key == "X-Codex-Plan-Type" || key == "X-Codex-Active-Limit" {
		return true
	}
	if !strings.HasPrefix(key, codexHeaderPrefix) {
		return false
	}
	for _, suffix := range []string{
		"-Limit-Name",
		"-Used-Percent",
		"-Window-Minutes",
		"-Reset-At",
		"-Reset-After-Seconds",
	} {
		if strings.HasSuffix(key, suffix) {
			return true
		}
	}
	return false
}

func firstBoundedHeaderValue(values []string) (string, bool) {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || len(trimmed) > codexHeaderSnapshotValueMaxLength {
			continue
		}
		return trimmed, true
	}
	return "", false
}
