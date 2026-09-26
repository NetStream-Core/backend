package api

import (
	"network-monitor-backend/internal/models"
	"network-monitor-backend/internal/sensors"

	"github.com/gin-gonic/gin"
)

type API struct {
	Router  *gin.Engine
	sensors *sensors.Service
	models  *models.Service
}

func New(sensorService *sensors.Service, modelService *models.Service) *API {
	r := gin.Default()
	api := &API{
		Router:  r,
		sensors: sensorService,
		models:  modelService,
	}

	api.InitRoutes()
	return api
}
