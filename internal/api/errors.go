package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func writeInternalError(c *gin.Context, message string, err error) {
	if err != nil {
		logrus.WithError(err).Error(message)
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
}
