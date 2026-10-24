package api

import (
	"time"

	"network-monitor-backend/internal/incidents"
	"network-monitor-backend/internal/models"
	"network-monitor-backend/internal/pki"
	"network-monitor-backend/internal/sensorcerts"
	"network-monitor-backend/internal/sensors"
	"network-monitor-backend/internal/storage/objectstore"

	"github.com/gin-gonic/gin"
)

type API struct {
	Router      *gin.Engine
	sensors     *sensors.Service
	models      *models.Service
	incidents   *incidents.Service
	sensorCerts *sensorcerts.Service
	objectStore *objectstore.Store
	// ca is nil unless CA_CERT_FILE/CA_KEY_FILE were configured (see
	// cmd/main.go) — IssueSensorCertificate checks this itself rather
	// than the routes being conditionally registered, since whether PKI
	// is configured is independent of whether mTLS is.
	ca           *pki.CA
	certValidity time.Duration
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
	sensorCertService *sensorcerts.Service,
	objectStore *objectstore.Store,
	ca *pki.CA,
	certValidity time.Duration,
	mtlsEnabled bool,
) *API {
	r := gin.Default()
	api := &API{
		Router:       r,
		sensors:      sensorService,
		models:       modelService,
		incidents:    incidentService,
		sensorCerts:  sensorCertService,
		objectStore:  objectStore,
		ca:           ca,
		certValidity: certValidity,
		mtlsEnabled:  mtlsEnabled,
	}

	api.InitRoutes()
	return api
}
