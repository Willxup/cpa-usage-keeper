package quota

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"cpa-usage-keeper/internal/cpa/dto/apicall"
)

func parseAntigravityQuotaPayload(response *apicall.Response) (*AntigravityQuotaPayload, error) {
	object, err := parseResponseObject(response)
	if err != nil {
		return nil, err
	}
	if _, ok := object["models"]; !ok {
		if nested := objectField(object, "body"); nested != nil {
			object = nested
		}
	}
	modelsObject := objectField(object, "models")
	payload := &AntigravityQuotaPayload{Models: map[string]AntigravityQuotaModel{}}
	for key, raw := range modelsObject {
		modelObject := rawObject(raw)
		if modelObject == nil {
			continue
		}
		infoObject := objectField(modelObject, "quotaInfo", "quota_info")
		model := AntigravityQuotaModel{
			DisplayName: stringField(modelObject, "displayName", "display_name"),
		}
		if infoObject != nil {
			model.QuotaInfo = &AntigravityQuotaInfo{
				RemainingFraction: floatField(infoObject, "remainingFraction", "remaining_fraction"),
				Remaining:         floatField(infoObject, "remaining"),
				ResetTime:         stringField(infoObject, "resetTime", "reset_time"),
			}
		}
		payload.Models[key] = model
	}
	return payload, nil
}

func parseCodexUsagePayload(response *apicall.Response) (*CodexUsagePayload, error) {
	object, err := parseResponseObject(response)
	if err != nil {
		return nil, err
	}
	payload := &CodexUsagePayload{
		PlanType:            stringField(object, "plan_type", "planType"),
		RateLimit:           parseCodexRateLimitInfo(objectField(object, "rate_limit", "rateLimit")),
		CodeReviewRateLimit: parseCodexRateLimitInfo(objectField(object, "code_review_rate_limit", "codeReviewRateLimit")),
	}
	for _, raw := range arrayField(object, "additional_rate_limits", "additionalRateLimits") {
		limitObject := rawObject(raw)
		if limitObject == nil {
			continue
		}
		payload.AdditionalRateLimits = append(payload.AdditionalRateLimits, CodexAdditionalRateLimit{
			LimitName:      stringField(limitObject, "limit_name", "limitName"),
			MeteredFeature: stringField(limitObject, "metered_feature", "meteredFeature"),
			RateLimit:      parseCodexRateLimitInfo(objectField(limitObject, "rate_limit", "rateLimit")),
		})
	}
	return payload, nil
}

func parseCodexRateLimitInfo(object map[string]json.RawMessage) *CodexRateLimitInfo {
	if object == nil {
		return nil
	}
	return &CodexRateLimitInfo{
		Allowed:         boolPtrField(object, "allowed"),
		LimitReached:    boolPtrField(object, "limit_reached", "limitReached"),
		PrimaryWindow:   parseCodexUsageWindow(objectField(object, "primary_window", "primaryWindow")),
		SecondaryWindow: parseCodexUsageWindow(objectField(object, "secondary_window", "secondaryWindow")),
	}
}

func parseCodexUsageWindow(object map[string]json.RawMessage) *CodexUsageWindow {
	if object == nil {
		return nil
	}
	return &CodexUsageWindow{
		UsedPercent:        floatField(object, "used_percent", "usedPercent"),
		LimitWindowSeconds: intField(object, "limit_window_seconds", "limitWindowSeconds"),
		ResetAfterSeconds:  intField(object, "reset_after_seconds", "resetAfterSeconds"),
		ResetAt:            intField(object, "reset_at", "resetAt"),
	}
}

