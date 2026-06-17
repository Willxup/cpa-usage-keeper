package quota

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrValidation      = errors.New("quota request validation failed")
	ErrNotFound        = errors.New("quota auth identity not found")
	ErrUnsupportedType = errors.New("quota identity type is unsupported")
	ErrProviderInput   = errors.New("quota provider input is invalid")
	ErrTaskNotFound    = errors.New("quota refresh task not found")

	ErrResetInProgress = errors.New("quota reset already in progress")
)

type ProviderHTTPError struct {
	StatusCode int
	Message    string
}

func (e ProviderHTTPError) Error() string {
	message := strings.TrimSpace(e.Message)
	if message == "" {
		return fmt.Sprintf("HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, message)
}

func ProviderInputErrorMessage(err error, fallback string) string {
	message := strings.ReplaceAll(err.Error(), ErrProviderInput.Error()+": ", "")
	message = strings.ReplaceAll(message, ErrProviderInput.Error()+"\n", "")
	message = strings.TrimSpace(message)
	if message == "" || message == ErrProviderInput.Error() {
		return fallback
	}
	return message
}
