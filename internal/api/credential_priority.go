package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"

	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

const maxCredentialPriority = int64(9_007_199_254_740_991)

var credentialPriorityIntegerPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

type credentialPriorityRequest struct {
	Priority json.RawMessage `json:"priority"`
}

func registerCredentialPriorityRoutes(router gin.IRoutes, provider service.CredentialPriorityProvider) {
	register := func(path string, update func(*gin.Context, int) (service.CredentialPriorityResponse, error)) {
		router.PATCH(path, func(c *gin.Context) {
			if provider == nil {
				writeInternalError(c, "credential priority provider is not configured", nil)
				return
			}
			priority, ok := bindCredentialPriorityRequest(c)
			if !ok {
				return
			}
			response, err := update(c, priority)
			if err != nil {
				writeCredentialPriorityError(c, err)
				return
			}
			c.JSON(http.StatusOK, response)
		})
	}
	register("/auth-files/:auth_index/priority", func(c *gin.Context, priority int) (service.CredentialPriorityResponse, error) {
		return provider.SetAuthFilePriority(c.Request.Context(), c.Param("auth_index"), priority)
	})
	register("/ai-providers/:auth_index/priority", func(c *gin.Context, priority int) (service.CredentialPriorityResponse, error) {
		return provider.SetAIProviderPriority(c.Request.Context(), c.Param("auth_index"), priority)
	})
}

func bindCredentialPriorityRequest(c *gin.Context) (int, bool) {
	var request credentialPriorityRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "priority must be a safe integer"})
		return 0, false
	}
	raw := bytes.TrimSpace(request.Priority)
	if !credentialPriorityIntegerPattern.Match(raw) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "priority must be a safe integer"})
		return 0, false
	}
	parsed, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || parsed < -maxCredentialPriority || parsed > maxCredentialPriority || int64(int(parsed)) != parsed {
		c.JSON(http.StatusBadRequest, gin.H{"error": "priority must be a safe integer"})
		return 0, false
	}
	return int(parsed), true
}

func writeCredentialPriorityError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrCredentialPriorityValidation):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid credential priority request"})
	case errors.Is(err, service.ErrCredentialPriorityNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "credential not found"})
	case errors.Is(err, service.ErrCredentialPriorityUnsupported):
		c.JSON(http.StatusConflict, gin.H{"error": "credential priority is not supported"})
	case errors.Is(err, service.ErrCredentialPriorityConflict):
		c.JSON(http.StatusConflict, gin.H{"error": "credential priority target cannot be changed on its own"})
	default:
		writeInternalError(c, "credential priority update failed", err)
	}
}
