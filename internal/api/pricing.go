package api

import (
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

// registerPricingRoutes 仅注册完整模型配置、基础价同步和单次重算，统一沿用管理员认证边界。
func registerPricingRoutes(router gin.IRoutes, pricingProvider service.PricingProvider) {
	registerCompletePricingRoutes(router, pricingProvider)
	registerPricingSyncContractRoutes(router, pricingProvider)
	registerPricingRecalculationRoutes(router, pricingProvider)
}
