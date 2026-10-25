package api

func (a *API) InitRoutes() {
	a.Router.GET("/", a.Home)

	v1 := a.Router.Group("/api/v1")

	sensorRoutes := v1.Group("/sensors")
	modelRoutes := v1.Group("/models")
	incidentRoutes := v1.Group("/incidents")
	if a.mtlsEnabled {
		sensorRoutes.Use(requireClientCert())
		modelRoutes.Use(requireClientCert(), requireOperator())
		incidentRoutes.Use(requireClientCert(), requireOperator())
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
		sensorRoutes.PUT("/:host_id/config", requireWriteOperator(), a.SetDesiredConfig)
		sensorRoutes.PUT("/:host_id/config/ack", requireSelfOrWriteOperator(), a.AckActualConfig)
		sensorRoutes.POST("/:host_id/certificate", requireWriteOperator(), a.IssueSensorCertificate)
		sensorRoutes.GET("/:host_id/certificate", requireOperator(), a.ListSensorCertificates)
		sensorRoutes.PUT("/:host_id/certificate/revoke", requireWriteOperator(), a.RevokeSensorCertificate)
	} else {
		sensorRoutes.GET("", a.ListSensors)
		sensorRoutes.GET("/:host_id", a.GetSensor)
		sensorRoutes.GET("/:host_id/config", a.GetDesiredConfig)
		sensorRoutes.PUT("/:host_id/config", a.SetDesiredConfig)
		sensorRoutes.PUT("/:host_id/config/ack", a.AckActualConfig)
		sensorRoutes.POST("/:host_id/certificate", a.IssueSensorCertificate)
		sensorRoutes.GET("/:host_id/certificate", a.ListSensorCertificates)
		sensorRoutes.PUT("/:host_id/certificate/revoke", a.RevokeSensorCertificate)
	}

	modelRoutes.GET("", a.ListModels)
	modelRoutes.GET("/:name/:version", a.GetModel)
	modelRoutes.GET("/:name/:version/artifact", a.DownloadModelArtifact)
	modelRoutes.GET("/:name/:version/onnx", a.DownloadModelOnnxArtifact)
	if a.mtlsEnabled {
		modelRoutes.POST("", requireWriteOperator(), a.RegisterModel)
		modelRoutes.PUT("/:name/:version/artifact", requireWriteOperator(), a.UploadModelArtifact)
		modelRoutes.PUT("/:name/:version/onnx", requireWriteOperator(), a.UploadModelOnnxArtifact)
	} else {
		modelRoutes.POST("", a.RegisterModel)
		modelRoutes.PUT("/:name/:version/artifact", a.UploadModelArtifact)
		modelRoutes.PUT("/:name/:version/onnx", a.UploadModelOnnxArtifact)
	}

	incidentRoutes.GET("", a.ListIncidents)
	incidentRoutes.GET("/:id", a.GetIncident)
	if a.mtlsEnabled {
		incidentRoutes.PUT("/:id/close", requireWriteOperator(), a.CloseIncident)
	} else {
		incidentRoutes.PUT("/:id/close", a.CloseIncident)
	}
}
