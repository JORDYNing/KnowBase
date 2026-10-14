package middleware

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type stubKBLookup struct {
	kbs    map[string]*types.KnowledgeBase
	getErr error
}

func (s *stubKBLookup) GetKnowledgeBaseByID(_ context.Context, id string) (*types.KnowledgeBase, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if kb, ok := s.kbs[id]; ok {
		return kb, nil
	}
	return nil, apprepo.ErrKnowledgeBaseNotFound
}

func TestRequireKBAccessWorkspaceBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, permission := range []types.KBPermission{types.KBPermissionRead, types.KBPermissionWrite, types.KBPermissionAdmin} {
		for _, tc := range []struct {
			name          string
			caller, owner uint64
			lookupErr     error
			denied        bool
		}{
			{name: "own workspace", caller: 100, owner: 100},
			{name: "other workspace", caller: 100, owner: 200, denied: true},
			{name: "no workspace", caller: 0, owner: 100, denied: true},
			{name: "missing", caller: 100, denied: true},
			{name: "database failure", caller: 100, owner: 100, lookupErr: errors.New("database unavailable"), denied: true},
		} {
			t.Run(string(permission)+"/"+tc.name, func(t *testing.T) {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("GET", "/knowledge-bases/kb-1?agent_id=arbitrary&agent_source_tenant_id=200", nil)
				c.Params = gin.Params{{Key: "id", Value: "kb-1"}}
				caller := types.Caller{TenantID: tc.caller, UserID: "user-1", Role: types.TenantRoleAdmin}
				c.Request = c.Request.WithContext(types.WithCaller(c.Request.Context(), caller))
				c.Set(types.TenantIDContextKey.String(), tc.caller)
				c.Set(types.UserIDContextKey.String(), caller.UserID)
				lookup := &stubKBLookup{kbs: map[string]*types.KnowledgeBase{}, getErr: tc.lookupErr}
				if tc.owner > 0 {
					lookup.kbs["kb-1"] = &types.KnowledgeBase{ID: "kb-1", TenantID: tc.owner}
				}
				RequireKBAccess(KBIDFromParam("id"), permission, lookup, &config.Config{})(c)
				require.Equal(t, tc.denied, c.IsAborted())
				if tc.denied {
					require.NotEmpty(t, c.Errors)
					return
				}
				grant, ok := KBAccessFromContext(c)
				require.True(t, ok)
				require.Equal(t, tc.caller, grant.EffectiveTenantID)
			})
		}
	}
}

func TestIsResourceNotFoundRecognizesResourceSentinels(t *testing.T) {
	for _, err := range []error{apprepo.ErrKnowledgeNotFound, apprepo.ErrKnowledgeBaseNotFound, apprepo.ErrChunkNotFound, ErrResourceNotFound} {
		require.True(t, isResourceNotFound(err))
	}
	require.False(t, isResourceNotFound(errors.New("database unavailable")))
}
