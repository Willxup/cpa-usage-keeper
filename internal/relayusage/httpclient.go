package relayusage

import (
	"crypto/tls"
	"net/http"
	"time"
)

// HTTPDoer 是 adapter 发起出站请求的抽象，便于单测注入桩 client。
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// NewHTTPClient 构造直连中转商用量接口的 *http.Client，
// 复用 keeper 全局的 REQUEST_TIMEOUT 与 TLS_SKIP_VERIFY 语义（与 internal/cpa/client.go 对齐）。
func NewHTTPClient(timeout time.Duration, tlsSkipVerify bool) HTTPDoer {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	if tlsSkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}
}
