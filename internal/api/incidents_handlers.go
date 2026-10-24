package api

import (
	"errors"
	"net/http"
	"strconv"

	"network-monitor-backend/internal/incidents"
	"network-monitor-backend/internal/logger"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func (a *API) ListIncidents(ctx *gin.Context) {
	list, err := a.incidents.List(ctx.Request.Context())
	if err != nil {
		logger.Logger.Error("Failed to list incidents", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, list)
}

func (a *API) GetIncident(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "id must be an integer"})
		return
	}

	incident, err := a.incidents.Get(ctx.Request.Context(), id)
	if errors.Is(err, incidents.ErrNotFound) {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "incident not found"})
		return
	}
	if err != nil {
		logger.Logger.Error("Failed to get incident", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, incident)
}

func (a *API) CloseIncident(ctx *gin.Context) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "id must be an integer"})
		return
	}

	incident, err := a.incidents.Close(ctx.Request.Context(), id)
	if errors.Is(err, incidents.ErrNotFound) {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "incident not found, or already closed"})
		return
	}
	if err != nil {
		logger.Logger.Error("Failed to close incident", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, incident)
}
