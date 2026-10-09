package cpa

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"cpa-usage-keeper/internal/cpa/dto/authfiles"
)

// FetchKimiCredentialMetadata 按需读取单个文件；凭证正文不进入返回值、缓存或错误。
func (c *Client) FetchKimiCredentialMetadata(ctx context.Context, name string) (*authfiles.KimiCredentialMetadata, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("Kimi credential is not downloadable")
	}
	var object map[string]json.RawMessage
	_, _, err := c.doManagementJSONRequest(ctx, cpaManagementAuthFilesDownloadEndpoint+"?name="+url.QueryEscape(name), &object, "Kimi credential")
	if err != nil || object == nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// JSON 解码错误可能包含凭证片段；只返回固定错误文本。
		return nil, fmt.Errorf("Kimi credential metadata is unavailable")
	}
	readString := func(raw json.RawMessage) string {
		var value string
		_ = json.Unmarshal(raw, &value)
		return strings.TrimSpace(value)
	}
	baseURL, present := object["base_url"]
	if !present {
		baseURL = object["base-url"]
	}
	return &authfiles.KimiCredentialMetadata{
		Domain: readString(object["domain"]), BaseURL: readString(baseURL), Type: readString(object["type"]),
	}, nil
}
