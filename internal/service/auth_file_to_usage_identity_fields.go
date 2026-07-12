package service

import (
	"strings"
	"time"

	"cpa-usage-keeper/internal/cpa/dto/authfiles"
)

func resolveAuthFileProjectID(file authfiles.AuthFile) *string {
	switch strings.ToLower(strings.TrimSpace(file.Type)) {
	case "gemini", "gemini-cli", "gemini-cli-code-assist", "antigravity":
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
	)
}

func resolveXAIUserID(file authfiles.AuthFile) *string {
	var metadata authfiles.AuthFileClaims
	if file.Metadata != nil {
		metadata = *file.Metadata
	}
	var attributes authfiles.AuthFileClaims
	if file.Attributes != nil {
		attributes = *file.Attributes
	}
	oauth := file.ResolvedXAIOAuth()
	user := file.ResolvedXAIUser()

	var oauthSub *authfiles.AuthFileXAIUserIDValue
	var oauthSubject *authfiles.AuthFileXAIUserIDValue
	if oauth != nil {
		oauthSub = oauth.Sub
		oauthSubject = oauth.Subject
	}
	var userSub *authfiles.AuthFileXAIUserIDValue
	var userID *authfiles.AuthFileXAIUserIDValue
	if user != nil {
		userSub = user.Sub
		userID = user.ID
	}

	return firstNonEmptyXAIUserIDValue(
		file.Sub,
		file.Subject,
		file.UserID,
		file.UserIDCamel,
		metadata.Sub,
		metadata.Subject,
		metadata.UserID,
		metadata.UserIDCamel,
		attributes.Sub,
		attributes.Subject,
		attributes.UserID,
		attributes.UserIDCamel,
		oauthSub,
		oauthSubject,
		userSub,
		userID,
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
	return stringValue(file.ProjectID)
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

func firstNonEmptyXAIUserIDValue(values ...*authfiles.AuthFileXAIUserIDValue) *string {
	for _, value := range values {
		if value == nil {
			continue
		}
		trimmed := strings.TrimSpace(string(*value))
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
