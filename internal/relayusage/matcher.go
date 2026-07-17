package relayusage

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"cpa-usage-keeper/internal/entities"
)

// domainToPlatform 把中转商 base-url 的 host 映射到平台标识。
// 新增平台时在此追加域名，并在 NewDefaultAdapterRegistry 注册 adapter。
var domainToPlatform = map[string]string{
	"open.bigmodel.cn":     "glm",
	"api.minimaxi.com":     "minimax",
	"www.minimax.io":       "minimax",
	"api.kimi.com":         "kimi",
	"api.moonshot.cn":      "kimi",
	"platform.moonshot.cn": "kimi",
	"api.deepseek.com":     "deepseek",
}

// Match 返回 identity 对应的中转商平台标识；空字符串表示不支持或官方端点。
// 优先级：手动覆盖 > base-url 域名匹配。手动覆盖值为 "none" 时强制跳过查询。
// overrides 的 key 是 identity ID 的字符串形式（与 UsageIdentity.id 一致）。
func Match(identity entities.UsageIdentity, overrides map[string]string) string {
	if overrides != nil {
		if platform, ok := overrides[strconv.FormatInt(identity.ID, 10)]; ok {
			normalized := strings.TrimSpace(strings.ToLower(platform))
			if normalized == "none" {
				return ""
			}
			if normalized != "" {
				return normalized
			}
		}
	}
	return MatchByBaseURL(identity.BaseURL)
}

// MatchByBaseURL 仅按 base-url 域名匹配平台，不考虑手动覆盖。
func MatchByBaseURL(baseURL string) string {
	host := hostFromBaseURL(baseURL)
	if host == "" {
		return ""
	}
	return domainToPlatform[host]
}

func hostFromBaseURL(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return ""
	}
	host := baseURL
	if parsed, err := url.Parse(baseURL); err == nil && parsed.Host != "" {
		host = strings.ToLower(parsed.Host)
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return host
}
