package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTeamLegacyHumanAuthenticationAlwaysDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &AuthHandler{configInfo: &config.Config{Auth: &config.AuthConfig{RegistrationMode: config.AuthRegistrationModeSelfServe}, OIDCAuth: &config.OIDCAuthConfig{Enable: true}}}
	for _, tt := range []struct {
		name    string
		handler gin.HandlerFunc
	}{
		{"password", h.Login}, {"registration", h.Register}, {"invite registration", h.RegisterByInvite}, {"invite lookup", h.LookupInvitationByToken},
		{"oidc url", h.GetOIDCAuthorizationURL}, {"oidc start", h.OIDCStart}, {"oidc callback", h.OIDCRedirectCallback}, {"lite setup", h.AutoSetup},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(tenantPolicyErrorCapture())
			r.POST("/legacy", tt.handler)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/legacy", bytes.NewBufferString(`{"email":"x@example.com","password":"test-secret"}`))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
		})
	}
}
func TestTeamAuthConfigIsFeishuOnlyDespiteLegacySettings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &AuthHandler{configInfo: &config.Config{OIDCAuth: &config.OIDCAuthConfig{Enable: true}}}
	r := gin.New()
	r.GET("/config", h.GetAuthConfig)
	r.GET("/oidc", h.GetOIDCConfig)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/config", nil))
	var payload map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	require.Equal(t, false, payload["password_login_enabled"])
	require.Equal(t, false, payload["oidc_enabled"])
	require.Equal(t, []any{"feishu"}, payload["login_methods"])
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/oidc", nil))
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	require.Equal(t, false, payload["enabled"])
}
