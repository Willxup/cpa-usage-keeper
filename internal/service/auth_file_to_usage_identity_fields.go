package service

import (
	"strings"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/authfiles"
)

func resolveAuthFileProjectID(file authfiles.AuthFile) *string {
	switch strings.ToLower(strings.TrimSpace(file.Type)) {
	case "antigravity":
		return resolveAntigravityProjectID(file)
	case "gemini", "gemini-cli", "gemini-cli-code-assist":
		return resolveGeminiCLIProjectID(file)
	default:
		return nil
	}
}

func resolveCodexAccountID(file authfiles.AuthFile) *string {
	var idTokenAccountID *string
	var idTokenAccountIDCamel *string
	if file.IDToken != nil {
		idTokenAccountID = file.IDToken.AccountID
		idTokenAccountIDCamel = file.IDToken.AccountIDCamel
	}
	return firstNonEmptyStringPtr(
		idTokenAccountID,
		idTokenAccountIDCamel,
		mapString(file.Metadata, "id_token", "chatgpt_account_id"),
		mapString(file.Metadata, "id_token", "chatgptAccountId"),
		mapString(file.Metadata, "idToken", "chatgpt_account_id"),
		mapString(file.Metadata, "idToken", "chatgptAccountId"),
		mapString(file.Attributes, "id_token", "chatgpt_account_id"),
		mapString(file.Attributes, "id_token", "chatgptAccountId"),
		mapString(file.Attributes, "idToken", "chatgpt_account_id"),
		mapString(file.Attributes, "idToken", "chatgptAccountId"),
	)
}

func resolveCodexPlanType(file authfiles.AuthFile) *string {
	var idTokenPlanType *string
	var idTokenPlanTypeCamel *string
	if file.IDToken != nil {
		idTokenPlanType = file.IDToken.PlanType
		idTokenPlanTypeCamel = file.IDToken.PlanTypeCamel
	}
	return firstNonEmptyStringPtr(
		idTokenPlanType,
		idTokenPlanTypeCamel,
		mapString(file.Metadata, "plan_type"),
		mapString(file.Metadata, "planType"),
		mapString(file.Metadata, "id_token", "plan_type"),
		mapString(file.Metadata, "id_token", "planType"),
		mapString(file.Metadata, "idToken", "plan_type"),
		mapString(file.Metadata, "idToken", "planType"),
		mapString(file.Attributes, "plan_type"),
		mapString(file.Attributes, "planType"),
		mapString(file.Attributes, "id_token", "plan_type"),
		mapString(file.Attributes, "id_token", "planType"),
		mapString(file.Attributes, "idToken", "plan_type"),
		mapString(file.Attributes, "idToken", "planType"),
	)
}

func resolveCodexActiveStart(file authfiles.AuthFile) *time.Time {
	if file.IDToken == nil {
		return nil
	}
	if file.IDToken.ActiveStart != nil {
		return file.IDToken.ActiveStart
	}
	return file.IDToken.ActiveStartCamel
}

func resolveCodexActiveUntil(file authfiles.AuthFile) *time.Time {
	if file.IDToken == nil {
		return nil
	}
	if file.IDToken.ActiveUntil != nil {
		return file.IDToken.ActiveUntil
	}
	return file.IDToken.ActiveUntilCamel
}

func resolveGeminiCLIProjectID(file authfiles.AuthFile) *string {
	return firstNonEmptyStringPtr(
		lastParenthesizedValue(file.Account),
		lastParenthesizedValuePtr(mapString(file.Metadata, "account")),
		lastParenthesizedValuePtr(mapString(file.Attributes, "account")),
	)
}

func resolveAntigravityProjectID(file authfiles.AuthFile) *string {
	return firstNonEmptyStringPtr(
		stringValue(file.ProjectID),
		stringValue(file.ProjectIDCamel),
		mapString(file.Metadata, "project_id"),
		mapString(file.Metadata, "projectId"),
		mapString(file.Metadata, "installed", "project_id"),
		mapString(file.Metadata, "installed", "projectId"),
		mapString(file.Metadata, "web", "project_id"),
		mapString(file.Metadata, "web", "projectId"),
		mapString(file.Attributes, "project_id"),
		mapString(file.Attributes, "projectId"),
		mapString(file.Attributes, "installed", "project_id"),
		mapString(file.Attributes, "installed", "projectId"),
		mapString(file.Attributes, "web", "project_id"),
		mapString(file.Attributes, "web", "projectId"),
	)
}

func firstNonEmptyStringPtr(values ...*string) *string {
	for _, value := range values {
		if value == nil {
			continue
		}
		trimmed := strings.TrimSpace(*value)
		if trimmed == "" {
			continue
		}
		return &trimmed
	}
	return nil
}

func stringValue(value string) *string {
	return firstNonEmptyStringPtr(&value)
}

func mapString(values map[string]any, path ...string) *string {
	if len(path) == 0 || values == nil {
		return nil
	}
	var current any = values
	for index, key := range path {
		currentMap, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		value, ok := mapValue(currentMap, key)
		if !ok {
			return nil
		}
		if index == len(path)-1 {
			stringValue, ok := value.(string)
			if !ok {
				return nil
			}
			return firstNonEmptyStringPtr(&stringValue)
		}
		current = value
	}
	return nil
}

func mapValue(values map[string]any, key string) (any, bool) {
	if value, ok := values[key]; ok {
		return value, true
	}
	for candidateKey, value := range values {
		if strings.EqualFold(candidateKey, key) {
			return value, true
		}
	}
	return nil, false
}

func lastParenthesizedValue(value string) *string {
	end := strings.LastIndex(value, ")")
	if end < 0 {
		return nil
	}
	start := strings.LastIndex(value[:end], "(")
	if start < 0 {
		return nil
	}
	return stringValue(value[start+1 : end])
}

func lastParenthesizedValuePtr(value *string) *string {
	if value == nil {
		return nil
	}
	return lastParenthesizedValue(*value)
}
