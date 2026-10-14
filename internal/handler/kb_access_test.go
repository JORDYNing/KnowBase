package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type handlerKnowledgeAccessStub struct {
	interfaces.KnowledgeService
	knowledge *types.Knowledge
}

func (s *handlerKnowledgeAccessStub) GetKnowledgeByIDOnly(context.Context, string) (*types.Knowledge, error) {
	return s.knowledge, nil
}

func TestKBHandlerDoesNotReuseGrantForAnotherResourceOrCaller(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name, requestedID string
		caller            uint64
	}{
		{name: "another KB", requestedID: "other", caller: 1},
		{name: "another caller", requestedID: "kb", caller: 3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			c.Set(types.TenantIDContextKey.String(), tt.caller)
			c.Set(middleware.KBAccessContextKey, &middleware.KBAccess{
				KnowledgeBase: &types.KnowledgeBase{ID: "kb", TenantID: 2},
				Caller: types.Caller{
					TenantID: 1,
					Role:     types.TenantRoleViewer,
				}, EffectiveTenantID: 2, Permission: types.KBPermissionAdmin,
			})
			svc := &stubKBOnlyService{getByID: func(context.Context, string) (*types.KnowledgeBase, error) {
				return &types.KnowledgeBase{ID: tt.requestedID, TenantID: 2}, nil
			}}
			_, err := resolveHandlerKBAccess(c, tt.requestedID, svc)
			require.Error(t, err)
		})
	}
}

func TestKBHandlerAPIKeyScopeStillAppliesToCachedGrant(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx := types.WithTenantAPIKeyScope(
		context.Background(),
		types.TenantAPIKeyScope{KnowledgeBaseIDs: types.StringArray{"other"}},
	)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	c.Set(types.TenantIDContextKey.String(), uint64(1))
	c.Set(middleware.KBAccessContextKey, &middleware.KBAccess{
		KnowledgeBase: &types.KnowledgeBase{ID: "kb", TenantID: 1},
		Caller: types.Caller{
			TenantID: 1,
			Role:     types.TenantRoleViewer,
		}, EffectiveTenantID: 1, Permission: types.KBPermissionAdmin,
	})
	_, err := resolveHandlerKBAccess(c, "kb", nil)
	require.Error(t, err)
}

func TestKBHandlerEnforcesAccessWhenRouteRBACIsDisabled(t *testing.T) {
	svc := &stubKBOnlyService{getByID: func(context.Context, string) (*types.KnowledgeBase, error) {
		return &types.KnowledgeBase{ID: "kb", TenantID: 2}, nil
	}}
	r := gin.New()
	r.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		c.Set(types.TenantIDContextKey.String(), uint64(1))
		c.Next()
	})
	disabled := false
	r.GET("/:id", middleware.RequireKBAccess(middleware.KBIDFromParam("id"), types.KBPermissionRead,
		svc, &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &disabled}}), func(c *gin.Context) {
		_, err := resolveHandlerKBAccess(c, c.Param("id"), svc)
		require.Error(t, err)
		_ = c.Error(err)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/kb", nil))
	require.Equal(t, http.StatusForbidden, w.Code)
}
