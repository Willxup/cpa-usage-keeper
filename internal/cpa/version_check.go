package cpa

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const versionCheckTimeout = 3 * time.Second

var cpaReleaseVersion = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// CheckMinimumVersion 为后台提示读取最低 CPA 版本，不决定 Keeper 是否启动。
// 未知/dev 版本和暂时无法连接返回未验证；仅明确低于要求的正式版本返回升级提示。
func (c *Client) CheckMinimumVersion(ctx context.Context) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if c == nil || c.baseURL == "" || c.managementKey == "" {
		return false, nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, versionCheckTimeout)
	defer cancel()
	// CPA 8.0.12 保留的轻量只读 debug 接口同样携带版本头，无需枚举凭证。
	// 只发一次请求；不追随重定向，也不读取或输出响应正文。
	client := *c.httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	probe := func() (string, error) {
		req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, c.baseURL+"/v0/management/debug", nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+c.managementKey)
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		return strings.TrimSpace(resp.Header.Get("X-CPA-VERSION")), nil
	}
	version, err := probe()
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if err != nil {
		return false, nil
	}
	parts := cpaReleaseVersion.FindStringSubmatch(version)
	if parts == nil {
		return false, nil
	}
	var numbers [3]uint64
	for i := range numbers {
		value, err := strconv.ParseUint(parts[i+1], 10, 64)
		if err != nil {
			return false, nil
		}
		numbers[i] = value
	}
	if numbers[0] < 8 || numbers[0] == 8 && numbers[1] == 0 && numbers[2] < 12 {
		return false, fmt.Errorf("CPA %d.%d.%d is below the minimum supported version. Please upgrade CPA to v8.0.12 or later. Keeper will continue running.", numbers[0], numbers[1], numbers[2])
	}
	return true, nil
}
