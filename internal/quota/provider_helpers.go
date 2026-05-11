package quota

import (
	"encoding/json"
	"fmt"
	"strings"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
)

func mergeHeaders(base map[string]string, overrides map[string]string) map[string]string {
	// provider 默认 header 先铺底，身份相关 header 后覆盖，保证账号参数优先。
	if len(base) == 0 && len(overrides) == 0 {
		return nil
	}
	headers := make(map[string]string, len(base)+len(overrides))
	for key, value := range base {
		headers[key] = value
	}
	for key, value := range overrides {
		headers[key] = value
	}
	return headers
}

func targetHTTPError(response *apicall.Response) error {
	return fmt.Errorf("HTTP %d: %s", response.StatusCode, targetErrorMessage(response))
}

func targetErrorMessage(response *apicall.Response) string {
	// 优先解析结构化 body，再回退到 body_text，尽量把上游返回的真实错误带给用户。
	for _, data := range [][]byte{response.Body, []byte(strings.TrimSpace(response.BodyText))} {
		if message := targetErrorMessageFromBytes(data); message != "" {
			return message
		}
	}
	return strings.TrimSpace(response.BodyText)
}

func targetErrorMessageFromBytes(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		return strings.TrimSpace(text)
	}
	// 相邻项目常见错误形态包含 message/error/detail，也兼容 error 内嵌对象。
	object := rawObject(data)
	if object == nil {
		return strings.TrimSpace(string(data))
	}
	if message := stringField(object, "message", "error_description", "detail"); message != "" {
		return message
	}
	if errorText := stringField(object, "error"); errorText != "" {
		return errorText
	}
	if nested := objectField(object, "error"); nested != nil {
		if message := stringField(nested, "message", "detail", "error_description"); message != "" {
			return message
		}
	}
	return strings.TrimSpace(string(data))
}
