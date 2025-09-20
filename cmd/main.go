package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func main() {
	logger, _ := zap.NewProduction()

	logger.Info("Backend started")

	r := gin.Default()

	r.GET("/", func(ctx *gin.Context) {
		logger.Info("Received request", zap.String("path", ctx.Request.UserAgent()))
		ctx.JSON(http.StatusOK, gin.H{
			"message": "yo!",
		})
	})

	if err := r.Run(":8081"); err != nil {
		logger.Fatal("Backend is down :(")
	}

	if err := logger.Sync(); err != nil {
		logger.Warn("Failed to sync logger", zap.Error(err))
	}
}
