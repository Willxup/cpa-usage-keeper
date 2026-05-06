package api

import (
	"cpa-usage-keeper/internal/models"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

// loadUsageResolutionData 为 Request Events 和 Credentials 加载 source 解析所需的活跃 usage identities。
func loadUsageResolutionData(
	c *gin.Context,
	usageIdentityProvider service.UsageIdentityProvider,
) ([]models.UsageIdentity, error) {
	if usageIdentityProvider == nil {
		return []models.UsageIdentity{}, nil
	}

	// Request Events 的 Source 下拉和 Credentials 的展示解析只需要活跃身份，直接调用 SQL 层 active-only 查询。
	return usageIdentityProvider.ListActiveUsageIdentities(c.Request.Context())
}
