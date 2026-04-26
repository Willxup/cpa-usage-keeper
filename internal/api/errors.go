package api

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

func writeInternalError(c *gin.Context, message string, err error) {
	if err != nil {
		slog.Error(message, "error", err)
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
}
