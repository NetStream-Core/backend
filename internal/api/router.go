package api

import (
	"github.com/gin-gonic/gin"
)

type API struct {
	Router *gin.Engine
}

func (a *API) InitRoutes() {
	a.Router.GET(Routes["home"], HomeHandler)
}

func New() *API {
	r := gin.Default()
	api := &API{
		Router: r,
	}

	api.InitRoutes()
	return api
}
