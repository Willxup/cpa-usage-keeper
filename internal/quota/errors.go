package quota

import (
	"errors"
	"strings"
)

var (
	ErrValidation      = errors.New("quota request validation failed")
	ErrNotFound        = errors.New("quota auth identity not found")
	ErrUnsupportedType = errors.New("quota identity type is unsupported")
	ErrProviderInput   = errors.New("quota provider input is invalid")
	ErrTaskNotFound    = errors.New("quota refresh task not found")
)

func ProviderInputErrorMessage(err error, fallback string) string {
	message := strings.ReplaceAll(err.Error(), ErrProviderInput.Error()+": ", "")
	message = strings.ReplaceAll(message, ErrProviderInput.Error()+"\n", "")
	message = strings.TrimSpace(message)
	if message == "" || message == ErrProviderInput.Error() {
		return fallback
	}
	return message
}
