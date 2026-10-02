package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"github.com/gin-gonic/gin"
)

// registerPricingRecalculationRoutes 提供管理员手工重算的边界、唯一任务启动及当前状态。
// 这三条路由不读取费用结果；运行期也允许弹框和进度轮询继续访问。
func registerPricingRecalculationRoutes(router gin.IRoutes, provider service.PricingProvider) {
	router.GET("/pricing/recalculations/options", func(c *gin.Context) {
		if provider == nil {
			writeCompletePricingInternalError(c, errors.New("pricing provider is not configured"))
			return
		}
		options, err := provider.GetPricingRecalculationOptions(c.Request.Context())
		if err != nil {
			writePricingRecalculationError(c, err)
			return
		}
		c.JSON(http.StatusOK, options)
	})

	router.POST("/pricing/recalculations", func(c *gin.Context) {
		if provider == nil {
			writeCompletePricingInternalError(c, errors.New("pricing provider is not configured"))
			return
		}
		// 指针区分缺失/null 与合法修订号 0；服务层再校验实际整点、范围和修订。
		var input struct {
			StartAt        *time.Time `json:"start_at"`
			ConfigRevision *int64     `json:"config_revision"`
		}
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeCompletePricingError(c, http.StatusBadRequest, "invalid_request", "Invalid pricing recalculation request", nil)
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			writeCompletePricingError(c, http.StatusBadRequest, "invalid_request", "Invalid pricing recalculation request", nil)
			return
		}
		if input.StartAt == nil {
			writeCompletePricingError(c, http.StatusBadRequest, "invalid_request", "Start hour is required", []servicedto.PricingFieldError{{Path: "start_at", Code: "required"}})
			return
		}
		if input.ConfigRevision == nil {
			writeCompletePricingError(c, http.StatusBadRequest, "invalid_request", "Pricing configuration revision is required", []servicedto.PricingFieldError{{Path: "config_revision", Code: "required"}})
			return
		}
		result, err := provider.StartPricingRecalculation(c.Request.Context(), servicedto.StartRecalculationRequest{StartAt: *input.StartAt, ConfigRevision: *input.ConfigRevision})
		if err != nil {
			writePricingRecalculationError(c, err)
			return
		}
		if result.Started {
			c.JSON(http.StatusAccepted, result)
			return
		}
		c.JSON(http.StatusOK, result)
	})

	router.GET("/pricing/recalculations/current", func(c *gin.Context) {
		if provider == nil {
			writeCompletePricingInternalError(c, errors.New("pricing provider is not configured"))
			return
		}
		task, err := provider.CurrentPricingRecalculation(c.Request.Context())
		if err != nil {
			writePricingRecalculationError(c, err)
			return
		}
		// nil 是未启动或进程重启后的正常状态，不伪造空任务对象。
		c.JSON(http.StatusOK, task)
	})
}

// writePricingRecalculationError 将受理错误映射到稳定业务码，内部细节只写服务端日志。
func writePricingRecalculationError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrPricingBusy) {
		writeCompletePricingError(c, http.StatusConflict, "pricing_busy", "Pricing recalculation is running", nil)
		return
	}
	if errors.Is(err, service.ErrPricingChanged) {
		writeCompletePricingError(c, http.StatusConflict, "pricing_changed", "Pricing configuration changed", []servicedto.PricingFieldError{{Path: "config_revision", Code: "invalid"}})
		return
	}
	if errors.Is(err, service.ErrInvalidPricingRecalculationStart) {
		var field *pricing.ValidationError
		if errors.As(err, &field) {
			code := field.Code
			if code == "" {
				code = "invalid"
			}
			writeCompletePricingError(c, http.StatusBadRequest, "invalid_request", "Invalid pricing recalculation start", []servicedto.PricingFieldError{{Path: field.Path, Code: code}})
			return
		}
		writeCompletePricingError(c, http.StatusBadRequest, "invalid_request", "Invalid pricing recalculation start", nil)
		return
	}
	writeCompletePricingInternalError(c, err)
}
