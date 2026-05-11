package quota

import (
	"context"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
	"cpa-usage-keeper/internal/entities"
)

type ManagementAPICaller interface {
	CallManagementAPI(context.Context, apicall.Request) (*apicall.Response, error)
}

type ProviderInput struct {
	Identity entities.UsageIdentity
}

type ProviderOutput struct {
	Provider string
	Result   any
}

type QuotaWindow struct {
	Duration *float64 `json:"duration,omitempty"`
	Unit     string   `json:"unit,omitempty"`
	Seconds  *int64   `json:"seconds,omitempty"`
}

type QuotaRow struct {
	Key               string       `json:"key"`
	Label             string       `json:"label,omitempty"`
	Scope             string       `json:"scope,omitempty"`
	Metric            string       `json:"metric,omitempty"`
	Used              *float64     `json:"used,omitempty"`
	Limit             *float64     `json:"limit,omitempty"`
	Remaining         *float64     `json:"remaining,omitempty"`
	UsedPercent       *float64     `json:"usedPercent,omitempty"`
	RemainingFraction *float64     `json:"remainingFraction,omitempty"`
	Allowed           *bool        `json:"allowed,omitempty"`
	LimitReached      *bool        `json:"limitReached,omitempty"`
	Window            *QuotaWindow `json:"window,omitempty"`
	ResetAt           string       `json:"resetAt,omitempty"`
	ResetAfterSeconds *int64       `json:"resetAfterSeconds,omitempty"`
}

type AntigravityQuotaInfo struct {
	RemainingFraction float64 `json:"remainingFraction,omitempty"`
	Remaining         float64 `json:"remaining,omitempty"`
	ResetTime         string  `json:"resetTime,omitempty"`
}

type AntigravityQuotaModel struct {
	DisplayName string                `json:"displayName,omitempty"`
	QuotaInfo   *AntigravityQuotaInfo `json:"quotaInfo,omitempty"`
}

type AntigravityQuotaPayload struct {
	Models map[string]AntigravityQuotaModel `json:"models,omitempty"`
}

type CodexUsageWindow struct {
	UsedPercent        float64 `json:"usedPercent,omitempty"`
	LimitWindowSeconds int64   `json:"limitWindowSeconds,omitempty"`
	ResetAfterSeconds  int64   `json:"resetAfterSeconds,omitempty"`
	ResetAt            int64   `json:"resetAt,omitempty"`
}

type CodexRateLimitInfo struct {
	Allowed         *bool             `json:"allowed,omitempty"`
	LimitReached    *bool             `json:"limitReached,omitempty"`
	PrimaryWindow   *CodexUsageWindow `json:"primaryWindow,omitempty"`
	SecondaryWindow *CodexUsageWindow `json:"secondaryWindow,omitempty"`
}

type CodexAdditionalRateLimit struct {
	LimitName      string              `json:"limitName,omitempty"`
	MeteredFeature string              `json:"meteredFeature,omitempty"`
	RateLimit      *CodexRateLimitInfo `json:"rateLimit,omitempty"`
}

type CodexUsagePayload struct {
	PlanType             string                     `json:"planType,omitempty"`
	RateLimit            *CodexRateLimitInfo        `json:"rateLimit,omitempty"`
	CodeReviewRateLimit  *CodexRateLimitInfo        `json:"codeReviewRateLimit,omitempty"`
	AdditionalRateLimits []CodexAdditionalRateLimit `json:"additionalRateLimits,omitempty"`
}

type GeminiCliQuotaBucket struct {
	ModelID           string  `json:"modelId,omitempty"`
	TokenType         string  `json:"tokenType,omitempty"`
	RemainingFraction float64 `json:"remainingFraction,omitempty"`
	RemainingAmount   float64 `json:"remainingAmount,omitempty"`
	ResetTime         string  `json:"resetTime,omitempty"`
}

type GeminiCliQuotaPayload struct {
	Buckets []GeminiCliQuotaBucket `json:"buckets,omitempty"`
}

type GeminiCliCredits struct {
	CreditType   string  `json:"creditType,omitempty"`
	CreditAmount float64 `json:"creditAmount,omitempty"`
}

type GeminiCliUserTier struct {
	ID               string             `json:"id,omitempty"`
	Name             string             `json:"name,omitempty"`
	Description      string             `json:"description,omitempty"`
	AvailableCredits []GeminiCliCredits `json:"availableCredits,omitempty"`
}

type GeminiCLICodeAssistPayload struct {
	CurrentTier *GeminiCliUserTier `json:"currentTier,omitempty"`
	PaidTier    *GeminiCliUserTier `json:"paidTier,omitempty"`
}

type ClaudeUsageWindow struct {
	Utilization float64 `json:"utilization,omitempty"`
	ResetsAt    string  `json:"resetsAt,omitempty"`
}