func parseGeminiCliQuotaPayload(response *apicall.Response) (*GeminiCliQuotaPayload, error) {
	object, err := parseResponseObject(response)
	if err != nil {
		return nil, err
	}
	payload := &GeminiCliQuotaPayload{}
	for _, raw := range arrayField(object, "buckets") {
		bucketObject := rawObject(raw)
		if bucketObject == nil {
			continue
		}
		payload.Buckets = append(payload.Buckets, GeminiCliQuotaBucket{
			ModelID:           stringField(bucketObject, "modelId", "model_id"),
			TokenType:         stringField(bucketObject, "tokenType", "token_type"),
			RemainingFraction: floatField(bucketObject, "remainingFraction", "remaining_fraction"),
			RemainingAmount:   floatField(bucketObject, "remainingAmount", "remaining_amount"),
			ResetTime:         stringField(bucketObject, "resetTime", "reset_time"),
		})
	}
	return payload, nil
}

func parseGeminiCliCodeAssistPayload(response *apicall.Response) (*GeminiCLICodeAssistPayload, error) {
	object, err := parseResponseObject(response)
	if err != nil {
		return nil, err
	}
	return &GeminiCLICodeAssistPayload{
		CurrentTier: parseGeminiCliUserTier(objectField(object, "currentTier", "current_tier")),
		PaidTier:    parseGeminiCliUserTier(objectField(object, "paidTier", "paid_tier")),
	}, nil
}

func parseGeminiCliUserTier(object map[string]json.RawMessage) *GeminiCliUserTier {
	if object == nil {
		return nil
	}
	tier := &GeminiCliUserTier{
		ID:          stringField(object, "id"),
		Name:        stringField(object, "name"),
		Description: stringField(object, "description"),
	}
	for _, raw := range arrayField(object, "availableCredits", "available_credits") {
		creditObject := rawObject(raw)
		if creditObject == nil {
			continue
		}
		tier.AvailableCredits = append(tier.AvailableCredits, GeminiCliCredits{
			CreditType:   stringField(creditObject, "creditType", "credit_type"),
			CreditAmount: floatField(creditObject, "creditAmount", "credit_amount"),
		})
	}
	return tier
}

func parseClaudeUsagePayload(response *apicall.Response) (*ClaudeUsagePayload, error) {
	object, err := parseResponseObject(response)
	if err != nil {
		return nil, err
	}
	return &ClaudeUsagePayload{
		FiveHour:          parseClaudeUsageWindow(objectField(object, "five_hour", "fiveHour")),
		SevenDay:          parseClaudeUsageWindow(objectField(object, "seven_day", "sevenDay")),
		SevenDayOAuthApps: parseClaudeUsageWindow(objectField(object, "seven_day_oauth_apps", "sevenDayOauthApps")),
		SevenDayOpus:      parseClaudeUsageWindow(objectField(object, "seven_day_opus", "sevenDayOpus")),
		SevenDaySonnet:    parseClaudeUsageWindow(objectField(object, "seven_day_sonnet", "sevenDaySonnet")),
		SevenDayCowork:    parseClaudeUsageWindow(objectField(object, "seven_day_cowork", "sevenDayCowork")),
		IguanaNecktie:     parseClaudeUsageWindow(objectField(object, "iguana_necktie", "iguanaNecktie")),
		ExtraUsage:        parseClaudeExtraUsage(objectField(object, "extra_usage", "extraUsage")),
	}, nil
}

func parseClaudeUsageWindow(object map[string]json.RawMessage) *ClaudeUsageWindow {
	if object == nil {
		return nil
	}
	return &ClaudeUsageWindow{
		Utilization: floatField(object, "utilization"),
		ResetsAt:    stringField(object, "resets_at", "resetsAt"),
	}
}

func parseClaudeExtraUsage(object map[string]json.RawMessage) *ClaudeExtraUsage {
	if object == nil {
		return nil
	}
	return &ClaudeExtraUsage{
		IsEnabled:    boolField(object, "is_enabled", "isEnabled"),
		MonthlyLimit: floatField(object, "monthly_limit", "monthlyLimit"),
		UsedCredits:  floatField(object, "used_credits", "usedCredits"),
		Utilization:  floatPtrField(object, "utilization"),
	}
}

