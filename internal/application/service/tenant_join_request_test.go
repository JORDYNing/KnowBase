package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type teamJoinRepoStub struct {
	interfaces.TenantJoinRequestRepository
	bootstrapCalls int
	submitCalls    int
	userID         string
	request        *types.TenantJoinRequest
}

func (r *teamJoinRepoStub) BootstrapOwner(_ context.Context, tenantID uint64, userID string) (bool, error) {
	r.bootstrapCalls++
	r.userID = userID
	return false, nil
}
func (r *teamJoinRepoStub) Submit(_ context.Context, tenantID uint64, userID string) (*types.TenantJoinRequest, error) {
	r.submitCalls++
	r.userID = userID
	return r.request, nil
}
func (r *teamJoinRepoStub) GetForUser(context.Context, uint64, string) (*types.TenantJoinRequest, error) {
	return r.request, nil
}

type teamTargetStub struct {
	interfaces.TenantService
	tenant *types.Tenant
}

func (s *teamTargetStub) GetTenantByID(context.Context, uint64) (*types.Tenant, error) {
	return s.tenant, nil
}

type teamMembersStub struct {
	interfaces.TenantMemberService
	member *types.TenantMember
}

func (s *teamMembersStub) GetMembership(context.Context, string, uint64) (*types.TenantMember, error) {
	return s.member, nil
}

func TestTenantJoinBootstrapUsesOnlyVerifiedProviderEmail(t *testing.T) {
	ctx := context.Background()
	repo := &teamJoinRepoStub{}
	svc := &tenantJoinRequestService{repo: repo}
	user := &types.User{ID: "arbitrary-local-id", Email: types.TeamBootstrapOwnerEmail, FeishuOpenID: "ou_user", IsActive: true}
	require.NoError(t, svc.EnsureFeishuApplication(ctx, user, "other@example.com"))
	require.Zero(t, repo.bootstrapCalls)
	require.NoError(t, svc.EnsureFeishuApplication(ctx, user, " JORDYN.JI@IDREAMSKY.COM "))
	require.Equal(t, 1, repo.bootstrapCalls)
	require.Equal(t, user.ID, repo.userID)
	require.False(t, user.IsSystemAdmin)
	require.False(t, user.CanAccessAllTenants)
	user.IsActive = false
	require.ErrorIs(t, svc.EnsureFeishuApplication(ctx, user, types.TeamBootstrapOwnerEmail), types.ErrJoinRequestForbidden)
	user.IsActive = true
	user.FeishuOpenID = ""
	require.ErrorIs(t, svc.EnsureFeishuApplication(ctx, user, types.TeamBootstrapOwnerEmail), types.ErrJoinRequestForbidden)
}
func TestTenantJoinStateDistinguishesPendingRejectedRemovedAndUnavailable(t *testing.T) {
	ctx := context.Background()
	repo := &teamJoinRepoStub{request: &types.TenantJoinRequest{Status: types.TenantJoinRequestPending, Source: types.TenantJoinSourceFeishu}}
	target := &teamTargetStub{tenant: &types.Tenant{ID: types.TeamTenantID, Name: "Team", Status: "active"}}
	members := &teamMembersStub{}
	svc := &tenantJoinRequestService{repo: repo, tenants: target, members: members}
	state, err := svc.GetState(ctx, "alice")
	require.NoError(t, err)
	require.True(t, state.TargetAvailable)
	require.False(t, state.CanResubmit)
	require.Nil(t, state.Membership)
	repo.request.Status = types.TenantJoinRequestRejected
	state, err = svc.GetState(ctx, "alice")
	require.NoError(t, err)
	require.True(t, state.CanResubmit)
	repo.request.Status = types.TenantJoinRequestApproved
	state, err = svc.GetState(ctx, "alice")
	require.NoError(t, err)
	require.True(t, state.AccessRevoked)
	require.False(t, state.CanResubmit)
	members.member = &types.TenantMember{TenantID: types.TeamTenantID, Role: types.TenantRoleContributor, Status: types.TenantMemberStatusSuspended}
	state, err = svc.GetState(ctx, "alice")
	require.NoError(t, err)
	require.Nil(t, state.Membership)
	require.True(t, state.AccessRevoked)
	members.member.Status = types.TenantMemberStatusActive
	state, err = svc.GetState(ctx, "alice")
	require.NoError(t, err)
	require.NotNil(t, state.Membership)
	require.False(t, state.AccessRevoked)
	target.tenant.Status = "inactive"
	state, err = svc.GetState(ctx, "alice")
	require.NoError(t, err)
	require.False(t, state.TargetAvailable)
	target.tenant = nil
	state, err = svc.GetState(ctx, "alice")
	require.NoError(t, err)
	require.False(t, state.TargetAvailable)
	require.Nil(t, state.Tenant)
}
func TestTenantJoinApprovedBootstrapCannotResubmit(t *testing.T) {
	repo := &teamJoinRepoStub{request: &types.TenantJoinRequest{Status: types.TenantJoinRequestApproved, Source: types.TenantJoinSourceBootstrap, Version: 1}}
	svc := &tenantJoinRequestService{repo: repo}
	_, err := svc.Submit(context.Background(), "alice", 1)
	require.ErrorIs(t, err, types.ErrJoinRequestConflict)
	require.Zero(t, repo.submitCalls)
}
func TestTeamRegistrationNeverCreatesPersonalTenant(t *testing.T) {
	repo := &provisioningUserRepo{}
	tenants := &provisioningTenantService{}
	svc := &userService{userRepo: repo, tenantService: tenants}
	user, err := svc.Register(context.Background(), &types.RegisterRequest{Username: "alice", Email: "alice@example.com", Password: "test-secret", TenantProvisioning: types.TenantProvisioningCreatePersonal})
	require.NoError(t, err)
	require.Zero(t, tenants.createCalls)
	require.Zero(t, user.TenantID)
}
