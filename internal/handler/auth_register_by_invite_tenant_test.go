package handler

import (
	"bytes"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTeamInviteRegistrationCannotBeEnabledByLegacyMode(t *testing.T) {
	for _, mode := range []string{"self_serve", "invite_register", "invite_only", ""} {
		t.Run(mode, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			h := &AuthHandler{configInfo: &config.Config{Auth: &config.AuthConfig{RegistrationMode: mode}}}
			r := gin.New()
			r.Use(errorCapture())
			r.POST("/auth/register-by-invite", h.RegisterByInvite)
			req := httptest.NewRequest(http.MethodPost, "/auth/register-by-invite", bytes.NewBufferString(`{"token":"legacy-link","email":"alice@example.com","username":"alice","password":"supersecret1"}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("mode %q status=%d", mode, w.Code)
			}
		})
	}
}
