package quota

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/repository"
	"gorm.io/gorm"
)

const claudeClaimTimeout = 25 * time.Second

type ClaudeResetGrantsResponse struct {
	AuthIndex       string                  `json:"authIndex"`
	Status          *ClaudeResetGrantStatus `json:"status"`
	SelectedGrantID string                  `json:"selectedGrantId,omitempty"`
	OrganizationID  string                  `json:"organizationId,omitempty"`
}

type ClaudeResetProvider interface {
	ListClaudeResetGrants(context.Context, ProviderInput) (ClaudeResetGrantsResponse, error)
	ResetClaude(context.Context, ProviderInput, string, string) (ProviderResetOutput, error)
}

func (s *Service) GetClaudeResetGrants(ctx context.Context, authIndex string) (ClaudeResetGrantsResponse, error) {
	authIndex = strings.TrimSpace(authIndex)
	if authIndex == "" {
		return ClaudeResetGrantsResponse{}, fmt.Errorf("%w: auth_index is required", ErrValidation)
	}
	identity, err := repository.GetActiveAuthFileUsageIdentityByAuthIndex(ctx, s.db, authIndex)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ClaudeResetGrantsResponse{}, fmt.Errorf("%w: %s", ErrNotFound, authIndex)
		}
		return ClaudeResetGrantsResponse{}, err
	}
	provider, handler, ok := s.resolveQuotaHandlerForIdentity(identity)
	lister, supported := handler.(ClaudeResetProvider)
	if !ok || provider != "claude" || !supported {
		return ClaudeResetGrantsResponse{}, ErrUnsupportedType
	}
	return lister.ListClaudeResetGrants(ctx, ProviderInput{Identity: identity})
}

func (p claudeProvider) ListClaudeResetGrants(ctx context.Context, input ProviderInput) (ClaudeResetGrantsResponse, error) {
	output, err := p.Check(ctx, input)
	if err != nil {
		return ClaudeResetGrantsResponse{}, err
	}
	result := output.Result.(ClaudeResult)
	response := ClaudeResetGrantsResponse{AuthIndex: input.Identity.Identity}
	now := time.Now()
	// 弹窗只展示本次可用权益，普通额度刷新仍保留完整官方 grants。
	if original := result.Usage.ResetGrants; original != nil {
		status := *original
		status.Grants = []ClaudeResetGrant{}
		status.AvailableCount = 0
		for _, grant := range original.Grants {
			if claudeGrantUsable(original, grant, now) {
				status.Grants = append(status.Grants, grant)
				status.AvailableCount += grant.ResetsLeft
			}
		}
		response.Status = &status
	}
	if grant := selectClaudeResetGrant(response.Status, now); grant != nil {
		response.SelectedGrantID = grant.ID
	}
	if result.Profile != nil && result.Profile.Organization != nil && claudeUUIDPattern.MatchString(result.Profile.Organization.UUID) {
		response.OrganizationID = strings.ToLower(result.Profile.Organization.UUID)
	}
	return response, nil
}

func (p claudeProvider) ResetClaude(ctx context.Context, input ProviderInput, grantID, organizationID string) (ProviderResetOutput, error) {
	// 弹窗提供已展示的 grant 和组织；确认只检查参数格式，是否允许消费由官方接口判断。
	if !claudeGrantIDPattern.MatchString(grantID) || !claudeUUIDPattern.MatchString(organizationID) {
		return ProviderResetOutput{Code: "unavailable"}, nil
	}
	requestID, err := newRedeemRequestID()
	if err != nil {
		return ProviderResetOutput{}, err
	}
	claimCtx, cancel := context.WithTimeout(ctx, claudeClaimTimeout)
	defer cancel()
	response, err := p.caller.CallManagementAPI(claimCtx, apicall.Request{AuthIndex: input.Identity.Identity, Method: "POST", URL: "https://api.anthropic.com/api/organizations/" + strings.ToLower(organizationID) + "/reset_rate_limits", Header: copyHeaders(DefaultProviderConfigs().ClaudeUsage.Headers), Data: map[string]string{"program": "cedar_ember", "grant_id": grantID, "request_id": requestID}})
	output := ProviderResetOutput{Code: "unknown"}
	// 断连或非预期响应可能发生在官方已经处理之后，不自动重试，也不执行路由恢复。
	if err != nil || response == nil {
		return output, nil
	}
	if response.StatusCode == 429 {
		output.Code = "rate_limited"
		return output, nil
	}
	if response.StatusCode == 401 || response.StatusCode == 403 {
		output.Code = "auth_error"
		return output, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return output, nil
	}
	object, err := parseResponseObject(response)
	if err != nil {
		return output, nil
	}
	switch code := stringField(object, "result"); code {
	case "reset", "already_used", "not_limited", "cooldown", "ineligible", "unavailable":
		output.Code = code
	}
	if output.Code == "reset" || output.Code == "already_used" {
		client, ok := p.caller.(interface {
			ResetQuota(context.Context, string) error
		})
		output.RecoveryFailed = !ok || client.ResetQuota(ctx, input.Identity.Identity) != nil
	}
	return output, nil
}
