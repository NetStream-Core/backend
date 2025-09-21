package api

import (
	"net/http"
	"network-monitor-backend/internal/logger"

	"github.com/gin-gonic/gin"
)

func HomeHandler(ctx *gin.Context) {
	logger.Logger.Info("Received request")
	ctx.JSON(http.StatusOK, gin.H{
		"message": "yo!",
	})
}
