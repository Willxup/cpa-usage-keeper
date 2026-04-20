package api

import (
	"net/http"
	"strings"

	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/models"
	"cpa-usage-keeper/internal/redact"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

type usageResponse struct {
	Usage any `json:"usage"`
}

func registerUsageRoutes(
	router gin.IRoutes,
	usageProvider service.UsageProvider,
	authFileProvider service.AuthFileProvider,
	providerMetadataProvider service.ProviderMetadataProvider,
) {
	router.GET("/usage", func(c *gin.Context) {
		if usageProvider == nil {
			c.JSON(http.StatusOK, usageResponse{Usage: gin.H{
				"total_requests":   0,
				"success_count":    0,
				"failure_count":    0,
				"total_tokens":     0,
				"apis":             gin.H{},
				"requests_by_day":  gin.H{},
				"requests_by_hour": gin.H{},
				"tokens_by_day":    gin.H{},
				"tokens_by_hour":   gin.H{},
			}})
			return
		}

		usage, err := usageProvider.GetUsage(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		redactedUsage := redact.UsageSnapshot(usage)
		authFiles, providerMetadata, err := loadUsageResolutionData(c, authFileProvider, providerMetadataProvider)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		applyUsageSourceDisplay(redactedUsage, authFiles, providerMetadata)
		c.JSON(http.StatusOK, usageResponse{Usage: redactedUsage})
	})
}

func loadUsageResolutionData(
	c *gin.Context,
	authFileProvider service.AuthFileProvider,
	providerMetadataProvider service.ProviderMetadataProvider,
) ([]models.AuthFile, []models.ProviderMetadata, error) {
	authFiles := []models.AuthFile{}
	providerMetadata := []models.ProviderMetadata{}
	var err error
	if authFileProvider != nil {
		authFiles, err = authFileProvider.ListAuthFiles(c.Request.Context())
		if err != nil {
			return nil, nil, err
		}
	}
	if providerMetadataProvider != nil {
		providerMetadata, err = providerMetadataProvider.ListProviderMetadata(c.Request.Context())
		if err != nil {
			return nil, nil, err
		}
	}
	return authFiles, providerMetadata, nil
}

func applyUsageSourceDisplay(snapshot *cpa.StatisticsSnapshot, authFiles []models.AuthFile, providerMetadata []models.ProviderMetadata) {
	if snapshot == nil {
		return
	}

	authFileMap := make(map[string]models.AuthFile, len(authFiles))
	for _, file := range authFiles {
		authIndex := strings.TrimSpace(file.AuthIndex)
		if authIndex == "" {
			continue
		}
		authFileMap[authIndex] = file
	}

	providerMetadataMap := make(map[string]models.ProviderMetadata, len(providerMetadata))
	for _, item := range providerMetadata {
		lookupKey := strings.TrimSpace(item.LookupKey)
		if lookupKey == "" {
			continue
		}
		providerMetadataMap[lookupKey] = item
	}

	for apiName, apiSnapshot := range snapshot.APIs {
		for modelName, modelSnapshot := range apiSnapshot.Models {
			for i := range modelSnapshot.Details {
				resolved := resolveUsageSource(modelSnapshot.Details[i].Source, modelSnapshot.Details[i].AuthIndex, authFileMap, providerMetadataMap)
				modelSnapshot.Details[i].SourceRaw = modelSnapshot.Details[i].Source
				modelSnapshot.Details[i].Source = resolved.DisplayName
				modelSnapshot.Details[i].SourceDisplay = resolved.DisplayName
				modelSnapshot.Details[i].SourceType = resolved.SourceType
				modelSnapshot.Details[i].SourceKey = resolved.SourceKey
			}
			apiSnapshot.Models[modelName] = modelSnapshot
		}
		snapshot.APIs[apiName] = apiSnapshot
	}
}

type usageSourceResolution struct {
	DisplayName string
	SourceType  string
	SourceKey   string
}

func resolveUsageSource(
	rawSource string,
	authIndex string,
	authFiles map[string]models.AuthFile,
	providerMetadata map[string]models.ProviderMetadata,
) usageSourceResolution {
	normalizedSource := strings.TrimSpace(rawSource)
	if normalizedSource != "" {
		if item, ok := providerMetadata[normalizedSource]; ok {
			displayName := firstNonEmptyString(item.DisplayName, item.ProviderType, redact.APIKeyDisplayName(normalizedSource))
			providerType := strings.TrimSpace(item.ProviderType)
			providerKey := strings.TrimSpace(item.ProviderKey)
			if providerKey == "" {
				providerKey = "provider:" + firstNonEmptyString(providerType, displayName)
			}
			return usageSourceResolution{
				DisplayName: displayName,
				SourceType:  providerType,
				SourceKey:   providerKey,
			}
		}
	}

	normalizedAuthIndex := strings.TrimSpace(authIndex)
	if normalizedAuthIndex != "" {
		if file, ok := authFiles[normalizedAuthIndex]; ok {
			displayName := firstNonEmptyString(file.Email, file.Label, file.Name, normalizedAuthIndex)
			return usageSourceResolution{
				DisplayName: displayName,
				SourceType:  firstNonEmptyString(file.Type, file.Provider),
				SourceKey:   "auth:" + normalizedAuthIndex,
			}
		}
	}

	if normalizedSource == "" {
		return usageSourceResolution{DisplayName: "-", SourceKey: "raw:-"}
	}
	if looksLikeEmail(normalizedSource) {
		return usageSourceResolution{
			DisplayName: normalizedSource,
			SourceKey:   "email:" + normalizedSource,
		}
	}
	if inferredProvider := inferUsageProviderType(normalizedSource); inferredProvider != "" {
		return usageSourceResolution{
			DisplayName: inferredProvider,
			SourceType:  inferredProvider,
			SourceKey:   "provider:fallback:" + inferredProvider,
		}
	}
	masked := redact.APIKeyDisplayName(normalizedSource)
	return usageSourceResolution{
		DisplayName: masked,
		SourceKey:   "raw:" + masked,
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func looksLikeEmail(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	atIndex := strings.Index(trimmed, "@")
	return atIndex > 0 && atIndex < len(trimmed)-1 && strings.Contains(trimmed[atIndex+1:], ".")
}

func inferUsageProviderType(source string) string {
	value := strings.ToLower(strings.TrimSpace(source))
	switch {
	case value == "":
		return ""
	case strings.Contains(value, "ampcode"):
		return "ampcode"
	case strings.HasPrefix(value, "sk-ant-") || strings.Contains(value, "anthropic") || strings.Contains(value, "claude"):
		return "claude"
	case strings.HasPrefix(value, "sk-proj-") || strings.HasPrefix(value, "sk-") || strings.Contains(value, "openai") || strings.Contains(value, "gpt"):
		return "openai"
	case strings.HasPrefix(value, "aiza") || strings.Contains(value, "gemini"):
		return "gemini"
	case strings.Contains(value, "vertex"):
		return "vertex"
	case strings.Contains(value, "codex"):
		return "codex"
	default:
		return ""
	}
}