func parseClaudeProfilePayload(response *apicall.Response) (*ClaudeProfileResponse, error) {
	object, err := parseResponseObject(response)
	if err != nil {
		return nil, err
	}
	return &ClaudeProfileResponse{
		Account:      parseClaudeProfileAccount(objectField(object, "account")),
		Organization: parseClaudeProfileOrganization(objectField(object, "organization")),
	}, nil
}

func parseClaudeProfileAccount(object map[string]json.RawMessage) *ClaudeProfileAccount {
	if object == nil {
		return nil
	}
	return &ClaudeProfileAccount{
		UUID:         stringField(object, "uuid"),
		FullName:     stringField(object, "full_name", "fullName"),
		DisplayName:  stringField(object, "display_name", "displayName"),
		Email:        stringField(object, "email"),
		HasClaudeMax: boolField(object, "has_claude_max", "hasClaudeMax"),
		HasClaudePro: boolField(object, "has_claude_pro", "hasClaudePro"),
	}
}

func parseClaudeProfileOrganization(object map[string]json.RawMessage) *ClaudeProfileOrganization {
	if object == nil {
		return nil
	}
	return &ClaudeProfileOrganization{
		UUID:                 stringField(object, "uuid"),
		Name:                 stringField(object, "name"),
		OrganizationType:     stringField(object, "organization_type", "organizationType"),
		BillingType:          stringField(object, "billing_type", "billingType"),
		RateLimitTier:        stringField(object, "rate_limit_tier", "rateLimitTier"),
		HasExtraUsageEnabled: boolField(object, "has_extra_usage_enabled", "hasExtraUsageEnabled"),
		SubscriptionStatus:   stringField(object, "subscription_status", "subscriptionStatus"),
	}
}

func parseKimiUsagePayload(response *apicall.Response) (*KimiUsagePayload, error) {
	object, err := parseResponseObject(response)
	if err != nil {
		return nil, err
	}
	payload := &KimiUsagePayload{Usage: parseKimiUsageDetail(objectField(object, "usage"))}
	for _, raw := range arrayField(object, "limits") {
		limitObject := rawObject(raw)
		if limitObject == nil {
			continue
		}
		detail := parseKimiUsageDetail(objectField(limitObject, "detail"))
		payload.Limits = append(payload.Limits, KimiLimitItem{
			Name:      stringField(limitObject, "name"),
			Title:     stringField(limitObject, "title"),
			Scope:     stringField(limitObject, "scope"),
			Detail:    detail,
			Window:    parseKimiLimitWindow(objectField(limitObject, "window")),
			Used:      floatField(limitObject, "used"),
			Limit:     floatField(limitObject, "limit"),
			Remaining: floatField(limitObject, "remaining"),
			Duration:  intField(limitObject, "duration"),
			TimeUnit:  stringField(limitObject, "timeUnit", "time_unit"),
			ResetAt:   stringField(limitObject, "resetAt", "reset_at", "resetTime", "reset_time"),
			ResetIn:   floatField(limitObject, "resetIn", "reset_in"),
			TTL:       floatField(limitObject, "ttl"),
		})
		limit := &payload.Limits[len(payload.Limits)-1]
		if detail != nil {
			if limit.ResetAt == "" {
				limit.ResetAt = detail.ResetAt
			}
			if limit.ResetIn == 0 {
				limit.ResetIn = detail.ResetIn
			}
			if limit.TTL == 0 {
				limit.TTL = detail.TTL
			}
		}
	}
	return payload, nil
}

func parseKimiUsageDetail(object map[string]json.RawMessage) *KimiUsageDetail {
	if object == nil {
		return nil
	}
	return &KimiUsageDetail{
		Used:      floatField(object, "used"),
		Limit:     floatField(object, "limit"),
		Remaining: floatField(object, "remaining"),
		Name:      stringField(object, "name"),
		Title:     stringField(object, "title"),
		ResetAt:   stringField(object, "resetAt", "reset_at", "resetTime", "reset_time"),
		ResetIn:   floatField(object, "resetIn", "reset_in"),
		TTL:       floatField(object, "ttl"),
	}
}

