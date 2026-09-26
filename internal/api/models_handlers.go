package api

import (
	"errors"
	"net/http"

	"network-monitor-backend/internal/logger"
	"network-monitor-backend/internal/models"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func (a *API) RegisterModel(ctx *gin.Context) {
	var req models.RegisterRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	model, err := a.models.Register(ctx.Request.Context(), req)
	if err != nil {
		logger.Logger.Error("Failed to register model", zap.Error(err))
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, model)
}

func (a *API) ListModels(ctx *gin.Context) {
	result, err := a.models.List(ctx.Request.Context())
	if err != nil {
		logger.Logger.Error("Failed to list models", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list models"})
		return
	}
	ctx.JSON(http.StatusOK, result)
}

func (a *API) GetModel(ctx *gin.Context) {
	model, err := a.models.Get(ctx.Request.Context(), ctx.Param("name"), ctx.Param("version"))
	if errors.Is(err, models.ErrNotFound) {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "model not found"})
		return
	}
	if err != nil {
		logger.Logger.Error("Failed to get model", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get model"})
		return
	}
	ctx.JSON(http.StatusOK, model)
}
