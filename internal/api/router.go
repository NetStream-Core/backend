package api

func (a *API) InitRoutes() {
	a.Router.GET("/", a.Home)

	v1 := a.Router.Group("/api/v1")

	sensorRoutes := v1.Group("/sensors")
	sensorRoutes.POST("/register", a.RegisterSensor)
	sensorRoutes.GET("", a.ListSensors)
	sensorRoutes.GET("/:host_id", a.GetSensor)
	sensorRoutes.GET("/:host_id/config", a.GetDesiredConfig)
	sensorRoutes.PUT("/:host_id/config", a.SetDesiredConfig)

	modelRoutes := v1.Group("/models")
	modelRoutes.POST("", a.RegisterModel)
	modelRoutes.GET("", a.ListModels)
	modelRoutes.GET("/:name/:version", a.GetModel)
	modelRoutes.PUT("/:name/:version/artifact", a.UploadModelArtifact)
	modelRoutes.GET("/:name/:version/artifact", a.DownloadModelArtifact)
}
