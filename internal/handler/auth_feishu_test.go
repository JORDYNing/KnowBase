package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

type stubFeishuUserService struct {
	interfaces.UserService
	getFeishuAuthorizationURL func(ctx context.Context, redirectURI string) (*types.OIDCAuthURLResponse, error)
}

func (s *stubFeishuUserService) GetFeishuAuthorizationURL(ctx context.Context, redirectURI string) (*types.OIDCAuthURLResponse, error) {
	return s.getFeishuAuthorizationURL(ctx, redirectURI)
}

func TestFeishuStart_RedirectsAndSetsNonce(t *testing.T) {
	const authURL = "https://accounts.feishu.cn/open-apis/authen/v1/authorize?client_id=cli_x"
	us := &stubFeishuUserService{
		getFeishuAuthorizationURL: func(context.Context, string) (*types.OIDCAuthURLResponse, error) {
			return &types.OIDCAuthURLResponse{
				Success:          true,
				AuthorizationURL: authURL,
				State:            "state123",
				Nonce:            "nonce456",
			}, nil
		},
	}
	h := NewAuthHandler(&config.Config{}, us, nil, nil, nil)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(errorCapture())
	r.GET("/auth/feishu/start", h.FeishuStart)

	req := httptest.NewRequest(http.MethodGet, "/auth/feishu/start", nil)
	req.Host = "knowbase.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != authURL {
		t.Errorf("Location = %q, want %q", loc, authURL)
	}
	var hasNonce bool
	for _, ck := range w.Result().Cookies() {
		if ck.Name == oidcNonceCookieName && ck.Value == "nonce456" {
			hasNonce = true
		}
	}
	if !hasNonce {
		t.Errorf("expected %s cookie", oidcNonceCookieName)
	}
}

func TestFeishuStart_BuildsCallbackFromOrigin(t *testing.T) {
	var captured string
	us := &stubFeishuUserService{
		getFeishuAuthorizationURL: func(_ context.Context, redirectURI string) (*types.OIDCAuthURLResponse, error) {
			captured = redirectURI
			return &types.OIDCAuthURLResponse{Success: true, AuthorizationURL: "https://accounts.feishu.cn/x", Nonce: "n"}, nil
		},
	}
	h := NewAuthHandler(&config.Config{}, us, nil, nil, nil)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(errorCapture())
	r.GET("/auth/feishu/start", h.FeishuStart)

	req := httptest.NewRequest(http.MethodGet, "/auth/feishu/start", nil)
	req.Host = "knowbase.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	want := "https://knowbase.example.com/api/v1/auth/feishu/callback"
	if captured != want {
		t.Errorf("callback = %q, want %q", captured, want)
	}
}

func TestGetFeishuConfig_ReadyGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name    string
		cfg     *config.FeishuLoginConfig
		enabled bool
	}{
		{name: "nil", enabled: false},
		{name: "disabled", cfg: &config.FeishuLoginConfig{Enable: false, AppID: "a", AppSecret: "b"}, enabled: false},
		{name: "missing secret", cfg: &config.FeishuLoginConfig{Enable: true, AppID: "a"}, enabled: false},
		{name: "ready", cfg: &config.FeishuLoginConfig{Enable: true, AppID: "a", AppSecret: "b"}, enabled: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewAuthHandler(&config.Config{FeishuLogin: tc.cfg}, &stubFeishuUserService{}, nil, nil, nil)
			r := gin.New()
			r.GET("/auth/feishu/config", h.GetFeishuConfig)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/auth/feishu/config", nil))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d", w.Code)
			}
			var body types.FeishuConfigResponse
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Enabled != tc.enabled {
				t.Fatalf("enabled = %v, want %v", body.Enabled, tc.enabled)
			}
		})
	}
}
