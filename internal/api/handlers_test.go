package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"network-monitor-backend/internal/logger"

	"github.com/gin-gonic/gin"
)

func TestHomeHandlerReturnsOK(t *testing.T) {
	if logger.Logger == nil {
		if err := logger.New(); err != nil {
			t.Fatalf("failed to initialize logger: %v", err)
		}
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	a := &API{}
	router.GET("/", a.Home)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}
