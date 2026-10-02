package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"

	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// registerPricingSyncContractRoutes 暴露审核来源价格和整批应用两个管理员操作。
func registerPricingSyncContractRoutes(router gin.IRoutes, provider service.PricingProvider) {
	// 拉取只读取来源元数据与本地模型，不改已保存配置或费用。
	router.GET("/pricing/sync/fetch", func(c *gin.Context) {
		if provider == nil {
			writeCompletePricingInternalError(c, errors.New("pricing provider is not configured"))
			return
		}
		source := c.Query("source")
		if !validPricingSyncHTTPSource(source) {
			writePricingSyncSourceError(c, source == "")
			return
		}
		response, err := provider.FetchPricingSync(c.Request.Context(), source)
		if err != nil {
			writePricingSyncFetchError(c, err)
			return
		}
		if response.Matches == nil {
			response.Matches = []servicedto.PricingSyncFetchMatch{}
		}
		if response.UnmatchedModels == nil {
			response.UnmatchedModels = []string{}
		}
		c.JSON(http.StatusOK, response)
	})
	// 应用只提交审核后选中的基础价；旧配置风格、倍率、条件和分支由服务层事务保持。
	router.POST("/pricing/sync/apply", func(c *gin.Context) {
		if provider == nil {
			writeCompletePricingInternalError(c, errors.New("pricing provider is not configured"))
			return
		}
		var input servicedto.PricingSyncApplyRequest
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writePricingSyncApplyDecodeError(c, err)
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			writeCompletePricingError(c, http.StatusBadRequest, "invalid_request", "Invalid pricing sync request", nil)
			return
		}
		if !validPricingSyncHTTPSource(input.Source) {
			writePricingSyncSourceError(c, input.Source == "")
			return
		}
		response, err := provider.ApplyPricingSync(c.Request.Context(), input)
		if err != nil {
			writePricingSyncApplyError(c, err)
			return
		}
		c.JSON(http.StatusOK, response)
	})
}

func validPricingSyncHTTPSource(source string) bool {
	return source == "models-dev" || source == "litellm"
}

func writePricingSyncSourceError(c *gin.Context, missing bool) {
	fieldCode := "invalid"
	if missing {
		fieldCode = "required"
	}
	writeCompletePricingError(c, http.StatusBadRequest, "invalid_request", "Select a pricing source", []servicedto.PricingFieldError{{Path: "source", Code: fieldCode}})
}

// writePricingSyncApplyDecodeError 保留 items[i] 的结构化错误，来源缺失仍属于请求参数错误。
func writePricingSyncApplyDecodeError(c *gin.Context, err error) {
	var field *pricing.ValidationError
	if errors.As(err, &field) && field.Path == "source" {
		writePricingSyncSourceError(c, true)
		return
	}
	writeCompletePricingDecodeError(c, err)
}

// writePricingSyncFetchError 只把真实来源超时标为 504，取消和其他内部故障不冒充来源超时。
func writePricingSyncFetchError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrInvalidPricingSyncSource) {
		writePricingSyncSourceError(c, false)
		return
	}
	var networkError net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &networkError) && networkError.Timeout() {
		logrus.WithError(err).Error("pricing source request timed out")
		writeCompletePricingError(c, http.StatusGatewayTimeout, "price_source_timeout", "Pricing source timed out", nil)
		return
	}
	writeCompletePricingServiceError(c, err)
}

// writePricingSyncApplyError 仅映射输入校验；数据库事务错误保留安全内部摘要。
func writePricingSyncApplyError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrInvalidPricingSyncSource) {
		writePricingSyncSourceError(c, false)
		return
	}
	writeCompletePricingServiceError(c, err)
}
