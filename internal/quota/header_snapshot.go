package quota

import (
	"net/http"
	"strings"
	"time"
)

const codexHeaderSnapshotValueMaxLength = 4096

type UsageHeaderSnapshotInput struct {
	AuthType   string
	AuthIndex  string
	Provider   string
	ObservedAt time.Time
	Headers    http.Header
}

type UsageHeaderSnapshot struct {
	AuthType   string
	AuthIndex  string
	Provider   string
	ObservedAt time.Time
	Headers    http.Header
}

type UsageHeaderSnapshotAppender interface {
	TryAppendUsageHeaderSnapshots([]UsageHeaderSnapshot) bool
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
	headers := codexQuotaSnapshotHeaders(input.Headers)
	if _, ok := parseCodexHeaderQuota(headers); !ok {
		return nil, false
	}
	return &UsageHeaderSnapshot{
		AuthType:   authType,
		AuthIndex:  authIndex,
		Provider:   strings.TrimSpace(input.Provider),
		ObservedAt: input.ObservedAt,
		Headers:    headers,
	}, true
}

func codexQuotaSnapshotHeaders(headers http.Header) http.Header {
	canonical := canonicalQuotaHeaders(headers)
	if len(canonical) == 0 {
		return nil
	}
	filtered := make(http.Header)
	for key, values := range canonical {
		if !isCodexQuotaHeaderKey(key) {
			continue
		}
		if value, ok := firstBoundedHeaderValue(values); ok {
			filtered.Set(key, value)
		}
	}
	return filtered
}

func isCodexQuotaHeaderKey(key string) bool {
	if key == "X-Codex-Plan-Type" {
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
