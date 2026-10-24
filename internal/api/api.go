package api

import (
	"network-monitor-backend/internal/incidents"
	"network-monitor-backend/internal/models"
	"network-monitor-backend/internal/sensors"
	"network-monitor-backend/internal/storage/objectstore"

	"github.com/gin-gonic/gin"
)

type API struct {
	Router      *gin.Engine
	sensors     *sensors.Service
	models      *models.Service
	incidents   *incidents.Service
	objectStore *objectstore.Store
	// mtlsEnabled gates every auth middleware in InitRoutes. It is true
	// only when the server was actually started with a client CA
	// configured (see cmd/main.go) — with it false, routes behave exactly
	// as before mTLS existed, for local development and existing tests
	// that talk to the API over plain HTTP.
	mtlsEnabled bool
}

func New(
	sensorService *sensors.Service,
	modelService *models.Service,
	incidentService *incidents.Service,
	objectStore *objectstore.Store,
	mtlsEnabled bool,
) *API {
	r := gin.Default()
	api := &API{
		Router:      r,
		sensors:     sensorService,
		models:      modelService,
		incidents:   incidentService,
		objectStore: objectStore,
		mtlsEnabled: mtlsEnabled,
	}

	api.InitRoutes()
	return api
}
