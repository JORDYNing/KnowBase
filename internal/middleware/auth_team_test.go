package middleware

import (
	"context"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestTeamOrphanAndDisabledRBACDoNotGrantMembership(t *testing.T) {
	user := &types.User{ID: "removed", TenantID: types.TeamTenantID}
	svc := newFakeMemberService()
	role, ok := resolveTenantRole(context.Background(), svc, user, types.TeamTenantID, false, cfgWithRBAC(false))
	require.False(t, ok)
	require.Empty(t, role)
	require.Empty(t, svc.members)
	role, ok = resolveTenantRole(context.Background(), svc, user, types.TeamTenantID, false, nil)
	require.False(t, ok)
	require.Empty(t, role)
	require.True(t, rbacEnforcementEnabled(nil))
}
func TestTeamApplicationRoutesWorkWithoutTenant(t *testing.T) {
	require.True(t, isTenantOptionalAPI("/api/v1/me/tenant-join-request", http.MethodGet))
	require.True(t, isTenantOptionalAPI("/api/v1/me/tenant-join-request", http.MethodPost))
	require.False(t, isTenantOptionalAPI("/api/v1/tenants/10000/join-requests", http.MethodGet))
	require.False(t, isTenantOptionalAPI("/api/v1/knowledge-bases", http.MethodGet))
}

func TestTeamUnavailableOrRemovedMembershipStillAllowsOwnStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tenant := range []*types.Tenant{nil, {ID: types.TeamTenantID, Status: "inactive"}, {ID: types.TeamTenantID, Status: "active"}} {
		members := newFakeMemberService()
		user := &types.User{ID: "removed", TenantID: types.TeamTenantID, IsActive: true}
		router := gin.New()
		router.GET("/api/v1/me/tenant-join-request", func(c *gin.Context) {
			if authenticateJWTUser(c, &fakeTenantService{tenant: tenant}, members, cfgWithRBAC(false), user, types.TeamTenantID) {
				tenantID, _ := types.TenantIDFromContext(c.Request.Context())
				require.Zero(t, tenantID)
				c.Status(http.StatusOK)
			}
		})
		router.GET("/api/v1/knowledge-bases", func(c *gin.Context) {
			if authenticateJWTUser(c, &fakeTenantService{tenant: tenant}, members, cfgWithRBAC(false), user, types.TeamTenantID) {
				c.Status(http.StatusOK)
			}
		})
		recorder := httptest.NewRecorder()
		statusRequest := httptest.NewRequest(http.MethodGet, "/api/v1/me/tenant-join-request", nil)
		statusRequest.Header.Set("X-Tenant-ID", "10000")
		router.ServeHTTP(recorder, statusRequest)
		require.Equal(t, http.StatusOK, recorder.Code)
		recorder = httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-bases", nil))
		require.NotEqual(t, http.StatusOK, recorder.Code)
	}
}
func TestTeamOldTenantlessTokenBecomesUsableOnlyAfterApproval(t *testing.T) {
	gin.SetMode(gin.TestMode)
	members := newFakeMemberService()
	user := &types.User{ID: "alice", IsActive: true}
	tenants := &fakeTenantService{tenant: &types.Tenant{ID: types.TeamTenantID, Status: "active"}}
	router := gin.New()
	router.GET("/api/v1/knowledge-bases", func(c *gin.Context) {
		if authenticateJWTUser(c, tenants, members, cfgWithRBAC(true), user, 0) {
			c.Status(http.StatusOK)
		}
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-bases", nil))
	require.Equal(t, http.StatusConflict, recorder.Code)
	members.seedActive("alice", types.TeamTenantID, types.TenantRoleContributor)
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge-bases", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestTeamDisabledAccountCannotUseExistingActiveMembership(t *testing.T) {
	gin.SetMode(gin.TestMode)
	members := newFakeMemberService()
	members.seedActive("alice", types.TeamTenantID, types.TenantRoleOwner)
	user := &types.User{ID: "alice", TenantID: types.TeamTenantID, IsActive: false}
	tenants := &fakeTenantService{tenant: &types.Tenant{ID: types.TeamTenantID, Status: "active"}}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/tenants/10000/join-requests", nil)
	require.False(t, authenticateJWTUser(ctx, tenants, members, cfgWithRBAC(false), user, types.TeamTenantID))
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}
