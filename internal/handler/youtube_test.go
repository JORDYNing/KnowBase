package handler

import (
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestYouTubeConfigViewerDeniedAndAdminRedacted(t *testing.T) {
	tenant := &types.Tenant{ID: 42, YouTubeTranscriptConfig: &types.YouTubeTranscriptConfig{ProxyURL: "http://alice:super-secret@proxy.example:8080"}}
	for _, role := range []types.TenantRole{types.TenantRoleViewer, types.TenantRoleContributor, types.TenantRoleAdmin} {
		engine := newTenantHandlerTestEngine(t, role, tenant)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tenants/kv/youtube-transcript-config", nil))
		if role == types.TenantRoleAdmin {
			require.Equal(t, 200, rec.Code)
			require.Contains(t, rec.Body.String(), "proxy_url")
		} else {
			require.Equal(t, 403, rec.Code)
		}
		require.NotContains(t, rec.Body.String(), "super-secret")
		if role != types.TenantRoleAdmin {
			rec = httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPut, "/tenants/kv/youtube-transcript-config", strings.NewReader(`{"enabled":false}`))
			req.Header.Set("Content-Type", "application/json")
			engine.ServeHTTP(rec, req)
			require.Equal(t, 403, rec.Code)
		}
	}
}

func TestYouTubeConfigRejectsMissingDestinationAndInvalidProxy(t *testing.T) {
	tenant := &types.Tenant{ID: 42, YouTubeTranscriptConfig: &types.YouTubeTranscriptConfig{ProxyURL: "http://alice:super-secret@proxy.example:8080"}}
	engine := newTenantHandlerTestEngine(t, types.TenantRoleAdmin, tenant)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/tenants/kv/youtube-transcript-config", strings.NewReader(`{"enabled":true}`))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, req)
	require.Equal(t, 400, rec.Code)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/tenants/kv/youtube-transcript-config", strings.NewReader(`{"enabled":false,"proxy_url":"file:///etc/passwd"}`))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, req)
	require.Equal(t, 400, rec.Code)
}
