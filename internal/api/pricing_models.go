package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// registerCompletePricingRoutes 将模型完整配置读写接到现有管理员组，事务与修订由 PricingProvider 持有。
func registerCompletePricingRoutes(router gin.IRoutes, provider service.PricingProvider) {
	// 列表从同一已提交快照读取完整配置与配置修订。
	router.GET("/pricing/models", func(c *gin.Context) {
		if provider == nil {
			writeCompletePricingInternalError(c, errors.New("pricing provider is not configured"))
			return
		}
		response, err := provider.ListPricingModels(c.Request.Context())
		if err != nil {
			writeCompletePricingInternalError(c, err)
			return
		}
		if response.Models == nil {
			response.Models = []pricing.ModelPricingConfig{}
		}
		c.JSON(http.StatusOK, response)
	})
	// 添加弹框沿用现有完整模型标识来源与去重口径。
	router.GET("/pricing/model-options", func(c *gin.Context) {
		if provider == nil {
			writeCompletePricingInternalError(c, errors.New("pricing provider is not configured"))
			return
		}
		models, err := provider.ListUsedModels(c.Request.Context())
		if err != nil {
			writeCompletePricingInternalError(c, err)
			return
		}
		if models == nil {
			models = []string{}
		}
		c.JSON(http.StatusOK, servicedto.PricingModelOptionsResponse{Models: models})
	})
	// 保存交给服务层完整替换事务，不重写历史事件金额。
	router.PUT("/pricing/models", func(c *gin.Context) {
		if provider == nil {
			writeCompletePricingInternalError(c, errors.New("pricing provider is not configured"))
			return
		}
		var input pricing.ModelPricingConfig
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeCompletePricingDecodeError(c, err)
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			writeCompletePricingError(c, http.StatusBadRequest, "invalid_request", "Invalid model configuration", nil)
			return
		}
		response, err := provider.SavePricingModel(c.Request.Context(), input)
		if err != nil {
			writeCompletePricingServiceError(c, err)
			return
		}
		c.JSON(http.StatusOK, response)
	})
	// 删除仅移除当前配置和关联倍率，模型标识由 query 承载斜杠。
	router.DELETE("/pricing/models", func(c *gin.Context) {
		if provider == nil {
			writeCompletePricingInternalError(c, errors.New("pricing provider is not configured"))
			return
		}
		model := strings.TrimSpace(c.Query("model"))
		if model == "" {
			writeCompletePricingError(c, http.StatusBadRequest, "invalid_request", "Model is required", []servicedto.PricingFieldError{{Path: "model", Code: "required"}})
			return
		}
		response, err := provider.DeletePricingModel(c.Request.Context(), model)
		if err != nil {
			writeCompletePricingServiceError(c, err)
			return
		}
		c.JSON(http.StatusOK, response)
	})
}

// writeCompletePricingDecodeError 区分合法 JSON 的字段缺失与损坏或未知 JSON 结构。
func writeCompletePricingDecodeError(c *gin.Context, err error) {
	var field *pricing.ValidationError
	if errors.As(err, &field) {
		writeCompletePricingError(c, http.StatusBadRequest, "invalid_pricing", "Invalid model pricing", []servicedto.PricingFieldError{{Path: field.Path, Code: field.Code}})
		return
	}
	writeCompletePricingError(c, http.StatusBadRequest, "invalid_request", "Invalid model configuration", nil)
}

// writeCompletePricingServiceError 对重算期间配置写忙和字段错误返回稳定业务码，内部失败只返回公共摘要。
func writeCompletePricingServiceError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrPricingBusy) {
		writeCompletePricingError(c, http.StatusConflict, "pricing_busy", "Pricing recalculation is running", nil)
		return
	}
	var conflict *pricing.BranchConflictError
	if errors.As(err, &conflict) {
		fields := make([]servicedto.PricingFieldError, 0, len(conflict.FieldPaths))
		for _, path := range conflict.FieldPaths {
			fields = append(fields, servicedto.PricingFieldError{Path: path, Code: "conflict", BranchIDs: []string{conflict.BranchIDs[0], conflict.BranchIDs[1]}})
		}
		writeCompletePricingError(c, http.StatusBadRequest, "branch_conflict", "Pricing branches overlap", fields)
		return
	}
	if errors.Is(err, service.ErrPricingModelNotFound) {
		writeCompletePricingError(c, http.StatusNotFound, "model_not_found", "Model pricing was not found", nil)
		return
	}
	if errors.Is(err, service.ErrInvalidPricingInput) {
		var field *pricing.ValidationError
		if errors.As(err, &field) {
			writeCompletePricingError(c, http.StatusBadRequest, "invalid_pricing", "Invalid model pricing", []servicedto.PricingFieldError{{Path: field.Path, Code: field.Code}})
			return
		}
		writeCompletePricingError(c, http.StatusBadRequest, "invalid_pricing", "Invalid model pricing", nil)
		return
	}
	writeCompletePricingInternalError(c, err)
}

func writeCompletePricingError(c *gin.Context, status int, code, message string, fields []servicedto.PricingFieldError) {
	c.JSON(status, servicedto.PricingErrorResponse{Code: code, Message: message, Fields: fields})
}

// writeCompletePricingInternalError 仅记日志细节，客户端不接收数据库语句或内部路径。
func writeCompletePricingInternalError(c *gin.Context, err error) {
	logrus.WithError(err).Error("complete pricing request failed")
	writeCompletePricingError(c, http.StatusInternalServerError, "internal_error", "Internal server error", nil)
}
