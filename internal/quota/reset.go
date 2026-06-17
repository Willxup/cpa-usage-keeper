package quota

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	"cpa-usage-keeper/internal/repository"

	"gorm.io/gorm"
)

type ResetRequest struct {
	AuthIndex string `json:"auth_index"`
}

type ResetResponse struct {
	AuthIndex    string `json:"authIndex"`
	Code         string `json:"code,omitempty"`
	WindowsReset int    `json:"windowsReset,omitempty"`
}

func (s *Service) Reset(ctx context.Context, request ResetRequest) (ResetResponse, error) {
	if s == nil {
		return ResetResponse{}, errors.New("quota service is nil")
	}
	authIndex := strings.TrimSpace(request.AuthIndex)
	if authIndex == "" {
		return ResetResponse{}, fmt.Errorf("%w: auth_index is required", ErrValidation)
	}

	// reset 会真实消费官方次数；同一 auth_index 同时只能有一个请求进入 provider。
	if !s.beginReset(authIndex) {
		return ResetResponse{}, fmt.Errorf("%w: %s", ErrResetInProgress, authIndex)
	}
	defer s.finishReset(authIndex)
	// reset 会消费官方次数，只要求命中当前未删除的 Auth File；disabled 仅限制自动刷新，不限制用户手动 reset。
	identity, err := repository.GetActiveAuthFileUsageIdentityByAuthIndex(ctx, s.db, authIndex)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ResetResponse{}, fmt.Errorf("%w: %s", ErrNotFound, authIndex)
		}
		return ResetResponse{}, err
	}
	resolvedType, handler, ok := s.resolveQuotaHandlerForIdentity(identity)
	if !ok || resolvedType != "codex" {
		return ResetResponse{}, fmt.Errorf("%w: %s", ErrUnsupportedType, normalizeIdentityType(identity.Provider))
	}
	// 当前只有 Codex 官方接口暴露 reset credit 消费能力，其它 provider 继续走只读刷新链路。
	resetter, ok := handler.(ProviderResetter)
	if !ok {
		return ResetResponse{}, fmt.Errorf("%w: %s", ErrUnsupportedType, normalizeIdentityType(identity.Provider))
	}
	output, err := resetter.Reset(ctx, ProviderInput{Identity: identity})
	if err != nil {
		return ResetResponse{}, err
	}
	return ResetResponse{
		AuthIndex:    authIndex,
		Code:         output.Code,
		WindowsReset: output.WindowsReset,
	}, nil
}

func (s *Service) beginReset(authIndex string) bool {
	if s == nil {
		return false
	}
	s.resetMu.Lock()
	defer s.resetMu.Unlock()
	if s.resetInFlight == nil {
		s.resetInFlight = make(map[string]struct{})
	}
	if _, exists := s.resetInFlight[authIndex]; exists {
		return false
	}
	s.resetInFlight[authIndex] = struct{}{}
	return true
}

func (s *Service) finishReset(authIndex string) {
	if s == nil {
		return
	}
	s.resetMu.Lock()
	defer s.resetMu.Unlock()
	delete(s.resetInFlight, authIndex)
}

func newRedeemRequestID() (string, error) {
	// Codex consume 接口要求每次请求携带新的 UUID v4，用于服务端去重和审计。
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
