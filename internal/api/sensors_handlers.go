package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"network-monitor-backend/internal/logger"
	"network-monitor-backend/internal/sensors"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func (a *API) RegisterSensor(ctx *gin.Context) {
	var req sensors.RegisterRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	sensor, err := a.sensors.Register(ctx.Request.Context(), req)
	if err != nil {
		logger.Logger.Error("Failed to register sensor", zap.Error(err))
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, sensor)
}

func (a *API) ListSensors(ctx *gin.Context) {
	result, err := a.sensors.List(ctx.Request.Context())
	if err != nil {
		logger.Logger.Error("Failed to list sensors", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list sensors"})
		return
	}
	ctx.JSON(http.StatusOK, result)
}

func (a *API) GetSensor(ctx *gin.Context) {
	sensor, err := a.sensors.Get(ctx.Request.Context(), ctx.Param("host_id"))
	if errors.Is(err, sensors.ErrNotFound) {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "sensor not found"})
		return
	}
	if err != nil {
		logger.Logger.Error("Failed to get sensor", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get sensor"})
		return
	}
	ctx.JSON(http.StatusOK, sensor)
}

func (a *API) GetDesiredConfig(ctx *gin.Context) {
	config, err := a.sensors.GetDesiredConfig(ctx.Request.Context(), ctx.Param("host_id"))
	if errors.Is(err, sensors.ErrNotFound) {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "no config for this sensor"})
		return
	}
	if err != nil {
		logger.Logger.Error("Failed to get desired config", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get desired config"})
		return
	}
	ctx.JSON(http.StatusOK, config)
}

func (a *API) SetDesiredConfig(ctx *gin.Context) {
	var payload json.RawMessage
	if err := ctx.ShouldBindJSON(&payload); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	config, err := a.sensors.SetDesiredConfig(ctx.Request.Context(), ctx.Param("host_id"), payload)
	if errors.Is(err, sensors.ErrNotFound) {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "sensor not found"})
		return
	}
	if err != nil {
		logger.Logger.Error("Failed to set desired config", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "failed to set desired config"})
		return
	}
	ctx.JSON(http.StatusOK, config)
}
