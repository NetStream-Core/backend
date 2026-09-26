package api

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"network-monitor-backend/internal/logger"
	"network-monitor-backend/internal/models"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const presignedDownloadExpiry = 15 * time.Minute

func (a *API) RegisterModel(ctx *gin.Context) {
	var req models.RegisterRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	model, err := a.models.Register(ctx.Request.Context(), req)
	if err != nil {
		logger.Logger.Error("Failed to register model", zap.Error(err))
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, model)
}

func (a *API) ListModels(ctx *gin.Context) {
	result, err := a.models.List(ctx.Request.Context())
	if err != nil {
		logger.Logger.Error("Failed to list models", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list models"})
		return
	}
	ctx.JSON(http.StatusOK, result)
}

func (a *API) GetModel(ctx *gin.Context) {
	model, err := a.models.Get(ctx.Request.Context(), ctx.Param("name"), ctx.Param("version"))
	if errors.Is(err, models.ErrNotFound) {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "model not found"})
		return
	}
	if err != nil {
		logger.Logger.Error("Failed to get model", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get model"})
		return
	}
	ctx.JSON(http.StatusOK, model)
}

// UploadModelArtifact stores the request body as the model's artifact and
// records the resulting URI on the registry entry. The model must already
// be registered (RegisterModel first) — this only ever attaches a blob to
// an existing catalog entry, it never creates one.
func (a *API) UploadModelArtifact(ctx *gin.Context) {
	name, version := ctx.Param("name"), ctx.Param("version")

	if _, err := a.models.Get(ctx.Request.Context(), name, version); errors.Is(err, models.ErrNotFound) {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "model not found; register it before uploading an artifact"})
		return
	} else if err != nil {
		logger.Logger.Error("Failed to look up model before upload", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "failed to look up model"})
		return
	}

	if ctx.Request.ContentLength <= 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Content-Length is required"})
		return
	}

	key := fmt.Sprintf("%s/%s/artifact", name, version)
	contentType := ctx.ContentType()
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	uri, err := a.objectStore.Upload(ctx.Request.Context(), key, ctx.Request.Body, ctx.Request.ContentLength, contentType)
	if err != nil {
		logger.Logger.Error("Failed to upload model artifact", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "failed to upload artifact"})
		return
	}

	model, err := a.models.SetArtifactURI(ctx.Request.Context(), name, version, uri)
	if err != nil {
		logger.Logger.Error("Failed to record artifact uri", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "failed to record artifact uri"})
		return
	}
	ctx.JSON(http.StatusOK, model)
}

// DownloadModelArtifact redirects to a time-limited direct link to the
// artifact, so the file's bytes never have to be proxied through the
// backend.
func (a *API) DownloadModelArtifact(ctx *gin.Context) {
	model, err := a.models.Get(ctx.Request.Context(), ctx.Param("name"), ctx.Param("version"))
	if errors.Is(err, models.ErrNotFound) {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "model not found"})
		return
	}
	if err != nil {
		logger.Logger.Error("Failed to get model", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get model"})
		return
	}
	if model.ArtifactURI == "" {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "model has no artifact uploaded"})
		return
	}

	key, err := a.objectStore.ParseKey(model.ArtifactURI)
	if err != nil {
		logger.Logger.Error("Failed to parse artifact uri", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse artifact uri"})
		return
	}

	url, err := a.objectStore.PresignedDownloadURL(ctx.Request.Context(), key, presignedDownloadExpiry)
	if err != nil {
		logger.Logger.Error("Failed to presign artifact download", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "failed to presign download"})
		return
	}
	ctx.Redirect(http.StatusFound, url)
}
