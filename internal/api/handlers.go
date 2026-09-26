package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (a *API) Home(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, gin.H{
		"message": "netstream control plane",
	})
}
