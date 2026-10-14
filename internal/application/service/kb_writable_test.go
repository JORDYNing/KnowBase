package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestKBWritableIDs(t *testing.T) {
	targets := types.SearchTargets{
		{KnowledgeBaseID: "own", TenantID: 42},
		{KnowledgeBaseID: "shared-editor", TenantID: 7},
		{KnowledgeBaseID: "shared-viewer", TenantID: 7},
		{KnowledgeBaseID: "agent-scope", TenantID: 7},
	}
	caller := func(role types.TenantRole, userID string) context.Context {
		return types.WithCaller(context.Background(), types.Caller{TenantID: 42, UserID: userID, Role: role})
	}
	apiKey := func(scope types.TenantAPIKeyScope) context.Context {
		return types.WithTenantAPIKeyScope(caller(types.TenantRoleViewer, "system-42"), scope)
	}

	require.Equal(t, []string{"own"},
		kbWritableIDs(caller(types.TenantRoleContributor, "u"), targets, true))
	require.Equal(t, []string{"own"}, kbWritableIDs(caller(types.TenantRoleContributor, ""), targets, true),
		"callers without a user do not expand through org shares")

	// A tenant Viewer (also the IM / embed / MCP endpoint principals) is
	// read-only, like on the HTTP write routes; with RBAC enforcement off the
	// role check only logs, but a share never grants a Viewer more than read.
	require.Empty(t, kbWritableIDs(caller(types.TenantRoleViewer, "u"), targets, true))
	require.Equal(t, []string{"own"}, kbWritableIDs(caller(types.TenantRoleViewer, "u"), targets, false))

	// Scoped API keys write only with the ingest capability.
	chatOnly := types.TenantAPIKeyScope{Capabilities: types.StringArray{string(types.APIKeyCapabilityChat)}}
	require.Empty(t, kbWritableIDs(apiKey(chatOnly), targets, true))
	ingest := types.TenantAPIKeyScope{Capabilities: types.StringArray{string(types.APIKeyCapabilityIngest)}}
	require.Equal(t, []string{"own"}, kbWritableIDs(apiKey(ingest), targets, true))
}
