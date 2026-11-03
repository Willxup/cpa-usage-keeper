package api

import (
	"net/http"

	"cpa-usage-keeper/internal/cpa/dto/models"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

// 模型广场只呈现 CPA 当前目录，不混入价格候选中的历史已用模型。
// 复用现有客户端的密钥读取及超时配置；不向浏览器转发原始响应或上游错误。
func registerModelsRoutes(router gin.IRoutes, provider service.ModelsFetcher) {
	router.GET("/models", func(c *gin.Context) {
		setNoStoreHeaders(c)
		if provider == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "model catalog is unavailable", "code": "models_unavailable"})
			return
		}
		result, err := provider.FetchModels(c.Request.Context())
		if err != nil || result == nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "CPA model catalog is unavailable", "code": "models_unavailable"})
			return
		}
		data := result.Payload.Data
		if data == nil {
			data = []models.ModelInfo{}
		}
		c.JSON(http.StatusOK, models.ModelsResponse{Object: "list", Data: data})
	})
}
