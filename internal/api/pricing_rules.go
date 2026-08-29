package api

import (
	"errors"
	"net/http"
	"strings"

	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
	"github.com/gin-gonic/gin"
)

type pricingRuleResponse struct {
	Key        string  `json:"key"`
	Value      string  `json:"value"`
	Multiplier float64 `json:"multiplier"`
}

type pricingRulesResponse struct {
	Model string                `json:"model"`
	Rules []pricingRuleResponse `json:"rules"`
}

type pricingRuleRequest struct {
	Key        string   `json:"key"`
	Value      string   `json:"value"`
	Multiplier *float64 `json:"multiplier"`
}

type replacePricingRulesRequest struct {
	Model string               `json:"model"`
	Rules []pricingRuleRequest `json:"rules"`
}

func registerPricingRuleRoutes(router gin.IRoutes, provider service.PricingProvider) {
	router.GET("/pricing/rules", func(c *gin.Context) {
		if provider == nil {
			c.JSON(http.StatusNotImplemented, gin.H{"error": "pricing provider is not configured"})
			return
		}
		model := strings.TrimSpace(c.Query("model"))
		if model == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "model is required"})
			return
		}
		rules, err := provider.ListPricingRules(c.Request.Context(), model)
		if err != nil {
			writePricingRulesError(c, "list pricing rules failed", err)
			return
		}
		c.JSON(http.StatusOK, newPricingRulesResponse(model, rules))
	})

	router.PUT("/pricing/rules", func(c *gin.Context) {
		if provider == nil {
			c.JSON(http.StatusNotImplemented, gin.H{"error": "pricing provider is not configured"})
			return
		}
		var request replacePricingRulesRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		model := strings.TrimSpace(request.Model)
		if model == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "model is required"})
			return
		}
		inputs := make([]servicedto.PricingRuleInput, len(request.Rules))
		for index := range request.Rules {
			inputs[index] = servicedto.PricingRuleInput{
				Key:        request.Rules[index].Key,
				Value:      request.Rules[index].Value,
				Multiplier: request.Rules[index].Multiplier,
			}
		}
		rules, err := provider.ReplacePricingRules(c.Request.Context(), servicedto.ReplacePricingRulesInput{
			Model: model,
			Rules: inputs,
		})
		if err != nil {
			writePricingRulesError(c, "replace pricing rules failed", err)
			return
		}
		c.JSON(http.StatusOK, newPricingRulesResponse(model, rules))
	})
}

func newPricingRulesResponse(model string, rules []servicedto.PricingRule) pricingRulesResponse {
	responseRules := make([]pricingRuleResponse, len(rules))
	for index := range rules {
		responseRules[index] = pricingRuleResponse{
			Key:        rules[index].Key,
			Value:      rules[index].Value,
			Multiplier: rules[index].Multiplier,
		}
	}
	return pricingRulesResponse{Model: model, Rules: responseRules}
}

func writePricingRulesError(c *gin.Context, message string, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidPricingRule):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrPricingModelNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	default:
		writeInternalError(c, message, err)
	}
}
