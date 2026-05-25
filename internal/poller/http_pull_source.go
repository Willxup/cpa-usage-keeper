package poller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/cpa"
)

type HTTPPullSource struct {
	// client 封装 CPA 管理 HTTP 接口调用。
	client *cpa.Client
	// batchSize 控制每次从 HTTP usage queue 拉取的最大数量。
	batchSize int
}

func NewHTTPPullSource(baseURL, managementKey string, timeout time.Duration, tlsSkipVerify bool, batchSize int) *HTTPPullSource {
	// HTTP source 只构造 client，不主动请求 CPA。
	return &HTTPPullSource{
		// CPA client 内部持有 baseURL、managementKey、timeout 和 TLS 配置。
		client: cpa.NewClient(baseURL, managementKey, timeout, tlsSkipVerify),
		// batchSize 与 Redis pull 使用同一批量配置，保持 ingest 速度一致。
		batchSize: batchSize,
	}
}

func (s *HTTPPullSource) Pull(ctx context.Context) ([]string, error) {
	if s == nil || s.client == nil {
		// client 缺失说明 app wiring 有问题。
		return nil, fmt.Errorf("http pull source client is nil")
	}
	// 通过 CPA 管理接口读取 usage queue；这里只拿 raw payload，不做 decode。
	result, err := s.client.FetchUsageQueue(ctx, s.batchSize)
	if err != nil {
		// HTTP/network/API 错误交给 runner 决定退避或记录。
		return nil, err
	}
	// 预分配 payload 数量大小，后面会过滤空值和 null。
	messages := make([]string, 0, len(result.Payload))
	for _, item := range result.Payload {
		// HTTP 返回的是 json.RawMessage，这里只 trim 外层空白，保持原始 JSON 内容。
		trimmed := strings.TrimSpace(string(item))
		if trimmed == "" || trimmed == "null" {
			// 空 payload 和 null 不写入 inbox，避免后续 decode 产生无效失败。
			continue
		}
		// 保留 raw usage JSON，交给既有 inbox process 解码。
		messages = append(messages, trimmed)
	}
	// 返回过滤后的 raw messages。
	return messages, nil
}
