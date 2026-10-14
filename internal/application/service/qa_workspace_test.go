package service

import (
	"context"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestQAWorkspaceRejectsForeignSessionOrAgent(t *testing.T) {
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 1})
	req := &types.QARequest{Session: &types.Session{TenantID: 1}, CustomAgent: &types.CustomAgent{TenantID: 1}}
	require.NoError(t, validateQAWorkspace(ctx, req))
	req.CustomAgent.TenantID = 2
	require.Error(t, validateQAWorkspace(ctx, req))
	req.CustomAgent = nil
	req.Session.TenantID = 2
	require.Error(t, validateQAWorkspace(ctx, req))
	require.Error(t, validateQAWorkspace(ctx, nil))
	svc := &sessionService{}
	require.Equal(t, uint64(1), svc.resolveRetrievalTenantID(types.WithExecutionTenant(ctx, 2), req))
}
