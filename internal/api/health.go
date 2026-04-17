package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func registerHealthRoutes(router gin.IRoutes) {
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
}
