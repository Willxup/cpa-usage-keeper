package cpa

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

// FetchMetaDCAToken 只返回请求额度所需的 DCA；下载正文和解码错误不进入缓存或日志。
func (c *Client) FetchMetaDCAToken(ctx context.Context, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("Meta credential is not downloadable")
	}
	var object map[string]json.RawMessage
	_, _, err := c.doManagementJSONRequest(ctx, cpaManagementCredentialsDownloadEndpoint+"?name="+url.QueryEscape(name), &object, "Meta credential")
	if err != nil || object == nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("Meta credential is unavailable")
	}
	var token string
	if err := json.Unmarshal(object["dca_token"], &token); err != nil {
		return "", fmt.Errorf("Meta credential has no valid DCA token")
	}
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, "dca:") || len(token) <= len("dca:") || strings.IndexFunc(token, unicode.IsSpace) >= 0 {
		return "", fmt.Errorf("Meta credential has no valid DCA token")
	}
	return token, nil
}
