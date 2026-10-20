package api

func (a *API) InitRoutes() {
	a.Router.GET("/", a.Home)

	v1 := a.Router.Group("/api/v1")

	sensorRoutes := v1.Group("/sensors")
	modelRoutes := v1.Group("/models")
	if a.mtlsEnabled {
		sensorRoutes.Use(requireClientCert())
		modelRoutes.Use(requireClientCert(), requireOperator())
	}

	// A sensor enrolls itself (no host_id yet to scope to) and only an
	// operator should see the registry as a whole; everything keyed by
	// :host_id is either the sensor acting on itself or an operator
	// acting on any sensor.
	sensorRoutes.POST("/register", a.RegisterSensor)
	if a.mtlsEnabled {
		sensorRoutes.GET("", requireOperator(), a.ListSensors)
		sensorRoutes.GET("/:host_id", requireSelfOrOperator(), a.GetSensor)
		sensorRoutes.GET("/:host_id/config", requireSelfOrOperator(), a.GetDesiredConfig)
		sensorRoutes.PUT("/:host_id/config", requireOperator(), a.SetDesiredConfig)
		sensorRoutes.PUT("/:host_id/config/ack", requireSelfOrOperator(), a.AckActualConfig)
	} else {
		sensorRoutes.GET("", a.ListSensors)
		sensorRoutes.GET("/:host_id", a.GetSensor)
		sensorRoutes.GET("/:host_id/config", a.GetDesiredConfig)
		sensorRoutes.PUT("/:host_id/config", a.SetDesiredConfig)
		sensorRoutes.PUT("/:host_id/config/ack", a.AckActualConfig)
	}

	modelRoutes.POST("", a.RegisterModel)
	modelRoutes.GET("", a.ListModels)
	modelRoutes.GET("/:name/:version", a.GetModel)
	modelRoutes.PUT("/:name/:version/artifact", a.UploadModelArtifact)
	modelRoutes.GET("/:name/:version/artifact", a.DownloadModelArtifact)
}
