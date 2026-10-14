package handler

import (
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTeamOIDCStartStaysDisabledWithoutRedirectOrNonce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &AuthHandler{configInfo: &config.Config{OIDCAuth: &config.OIDCAuthConfig{Enable: true}}}
	r := gin.New()
	r.Use(errorCapture())
	r.GET("/auth/oidc/start", h.OIDCStart)
	req := httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden || w.Header().Get("Location") != "" || len(w.Result().Cookies()) != 0 {
		t.Fatalf("disabled OIDC emitted redirect/session: status=%d", w.Code)
	}
}
