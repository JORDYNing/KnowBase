package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type resolveOwnAgentStub struct {
	agent *types.CustomAgent
	err   error
}

func (s *resolveOwnAgentStub) GetAgentByID(context.Context, string) (*types.CustomAgent, error) {
	return s.agent, s.err
}

func (s *resolveOwnAgentStub) CreateAgent(context.Context, *types.CustomAgent) (*types.CustomAgent, error) {
	panic("not implemented")
}
func (s *resolveOwnAgentStub) GetAgentByIDAndTenant(context.Context, string, uint64) (*types.CustomAgent, error) {
	panic("not implemented")
}
func (s *resolveOwnAgentStub) ListAgents(context.Context) ([]*types.CustomAgent, error) {
	panic("not implemented")
}
func (s *resolveOwnAgentStub) UpdateAgent(
	context.Context, *types.CustomAgent, *string,
) (*types.CustomAgent, error) {
	panic("not implemented")
}
func (s *resolveOwnAgentStub) DeleteAgent(context.Context, string) error {
	panic("not implemented")
}
func (s *resolveOwnAgentStub) CopyAgent(context.Context, string) (*types.CustomAgent, error) {
	panic("not implemented")
}
func (s *resolveOwnAgentStub) GetSuggestedQuestions(context.Context, string, []string, []string, []types.TagScope, int) ([]types.SuggestedQuestion, error) {
	panic("not implemented")
}
func (s *resolveOwnAgentStub) GetKnowledgeSuggestedQuestions(context.Context, string, []string, []string, []types.TagScope, int) ([]types.SuggestedQuestion, error) {
	panic("not implemented")
}

func newResolveAgentTestContext(tenantID uint64) (*gin.Context, context.Context) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	c.Set(types.UserIDContextKey.String(), "user-1")
	c.Set(types.TenantIDContextKey.String(), tenantID)
	ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, tenantID)
	c.Request = c.Request.WithContext(ctx)
	return c, ctx
}

func TestResolveAgentOnlyReturnsCallerTenantAgent(t *testing.T) {
	c, ctx := newResolveAgentTestContext(7)
	local := &types.CustomAgent{ID: "agent", TenantID: 7}
	h := &Handler{customAgentService: &resolveOwnAgentStub{agent: local}}
	agent, scope := h.resolveAgent(ctx, c, "agent", 0)
	require.Equal(t, local, agent)
	require.Zero(t, scope)
	agent, _ = h.resolveAgent(ctx, c, "agent", 84)
	require.Nil(t, agent)
	h.customAgentService = &resolveOwnAgentStub{agent: &types.CustomAgent{ID: "agent", TenantID: 84}}
	agent, _ = h.resolveAgent(ctx, c, "agent", 0)
	require.Nil(t, agent)
}
func TestTerminalProvisionPinOwnAgentCarriesNoWorkspace(t *testing.T) {
	c, ctx := newResolveAgentTestContext(7)
	c.Request = httptest.NewRequest(http.MethodGet, "/?agent_id=agent", nil).WithContext(ctx)
	h := &Handler{customAgentService: &resolveOwnAgentStub{agent: &types.CustomAgent{ID: "agent", TenantID: 7, Config: types.CustomAgentConfig{SandboxConfigID: "local-cfg"}}}}
	pin := h.terminalProvisionPin(ctx, c, true)
	require.Equal(t, service.SandboxPin{ConfigID: "local-cfg"}, pin)
}
