package access

import (
	"context"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestResolveKBRejectsForeignTenantForEveryPermission(t *testing.T) {
	for _, perm := range []types.KBPermission{types.KBPermissionRead, types.KBPermissionWrite, types.KBPermissionAdmin} {
		request := KBRequest{Caller: types.Caller{TenantID: 1}, AgentID: "agent", AgentSourceTenantID: "2"}
		grant, err := ResolveKB(context.Background(), request, &types.KnowledgeBase{ID: "kb", TenantID: 2}, perm)
		require.ErrorIs(t, err, ErrForbidden)
		require.Nil(t, grant)
		grant, err = ResolveKB(context.Background(), request, &types.KnowledgeBase{ID: "kb", TenantID: 1}, perm)
		require.NoError(t, err)
		require.Equal(t, types.KBPermissionAdmin, grant.Permission)
	}
}
func TestResolveKBMissingIdentityResourceAndAPIKeyScope(t *testing.T) {
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 1}
	req := KBRequest{Caller: types.Caller{TenantID: 1}}
	_, err := ResolveKB(context.Background(), KBRequest{}, kb, types.KBPermissionRead)
	require.ErrorIs(t, err, ErrUnauthorized)
	_, err = ResolveKB(context.Background(), req, nil, types.KBPermissionRead)
	require.ErrorIs(t, err, ErrNotFound)
	ctx := types.WithTenantAPIKeyScope(context.Background(), types.TenantAPIKeyScope{KnowledgeBaseIDs: types.StringArray{"other"}})
	_, err = ResolveKB(ctx, req, kb, types.KBPermissionRead)
	require.Error(t, err)
	_, err = ResolveKB(context.Background(), req, kb, types.KBPermission("unknown"))
	require.ErrorIs(t, err, ErrForbidden)
}