func parseKimiLimitWindow(object map[string]json.RawMessage) *KimiLimitWindow {
	if object == nil {
		return nil
	}
	return &KimiLimitWindow{
		Duration: intField(object, "duration"),
		TimeUnit: stringField(object, "timeUnit", "time_unit"),
	}
}

func parseResponseObject(response *apicall.Response) (map[string]json.RawMessage, error) {
	if response == nil {
		return nil, fmt.Errorf("missing quota response")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, targetHTTPError(response)
	}
	if object := rawObject(response.Body); object != nil {
		return object, nil
	}
	trimmed := strings.TrimSpace(response.BodyText)
	if trimmed == "" {
		return nil, fmt.Errorf("empty quota response body")
	}
	if object := rawObject([]byte(trimmed)); object != nil {
		return object, nil
	}
	return nil, fmt.Errorf("parse quota response body")
}

func rawObject(data []byte) map[string]json.RawMessage {
	if len(data) == 0 {
		return nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err == nil && object != nil {
		return object
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		return rawObject([]byte(strings.TrimSpace(text)))
	}
	return nil
}

func objectField(object map[string]json.RawMessage, keys ...string) map[string]json.RawMessage {
	for _, key := range keys {
		if raw, ok := object[key]; ok {
			return rawObject(raw)
		}
	}
	return nil
}

func arrayField(object map[string]json.RawMessage, keys ...string) []json.RawMessage {
	for _, key := range keys {
		if raw, ok := object[key]; ok {
			var values []json.RawMessage
			if err := json.Unmarshal(raw, &values); err == nil {
				return values
			}
		}
	}
	return nil
}

func stringField(object map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		if raw, ok := object[key]; ok {
			var text string
			if err := json.Unmarshal(raw, &text); err == nil {
				return strings.TrimSpace(text)
			}
			var number json.Number
			if err := json.Unmarshal(raw, &number); err == nil {
				return number.String()
			}
		}
	}
	return ""
}

func floatField(object map[string]json.RawMessage, keys ...string) float64 {
	value, _ := floatValue(object, keys...)
	return value
}

func floatPtrField(object map[string]json.RawMessage, keys ...string) *float64 {
	value, ok := floatValue(object, keys...)
	if !ok {
		return nil
	}
	return &value
}

func floatValue(object map[string]json.RawMessage, keys ...string) (float64, bool) {
	for _, key := range keys {
		if raw, ok := object[key]; ok {
			var number float64
			if err := json.Unmarshal(raw, &number); err == nil {
				return number, true
			}
			var text string
			if err := json.Unmarshal(raw, &text); err == nil {
				parsed, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
				if err == nil {
					return parsed, true
				}
			}
		}
	}
	return 0, false
}

func intField(object map[string]json.RawMessage, keys ...string) int64 {
	value, ok := floatValue(object, keys...)
	if !ok {
		return 0
	}
	return int64(value)
}

func boolField(object map[string]json.RawMessage, keys ...string) bool {
	value := boolPtrField(object, keys...)
	return value != nil && *value
}

func boolPtrField(object map[string]json.RawMessage, keys ...string) *bool {
	for _, key := range keys {
		if raw, ok := object[key]; ok {
			var value bool
			if err := json.Unmarshal(raw, &value); err == nil {
				return &value
			}
			var text string
			if err := json.Unmarshal(raw, &text); err == nil {
				switch strings.ToLower(strings.TrimSpace(text)) {
				case "true", "1", "yes", "y", "on":
					parsed := true
					return &parsed
				case "false", "0", "no", "n", "off":
					parsed := false
					return &parsed
				}
			}
		}
	}
	return nil
}