type ClaudeExtraUsage struct {
	IsEnabled    bool     `json:"isEnabled,omitempty"`
	MonthlyLimit float64  `json:"monthlyLimit,omitempty"`
	UsedCredits  float64  `json:"usedCredits,omitempty"`
	Utilization  *float64 `json:"utilization,omitempty"`
}

type ClaudeUsagePayload struct {
	FiveHour          *ClaudeUsageWindow `json:"fiveHour,omitempty"`
	SevenDay          *ClaudeUsageWindow `json:"sevenDay,omitempty"`
	SevenDayOAuthApps *ClaudeUsageWindow `json:"sevenDayOauthApps,omitempty"`
	SevenDayOpus      *ClaudeUsageWindow `json:"sevenDayOpus,omitempty"`
	SevenDaySonnet    *ClaudeUsageWindow `json:"sevenDaySonnet,omitempty"`
	SevenDayCowork    *ClaudeUsageWindow `json:"sevenDayCowork,omitempty"`
	IguanaNecktie     *ClaudeUsageWindow `json:"iguanaNecktie,omitempty"`
	ExtraUsage        *ClaudeExtraUsage  `json:"extraUsage,omitempty"`
}

type ClaudeProfileAccount struct {
	UUID         string `json:"uuid,omitempty"`
	FullName     string `json:"fullName,omitempty"`
	DisplayName  string `json:"displayName,omitempty"`
	Email        string `json:"email,omitempty"`
	HasClaudeMax bool   `json:"hasClaudeMax,omitempty"`
	HasClaudePro bool   `json:"hasClaudePro,omitempty"`
}

type ClaudeProfileOrganization struct {
	UUID                 string `json:"uuid,omitempty"`
	Name                 string `json:"name,omitempty"`
	OrganizationType     string `json:"organizationType,omitempty"`
	BillingType          string `json:"billingType,omitempty"`
	RateLimitTier        string `json:"rateLimitTier,omitempty"`
	HasExtraUsageEnabled bool   `json:"hasExtraUsageEnabled,omitempty"`
	SubscriptionStatus   string `json:"subscriptionStatus,omitempty"`
}

type ClaudeProfileResponse struct {
	Account      *ClaudeProfileAccount      `json:"account,omitempty"`
	Organization *ClaudeProfileOrganization `json:"organization,omitempty"`
}

type KimiUsageDetail struct {
	Used      float64 `json:"used,omitempty"`
	Limit     float64 `json:"limit,omitempty"`
	Remaining float64 `json:"remaining,omitempty"`
	Name      string  `json:"name,omitempty"`
	Title     string  `json:"title,omitempty"`
	ResetAt   string  `json:"resetAt,omitempty"`
	ResetIn   float64 `json:"resetIn,omitempty"`
	TTL       float64 `json:"ttl,omitempty"`
}

type KimiLimitWindow struct {
	Duration int64  `json:"duration,omitempty"`
	TimeUnit string `json:"timeUnit,omitempty"`
}

type KimiLimitItem struct {
	Name      string           `json:"name,omitempty"`
	Title     string           `json:"title,omitempty"`
	Scope     string           `json:"scope,omitempty"`
	Detail    *KimiUsageDetail `json:"detail,omitempty"`
	Window    *KimiLimitWindow `json:"window,omitempty"`
	Used      float64          `json:"used,omitempty"`
	Limit     float64          `json:"limit,omitempty"`
	Remaining float64          `json:"remaining,omitempty"`
	Duration  int64            `json:"duration,omitempty"`
	TimeUnit  string           `json:"timeUnit,omitempty"`
	ResetAt   string           `json:"resetAt,omitempty"`
	ResetIn   float64          `json:"resetIn,omitempty"`
	TTL       float64          `json:"ttl,omitempty"`
}

type KimiUsagePayload struct {
	Usage  *KimiUsageDetail `json:"usage,omitempty"`
	Limits []KimiLimitItem  `json:"limits,omitempty"`
}

type AntigravityResult struct {
	Quota *AntigravityQuotaPayload `json:"quota"`
}

type CodexResult struct {
	Usage *CodexUsagePayload `json:"usage"`
}

type GeminiCLIResult struct {
	Quota      *GeminiCliQuotaPayload      `json:"quota"`
	CodeAssist *GeminiCLICodeAssistPayload `json:"codeAssist"`
}

type ClaudeResult struct {
	Usage   *ClaudeUsagePayload    `json:"usage"`
	Profile *ClaudeProfileResponse `json:"profile"`
}

type KimiResult struct {
	Usage *KimiUsagePayload `json:"usage"`
}

type ProviderHandler interface {
	Check(context.Context, ProviderInput) (ProviderOutput, error)
}
