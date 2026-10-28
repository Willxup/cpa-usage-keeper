package api

import (
	"errors"
	"net/http"

	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

type credentialsRequest struct {
	Names []string `json:"names"`
}

type credentialsStatusRequest struct {
	Names    []string `json:"names"`
	Disabled bool     `json:"disabled"`
}

func registerCredentialManagementRoutes(router gin.IRoutes, provider service.CredentialsManagementProvider) {
	router.PATCH("/credentials/status", func(c *gin.Context) {
		if provider == nil {
			writeInternalError(c, "credentials management provider is not configured", nil)
			return
		}

		var request credentialsStatusRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "names are required"})
			return
		}

		response, err := provider.SetCredentialsDisabled(c.Request.Context(), request.Names, request.Disabled)
		if err != nil {
			if errors.Is(err, service.ErrCredentialsManagementValidation) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "names are required"})
				return
			}
			writeInternalError(c, "credentials status update failed", err)
			return
		}
		c.JSON(http.StatusOK, response)
	})

	router.DELETE("/credentials", func(c *gin.Context) {
		if provider == nil {
			writeInternalError(c, "credentials management provider is not configured", nil)
			return
		}

		var request credentialsRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "names are required"})
			return
		}

		response, err := provider.DeleteCredentials(c.Request.Context(), request.Names)
		if err != nil {
			if errors.Is(err, service.ErrCredentialsManagementValidation) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "names are required"})
				return
			}
			writeInternalError(c, "credentials delete failed", err)
			return
		}
		c.JSON(http.StatusOK, response)
	})
}
