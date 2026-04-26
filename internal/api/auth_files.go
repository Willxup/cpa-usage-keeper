package api

import (
	"net/http"

	"cpa-usage-keeper/internal/models"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

type authFilesResponse struct {
	Files []authFileResponse `json:"files"`
}

type authFileResponse struct {
	AuthIndex string `json:"auth_index"`
	Name      string `json:"name,omitempty"`
	Email     string `json:"email,omitempty"`
	Type      string `json:"type,omitempty"`
	Provider  string `json:"provider,omitempty"`
}

func registerAuthFileRoutes(router gin.IRoutes, authFileProvider service.AuthFileProvider) {
	router.GET("/auth-files", func(c *gin.Context) {
		if authFileProvider == nil {
			c.JSON(http.StatusOK, authFilesResponse{Files: []authFileResponse{}})
			return
		}

		files, err := authFileProvider.ListAuthFiles(c.Request.Context())
		if err != nil {
			writeInternalError(c, "list auth files failed", err)
			return
		}

		response := make([]authFileResponse, 0, len(files))
		for _, file := range files {
			response = append(response, mapAuthFileResponse(file))
		}
		c.JSON(http.StatusOK, authFilesResponse{Files: response})
	})
}

func mapAuthFileResponse(file models.AuthFile) authFileResponse {
	return authFileResponse{
		AuthIndex: file.AuthIndex,
		Name:      file.Name,
		Email:     file.Email,
		Type:      file.Type,
		Provider:  file.Provider,
	}
}
