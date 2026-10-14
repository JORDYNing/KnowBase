package access

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func callerContext() context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	ctx = context.WithValue(ctx, types.UserIDContextKey, "user")
	return context.WithValue(ctx, types.TenantRoleContextKey, types.TenantRoleContributor)
}

func TestKBGrantSurvivesExecutionSwitchAndDetachWithoutWidening(t *testing.T) {
	ctx := callerContext()
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 1}
	grant, err := ResolveKB(
		ctx,
		KBRequest{Caller: types.CallerFromContext(ctx)},
		kb,
		types.KBPermissionRead,
	)
	require.NoError(t, err)
	ctx = grant.Context(ctx)
	require.Equal(t, uint64(1), types.MustTenantIDFromContext(ctx))
	ctx = logger.CloneContext(types.WithExecutionTenant(ctx, 3))
	require.Equal(
		t,
		types.Caller{TenantID: 1, UserID: "user", Role: types.TenantRoleContributor},
		types.CallerFromContext(ctx),
	)
	require.True(t, HasKBGrant(ctx, "kb", 1, types.KBPermissionRead))
	// A later mutation of the service model cannot mutate the captured grant.
	kb.ID = "private"
	require.False(t, HasKBGrant(ctx, "private", 1, types.KBPermissionRead))
	require.False(t, HasKBGrant(ctx, "kb", 3, types.KBPermissionRead))
	require.False(t, HasKBGrant(ctx, "kb", 1, types.KBPermissionWrite))
	for _, caller := range []types.Caller{
		{TenantID: 9, UserID: "user", Role: types.TenantRoleContributor},
		{TenantID: 1, UserID: "other", Role: types.TenantRoleContributor},
		{TenantID: 1, UserID: "user", Role: types.TenantRoleViewer},
	} {
		otherCtx := types.WithCaller(ctx, caller)
		require.False(t, HasKBGrant(otherCtx, "kb", 1, types.KBPermissionRead))
		require.Equal(
			t,
			caller,
			types.CallerFromContext(grant.Context(otherCtx)),
			"reusing a grant cannot impersonate its caller",
		)
		require.Equal(
			t,
			uint64(3),
			types.MustTenantIDFromContext(grant.Context(otherCtx)),
			"a caller mismatch must not change execution scope",
		)
	}
	narrowed := types.WithTenantAPIKeyScope(
		ctx,
		types.TenantAPIKeyScope{KnowledgeBaseIDs: types.StringArray{"another"}},
	)
	require.False(t, HasKBGrant(narrowed, "kb", 1, types.KBPermissionRead))
}

func TestExecutionTenantDoesNotGrantOwnership(t *testing.T) {
	ctx := types.WithExecutionTenant(callerContext(), 2)
	allowed, err := NewKBPermissions(ctx).Check("private", 2, types.KBPermissionRead)
	require.NoError(t, err)
	require.False(t, allowed)
	ctx = types.WithExecutionTenant(context.Background(), 2)
	allowed, err = NewKBPermissions(ctx).Check("private", 2, types.KBPermissionRead)
	require.NoError(t, err)
	require.False(t, allowed, "an absent caller must not become the execution tenant")
}

func TestKBGrantBranchesRemainIndependent(t *testing.T) {
	base := callerContext()
	grant := &KBAccess{Caller: types.CallerFromContext(base), EffectiveTenantID: 2, Permission: types.KBPermissionRead}
	grant.KnowledgeBase = &types.KnowledgeBase{ID: "first", TenantID: 2}
	first := grant.WithGrant(base)
	grant.KnowledgeBase = &types.KnowledgeBase{ID: "second", TenantID: 2}
	second := grant.WithGrant(base)
	require.True(t, HasKBGrant(first, "first", 2, types.KBPermissionRead))
	require.False(t, HasKBGrant(first, "second", 2, types.KBPermissionRead))
	require.True(t, HasKBGrant(second, "second", 2, types.KBPermissionRead))
	require.False(t, HasKBGrant(second, "first", 2, types.KBPermissionRead))
}

func TestKBPermissionsOwnerShortcutOnlyGrantsRead(t *testing.T) {
	roles := []types.TenantRole{types.TenantRoleViewer, types.TenantRoleContributor, types.TenantRoleAdmin}
	for _, role := range roles {
		ctx := context.WithValue(callerContext(), types.TenantRoleContextKey, role)
		permissions := NewKBPermissions(ctx)
		for _, required := range []types.KBPermission{types.KBPermissionRead, types.KBPermissionWrite, types.KBPermissionAdmin} {
			allowed, err := permissions.Check("kb", 1, required)
			require.NoError(t, err)
			require.Equal(t, required == types.KBPermissionRead, allowed)
		}
		grant := &KBAccess{
			Caller:            types.CallerFromContext(ctx),
			KnowledgeBase:     &types.KnowledgeBase{ID: "kb", TenantID: 1},
			EffectiveTenantID: 1,
			Permission:        types.KBPermissionWrite,
		}
		permissions = NewKBPermissions(grant.Context(ctx))
		allowed, err := permissions.Check("kb", 1, types.KBPermissionWrite)
		require.NoError(t, err)
		require.True(t, allowed)
		allowed, err = permissions.Check("kb", 1, types.KBPermissionAdmin)
		require.NoError(t, err)
		require.False(t, allowed)
	}
}
