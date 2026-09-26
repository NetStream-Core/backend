package api

import (
	"network-monitor-backend/internal/models"
	"network-monitor-backend/internal/sensors"
	"network-monitor-backend/internal/storage/objectstore"

	"github.com/gin-gonic/gin"
)

type API struct {
	Router      *gin.Engine
	sensors     *sensors.Service
	models      *models.Service
	objectStore *objectstore.Store
}

func New(sensorService *sensors.Service, modelService *models.Service, objectStore *objectstore.Store) *API {
	r := gin.Default()
	api := &API{
		Router:      r,
		sensors:     sensorService,
		models:      modelService,
		objectStore: objectStore,
	}

	api.InitRoutes()
	return api
}
