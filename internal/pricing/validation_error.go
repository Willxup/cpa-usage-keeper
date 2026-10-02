package pricing

import (
	"errors"
	"fmt"
)

// ValidationError 把完整配置的一个语义错误定位到稳定的 JSON 字段路径。
// HTTP 层只使用 Path/Code，内部详细原因不直接返回给用户。
type ValidationError struct {
	Path   string
	Code   string
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Path, e.Reason)
}

func invalidPricingField(path, code, reason string) error {
	return &ValidationError{Path: path, Code: code, Reason: reason}
}

// prefixPricingValidationError 在原有解码或编译错误上叠加父字段位置，不重复校验值。
func prefixPricingValidationError(prefix string, err error) error {
	var field *ValidationError
	if errors.As(err, &field) {
		path := prefix
		if field.Path != "" {
			path += "." + field.Path
		}
		return &ValidationError{Path: path, Code: field.Code, Reason: field.Reason}
	}
	return err
}

// branchPricingValidationError 把通用单价校验路径映射到分支 prices 字段。
func branchPricingValidationError(err error) error {
	var field *ValidationError
	if !errors.As(err, &field) {
		return err
	}
	path := field.Path
	if path == "base_prices" {
		path = "prices"
	} else if len(path) > len("base_prices.") && path[:len("base_prices.")] == "base_prices." {
		path = "prices." + path[len("base_prices."):]
	}
	return &ValidationError{Path: path, Code: field.Code, Reason: field.Reason}
}
